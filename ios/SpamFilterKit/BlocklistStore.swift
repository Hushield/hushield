import Foundation

/// Persists `BlocklistState` as JSON in a directory, injected so tests can
/// use a temp directory instead of the real App Group container. The Call
/// Directory and SMS extensions and the host app all read/write the same
/// file via `makeAppGroupStore`, which resolves the shared App Group
/// container.
public final class BlocklistStore {
    /// Errors from `makeAppGroupStore` -- never a crash, always a thrown
    /// error, so callers can fall back gracefully.
    public enum FactoryError: Error, Equatable {
        case appGroupContainerUnavailable(identifier: String)
    }

    private static let fileName = "blocklist.json"

    private let directory: URL

    /// - Parameter directory: where `blocklist.json` is read from / written
    ///   to. Inject a temp directory in tests -- never the real App Group
    ///   container.
    public init(directory: URL) {
        self.directory = directory
    }

    /// Resolves the shared App Group container and returns a store rooted
    /// there. Throws (rather than crashing) if the container URL can't be
    /// resolved, e.g. the entitlement is missing at runtime.
    ///
    /// - Parameter containerResolver: how to resolve the App Group
    ///   identifier to a container URL. Defaults to
    ///   `FileManager.containerURL(forSecurityApplicationGroupIdentifier:)`;
    ///   overridable in tests, since that API doesn't actually validate the
    ///   entitlement in-process (it happily returns a path for a
    ///   nonexistent identifier), so the nil/failure branch can't otherwise
    ///   be exercised without injection.
    public static func makeAppGroupStore(
        appGroupIdentifier: String = "group.com.brahy.spamfilter",
        containerResolver: (String) -> URL? = { FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: $0) }
    ) throws -> BlocklistStore {
        guard let containerURL = containerResolver(appGroupIdentifier) else {
            throw FactoryError.appGroupContainerUnavailable(identifier: appGroupIdentifier)
        }
        return BlocklistStore(directory: containerURL)
    }

    private var fileURL: URL {
        directory.appendingPathComponent(Self.fileName)
    }

    /// Decodes the persisted state, or returns an empty `BlocklistState` if
    /// the file is absent or its contents aren't valid JSON for this type.
    public func load() -> BlocklistState {
        guard let data = try? Data(contentsOf: fileURL),
              let state = try? JSONDecoder().decode(BlocklistState.self, from: data) else {
            return BlocklistState()
        }
        return state
    }

    /// Atomically writes `state` as JSON to the directory, creating the
    /// directory first if needed.
    public func save(_ state: BlocklistState) throws {
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        let data = try JSONEncoder().encode(state)
        try data.write(to: fileURL, options: .atomic)
    }
}
