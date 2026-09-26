package com.hushield.android.enroll

import com.hushield.android.attest.AttestationProvider
import com.hushield.android.attest.AttestationProviderException
import com.hushield.android.net.APIClient
import com.hushield.android.net.ChallengeResponse
import com.hushield.android.net.DeviceTokenResponse
import com.hushield.android.store.TokenStore
import io.mockk.coEvery
import io.mockk.every
import io.mockk.mockk
import io.mockk.slot
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Test
import java.security.MessageDigest
import java.time.Instant
import java.util.Base64

class EnrollmentServiceTest {

    @Test
    fun `enroll generates a key when none is stored, attests it, and persists both`() = runBlocking {
        val apiClient = mockk<APIClient>()
        val provider = mockk<AttestationProvider>()
        val tokenStore = mockk<TokenStore>(relaxUnitFun = true)

        every { tokenStore.loadKeyId() } returns null
        coEvery { provider.generateKeyId() } returns "new-key-id"
        coEvery { apiClient.challenge() } returns ChallengeResponse("chal123", "2099-01-01T00:00:00Z")
        coEvery { provider.attest("new-key-id", any()) } returns "attestation-bytes".toByteArray()
        coEvery { apiClient.verify("new-key-id", any(), "chal123") } returns
            DeviceTokenResponse("tok", "2026-01-01T00:00:00Z")

        val service = EnrollmentService(apiClient, provider, tokenStore)
        service.enroll()

        io.mockk.verify { tokenStore.saveKeyId("new-key-id") }
        io.mockk.verify { tokenStore.saveToken("tok", any()) }
    }

    /**
     * Regression guard for a real bug the final whole-branch review caught
     * and fixed: enroll() was passing SHA256(challenge) into attest(), which
     * then hashed it again with the public key, producing one extra hash
     * layer versus what the Go backend's Play Integrity nonce check expects
     * (SHA256(rawChallenge || pubDER)). Every prior test stubbed attest()
     * with `any()`, so this regressed silently with a fully green suite --
     * this test exists specifically to capture the actual bytes and check
     * them, not just that attest() was called.
     */
    @Test
    fun `enroll passes the raw decoded challenge (not its hash) to attest`() = runBlocking {
        val apiClient = mockk<APIClient>()
        val provider = mockk<AttestationProvider>()
        val tokenStore = mockk<TokenStore>(relaxUnitFun = true)

        val challengeB64 = "c2VydmVyLWNoYWxsZW5nZS1ieXRlcw==" // "server-challenge-bytes"
        val rawChallenge = Base64.getDecoder().decode(challengeB64)
        val hashedChallenge = MessageDigest.getInstance("SHA-256").digest(rawChallenge)

        every { tokenStore.loadKeyId() } returns null
        coEvery { provider.generateKeyId() } returns "new-key-id"
        coEvery { apiClient.challenge() } returns ChallengeResponse(challengeB64, "2099-01-01T00:00:00Z")
        val capturedInput = slot<ByteArray>()
        coEvery { provider.attest("new-key-id", capture(capturedInput)) } returns "attestation-bytes".toByteArray()
        coEvery { apiClient.verify("new-key-id", any(), challengeB64) } returns
            DeviceTokenResponse("tok", "2026-01-01T00:00:00Z")

        val service = EnrollmentService(apiClient, provider, tokenStore)
        service.enroll()

        assertArrayEquals(rawChallenge, capturedInput.captured)
        assertFalse(
            "attest() received SHA256(challenge) instead of the raw challenge -- the double-hash bug is back",
            capturedInput.captured.contentEquals(hashedChallenge)
        )
    }

    /**
     * Companion to the test above: refresh()'s use of SHA256(challenge) for
     * assert() is CORRECT (the Go backend's handleAssert independently
     * computes and expects that same pre-hashed value) and must not be
     * "fixed" to raw challenge bytes -- that would be the opposite bug.
     */
    @Test
    fun `refresh passes SHA256(challenge) to assert, not the raw challenge`() = runBlocking {
        val apiClient = mockk<APIClient>()
        val provider = mockk<AttestationProvider>()
        val tokenStore = mockk<TokenStore>(relaxUnitFun = true)

        val challengeB64 = "c2VydmVyLWNoYWxsZW5nZS1ieXRlcw=="
        val rawChallenge = Base64.getDecoder().decode(challengeB64)
        val hashedChallenge = MessageDigest.getInstance("SHA-256").digest(rawChallenge)

        every { tokenStore.loadKeyId() } returns "existing-key-id"
        coEvery { apiClient.challenge() } returns ChallengeResponse(challengeB64, "2099-01-01T00:00:00Z")
        val capturedInput = slot<ByteArray>()
        coEvery { provider.assert("existing-key-id", capture(capturedInput)) } returns "assertion-bytes".toByteArray()
        coEvery { apiClient.assert("existing-key-id", any(), challengeB64) } returns
            DeviceTokenResponse("tok", "2026-01-01T00:00:00Z")

        val service = EnrollmentService(apiClient, provider, tokenStore)
        service.refresh()

        assertArrayEquals(hashedChallenge, capturedInput.captured)
        assertFalse(
            "assert() received the raw challenge instead of SHA256(challenge) -- refresh() must keep pre-hashing",
            capturedInput.captured.contentEquals(rawChallenge)
        )
    }

