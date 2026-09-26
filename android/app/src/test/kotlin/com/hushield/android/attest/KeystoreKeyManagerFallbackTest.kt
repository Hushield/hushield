package com.hushield.android.attest

import android.security.keystore.StrongBoxUnavailableException
import io.mockk.mockk
import io.mockk.verify
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.PublicKey

/**
 * Unit tests for the StrongBox-then-TEE fallback LOGIC in
 * [KeystoreKeyManager.generateKey], exercised through a fake
 * [KeyPairGeneration] rather than a real AndroidKeyStore Provider.
 *
 * Unlike [KeystoreKeyManagerTest], these do not need Robolectric or a
 * device: they never call `KeyStore.getInstance("AndroidKeyStore")`. The
 * `keyStore` constructor param is given a plain, provider-agnostic JVM
 * keystore (`KeyStore.getDefaultType()`, e.g. JKS/PKCS12), which is legal to
 * instantiate on any JVM and is never touched by the code paths under test
 * here.
 */
class KeystoreKeyManagerFallbackTest {

    private fun plainKeyStore(): KeyStore =
        KeyStore.getInstance(KeyStore.getDefaultType()).apply { load(null, null) }

    /** A stand-in "generated" key -- content doesn't matter, only identity. */
    private fun fakePublicKey(): PublicKey =
        KeyPairGenerator.getInstance("EC").apply { initialize(256) }.generateKeyPair().public

    @Test
    fun `falls back to TEE when StrongBox is unavailable`() {
        val teeKey = fakePublicKey()
        val generation = object : KeyPairGeneration {
            override fun generateKeyPair(alias: String, strongBoxBacked: Boolean): PublicKey {
                if (strongBoxBacked) throw StrongBoxUnavailableException("no StrongBox on this device")
                return teeKey
            }
        }
        val manager = KeystoreKeyManager(plainKeyStore(), generation)

        val result = manager.generateKey("alias")

        assertEquals(teeKey, result)
    }

    @Test
    fun `a non-StrongBoxUnavailableException from the StrongBox attempt propagates instead of falling back`() {
        val generation = object : KeyPairGeneration {
            override fun generateKeyPair(alias: String, strongBoxBacked: Boolean): PublicKey {
                if (strongBoxBacked) throw IllegalStateException("bad params, or a real provider bug")
                throw AssertionError("should never reach the TEE fallback for a non-StrongBox failure")
            }
        }
        val manager = KeystoreKeyManager(plainKeyStore(), generation)

        assertThrows(IllegalStateException::class.java) {
            manager.generateKey("alias")
        }
    }

    @Test
    fun `a pre-existing key is left intact when both StrongBox and TEE attempts fail`() {
        val keyStore = mockk<KeyStore>(relaxed = true)
        val generation = object : KeyPairGeneration {
            override fun generateKeyPair(alias: String, strongBoxBacked: Boolean): PublicKey {
                throw if (strongBoxBacked) {
                    StrongBoxUnavailableException("no StrongBox")
                } else {
                    IllegalStateException("TEE attempt also failed")
                }
            }
        }
        val manager = KeystoreKeyManager(keyStore, generation)

        assertThrows(IllegalStateException::class.java) {
            manager.generateKey("alias")
        }

        // generateKey never deletes the old key at `alias` -- a new key
        // (which would replace it) only happens on success. If generation
        // fails, whatever was already at `alias` (real KeyStore semantics)
        // is untouched.
        verify(exactly = 0) { keyStore.deleteEntry(any()) }
    }
}
