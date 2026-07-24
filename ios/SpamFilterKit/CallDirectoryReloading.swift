import Foundation

/// Abstraction over "ask the system to re-invoke the Call Directory
/// extension's `beginRequest(with:)`." `SyncService` talks only to this
/// protocol, so tests can inject a spy instead of touching CallKit.
public protocol CallDirectoryReloading {
    func reload(_ identifier: String) async throws
}
