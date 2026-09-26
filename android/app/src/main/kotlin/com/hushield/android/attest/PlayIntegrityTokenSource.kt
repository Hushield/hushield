package com.hushield.android.attest

import android.util.Base64
import com.google.android.play.core.integrity.IntegrityManager
import com.google.android.play.core.integrity.IntegrityTokenRequest
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

/**
 * The real Play Integrity SDK call.
 *
 * Verified against the actual `com.google.android.play:integrity:1.4.0` jar
 * pinned in Task 1's build.gradle.kts (inspected via `javap` on the AAR's
 * extracted classes in the Gradle cache, not from training data):
 *
 *   IntegrityManager.requestIntegrityToken(IntegrityTokenRequest):
 *       Task<IntegrityTokenResponse>
 *   IntegrityTokenRequest.builder(): IntegrityTokenRequest.Builder
 *   IntegrityTokenRequest.Builder.setNonce(String): Builder
 *   IntegrityTokenRequest.Builder.setCloudProjectNumber(long): Builder
 *   IntegrityTokenRequest.Builder.build(): IntegrityTokenRequest
 *   IntegrityTokenResponse.token(): String
 *
 * This matches the shape below exactly. If a future version of the library
 * changes this surface, fix this file, not the version pin, unless the
 * pinned version is itself the problem.
 */
class PlayIntegrityTokenSource(
    private val integrityManager: IntegrityManager,
    private val cloudProjectNumber: Long
) : IntegrityTokenSource {

    override suspend fun requestToken(nonce: ByteArray): String {
        val nonceB64 = Base64.encodeToString(nonce, Base64.NO_WRAP or Base64.URL_SAFE)
        val request = IntegrityTokenRequest.builder()
            .setNonce(nonceB64)
            .setCloudProjectNumber(cloudProjectNumber)
            .build()

        return suspendCancellableCoroutine { continuation ->
            integrityManager.requestIntegrityToken(request)
                .addOnSuccessListener { response -> continuation.resume(response.token()) }
                .addOnFailureListener { error -> continuation.resumeWithException(error) }
        }
    }
}
