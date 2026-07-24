import Foundation
@testable import SpamFilterKit

/// In-memory `TokenStore` test double. Never touches the real Keychain, so
/// it is safe to use in unit tests running on a simulator without a host.
final class InMemoryTokenStore: TokenStore {
    private var storedToken: (token: String, expiresAt: Date)?
    private var storedKeyID: String?

    private(set) var saveTokenCallCount = 0
    private(set) var saveKeyIDCallCount = 0

    func saveToken(_ token: String, expiresAt: Date) {
        saveTokenCallCount += 1
        storedToken = (token, expiresAt)
    }

    func loadToken() -> (token: String, expiresAt: Date)? {
        storedToken
    }

    func saveKeyID(_ keyID: String) {
        saveKeyIDCallCount += 1
        storedKeyID = keyID
    }

    func loadKeyID() -> String? {
        storedKeyID
    }

    func clear() {
        storedToken = nil
        storedKeyID = nil
    }
}
