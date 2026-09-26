package com.hushield.android.attest

import android.util.Base64
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import org.json.JSONObject
import java.security.MessageDigest
import java.security.PublicKey

sealed class AttestationProviderException : Exception() {
    object Unknown : AttestationProviderException()
    object KeyUnusable : AttestationProviderException()
}

/**
 * Abstraction over the device-identity key lifecycle, mirroring
 * ios/SpamFilterKit/AttestationProvider.swift's `AttestationProvider`
 * protocol so EnrollmentService (Task 6) can be written and tested against
 * the same shape on both platforms.
 *
 * Like the iOS protocol, an `AttestationProvider` persists nothing of its
 * own: the caller (EnrollmentService/TokenStore on iOS; the Android
 * equivalent from a later task) is the sole persistent owner of the
 * returned `keyId`, and only calls `generateKeyId()` again once that stored
 * value has been cleared.
 */
interface AttestationProvider {
    suspend fun generateKeyId(): String

    /**
     * `challenge` here is the RAW server challenge bytes (base64-decoded,
     * NOT pre-hashed) -- the nonce this builds is
     * SHA256(challenge || pubKey.encoded), matching the Go backend's
     * VerifyAttestation (internal/api/attest.go's handleVerify passes the
     * raw decoded challenge, never pre-hashed, into VerifyAttestation).
     * Unlike [assert]'s `clientDataHash` parameter, this one is deliberately
     * NOT a digest -- see EnrollmentService.enroll() for the call site.
     */
    suspend fun attest(keyId: String, challenge: ByteArray): ByteArray

    /**
     * `clientDataHash` here IS pre-hashed (SHA256(challenge)) -- matching
     * the Go backend's handleAssert, which independently computes
     * `sha256.Sum256(chBytes)` before calling VerifyAssertion. See
     * EnrollmentService.refresh() for the call site.
     */
    suspend fun assert(keyId: String, clientDataHash: ByteArray): ByteArray
}

/** The seam around the real Play Integrity SDK call -- see PlayIntegrityTokenSource. */
interface IntegrityTokenSource {
    suspend fun requestToken(nonce: ByteArray): String
}

class RealAttestationProvider(
    private val keyManager: KeystoreKeyManager,
    private val integrityDecoder: IntegrityTokenSource,
    private val androidAssertion: AndroidAssertion
) : AttestationProvider {

    // Guards every operation below: all three target the SAME fixed
    // Keystore alias (DEVICE_KEY_ALIAS), and KeystoreKeyManager.generateKey
    // PHYSICALLY OVERWRITES whatever key currently lives at that alias. Two
    // concurrent generateKeyId() calls -- e.g. two callers racing to enroll
    // when no key exists yet -- would otherwise both generate against the
    // same alias, and the second's key material would silently replace the
    // first's, orphaning any keyId/public key the first call already
    // returned (and any server that already accepted it). The mutex makes
    // generateKeyId(), attest(), and assert() mutually exclusive against
    // this provider's key state so that race can't happen.
    private val mutex = Mutex()

    override suspend fun generateKeyId(): String = mutex.withLock {
        val pubKey = keyManager.generateKey(DEVICE_KEY_ALIAS)
        deriveKeyId(pubKey.encoded)
    }

    override suspend fun attest(keyId: String, challenge: ByteArray): ByteArray = mutex.withLock {
        val pubKey = currentKeyMatching(keyId)
        val nonce = MessageDigest.getInstance("SHA-256").digest(challenge + pubKey.encoded)
        val integrityToken = integrityDecoder.requestToken(nonce)
        val envelope = JSONObject()
            .put("integrity_token", integrityToken)
            .put("public_key_der", Base64.encodeToString(pubKey.encoded, Base64.NO_WRAP))
        envelope.toString().toByteArray()
    }

    override suspend fun assert(keyId: String, clientDataHash: ByteArray): ByteArray = mutex.withLock {
        // Confirm keyId still names the live device key before signing --
        // catches a caller passing a keyId from before the key was rotated
        // by a later generateKeyId() call.
        currentKeyMatching(keyId)
        try {
            androidAssertion.sign(DEVICE_KEY_ALIAS, keyId, clientDataHash)
        } catch (e: android.security.keystore.KeyPermanentlyInvalidatedException) {
            // The Keystore entry named by DEVICE_KEY_ALIAS is no longer
            // usable -- the Android analog of DCError.invalidKey on iOS.
            // EnrollmentService.validToken() catches this, clears the stored
            // identity, and enrolls once with a genuinely new key.
            throw AttestationProviderException.KeyUnusable
        }
    }

    private fun currentKeyMatching(keyId: String): PublicKey {
        val pubKey = keyManager.publicKey(DEVICE_KEY_ALIAS) ?: throw AttestationProviderException.KeyUnusable
        if (deriveKeyId(pubKey.encoded) != keyId) throw AttestationProviderException.KeyUnusable
        return pubKey
    }

    private fun deriveKeyId(pubKeyDer: ByteArray): String =
        Base64.encodeToString(MessageDigest.getInstance("SHA-256").digest(pubKeyDer), Base64.NO_WRAP)

    companion object {
        /**
         * The single Keystore alias this device's identity key always
         * lives under.
         *
         * `generateKeyId()`'s return value is the SHA-256 of the key it
         * just generated, but `KeystoreKeyManager.generateKey(alias)` needs
         * an alias BEFORE that key exists -- a chicken-and-egg problem
         * (see task-3-report.md for the alternatives considered and why
         * this one was chosen over the brief's throwaway-then-regenerate
         * sample and over an internal keyId->alias map).
         */
        internal const val DEVICE_KEY_ALIAS = "hushield-device-identity-key"
    }
}
