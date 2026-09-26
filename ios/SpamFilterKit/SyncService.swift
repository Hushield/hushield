import Foundation

/// How far a sync has got. `total` of 0 means the server did not supply one,
/// in which case there is no meaningful fraction to show.
public struct SyncProgress: Equatable, Sendable {
    /// Entries folded into local state so far, across every page.
    public let applied: Int
    /// Servable rows the server reports having, or 0 if unknown.
    public let total: Int

    public init(applied: Int, total: Int) {
        self.applied = applied
        self.total = total
    }

    /// Completion in 0...1, or nil when no total is available.
    ///
    /// Clamped deliberately. `total` counts servable rows, but a delta also
    /// carries "unblock" tombstones that are not in that count, so a
    /// removal-heavy sync legitimately applies more entries than the total.
    /// A bar that runs past its end reads as a bug; this reads as done.
    public var fraction: Double? {
        guard total > 0 else { return nil }
        return min(1.0, Double(applied) / Double(total))
    }
}

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
    private static let callDirectoryIdentifier = "com.brahy.hushield.CallDirectory"

    private let apiClient: APIClient
    private let tokenProvider: () async throws -> String
    private let store: BlocklistStore
    private let reloader: CallDirectoryReloading
    private let pageLimit: Int
    private let pagesPerSave: Int

    /// - Parameters:
    ///   - apiClient: talks to `GET /api/v1/blocklist`.
    ///   - tokenProvider: supplies a valid device token per page (e.g.
    ///     `EnrollmentService.validToken`).
    ///   - store: reads the starting cursor and persists state after every
    ///     page.
    ///   - reloader: asked to reload the Call Directory extension once the
    ///     sync completes.
    ///   - pageLimit: entries requested per page; injectable so tests can
    ///     exercise paging without huge fixtures. 1000 is the server's
    ///     maxBlocklistLimit -- requesting less just buys more round trips.
    ///   - pagesPerSave: how many pages to fold in before writing the state
    ///     to disk. Injectable for the same reason as pageLimit.
    public init(
        apiClient: APIClient,
        tokenProvider: @escaping () async throws -> String,
        store: BlocklistStore,
        reloader: CallDirectoryReloading,
        pageLimit: Int = 1000,
        pagesPerSave: Int = 10
    ) {
        self.apiClient = apiClient
        self.tokenProvider = tokenProvider
        self.store = store
        self.reloader = reloader
        self.pageLimit = pageLimit
        self.pagesPerSave = pagesPerSave
    }

    /// Runs one full sync pass:
    /// 1. Loads the local state; its `cursor` (empty means "never synced")
    ///    seeds the first page's `since`.
    /// 2. Loops requesting pages, folding each into the state and threading
    ///    each request's `since` from the prior response's `cursor`. Keeps
    ///    paging while a page came back full (`count == pageLimit`) -- a
    ///    short page means the delta is exhausted.
    ///
    ///    State is written every `pagesPerSave` pages rather than after
    ///    every page. `BlocklistStore.save` serializes the WHOLE state
    ///    atomically, so its cost grows with the accumulated blocklist, and
    ///    saving per page makes a full sync quadratic in disk writes: at
    ///    732k entries the file is ~8.8MB, so ~1465 per-page saves wrote
    ///    ~6.4GB to flash for one sync. Batching cuts that by pagesPerSave.
    ///
    ///    Durability is unaffected: a page failure persists whatever has
    ///    been folded in so far before rethrowing, so progress still
    ///    survives a mid-sync error exactly as it did when every page was
    ///    saved.
    /// 3. Guards against a server that keeps returning full pages without
    ///    advancing the cursor: if a page doesn't move the cursor forward,
    ///    the loop stops rather than re-requesting the same page forever.
    /// 4. Once the loop ends, reloads the Call Directory extension so it
    ///    picks up the freshly synced data.
    /// - Parameter onProgress: called after each page is folded in, on
    ///   whatever context the page completed on. Optional so callers that do
    ///   not render progress -- the extensions, tests -- pay nothing.
    public func sync(onProgress: (@Sendable (SyncProgress) -> Void)? = nil) async throws {
        var state = store.load()
        var cursor = state.cursor
        var unsavedPages = 0
        var applied = 0

        func persistPendingPages() throws {
            guard unsavedPages > 0 else { return }
            try store.save(state)
            unsavedPages = 0
        }

        while true {
            let response: BlocklistData
            do {
                let token = try await tokenProvider()
                let sinceParam = cursor.isEmpty ? nil : cursor
                response = try await apiClient.blocklist(since: sinceParam, prefix: nil, limit: pageLimit, token: token)
            } catch {
                // Persist what earlier pages already produced before giving up,
                // so a mid-sync failure costs at most the pages folded in since
                // the last save instead of the whole run. A save failure here is
                // deliberately swallowed: the original network error is the one
                // worth surfacing, and a lost batch is re-fetched next sync.
                try? persistPendingPages()
                throw error
            }

            state = state.applying(response.entries, newCursor: response.cursor)
            applied += response.entries.count
            onProgress?(SyncProgress(applied: applied, total: response.total ?? 0))
            unsavedPages += 1
            if unsavedPages >= pagesPerSave {
                try persistPendingPages()
            }

            let cursorAdvanced = response.cursor != cursor
            cursor = response.cursor

            guard response.count == pageLimit else { break }
            guard cursorAdvanced else { break }
        }

        try persistPendingPages()

        try await reloader.reload(Self.callDirectoryIdentifier)
    }
}
