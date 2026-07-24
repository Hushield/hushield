import Foundation

/// The decision `MessageClassifier` returns for an inbound SMS/iMessage,
/// deliberately plain (no `IdentityLookup` import) so it's usable from
/// unit tests without an OS host. The `MessageFilterExtension` shim maps
/// this to `ILMessageFilterAction`.
public enum MessageAction: Equatable {
    /// The sender is a known spammer -- filter the message into Junk.
    case junk
    /// The sender is known-good, or nothing is known about it -- let it
    /// through unfiltered.
    case allow
    /// No opinion; defer to the system/other filters.
    case none
}

/// Pure sender classification against the local `BlocklistState` snapshot.
/// No I/O, no `IdentityLookup` import -- fully unit-testable.
public enum MessageClassifier {

    /// Classifies an inbound message by its sender's phone number.
    ///
    /// - `sender` is parsed via `PhoneNumber.e164ToInt64`; if it isn't a
    ///   parseable E.164 number (e.g. a short code or alphanumeric sender
    ///   ID), the classifier has no opinion and returns `.allow`.
    /// - A number in `state.blocked` or `state.labeled` is spam -> `.junk`.
    /// - Anything else (unknown number) -> `.allow`.
    public static func classify(sender: String, body: String, state: BlocklistState) -> MessageAction {
        guard let number = PhoneNumber.e164ToInt64(sender) else { return .allow }

        if state.blocked.contains(number) || state.labeled.contains(number) {
            return .junk
        }
        return .allow
    }
}
