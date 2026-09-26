# Android Client Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the Android Gradle project and give it a working device identity: a Keystore-backed key, Play Integrity enrollment, and a device token obtained from the server's `/api/v1/attest/*` endpoints -- the foundation call blocking, SMS filtering, and the UI all build on.

**Architecture:** Kotlin, single `app` module, mirroring the iOS app's layering one-to-one: `AttestationProvider` (Keystore key lifecycle + Play Integrity token) -> `EnrollmentService` (composes provider + API client + token store into enroll/refresh/validToken) -> `TokenStore` (EncryptedSharedPreferences, the Android analog of Keychain) -> `APIClient` (OkHttp, hitting the same `/api/v1` endpoints the iOS app and this plan's backend PR (#27) already serve). No UI, no call blocking, no SMS filtering in this plan -- those are separate follow-up plans per the spec's own subsystem breakdown, and each needs a working device identity to build on top of.

**Tech Stack:** Kotlin, Gradle (Kotlin DSL), JDK 17 via Gradle toolchain, Android Keystore, Play Integrity API (`com.google.android.play:integrity`), OkHttp, EncryptedSharedPreferences (`androidx.security:security-crypto`), JUnit + Robolectric/MockK for unit tests.

**Spec:** `docs/superpowers/specs/2026-09-24-android-port-design.md`

## Global Constraints

- Zero PII, anywhere (`CONTRIBUTING.md`). The device identity is a Keystore key + its derived key_id, exactly as the spec requires -- no account, no phone number, no analytics identifier.
- `applicationId` is `com.hushield.android` (confirmed with the maintainer).
- `minSdk = 26` (Android 8.0) -- StrongBox key attestation needs API 28+ but this plan's Keystore code must run and fall back gracefully on 26/27 (TEE-backed key, no StrongBox); `CallScreeningService` (a later plan) needs API 24+, satisfied. `compileSdk`/`targetSdk = 35`.
- JDK: pin via Gradle's toolchain feature (`kotlin { jvmToolchain(17) }`), not a machine-wide `JAVA_HOME` -- this repo's CI and every contributor's machine may have other JDKs installed for unrelated work.
- Every file this plan creates lives under `android/app/src/main/kotlin/com/hushield/android/` (production) or `android/app/src/test/kotlin/com/hushield/android/` (unit tests), mirroring `ios/SpamFilter*/` as the sibling platform directory. Update `CONTRIBUTING.md`'s repository layout table to add these two lines (Task 1).
- TDD: failing test first, for the right reason, then the minimal implementation. Every task ends with `./gradlew testDebugUnitTest` (or the project's equivalent full-test task, confirmed in Task 1) actually passing, not just compiling.
- Real device/Play Integrity behavior cannot be unit-tested on a machine with no Android device or emulator attached. Where this plan hits that wall (the real `IntegrityManager` call), it follows the same pattern the backend plan (#27) used for its Google/FCM calls: implement everything around the seam fully and test it with a fake, leave the one line that calls a real, live-docs-dependent API as an explicit, TODO-marked stub rather than a guess. Do not invent Play Integrity SDK method signatures from memory -- verify against Google's current docs (via the `context7` MCP tool if available) before writing that one call, or leave it stubbed if that isn't available to the implementer.

## Review Focus

- **A device with no StrongBox** (API 26/27, or a StrongBox-less OEM on newer Android) -- key generation must still succeed with a TEE-backed key, not crash or silently produce an unusable key. Covered in Task 2.
- **A Keystore key invalidated after the fact** (`KeyPermanentlyInvalidatedException`, Android's analog to iOS's `DCError.invalidKey` that PR #24 fixed) -- must clear the stored identity and re-enroll once, not loop forever against a dead key. The translation from the raw platform exception to `AttestationProviderException.KeyUnusable` is covered in Task 4 (without it, nothing ever throws what Task 6's recovery path is watching for); the clear-and-re-enroll-once recovery itself is covered in Task 6, mirroring PR #24's fix exactly.
- **A server response whose `expires_at` isn't parseable** -- must fail with a typed error, not crash the app or silently treat the token as eternally valid. Covered in Task 6 (mirrors iOS `EnrollmentError.malformedExpiry`).
- **A stored token that's valid but about to expire within the skew window** -- `validToken()` must refresh proactively rather than handing back a token the server is about to reject. Covered in Task 6.
- **The `androidAssertion` wire format producing a signature the Go backend's `PlayIntegrityVerifier.VerifyAssertion` cannot verify** (a counter byte-order mismatch, a JSON field-name typo) -- this is a cross-repo contract with zero compiler help if it drifts. Covered in Task 4 with an exact byte-level worked example lifted from the backend's own implementation.

---

## File structure

- `android/settings.gradle.kts`, `android/build.gradle.kts`, `android/gradle.properties`, `android/app/build.gradle.kts` -- project scaffold.
- `android/app/src/main/kotlin/com/hushield/android/attest/KeystoreKeyManager.kt` (new) -- Keystore key generation, StrongBox-then-TEE fallback, key invalidation detection.
- `android/app/src/main/kotlin/com/hushield/android/attest/AttestationProvider.kt` (new) -- the interface + real/stub implementations, mirroring `ios/SpamFilterKit/AttestationProvider.swift`.
- `android/app/src/main/kotlin/com/hushield/android/attest/AndroidAssertion.kt` (new) -- the wire-format types and local signing, matching the backend's `internal/attest/android.go` exactly.
- `android/app/src/main/kotlin/com/hushield/android/net/APIClient.kt` (new) -- mirrors `ios/SpamFilterKit/APIClient.swift`'s three attest endpoints only (report/blocklist/lookup endpoints are out of scope for this plan; a later plan adds them alongside the subsystem that needs them).
- `android/app/src/main/kotlin/com/hushield/android/net/HttpTransport.kt` (new) -- thin OkHttp wrapper, mirrors `HTTPTransport`'s testability role.
- `android/app/src/main/kotlin/com/hushield/android/store/TokenStore.kt` (new) -- EncryptedSharedPreferences-backed, mirrors `ios/SpamFilterKit/TokenStore.swift`.
- `android/app/src/main/kotlin/com/hushield/android/enroll/EnrollmentService.kt` (new) -- mirrors `ios/SpamFilterKit/EnrollmentService.swift`.
- Matching `src/test/kotlin/...` test files for each of the above.
- `CONTRIBUTING.md` -- add `android/app/` to the repository layout table (Task 1).

---

### Task 1: Gradle project scaffold

**Files:**
- Create: `android/settings.gradle.kts`
- Create: `android/build.gradle.kts`
- Create: `android/gradle.properties`
- Create: `android/app/build.gradle.kts`
- Create: `android/app/src/main/AndroidManifest.xml`
- Create: `android/app/src/main/kotlin/com/hushield/android/HuShieldApplication.kt` (minimal `Application` subclass, no logic yet -- every Android app needs one, and later plans will hang initialization off it)
- Create: `android/app/src/test/kotlin/com/hushield/android/SmokeTest.kt`
- Modify: `CONTRIBUTING.md` (repository layout table)

**Interfaces:**
- Produces: a working Gradle build. Nothing downstream depends on a specific API here -- this task's job is "a later task's `./gradlew` command doesn't fail before it even reaches the code being tested."

- [ ] **Step 1: Scaffold the Gradle project**

`android/settings.gradle.kts`:
```kotlin
pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}
dependencyResolutionManagement {
    repositories {
        google()
        mavenCentral()
    }
}
rootProject.name = "hushield-android"
include(":app")
```

`android/build.gradle.kts`:
```kotlin
plugins {
    id("com.android.application") version "8.7.0" apply false
    id("org.jetbrains.kotlin.android") version "2.0.21" apply false
}
```

`android/gradle.properties`:
```properties
android.useAndroidX=true
kotlin.code.style=official
```

`android/app/build.gradle.kts`:
```kotlin
plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "com.hushield.android"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.hushield.android"
        minSdk = 26
        targetSdk = 35
        versionCode = 1
        versionName = "0.1.0"
    }

    buildTypes {
        release {
            isMinifyEnabled = false
        }
    }

    testOptions {
        unitTests.isIncludeAndroidResources = true
    }
}

kotlin {
    jvmToolchain(17)
}

dependencies {
    implementation("androidx.core:core-ktx:1.15.0")
    implementation("androidx.security:security-crypto:1.1.0-alpha06")
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    implementation("com.google.android.play:integrity:1.4.0")

    testImplementation("junit:junit:4.13.2")
    testImplementation("io.mockk:mockk:1.13.13")
    testImplementation("org.robolectric:robolectric:4.14.1")
}
```

`android/app/src/main/AndroidManifest.xml`:
```xml
<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/auto">
    <application
        android:name=".HuShieldApplication"
        android:label="HuShield"
        android:allowBackup="false" />
</manifest>
```

`android/app/src/main/kotlin/com/hushield/android/HuShieldApplication.kt`:
```kotlin
package com.hushield.android

import android.app.Application

/** No initialization logic yet -- later plans (enrollment startup, sync scheduling) hang off this. */
class HuShieldApplication : Application()
```

Generate the Gradle wrapper (pin a version compatible with AGP 8.7.0 and JDK 17):
```bash
cd android
gradle wrapper --gradle-version 8.10.2
```

- [ ] **Step 2: Write a smoke test to prove the build actually runs tests**

`android/app/src/test/kotlin/com/hushield/android/SmokeTest.kt`:
```kotlin
package com.hushield.android

import org.junit.Assert.assertEquals
import org.junit.Test

class SmokeTest {
    @Test
    fun `gradle test task actually runs`() {
        assertEquals(4, 2 + 2)
    }
}
```

- [ ] **Step 3: Run the build and the test**

```bash
cd android
./gradlew testDebugUnitTest --console=plain
```

Expected: `BUILD SUCCESSFUL`, with `SmokeTest` shown as run and passed in the output (check `android/app/build/test-results/testDebugUnitTest/` for the XML report if the console output is ambiguous). If this fails on a JDK/AGP/Gradle version mismatch, adjust the pinned versions above to a known-compatible combination for AGP 8.7.0 -- do not disable toolchain enforcement to work around it.

- [ ] **Step 4: Update `CONTRIBUTING.md`'s repository layout**

Find the existing layout code block (the one listing `cmd/server/`, `ios/SpamFilterKit/`, etc.) and add, in a sensible position near the `ios/` lines:
```
android/app/       Native Android app (Kotlin): attestation, call blocking, SMS filtering
```

- [ ] **Step 5: Commit**

```bash
git add android/ CONTRIBUTING.md
git commit -m "feat(android): scaffold the Gradle project"
```

---

### Task 2: Keystore key management

**Files:**
- Create: `android/app/src/main/kotlin/com/hushield/android/attest/KeystoreKeyManager.kt`
- Create: `android/app/src/test/kotlin/com/hushield/android/attest/KeystoreKeyManagerTest.kt`

**Interfaces:**
- Produces:
  - `class KeystoreKeyManager(private val keyStore: KeyStore = KeyStore.getInstance("AndroidKeyStore").apply { load(null) })`
  - `fun generateKey(alias: String): PublicKey` -- generates a fresh EC P-256 key pair in the given alias, StrongBox-backed if available, TEE-backed otherwise. Overwrites any existing key at that alias (mirrors iOS's `generateKeyID()` contract: "every call produces a genuinely new key").
  - `fun publicKey(alias: String): PublicKey?` -- returns the stored public key for an alias, or `null` if none exists.
  - `fun sign(alias: String, data: ByteArray): ByteArray` -- ECDSA-SHA256 signs `data` with the named key's private key. Throws `java.security.KeyStoreException` (specifically may wrap `android.security.keystore.KeyPermanentlyInvalidatedException`) if the key is gone or unusable -- Task 6's `EnrollmentService` catches this.
  - `fun deleteKey(alias: String)` -- removes the key, used when discarding a stored identity (mirrors `TokenStore.clear()`'s counterpart on the key side).

- [ ] **Step 1: Write the failing tests**

```kotlin
package com.hushield.android.attest

import org.junit.Assert.assertNotEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import java.security.KeyStore
import java.security.Signature

@RunWith(RobolectricTestRunner::class)
class KeystoreKeyManagerTest {

    @Test
    fun `generateKey produces a usable public key`() {
        val manager = KeystoreKeyManager()
        val pubKey = manager.generateKey("test-alias-1")
        assertNotNull(pubKey)
    }

    @Test
    fun `generateKey called twice for the same alias produces a genuinely different key`() {
        val manager = KeystoreKeyManager()
        val first = manager.generateKey("test-alias-2")
        val second = manager.generateKey("test-alias-2")
        assertNotEquals(first, second)
    }

    @Test
    fun `publicKey returns null for an alias that was never generated`() {
        val manager = KeystoreKeyManager()
        assertNull(manager.publicKey("never-generated-alias"))
    }

    @Test
    fun `sign produces a signature that verifies against the public key`() {
        val manager = KeystoreKeyManager()
        val pubKey = manager.generateKey("test-alias-3")
        val data = "sign-me".toByteArray()

        val signatureBytes = manager.sign("test-alias-3", data)

        val verifier = Signature.getInstance("SHA256withECDSA")
        verifier.initVerify(pubKey)
        verifier.update(data)
        assertTrue(verifier.verify(signatureBytes))
    }

    @Test
    fun `deleteKey removes the key so publicKey returns null afterward`() {
        val manager = KeystoreKeyManager()
        manager.generateKey("test-alias-4")
        manager.deleteKey("test-alias-4")
        assertNull(manager.publicKey("test-alias-4"))
    }
}
```

Note: Robolectric's `AndroidKeyStore` provider support is limited -- if `KeyStore.getInstance("AndroidKeyStore")` fails to initialize under Robolectric in practice, fall back to running these as instrumented tests (`android/app/src/androidTest/kotlin/...`, requiring a connected device or emulator) instead of unit tests, and note this explicitly in the task report. Try the unit-test path first; it's faster and this plan's other tasks assume unit-testable code wherever possible.

- [ ] **Step 2: Run tests to verify they fail**

```bash
./gradlew testDebugUnitTest --tests "com.hushield.android.attest.KeystoreKeyManagerTest" --console=plain
```

Expected: FAIL to compile -- `KeystoreKeyManager` doesn't exist yet.

- [ ] **Step 3: Implement**

```kotlin
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
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
./gradlew testDebugUnitTest --tests "com.hushield.android.attest.KeystoreKeyManagerTest" --console=plain
```

Expected: PASS, all 5 cases. If Robolectric's AndroidKeyStore support turns out inadequate (see the Step 1 note), report this in the task report as DONE_WITH_CONCERNS with the specifics, rather than silently switching to instrumented tests without flagging it -- that changes what CI can run without a device attached.

- [ ] **Step 5: Commit**

```bash
git add android/app/src/main/kotlin/com/hushield/android/attest/KeystoreKeyManager.kt android/app/src/test/kotlin/com/hushield/android/attest/KeystoreKeyManagerTest.kt
git commit -m "feat(android): add Keystore key management"
```

---

### Task 3: `AttestationProvider` abstraction

**Files:**
- Create: `android/app/src/main/kotlin/com/hushield/android/attest/AttestationProvider.kt`
- Create: `android/app/src/test/kotlin/com/hushield/android/attest/AttestationProviderTest.kt`

**Interfaces:**
- Consumes: `KeystoreKeyManager` (Task 2).
- Produces:
  - `sealed class AttestationProviderException : Exception() { object Unknown : AttestationProviderException(); object KeyUnusable : AttestationProviderException() }` -- mirrors iOS's `AttestationProviderError` exactly (`unknown` / `keyUnusable`).
  - `interface AttestationProvider { suspend fun generateKeyId(): String; suspend fun attest(keyId: String, clientDataHash: ByteArray): ByteArray; suspend fun assert(keyId: String, clientDataHash: ByteArray): ByteArray }` -- mirrors `ios/SpamFilterKit/AttestationProvider.swift`'s `AttestationProvider` protocol method-for-method (`generateKeyID`/`attest`/`assert`, `async throws` -> `suspend fun` that throws).
  - `class RealAttestationProvider(private val keyManager: KeystoreKeyManager, private val integrityDecoder: IntegrityTokenSource) : AttestationProvider` -- the production implementation.
  - `interface IntegrityTokenSource { suspend fun requestToken(nonce: ByteArray): String }` -- the seam around the real Play Integrity SDK call, exactly so this task's tests never need a real Play Integrity round-trip (mirrors the backend's `integrityTokenDecoder` seam in `internal/attest/playintegrity.go`).

**`keyId`, the server contract:** the backend's `PlayIntegrityVerifier.VerifyAttestation` (already merged, PR #27) requires `keyID == base64(SHA256(pubDER))` where `pubDER` is the key's PKIX-DER-encoded public key bytes (see `internal/attest/playintegrity.go:74-77`). `generateKeyId()` must derive its return value the same way, from the SAME encoding Java's `PublicKey.encoded` produces for an EC key (which is X.509 SubjectPublicKeyInfo -- PKIX-DER, matching what the Go side expects) -- otherwise every real enrollment attempt fails the server's key-binding check.

- [ ] **Step 1: Write the failing tests**

```kotlin
package com.hushield.android.attest

import android.util.Base64
import io.mockk.coEvery
import io.mockk.every
import io.mockk.mockk
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import java.security.MessageDigest
import java.security.PublicKey

@RunWith(RobolectricTestRunner::class)
class AttestationProviderTest {

    @Test
    fun `generateKeyId derives the key ID from sha256 of the PKIX-DER public key`() = runBlocking {
        val keyManager = KeystoreKeyManager()
        val decoder = mockk<IntegrityTokenSource>()
        val provider = RealAttestationProvider(keyManager, decoder)

        val keyId = provider.generateKeyId()
        val pubKey = keyManager.publicKey(keyId) // see note below
        // The provider must have stored the key under an alias it can look
        // up by the SAME keyId it returned -- verify the round trip instead
        // of just checking the string format.
        assertTrue(pubKey != null)

        val expectedKeyId = Base64.encodeToString(
            MessageDigest.getInstance("SHA-256").digest(pubKey!!.encoded),
            Base64.NO_WRAP
        )
        assertEquals(expectedKeyId, keyId)
    }

    @Test
    fun `attest builds a nonce from sha256(challenge concat pubkey) and returns the integrity token wrapped in the envelope`() = runBlocking {
        val keyManager = KeystoreKeyManager()
        val decoder = mockk<IntegrityTokenSource>()
        val provider = RealAttestationProvider(keyManager, decoder)

        val keyId = provider.generateKeyId()
        val pubKey = keyManager.publicKey(keyId)!!
        val challenge = "server-challenge".toByteArray()

        val expectedNonce = MessageDigest.getInstance("SHA-256").digest(challenge + pubKey.encoded)
        coEvery { decoder.requestToken(expectedNonce) } returns "fake-integrity-token"

        val envelope = provider.attest(keyId, challenge)
        val envelopeText = String(envelope)
        assertTrue(envelopeText.contains("fake-integrity-token"))
        assertTrue(envelopeText.contains(Base64.encodeToString(pubKey.encoded, Base64.NO_WRAP)))
    }

    @Test
    fun `assert produces the same androidAssertion wire format signed by the Keystore key`() = runBlocking {
        val keyManager = KeystoreKeyManager()
        val decoder = mockk<IntegrityTokenSource>()
        val provider = RealAttestationProvider(keyManager, decoder)

        val keyId = provider.generateKeyId()
        val clientDataHash = "client-data-hash".toByteArray()

        val assertion = provider.assert(keyId, clientDataHash)
        // Full wire-format correctness (counter, JSON shape) is Task 4's
        // job -- this test only proves `assert` delegates signing to the
        // Keystore key named by keyId, which Task 4's tests build on.
        assertTrue(assertion.isNotEmpty())
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
./gradlew testDebugUnitTest --tests "com.hushield.android.attest.AttestationProviderTest" --console=plain
```

Expected: FAIL to compile -- `AttestationProvider`, `RealAttestationProvider`, `IntegrityTokenSource` don't exist yet.

- [ ] **Step 3: Implement `AttestationProvider.kt` (the interface, exceptions, and provider -- `attest`'s body calls into Task 4's wire format for the envelope construction and Task 4's `AndroidAssertion` for `assert`, so implement this task's shape first and leave `attest`/`assert` calling a to-be-written `AndroidAssertion` helper that Task 4 provides -- if Task 4 hasn't landed yet in your session, write `attest`/`assert` inline here per the tests above, and Task 4 will refactor the shared envelope/signing logic out. Do not block this task on Task 4.)**

```kotlin
package com.hushield.android.attest

import android.util.Base64
import org.json.JSONObject
import java.security.MessageDigest

sealed class AttestationProviderException : Exception() {
    object Unknown : AttestationProviderException()
    object KeyUnusable : AttestationProviderException()
}

/**
 * Abstraction over the device-identity key lifecycle, mirroring
 * ios/SpamFilterKit/AttestationProvider.swift's `AttestationProvider`
 * protocol so EnrollmentService (Task 6) can be written and tested against
 * the same shape on both platforms.
 */
interface AttestationProvider {
    suspend fun generateKeyId(): String
    suspend fun attest(keyId: String, clientDataHash: ByteArray): ByteArray
    suspend fun assert(keyId: String, clientDataHash: ByteArray): ByteArray
}

/** The seam around the real Play Integrity SDK call -- see PlayIntegrityTokenSource. */
interface IntegrityTokenSource {
    suspend fun requestToken(nonce: ByteArray): String
}

class RealAttestationProvider(
    private val keyManager: KeystoreKeyManager,
    private val integrityDecoder: IntegrityTokenSource
) : AttestationProvider {

    override suspend fun generateKeyId(): String {
        val pubKey = keyManager.generateKey(alias = "pending-key")
        val keyId = deriveKeyId(pubKey.encoded)
        // Re-key under the alias this keyId will be looked up by from now on,
        // so publicKey(keyId)/sign(keyId, ...) work for every later call.
        // generateKey() always makes a fresh key, so re-deriving under the
        // final alias would produce a DIFFERENT key -- instead, alias every
        // key directly by its own keyId from the start.
        keyManager.deleteKey("pending-key")
        val finalPubKey = keyManager.generateKey(alias = keyId)
        check(deriveKeyId(finalPubKey.encoded) == keyId) {
            "key regenerated under its own keyId alias produced a different key -- this should never happen"
        }
        return keyId
    }

    override suspend fun attest(keyId: String, clientDataHash: ByteArray): ByteArray {
        val pubKey = keyManager.publicKey(keyId)
            ?: throw AttestationProviderException.KeyUnusable
        val nonce = MessageDigest.getInstance("SHA-256").digest(clientDataHash + pubKey.encoded)
        val integrityToken = integrityDecoder.requestToken(nonce)
        val envelope = JSONObject()
            .put("integrity_token", integrityToken)
            .put("public_key_der", Base64.encodeToString(pubKey.encoded, Base64.NO_WRAP))
        return envelope.toString().toByteArray()
    }

    override suspend fun assert(keyId: String, clientDataHash: ByteArray): ByteArray {
        // Task 4 (AndroidAssertion.kt) provides the counter-tracking and
        // exact wire format; this call is the seam Task 4's implementer
        // should wire this method through once that file exists.
        throw NotImplementedError("assert() wiring completed in Task 4 -- see AndroidAssertion.kt")
    }

    private fun deriveKeyId(pubKeyDer: ByteArray): String {
        return Base64.encodeToString(MessageDigest.getInstance("SHA-256").digest(pubKeyDer), Base64.NO_WRAP)
    }
}
```

Note the `generateKeyId`/re-keying dance above: because `KeystoreKeyManager.generateKey` takes an alias and the key_id IS derived from the generated key, there's a chicken-and-egg problem (you need the key to know its own alias). The implementation above resolves it by generating once under a throwaway alias to learn the key_id, then regenerating under the real alias -- report in your task notes if this feels fragile; an alternative (keep an internal alias-to-keyId mapping instead of aliasing directly by keyId) is a reasonable deviation if you hit a real problem with the throwaway-then-regenerate approach, since `generateKey` is specified to produce a genuinely new key each call, which makes the "regenerate under final alias" step non-deterministic against the throwaway key's identity -- **this is a real design gap worth flagging in your report rather than silently working around**, and the alias-to-keyId-mapping alternative (store a `Map<String, String>` from keyId to the actual Keystore alias used) is probably the more correct fix. Implement whichever you're confident is correct and explain your choice.

- [ ] **Step 4: Run tests to verify `generateKeyId` and `attest` pass** (per Step 3's note, `assert` is expected to fail/throw until Task 4 lands -- that third test should be marked `@Ignore` with a comment pointing at Task 4 if Task 4 hasn't been implemented yet in your session)

```bash
./gradlew testDebugUnitTest --tests "com.hushield.android.attest.AttestationProviderTest" --console=plain
```

- [ ] **Step 5: Commit**

```bash
git add android/app/src/main/kotlin/com/hushield/android/attest/AttestationProvider.kt android/app/src/test/kotlin/com/hushield/android/attest/AttestationProviderTest.kt
git commit -m "feat(android): add AttestationProvider with Keystore-backed key identity"
```

---

### Task 4: `AndroidAssertion` wire format (local signing) -- must match the Go backend exactly

**Files:**
- Create: `android/app/src/main/kotlin/com/hushield/android/attest/AndroidAssertion.kt`
- Create: `android/app/src/test/kotlin/com/hushield/android/attest/AndroidAssertionTest.kt`
- Modify: `android/app/src/main/kotlin/com/hushield/android/attest/AttestationProvider.kt` (wire `assert()` through to this)

**Interfaces:**
- Consumes: `KeystoreKeyManager.sign` (Task 2).
- Produces:
  - `class AndroidAssertion(private val keyManager: KeystoreKeyManager, private val counterStore: CounterStore)` where `interface CounterStore { fun next(keyId: String): Int }` -- a strictly-increasing counter per key_id, persisted (SharedPreferences is enough; this doesn't hold secrets).
  - `fun sign(keyId: String, clientDataHash: ByteArray): ByteArray` -- builds the exact JSON the Go backend's `androidAssertion` struct (`internal/attest/android.go`) expects and returns it as the assertion bytes `EnrollmentService.assert()`/`APIClient.assert()` send to the server.

**The exact wire format, copied from the already-merged, already-tested Go implementation (`internal/attest/android.go`), because this is a cross-repo contract with no compiler to catch drift:**

```go
// Go side (server), for reference -- do not re-derive this, copy it:
type androidAssertion struct {
    Counter   uint32 `json:"counter"`
    Signature []byte `json:"signature"`  // JSON-marshaled as base64 by Go's encoding/json for a []byte field
}
// The signed message: SHA256(clientDataHash || big-endian-uint32(counter))
```

So the Kotlin side must produce a JSON object `{"counter": <uint32>, "signature": "<base64>"}`, where the signature is over `SHA256(clientDataHash + counterAsBigEndian4Bytes)`. `counter` must be **strictly greater** than the previous value the server has on file for this device (the server rejects `newCounter <= prevCounter`) -- so `CounterStore.next()` must persist and monotonically increase per key_id, surviving app restarts (SharedPreferences, not an in-memory field).

- [ ] **Step 1: Write the failing tests**

```kotlin
package com.hushield.android.attest

import io.mockk.every
import io.mockk.mockk
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import java.security.MessageDigest
import java.security.Signature
import java.nio.ByteBuffer

@RunWith(RobolectricTestRunner::class)
class AndroidAssertionTest {

    @Test
    fun `sign produces JSON with the exact counter and signature fields the Go backend expects`() {
        val keyManager = KeystoreKeyManager()
        val keyId = deriveTestKeyId(keyManager, "assertion-test-key")
        val counterStore = mockk<CounterStore>()
        every { counterStore.next(keyId) } returns 1

        val assertion = AndroidAssertion(keyManager, counterStore)
        val clientDataHash = "hash-bytes".toByteArray()
        val result = assertion.sign(keyId, clientDataHash)

        val json = JSONObject(String(result))
        assertEquals(1, json.getInt("counter"))
        assertTrue(json.has("signature"))
    }

    @Test
    fun `the signature verifies against sha256(clientDataHash concat big-endian counter)`() {
        val keyManager = KeystoreKeyManager()
        val keyId = deriveTestKeyId(keyManager, "assertion-test-key-2")
        val counterStore = mockk<CounterStore>()
        every { counterStore.next(keyId) } returns 7

        val assertion = AndroidAssertion(keyManager, counterStore)
        val clientDataHash = "hash-bytes-2".toByteArray()
        val result = assertion.sign(keyId, clientDataHash)
        val json = JSONObject(String(result))

        val counterBytes = ByteBuffer.allocate(4).putInt(json.getInt("counter")).array()
        val expectedMessage = MessageDigest.getInstance("SHA-256").digest(clientDataHash + counterBytes)
        val signatureBytes = android.util.Base64.decode(json.getString("signature"), android.util.Base64.NO_WRAP)

        val verifier = Signature.getInstance("SHA256withECDSA")
        verifier.initVerify(keyManager.publicKey(keyId))
        verifier.update(expectedMessage)
        assertTrue(verifier.verify(signatureBytes))
    }

    @Test
    fun `counterStore next is called exactly once per sign call, not per verification attempt`() {
        val keyManager = KeystoreKeyManager()
        val keyId = deriveTestKeyId(keyManager, "assertion-test-key-3")
        val counterStore = mockk<CounterStore>()
        every { counterStore.next(keyId) } returns 1

        val assertion = AndroidAssertion(keyManager, counterStore)
        assertion.sign(keyId, "x".toByteArray())

        io.mockk.verify(exactly = 1) { counterStore.next(keyId) }
    }

    private fun deriveTestKeyId(keyManager: KeystoreKeyManager, alias: String): String {
        val pubKey = keyManager.generateKey(alias)
        val keyId = android.util.Base64.encodeToString(
            MessageDigest.getInstance("SHA-256").digest(pubKey.encoded),
            android.util.Base64.NO_WRAP
        )
        // Regenerate under the derived keyId so keyManager.publicKey(keyId) works,
        // same dance AttestationProvider.generateKeyId() does in Task 3.
        keyManager.deleteKey(alias)
        keyManager.generateKey(keyId)
        return keyId
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
./gradlew testDebugUnitTest --tests "com.hushield.android.attest.AndroidAssertionTest" --console=plain
```

Expected: FAIL to compile -- `AndroidAssertion`, `CounterStore` don't exist yet.

- [ ] **Step 3: Implement**

```kotlin
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
 */
class AndroidAssertion(
    private val keyManager: KeystoreKeyManager,
    private val counterStore: CounterStore
) {
    fun sign(keyId: String, clientDataHash: ByteArray): ByteArray {
        val counter = counterStore.next(keyId)
        val counterBytes = ByteBuffer.allocate(4).putInt(counter).array()
        val message = MessageDigest.getInstance("SHA-256").digest(clientDataHash + counterBytes)
        val signature = keyManager.sign(keyId, message)

        val json = JSONObject()
            .put("counter", counter)
            .put("signature", Base64.encodeToString(signature, Base64.NO_WRAP))
        return json.toString().toByteArray()
    }
}
```

Now wire `AttestationProvider.assert()` through to this, and translate Android's key-invalidation exception to `AttestationProviderException.KeyUnusable` -- **this is the load-bearing part of this task, not a side note.** Without it, `EnrollmentService.validToken()`'s recovery path (Task 6) can never trigger, because nothing ever throws the exception it's watching for. This is exactly the bug PR #24 fixed on iOS (`DCError.invalidKey` was never translated to `keyUnusable` before that fix); mirror that fix here from the start rather than reintroducing it as a fresh Android bug report later.

Edit `AttestationProvider.kt`:

```kotlin
import android.security.keystore.KeyPermanentlyInvalidatedException

class RealAttestationProvider(
    private val keyManager: KeystoreKeyManager,
    private val integrityDecoder: IntegrityTokenSource,
    private val androidAssertion: AndroidAssertion
) : AttestationProvider {
    // ... generateKeyId, attest unchanged ...

    override suspend fun assert(keyId: String, clientDataHash: ByteArray): ByteArray {
        return try {
            androidAssertion.sign(keyId, clientDataHash)
        } catch (e: KeyPermanentlyInvalidatedException) {
            // The keyId names a Keystore entry that no longer names a usable
            // key -- the Android analog of DCError.invalidKey (see
            // AttestationProviderException.KeyUnusable's doc comment).
            // EnrollmentService.validToken() catches this, clears the stored
            // identity, and enrolls once with a genuinely new key.
            throw AttestationProviderException.KeyUnusable
        }
    }
}
```

(This changes `RealAttestationProvider`'s constructor -- update Task 3's tests that construct it to pass an `AndroidAssertion` instance, and remove the `@Ignore` from the `assert` test you marked in Task 3 Step 4.)

- [ ] **Step 4: Write a failing test proving the translation, then confirm it and the rest pass**

Add to `AndroidAssertionTest.kt` or a new small test in `AttestationProviderTest.kt` (your call on placement -- it needs a `RealAttestationProvider` wired to a `KeystoreKeyManager`/`AndroidAssertion` pair where signing throws `KeyPermanentlyInvalidatedException`, which likely means mocking `AndroidAssertion.sign` to throw it directly, since provoking Keystore into a genuinely invalidated state isn't reachable from a unit test):

```kotlin
@Test
fun `assert translates KeyPermanentlyInvalidatedException into AttestationProviderException KeyUnusable`() = runBlocking {
    val keyManager = mockk<KeystoreKeyManager>()
    val decoder = mockk<IntegrityTokenSource>()
    val androidAssertion = mockk<AndroidAssertion>()
    every { androidAssertion.sign(any(), any()) } throws
        android.security.keystore.KeyPermanentlyInvalidatedException()

    val provider = RealAttestationProvider(keyManager, decoder, androidAssertion)

    assertThrows(AttestationProviderException.KeyUnusable::class) {
        runBlocking { provider.assert("some-key-id", "hash".toByteArray()) }
    }
}
```

```bash
./gradlew testDebugUnitTest --tests "com.hushield.android.attest.AndroidAssertionTest" --console=plain
./gradlew testDebugUnitTest --tests "com.hushield.android.attest.AttestationProviderTest" --console=plain
```

Expected: PASS, all cases in both files, including the previously-`@Ignore`d `assert` test now un-ignored and passing.

- [ ] **Step 5: Commit**

```bash
git add android/app/src/main/kotlin/com/hushield/android/attest/AndroidAssertion.kt android/app/src/main/kotlin/com/hushield/android/attest/AttestationProvider.kt android/app/src/test/kotlin/com/hushield/android/attest/AndroidAssertionTest.kt android/app/src/test/kotlin/com/hushield/android/attest/AttestationProviderTest.kt
git commit -m "feat(android): wire the per-request assertion signing matching the Go backend's wire format"
```

---

### Task 5: `TokenStore` (EncryptedSharedPreferences)

**Files:**
- Create: `android/app/src/main/kotlin/com/hushield/android/store/TokenStore.kt`
- Create: `android/app/src/test/kotlin/com/hushield/android/store/TokenStoreTest.kt`

**Interfaces:**
- Produces:
  - `interface TokenStore { fun saveToken(token: String, expiresAt: Instant); fun loadToken(): Pair<String, Instant>?; fun saveKeyId(keyId: String); fun loadKeyId(): String?; fun clear() }` -- mirrors `ios/SpamFilterKit/TokenStore.swift`'s `TokenStore` protocol exactly (`saveToken`/`loadToken`/`saveKeyID`/`loadKeyID`/`clear`).
  - `class EncryptedPrefsTokenStore(context: Context) : TokenStore` -- the production implementation.

- [ ] **Step 1: Write the failing tests**

```kotlin
package com.hushield.android.store

import android.content.Context
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import java.time.Instant

@RunWith(RobolectricTestRunner::class)
class TokenStoreTest {

    private fun newStore(): TokenStore {
        val context = ApplicationProvider.getApplicationContext<Context>()
        return EncryptedPrefsTokenStore(context)
    }

    @Test
    fun `saveToken then loadToken round-trips the token and expiry`() {
        val store = newStore()
        val expiry = Instant.now().plusSeconds(3600)
        store.saveToken("device-token-abc", expiry)

        val loaded = store.loadToken()
        assertEquals("device-token-abc", loaded?.first)
        assertEquals(expiry.epochSecond, loaded?.second?.epochSecond)
    }

    @Test
    fun `loadToken returns null when nothing was saved`() {
        assertNull(newStore().loadToken())
    }

    @Test
    fun `saveKeyId then loadKeyId round-trips`() {
        val store = newStore()
        store.saveKeyId("key-id-xyz")
        assertEquals("key-id-xyz", store.loadKeyId())
    }

    @Test
    fun `clear removes both the token and the key id`() {
        val store = newStore()
        store.saveToken("t", Instant.now())
        store.saveKeyId("k")
        store.clear()
        assertNull(store.loadToken())
        assertNull(store.loadKeyId())
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
./gradlew testDebugUnitTest --tests "com.hushield.android.store.TokenStoreTest" --console=plain
```

Expected: FAIL to compile -- `TokenStore`, `EncryptedPrefsTokenStore` don't exist yet.

- [ ] **Step 3: Implement**

```kotlin
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
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
./gradlew testDebugUnitTest --tests "com.hushield.android.store.TokenStoreTest" --console=plain
```

Expected: PASS, all 4 cases. If Robolectric can't resolve `EncryptedSharedPreferences`'s Keystore dependency in a unit test (a known friction point for this library under Robolectric), report DONE_WITH_CONCERNS with specifics rather than silently downgrading to plain unencrypted `SharedPreferences` -- that would be a real security regression from the spec's intent, not a test-infrastructure detail to route around quietly.

- [ ] **Step 5: Commit**

```bash
git add android/app/src/main/kotlin/com/hushield/android/store/TokenStore.kt android/app/src/test/kotlin/com/hushield/android/store/TokenStoreTest.kt
git commit -m "feat(android): add EncryptedSharedPreferences-backed TokenStore"
```

---

### Task 6: `APIClient` (attest endpoints) + `EnrollmentService`

**Files:**
- Create: `android/app/src/main/kotlin/com/hushield/android/net/HttpTransport.kt`
- Create: `android/app/src/main/kotlin/com/hushield/android/net/APIClient.kt`
- Create: `android/app/src/main/kotlin/com/hushield/android/enroll/EnrollmentService.kt`
- Create matching test files under `src/test/kotlin/com/hushield/android/net/` and `.../enroll/`

**Interfaces:**
- Consumes: `AttestationProvider` (Task 3/4), `TokenStore` (Task 5).
- Produces:
  - `interface HttpTransport { suspend fun send(request: HttpRequest): HttpResponse }` with simple `HttpRequest`/`HttpResponse` data classes (method, path, body, headers, status code, body) -- the OkHttp-backed testability seam, mirroring `HTTPTransport`'s role for the iOS `APIClient`.
  - `class APIClient(private val transport: HttpTransport, private val baseUrl: String)` with `suspend fun challenge(): ChallengeResponse`, `suspend fun verify(keyId: String, attestationB64: String, challengeB64: String, platform: String = "android"): DeviceTokenResponse`, `suspend fun assert(keyId: String, assertionB64: String, challengeB64: String): DeviceTokenResponse` -- mirrors `ios/SpamFilterKit/APIClient.swift`'s three attest methods. Note `verify` sends `"platform": "android"` in its request body (the field the backend's `verifyRequest.Platform` reads, PR #27) -- `assert` sends NO platform field, matching the backend's `assertRequest` having none (the server uses the device's stored platform, never a client-supplied one -- see the backend plan's Task 6 security property).
  - `class EnrollmentService(private val apiClient: APIClient, private val provider: AttestationProvider, private val tokenStore: TokenStore, private val tokenExpirySkewSeconds: Long = 30)` with `suspend fun enroll()`, `suspend fun refresh()`, `suspend fun validToken(): String` -- mirrors `ios/SpamFilterKit/EnrollmentService.swift` method-for-method, including the `KeyUnusable` recovery path in `validToken()` (clear the stored identity, enroll once, propagate a second failure -- exactly PR #24's fix, applied to the Android side from day one instead of needing its own later bug report).
  - `sealed class EnrollmentException : Exception() { object NotEnrolled : EnrollmentException(); data class MalformedExpiry(val raw: String) : EnrollmentException() }` -- mirrors iOS's `EnrollmentError`.

- [ ] **Step 1: Write the failing tests for `APIClient`**

```kotlin
package com.hushield.android.net

import io.mockk.coEvery
import io.mockk.mockk
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class APIClientTest {

    @Test
    fun `challenge sends POST to attest challenge and decodes the response`() = runBlocking {
        val transport = mockk<HttpTransport>()
        coEvery { transport.send(match { it.method == "POST" && it.path == "/api/v1/attest/challenge" }) } returns
            HttpResponse(200, """{"data":{"challenge":"abc123","expires_at":"2026-01-01T00:00:00Z"}}""".toByteArray())

        val client = APIClient(transport, "http://localhost:8080")
        val result = client.challenge()
        assertEquals("abc123", result.challenge)
    }

    @Test
    fun `verify sends platform android in the request body`() = runBlocking {
        val transport = mockk<HttpTransport>()
        var capturedBody: String? = null
        coEvery { transport.send(match { it.method == "POST" && it.path == "/api/v1/attest/verify" }) } answers {
            capturedBody = String(firstArg<HttpRequest>().body!!)
            HttpResponse(200, """{"data":{"device_token":"tok","expires_at":"2026-01-01T00:00:00Z"}}""".toByteArray())
        }

        val client = APIClient(transport, "http://localhost:8080")
        client.verify(keyId = "kid", attestationB64 = "att", challengeB64 = "chal")

        assertTrue(capturedBody!!.contains("\"platform\":\"android\""))
    }

    @Test
    fun `assert sends no platform field at all`() = runBlocking {
        val transport = mockk<HttpTransport>()
        var capturedBody: String? = null
        coEvery { transport.send(match { it.method == "POST" && it.path == "/api/v1/attest/assert" }) } answers {
            capturedBody = String(firstArg<HttpRequest>().body!!)
            HttpResponse(200, """{"data":{"device_token":"tok","expires_at":"2026-01-01T00:00:00Z"}}""".toByteArray())
        }

        val client = APIClient(transport, "http://localhost:8080")
        client.assert(keyId = "kid", assertionB64 = "asrt", challengeB64 = "chal")

        assertTrue(!capturedBody!!.contains("platform"))
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
./gradlew testDebugUnitTest --tests "com.hushield.android.net.APIClientTest" --console=plain
```

Expected: FAIL to compile.

- [ ] **Step 3: Implement `HttpTransport.kt` and `APIClient.kt`**

```kotlin
package com.hushield.android.net

data class HttpRequest(val method: String, val path: String, val body: ByteArray? = null)
data class HttpResponse(val statusCode: Int, val body: ByteArray)

interface HttpTransport {
    suspend fun send(request: HttpRequest): HttpResponse
}

class OkHttpTransport(private val client: okhttp3.OkHttpClient, private val baseUrl: String) : HttpTransport {
    override suspend fun send(request: HttpRequest): HttpResponse {
        val builder = okhttp3.Request.Builder().url(baseUrl + request.path)
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
```

```kotlin
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
```

- [ ] **Step 4: Run `APIClient` tests to verify they pass**

```bash
./gradlew testDebugUnitTest --tests "com.hushield.android.net.APIClientTest" --console=plain
```

- [ ] **Step 5: Write the failing tests for `EnrollmentService`**

```kotlin
package com.hushield.android.enroll

import com.hushield.android.attest.AttestationProvider
import com.hushield.android.attest.AttestationProviderException
import com.hushield.android.net.APIClient
import com.hushield.android.net.ChallengeResponse
import com.hushield.android.net.DeviceTokenResponse
import com.hushield.android.store.TokenStore
import io.mockk.coEvery
import io.mockk.every
import io.mockk.mockk
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertThrows
import org.junit.Test
import java.time.Instant

class EnrollmentServiceTest {

    @Test
    fun `enroll generates a key when none is stored, attests it, and persists both`() = runBlocking {
        val apiClient = mockk<APIClient>()
        val provider = mockk<AttestationProvider>()
        val tokenStore = mockk<TokenStore>(relaxUnitFun = true)

        every { tokenStore.loadKeyId() } returns null
        coEvery { provider.generateKeyId() } returns "new-key-id"
        coEvery { apiClient.challenge() } returns ChallengeResponse("chal123", "2099-01-01T00:00:00Z")
        coEvery { provider.attest("new-key-id", any()) } returns "attestation-bytes".toByteArray()
        coEvery { apiClient.verify("new-key-id", any(), "chal123") } returns
            DeviceTokenResponse("tok", "2026-01-01T00:00:00Z")

        val service = EnrollmentService(apiClient, provider, tokenStore)
        service.enroll()

        io.mockk.verify { tokenStore.saveKeyId("new-key-id") }
        io.mockk.verify { tokenStore.saveToken("tok", any()) }
    }

    @Test
    fun `validToken returns the cached token when it is not near expiry`() = runBlocking {
        val apiClient = mockk<APIClient>()
        val provider = mockk<AttestationProvider>()
        val tokenStore = mockk<TokenStore>()

        every { tokenStore.loadToken() } returns ("cached-token" to Instant.now().plusSeconds(3600))

        val service = EnrollmentService(apiClient, provider, tokenStore)
        val result = service.validToken()

        assertEquals("cached-token", result)
    }

    @Test
    fun `validToken clears the stored identity and re-enrolls once on KeyUnusable, then propagates a second failure`() = runBlocking {
        val apiClient = mockk<APIClient>()
        val provider = mockk<AttestationProvider>()
        val tokenStore = mockk<TokenStore>(relaxUnitFun = true)

        every { tokenStore.loadToken() } returns ("stale-token" to Instant.now().minusSeconds(10)) andThen null
        every { tokenStore.loadKeyId() } returns "dead-key-id" andThen null
        coEvery { apiClient.challenge() } returns ChallengeResponse("chal", "2099-01-01T00:00:00Z")
        coEvery { provider.assert("dead-key-id", any()) } throws AttestationProviderException.KeyUnusable
        coEvery { provider.generateKeyId() } throws AttestationProviderException.KeyUnusable // second attempt also fails

        val service = EnrollmentService(apiClient, provider, tokenStore)

        assertThrows(AttestationProviderException.KeyUnusable::class.java) {
            runBlocking { service.validToken() }
        }
        io.mockk.verify { tokenStore.clear() }
    }
}
```

- [ ] **Step 6: Run tests to verify they fail**

```bash
./gradlew testDebugUnitTest --tests "com.hushield.android.enroll.EnrollmentServiceTest" --console=plain
```

Expected: FAIL to compile.

- [ ] **Step 7: Implement `EnrollmentService.kt`**

```kotlin
package com.hushield.android.enroll

import com.hushield.android.attest.AttestationProvider
import com.hushield.android.attest.AttestationProviderException
import com.hushield.android.net.APIClient
import com.hushield.android.store.TokenStore
import java.security.MessageDigest
import java.time.Instant
import java.time.format.DateTimeFormatter
import android.util.Base64

sealed class EnrollmentException : Exception() {
    object NotEnrolled : EnrollmentException()
    data class MalformedExpiry(val raw: String) : EnrollmentException()
}

/**
 * Composes APIClient + AttestationProvider + TokenStore into the enroll/
 * refresh flow described by the backend's /api/v1/attest/* endpoints,
 * mirroring ios/SpamFilterKit/EnrollmentService.swift method-for-method.
 */
class EnrollmentService(
    private val apiClient: APIClient,
    private val provider: AttestationProvider,
    private val tokenStore: TokenStore,
    private val tokenExpirySkewSeconds: Long = 30
) {

    suspend fun enroll() {
        val keyId = tokenStore.loadKeyId() ?: provider.generateKeyId()

        val challengeResponse = apiClient.challenge()
        val clientDataHash = clientDataHash(challengeResponse.challenge)
        val attestation = provider.attest(keyId, clientDataHash)
        val tokenResponse = apiClient.verify(
            keyId = keyId,
            attestationB64 = Base64.encodeToString(attestation, Base64.NO_WRAP),
            challengeB64 = challengeResponse.challenge
        )
        val expiresAt = parseExpiry(tokenResponse.expiresAt)

        tokenStore.saveKeyId(keyId)
        tokenStore.saveToken(tokenResponse.deviceToken, expiresAt)
    }

    suspend fun refresh() {
        val keyId = tokenStore.loadKeyId() ?: throw EnrollmentException.NotEnrolled

        val challengeResponse = apiClient.challenge()
        val clientDataHash = clientDataHash(challengeResponse.challenge)
        val assertion = provider.assert(keyId, clientDataHash)
        val tokenResponse = apiClient.assert(
            keyId = keyId,
            assertionB64 = Base64.encodeToString(assertion, Base64.NO_WRAP),
            challengeB64 = challengeResponse.challenge
        )
        val expiresAt = parseExpiry(tokenResponse.expiresAt)

        tokenStore.saveToken(tokenResponse.deviceToken, expiresAt)
    }

    suspend fun validToken(): String {
        val stored = tokenStore.loadToken()
        if (stored != null && stored.second.epochSecond > Instant.now().epochSecond + tokenExpirySkewSeconds) {
            return stored.first
        }

        try {
            if (tokenStore.loadToken() == null) {
                enroll()
            } else {
                refresh()
            }
        } catch (e: AttestationProviderException.KeyUnusable) {
            // Mirrors PR #24's iOS fix: the stored key_id names a key this
            // install can no longer use. Retrying with the same key_id loops
            // forever, so discard the whole stored identity and enroll once
            // with a genuinely new key; a second failure propagates.
            tokenStore.clear()
            enroll()
        }

        return tokenStore.loadToken()?.first ?: throw EnrollmentException.NotEnrolled
    }

    private fun clientDataHash(challengeB64: String): ByteArray {
        val challengeBytes = Base64.decode(challengeB64, Base64.NO_WRAP)
        return MessageDigest.getInstance("SHA-256").digest(challengeBytes)
    }

    private fun parseExpiry(raw: String): Instant {
        return try {
            Instant.from(DateTimeFormatter.ISO_INSTANT.parse(raw))
        } catch (e: Exception) {
            throw EnrollmentException.MalformedExpiry(raw)
        }
    }
}
```

- [ ] **Step 8: Run tests to verify they pass**

```bash
./gradlew testDebugUnitTest --tests "com.hushield.android.enroll.EnrollmentServiceTest" --console=plain
```

Expected: PASS, all 3 cases.

- [ ] **Step 9: Run the full module test suite**

```bash
./gradlew testDebugUnitTest --console=plain
```

Expected: every test in every file this plan created passes.

- [ ] **Step 10: Commit**

```bash
git add android/app/src/main/kotlin/com/hushield/android/net/ android/app/src/main/kotlin/com/hushield/android/enroll/ android/app/src/test/kotlin/com/hushield/android/net/ android/app/src/test/kotlin/com/hushield/android/enroll/
git commit -m "feat(android): add APIClient attest endpoints and EnrollmentService"
```

---

### Task 7: The real Play Integrity call (deliberately deferred, mirroring the backend's Google/FCM stubs)

**Files:**
- Create: `android/app/src/main/kotlin/com/hushield/android/attest/PlayIntegrityTokenSource.kt`
- Create: `android/app/src/test/kotlin/com/hushield/android/attest/PlayIntegrityTokenSourceTest.kt` (only if there's something testable without a real Play Integrity round-trip -- see below)

**Interfaces:**
- Produces: `class PlayIntegrityTokenSource(private val integrityManager: com.google.android.play.core.integrity.IntegrityManager, private val cloudProjectNumber: Long) : IntegrityTokenSource`

This is this plan's one deliberate, pre-authorized exception to full specification, matching the pattern the backend plan (#27) used for `googlePlayIntegrityDecoder.Decode` and the FCM request body: the real Play Integrity SDK call (`IntegrityManager.requestIntegrityToken`, a Play Services API) has an exact method signature and callback/Task-based API that should be verified against Google's current documentation at implementation time (via the `context7` MCP tool if available), not guessed from training data that may be stale on a fast-moving Play Services library.

- [ ] **Step 1: Implement a real attempt, with a TODO fallback if the exact API can't be verified**

Attempt the real call using Google's documented pattern (as of recent Play Integrity API versions, `IntegrityManager.requestIntegrityToken(IntegrityTokenRequest)` returns a `Task<IntegrityTokenResponse>`):

```kotlin
package com.hushield.android.attest

import android.util.Base64
import com.google.android.play.core.integrity.IntegrityManager
import com.google.android.play.core.integrity.IntegrityTokenRequest
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

/**
 * The real Play Integrity SDK call. VERIFY THIS AGAINST GOOGLE'S CURRENT
 * DOCS BEFORE TRUSTING IT -- the Play Integrity library's API has changed
 * across versions and this was written without a live round-trip against a
 * real device. If `requestIntegrityToken`'s signature, the nonce parameter
 * name, or the response accessor below don't match the actual
 * `com.google.android.play:integrity` version pinned in Task 1's
 * build.gradle.kts, fix this file, not the version pin, unless the pinned
 * version is itself the problem.
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
```

- [ ] **Step 2: Verify the build compiles against the actual pinned Play Integrity library version**

```bash
./gradlew compileDebugKotlin --console=plain
```

If this fails because `IntegrityTokenRequest.builder()`, `.setNonce()`, `.setCloudProjectNumber()`, or `response.token()` don't exist on the pinned version (1.4.0, from Task 1) -- check the actual API surface of that installed dependency (`./gradlew :app:dependencies` or unpacking the AAR) and fix the calls to match what's really there, rather than guessing at a different plausible-sounding method name. This is exactly the situation the backend plan's Task 4 anticipated for its own Google API call: prefer a working build against the real, present API over an unverified guess.

- [ ] **Step 3: If it does not compile and the real API can't be determined without live documentation access**

Fall back to a stub matching the backend's own pattern (`internal/attest/playintegrity.go`'s `googlePlayIntegrityDecoder.Decode`):

```kotlin
override suspend fun requestToken(nonce: ByteArray): String {
    // TODO(android): Play Integrity SDK call not yet verified against the
    // actual com.google.android.play:integrity API surface -- see task
    // report for what was tried. Enrollment will fail closed (an exception
    // here propagates up through AttestationProvider.attest ->
    // EnrollmentService.enroll, which fails the whole enroll() call) rather
    // than silently proceeding without a real token.
    throw NotImplementedError("PlayIntegrityTokenSource.requestToken: verify against Google's current Play Integrity API docs before implementing")
}
```

Report which path you took (real implementation vs. stub) clearly in your task report -- this determines whether the app can actually enroll a real device yet.

- [ ] **Step 4: Wire `RealAttestationProvider`'s production construction site**

There is no production entry point yet (no `Application.onCreate()` wiring -- that's a later plan, once there's a UI or a background service to trigger enrollment). For this task, just confirm `PlayIntegrityTokenSource` type-checks as an `IntegrityTokenSource` and compiles; do not add unused wiring code to `HuShieldApplication` speculatively.

- [ ] **Step 5: Run the full test suite one more time to confirm nothing regressed**

```bash
./gradlew testDebugUnitTest --console=plain
```

- [ ] **Step 6: Commit**

```bash
git add android/app/src/main/kotlin/com/hushield/android/attest/PlayIntegrityTokenSource.kt
git commit -m "feat(android): add Play Integrity token source (verify against live API docs -- see file header)"
```

---

## What this plan deliberately does not cover

- `PlayIntegrityTokenSource`'s real SDK call (Task 7) may land as a verified implementation or a TODO-marked stub, exactly like the backend plan's Google/FCM calls -- this is disclosed, not hidden.
- No UI, no `CallScreeningService`, no SMS filtering, no local blocklist Room database. Each is a separate follow-up plan per the spec's subsystem breakdown; this plan's only job is "a device can obtain a token from the server."
- No push/FCM token registration on the Android side (the backend side already exists from PR #27) -- that's naturally scoped into whichever later plan first needs the app to receive a silent refresh push (likely the call-blocking plan, since that's the thing a push would refresh).
- No instrumented/on-device testing. Everything in this plan should be unit-testable via Robolectric/MockK on a machine with no emulator or physical device attached, per this plan's Global Constraints. If Task 2 or Task 5 hit a genuine Robolectric limitation, that gets disclosed in the task report rather than worked around silently -- true on-device verification (Keystore/StrongBox availability actually varies by real hardware) is out of scope for this plan and belongs in a manual verification pass before this ships, mirroring how the backend plan flagged "verify manually against a real Play Integrity token" as a follow-up.