    @Test
    fun `validToken returns the cached token when it is not near expiry`() = runBlocking {
        val apiClient = mockk<APIClient>()
        val provider = mockk<AttestationProvider>()
        val tokenStore = mockk<TokenStore>()

        every { tokenStore.loadToken() } returns ("cached-token" to Instant.now().plusSeconds(3600))

        val service = EnrollmentService(apiClient, provider, tokenStore)
        val result = service.validToken()

        assertEquals("cached-token", result)
    }

    @Test
    fun `validToken clears the stored identity and re-enrolls once on KeyUnusable, then propagates a second failure`() = runBlocking {
        val apiClient = mockk<APIClient>()
        val provider = mockk<AttestationProvider>()
        val tokenStore = mockk<TokenStore>(relaxUnitFun = true)

        every { tokenStore.loadToken() } returns ("stale-token" to Instant.now().minusSeconds(10)) andThen null
        every { tokenStore.loadKeyId() } returns "dead-key-id" andThen null
        coEvery { apiClient.challenge() } returns ChallengeResponse("chal", "2099-01-01T00:00:00Z")
        coEvery { provider.assert("dead-key-id", any()) } throws AttestationProviderException.KeyUnusable
        coEvery { provider.generateKeyId() } throws AttestationProviderException.KeyUnusable // second attempt also fails

        val service = EnrollmentService(apiClient, provider, tokenStore)

        assertThrows(AttestationProviderException.KeyUnusable::class.java) {
            runBlocking { service.validToken() }
        }
        io.mockk.verify { tokenStore.clear() }
    }

    /** In-memory TokenStore, real enough to let a second racing caller
     * observe what the first one actually saved -- unlike a mockk stub that
     * would keep returning null/empty forever regardless of what "saves"
     * happened, which would hide exactly the race this test exists to catch.
     */
    private class FakeTokenStore : TokenStore {
        private var token: Pair<String, Instant>? = null
        private var keyId: String? = null

        override fun saveToken(token: String, expiresAt: Instant) {
            this.token = token to expiresAt
        }

        override fun loadToken(): Pair<String, Instant>? = token
        override fun saveKeyId(keyId: String) {
            this.keyId = keyId
        }

        override fun loadKeyId(): String? = keyId
        override fun clear() {
            token = null
            keyId = null
        }
    }

    @Test
    fun `two concurrent validToken calls on a fresh service single-flight instead of interleaving`() = runBlocking {
        val apiClient = mockk<APIClient>()
        val provider = mockk<AttestationProvider>()
        val tokenStore = FakeTokenStore()

        // Track concurrent entries into generateKeyId()/attest() so an
        // interleaving would be caught even if the final stored state
        // happened to look fine by luck.
        val inFlight = java.util.concurrent.atomic.AtomicInteger(0)
        val maxConcurrent = java.util.concurrent.atomic.AtomicInteger(0)
        val generateKeyIdCalls = java.util.concurrent.atomic.AtomicInteger(0)
        val attestCalls = java.util.concurrent.atomic.AtomicInteger(0)

        suspend fun <T> trackConcurrency(block: suspend () -> T): T {
            val current = inFlight.incrementAndGet()
            maxConcurrent.updateAndGet { prev -> maxOf(prev, current) }
            try {
                kotlinx.coroutines.delay(20) // widen the race window
                return block()
            } finally {
                inFlight.decrementAndGet()
            }
        }

        coEvery { provider.generateKeyId() } coAnswers {
            trackConcurrency {
                generateKeyIdCalls.incrementAndGet()
                "shared-key-id"
            }
        }
        coEvery { apiClient.challenge() } returns ChallengeResponse("chal123", "2099-01-01T00:00:00Z")
        coEvery { provider.attest("shared-key-id", any()) } coAnswers {
            trackConcurrency {
                attestCalls.incrementAndGet()
                "attestation-bytes".toByteArray()
            }
        }
        coEvery { apiClient.verify("shared-key-id", any(), "chal123") } returns
            DeviceTokenResponse("tok", "2099-06-01T00:00:00Z")

        val service = EnrollmentService(apiClient, provider, tokenStore)

        val results = coroutineScope {
            val a = async(Dispatchers.Default) { service.validToken() }
            val b = async(Dispatchers.Default) { service.validToken() }
            listOf(a, b).awaitAll()
        }

        // Never more than one caller inside generateKeyId()/attest() at once.
        assertEquals(1, maxConcurrent.get())
        // Exactly one full enrollment happened: the mutex made the second
        // caller wait until the first had already saved a valid token, so
        // it took the cached-token fast path instead of enrolling again.
        assertEquals(1, generateKeyIdCalls.get())
        assertEquals(1, attestCalls.get())
        assertEquals(listOf("tok", "tok"), results)
    }
}
