package attest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
)

// fakeDecoder is a integrityTokenDecoder test double: it returns whatever
// verdict was configured, keyed by the token string it was handed, so a test
// can simulate Google returning a verdict for one token and an error for
// another within the same test.
type fakeDecoder struct {
	verdicts map[string]*integrityVerdict
	err      error
}

func (f *fakeDecoder) Decode(ctx context.Context, integrityToken string) (*integrityVerdict, error) {
	if f.err != nil {
		return nil, f.err
	}
	v, ok := f.verdicts[integrityToken]
	if !ok {
		return nil, errors.New("fakeDecoder: no verdict configured for this token")
	}
	return v, nil
}

func androidAttestationEnvelope(t *testing.T, integrityToken string, pubDER []byte) []byte {
	t.Helper()
	body, err := json.Marshal(struct {
		IntegrityToken string `json:"integrity_token"`
		PublicKeyDER   string `json:"public_key_der"`
	}{
		IntegrityToken: integrityToken,
		PublicKeyDER:   base64.StdEncoding.EncodeToString(pubDER),
	})
	if err != nil {
		t.Fatalf("json.Marshal envelope: %v", err)
	}
	return body
}

func TestPlayIntegrityVerifier_VerifyAttestation_valid(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	challenge := []byte("server-challenge-bytes")
	wantNonce := sha256.Sum256(append(append([]byte{}, challenge...), pubDER...))

	decoder := &fakeDecoder{verdicts: map[string]*integrityVerdict{
		"good-token": {
			PackageName:               "com.hushield.android",
			RequestHash:               base64.StdEncoding.EncodeToString(wantNonce[:]),
			AppRecognitionVerdict:     "PLAY_RECOGNIZED",
			DeviceRecognitionVerdicts: []string{"MEETS_DEVICE_INTEGRITY"},
		},
	}}
	v := NewPlayIntegrityVerifier("com.hushield.android", decoder)

	pubDERHash := sha256.Sum256(pubDER)
	keyID := base64.StdEncoding.EncodeToString(pubDERHash[:])
	gotPubDER, _, err := v.VerifyAttestation(t.Context(), keyID, androidAttestationEnvelope(t, "good-token", pubDER), challenge)
	if err != nil {
		t.Fatalf("VerifyAttestation: %v", err)
	}
	if string(gotPubDER) != string(pubDER) {
		t.Errorf("returned public key does not match the one submitted")
	}
}

// TestPlayIntegrityVerifier_VerifyAttestation_keyIDNotBoundToPublicKey proves
// the key-binding gap: a keyID that does not derive from the submitted
// public key must be rejected, otherwise a client can claim any key_id --
// including a victim's -- for a key it does not actually own, letting
// VerifyAttestation's caller silently replace that victim's stored public
// key (see UpsertDevicePlatform).
func TestPlayIntegrityVerifier_VerifyAttestation_keyIDNotBoundToPublicKey(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	challenge := []byte("server-challenge-bytes")
	wantNonce := sha256.Sum256(append(append([]byte{}, challenge...), pubDER...))

	decoder := &fakeDecoder{verdicts: map[string]*integrityVerdict{
		"good-token": {
			PackageName:               "com.hushield.android",
			RequestHash:               base64.StdEncoding.EncodeToString(wantNonce[:]),
			AppRecognitionVerdict:     "PLAY_RECOGNIZED",
			DeviceRecognitionVerdicts: []string{"MEETS_DEVICE_INTEGRITY"},
		},
	}}
	v := NewPlayIntegrityVerifier("com.hushield.android", decoder)

	// "victims-key-id" does not derive from pubDER at all -- this is the
	// attacker claiming a victim's key_id for a key that isn't theirs.
	_, _, err = v.VerifyAttestation(t.Context(), "victims-key-id", androidAttestationEnvelope(t, "good-token", pubDER), challenge)
	if err == nil {
		t.Fatal("VerifyAttestation accepted a keyID that does not derive from the submitted public key -- key_id is not bound to the public key")
	}
}

