import Foundation

/// E.164 <-> `Int64` conversion. `CXCallDirectoryPhoneNumber` (CallKit) and
/// the IdentityLookup extensions represent phone numbers as `Int64`; the
/// backend represents them as E.164 strings (`"+14155551234"`).
public enum PhoneNumber {

    /// Parses an E.164 string into the `Int64` CallKit expects.
    ///
    /// Valid input: a single leading `+`, followed only by ASCII digits,
    /// 8-15 digits long, with no leading zero after the `+` (E.164 numbers
    /// never have one -- accepting one would also break the round trip,
    /// since `Int64` silently drops it). Anything else returns `nil`.
    public static func e164ToInt64(_ e164: String) -> Int64? {
        guard e164.hasPrefix("+") else { return nil }
        let digits = e164.dropFirst()
        guard digits.count >= 8, digits.count <= 15 else { return nil }
        guard digits.allSatisfy({ $0.isASCII && $0.isNumber }) else { return nil }
        guard digits.first != "0" else { return nil }
        return Int64(digits)
    }

    /// `+` followed by the decimal representation of `value`.
    public static func int64ToE164(_ value: Int64) -> String {
        "+" + String(value)
    }
}
