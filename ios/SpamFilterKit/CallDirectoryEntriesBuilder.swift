import Foundation

/// Builds the two ascending-sorted entry lists `CXCallDirectoryProvider`
/// requires from a `BlocklistState` snapshot. Pure and CallKit-free so it's
/// unit-testable without an OS host; the `CallDirectoryHandler` shim feeds
/// its output straight to `CXCallDirectoryExtensionContext`.
public enum CallDirectoryEntriesBuilder {

    /// Fallback label for an identified number with no known name.
    public static let unknownLabel = "Suspected Spam"

    /// - Returns:
    ///   - `blocking`: `state.blocked`, sorted strictly ascending.
    ///   - `identification`: `state.labeled`, sorted strictly ascending,
    ///     each paired with `state.names[number] ?? "Suspected Spam"`.
    ///
    ///   Both arrays MUST be strictly ascending -- `CXCallDirectoryExtensionContext`
    ///   (`addBlockingEntry`/`addIdentificationEntry` "NextSequentialPhoneNumber")
    ///   throws if a number is added out of order.
    public static func build(_ state: BlocklistState) -> (blocking: [Int64], identification: [(number: Int64, label: String)]) {
        let blocking = state.blocked.sorted()
        let identification = state.labeled.sorted().map { number in
            (number: number, label: state.names[number] ?? unknownLabel)
        }
        return (blocking: blocking, identification: identification)
    }
}
