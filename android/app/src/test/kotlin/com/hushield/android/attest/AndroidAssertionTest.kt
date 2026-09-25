package com.hushield.android.attest

import io.mockk.every
import io.mockk.mockk
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import java.security.KeyPair
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.MessageDigest
import java.security.PublicKey
import java.security.Signature
import java.security.cert.Certificate
import java.nio.ByteBuffer

/**
 * `AndroidAssertion.sign` takes both a Keystore `alias` (what
 * `KeystoreKeyManager.sign` actually signs with) and a logical `keyId`
 * (used only to key `CounterStore`, never passed to the Keystore). In
 * production these come from `RealAttestationProvider`, where `alias` is
 * always the fixed `DEVICE_KEY_ALIAS` and `keyId` is the rotating hash of
 * the current key -- see AttestationProvider.kt. These tests exercise
 * `AndroidAssertion` in isolation, so `alias` and `keyId` are deliberately
 * different strings in most cases to prove the two are never confused.
 *
 * `KeystoreKeyManager()`'s zero-arg constructor calls
 * `KeyStore.getInstance("AndroidKeyStore")`, which throws under Robolectric
 * (no AndroidKeyStore Provider registered on a plain JVM -- see
 * KeystoreKeyManagerTest / task-2-report.md). Like AttestationProviderTest,
 * these tests build a real KeystoreKeyManager via constructor injection: a
 * fake KeyPairGeneration plus a mocked KeyStore standing in for the
 * hardware, so real EC key generation and real SHA256withECDSA signing are
 * still exercised.
 */
@RunWith(RobolectricTestRunner::class)
class AndroidAssertionTest {

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
    fun `sign produces JSON with the exact counter and signature fields the Go backend expects`() {
        val keyManager = newManager()
        val alias = "assertion-test-key"
        keyManager.generateKey(alias)
        val keyId = "logical-key-id-1"
        val counterStore = mockk<CounterStore>()
        every { counterStore.next(keyId) } returns 1

        val assertion = AndroidAssertion(keyManager, counterStore)
        val clientDataHash = "hash-bytes".toByteArray()
        val result = assertion.sign(alias, keyId, clientDataHash)

        val json = JSONObject(String(result))
        assertEquals(1, json.getInt("counter"))
        assertTrue(json.has("signature"))
    }

    @Test
    fun `the signature verifies against sha256(clientDataHash concat big-endian counter)`() {
        val keyManager = newManager()
        val alias = "assertion-test-key-2"
        keyManager.generateKey(alias)
        val keyId = "logical-key-id-2"
        val counterStore = mockk<CounterStore>()
        every { counterStore.next(keyId) } returns 7

        val assertion = AndroidAssertion(keyManager, counterStore)
        val clientDataHash = "hash-bytes-2".toByteArray()
        val result = assertion.sign(alias, keyId, clientDataHash)
        val json = JSONObject(String(result))

        val counterBytes = ByteBuffer.allocate(4).putInt(json.getInt("counter")).array()
        val expectedMessage = MessageDigest.getInstance("SHA-256").digest(clientDataHash + counterBytes)
        val signatureBytes = android.util.Base64.decode(json.getString("signature"), android.util.Base64.NO_WRAP)

        val verifier = Signature.getInstance("SHA256withECDSA")
        verifier.initVerify(keyManager.publicKey(alias))
        verifier.update(expectedMessage)
        assertTrue(verifier.verify(signatureBytes))
    }

    @Test
    fun `counterStore next is called exactly once per sign call, not per verification attempt`() {
        val keyManager = newManager()
        val alias = "assertion-test-key-3"
        keyManager.generateKey(alias)
        val keyId = "logical-key-id-3"
        val counterStore = mockk<CounterStore>()
        every { counterStore.next(keyId) } returns 1

        val assertion = AndroidAssertion(keyManager, counterStore)
        assertion.sign(alias, keyId, "x".toByteArray())

        io.mockk.verify(exactly = 1) { counterStore.next(keyId) }
    }

    @Test
    fun `sign uses the alias for Keystore signing, never the logical keyId`() {
        val keyManager = newManager()
        val alias = "assertion-test-key-4"
        keyManager.generateKey(alias)
        // A keyId that does NOT name any Keystore entry -- if sign()
        // mistakenly used keyId as the alias, KeystoreKeyManager.sign would
        // throw looking it up (getKey returns null -> ClassCastException).
        val keyId = "not-a-keystore-alias"
        val counterStore = mockk<CounterStore>()
        every { counterStore.next(keyId) } returns 1

        val assertion = AndroidAssertion(keyManager, counterStore)
        val result = assertion.sign(alias, keyId, "y".toByteArray())

        assertTrue(JSONObject(String(result)).has("signature"))
    }
}
