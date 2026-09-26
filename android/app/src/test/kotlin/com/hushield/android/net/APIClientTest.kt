package com.hushield.android.net

import io.mockk.coEvery
import io.mockk.mockk
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class APIClientTest {

    @Test
    fun `challenge sends POST to attest challenge and decodes the response`() = runBlocking {
        val transport = mockk<HttpTransport>()
        coEvery { transport.send(match { it.method == "POST" && it.path == "/api/v1/attest/challenge" }) } returns
            HttpResponse(200, """{"data":{"challenge":"abc123","expires_at":"2026-01-01T00:00:00Z"}}""".toByteArray())

        val client = APIClient(transport, "http://localhost:8080")
        val result = client.challenge()
        assertEquals("abc123", result.challenge)
    }

    @Test
    fun `verify sends platform android in the request body`() = runBlocking {
        val transport = mockk<HttpTransport>()
        var capturedBody: String? = null
        coEvery { transport.send(match { it.method == "POST" && it.path == "/api/v1/attest/verify" }) } answers {
            capturedBody = String(firstArg<HttpRequest>().body!!)
            HttpResponse(200, """{"data":{"device_token":"tok","expires_at":"2026-01-01T00:00:00Z"}}""".toByteArray())
        }

        val client = APIClient(transport, "http://localhost:8080")
        client.verify(keyId = "kid", attestationB64 = "att", challengeB64 = "chal")

        assertTrue(capturedBody!!.contains("\"platform\":\"android\""))
    }

    @Test
    fun `assert sends no platform field at all`() = runBlocking {
        val transport = mockk<HttpTransport>()
        var capturedBody: String? = null
        coEvery { transport.send(match { it.method == "POST" && it.path == "/api/v1/attest/assert" }) } answers {
            capturedBody = String(firstArg<HttpRequest>().body!!)
            HttpResponse(200, """{"data":{"device_token":"tok","expires_at":"2026-01-01T00:00:00Z"}}""".toByteArray())
        }

        val client = APIClient(transport, "http://localhost:8080")
        client.assert(keyId = "kid", assertionB64 = "asrt", challengeB64 = "chal")

        assertTrue(!capturedBody!!.contains("platform"))
    }
}
