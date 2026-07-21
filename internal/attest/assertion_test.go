package attest

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

// assertionFixture holds the inputs and outputs needed to build and tweak a
// valid App Attest assertion for testing, mirroring the attestation harness.
type assertionFixture struct {
	deviceKey      *ecdsa.PrivateKey
	publicKeyDER   []byte
	clientDataHash []byte
	authData       []byte
	signCount      uint32
	assertion      []byte // CBOR
}

// buildAssertionAuthData builds the 37-byte authenticatorData for an assertion:
// rpIdHash[32] || flags[1] || signCount[4].
func buildAssertionAuthData(appID string, signCount uint32) []byte {
	rpIDHash := sha256.Sum256([]byte(appID))
	buf := bytes.Buffer{}
	buf.Write(rpIDHash[:]) // 32
	buf.WriteByte(0x00)    // flags
	sc := make([]byte, 4)
	binary.BigEndian.PutUint32(sc, signCount)
	buf.Write(sc) // 4
	return buf.Bytes()
}

// buildValidAssertion crafts a genuine assertion signed by a fresh device key
// over nonce = SHA256(authData || clientDataHash).
func buildValidAssertion(t *testing.T, appID string, signCount uint32) assertionFixture {
	t.Helper()

	deviceKey := mustGenKey(t)
	pubDER, err := x509.MarshalPKIXPublicKey(&deviceKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}

	clientDataHash := make([]byte, 32)
	if _, err := rand.Read(clientDataHash); err != nil {
		t.Fatalf("rand client data hash: %v", err)
	}

	authData := buildAssertionAuthData(appID, signCount)

	nh := sha256.New()
	nh.Write(authData)
	nh.Write(clientDataHash)
	nonce := nh.Sum(nil)

	sig, err := ecdsa.SignASN1(rand.Reader, deviceKey, nonce)
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}

	assertionCBOR, err := cbor.Marshal(assertionObject{
		Signature:         sig,
		AuthenticatorData: authData,
	})
	if err != nil {
		t.Fatalf("cbor marshal assertion: %v", err)
	}

	return assertionFixture{
		deviceKey:      deviceKey,
		publicKeyDER:   pubDER,
		clientDataHash: clientDataHash,
		authData:       authData,
		signCount:      signCount,
		assertion:      assertionCBOR,
	}
}

func TestMockVerifier_VerifyAssertion_DefaultSuccess(t *testing.T) {
	m := &MockVerifier{}
	got, err := m.VerifyAssertion(context.Background(), nil, nil, nil, 7)
	if err != nil {
		t.Fatalf("VerifyAssertion returned error: %v", err)
	}
	if got != 8 {
		t.Errorf("newCounter = %d, want 8 (prevCounter+1 by default)", got)
	}
}

func TestMockVerifier_VerifyAssertion_ConfiguredCounter(t *testing.T) {
	m := &MockVerifier{NewCounter: 42}
	got, err := m.VerifyAssertion(context.Background(), nil, nil, nil, 7)
	if err != nil {
		t.Fatalf("VerifyAssertion returned error: %v", err)
	}
	if got != 42 {
		t.Errorf("newCounter = %d, want 42 (configured)", got)
	}
}

func TestMockVerifier_VerifyAssertion_ForcedError(t *testing.T) {
	m := &MockVerifier{Err: ErrAttestationInvalid}
	if _, err := m.VerifyAssertion(context.Background(), nil, nil, nil, 0); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("err = %v, want ErrAttestationInvalid", err)
	}
}

func TestAppleVerifier_VerifyAssertion_HappyPath(t *testing.T) {
	f := buildValidAssertion(t, testAppID, 5)
	v := NewAppleVerifier(testAppID, nil)

	newCounter, err := v.VerifyAssertion(context.Background(), f.publicKeyDER, f.assertion, f.clientDataHash, 4)
	if err != nil {
		t.Fatalf("VerifyAssertion returned error: %v", err)
	}
	if newCounter != 5 {
		t.Errorf("newCounter = %d, want 5", newCounter)
	}
}

