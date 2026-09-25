package com.hushield.android.attest

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.PublicKey
import java.security.Signature

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
    private val keyStore: KeyStore = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
) {

    fun generateKey(alias: String): PublicKey {
        deleteKey(alias)

        val purposes = KeyProperties.PURPOSE_SIGN or KeyProperties.PURPOSE_VERIFY
        val baseSpec = KeyGenParameterSpec.Builder(alias, purposes)
            .setDigests(KeyProperties.DIGEST_SHA256)
            .setAlgorithmParameterSpec(java.security.spec.ECGenParameterSpec("secp256r1"))

        val generator = KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_EC, "AndroidKeyStore")
        try {
            generator.initialize(baseSpec.setIsStrongBoxBacked(true).build())
            return generator.generateKeyPair().public
        } catch (e: Exception) {
            // StrongBox unavailable on this device/API level -- fall back to
            // a TEE-backed key rather than failing enrollment outright.
            generator.initialize(baseSpec.setIsStrongBoxBacked(false).build())
            return generator.generateKeyPair().public
        }
    }

    fun publicKey(alias: String): PublicKey? {
        return keyStore.getCertificate(alias)?.publicKey
    }

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
