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
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test
import java.time.Instant

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
}
