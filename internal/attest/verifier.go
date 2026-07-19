package attest

import (
	"context"
	"errors"
)

// ErrAttestationInvalid is the generic failure returned when an attestation
// object does not verify. The verifier fails closed: any mismatch, malformed
// input, or unexpected condition yields an error and never a partial success.
var ErrAttestationInvalid = errors.New("attest: attestation invalid")

// Verifier verifies an App Attest attestation and, on success, returns the
// device's PKIX-DER public key and the App Attest receipt. It fails closed.
type Verifier interface {
	VerifyAttestation(ctx context.Context, keyID string, attestationCBOR, challenge []byte) (publicKeyDER, receipt []byte, err error)
}

// MockVerifier is a Verifier for dev/tests. By default it succeeds, returning
// PublicKeyDER and Receipt. Set Err to force a failure.
type MockVerifier struct {
	PublicKeyDER []byte
	Receipt      []byte
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
