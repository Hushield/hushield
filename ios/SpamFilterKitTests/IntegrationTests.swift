import XCTest
@testable import SpamFilterKit

/// Live end-to-end test against a REAL Go backend running locally in
/// `ATTEST_MODE=mock` -- the real `URLSessionTransport` over real HTTP,
/// proving the wire contract (JSON shapes, envelope, auth) actually matches
/// what the hermetic `MockTransport`-based unit tests assume.
///
/// This suite is entirely separate from the hermetic unit run: it is gated
/// on the `SPAMFILTER_INTEGRATION_BASE_URL` environment variable and SKIPS
/// (via `XCTSkip`, never fails) whenever that variable is unset or the
/// server it points at isn't reachable. See `INTEGRATION.md` in this
/// directory for exact run instructions -- note that reaching this process
/// running inside the Simulator requires exporting `TEST_RUNNER_<key>`
/// (not a plain `xcodebuild` command-line prefix).
final class IntegrationTests: XCTestCase {
    private static let baseURLEnvKey = "SPAMFILTER_INTEGRATION_BASE_URL"

    private var baseURL: URL!

    override func setUp() async throws {
        try await super.setUp()

        guard let raw = ProcessInfo.processInfo.environment[Self.baseURLEnvKey], !raw.isEmpty else {
            throw XCTSkip("\(Self.baseURLEnvKey) is not set -- skipping live integration suite. See INTEGRATION.md to run it.")
        }
        guard let url = URL(string: raw) else {
            throw XCTSkip("\(Self.baseURLEnvKey)=\(raw) is not a valid URL -- skipping live integration suite.")
        }
        baseURL = url

        guard await Self.isReachable(url) else {
            throw XCTSkip("Server at \(url) is not reachable (GET /healthz failed) -- skipping live integration suite.")
        }
    }

    /// Quick liveness probe so an env var pointed at a server that isn't
    /// actually up yet (or already torn down) skips cleanly instead of
    /// failing every assertion in the flow below.
    private static func isReachable(_ baseURL: URL) async -> Bool {
        var request = URLRequest(url: baseURL.appendingPathComponent("healthz"))
        request.timeoutInterval = 3
        do {
            let (_, response) = try await URLSession.shared.data(for: request)
            guard let http = response as? HTTPURLResponse else { return false }
            return (200..<300).contains(http.statusCode)
        } catch {
            return false
        }
    }

    /// A NANP number in the `555-01xx`-style fictional test range, with a
    /// random suffix so repeated runs against the same database don't pile
    /// up reports on one number.
    private static func randomTestNumber() -> String {
        "+1415555" + String(format: "%04d", Int.random(in: 0...9999))
    }

    // MARK: - enroll -> refresh -> report -> lookup -> blocklist sync

    func test_liveBackend_enrollRefreshReportLookupBlocklistSync_endToEnd() async throws {
        let transport = URLSessionTransport()
        let apiClient = APIClient(transport: transport, baseURL: baseURL)
        let tokenStore = InMemoryTokenStore()
        let enrollment = EnrollmentService(
            apiClient: apiClient,
            provider: SimulatorAttestationProvider(),
            tokenStore: tokenStore
        )

        // 1. Enroll: the mock backend accepts SimulatorAttestationProvider's
        // dummy attestation and mints a real device token.
        try await enrollment.enroll()
        let afterEnroll = try XCTUnwrap(tokenStore.loadToken(), "enroll() must store a device token")
        XCTAssertFalse(afterEnroll.token.isEmpty)
        XCTAssertNotNil(tokenStore.loadKeyID())

        // 2. Refresh: exercises the assert path (no re-attestation) and
        // stores a new token. `saveTokenCallCount` (rather than string
        // inequality) proves the assert round trip actually ran a second
        // time -- the two tokens can otherwise legitimately collide when a
        // test runs fast enough to mint both within the same expiry second.
        try await enrollment.refresh()
        let afterRefresh = try XCTUnwrap(tokenStore.loadToken(), "refresh() must store a device token")
        XCTAssertFalse(afterRefresh.token.isEmpty)
        XCTAssertEqual(tokenStore.saveTokenCallCount, 2, "expected one saveToken from enroll() and one from refresh()")

        let token = afterRefresh.token
        let testNumber = Self.randomTestNumber()

        // 3. Report: file a spam vote for a fresh test number.
        let reportResult = try await apiClient.report(
            number: testNumber,
            vote: "spam",
            category: "scam",
            name: "Integration Test Caller",
            token: token
        )
        XCTAssertEqual(reportResult.number, testNumber)
        XCTAssertFalse(reportResult.status.isEmpty)

        // 4. Lookup: the envelope decodes and status/action are present.
        let lookup = try await apiClient.lookup(number: testNumber, token: token)
        XCTAssertEqual(lookup.number, testNumber)
        XCTAssertFalse(lookup.status.isEmpty)
        XCTAssertFalse(lookup.action.isEmpty)

        // 5. Blocklist sync: envelope decodes (entries/count/cursor present),
        // and the returned cursor round-trips as `since` on a second call.
        let firstPage = try await apiClient.blocklist(since: nil, token: token)
        XCTAssertGreaterThanOrEqual(firstPage.count, 0)
        XCTAssertEqual(firstPage.entries.count, firstPage.count)

        let secondPage = try await apiClient.blocklist(since: firstPage.cursor, token: token)
        XCTAssertGreaterThanOrEqual(secondPage.count, 0)
        XCTAssertEqual(secondPage.entries.count, secondPage.count)
    }
}
