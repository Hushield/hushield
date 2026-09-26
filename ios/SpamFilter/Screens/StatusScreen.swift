import SwiftUI
import SpamFilterKit

/// Enrollment + sync status, blocklist counts, and a manual "Sync now".
struct StatusScreen: View {
    @State private var model: SyncStatusViewModel

    init(syncing: Syncing, status: StatusReading) {
        _model = State(initialValue: SyncStatusViewModel(syncer: syncing, status: status))
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: Theme.Spacing.lg) {
                    enrollmentCard
                    countsRow
                    lastSyncRow

                    if model.isSyncing {
                        syncProgressCard
                    }

                    if case let .failed(message) = model.phase {
                        ErrorBanner(message: message)
                            .accessibilityIdentifier("status.syncError")
                    }

                    Button("Sync now") { sync() }
                        .buttonStyle(PrimaryButtonStyle(isBusy: model.isSyncing))
                        .disabled(model.isSyncing)
                        .accessibilityIdentifier("status.syncButton")
                }
                .padding(Theme.Spacing.md)
            }
            .background(Theme.background.ignoresSafeArea())
            .navigationTitle("Status")
            .onAppear { model.refresh() }
        }
    }

    /// Shown only while a sync runs. A first sync pages through the entire
    /// blocklist, so without this the screen looks frozen.
    private var syncProgressCard: some View {
        Card {
            VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
                HStack {
                    Text("Syncing")
                        .font(Theme.Typography.sectionTitle)
                    Spacer()
                    if let percent = model.progressPercentText {
                        Text(percent)
                            .font(Theme.Typography.body)
                            .foregroundStyle(Theme.secondaryText)
                            .contentTransition(.numericText())
                            .accessibilityIdentifier("status.syncPercent")
                    }
                }

                // Determinate once a page has reported a total; indeterminate
                // until then, and against a server that sends none at all.
                if let fraction = model.progress?.fraction {
                    ProgressView(value: fraction)
                        .tint(Theme.safe)
                } else {
                    ProgressView()
                        .progressViewStyle(.linear)
                        .tint(Theme.safe)
                }

                if let detail = model.progressDetailText {
                    Text(detail)
                        .font(Theme.Typography.body)
                        .foregroundStyle(Theme.secondaryText)
                        .accessibilityIdentifier("status.syncDetail")
                }
            }
        }
        .accessibilityIdentifier("status.syncProgress")
    }

    private var enrollmentCard: some View {
        Card {
            HStack(spacing: Theme.Spacing.md) {
                Image(systemName: model.enrolled ? "checkmark.seal.fill" : "hourglass")
                    .font(.system(size: 32))
                    .foregroundStyle(model.enrolled ? Theme.safe : Theme.warn)
                VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
                    Text(model.enrolled ? "Device enrolled" : "Not enrolled yet")
                        .font(Theme.Typography.sectionTitle)
                    Text(model.enrolled
                         ? "This device is registered and can sync the community blocklist."
                         : "Enrollment happens automatically the first time you sync or report.")
                        .font(Theme.Typography.body)
                        .foregroundStyle(Theme.secondaryText)
                }
            }
        }
    }

    private var countsRow: some View {
        HStack(spacing: Theme.Spacing.md) {
            statTile(count: model.blockedCount, label: "Blocked", color: Theme.danger, symbol: "hand.raised.fill")
            statTile(count: model.labeledCount, label: "Labeled", color: Theme.warn, symbol: "tag.fill")
        }
    }

    private func statTile(count: Int, label: String, color: Color, symbol: String) -> some View {
        Card {
            VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
                Image(systemName: symbol)
                    .foregroundStyle(color)
                Text("\(count)")
                    .font(.system(size: 34, weight: .bold, design: .rounded))
                    .contentTransition(.numericText())
                Text(label)
                    .font(Theme.Typography.body)
                    .foregroundStyle(Theme.secondaryText)
            }
        }
    }

    private var lastSyncRow: some View {
        Card {
            HStack {
                Label("Last synced", systemImage: "clock.arrow.circlepath")
                    .foregroundStyle(Theme.secondaryText)
                Spacer()
                Text(lastSyncedText)
                    .fontWeight(.medium)
                    .accessibilityIdentifier("status.lastSynced")
            }
            .font(Theme.Typography.body)
        }
    }

    private var lastSyncedText: String {
        guard let date = model.lastSyncedAt else { return "Never" }
        let formatter = RelativeDateTimeFormatter()
        formatter.unitsStyle = .full
        return formatter.localizedString(for: date, relativeTo: Date())
    }

    private func sync() {
        UIImpactFeedbackGenerator(style: .medium).impactOccurred()
        Task { await model.syncNow() }
    }
}
