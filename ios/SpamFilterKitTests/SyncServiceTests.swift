import XCTest
@testable import SpamFilterKit

final class SyncServiceTests: XCTestCase {
    private let baseURL = URL(string: "https://api.example.test")!
    private let callDirectoryIdentifier = "com.brahy.spamfilter.CallDirectory"
    private var tempDirectory: URL!

    override func setUp() {
        super.setUp()
        tempDirectory = FileManager.default.temporaryDirectory
            .appendingPathComponent("SyncServiceTests-\(UUID().uuidString)")
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: tempDirectory)
        tempDirectory = nil
        super.tearDown()
    }

    private func makeClient(transport: MockTransport) -> APIClient {
        APIClient(transport: transport, baseURL: baseURL)
    }

    private func entryJSON(number: String, action: String, name: String?) -> String {
        let nameJSON = name.map { "\"\($0)\"" } ?? "null"
        return """
        {"number":"\(number)","action":"\(action)","name":\(nameJSON),"spoof_suspected":false}
        """
    }

    private func enqueueBlocklistPage(
        _ transport: MockTransport,
        entries: [(number: String, action: String, name: String?)],
        count: Int,
        cursor: String,
        requestID: String = "req-b"
    ) {
        let entriesJSON = entries.map { entryJSON(number: $0.number, action: $0.action, name: $0.name) }.joined(separator: ",")
        transport.enqueue((jsonData("""
        {"success":true,"data":{"entries":[\(entriesJSON)],"count":\(count),"cursor":"\(cursor)"},"errors":[],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"\(requestID)"}}
        """), makeHTTPResponse(url: baseURL, status: 200)))
    }

    private func enqueueError(_ transport: MockTransport, status: Int = 500, code: String = "internal_error", message: String = "boom") {
        transport.enqueue((jsonData("""
        {"success":false,"data":null,"errors":[{"field":"","message":"\(message)","code":"\(code)"}],"meta":{"timestamp":"2026-07-23T12:00:00Z","request_id":"req-e"}}
        """), makeHTTPResponse(url: baseURL, status: status)))
    }

    private func sinceQueryValue(_ request: URLRequest) -> String? {
        guard let url = request.url, let components = URLComponents(url: url, resolvingAgainstBaseURL: false) else { return nil }
        return components.queryItems?.first(where: { $0.name == "since" })?.value
    }

    private func makeService(
        transport: MockTransport,
        store: BlocklistStore,
        reloader: SpyCallDirectoryReloader,
        pageLimit: Int = 2,
        token: @escaping () async throws -> String = { "test-token" }
    ) -> SyncService {
        SyncService(
            apiClient: makeClient(transport: transport),
            tokenProvider: token,
            store: store,
            reloader: reloader,
            pageLimit: pageLimit
        )
    }

    // MARK: - Single short page: one request, saves final cursor, reloads once

    func test_sync_singleShortPage_makesOneRequest_savesState_reloadsOnce() async throws {
        let transport = MockTransport()
        enqueueBlocklistPage(transport, entries: [(number: "+14155551111", action: "block", name: "Robocaller")], count: 1, cursor: "100.5")

        let store = BlocklistStore(directory: tempDirectory)
        let reloader = SpyCallDirectoryReloader()
        let service = makeService(transport: transport, store: store, reloader: reloader, pageLimit: 2)

        try await service.sync()

        XCTAssertEqual(transport.recordedRequests.count, 1)
        let final = store.load()
        XCTAssertEqual(final.blocked, [14_155_551_111])
        XCTAssertEqual(final.cursor, "100.5")
        XCTAssertEqual(reloader.reloadedIdentifiers, [callDirectoryIdentifier])
    }

    // MARK: - Fresh sync (empty cursor) omits the `since` query param on the first request

    func test_sync_freshState_omitsSinceParam_onFirstRequest() async throws {
        let transport = MockTransport()
        enqueueBlocklistPage(transport, entries: [], count: 0, cursor: "100.1")

        let store = BlocklistStore(directory: tempDirectory)
        let reloader = SpyCallDirectoryReloader()
        let service = makeService(transport: transport, store: store, reloader: reloader, pageLimit: 500)

        try await service.sync()

        let request = try XCTUnwrap(transport.recordedRequests.first)
        XCTAssertNil(sinceQueryValue(request))
    }

    // MARK: - Resumed sync sends the stored cursor as `since` on the first request

    func test_sync_resumedState_sendsStoredCursorAsSince_onFirstRequest() async throws {
        let store = BlocklistStore(directory: tempDirectory)
        try store.save(BlocklistState(blocked: [1], labeled: [], names: [:], cursor: "50.1"))

        let transport = MockTransport()
        enqueueBlocklistPage(transport, entries: [], count: 0, cursor: "50.1")
        let reloader = SpyCallDirectoryReloader()
        let service = makeService(transport: transport, store: store, reloader: reloader, pageLimit: 500)

        try await service.sync()

        let request = try XCTUnwrap(transport.recordedRequests.first)
        XCTAssertEqual(sinceQueryValue(request), "50.1")
    }

    // MARK: - Paging: keeps requesting while count == limit, stops on a short page

    func test_sync_multiPage_pagesWhileCountEqualsLimit_stopsOnShortPage() async throws {
        let transport = MockTransport()
        enqueueBlocklistPage(transport, entries: [
            (number: "+14155551111", action: "block", name: nil),
            (number: "+14155552222", action: "block", name: nil),
        ], count: 2, cursor: "100.2")
        enqueueBlocklistPage(transport, entries: [
            (number: "+14155553333", action: "label", name: "Suspect"),
        ], count: 1, cursor: "100.3")

        let store = BlocklistStore(directory: tempDirectory)
        let reloader = SpyCallDirectoryReloader()
        let service = makeService(transport: transport, store: store, reloader: reloader, pageLimit: 2)

        try await service.sync()

        XCTAssertEqual(transport.recordedRequests.count, 2)
        let final = store.load()
        XCTAssertEqual(final.blocked, [14_155_551_111, 14_155_552_222])
        XCTAssertEqual(final.labeled, [14_155_553_333])
        XCTAssertEqual(final.cursor, "100.3")
        XCTAssertEqual(reloader.reloadedIdentifiers, [callDirectoryIdentifier])
    }

    // MARK: - Cursor threading: each request's `since` is the prior response's cursor

    func test_sync_cursorThreading_eachRequestSinceEqualsPriorResponseCursor() async throws {
        let transport = MockTransport()
        enqueueBlocklistPage(transport, entries: [
            (number: "+14155551111", action: "block", name: nil),
            (number: "+14155552222", action: "block", name: nil),
        ], count: 2, cursor: "100.2")
        enqueueBlocklistPage(transport, entries: [
            (number: "+14155553333", action: "block", name: nil),
            (number: "+14155554444", action: "block", name: nil),
        ], count: 2, cursor: "100.4")
        enqueueBlocklistPage(transport, entries: [
            (number: "+14155555555", action: "block", name: nil),
        ], count: 1, cursor: "100.5")

        let store = BlocklistStore(directory: tempDirectory)
        let reloader = SpyCallDirectoryReloader()
        let service = makeService(transport: transport, store: store, reloader: reloader, pageLimit: 2)

        try await service.sync()

        XCTAssertEqual(transport.recordedRequests.count, 3)
        XCTAssertNil(sinceQueryValue(transport.recordedRequests[0]))
        XCTAssertEqual(sinceQueryValue(transport.recordedRequests[1]), "100.2")
        XCTAssertEqual(sinceQueryValue(transport.recordedRequests[2]), "100.4")
        XCTAssertEqual(store.load().cursor, "100.5")
    }

    // MARK: - Anti-infinite-loop guard: a full page with a non-advancing cursor stops the loop

    func test_sync_cursorNotAdvancing_onFullPage_stopsLoop_doesNotHang() async throws {
        let transport = MockTransport()
        // First page: full page, cursor advances from "" -> "100.2" (progress).
        enqueueBlocklistPage(transport, entries: [
            (number: "+14155551111", action: "block", name: nil),
            (number: "+14155552222", action: "block", name: nil),
        ], count: 2, cursor: "100.2")
        // Second page: still a full page (count == limit), but the server
        // keeps echoing the same cursor back -- must stop here, not loop
        // forever re-requesting "100.2".
        enqueueBlocklistPage(transport, entries: [
            (number: "+14155553333", action: "block", name: nil),
            (number: "+14155554444", action: "block", name: nil),
        ], count: 2, cursor: "100.2")

        let store = BlocklistStore(directory: tempDirectory)
        let reloader = SpyCallDirectoryReloader()
        let service = makeService(transport: transport, store: store, reloader: reloader, pageLimit: 2)

        try await service.sync()

        // Exactly 2 requests -- the guard stops it from re-requesting "100.2" forever.
        XCTAssertEqual(transport.recordedRequests.count, 2)
        XCTAssertEqual(sinceQueryValue(transport.recordedRequests[1]), "100.2")
        XCTAssertEqual(store.load().cursor, "100.2")
        XCTAssertEqual(reloader.reloadedIdentifiers, [callDirectoryIdentifier])
    }

    // MARK: - Error propagation: a failing page rethrows, does not save further, does not reload

    func test_sync_pageFailure_rethrows_doesNotReload_priorPageStillPersisted() async throws {
        let transport = MockTransport()
        enqueueBlocklistPage(transport, entries: [
            (number: "+14155551111", action: "block", name: nil),
            (number: "+14155552222", action: "block", name: nil),
        ], count: 2, cursor: "100.2")
        enqueueError(transport)

        let store = BlocklistStore(directory: tempDirectory)
        let reloader = SpyCallDirectoryReloader()
        let service = makeService(transport: transport, store: store, reloader: reloader, pageLimit: 2)

        do {
            try await service.sync()
            XCTFail("expected sync() to rethrow the second page's error")
        } catch APIClientError.api(let code, _, _, _) {
            XCTAssertEqual(code, "internal_error")
        }

        XCTAssertEqual(store.load().cursor, "100.2", "the first page's progress must survive a later page's failure")
        XCTAssertTrue(reloader.reloadedIdentifiers.isEmpty, "must not reload when sync() doesn't complete the loop")
    }

    // MARK: - Token provider supplies the Authorization bearer token on every request

    func test_sync_usesTokenProvider_setsAuthorizationBearerHeader_onEveryRequest() async throws {
        let transport = MockTransport()
        enqueueBlocklistPage(transport, entries: [
            (number: "+14155551111", action: "block", name: nil),
            (number: "+14155552222", action: "block", name: nil),
        ], count: 2, cursor: "100.2")
        enqueueBlocklistPage(transport, entries: [], count: 0, cursor: "100.3")

        let store = BlocklistStore(directory: tempDirectory)
        let reloader = SpyCallDirectoryReloader()
        let service = makeService(transport: transport, store: store, reloader: reloader, pageLimit: 2, token: { "device-token-xyz" })

        try await service.sync()

        XCTAssertEqual(transport.recordedRequests.count, 2)
        for request in transport.recordedRequests {
            XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer device-token-xyz")
        }
    }

    // MARK: - unblock entries applied mid-sync remove a number added by an earlier page

    func test_sync_unblockOnLaterPage_removesEarlierBlockedNumber() async throws {
        let transport = MockTransport()
        enqueueBlocklistPage(transport, entries: [
            (number: "+14155551111", action: "block", name: nil),
            (number: "+14155552222", action: "block", name: nil),
        ], count: 2, cursor: "100.2")
        enqueueBlocklistPage(transport, entries: [
            (number: "+14155551111", action: "unblock", name: nil),
        ], count: 1, cursor: "100.3")

        let store = BlocklistStore(directory: tempDirectory)
        let reloader = SpyCallDirectoryReloader()
        let service = makeService(transport: transport, store: store, reloader: reloader, pageLimit: 2)

        try await service.sync()

        let final = store.load()
        XCTAssertEqual(final.blocked, [14_155_552_222])
        XCTAssertEqual(final.cursor, "100.3")
    }
}
