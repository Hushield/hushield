import Foundation
@testable import SpamFilter
import SpamFilterKit

/// Records the args of the last `report` call and returns a canned result or
/// throws a canned error.
final class FakeReporter: Reporting {
    struct Call: Equatable {
        let number: String
        let vote: String?
        let category: String?
        let name: String?
    }

    private(set) var calls: [Call] = []
    var result: ReportResultData = ReportResultData(number: "+14155550100", status: "blocked")
    var error: Error?

    func report(number: String, vote: String?, category: String?, name: String?) async throws -> ReportResultData {
        calls.append(Call(number: number, vote: vote, category: category, name: name))
        if let error { throw error }
        return result
    }
}

final class FakeLookup: NumberLookup {
    private(set) var lookedUpNumbers: [String] = []
    var result: NumberLookupData = NumberLookupData(
        number: "+14155550100",
        status: "blocked",
        action: "block",
        category: "scam",
        name: "Reported Scammer",
        spoofSuspected: false
    )
    var error: Error?

    func lookup(number: String) async throws -> NumberLookupData {
        lookedUpNumbers.append(number)
        if let error { throw error }
        return result
    }
}

final class FakeSyncer: Syncing {
    private(set) var syncCallCount = 0
    var error: Error?
    /// Optional hook run inside `sync()` so a test can mutate the fake status
    /// reader to simulate the sync having changed local state.
    var onSync: (() -> Void)?

    func sync() async throws {
        syncCallCount += 1
        onSync?()
        if let error { throw error }
    }
}

final class FakeStatusReader: StatusReading {
    var enrolled = false
    var blocked = 0
    var labeled = 0
    var lastSynced: Date?

    func isEnrolled() -> Bool { enrolled }
    func counts() -> (blocked: Int, labeled: Int) { (blocked, labeled) }
    func lastSyncedAt() -> Date? { lastSynced }
}
