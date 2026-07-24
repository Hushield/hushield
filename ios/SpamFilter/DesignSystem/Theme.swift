import SwiftUI

/// The app's single source of design truth: palette, semantic status colors,
/// spacing, radii, and typography. Everything visual references `Theme` so
/// the look stays cohesive and light/dark are handled in one place.
enum Theme {

    // MARK: - Brand palette

    /// Confident indigo/violet brand accent (not system blue). Slightly
    /// brighter in dark mode so it holds contrast on near-black surfaces.
    static let accent = Color(
        light: Color(red: 0.36, green: 0.30, blue: 0.86),
        dark: Color(red: 0.55, green: 0.49, blue: 0.98)
    )

    /// App background, one step off pure white / pure black.
    static let background = Color(
        light: Color(red: 0.96, green: 0.96, blue: 0.98),
        dark: Color(red: 0.05, green: 0.05, blue: 0.08)
    )

    /// Raised surface (cards) against `background`.
    static let surface = Color(
        light: .white,
        dark: Color(red: 0.11, green: 0.11, blue: 0.15)
    )

    static let primaryText = Color.primary
    static let secondaryText = Color.secondary

    // MARK: - Semantic status colors

    /// Danger — blocked calls/messages.
    static let danger = Color(
        light: Color(red: 0.85, green: 0.22, blue: 0.24),
        dark: Color(red: 1.00, green: 0.42, blue: 0.44)
    )

    /// Warn — suspected / labeled.
    static let warn = Color(
        light: Color(red: 0.80, green: 0.53, blue: 0.05),
        dark: Color(red: 1.00, green: 0.74, blue: 0.28)
    )

    /// Safe — allowlisted / not spam.
    static let safe = Color(
        light: Color(red: 0.13, green: 0.55, blue: 0.34),
        dark: Color(red: 0.36, green: 0.82, blue: 0.55)
    )

    /// Neutral — unknown / no data.
    static let neutral = Color.gray

    // MARK: - Spacing

    enum Spacing {
        static let xs: CGFloat = 4
        static let sm: CGFloat = 8
        static let md: CGFloat = 16
        static let lg: CGFloat = 24
        static let xl: CGFloat = 32
    }

    // MARK: - Corner radii

    enum Radius {
        static let card: CGFloat = 16
        static let control: CGFloat = 12
        static let badge: CGFloat = 8
    }
}

// MARK: - Light/dark color convenience

extension Color {
    /// Builds a dynamic color that resolves to `light` or `dark` per the
    /// active trait collection — the idiomatic way to support both modes
    /// without asset catalog entries.
    init(light: Color, dark: Color) {
        self.init(UIColor { traits in
            traits.userInterfaceStyle == .dark ? UIColor(dark) : UIColor(light)
        })
    }
}

// MARK: - Reputation status vocabulary

/// Normalizes the various server status/action strings into one semantic
/// bucket used for badge color + iconography across the app.
enum ReputationLevel {
    case blocked
    case suspected
    case allowed
    case unknown

    /// Maps a server `status` (and optional `action`) string.
    init(status: String, action: String? = nil) {
        switch status.lowercased() {
        case "blocked", "block":
            self = .blocked
        case "suspected", "label", "labeled", "warn":
            self = .suspected
        case "allowlisted", "allowed", "allow", "not_spam", "clean":
            self = .allowed
        default:
            switch (action ?? "").lowercased() {
            case "block": self = .blocked
            case "label": self = .suspected
            case "allow": self = .allowed
            default: self = .unknown
            }
        }
    }

    var color: Color {
        switch self {
        case .blocked: return Theme.danger
        case .suspected: return Theme.warn
        case .allowed: return Theme.safe
        case .unknown: return Theme.neutral
        }
    }

    var symbol: String {
        switch self {
        case .blocked: return "hand.raised.fill"
        case .suspected: return "exclamationmark.triangle.fill"
        case .allowed: return "checkmark.seal.fill"
        case .unknown: return "questionmark.circle.fill"
        }
    }
}
