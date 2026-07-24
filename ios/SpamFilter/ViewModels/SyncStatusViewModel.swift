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

    var isSyncing: Bool { phase == .syncing }

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
        do {
            try await syncer.sync()
            refresh()
            phase = .idle
        } catch {
            phase = .failed(message: ServiceErrorText.message(for: error))
        }
    }
}
