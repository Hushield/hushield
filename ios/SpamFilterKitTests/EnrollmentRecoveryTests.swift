import XCTest
@testable import SpamFilterKit

/// Regression tests for a device-observed defect: a stored App Attest identity
/// can become permanently unusable, and the app had no way out of that state.
///
/// On a real device the Secure Enclave key named by the stored key ID stops
/// being usable whenever the install's App Attest identity changes -- a
/// reinstall, or a build that switches the `appattest-environment` entitlement
/// between `development` and `production`. The Keychain items outlive that
/// change, so `TokenStore` keeps handing out a key ID that DeviceCheck now
/// rejects with `DCError.invalidKey`.
///
/// Pre-fix, `validToken()` propagated that error and nothing ever called
/// `TokenStore.clear()`, so every retry took the identical path and failed
/// identically -- the app was bricked for reporting until it was deleted. The
/// captured failure was:
///
///     validToken: storedToken=present expiry=2026-08-28 storedKeyID=present
///     validToken: -> refresh()
///     refresh: generateAssertion FAILED domain=com.apple.devicecheck.error code=3
///
/// Post-fix, an unusable key discards the stored identity and re-enrols with a
/// genuinely new key.
final class EnrollmentRecoveryTests: XCTestCase {
    private let baseURL = URL(string: "https://api.example.test")!

    private func makeClient(transport: MockTransport) -> APIClient {
        APIClient(transport: transport, baseURL: baseURL)
    }

    private func enqueueChallenge(_ transport: MockTransport, requestID: String) {
        transport.enqueue((jsonData("""
        {"success":true,"data":{"challenge":"Y2hhbGxlbmdl","expires_at":"2026-07-23T12:05:00Z"},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"\(requestID)"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
    }

    private func enqueueDeviceToken(_ transport: MockTransport, token: String, requestID: String) {
        transport.enqueue((jsonData("""
        {"success":true,"data":{"device_token":"\(token)","expires_at":"2099-01-01T00:00:00Z"},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"\(requestID)"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
    }

    /// The exact device scenario: an expired token plus a stale key ID sends
    /// `validToken()` down the `refresh()` branch, where `generateAssertion`
    /// fails because the key no longer exists in this install.
    func test_validToken_whenStoredKeyCannotAssert_discardsIdentityAndReEnrols() async throws {
        let transport = MockTransport()
        enqueueChallenge(transport, requestID: "req-refresh")   // consumed by the failing refresh()
        enqueueChallenge(transport, requestID: "req-enroll")    // consumed by the recovery enroll()
        enqueueDeviceToken(transport, token: "recovered-token", requestID: "req-token")

        let provider = MockAttestationProvider()
        provider.assertError = AttestationProviderError.keyUnusable
        provider.stubKeyID = "fresh-key"

        let tokenStore = InMemoryTokenStore()
        tokenStore.saveKeyID("stale-key-from-previous-install")
        tokenStore.saveToken("expired-token", expiresAt: Date(timeIntervalSince1970: 0))

        let service = EnrollmentService(apiClient: makeClient(transport: transport), provider: provider, tokenStore: tokenStore)

        let token = try await service.validToken()

        XCTAssertEqual(token, "recovered-token")
        XCTAssertEqual(provider.generateKeyIDCallCount, 1, "recovery must mint a genuinely new key, not reuse the rejected one")
        XCTAssertEqual(provider.lastAttestKeyID, "fresh-key")
        XCTAssertEqual(tokenStore.loadKeyID(), "fresh-key", "the stale key ID must not survive recovery")
    }

    /// The same defect reached through the `enroll()` branch: the token item is
    /// gone but the key ID survives, so `enroll()` reuses it and `attestKey`
    /// rejects it (Apple's documented "key that's already been attested" case).
    func test_validToken_whenStoredKeyCannotAttest_discardsIdentityAndReEnrols() async throws {
        let transport = MockTransport()
        enqueueChallenge(transport, requestID: "req-enroll-1")  // consumed by the failing enroll()
        enqueueChallenge(transport, requestID: "req-enroll-2")  // consumed by the recovery enroll()
        enqueueDeviceToken(transport, token: "recovered-token", requestID: "req-token")

        let provider = MockAttestationProvider()
        provider.attestErrorOnce = AttestationProviderError.keyUnusable
        provider.stubKeyID = "fresh-key"

        let tokenStore = InMemoryTokenStore()
        tokenStore.saveKeyID("stale-key-from-previous-install")

        let service = EnrollmentService(apiClient: makeClient(transport: transport), provider: provider, tokenStore: tokenStore)

        let token = try await service.validToken()

        XCTAssertEqual(token, "recovered-token")
        XCTAssertEqual(provider.attestCallCount, 2, "the rejected key is attested once, then the fresh key")
        XCTAssertEqual(tokenStore.loadKeyID(), "fresh-key")
    }

    /// Recovery is one-shot. If the freshly minted key is also rejected, the
    /// error propagates instead of looping forever against Apple's servers.
    func test_validToken_whenFreshKeyAlsoUnusable_propagatesInsteadOfLooping() async throws {
        let transport = MockTransport()
        enqueueChallenge(transport, requestID: "req-1")
        enqueueChallenge(transport, requestID: "req-2")

        let provider = MockAttestationProvider()
        provider.assertError = AttestationProviderError.keyUnusable
        provider.attestError = AttestationProviderError.keyUnusable

        let tokenStore = InMemoryTokenStore()
        tokenStore.saveKeyID("stale-key")
        tokenStore.saveToken("expired-token", expiresAt: Date(timeIntervalSince1970: 0))

        let service = EnrollmentService(apiClient: makeClient(transport: transport), provider: provider, tokenStore: tokenStore)

        do {
            _ = try await service.validToken()
            XCTFail("expected the second failure to propagate")
        } catch AttestationProviderError.keyUnusable {
            XCTAssertEqual(provider.attestCallCount, 1, "must not retry beyond the single recovery attempt")
        }
    }

    /// The `DCError.invalidKey` -> `keyUnusable` translation, which is what
    /// lets `EnrollmentService` recognise the condition without importing
    /// DeviceCheck. Codes other than `invalidKey` must pass through unchanged
    /// so a transient outage is never mistaken for a dead key.
    func test_deviceCheckErrorMapping_translatesOnlyInvalidKey() {
        let invalidKey = NSError(domain: "com.apple.devicecheck.error", code: 3, userInfo: nil)
        XCTAssertEqual(DeviceAttestationProvider.mapDeviceCheckError(invalidKey) as? AttestationProviderError, .keyUnusable)

        let serverUnavailable = NSError(domain: "com.apple.devicecheck.error", code: 4, userInfo: nil)
        XCTAssertNil(DeviceAttestationProvider.mapDeviceCheckError(serverUnavailable) as? AttestationProviderError)

        let unrelated = NSError(domain: NSURLErrorDomain, code: 3, userInfo: nil)
        XCTAssertNil(DeviceAttestationProvider.mapDeviceCheckError(unrelated) as? AttestationProviderError)
    }
}
