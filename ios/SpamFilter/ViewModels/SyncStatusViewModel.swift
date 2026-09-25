import Foundation
import Observation
import SpamFilterKit

/// Drives the Status screen: reflects local enrollment + blocklist counts +
/// last sync time from `StatusReading`, and runs a manual sync via `Syncing`.
@MainActor
@Observable
final class SyncStatusViewModel {
    enum Phase: Equatable {
        case idle
        case syncing
        case failed(message: String)
    }

    var enrolled: Bool = false
    var blockedCount: Int = 0
    var labeledCount: Int = 0
    var lastSyncedAt: Date?
    var phase: Phase = .idle

    /// Latest progress from an in-flight sync, nil when none is running or
    /// before the first page lands.
    var progress: SyncProgress?

    var isSyncing: Bool { phase == .syncing }

    /// "41%", or nil until a page reports a total. A first sync pages through
    /// the whole blocklist, so the percentage is the difference between a
    /// progress bar and an apparent hang.
    var progressPercentText: String? {
        guard let fraction = progress?.fraction else { return nil }
        return "\(Int(fraction * 100))%"
    }

    /// "302,400 of 732,379 numbers", degrading to a bare count when the
    /// server supplied no total.
    var progressDetailText: String? {
        guard let progress else { return nil }
        let applied = Self.grouped(progress.applied)
        guard progress.total > 0 else { return "\(applied) numbers" }
        return "\(applied) of \(Self.grouped(progress.total)) numbers"
    }

    private static let numberFormatter: NumberFormatter = {
        let formatter = NumberFormatter()
        formatter.numberStyle = .decimal
        return formatter
    }()

    private static func grouped(_ value: Int) -> String {
        numberFormatter.string(from: NSNumber(value: value)) ?? "\(value)"
    }

    private let syncer: Syncing
    private let status: StatusReading

    init(syncer: Syncing, status: StatusReading) {
        self.syncer = syncer
        self.status = status
        refresh()
    }

    func refresh() {
        enrolled = status.isEnrolled()
        let counts = status.counts()
        blockedCount = counts.blocked
        labeledCount = counts.labeled
        lastSyncedAt = status.lastSyncedAt()
    }

    func syncNow() async {
        phase = .syncing
        // Cleared here rather than after the run. The handler hands updates
        // over via a MainActor task, so the last page's update can land after
        // syncNow() returns -- clearing on the way out would race it and
        // sometimes lose, leaving a value set with no sync running. Clearing on
        // the way in is race-free, and the Status screen only renders progress
        // while isSyncing, so a leftover value is never shown.
        progress = nil

        // Refresh on BOTH paths. A sync is not all-or-nothing: SyncService
        // enrolls, fetches the blocklist, and saves it before asking CallKit to
        // reload the Call Directory. That reload fails with
        // `calldirectorymanager error 6` (extensionDisabled) until the user
        // switches the extension on in Settings -- so on first run the sync
        // reliably throws *after* enrollment and the blocklist have committed.
        // Refreshing only on success made the Status screen report "Not enrolled
        // yet" to a device that was already enrolled.
        defer { refresh() }

        do {
            // The handler fires off the main actor (it is called wherever the
            // page completed), so hop before touching observable state.
            try await syncer.sync(onProgress: { [weak self] update in
                Task { @MainActor in self?.progress = update }
            })
            phase = .idle
        } catch {
            phase = .failed(message: ServiceErrorText.message(for: error))
        }
    }
}
