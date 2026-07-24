import Foundation
import SpamFilterKit

/// The composition root. Wires the real SpamFilterKit stack once and vends the
/// small protocol-shaped adapters the view-models depend on. Constructed once
/// in `SpamFilterApp` and passed down.
final class AppEnvironment {
    let reporting: Reporting
    let lookup: NumberLookup
    let syncing: Syncing
    let statusReading: StatusReading

    init() {
        // UI-test-only branch: when launched with `-uitest` (set by
        // SpamFilterUITests), wire deterministic in-memory fakes instead of
        // the real network/keychain/App Group stack below. Production path
        // is unchanged.
        if UITestSupport.isEnabled {
            self.reporting = UITestReporting()
            self.lookup = UITestLookup()
            self.syncing = UITestSyncing()
            self.statusReading = UITestStatusReading()
            return
        }

        let baseURL = AppEnvironment.resolveBaseURL()

        let transport = URLSessionTransport()
        let apiClient = APIClient(transport: transport, baseURL: baseURL)
        let attestation = makeAttestationProvider()
        let tokenStore = KeychainTokenStore()
        let enrollment = EnrollmentService(apiClient: apiClient, provider: attestation, tokenStore: tokenStore)

        // App Group store — used for local blocklist counts + by SyncService.
        // If the container can't be resolved, the app still runs; status/sync
        // surface the failure instead of crashing.
        let store = try? BlocklistStore.makeAppGroupStore()
        let defaults = UserDefaults(suiteName: AppEnvironment.appGroupIdentifier) ?? .standard

        let syncService: SyncService?
        if let store {
            syncService = SyncService(
                apiClient: apiClient,
                tokenProvider: { try await enrollment.validToken() },
                store: store,
                reloader: CXCallDirectoryReloader()
            )
        } else {
            syncService = nil
        }

        self.reporting = ReportAdapter(apiClient: apiClient, enrollment: enrollment)
        self.lookup = LookupAdapter(apiClient: apiClient, enrollment: enrollment)
        self.syncing = SyncAdapter(syncService: syncService, defaults: defaults, store: store)
        self.statusReading = StatusReader(tokenStore: tokenStore, store: store, defaults: defaults)
    }

    static let appGroupIdentifier = "group.com.brahy.spamfilter"
    static let lastSyncedKey = "lastSyncedAt"

    /// Base URL strictly from the `SPAMFILTER_API_BASE_URL` Info.plist key
    /// (Task 1 build setting). Falls back to localhost if absent/malformed.
    private static func resolveBaseURL() -> URL {
        if let raw = Bundle.main.object(forInfoDictionaryKey: "SPAMFILTER_API_BASE_URL") as? String,
           !raw.isEmpty,
           let url = URL(string: raw) {
            return url
        }
        return URL(string: "http://localhost:8080")!
    }
}

// MARK: - Adapters (make the real Kit services satisfy the small protocols)

/// Ensures a valid device token (lazy enroll/refresh) before every report.
private struct ReportAdapter: Reporting {
    let apiClient: APIClient
    let enrollment: EnrollmentService

    func report(number: String, vote: String?, category: String?, name: String?) async throws -> ReportResultData {
        let token = try await enrollment.validToken()
        return try await apiClient.report(number: number, vote: vote, category: category, name: name, token: token)
    }
}

private struct LookupAdapter: NumberLookup {
    let apiClient: APIClient
    let enrollment: EnrollmentService

    func lookup(number: String) async throws -> NumberLookupData {
        let token = try await enrollment.validToken()
        return try await apiClient.lookup(number: number, token: token)
    }
}

/// Runs a real blocklist sync and records the completion time so the Status
/// screen can show "last synced".
private struct SyncAdapter: Syncing {
    let syncService: SyncService?
    let defaults: UserDefaults
    let store: BlocklistStore?

    func sync() async throws {
        guard let syncService else {
            throw BlocklistStore.FactoryError.appGroupContainerUnavailable(identifier: AppEnvironment.appGroupIdentifier)
        }
        try await syncService.sync()
        defaults.set(Date().timeIntervalSince1970, forKey: AppEnvironment.lastSyncedKey)
    }
}

/// Reads local enrollment + blocklist counts + last-sync time for the Status
/// screen.
private struct StatusReader: StatusReading {
    let tokenStore: TokenStore
    let store: BlocklistStore?
    let defaults: UserDefaults

    func isEnrolled() -> Bool {
        tokenStore.loadToken() != nil
    }

    func counts() -> (blocked: Int, labeled: Int) {
        guard let state = store?.load() else { return (0, 0) }
        return (state.blocked.count, state.labeled.count)
    }

    func lastSyncedAt() -> Date? {
        let raw = defaults.double(forKey: AppEnvironment.lastSyncedKey)
        return raw > 0 ? Date(timeIntervalSince1970: raw) : nil
    }
}
