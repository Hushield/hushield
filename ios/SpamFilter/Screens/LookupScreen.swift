import SwiftUI
import SpamFilterKit

/// Look up a number's community reputation.
struct LookupScreen: View {
    @State private var model: LookupViewModel

    init(lookup: NumberLookup) {
        _model = State(initialValue: LookupViewModel(service: lookup))
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: Theme.Spacing.lg) {
                    NumberField(title: "Phone number", text: $model.number)
                        .onChange(of: model.number) { model.validationMessage = nil }

                    if let validation = model.validationMessage {
                        Text(validation)
                            .font(.footnote)
                            .foregroundStyle(Theme.danger)
                    }

                    Button("Look up") { lookup() }
                        .buttonStyle(PrimaryButtonStyle(isBusy: model.isLoading))
                        .disabled(model.isLoading)

                    resultSection
                }
                .padding(Theme.Spacing.md)
            }
            .background(Theme.background.ignoresSafeArea())
            .navigationTitle("Lookup")
        }
    }

    @ViewBuilder
    private var resultSection: some View {
        switch model.phase {
        case .idle:
            EmptyStateView(
                systemImage: "magnifyingglass",
                title: "Check any number",
                message: "Enter a number to see how the community has rated it."
            )
        case .loading:
            ProgressView("Checking reputation\u{2026}")
                .frame(maxWidth: .infinity)
                .padding(.vertical, Theme.Spacing.xl)
        case let .loaded(data):
            reputationCard(data)
        case let .notFound(number):
            Card {
                VStack(alignment: .leading, spacing: Theme.Spacing.sm) {
                    StatusBadge(level: .unknown, text: "No reports")
                    Text(number)
                        .font(.title3.weight(.semibold))
                    Text("No community reports yet for this number. That's usually a good sign.")
                        .font(.subheadline)
                        .foregroundStyle(Theme.secondaryText)
                }
            }
        case let .failed(message):
            ErrorBanner(message: message)
        }
    }

    private func reputationCard(_ data: NumberLookupData) -> some View {
        let level = ReputationLevel(status: data.status, action: data.action)
        return Card {
            VStack(alignment: .leading, spacing: Theme.Spacing.md) {
                HStack {
                    Text(data.number)
                        .font(.title3.weight(.semibold))
                    Spacer()
                    StatusBadge(level: level, text: data.status.capitalized)
                }

                Divider()

                detailRow(label: "Recommended action", value: data.action.capitalized, symbol: "arrow.turn.down.right")
                if let category = data.category, !category.isEmpty {
                    detailRow(label: "Category", value: category.capitalized, symbol: "tag.fill")
                }
                if let name = data.name, !name.isEmpty {
                    detailRow(label: "Community name", value: name, symbol: "person.fill")
                }
                if data.spoofSuspected {
                    Label("Caller ID spoofing suspected", systemImage: "exclamationmark.shield.fill")
                        .font(.subheadline.weight(.medium))
                        .foregroundStyle(Theme.warn)
                }
            }
        }
    }

    private func detailRow(label: String, value: String, symbol: String) -> some View {
        HStack(spacing: Theme.Spacing.sm) {
            Image(systemName: symbol)
                .foregroundStyle(Theme.accent)
                .frame(width: 22)
            Text(label)
                .foregroundStyle(Theme.secondaryText)
            Spacer()
            Text(value)
                .fontWeight(.medium)
        }
        .font(.subheadline)
    }

    private func lookup() {
        UIImpactFeedbackGenerator(style: .light).impactOccurred()
        Task { await model.lookup() }
    }
}
