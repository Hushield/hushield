import Foundation

/// The local, offline snapshot of the community blocklist. `blocked` and
/// `labeled` are kept mutually exclusive -- a number is one or the other,
/// never both. `cursor` is the last-applied sync cursor (`"<sec>.<id>"`);
/// an empty cursor means "no sync has ever been applied, fetch a full
/// snapshot next".
public struct BlocklistState: Codable, Equatable {
    public var blocked: Set<Int64>
    public var labeled: Set<Int64>
    public var names: [Int64: String]
    public var cursor: String

    public init(blocked: Set<Int64> = [], labeled: Set<Int64> = [], names: [Int64: String] = [:], cursor: String = "") {
        self.blocked = blocked
        self.labeled = labeled
        self.names = names
        self.cursor = cursor
    }

    /// Applies `entries` in order to produce a new state, then sets `cursor`
    /// to `newCursor`. Entries whose `number` isn't a parseable E.164 string
    /// are skipped without affecting the rest.
    ///
    /// - `block`: moves the number into `blocked` (out of `labeled` if it
    ///   was there); sets `names[number]` if the entry carries a name.
    /// - `label`: moves the number into `labeled` (out of `blocked` if it
    ///   was there); sets `names[number]` if the entry carries a name.
    /// - `unblock`: a tombstone -- removes the number from both sets and
    ///   drops any stored name.
    public func applying(_ entries: [BlocklistEntry], newCursor: String) -> BlocklistState {
        var blocked = self.blocked
        var labeled = self.labeled
        var names = self.names

        for entry in entries {
            guard let number = PhoneNumber.e164ToInt64(entry.number) else { continue }

            switch entry.action {
            case "block":
                labeled.remove(number)
                blocked.insert(number)
                if let name = entry.name { names[number] = name }
            case "label":
                blocked.remove(number)
                labeled.insert(number)
                if let name = entry.name { names[number] = name }
            case "unblock":
                blocked.remove(number)
                labeled.remove(number)
                names.removeValue(forKey: number)
            default:
                continue
            }
        }

        return BlocklistState(blocked: blocked, labeled: labeled, names: names, cursor: newCursor)
    }
}
