package attest

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"fmt"
)

// PlayIntegrityVerifier verifies Android devices attested via the Play
// Integrity API. It implements Verifier, fails closed on any deviation, and
// mirrors AppleVerifier's structure: VerifyAttestation (added in a later
// change) establishes trust in a device-generated key once; VerifyAssertion
// (here) checks that key's signature on every later request.
//
// Unlike App Attest, Android has no OS-level per-request signed-assertion
// format, so this file defines a minimal one both this server and the
// Android client agree on: a JSON envelope carrying a strictly-increasing
// counter and an ECDSA-P256 signature over
// SHA256(clientDataHash || big-endian-uint32(counter)).
type PlayIntegrityVerifier struct{}

// androidAssertion is the wire format signTestAssertion in this file's tests
// constructs and the real Android client must produce identically.
type androidAssertion struct {
	Counter   uint32 `json:"counter"`
	Signature []byte `json:"signature"`
}

// VerifyAssertion implements Verifier for Android's assertion format.
func (v *PlayIntegrityVerifier) VerifyAssertion(ctx context.Context, publicKeyDER, assertion, clientDataHash []byte, prevCounter uint32) (uint32, error) {
	var obj androidAssertion
	if err := json.Unmarshal(assertion, &obj); err != nil {
		return 0, fmt.Errorf("%w: decode assertion: %v", ErrAttestationInvalid, err)
	}
	if len(obj.Signature) == 0 {
		return 0, fmt.Errorf("%w: assertion missing signature", ErrAttestationInvalid)
	}
	if obj.Counter <= prevCounter {
		return 0, fmt.Errorf("%w: sign counter %d not greater than previous %d", ErrAttestationInvalid, obj.Counter, prevCounter)
	}

	pub, err := x509.ParsePKIXPublicKey(publicKeyDER)
	if err != nil {
		return 0, fmt.Errorf("%w: parse public key: %v", ErrAttestationInvalid, err)
	}
	ecdsaPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return 0, fmt.Errorf("%w: stored public key is not ECDSA", ErrAttestationInvalid)
	}

	var counterBytes [4]byte
	binary.BigEndian.PutUint32(counterBytes[:], obj.Counter)
	h := sha256.New()
	h.Write(clientDataHash)
	h.Write(counterBytes[:])
	message := h.Sum(nil)

	if !ecdsa.VerifyASN1(ecdsaPub, message, obj.Signature) {
		return 0, fmt.Errorf("%w: assertion signature invalid", ErrAttestationInvalid)
	}

	return obj.Counter, nil
}
