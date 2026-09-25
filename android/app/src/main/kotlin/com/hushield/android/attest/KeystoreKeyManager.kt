package com.hushield.android.attest

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.security.keystore.StrongBoxUnavailableException
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.PublicKey
import java.security.Signature
import java.security.spec.ECGenParameterSpec

/**
 * Seam over the real StrongBox/TEE key generation call, so the
 * StrongBox-then-TEE fallback logic in [KeystoreKeyManager.generateKey] can
 * be unit-tested with a fake instead of requiring a real AndroidKeyStore
 * Provider (unavailable under Robolectric; see KeystoreKeyManagerTest).
 */
interface KeyPairGeneration {
    fun generateKeyPair(alias: String, strongBoxBacked: Boolean): PublicKey
}

private class RealKeyPairGeneration : KeyPairGeneration {
    override fun generateKeyPair(alias: String, strongBoxBacked: Boolean): PublicKey {
        val purposes = KeyProperties.PURPOSE_SIGN or KeyProperties.PURPOSE_VERIFY
        val spec = KeyGenParameterSpec.Builder(alias, purposes)
            .setDigests(KeyProperties.DIGEST_SHA256)
            .setAlgorithmParameterSpec(ECGenParameterSpec("secp256r1"))
            .setIsStrongBoxBacked(strongBoxBacked)
            .build()

        val generator = KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_EC, "AndroidKeyStore")
        generator.initialize(spec)
        return generator.generateKeyPair().public
    }
}

/**
 * Manages this app's Android Keystore-backed EC key pairs -- the Keystore
 * analog of iOS's Secure Enclave key that `DeviceAttestationProvider` wraps.
 *
 * Generation requests StrongBox (a discrete secure element, API 28+) and
 * falls back to the Trusted Execution Environment otherwise -- StrongBox
 * isn't universal even on newer devices, and TEE-backed keys are still
 * hardware-isolated from the app process, just not from a separate chip.
 */
class KeystoreKeyManager(
    private val keyStore: KeyStore = KeyStore.getInstance("AndroidKeyStore").apply { load(null) },
    private val generation: KeyPairGeneration = RealKeyPairGeneration()
) {

    fun generateKey(alias: String): PublicKey {
        // Generating a new key pair under an alias that already has an entry
        // replaces that entry -- so the old key is only ever removed once a
        // new one exists at the same alias. If both the StrongBox and TEE
        // attempts below fail, this method throws and the previous key (if
        // any) at `alias` is untouched: no unconditional delete-before-generate.
        return try {
            generation.generateKeyPair(alias, strongBoxBacked = true)
        } catch (e: StrongBoxUnavailableException) {
            // StrongBox unavailable on this device/API level -- fall back to
            // a TEE-backed key rather than failing enrollment outright.
            generation.generateKeyPair(alias, strongBoxBacked = false)
        }
    }

    fun publicKey(alias: String): PublicKey? {
        return keyStore.getCertificate(alias)?.publicKey
    }

    // TODO(android): once a future task adds biometric-gated
    // re-authentication to this Keystore key (setUserAuthenticationRequired),
    // sign()/publicKey() calls here need to catch the OS-level
    // android.security.keystore.KeyPermanentlyInvalidatedException and
    // translate it to AttestationProviderException.KeyUnusable too.
    // KeyUnusable currently only covers the self-caused case (a caller
    // passing a stale keyId after RealAttestationProvider rotated the key) --
    // not this OS-level invalidation case, which is unreachable today since
    // nothing sets setUserAuthenticationRequired anywhere in this codebase.
    fun sign(alias: String, data: ByteArray): ByteArray {
        val privateKey = keyStore.getKey(alias, null) as java.security.PrivateKey
        val signature = Signature.getInstance("SHA256withECDSA")
        signature.initSign(privateKey)
        signature.update(data)
        return signature.sign()
    }

    fun deleteKey(alias: String) {
        if (keyStore.containsAlias(alias)) {
            keyStore.deleteEntry(alias)
        }
    }
}
