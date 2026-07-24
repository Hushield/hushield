import Foundation
import SpamFilterKit

/// Small protocols the view-models depend on, so tests can inject fakes and
/// never touch the real network / keychain / App Group. The real
/// SpamFilterKit-backed adapters live in `AppEnvironment`.

/// Submits a community report for a number. Satisfied by `ReportAdapter`.
protocol Reporting {
    func report(number: String, vote: String?, category: String?, name: String?) async throws -> ReportResultData
}

/// Looks up a number's community reputation. Satisfied by `LookupAdapter`.
protocol NumberLookup {
    func lookup(number: String) async throws -> NumberLookupData
}

/// Runs a full blocklist sync. Satisfied by `SyncAdapter`.
protocol Syncing {
    func sync() async throws
}

/// Read-only view of local enrollment + blocklist status. Satisfied by
/// `StatusReader`.
protocol StatusReading {
    func isEnrolled() -> Bool
    func counts() -> (blocked: Int, labeled: Int)
    func lastSyncedAt() -> Date?
}

/// Turns the various error types the Kit throws into one human-readable
/// sentence for inline display. Kept in one place so every screen speaks the
/// same language.
enum ServiceErrorText {
    static func message(for error: Error) -> String {
        if let apiError = error as? APIClientError {
            switch apiError {
            case let .api(_, message, _, _):
                return message
            case .decodingFailed:
                return "The server sent a response we couldn't read. Try again."
            case let .invalidRequest(description):
                return description
            }
        }
        if let enrollmentError = error as? EnrollmentError {
            switch enrollmentError {
            case .notEnrolled:
                return "This device isn't enrolled yet. Try again in a moment."
            case .malformedExpiry:
                return "The server sent an invalid token expiry."
            }
        }
        if let factoryError = error as? BlocklistStore.FactoryError {
            switch factoryError {
            case .appGroupContainerUnavailable:
                return "Shared storage is unavailable. Check the app's App Group entitlement."
            }
        }
        let nsError = error as NSError
        if nsError.domain == NSURLErrorDomain {
            return "Couldn't reach the server. Check your connection and try again."
        }
        return nsError.localizedDescription
    }
}