// TestPlayIntegrityVerifier_VerifyAttestation_nonECDSAPublicKeyRejected
// proves a submitted public key that isn't an ECDSA P-256 key -- and so can
// never successfully sign a later assertion -- is rejected at enrollment
// rather than discovered only at assert time.
func TestPlayIntegrityVerifier_VerifyAttestation_nonECDSAPublicKeyRejected(t *testing.T) {
	rsaPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&rsaPriv.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	challenge := []byte("server-challenge-bytes")
	wantNonce := sha256.Sum256(append(append([]byte{}, challenge...), pubDER...))

	decoder := &fakeDecoder{verdicts: map[string]*integrityVerdict{
		"good-token": {
			PackageName:               "com.hushield.android",
			RequestHash:               base64.StdEncoding.EncodeToString(wantNonce[:]),
			AppRecognitionVerdict:     "PLAY_RECOGNIZED",
			DeviceRecognitionVerdicts: []string{"MEETS_DEVICE_INTEGRITY"},
		},
	}}
	v := NewPlayIntegrityVerifier("com.hushield.android", decoder)

	pubDERHash := sha256.Sum256(pubDER)
	keyID := base64.StdEncoding.EncodeToString(pubDERHash[:])
	_, _, err = v.VerifyAttestation(t.Context(), keyID, androidAttestationEnvelope(t, "good-token", pubDER), challenge)
	if err == nil {
		t.Fatal("VerifyAttestation accepted a non-ECDSA public key")
	}
}

func TestPlayIntegrityVerifier_VerifyAttestation_nonceMismatch(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pubDER, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	wrongNonce := sha256.Sum256([]byte("this does not match the challenge+key"))

	decoder := &fakeDecoder{verdicts: map[string]*integrityVerdict{
		"bad-token": {
			PackageName:               "com.hushield.android",
			RequestHash:               base64.StdEncoding.EncodeToString(wrongNonce[:]),
			AppRecognitionVerdict:     "PLAY_RECOGNIZED",
			DeviceRecognitionVerdicts: []string{"MEETS_DEVICE_INTEGRITY"},
		},
	}}
	v := NewPlayIntegrityVerifier("com.hushield.android", decoder)

	_, _, err := v.VerifyAttestation(t.Context(), "unused-keyid", androidAttestationEnvelope(t, "bad-token", pubDER), []byte("server-challenge-bytes"))
	if err == nil {
		t.Fatal("VerifyAttestation accepted a token whose nonce does not bind to the submitted public key")
	}
}

func TestPlayIntegrityVerifier_VerifyAttestation_wrongPackageName(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pubDER, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	challenge := []byte("server-challenge-bytes")
	nonce := sha256.Sum256(append(append([]byte{}, challenge...), pubDER...))

	decoder := &fakeDecoder{verdicts: map[string]*integrityVerdict{
		"impostor-token": {
			PackageName:               "com.attacker.evil",
			RequestHash:               base64.StdEncoding.EncodeToString(nonce[:]),
			AppRecognitionVerdict:     "PLAY_RECOGNIZED",
			DeviceRecognitionVerdicts: []string{"MEETS_DEVICE_INTEGRITY"},
		},
	}}
	v := NewPlayIntegrityVerifier("com.hushield.android", decoder)

	_, _, err := v.VerifyAttestation(t.Context(), "unused-keyid", androidAttestationEnvelope(t, "impostor-token", pubDER), challenge)
	if err == nil {
		t.Fatal("VerifyAttestation accepted a token issued for a different package name")
	}
}

func TestPlayIntegrityVerifier_VerifyAttestation_weakIntegrityVerdict(t *testing.T) {
	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pubDER, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	challenge := []byte("server-challenge-bytes")
	nonce := sha256.Sum256(append(append([]byte{}, challenge...), pubDER...))

	decoder := &fakeDecoder{verdicts: map[string]*integrityVerdict{
		"rooted-device-token": {
			PackageName:               "com.hushield.android",
			RequestHash:               base64.StdEncoding.EncodeToString(nonce[:]),
			AppRecognitionVerdict:     "PLAY_RECOGNIZED",
			DeviceRecognitionVerdicts: []string{}, // no integrity claim at all -- e.g. a rooted/tampered device
		},
	}}
	v := NewPlayIntegrityVerifier("com.hushield.android", decoder)

	_, _, err := v.VerifyAttestation(t.Context(), "unused-keyid", androidAttestationEnvelope(t, "rooted-device-token", pubDER), challenge)
	if err == nil {
		t.Fatal("VerifyAttestation accepted a token with no MEETS_*_INTEGRITY verdict")
	}
}

func TestPlayIntegrityVerifier_VerifyAttestation_decoderError(t *testing.T) {
	decoder := &fakeDecoder{err: errors.New("google api: token expired")}
	v := NewPlayIntegrityVerifier("com.hushield.android", decoder)

	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pubDER, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)

	_, _, err := v.VerifyAttestation(t.Context(), "unused-keyid", androidAttestationEnvelope(t, "expired-token", pubDER), []byte("challenge"))
	if err == nil {
		t.Fatal("VerifyAttestation swallowed a decoder error")
	}
}
