import Foundation
@testable import SpamFilterKit

/// Test-only `CallDirectoryReloading` spy that records every identifier it
/// was asked to reload, in call order.
final class SpyCallDirectoryReloader: CallDirectoryReloading {
    private(set) var reloadedIdentifiers: [String] = []

    func reload(_ identifier: String) async throws {
        reloadedIdentifiers.append(identifier)
    }
}
