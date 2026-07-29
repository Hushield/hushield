import Foundation

/// Formats phone numbers for display while the user types, and normalizes what
/// they typed into the E.164 form the API requires.
///
/// Deliberately narrow: this formats NANP (+1) numbers, because the seed data is
/// FTC/FCC US complaint data and that is who the app serves today. Anything else
/// is passed through as a `+` followed by its digits rather than mangled into a
/// US shape it does not fit. A real multi-region formatter means libphonenumber,
/// which is a large dependency for a client that currently has none.
public enum PhoneFormatter {

    /// Progressive display formatting, safe to call on every keystroke.
    ///
    /// A bare NANP number formats as the user types: `4` → `(4`,
    /// `424237` → `(424) 237`, `4242374565` → `(424) 237-4565`. Input that
    /// starts with `+` is treated as explicitly international and is only
    /// stripped of junk, never re-shaped — the user has told us the country.
    public static func formatAsTyped(_ input: String) -> String {
        if input.hasPrefix("+") {
            let digits = onlyDigits(input)
            // +1 followed by a full NANP number is worth grouping; other
            // country codes we leave alone.
            if digits.count > 1, digits.hasPrefix("1"), digits.count <= 11 {
                return "+1 " + groupNANP(String(digits.dropFirst()))
            }
            return "+" + digits
        }

        var digits = onlyDigits(input)

        // A leading 1 on an 11-digit entry is the country code, not an area code.
        if digits.count == 11, digits.hasPrefix("1") {
            return "+1 " + groupNANP(String(digits.dropFirst()))
        }
        if digits.count > 10 {
            // Past NANP length with no explicit +, assume they meant +.
            return "+" + digits
        }

        // Trim silently rather than dropping keystrokes on the floor.
        if digits.count > 10 { digits = String(digits.prefix(10)) }
        return groupNANP(digits)
    }

    /// Converts user input to E.164, or nil when it cannot be a valid number.
    ///
    /// Accepts every shape a person might reasonably type:
    /// `4242374565`, `424-237-4565`, `(424) 237-4565`, `+1 424 237 4565`,
    /// `1-424-237-4565`.
    public static func normalize(_ input: String) -> String? {
        let trimmed = input.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty else { return nil }

        let digits = onlyDigits(trimmed)
        guard !digits.isEmpty else { return nil }

        if trimmed.hasPrefix("+") {
            // Explicitly international: trust it, but require a plausible length.
            guard digits.count >= 8, digits.count <= 15 else { return nil }
            return "+" + digits
        }

        switch digits.count {
        case 10:
            // Bare NANP number. A NANP area code and exchange cannot start with
            // 0 or 1, so reject those rather than send the server garbage.
            guard let first = digits.first, first != "0", first != "1" else { return nil }
            return "+1" + digits
        case 11 where digits.hasPrefix("1"):
            let rest = String(digits.dropFirst())
            guard let first = rest.first, first != "0", first != "1" else { return nil }
            return "+1" + rest
        default:
            return nil
        }
    }

    /// True when `input` can be normalized to E.164.
    public static func isValid(_ input: String) -> Bool {
        normalize(input) != nil
    }

    /// Formats a stored E.164 value for display, e.g. `+14242374565` →
    /// `+1 (424) 237-4565`. Non-NANP values are returned unchanged.
    public static func displayE164(_ e164: String) -> String {
        guard e164.hasPrefix("+1") else { return e164 }
        let rest = onlyDigits(String(e164.dropFirst(2)))
        guard rest.count == 10 else { return e164 }
        return "+1 " + groupNANP(rest)
    }

    // MARK: - Helpers

    private static func onlyDigits(_ s: String) -> String {
        s.filter(\.isNumber)
    }

    /// Groups up to 10 digits as `(AAA) BBB-CCCC`, emitting only as much
    /// punctuation as the digits so far justify.
    private static func groupNANP(_ digits: String) -> String {
        let d = Array(digits.prefix(10))
        switch d.count {
        case 0:
            return ""
        case 1...3:
            return "(" + String(d)
        case 4...6:
            return "(" + String(d[0..<3]) + ") " + String(d[3...])
        default:
            return "(" + String(d[0..<3]) + ") " + String(d[3..<6]) + "-" + String(d[6...])
        }
    }
}
