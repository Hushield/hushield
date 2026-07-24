import Foundation
import SpamFilterKit

/// UI-test-only composition support. `SpamFilterUITests` launches the app
/// with the `-uitest` argument so `AppEnvironment` wires these deterministic,
/// offline, in-memory fakes instead of the real network/keychain/App Group
/// stack (see `AppEnvironment.init()`). This file is compiled into the app
/// target but is inert unless that launch argument is present -- the
/// production path is unchanged.

enum UITestSupport {
    static var isEnabled: Bool {
        ProcessInfo.processInfo.arguments.contains("-uitest")
    }

    /// When present alongside `-uitest`, wires a sync fake that always throws
    /// instead of the always-succeeds default -- lets UI tests exercise the
    /// Status screen's failure/error-banner path deterministically.
    static var syncShouldFail: Bool {
        ProcessInfo.processInfo.arguments.contains("-uitest-syncfail")
    }
}

struct UITestSyncFailure: Error {}

/// Always reports the number as "blocked", instantly.
final class UITestReporting: Reporting {
    func report(number: String, vote: String?, category: String?, name: String?) async throws -> ReportResultData {
        ReportResultData(number: number, status: "blocked")
    }
}

/// Always returns a fixed "suspected" reputation, instantly.
final class UITestLookup: NumberLookup {
    func lookup(number: String) async throws -> NumberLookupData {
        NumberLookupData(
            number: number,
            status: "suspected",
            action: "warn",
            category: "robocall",
            name: "Test Caller",
            spoofSuspected: false
        )
    }
}

/// Completes instantly and never fails, unless `-uitest-syncfail` was passed
/// at launch, in which case it always throws (see `UITestSupport.syncShouldFail`).
final class UITestSyncing: Syncing {
    func sync() async throws {
        if UITestSupport.syncShouldFail {
            throw UITestSyncFailure()
        }
    }
}

/// Reports a fixed enrolled/counts/last-synced state.
final class UITestStatusReading: StatusReading {
    func isEnrolled() -> Bool { true }
    func counts() -> (blocked: Int, labeled: Int) { (12, 3) }
    func lastSyncedAt() -> Date? { Date(timeIntervalSince1970: 1_700_000_000) }
}
