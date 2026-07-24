import SwiftUI

// MARK: - Card container

/// A raised, rounded surface used to group content. The one card look used
/// everywhere so screens read as one system.
struct Card<Content: View>: View {
    @ViewBuilder var content: Content

    var body: some View {
        content
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(Theme.Spacing.md)
            .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous))
            .overlay(
                RoundedRectangle(cornerRadius: Theme.Radius.card, style: .continuous)
                    .strokeBorder(Color.primary.opacity(0.06))
            )
    }
}

/// A titled section: small uppercase label above a card.
struct SectionCard<Content: View>: View {
    let title: String
    var systemImage: String?
    @ViewBuilder var content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Spacing.sm) {
            Label {
                Text(title.uppercased())
            } icon: {
                if let systemImage { Image(systemName: systemImage) }
            }
            .font(.caption.weight(.semibold))
            .foregroundStyle(Theme.secondaryText)
            .labelStyle(.titleAndIcon)

            Card { content }
        }
    }
}

// MARK: - Status badge

/// Color-coded pill for a reputation level. blocked=red, suspected=amber,
/// allowed=green, unknown=gray.
struct StatusBadge: View {
    let level: ReputationLevel
    let text: String

    init(level: ReputationLevel, text: String? = nil) {
        self.level = level
        self.text = text ?? Self.defaultText(for: level)
    }

    private static func defaultText(for level: ReputationLevel) -> String {
        switch level {
        case .blocked: return "Blocked"
        case .suspected: return "Suspected"
        case .allowed: return "Allowed"
        case .unknown: return "Unknown"
        }
    }

    var body: some View {
        Label {
            Text(text)
        } icon: {
            Image(systemName: level.symbol)
        }
        .font(.subheadline.weight(.semibold))
        .foregroundStyle(level.color)
        .padding(.horizontal, Theme.Spacing.sm + 2)
        .padding(.vertical, Theme.Spacing.xs + 2)
        .background(level.color.opacity(0.15), in: Capsule())
    }
}

// MARK: - Labeled number field

/// A labeled phone-number field with a leading SF Symbol and telephone-pad
/// keyboard.
struct NumberField: View {
    let title: String
    @Binding var text: String
    var placeholder: String = "+14155551234"

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
            Text(title)
                .font(.caption.weight(.semibold))
                .foregroundStyle(Theme.secondaryText)
            HStack(spacing: Theme.Spacing.sm) {
                Image(systemName: "phone.fill")
                    .foregroundStyle(Theme.accent)
                TextField(placeholder, text: $text)
                    .keyboardType(.phonePad)
                    .textContentType(.telephoneNumber)
                    .autocorrectionDisabled()
            }
            .padding(Theme.Spacing.md - 2)
            .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
            .overlay(
                RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous)
                    .strokeBorder(Color.primary.opacity(0.08))
            )
        }
    }
}

/// A plain labeled text field (e.g. caller name).
struct LabeledTextField: View {
    let title: String
    @Binding var text: String
    var placeholder: String = ""

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
            Text(title)
                .font(.caption.weight(.semibold))
                .foregroundStyle(Theme.secondaryText)
            TextField(placeholder, text: $text)
                .autocorrectionDisabled()
                .padding(Theme.Spacing.md - 2)
                .background(Theme.surface, in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
                .overlay(
                    RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous)
                        .strokeBorder(Color.primary.opacity(0.08))
                )
        }
    }
}

// MARK: - Button styles

/// Filled brand-accent primary action button, with a busy/disabled state.
struct PrimaryButtonStyle: ButtonStyle {
    var isBusy: Bool = false

    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: Theme.Spacing.sm) {
            if isBusy { ProgressView().tint(.white) }
            configuration.label
        }
        .font(.headline)
        .foregroundStyle(.white)
        .frame(maxWidth: .infinity)
        .padding(.vertical, Theme.Spacing.md - 2)
        .background(Theme.accent.opacity(configuration.isPressed ? 0.8 : 1), in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
        .opacity(isBusy ? 0.85 : 1)
    }
}

/// Tinted secondary action button.
struct SecondaryButtonStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.headline)
            .foregroundStyle(Theme.accent)
            .frame(maxWidth: .infinity)
            .padding(.vertical, Theme.Spacing.md - 2)
            .background(Theme.accent.opacity(configuration.isPressed ? 0.2 : 0.12), in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
    }
}

// MARK: - Shared state views

/// Centered empty/placeholder state with an icon, title, and message.
struct EmptyStateView: View {
    let systemImage: String
    let title: String
    let message: String

    var body: some View {
        VStack(spacing: Theme.Spacing.sm) {
            Image(systemName: systemImage)
                .font(.system(size: 42))
                .foregroundStyle(Theme.accent.opacity(0.6))
            Text(title)
                .font(.headline)
            Text(message)
                .font(.subheadline)
                .foregroundStyle(Theme.secondaryText)
                .multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity)
        .padding(.vertical, Theme.Spacing.xl)
    }
}

/// Inline error banner.
struct ErrorBanner: View {
    let message: String

    var body: some View {
        Label {
            Text(message)
        } icon: {
            Image(systemName: "exclamationmark.triangle.fill")
        }
        .font(.subheadline)
        .foregroundStyle(Theme.danger)
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(Theme.Spacing.md - 2)
        .background(Theme.danger.opacity(0.12), in: RoundedRectangle(cornerRadius: Theme.Radius.control, style: .continuous))
    }
}
