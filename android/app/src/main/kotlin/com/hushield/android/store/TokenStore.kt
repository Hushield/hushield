package com.hushield.android.store

import android.content.Context
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import java.time.Instant

/**
 * Persists this device's identity: the key_id (reused across enroll/refresh)
 * and the current device token + its expiry. Mirrors
 * ios/SpamFilterKit/TokenStore.swift's Keychain-backed TokenStore --
 * EncryptedSharedPreferences (backed by a Keystore-wrapped master key) is
 * the Android analog of the Keychain for this purpose.
 */
interface TokenStore {
    fun saveToken(token: String, expiresAt: Instant)
    fun loadToken(): Pair<String, Instant>?
    fun saveKeyId(keyId: String)
    fun loadKeyId(): String?
    fun clear()
}

class EncryptedPrefsTokenStore(context: Context) : TokenStore {
    private val masterKey = MasterKey.Builder(context)
        .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
        .build()

    private val prefs = EncryptedSharedPreferences.create(
        context,
        "hushield_identity",
        masterKey,
        EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
        EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM
    )

    override fun saveToken(token: String, expiresAt: Instant) {
        prefs.edit()
            .putString(KEY_TOKEN, token)
            .putLong(KEY_EXPIRES_AT, expiresAt.epochSecond)
            .apply()
    }

    override fun loadToken(): Pair<String, Instant>? {
        val token = prefs.getString(KEY_TOKEN, null) ?: return null
        val epochSecond = prefs.getLong(KEY_EXPIRES_AT, -1L)
        if (epochSecond < 0) return null
        return token to Instant.ofEpochSecond(epochSecond)
    }

    override fun saveKeyId(keyId: String) {
        prefs.edit().putString(KEY_KEY_ID, keyId).apply()
    }

    override fun loadKeyId(): String? = prefs.getString(KEY_KEY_ID, null)

    override fun clear() {
        prefs.edit().clear().apply()
    }

    companion object {
        private const val KEY_TOKEN = "device_token"
        private const val KEY_EXPIRES_AT = "device_token_expires_at"
        private const val KEY_KEY_ID = "attest_key_id"
    }
}
