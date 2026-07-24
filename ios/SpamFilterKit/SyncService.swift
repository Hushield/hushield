import Foundation

/// Orchestrates a full blocklist sync: pages through `APIClient.blocklist`
/// from the locally stored cursor, folds each page into `BlocklistState`
/// via `applying(_:newCursor:)`, persists after every page, then asks the
/// Call Directory extension to reload once the sync completes.
///
/// Pure orchestration -- no `CallKit`/`IdentityLookup` import -- so it's
/// unit-testable with a `MockTransport`-backed `APIClient` and a
/// `CallDirectoryReloading` spy.
public final class SyncService {
    /// Bundle identifier of the Call Directory extension target, reloaded
    /// once a sync completes.
    private static let callDirectoryIdentifier = "com.brahy.spamfilter.CallDirectory"

    private let apiClient: APIClient
    private let tokenProvider: () async throws -> String
    private let store: BlocklistStore
    private let reloader: CallDirectoryReloading
    private let pageLimit: Int

    /// - Parameters:
    ///   - apiClient: talks to `GET /api/v1/blocklist`.
    ///   - tokenProvider: supplies a valid device token per page (e.g.
    ///     `EnrollmentService.validToken`).
    ///   - store: reads the starting cursor and persists state after every
    ///     page.
    ///   - reloader: asked to reload the Call Directory extension once the
    ///     sync completes.
    ///   - pageLimit: entries requested per page; injectable so tests can
    ///     exercise paging without huge fixtures.
    public init(
        apiClient: APIClient,
        tokenProvider: @escaping () async throws -> String,
        store: BlocklistStore,
        reloader: CallDirectoryReloading,
        pageLimit: Int = 500
    ) {
        self.apiClient = apiClient
        self.tokenProvider = tokenProvider
        self.store = store
        self.reloader = reloader
        self.pageLimit = pageLimit
    }

    /// Runs one full sync pass:
    /// 1. Loads the local state; its `cursor` (empty means "never synced")
    ///    seeds the first page's `since`.
    /// 2. Loops requesting pages, folding each into the state and saving
    ///    it immediately, threading each request's `since` from the prior
    ///    response's `cursor`. Keeps paging while a page came back full
    ///    (`count == pageLimit`) -- a short page means the delta is
    ///    exhausted.
    /// 3. Guards against a server that keeps returning full pages without
    ///    advancing the cursor: if a page doesn't move the cursor forward,
    ///    the loop stops rather than re-requesting the same page forever.
    /// 4. Once the loop ends, reloads the Call Directory extension so it
    ///    picks up the freshly synced data.
    public func sync() async throws {
        var state = store.load()
        var cursor = state.cursor

        while true {
            let token = try await tokenProvider()
            let sinceParam = cursor.isEmpty ? nil : cursor
            let response = try await apiClient.blocklist(since: sinceParam, prefix: nil, limit: pageLimit, token: token)

            state = state.applying(response.entries, newCursor: response.cursor)
            try store.save(state)

            let cursorAdvanced = response.cursor != cursor
            cursor = response.cursor

            guard response.count == pageLimit else { break }
            guard cursorAdvanced else { break }
        }

        try await reloader.reload(Self.callDirectoryIdentifier)
    }
}
