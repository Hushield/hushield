import CallKit

/// Real `CallDirectoryReloading` that asks CallKit to re-invoke the Call
/// Directory extension's `beginRequest(with:)` so it picks up freshly
/// synced data. Only the container app can call this (not the extension
/// itself). The only file in `SpamFilterKit` that imports `CallKit` --
/// `SyncService`, `CallDirectoryEntriesBuilder`, and `MessageClassifier`
/// stay free of it so they're unit-testable without an OS host.
public final class CXCallDirectoryReloader: CallDirectoryReloading {
    public init() {}

    public func reload(_ identifier: String) async throws {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            CXCallDirectoryManager.sharedInstance.reloadExtension(withIdentifier: identifier) { error in
                if let error {
                    continuation.resume(throwing: error)
                } else {
                    continuation.resume()
                }
            }
        }
    }
}
