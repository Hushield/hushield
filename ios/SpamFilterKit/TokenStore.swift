import Foundation
import Security

/// Persists the device's App Attest identity: the key ID (reused across
/// enroll/refresh) and the current device token + its expiry.
public protocol TokenStore {
    func saveToken(_ token: String, expiresAt: Date)
    func loadToken() -> (token: String, expiresAt: Date)?
    func saveKeyID(_ keyID: String)
    func loadKeyID() -> String?
    func clear()
}

/// Keychain-backed `TokenStore`. Stores the device token + expiry and the
/// App Attest key ID as separate `kSecClassGenericPassword` items under a
/// single service name.
public final class KeychainTokenStore: TokenStore {
    private let service: String

    private static let tokenAccount = "device_token"
    private static let keyIDAccount = "attest_key_id"

    public init(service: String = "com.brahy.hushield") {
        self.service = service
    }

    public func saveToken(_ token: String, expiresAt: Date) {
        guard let data = try? JSONEncoder().encode(TokenPayload(token: token, expiresAt: expiresAt)) else { return }
        write(data, account: Self.tokenAccount)
    }

    public func loadToken() -> (token: String, expiresAt: Date)? {
        guard let data = read(account: Self.tokenAccount),
              let payload = try? JSONDecoder().decode(TokenPayload.self, from: data) else {
            return nil
        }
        return (payload.token, payload.expiresAt)
    }

    public func saveKeyID(_ keyID: String) {
        write(Data(keyID.utf8), account: Self.keyIDAccount)
    }

    public func loadKeyID() -> String? {
        guard let data = read(account: Self.keyIDAccount) else { return nil }
        return String(data: data, encoding: .utf8)
    }

    public func clear() {
        delete(account: Self.tokenAccount)
        delete(account: Self.keyIDAccount)
    }

    // MARK: - Keychain plumbing

    private func write(_ data: Data, account: String) {
        delete(account: account)
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecValueData as String: data,
        ]
        SecItemAdd(query as CFDictionary, nil)
    }

    private func read(account: String) -> Data? {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
            kSecReturnData as String: true,
            kSecMatchLimit as String: kSecMatchLimitOne,
        ]
        var result: AnyObject?
        let status = SecItemCopyMatching(query as CFDictionary, &result)
        guard status == errSecSuccess, let data = result as? Data else { return nil }
        return data
    }

    private func delete(account: String) {
        let query: [String: Any] = [
            kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: service,
            kSecAttrAccount as String: account,
        ]
        SecItemDelete(query as CFDictionary)
    }

    private struct TokenPayload: Codable {
        let token: String
        let expiresAt: Date
    }
}
