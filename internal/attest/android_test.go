package attest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"testing"
)

func generateTestKey(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	return priv, pubDER
}

func signTestAssertion(t *testing.T, priv *ecdsa.PrivateKey, clientDataHash []byte, counter uint32) []byte {
	t.Helper()
	var counterBytes [4]byte
	binary.BigEndian.PutUint32(counterBytes[:], counter)
	h := sha256.New()
	h.Write(clientDataHash)
	h.Write(counterBytes[:])
	message := h.Sum(nil)

	sig, err := ecdsa.SignASN1(rand.Reader, priv, message)
	if err != nil {
		t.Fatalf("ecdsa.SignASN1: %v", err)
	}
	body, err := json.Marshal(androidAssertion{Counter: counter, Signature: sig})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return body
}

func TestPlayIntegrityVerifier_VerifyAssertion_valid(t *testing.T) {
	priv, pubDER := generateTestKey(t)
	clientDataHash := sha256.Sum256([]byte("challenge-bytes"))
	assertion := signTestAssertion(t, priv, clientDataHash[:], 5)

	v := &PlayIntegrityVerifier{}
	newCounter, err := v.VerifyAssertion(t.Context(), pubDER, assertion, clientDataHash[:], 4)
	if err != nil {
		t.Fatalf("VerifyAssertion: %v", err)
	}
	if newCounter != 5 {
		t.Errorf("newCounter = %d, want 5", newCounter)
	}
}

func TestPlayIntegrityVerifier_VerifyAssertion_counterNotIncreasing(t *testing.T) {
	priv, pubDER := generateTestKey(t)
	clientDataHash := sha256.Sum256([]byte("challenge-bytes"))
	assertion := signTestAssertion(t, priv, clientDataHash[:], 4)

	v := &PlayIntegrityVerifier{}
	if _, err := v.VerifyAssertion(t.Context(), pubDER, assertion, clientDataHash[:], 4); err == nil {
		t.Fatal("VerifyAssertion accepted counter 4 after prevCounter 4; replay protection is broken")
	}
}

func TestPlayIntegrityVerifier_VerifyAssertion_wrongKeySignature(t *testing.T) {
	_, pubDER := generateTestKey(t)
	otherPriv, _ := generateTestKey(t)
	clientDataHash := sha256.Sum256([]byte("challenge-bytes"))
	assertion := signTestAssertion(t, otherPriv, clientDataHash[:], 5) // signed by the WRONG key

	v := &PlayIntegrityVerifier{}
	if _, err := v.VerifyAssertion(t.Context(), pubDER, assertion, clientDataHash[:], 4); err == nil {
		t.Fatal("VerifyAssertion accepted a signature from a key that does not match publicKeyDER")
	}
}

func TestPlayIntegrityVerifier_VerifyAssertion_malformedJSON(t *testing.T) {
	_, pubDER := generateTestKey(t)
	clientDataHash := sha256.Sum256([]byte("challenge-bytes"))

	v := &PlayIntegrityVerifier{}
	if _, err := v.VerifyAssertion(t.Context(), pubDER, []byte("not json"), clientDataHash[:], 4); err == nil {
		t.Fatal("VerifyAssertion accepted malformed assertion bytes")
	}
}
