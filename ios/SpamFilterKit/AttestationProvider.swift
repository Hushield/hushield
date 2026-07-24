import Foundation
import DeviceCheck

/// Error thrown by the concrete `AttestationProvider`s when
/// `DCAppAttestService`'s completion handler fires with neither a result nor
/// an error (should not happen in practice, but the API allows it).
public enum AttestationProviderError: Error, Equatable {
    case unknown
}

/// Abstraction over the App Attest key lifecycle, so `EnrollmentService` can
/// be unit-tested with a mock and the app can select a Simulator-safe stub
/// when the real `DCAppAttestService` is unavailable.
public protocol AttestationProvider {
    /// Whether this provider already has a generated key it can reuse.
    var isEnrolled: Bool { get }
    /// The previously generated key ID, if any.
    func existingKeyID() -> String?
    /// Generates (or returns the already-generated) key ID for this install.
    func generateKeyID() async throws -> String
    /// Produces the attestation blob for a freshly generated key.
    func attest(keyID: String, clientDataHash: Data) async throws -> Data
    /// Produces an assertion blob for an already-attested key.
    func assert(keyID: String, clientDataHash: Data) async throws -> Data
}

/// Wraps the real `DCAppAttestService`. Only usable on a physical device --
/// `DCAppAttestService.shared.isSupported` is `false` on the Simulator.
/// The key ID it generates is persisted in `UserDefaults` (it is not a
/// secret, just an identifier of the Secure Enclave key), so `generateKeyID`
/// stays idempotent across calls.
public final class DeviceAttestationProvider: AttestationProvider {
    private let service: DCAppAttestService
    private let defaults: UserDefaults
    private static let keyIDDefaultsKey = "com.brahy.spamfilter.attest.deviceKeyID"

    public init(service: DCAppAttestService = .shared, defaults: UserDefaults = .standard) {
        self.service = service
        self.defaults = defaults
    }

    public var isEnrolled: Bool { existingKeyID() != nil }

    public func existingKeyID() -> String? {
        defaults.string(forKey: Self.keyIDDefaultsKey)
    }

    public func generateKeyID() async throws -> String {
        if let existing = existingKeyID() { return existing }
        let keyID: String = try await withCheckedThrowingContinuation { continuation in
            service.generateKey { keyID, error in
                if let keyID {
                    continuation.resume(returning: keyID)
                } else {
                    continuation.resume(throwing: error ?? AttestationProviderError.unknown)
                }
            }
        }
        defaults.set(keyID, forKey: Self.keyIDDefaultsKey)
        return keyID
    }

    public func attest(keyID: String, clientDataHash: Data) async throws -> Data {
        try await withCheckedThrowingContinuation { continuation in
            service.attestKey(keyID, clientDataHash: clientDataHash) { data, error in
                if let data {
                    continuation.resume(returning: data)
                } else {
                    continuation.resume(throwing: error ?? AttestationProviderError.unknown)
                }
            }
        }
    }

    public func assert(keyID: String, clientDataHash: Data) async throws -> Data {
        try await withCheckedThrowingContinuation { continuation in
            service.generateAssertion(keyID, clientDataHash: clientDataHash) { data, error in
                if let data {
                    continuation.resume(returning: data)
                } else {
                    continuation.resume(throwing: error ?? AttestationProviderError.unknown)
                }
            }
        }
    }
}

/// Stub used when `DCAppAttestService.shared.isSupported == false` (the
/// Simulator, where App Attest hardware is unavailable). Generates a stable
/// fake key ID and returns a fixed non-empty blob for attest/assert -- the
/// backend's `ATTEST_MODE=mock` only checks that the bytes are present,
/// valid base64, and non-empty.
public final class SimulatorAttestationProvider: AttestationProvider {
    private let defaults: UserDefaults
    private static let keyIDDefaultsKey = "com.brahy.spamfilter.attest.simulatorKeyID"
    private static let dummyBlob = Data("spamfilter-simulator-attestation".utf8)

    public init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
    }

    public var isEnrolled: Bool { existingKeyID() != nil }

    public func existingKeyID() -> String? {
        defaults.string(forKey: Self.keyIDDefaultsKey)
    }

    public func generateKeyID() async throws -> String {
        if let existing = existingKeyID() { return existing }
        var bytes = Data(count: 32)
        bytes.withUnsafeMutableBytes { pointer in
            _ = SecRandomCopyBytes(kSecRandomDefault, 32, pointer.baseAddress!)
        }
        let keyID = bytes.base64EncodedString()
        defaults.set(keyID, forKey: Self.keyIDDefaultsKey)
        return keyID
    }

    public func attest(keyID: String, clientDataHash: Data) async throws -> Data {
        Self.dummyBlob
    }

    public func assert(keyID: String, clientDataHash: Data) async throws -> Data {
        Self.dummyBlob
    }
}

/// Selects `DeviceAttestationProvider` on hardware that supports App Attest,
/// falling back to `SimulatorAttestationProvider` otherwise (Simulator, or
/// devices without the Secure Enclave support App Attest requires).
public func makeAttestationProvider(defaults: UserDefaults = .standard) -> AttestationProvider {
    if DCAppAttestService.shared.isSupported {
        return DeviceAttestationProvider(defaults: defaults)
    }
    return SimulatorAttestationProvider(defaults: defaults)
}
