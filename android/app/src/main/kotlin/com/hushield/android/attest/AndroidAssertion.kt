package com.hushield.android.attest

import android.content.Context
import android.content.SharedPreferences
import android.util.Base64
import org.json.JSONObject
import java.nio.ByteBuffer
import java.security.MessageDigest

interface CounterStore {
    /** Returns the next counter value for keyId, strictly greater than any value previously returned for it. */
    fun next(keyId: String): Int
}

/**
 * SharedPreferences-backed CounterStore. Not a secret -- the counter is
 * replay-protection metadata, not identity -- so plain (non-encrypted)
 * SharedPreferences is fine, unlike TokenStore (Task 5) which holds the
 * device token.
 */
class SharedPreferencesCounterStore(context: Context) : CounterStore {
    private val prefs: SharedPreferences =
        context.getSharedPreferences("hushield_assertion_counters", Context.MODE_PRIVATE)

    override fun next(keyId: String): Int {
        val current = prefs.getInt(keyId, 0)
        val nextValue = current + 1
        prefs.edit().putInt(keyId, nextValue).apply()
        return nextValue
    }
}

/**
 * Builds the per-request signed assertion the Go backend's
 * PlayIntegrityVerifier.VerifyAssertion (internal/attest/android.go) expects:
 * {"counter": <uint32>, "signature": "<base64 ECDSA-P256 signature>"}, where
 * the signature covers SHA256(clientDataHash || big-endian-uint32(counter)).
 * This wire format is a cross-repo contract -- see internal/attest/android.go
 * on the server side; changing either side without the other breaks every
 * Android device's assert() call silently (wrong signature, not a decode
 * error), so treat any change here as needing a matching server-side change
 * in the same PR.
 *
 * `alias` and `keyId` are deliberately separate parameters: `alias` is the
 * physical Keystore entry to sign with (a fixed constant in production --
 * see RealAttestationProvider.DEVICE_KEY_ALIAS), while `keyId` is the
 * logical, rotating device identity used only to key CounterStore. A key
 * rotation gets a fresh counter sequence because the Go backend's signCount
 * column is itself keyed by device_id/key_id.
 */
class AndroidAssertion(
    private val keyManager: KeystoreKeyManager,
    private val counterStore: CounterStore
) {
    fun sign(alias: String, keyId: String, clientDataHash: ByteArray): ByteArray {
        val counter = counterStore.next(keyId)
        val counterBytes = ByteBuffer.allocate(4).putInt(counter).array()
        val message = MessageDigest.getInstance("SHA-256").digest(clientDataHash + counterBytes)
        val signature = keyManager.sign(alias, message)

        val json = JSONObject()
            .put("counter", counter)
            .put("signature", Base64.encodeToString(signature, Base64.NO_WRAP))
        return json.toString().toByteArray()
    }
}
