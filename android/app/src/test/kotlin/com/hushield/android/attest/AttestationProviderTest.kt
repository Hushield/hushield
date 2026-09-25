package com.hushield.android.attest

import android.util.Base64
import io.mockk.coEvery
import io.mockk.every
import io.mockk.mockk
import kotlinx.coroutines.runBlocking
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import java.security.KeyPair
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.MessageDigest
import java.security.PublicKey
import java.security.cert.Certificate

/**
 * `KeystoreKeyManager()`'s zero-arg default constructor calls
 * `KeyStore.getInstance("AndroidKeyStore")`, which throws under Robolectric
 * -- there is no "AndroidKeyStore" Provider registered on a plain JVM (see
 * KeystoreKeyManagerTest / task-2-report.md for the full investigation).
 *
 * Rather than `@Ignore`ing this whole file for that reason, these tests
 * build a real [KeystoreKeyManager] via constructor injection -- a fake
 * [KeyPairGeneration] plus a mocked [KeyStore] standing in for the hardware
 * -- mirroring KeystoreKeyManagerFallbackTest's approach from Task 2. That
 * means real EC key generation and real SHA256withECDSA signing, with only
 * the AndroidKeyStore-backed storage faked, so what's under test here --
 * RealAttestationProvider's own logic (keyId derivation, nonce/envelope
 * construction, the generate -> lookup round trip) -- is exercised for
 * real. See task-3-report.md for why this is a deliberate deviation from
 * the brief's literal `KeystoreKeyManager()` test snippet.
 */
@RunWith(RobolectricTestRunner::class)
class AttestationProviderTest {

    /** Alias-keyed fake Keystore backing: real EC keys, no AndroidKeyStore provider. */
    private class FakeKeystoreBacking {
        private val keys = mutableMapOf<String, KeyPair>()

        val generation = object : KeyPairGeneration {
            override fun generateKeyPair(alias: String, strongBoxBacked: Boolean): PublicKey {
                val pair = KeyPairGenerator.getInstance("EC").apply { initialize(256) }.generateKeyPair()
                keys[alias] = pair
                return pair.public
            }
        }

        val keyStore: KeyStore = mockk(relaxed = true)

        init {
            every { keyStore.getCertificate(any()) } answers {
                val alias = firstArg<String>()
                keys[alias]?.let { pair -> mockk<Certificate> { every { publicKey } returns pair.public } }
            }
            every { keyStore.getKey(any(), null) } answers { keys[firstArg<String>()]?.private }
            every { keyStore.containsAlias(any()) } answers { keys.containsKey(firstArg<String>()) }
            every { keyStore.deleteEntry(any()) } answers { keys.remove(firstArg<String>()); Unit }
        }
    }

    private fun newManager(): KeystoreKeyManager {
        val backing = FakeKeystoreBacking()
        return KeystoreKeyManager(backing.keyStore, backing.generation)
    }

    @Test
    fun `generateKeyId derives the key ID from sha256 of the PKIX-DER public key`() = runBlocking {
        val keyManager = newManager()
        val decoder = mockk<IntegrityTokenSource>()
        val provider = RealAttestationProvider(keyManager, decoder)

        val keyId = provider.generateKeyId()
        // The provider must have stored the key under an alias it can look
        // up by the SAME keyId it returned -- verify the round trip instead
        // of just checking the string format.
        val pubKey = keyManager.publicKey(RealAttestationProvider.DEVICE_KEY_ALIAS)
        assertNotNull(pubKey)

        val expectedKeyId = Base64.encodeToString(
            MessageDigest.getInstance("SHA-256").digest(pubKey!!.encoded),
            Base64.NO_WRAP
        )
        assertEquals(expectedKeyId, keyId)
    }

    @Test
    fun `generateKeyId called again rotates the device key, invalidating the previous keyId`() {
        val keyManager = newManager()
        val decoder = mockk<IntegrityTokenSource>()
        val provider = RealAttestationProvider(keyManager, decoder)

        val firstKeyId = runBlocking { provider.generateKeyId() }
        val secondKeyId = runBlocking { provider.generateKeyId() }
        assertNotEquals(firstKeyId, secondKeyId)

        assertThrows(AttestationProviderException.KeyUnusable::class.java) {
            runBlocking { provider.assert(firstKeyId, "client-data-hash".toByteArray()) }
        }
    }

    @Test
    fun `attest builds a nonce from sha256(challenge concat pubkey) and returns the integrity token wrapped in the envelope`() = runBlocking {
        val keyManager = newManager()
        val decoder = mockk<IntegrityTokenSource>()
        val provider = RealAttestationProvider(keyManager, decoder)

        val keyId = provider.generateKeyId()
        val pubKey = keyManager.publicKey(RealAttestationProvider.DEVICE_KEY_ALIAS)!!
        val challenge = "server-challenge".toByteArray()

        val expectedNonce = MessageDigest.getInstance("SHA-256").digest(challenge + pubKey.encoded)
        coEvery { decoder.requestToken(expectedNonce) } returns "fake-integrity-token"

        val envelope = provider.attest(keyId, challenge)
        val envelopeText = String(envelope)
        assertTrue(envelopeText.contains("fake-integrity-token"))

        val json = JSONObject(envelopeText)
        assertEquals("fake-integrity-token", json.getString("integrity_token"))
        assertEquals(Base64.encodeToString(pubKey.encoded, Base64.NO_WRAP), json.getString("public_key_der"))
    }

    @Test
    fun `assert produces the same androidAssertion wire format signed by the Keystore key`() = runBlocking {
        val keyManager = newManager()
        val decoder = mockk<IntegrityTokenSource>()
        val provider = RealAttestationProvider(keyManager, decoder)

        val keyId = provider.generateKeyId()
        val clientDataHash = "client-data-hash".toByteArray()

        val assertion = provider.assert(keyId, clientDataHash)
        // Full wire-format correctness (counter, JSON shape) is Task 4's
        // job -- this test only proves `assert` delegates signing to the
        // Keystore key named by keyId, which Task 4's tests build on.
        assertTrue(assertion.isNotEmpty())

        val verifier = java.security.Signature.getInstance("SHA256withECDSA")
        verifier.initVerify(keyManager.publicKey(RealAttestationProvider.DEVICE_KEY_ALIAS))
        verifier.update(clientDataHash)
        assertTrue(verifier.verify(assertion))
    }
}
