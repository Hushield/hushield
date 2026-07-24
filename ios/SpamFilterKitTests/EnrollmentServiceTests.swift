import XCTest
import CryptoKit
@testable import SpamFilterKit

final class EnrollmentServiceTests: XCTestCase {
    private let baseURL = URL(string: "https://api.example.test")!

    private func makeClient(transport: MockTransport) -> APIClient {
        APIClient(transport: transport, baseURL: baseURL)
    }

    private func enqueueChallenge(_ transport: MockTransport, challenge: String, expiresAt: String = "2026-07-23T12:05:00Z", requestID: String = "req-c") {
        transport.enqueue((jsonData("""
        {"success":true,"data":{"challenge":"\(challenge)","expires_at":"\(expiresAt)"},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"\(requestID)"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
    }

    private func enqueueDeviceToken(_ transport: MockTransport, token: String, expiresAt: String, requestID: String = "req-t") {
        transport.enqueue((jsonData("""
        {"success":true,"data":{"device_token":"\(token)","expires_at":"\(expiresAt)"},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"\(requestID)"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
    }

    private func enqueueError(_ transport: MockTransport, status: Int, code: String, message: String, field: String = "", requestID: String = "req-e") {
        transport.enqueue((jsonData("""
        {"success":false,"data":null,"errors":[{"field":"\(field)","message":"\(message)","code":"\(code)"}],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"\(requestID)"}}
        """), makeHTTPResponse(url: baseURL, status: status)))
    }

    private func expectedDate(_ rfc3339: String) -> Date {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        return formatter.date(from: rfc3339)!
    }

    // MARK: - enroll() happy path

    func test_enroll_happyPath_generatesKey_persistsTokenAndKeyID() async throws {
        let transport = MockTransport()
        let challengeB64 = "Y2hhbGxlbmdl" // base64("challenge")
        enqueueChallenge(transport, challenge: challengeB64)
        enqueueDeviceToken(transport, token: "dtok-1", expiresAt: "2026-07-24T12:00:00Z")

        let provider = MockAttestationProvider()
        provider.stubKeyID = "generated-key-1"
        let tokenStore = InMemoryTokenStore()
        let service = EnrollmentService(apiClient: makeClient(transport: transport), provider: provider, tokenStore: tokenStore)

        try await service.enroll()

        XCTAssertEqual(provider.generateKeyIDCallCount, 1)
        XCTAssertEqual(provider.attestCallCount, 1)
        XCTAssertEqual(provider.lastAttestKeyID, "generated-key-1")

        XCTAssertEqual(tokenStore.loadKeyID(), "generated-key-1")
        let stored = try XCTUnwrap(tokenStore.loadToken())
        XCTAssertEqual(stored.token, "dtok-1")
        XCTAssertEqual(stored.expiresAt, expectedDate("2026-07-24T12:00:00Z"))
    }

    func test_enroll_clientDataHash_equalsSHA256OfRawChallengeBytes() async throws {
        let transport = MockTransport()
        let challengeB64 = "Y2hhbGxlbmdl" // base64("challenge")
        enqueueChallenge(transport, challenge: challengeB64)
        enqueueDeviceToken(transport, token: "dtok-1", expiresAt: "2026-07-24T12:00:00Z")

        let provider = MockAttestationProvider()
        let service = EnrollmentService(apiClient: makeClient(transport: transport), provider: provider, tokenStore: InMemoryTokenStore())

        try await service.enroll()

        let challengeBytes = try XCTUnwrap(Data(base64Encoded: challengeB64))
        let expectedHash = Data(SHA256.hash(data: challengeBytes))
        XCTAssertEqual(provider.lastAttestClientDataHash, expectedHash)
    }

    func test_enroll_sendsVerify_withAttestationAndChallengeFromProvider() async throws {
        let transport = MockTransport()
        let challengeB64 = "Y2hhbGxlbmdl"
        enqueueChallenge(transport, challenge: challengeB64)
        enqueueDeviceToken(transport, token: "dtok-1", expiresAt: "2026-07-24T12:00:00Z")

        let provider = MockAttestationProvider()
        provider.stubKeyID = "generated-key-1"
        provider.stubAttestation = Data("canned-attestation-blob".utf8)
        let service = EnrollmentService(apiClient: makeClient(transport: transport), provider: provider, tokenStore: InMemoryTokenStore())

        try await service.enroll()

        let verifyRequest = try XCTUnwrap(transport.recordedRequests.last)
        XCTAssertEqual(verifyRequest.url?.path, "/api/v1/attest/verify")
        let data = try XCTUnwrap(verifyRequest.httpBody)
        let body = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
        XCTAssertEqual(body["key_id"] as? String, "generated-key-1")
        XCTAssertEqual(body["attestation"] as? String, provider.stubAttestation.base64EncodedString())
        XCTAssertEqual(body["challenge"] as? String, challengeB64)
    }

    // MARK: - enroll() keyID reuse

    func test_enroll_reusesStoredKeyID_doesNotGenerateNewKey() async throws {
        let transport = MockTransport()
        let challengeB64 = "Y2hhbGxlbmdl"
        enqueueChallenge(transport, challenge: challengeB64)
        enqueueDeviceToken(transport, token: "dtok-2", expiresAt: "2026-07-24T12:00:00Z")

        let provider = MockAttestationProvider()
        let tokenStore = InMemoryTokenStore()
        tokenStore.saveKeyID("already-stored-key")
        let service = EnrollmentService(apiClient: makeClient(transport: transport), provider: provider, tokenStore: tokenStore)

        try await service.enroll()

        XCTAssertEqual(provider.generateKeyIDCallCount, 0)
        XCTAssertEqual(provider.lastAttestKeyID, "already-stored-key")
        XCTAssertEqual(tokenStore.loadKeyID(), "already-stored-key")
    }

    // MARK: - enroll() after clearing enrollment state

    /// Regression test for the keyID double-persistence defect: the real
    /// providers (`DeviceAttestationProvider`, `SimulatorAttestationProvider`)
    /// used to cache their own generated keyID in `UserDefaults`, independent
    /// of `TokenStore`. `TokenStore.clear()` -- the API defined precisely to
    /// reset enrollment -- only cleared the Keychain-backed store, not that
    /// second cache. A subsequent `enroll()` would see `loadKeyID() == nil`,
    /// call `provider.generateKeyID()`, and silently get back the exact same
    /// stale keyID from the provider's own cache instead of a genuinely new
    /// one -- defeating `clear()`.
    ///
    /// This exercises the real `SimulatorAttestationProvider` (not the dumb
    /// mock, which never had this bug) so it actually reproduces the defect:
    /// pre-fix, `secondKeyID == firstKeyID` and the assertion below fails;
    /// post-fix, the provider no longer persists anything, so every
    /// `generateKeyID()` call produces a fresh 32-byte random value.
    func test_enroll_afterClear_generatesGenuinelyNewKeyID_withRealProvider() async throws {
        let transport = MockTransport()
        let challengeB64 = "Y2hhbGxlbmdl"
        enqueueChallenge(transport, challenge: challengeB64, requestID: "req-c1")
        enqueueDeviceToken(transport, token: "dtok-1", expiresAt: "2026-07-24T12:00:00Z", requestID: "req-t1")
        enqueueChallenge(transport, challenge: challengeB64, requestID: "req-c2")
        enqueueDeviceToken(transport, token: "dtok-2", expiresAt: "2026-07-25T12:00:00Z", requestID: "req-t2")

        let provider = SimulatorAttestationProvider()
        let tokenStore = InMemoryTokenStore()
        let service = EnrollmentService(apiClient: makeClient(transport: transport), provider: provider, tokenStore: tokenStore)

        try await service.enroll()
        let firstKeyID = try XCTUnwrap(tokenStore.loadKeyID())

        tokenStore.clear() // e.g. logout / reset-enrollment flow

        try await service.enroll()
        let secondKeyID = try XCTUnwrap(tokenStore.loadKeyID())

        XCTAssertNotEqual(firstKeyID, secondKeyID, "clearing TokenStore must force a genuinely new key, not a stale keyID from an independent provider-side cache")
    }

    // MARK: - refresh()

    func test_refresh_producesNewToken_viaAssert_persistsToken() async throws {
        let transport = MockTransport()
        let challengeB64 = "cmVmcmVzaC1jaGFsbGVuZ2U=" // base64("refresh-challenge")
        enqueueChallenge(transport, challenge: challengeB64)
        enqueueDeviceToken(transport, token: "refreshed-token", expiresAt: "2026-07-25T00:00:00Z")

        let provider = MockAttestationProvider()
        let tokenStore = InMemoryTokenStore()
        tokenStore.saveKeyID("existing-key")
        tokenStore.saveToken("old-token", expiresAt: Date(timeIntervalSince1970: 0))
        let service = EnrollmentService(apiClient: makeClient(transport: transport), provider: provider, tokenStore: tokenStore)

        try await service.refresh()

        XCTAssertEqual(provider.assertCallCount, 1)
        XCTAssertEqual(provider.attestCallCount, 0)
        XCTAssertEqual(provider.lastAssertKeyID, "existing-key")
        let stored = try XCTUnwrap(tokenStore.loadToken())
        XCTAssertEqual(stored.token, "refreshed-token")
        XCTAssertEqual(stored.expiresAt, expectedDate("2026-07-25T00:00:00Z"))
        // keyID untouched -- refresh doesn't re-attest or re-save it.
        XCTAssertEqual(tokenStore.loadKeyID(), "existing-key")
    }

    func test_refresh_clientDataHash_equalsSHA256OfRawChallengeBytes() async throws {
        let transport = MockTransport()
        let challengeB64 = "cmVmcmVzaC1jaGFsbGVuZ2U="
        enqueueChallenge(transport, challenge: challengeB64)
        enqueueDeviceToken(transport, token: "refreshed-token", expiresAt: "2026-07-25T00:00:00Z")

        let provider = MockAttestationProvider()
        let tokenStore = InMemoryTokenStore()
        tokenStore.saveKeyID("existing-key")
        let service = EnrollmentService(apiClient: makeClient(transport: transport), provider: provider, tokenStore: tokenStore)

        try await service.refresh()

        let challengeBytes = try XCTUnwrap(Data(base64Encoded: challengeB64))
        let expectedHash = Data(SHA256.hash(data: challengeBytes))
        XCTAssertEqual(provider.lastAssertClientDataHash, expectedHash)
    }

    func test_refresh_withNoStoredKeyID_throwsNotEnrolled() async throws {
        let transport = MockTransport()
        let provider = MockAttestationProvider()
        let tokenStore = InMemoryTokenStore()
        let service = EnrollmentService(apiClient: makeClient(transport: transport), provider: provider, tokenStore: tokenStore)

        do {
            try await service.refresh()
            XCTFail("expected EnrollmentError.notEnrolled")
        } catch EnrollmentError.notEnrolled {
            // expected
        }
        XCTAssertEqual(transport.recordedRequests.count, 0)
    }

    // MARK: - validToken()

    func test_validToken_returnsCachedToken_whenUnexpired() async throws {
        let transport = MockTransport() // nothing enqueued -- must not be called
        let provider = MockAttestationProvider()
        let tokenStore = InMemoryTokenStore()
        tokenStore.saveKeyID("existing-key")
        tokenStore.saveToken("cached-token", expiresAt: Date().addingTimeInterval(3600))
        let service = EnrollmentService(apiClient: makeClient(transport: transport), provider: provider, tokenStore: tokenStore)

        let token = try await service.validToken()

        XCTAssertEqual(token, "cached-token")
        XCTAssertEqual(transport.recordedRequests.count, 0)
        XCTAssertEqual(provider.generateKeyIDCallCount, 0)
        XCTAssertEqual(provider.assertCallCount, 0)
    }

    func test_validToken_withinSkewMargin_treatsAsExpired_triggersRefresh() async throws {
        let transport = MockTransport()
        let challengeB64 = "Y2hhbGxlbmdl"
        enqueueChallenge(transport, challenge: challengeB64)
        enqueueDeviceToken(transport, token: "refreshed-token", expiresAt: "2026-07-25T00:00:00Z")

        let provider = MockAttestationProvider()
        let tokenStore = InMemoryTokenStore()
        tokenStore.saveKeyID("existing-key")
        // expires in 5s -- inside the default 30s skew margin.
        tokenStore.saveToken("about-to-expire", expiresAt: Date().addingTimeInterval(5))
        let service = EnrollmentService(apiClient: makeClient(transport: transport), provider: provider, tokenStore: tokenStore)

        let token = try await service.validToken()

        XCTAssertEqual(token, "refreshed-token")
        XCTAssertEqual(provider.assertCallCount, 1)
    }

    func test_validToken_triggersRefresh_whenExpired() async throws {
        let transport = MockTransport()
        let challengeB64 = "Y2hhbGxlbmdl"
        enqueueChallenge(transport, challenge: challengeB64)
        enqueueDeviceToken(transport, token: "refreshed-token", expiresAt: "2026-07-25T00:00:00Z")

        let provider = MockAttestationProvider()
        let tokenStore = InMemoryTokenStore()
        tokenStore.saveKeyID("existing-key")
        tokenStore.saveToken("expired-token", expiresAt: Date(timeIntervalSince1970: 0))
        let service = EnrollmentService(apiClient: makeClient(transport: transport), provider: provider, tokenStore: tokenStore)

        let token = try await service.validToken()

        XCTAssertEqual(token, "refreshed-token")
        XCTAssertEqual(provider.generateKeyIDCallCount, 0)
        XCTAssertEqual(provider.assertCallCount, 1)
        XCTAssertEqual(provider.attestCallCount, 0)
    }

    func test_validToken_triggersEnroll_whenNeverEnrolled() async throws {
        let transport = MockTransport()
        let challengeB64 = "Y2hhbGxlbmdl"
        enqueueChallenge(transport, challenge: challengeB64)
        enqueueDeviceToken(transport, token: "enrolled-token", expiresAt: "2026-07-25T00:00:00Z")

        let provider = MockAttestationProvider()
        provider.stubKeyID = "fresh-key"
        let tokenStore = InMemoryTokenStore()
        let service = EnrollmentService(apiClient: makeClient(transport: transport), provider: provider, tokenStore: tokenStore)

        let token = try await service.validToken()

        XCTAssertEqual(token, "enrolled-token")
        XCTAssertEqual(provider.generateKeyIDCallCount, 1)
        XCTAssertEqual(provider.attestCallCount, 1)
        XCTAssertEqual(provider.assertCallCount, 0)
        XCTAssertEqual(tokenStore.loadKeyID(), "fresh-key")
    }

    // MARK: - Error propagation

    func test_enroll_verifyFailure_propagatesError_persistsNothing() async throws {
        let transport = MockTransport()
        enqueueChallenge(transport, challenge: "Y2hhbGxlbmdl")
        enqueueError(transport, status: 400, code: "bad_request", message: "attestation rejected", field: "attestation")

        let provider = MockAttestationProvider()
        let tokenStore = InMemoryTokenStore()
        let service = EnrollmentService(apiClient: makeClient(transport: transport), provider: provider, tokenStore: tokenStore)

        do {
            try await service.enroll()
            XCTFail("expected error to be thrown")
        } catch APIClientError.api(let code, _, _, _) {
            XCTAssertEqual(code, "bad_request")
        }

        XCTAssertNil(tokenStore.loadKeyID())
        XCTAssertNil(tokenStore.loadToken())
        XCTAssertEqual(tokenStore.saveKeyIDCallCount, 0)
        XCTAssertEqual(tokenStore.saveTokenCallCount, 0)
    }

    func test_enroll_attestFailure_propagatesError_persistsNothing() async throws {
        let transport = MockTransport()
        enqueueChallenge(transport, challenge: "Y2hhbGxlbmdl")

        let provider = MockAttestationProvider()
        provider.attestError = MockAttestationError(message: "attestation hardware failure")
        let tokenStore = InMemoryTokenStore()
        let service = EnrollmentService(apiClient: makeClient(transport: transport), provider: provider, tokenStore: tokenStore)

        do {
            try await service.enroll()
            XCTFail("expected error to be thrown")
        } catch let error as MockAttestationError {
            XCTAssertEqual(error.message, "attestation hardware failure")
        }

        XCTAssertNil(tokenStore.loadKeyID())
        XCTAssertNil(tokenStore.loadToken())
    }

    func test_refresh_assertFailure_propagates_doesNotOverwriteExistingToken() async throws {
        let transport = MockTransport()
        enqueueChallenge(transport, challenge: "Y2hhbGxlbmdl")
        enqueueError(transport, status: 401, code: "unauthorized", message: "assertion invalid")

        let provider = MockAttestationProvider()
        let tokenStore = InMemoryTokenStore()
        tokenStore.saveKeyID("existing-key")
        let oldExpiry = Date(timeIntervalSince1970: 1_800_000_000)
        tokenStore.saveToken("old-token", expiresAt: oldExpiry)
        let service = EnrollmentService(apiClient: makeClient(transport: transport), provider: provider, tokenStore: tokenStore)

        do {
            try await service.refresh()
            XCTFail("expected error to be thrown")
        } catch APIClientError.api(let code, _, _, _) {
            XCTAssertEqual(code, "unauthorized")
        }

        let stored = try XCTUnwrap(tokenStore.loadToken())
        XCTAssertEqual(stored.token, "old-token")
        XCTAssertEqual(stored.expiresAt, oldExpiry)
        XCTAssertEqual(tokenStore.saveTokenCallCount, 1) // only the initial setup save
    }

    // MARK: - expiresAt (RFC3339) parsing

    func test_enroll_parsesRFC3339ExpiresAt_correctly() async throws {
        let transport = MockTransport()
        enqueueChallenge(transport, challenge: "Y2hhbGxlbmdl")
        enqueueDeviceToken(transport, token: "dtok", expiresAt: "2026-12-31T23:59:59Z")

        let tokenStore = InMemoryTokenStore()
        let service = EnrollmentService(
            apiClient: makeClient(transport: transport),
            provider: MockAttestationProvider(),
            tokenStore: tokenStore
        )

        try await service.enroll()

        let stored = try XCTUnwrap(tokenStore.loadToken())
        XCTAssertEqual(stored.expiresAt, expectedDate("2026-12-31T23:59:59Z"))
    }

    func test_refresh_malformedExpiresAt_throwsAndDoesNotPersist() async throws {
        let transport = MockTransport()
        enqueueChallenge(transport, challenge: "Y2hhbGxlbmdl")
        enqueueDeviceToken(transport, token: "new-token", expiresAt: "not-a-date")

        let tokenStore = InMemoryTokenStore()
        tokenStore.saveKeyID("existing-key")
        tokenStore.saveToken("old-token", expiresAt: Date(timeIntervalSince1970: 1_800_000_000))
        let service = EnrollmentService(apiClient: makeClient(transport: transport), provider: MockAttestationProvider(), tokenStore: tokenStore)

        do {
            try await service.refresh()
            XCTFail("expected EnrollmentError.malformedExpiry")
        } catch EnrollmentError.malformedExpiry(let raw) {
            XCTAssertEqual(raw, "not-a-date")
        }

        XCTAssertEqual(tokenStore.loadToken()?.token, "old-token")
        XCTAssertEqual(tokenStore.saveTokenCallCount, 1)
    }
}