func TestAppleVerifier_VerifyAssertion_BadCBOR(t *testing.T) {
	f := buildValidAssertion(t, testAppID, 5)
	v := NewAppleVerifier(testAppID, nil)

	if _, err := v.VerifyAssertion(context.Background(), f.publicKeyDER, []byte{0xff, 0x00, 0x13, 0x37}, f.clientDataHash, 0); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("err = %v, want ErrAttestationInvalid", err)
	}
}

func TestAppleVerifier_VerifyAssertion_BadSignature(t *testing.T) {
	f := buildValidAssertion(t, testAppID, 5)
	v := NewAppleVerifier(testAppID, nil)

	// A different device key's public DER cannot verify this signature.
	other := mustGenKey(t)
	otherDER, err := x509.MarshalPKIXPublicKey(&other.PublicKey)
	if err != nil {
		t.Fatalf("marshal other pub: %v", err)
	}
	if _, err := v.VerifyAssertion(context.Background(), otherDER, f.assertion, f.clientDataHash, 0); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("err = %v, want ErrAttestationInvalid", err)
	}
}

func TestAppleVerifier_VerifyAssertion_WrongAppID(t *testing.T) {
	f := buildValidAssertion(t, testAppID, 5)
	// Verifier configured with a different appID -> rpIdHash mismatch.
	v := NewAppleVerifier("ZZZZZ99999.com.other.app", nil)

	if _, err := v.VerifyAssertion(context.Background(), f.publicKeyDER, f.assertion, f.clientDataHash, 0); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("err = %v, want ErrAttestationInvalid", err)
	}
}

func TestAppleVerifier_VerifyAssertion_CounterNotIncreasing(t *testing.T) {
	// signCount equal to prevCounter must be rejected (replay).
	f := buildValidAssertion(t, testAppID, 5)
	v := NewAppleVerifier(testAppID, nil)

	if _, err := v.VerifyAssertion(context.Background(), f.publicKeyDER, f.assertion, f.clientDataHash, 5); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("equal counter: err = %v, want ErrAttestationInvalid", err)
	}
	// And a counter lower than prevCounter is also rejected.
	if _, err := v.VerifyAssertion(context.Background(), f.publicKeyDER, f.assertion, f.clientDataHash, 6); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("lower counter: err = %v, want ErrAttestationInvalid", err)
	}
}

func TestAppleVerifier_VerifyAssertion_ShortAuthData(t *testing.T) {
	f := buildValidAssertion(t, testAppID, 5)
	// Truncate authData below the 37-byte minimum, then re-sign so the failure
	// is attributable to the length check, not the signature.
	shortAuth := f.authData[:20]
	nh := sha256.New()
	nh.Write(shortAuth)
	nh.Write(f.clientDataHash)
	nonce := nh.Sum(nil)
	sig, err := ecdsa.SignASN1(rand.Reader, f.deviceKey, nonce)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	assertionCBOR, err := cbor.Marshal(assertionObject{Signature: sig, AuthenticatorData: shortAuth})
	if err != nil {
		t.Fatalf("cbor marshal: %v", err)
	}
	v := NewAppleVerifier(testAppID, nil)
	if _, err := v.VerifyAssertion(context.Background(), f.publicKeyDER, assertionCBOR, f.clientDataHash, 0); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("err = %v, want ErrAttestationInvalid", err)
	}
}

func TestAppleVerifier_VerifyAssertion_BadPublicKeyDER(t *testing.T) {
	f := buildValidAssertion(t, testAppID, 5)
	v := NewAppleVerifier(testAppID, nil)
	if _, err := v.VerifyAssertion(context.Background(), []byte("not-a-pkix-key"), f.assertion, f.clientDataHash, 0); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("err = %v, want ErrAttestationInvalid", err)
	}
}
