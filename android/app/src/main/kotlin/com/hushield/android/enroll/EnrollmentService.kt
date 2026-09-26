package com.hushield.android.enroll

import com.hushield.android.attest.AttestationProvider
import com.hushield.android.attest.AttestationProviderException
import com.hushield.android.net.APIClient
import com.hushield.android.store.TokenStore
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.security.MessageDigest
import java.time.Instant
import java.time.format.DateTimeFormatter
import java.util.Base64

sealed class EnrollmentException : Exception() {
    object NotEnrolled : EnrollmentException()
    data class MalformedExpiry(val raw: String) : EnrollmentException()
}

/**
 * Composes APIClient + AttestationProvider + TokenStore into the enroll/
 * refresh flow described by the backend's attest endpoints (challenge,
 * verify, assert), mirroring ios/SpamFilterKit/EnrollmentService.swift
 * method-for-method.
 */
class EnrollmentService(
    private val apiClient: APIClient,
    private val provider: AttestationProvider,
    private val tokenStore: TokenStore,
    private val tokenExpirySkewSeconds: Long = 30
) {

    // Guards validToken()'s whole enroll-or-refresh-and-recovery sequence as
    // a single-flight operation. provider's own mutex (RealAttestationProvider)
    // only makes each individual generateKeyId()/attest()/assert() call
    // atomic; it says nothing about two concurrent validToken() callers
    // interleaving generateKey (overwriting the shared alias) with a still
    // in-flight attest()/verify() from another caller, or two recovery
    // paths (tokenStore.clear() + enroll()) stepping on each other. This
    // Mutex is NOT reentrant, so it must not be acquired from within
    // enroll()/refresh() too -- those stay unlocked and are only reached
    // through validToken()'s lock.
    private val mutex = Mutex()

    suspend fun enroll() {
        val keyId = tokenStore.loadKeyId() ?: provider.generateKeyId()

        val challengeResponse = apiClient.challenge()
        // attest()'s nonce needs the RAW challenge, not SHA256(challenge):
        // the Go backend's handleVerify passes the raw decoded challenge
        // bytes into VerifyAttestation and never pre-hashes it for this
        // path (unlike handleAssert below, which does). Passing the hash
        // here would make the nonce SHA256(SHA256(challenge) || pubKey),
        // one hash layer too many.
        val rawChallenge = decodeChallenge(challengeResponse.challenge)
        val attestation = provider.attest(keyId, rawChallenge)
        val tokenResponse = apiClient.verify(
            keyId = keyId,
            attestationB64 = Base64.getEncoder().encodeToString(attestation),
            challengeB64 = challengeResponse.challenge
        )
        val expiresAt = parseExpiry(tokenResponse.expiresAt)

        tokenStore.saveKeyId(keyId)
        tokenStore.saveToken(tokenResponse.deviceToken, expiresAt)
    }

    suspend fun refresh() {
        val keyId = tokenStore.loadKeyId() ?: throw EnrollmentException.NotEnrolled

        val challengeResponse = apiClient.challenge()
        val clientDataHash = clientDataHash(challengeResponse.challenge)
        val assertion = provider.assert(keyId, clientDataHash)
        val tokenResponse = apiClient.assert(
            keyId = keyId,
            assertionB64 = Base64.getEncoder().encodeToString(assertion),
            challengeB64 = challengeResponse.challenge
        )
        val expiresAt = parseExpiry(tokenResponse.expiresAt)

        tokenStore.saveToken(tokenResponse.deviceToken, expiresAt)
    }

    suspend fun validToken(): String = mutex.withLock {
        val stored = tokenStore.loadToken()
        if (stored != null && stored.second.epochSecond > Instant.now().epochSecond + tokenExpirySkewSeconds) {
            return@withLock stored.first
        }

        try {
            if (stored == null) {
                enroll()
            } else {
                refresh()
            }
        } catch (e: AttestationProviderException.KeyUnusable) {
            // Mirrors PR #24's iOS fix: the stored key_id names a key this
            // install can no longer use. Retrying with the same key_id loops
            // forever, so discard the whole stored identity and enroll once
            // with a genuinely new key; a second failure propagates.
            tokenStore.clear()
            enroll()
        }

        tokenStore.loadToken()?.first ?: throw EnrollmentException.NotEnrolled
    }

    private fun decodeChallenge(challengeB64: String): ByteArray =
        Base64.getDecoder().decode(challengeB64)

    private fun clientDataHash(challengeB64: String): ByteArray =
        MessageDigest.getInstance("SHA-256").digest(decodeChallenge(challengeB64))

    private fun parseExpiry(raw: String): Instant {
        return try {
            Instant.from(DateTimeFormatter.ISO_INSTANT.parse(raw))
        } catch (e: Exception) {
            throw EnrollmentException.MalformedExpiry(raw)
        }
    }
}
