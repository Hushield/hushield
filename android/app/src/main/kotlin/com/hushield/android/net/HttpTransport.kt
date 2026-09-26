package com.hushield.android.net

import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.toRequestBody

data class HttpRequest(val method: String, val path: String, val body: ByteArray? = null, val headers: Map<String, String> = emptyMap())
data class HttpResponse(val statusCode: Int, val body: ByteArray)

/**
 * The OkHttp-backed testability seam, mirroring `HTTPTransport`'s role for
 * the iOS `APIClient` -- callers depend on this interface, never on OkHttp
 * directly, so tests can substitute a mock transport.
 */
interface HttpTransport {
    suspend fun send(request: HttpRequest): HttpResponse
}

class OkHttpTransport(private val client: okhttp3.OkHttpClient, private val baseUrl: String) : HttpTransport {
    override suspend fun send(request: HttpRequest): HttpResponse {
        val builder = okhttp3.Request.Builder().url(baseUrl + request.path)
        request.headers.forEach { (name, value) -> builder.addHeader(name, value) }
        val body = request.body?.toRequestBody("application/json".toMediaType())
        when (request.method) {
            "GET" -> builder.get()
            "POST" -> builder.post(body ?: okhttp3.internal.EMPTY_REQUEST)
            else -> error("unsupported method ${request.method}")
        }
        val response = client.newCall(builder.build()).execute()
        val responseBody = response.body?.bytes() ?: ByteArray(0)
        return HttpResponse(response.code, responseBody)
    }
}
