import XCTest
@testable import SpamFilter
import SpamFilterKit

@MainActor
final class SyncStatusViewModelTests: XCTestCase {

    func test_init_populatesFromStatusReader() {
        let status = FakeStatusReader()
        status.enrolled = true
        status.blocked = 12
        status.labeled = 4
        let synced = Date(timeIntervalSince1970: 1_700_000_000)
        status.lastSynced = synced

        let vm = SyncStatusViewModel(syncer: FakeSyncer(), status: status)

        XCTAssertTrue(vm.enrolled)
        XCTAssertEqual(vm.blockedCount, 12)
        XCTAssertEqual(vm.labeledCount, 4)
        XCTAssertEqual(vm.lastSyncedAt, synced)
    }

    func test_syncNow_success_callsSyncer_thenRefreshesCounts() async {
        let status = FakeStatusReader()
        status.blocked = 0
        let syncer = FakeSyncer()
        // Simulate the sync writing new local state that a refresh will read.
        syncer.onSync = {
            status.blocked = 7
            status.enrolled = true
        }
        let vm = SyncStatusViewModel(syncer: syncer, status: status)

        await vm.syncNow()

        XCTAssertEqual(syncer.syncCallCount, 1)
        XCTAssertEqual(vm.blockedCount, 7)
        XCTAssertTrue(vm.enrolled)
        XCTAssertEqual(vm.phase, .idle)
    }

    func test_syncNow_error_setsFailedPhaseWithMessage() async {
        let syncer = FakeSyncer()
        syncer.error = APIClientError.api(code: "internal_error", message: "sync failed", field: nil, httpStatus: 500)
        let vm = SyncStatusViewModel(syncer: syncer, status: FakeStatusReader())

        await vm.syncNow()

        XCTAssertEqual(vm.phase, .failed(message: "sync failed"))
    }

    /// Regression: a sync that fails at its LAST step must still show the work
    /// that already succeeded.
    ///
    /// `SyncService.sync()` enrolls, fetches the blocklist, and saves it to the
    /// App Group store *before* asking CallKit to reload the Call Directory. That
    /// reload throws `calldirectorymanager error 6` (extensionDisabled) for every
    /// user who has not yet switched the extension on in Settings -- which is
    /// every user on first run.
    ///
    /// Previously `refresh()` ran only on the success path, so that one late
    /// failure discarded the whole UI update and the Status screen reported
    /// "Not enrolled yet" to a device that was demonstrably enrolled.
    func test_syncNow_failureAfterPartialSuccess_stillRefreshesState() async {
        let status = FakeStatusReader()
        let syncer = FakeSyncer()

        // Enrollment and the blocklist save commit, then the CallKit reload fails.
        syncer.onSync = {
            status.enrolled = true
            status.blocked = 7
            status.labeled = 3
        }
        syncer.error = NSError(
            domain: "com.apple.CallKit.error.calldirectorymanager",
            code: 6,
            userInfo: [NSLocalizedDescriptionKey: "The operation couldn’t be completed."]
        )

        let vm = SyncStatusViewModel(syncer: syncer, status: status)
        await vm.syncNow()

        XCTAssertTrue(vm.enrolled,
                      "enrollment succeeded before the reload failed; the card must not still say 'not enrolled'")
        XCTAssertEqual(vm.blockedCount, 7, "the blocklist was saved before the failure")
        XCTAssertEqual(vm.labeledCount, 3, "the blocklist was saved before the failure")

        // The failure still has to be surfaced -- this is not about hiding it.
        guard case .failed = vm.phase else {
            return XCTFail("expected .failed phase, got \(vm.phase)")
        }
    }

    func test_syncNow_transitionsThroughSyncing() async {
        let syncer = DelayingSyncer()
        let vm = SyncStatusViewModel(syncer: syncer, status: FakeStatusReader())

        let task = Task { await vm.syncNow() }
        await syncer.started.wait()
        XCTAssertEqual(vm.phase, .syncing)
        syncer.resume()
        await task.value
        XCTAssertEqual(vm.phase, .idle)
    }
}

private final class DelayingSyncer: Syncing {
    let started = AsyncSignal()
    private let gate = AsyncSignal()

    func resume() { gate.signal() }

    func sync(onProgress: (@Sendable (SyncProgress) -> Void)?) async throws {
        started.signal()
        await gate.wait()
    }
}

extension SyncStatusViewModelTests {
    /// The Status screen reads `progress` to draw its bar, so the view model
    /// must publish what the syncer reports.
    @MainActor
    func test_syncNow_publishesProgressFromTheSyncer() async {
        let syncer = FakeSyncer()
        syncer.progressUpdates = [
            SyncProgress(applied: 1000, total: 5000),
            SyncProgress(applied: 3500, total: 5000),
        ]
        let status = FakeStatusReader()
        let vm = SyncStatusViewModel(syncer: syncer, status: status)

        await vm.syncNow()

        // The handler hands updates over through a MainActor task, so let those
        // land before asserting.
        await Task.yield()

        XCTAssertEqual(vm.progress, SyncProgress(applied: 3500, total: 5000))
        XCTAssertEqual(vm.progressPercentText, "70%")
        XCTAssertEqual(vm.progressDetailText, "3,500 of 5,000 numbers")
    }

    /// A new run must not show the previous run's bar before its first page
    /// lands.
    @MainActor
    func test_syncNow_clearsPriorProgressOnEntry() async {
        let syncer = FakeSyncer()
        syncer.progressUpdates = [SyncProgress(applied: 42, total: 100)]
        let vm = SyncStatusViewModel(syncer: syncer, status: FakeStatusReader())

        await vm.syncNow()
        await Task.yield()
        XCTAssertNotNil(vm.progress)

        // Second run reports nothing; the stale value must not survive entry.
        syncer.progressUpdates = []
        await vm.syncNow()
        await Task.yield()

        XCTAssertNil(vm.progress, "a new sync must not inherit the previous run's progress")
    }

    /// Without a server total there is no fraction, so the screen falls back to
    /// an indeterminate bar and a bare count.
    @MainActor
    func test_syncNow_withoutTotal_hasNoPercentButStillShowsACount() async {
        let syncer = FakeSyncer()
        syncer.progressUpdates = [SyncProgress(applied: 1234, total: 0)]
        let vm = SyncStatusViewModel(syncer: syncer, status: FakeStatusReader())

        await vm.syncNow()
        await Task.yield()

        XCTAssertNil(vm.progressPercentText)
        XCTAssertEqual(vm.progressDetailText, "1,234 numbers")
    }
}
