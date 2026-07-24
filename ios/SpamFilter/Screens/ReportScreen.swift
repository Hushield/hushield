import SwiftUI
import SpamFilterKit

/// Report a number: vote + category + optional caller name → submit → badge.
struct ReportScreen: View {
    @State private var model: ReportViewModel

    init(reporting: Reporting) {
        _model = State(initialValue: ReportViewModel(reporter: reporting))
    }

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: Theme.Spacing.lg) {
                    header

                    NumberField(title: "Phone number", text: $model.number)
                        .onChange(of: model.number) { model.validationMessage = nil }

                    votePicker
                    categoryPicker

                    LabeledTextField(title: "Caller name (optional)", text: $model.callerName, placeholder: "e.g. \u{201C}IRS Refund\u{201D}")

                    if let validation = model.validationMessage {
                        Text(validation)
                            .font(.footnote)
                            .foregroundStyle(Theme.danger)
                    }

                    resultSection

                    Button("Submit report") { submit() }
                        .buttonStyle(PrimaryButtonStyle(isBusy: model.isSubmitting))
                        .disabled(model.isSubmitting)
                }
                .padding(Theme.Spacing.md)
            }
            .background(Theme.background.ignoresSafeArea())
            .navigationTitle("Report")
        }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
            Text("Report a number")
                .font(.title2.bold())
            Text("Help the community by flagging spam and scam callers.")
                .font(.subheadline)
                .foregroundStyle(Theme.secondaryText)
        }
    }

    private var votePicker: some View {
        VStack(alignment: .leading, spacing: Theme.Spacing.sm) {
            Text("Your verdict")
                .font(.caption.weight(.semibold))
                .foregroundStyle(Theme.secondaryText)
            Picker("Verdict", selection: $model.vote) {
                ForEach(ReportViewModel.Vote.allCases) { vote in
                    Text(vote.label).tag(vote)
                }
            }
            .pickerStyle(.segmented)
        }
    }

    private var categoryPicker: some View {
        VStack(alignment: .leading, spacing: Theme.Spacing.sm) {
            Text("Category")
                .font(.caption.weight(.semibold))
                .foregroundStyle(Theme.secondaryText)
            LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible())], spacing: Theme.Spacing.sm) {
                ForEach(ReportViewModel.Category.allCases) { category in
                    categoryChip(category)
                }
            }
        }
    }

    private func categoryChip(_ category: ReportViewModel.Category) -> some View {
        let selected = model.category == category
        return Button {
            model.category = category
        } label: {
            Text(category.label)
                .font(.subheadline.weight(.medium))
                .frame(maxWidth: .infinity)
                .padding(.vertical, Theme.Spacing.sm + 2)
                .foregroundStyle(selected ? .white : Theme.primaryText)
                .background(
                    (selected ? Theme.accent : Theme.surface),
                    in: RoundedRectangle(cornerRadius: Theme.Radius.badge, style: .continuous)
                )
                .overlay(
                    RoundedRectangle(cornerRadius: Theme.Radius.badge, style: .continuous)
                        .strokeBorder(Color.primary.opacity(selected ? 0 : 0.1))
                )
        }
        .buttonStyle(.plain)
    }

    @ViewBuilder
    private var resultSection: some View {
        switch model.phase {
        case .idle, .submitting:
            EmptyView()
        case let .success(status):
            let level = ReputationLevel(status: status)
            Card {
                VStack(alignment: .leading, spacing: Theme.Spacing.sm) {
                    Label("Report submitted", systemImage: "checkmark.circle.fill")
                        .font(.headline)
                        .foregroundStyle(Theme.safe)
                    HStack {
                        Text("Community status")
                            .foregroundStyle(Theme.secondaryText)
                        Spacer()
                        StatusBadge(level: level, text: status.capitalized)
                    }
                }
            }
        case let .failed(message):
            ErrorBanner(message: message)
        }
    }

    private func submit() {
        UIImpactFeedbackGenerator(style: .medium).impactOccurred()
        Task { await model.submit() }
    }
}
