package com.hushield.android.attest

import android.util.Base64
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
    suspend fun attest(keyId: String, clientDataHash: ByteArray): ByteArray
    suspend fun assert(keyId: String, clientDataHash: ByteArray): ByteArray
}

/** The seam around the real Play Integrity SDK call -- see PlayIntegrityTokenSource. */
interface IntegrityTokenSource {
    suspend fun requestToken(nonce: ByteArray): String
}

class RealAttestationProvider(
    private val keyManager: KeystoreKeyManager,
    private val integrityDecoder: IntegrityTokenSource
) : AttestationProvider {

    override suspend fun generateKeyId(): String {
        val pubKey = keyManager.generateKey(DEVICE_KEY_ALIAS)
        return deriveKeyId(pubKey.encoded)
    }

    override suspend fun attest(keyId: String, clientDataHash: ByteArray): ByteArray {
        val pubKey = currentKeyMatching(keyId)
        val nonce = MessageDigest.getInstance("SHA-256").digest(clientDataHash + pubKey.encoded)
        val integrityToken = integrityDecoder.requestToken(nonce)
        val envelope = JSONObject()
            .put("integrity_token", integrityToken)
            .put("public_key_der", Base64.encodeToString(pubKey.encoded, Base64.NO_WRAP))
        return envelope.toString().toByteArray()
    }

    override suspend fun assert(keyId: String, clientDataHash: ByteArray): ByteArray {
        // Confirm keyId still names the live device key before signing --
        // catches a caller passing a keyId from before the key was rotated
        // by a later generateKeyId() call.
        currentKeyMatching(keyId)
        // Full wire-format correctness (counter, JSON shape) is Task 4's
        // job (AndroidAssertion.kt) -- this delegates signing to the
        // Keystore key directly so assert() has a working seam now; Task 4
        // is expected to refactor this call site once it lands.
        return keyManager.sign(DEVICE_KEY_ALIAS, clientDataHash)
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
