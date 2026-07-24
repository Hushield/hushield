import Foundation
import CryptoKit

/// Errors specific to the enrollment/refresh flow (as opposed to
/// `APIClientError`, which `EnrollmentService` also propagates unchanged).
public enum EnrollmentError: Error, Equatable {
    /// `refresh()` or `validToken()` needed a stored key ID, but none was
    /// found and no enrollment has ever succeeded.
    case notEnrolled
    /// The server returned an `expires_at` string that isn't valid RFC3339.
    case malformedExpiry(String)
}

/// Composes `APIClient` + `AttestationProvider` + `TokenStore` into the App
/// Attest enroll/refresh flow described in the backend's `/api/v1/attest/*`
/// endpoints. This is what higher layers call to obtain a device token.
public final class EnrollmentService {
    private let apiClient: APIClient
    private let provider: AttestationProvider
    private let tokenStore: TokenStore
    private let tokenExpirySkew: TimeInterval

    /// - Parameter tokenExpirySkew: how far ahead of the real expiry a token
    ///   is treated as already-expired, so a refresh has time to complete
    ///   before the server actually rejects the old token.
    public init(apiClient: APIClient, provider: AttestationProvider, tokenStore: TokenStore, tokenExpirySkew: TimeInterval = 30) {
        self.apiClient = apiClient
        self.provider = provider
        self.tokenStore = tokenStore
        self.tokenExpirySkew = tokenExpirySkew
    }

    /// Full App Attest enrollment: reuse a stored key ID if present, else
    /// generate one; attest it against a fresh challenge; store the
    /// resulting device token + key ID. Nothing is persisted unless every
    /// step succeeds.
    public func enroll() async throws {
        let keyID: String
        if let stored = tokenStore.loadKeyID() {
            keyID = stored
        } else {
            keyID = try await provider.generateKeyID()
        }

        let challengeData = try await apiClient.challenge()
        let clientDataHash = try Self.clientDataHash(forChallengeB64: challengeData.challenge)
        let attestation = try await provider.attest(keyID: keyID, clientDataHash: clientDataHash)
        let tokenData = try await apiClient.verify(
            keyID: keyID,
            attestationB64: attestation.base64EncodedString(),
            challengeB64: challengeData.challenge
        )
        let expiresAt = try Self.parseExpiry(tokenData.expiresAt)

        tokenStore.saveKeyID(keyID)
        tokenStore.saveToken(tokenData.deviceToken, expiresAt: expiresAt)
    }

    /// Refresh a device token for an already-attested key -- no
    /// re-attestation. Requires a previously stored key ID.
    public func refresh() async throws {
        guard let keyID = tokenStore.loadKeyID() else {
            throw EnrollmentError.notEnrolled
        }

        let challengeData = try await apiClient.challenge()
        let clientDataHash = try Self.clientDataHash(forChallengeB64: challengeData.challenge)
        let assertion = try await provider.assert(keyID: keyID, clientDataHash: clientDataHash)
        let tokenData = try await apiClient.assert(
            keyID: keyID,
            assertionB64: assertion.base64EncodedString(),
            challengeB64: challengeData.challenge
        )
        let expiresAt = try Self.parseExpiry(tokenData.expiresAt)

        tokenStore.saveToken(tokenData.deviceToken, expiresAt: expiresAt)
    }

    /// Returns a usable device token: the cached one if it isn't within
    /// `tokenExpirySkew` of expiring, otherwise refreshes (or enrolls, if
    /// this device has never enrolled) first.
    public func validToken() async throws -> String {
        if let stored = tokenStore.loadToken(), stored.expiresAt > Date().addingTimeInterval(tokenExpirySkew) {
            return stored.token
        }

        if tokenStore.loadToken() == nil {
            try await enroll()
        } else {
            try await refresh()
        }

        guard let stored = tokenStore.loadToken() else {
            throw EnrollmentError.notEnrolled
        }
        return stored.token
    }

    // MARK: - Helpers

    /// `clientDataHash = SHA256(rawChallengeBytes)` -- the server re-derives
    /// the same hash from the raw bytes of the challenge it issued, so the
    /// base64 challenge string must be decoded first, never hashed as text.
    private static func clientDataHash(forChallengeB64 challengeB64: String) throws -> Data {
        guard let challengeBytes = Data(base64Encoded: challengeB64) else {
            throw APIClientError.invalidRequest(description: "challenge was not valid base64")
        }
        return Data(SHA256.hash(data: challengeBytes))
    }

    private static func parseExpiry(_ raw: String) throws -> Date {
        if let date = rfc3339Formatter.date(from: raw) {
            return date
        }
        if let date = rfc3339FractionalFormatter.date(from: raw) {
            return date
        }
        throw EnrollmentError.malformedExpiry(raw)
    }

    private static let rfc3339Formatter: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        return formatter
    }()

    private static let rfc3339FractionalFormatter: ISO8601DateFormatter = {
        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return formatter
    }()
}
