package attest

import (
	"context"
	"errors"
)

// ErrAttestationInvalid is the generic failure returned when an attestation
// object does not verify. The verifier fails closed: any mismatch, malformed
// input, or unexpected condition yields an error and never a partial success.
var ErrAttestationInvalid = errors.New("attest: attestation invalid")

// Verifier verifies App Attest attestations and assertions. It fails closed:
// any mismatch, malformed input, or unexpected condition yields an error and
// never a partial success.
//
// VerifyAttestation validates the one-time attestation and returns the
// device's PKIX-DER public key and the App Attest receipt.
//
// VerifyAssertion validates a per-request assertion signed by the previously
// attested key. It enforces a strictly-increasing signature counter (replay
// protection) and returns the assertion's new counter on success.
type Verifier interface {
	VerifyAttestation(ctx context.Context, keyID string, attestationCBOR, challenge []byte) (publicKeyDER, receipt []byte, err error)
	VerifyAssertion(ctx context.Context, publicKeyDER, assertion, clientDataHash []byte, prevCounter uint32) (newCounter uint32, err error)
}

// MockVerifier is a Verifier for dev/tests. By default it succeeds, returning
// PublicKeyDER and Receipt for attestations and prevCounter+1 for assertions.
// Set Err to force a failure. Set NewCounter to override the returned
// assertion counter.
type MockVerifier struct {
	PublicKeyDER []byte
	Receipt      []byte
	NewCounter   uint32
	Err          error
}

// NewMockVerifier returns a MockVerifier that succeeds by default with the
// given canned public key and receipt.
func NewMockVerifier(publicKeyDER, receipt []byte) *MockVerifier {
	return &MockVerifier{PublicKeyDER: publicKeyDER, Receipt: receipt}
}

// VerifyAttestation returns the configured error if set, otherwise the
// configured public key and receipt.
func (m *MockVerifier) VerifyAttestation(ctx context.Context, keyID string, attestationCBOR, challenge []byte) ([]byte, []byte, error) {
	if m.Err != nil {
		return nil, nil, m.Err
	}
	pk := m.PublicKeyDER
	if pk == nil {
		pk = []byte("mock-public-key-der")
	}
	return pk, m.Receipt, nil
}

// VerifyAssertion returns the configured error if set, otherwise NewCounter if
// set (nonzero), otherwise prevCounter+1.
func (m *MockVerifier) VerifyAssertion(ctx context.Context, publicKeyDER, assertion, clientDataHash []byte, prevCounter uint32) (uint32, error) {
	if m.Err != nil {
		return 0, m.Err
	}
	if m.NewCounter != 0 {
		return m.NewCounter, nil
	}
	return prevCounter + 1, nil
}
