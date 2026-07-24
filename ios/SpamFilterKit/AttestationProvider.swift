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
///
/// Providers do not persist anything themselves -- `TokenStore` is the sole
/// persistent owner of the key ID (see `EnrollmentService`). Every call to
/// `generateKeyID()` produces a genuinely new key; callers that want reuse
/// across enroll calls store the returned key ID themselves and only call
/// `generateKeyID()` again once that stored value has been cleared.
public protocol AttestationProvider {
    /// Generates a brand-new key ID. Never reuses a previous value.
    func generateKeyID() async throws -> String
    /// Produces the attestation blob for a freshly generated key.
    func attest(keyID: String, clientDataHash: Data) async throws -> Data
    /// Produces an assertion blob for an already-attested key.
    func assert(keyID: String, clientDataHash: Data) async throws -> Data
}

/// Wraps the real `DCAppAttestService`. Only usable on a physical device --
/// `DCAppAttestService.shared.isSupported` is `false` on the Simulator.
public final class DeviceAttestationProvider: AttestationProvider {
    private let service: DCAppAttestService

    public init(service: DCAppAttestService = .shared) {
        self.service = service
    }

    public func generateKeyID() async throws -> String {
        try await withCheckedThrowingContinuation { continuation in
            service.generateKey { keyID, error in
                if let keyID {
                    continuation.resume(returning: keyID)
                } else {
                    continuation.resume(throwing: error ?? AttestationProviderError.unknown)
                }
            }
        }
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
/// Simulator, where App Attest hardware is unavailable). Generates a fresh
/// fake key ID and returns a fixed non-empty blob for attest/assert -- the
/// backend's `ATTEST_MODE=mock` only checks that the bytes are present,
/// valid base64, and non-empty.
public final class SimulatorAttestationProvider: AttestationProvider {
    private static let dummyBlob = Data("spamfilter-simulator-attestation".utf8)

    public init() {}

    public func generateKeyID() async throws -> String {
        var bytes = Data(count: 32)
        bytes.withUnsafeMutableBytes { pointer in
            _ = SecRandomCopyBytes(kSecRandomDefault, 32, pointer.baseAddress!)
        }
        return bytes.base64EncodedString()
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
public func makeAttestationProvider() -> AttestationProvider {
    if DCAppAttestService.shared.isSupported {
        return DeviceAttestationProvider()
    }
    return SimulatorAttestationProvider()
}
