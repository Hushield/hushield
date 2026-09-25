package com.hushield.android.net

import org.json.JSONObject

class APIClientException(message: String) : Exception(message)

data class ChallengeResponse(val challenge: String, val expiresAt: String)
data class DeviceTokenResponse(val deviceToken: String, val expiresAt: String)

/**
 * Talks to the SpamFilter /api/v1 backend's attestation endpoints, mirroring
 * ios/SpamFilterKit/APIClient.swift's challenge/verify/assert methods. Only
 * these three are implemented here -- report/blocklist/lookup are added by
 * whichever later plan (call blocking, SMS filtering) needs them first.
 */
class APIClient(private val transport: HttpTransport, private val baseUrl: String) {

    suspend fun challenge(): ChallengeResponse {
        val response = transport.send(HttpRequest("POST", "/api/v1/attest/challenge"))
        val data = decodeEnvelope(response)
        return ChallengeResponse(data.getString("challenge"), data.getString("expires_at"))
    }

    suspend fun verify(keyId: String, attestationB64: String, challengeB64: String, platform: String = "android"): DeviceTokenResponse {
        val body = JSONObject()
            .put("key_id", keyId)
            .put("attestation", attestationB64)
            .put("challenge", challengeB64)
            .put("platform", platform)
        val response = transport.send(HttpRequest("POST", "/api/v1/attest/verify", body.toString().toByteArray()))
        return decodeDeviceToken(response)
    }

    suspend fun assert(keyId: String, assertionB64: String, challengeB64: String): DeviceTokenResponse {
        // No "platform" field -- the server uses the device's stored
        // platform for assert, never a client-supplied value. See the
        // backend plan's Task 6 security property.
        val body = JSONObject()
            .put("key_id", keyId)
            .put("assertion", assertionB64)
            .put("challenge", challengeB64)
        val response = transport.send(HttpRequest("POST", "/api/v1/attest/assert", body.toString().toByteArray()))
        return decodeDeviceToken(response)
    }

    private fun decodeDeviceToken(response: HttpResponse): DeviceTokenResponse {
        val data = decodeEnvelope(response)
        return DeviceTokenResponse(data.getString("device_token"), data.getString("expires_at"))
    }

    private fun decodeEnvelope(response: HttpResponse): JSONObject {
        if (response.statusCode !in 200..299) {
            throw APIClientException("request failed with status ${response.statusCode}")
        }
        return JSONObject(String(response.body)).getJSONObject("data")
    }
}
