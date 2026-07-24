import Foundation
@testable import SpamFilterKit

/// Test error usable for any of `MockAttestationProvider`'s throwing stubs.
struct MockAttestationError: Error, Equatable {
    let message: String
}

/// `AttestationProvider` test double with canned return values, optional
/// injected errors, and call-count/argument recording so tests can assert on
/// exactly what `EnrollmentService` passed in.
final class MockAttestationProvider: AttestationProvider {
    var stubKeyID = "mock-key-id"
    var stubAttestation = Data("mock-attestation".utf8)
    var stubAssertion = Data("mock-assertion".utf8)
    var generateKeyIDError: Error?
    var attestError: Error?
    var assertError: Error?

    private(set) var generateKeyIDCallCount = 0
    private(set) var attestCallCount = 0
    private(set) var assertCallCount = 0
    private(set) var lastAttestKeyID: String?
    private(set) var lastAssertKeyID: String?
    private(set) var lastAttestClientDataHash: Data?
    private(set) var lastAssertClientDataHash: Data?

    var isEnrolled: Bool { false }

    func existingKeyID() -> String? { nil }

    func generateKeyID() async throws -> String {
        generateKeyIDCallCount += 1
        if let generateKeyIDError { throw generateKeyIDError }
        return stubKeyID
    }

    func attest(keyID: String, clientDataHash: Data) async throws -> Data {
        attestCallCount += 1
        lastAttestKeyID = keyID
        lastAttestClientDataHash = clientDataHash
        if let attestError { throw attestError }
        return stubAttestation
    }

    func assert(keyID: String, clientDataHash: Data) async throws -> Data {
        assertCallCount += 1
        lastAssertKeyID = keyID
        lastAssertClientDataHash = clientDataHash
        if let assertError { throw assertError }
        return stubAssertion
    }
}
