package attest

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
)

const testAppID = "ABCDE12345.com.example.spamfilter"

// attestFixture holds every input/output needed to build and tweak a valid
// App Attest attestation object for testing.
type attestFixture struct {
	roots         *x509.CertPool
	keyID         string
	keyIDBytes    []byte
	challenge     []byte
	authData      []byte
	deviceKey     *ecdsa.PrivateKey
	credCertDER   []byte
	interCertDER  []byte
	receipt       []byte
	attestationOb []byte // CBOR
}

func mustGenKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return k
}

func buildValidAttestation(t *testing.T) attestFixture {
	t.Helper()

	now := time.Now()

	// Root CA.
	rootKey := mustGenKey(t)
	rootTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test App Attest Root"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTmpl, rootTmpl, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	rootCert, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatalf("parse root: %v", err)
	}

	// Intermediate CA.
	interKey := mustGenKey(t)
	interTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "Test App Attest CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	interDER, err := x509.CreateCertificate(rand.Reader, interTmpl, rootCert, &interKey.PublicKey, rootKey)
	if err != nil {
		t.Fatalf("create intermediate: %v", err)
	}
	interCert, err := x509.ParseCertificate(interDER)
	if err != nil {
		t.Fatalf("parse intermediate: %v", err)
	}

	// Device key + key identifier.
	deviceKey := mustGenKey(t)
	ecdhPub, err := deviceKey.PublicKey.ECDH()
	if err != nil {
		t.Fatalf("device key ECDH: %v", err)
	}
	keyIdentifier := sha256.Sum256(ecdhPub.Bytes())
	keyID := base64.StdEncoding.EncodeToString(keyIdentifier[:])

	// Challenge and authData.
	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		t.Fatalf("rand challenge: %v", err)
	}
	authData := buildAuthData(testAppID, aaguidProd, keyIdentifier[:])

	// Expected nonce.
	clientDataHash := sha256.Sum256(challenge)
	nh := sha256.New()
	nh.Write(authData)
	nh.Write(clientDataHash[:])
	nonce := nh.Sum(nil)

	// credCert with the nonce extension, signed by the intermediate.
	extDER, err := asn1.Marshal(nonceExtension{Nonce: nonce})
	if err != nil {
		t.Fatalf("marshal nonce ext: %v", err)
	}
	credTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(3),
		Subject:      pkix.Name{CommonName: "Test Device Cred"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(24 * time.Hour),
		ExtraExtensions: []pkix.Extension{
			{Id: oidAppAttestNonce, Value: extDER},
		},
	}
	credDER, err := x509.CreateCertificate(rand.Reader, credTmpl, interCert, &deviceKey.PublicKey, interKey)
	if err != nil {
		t.Fatalf("create credCert: %v", err)
	}

	receipt := []byte("test-receipt-bytes")
	attCBOR, err := cbor.Marshal(attestationObject{
		Fmt: "apple-appattest",
		AttStmt: attestStatement{
			X5C:     [][]byte{credDER, interDER},
			Receipt: receipt,
		},
		AuthData: authData,
	})
	if err != nil {
		t.Fatalf("cbor marshal: %v", err)
	}

	roots := x509.NewCertPool()
	roots.AddCert(rootCert)

	return attestFixture{
		roots:         roots,
		keyID:         keyID,
		keyIDBytes:    keyIdentifier[:],
		challenge:     challenge,
		authData:      authData,
		deviceKey:     deviceKey,
		credCertDER:   credDER,
		interCertDER:  interDER,
		receipt:       receipt,
		attestationOb: attCBOR,
	}
}

func buildAuthData(appID string, aaguid, credID []byte) []byte {
	rpIDHash := sha256.Sum256([]byte(appID))
	buf := bytes.Buffer{}
	buf.Write(rpIDHash[:])        // 32
	buf.WriteByte(0x00)           // flags
	buf.Write([]byte{0, 0, 0, 0}) // signCount
	buf.Write(aaguid)             // 16
	credLen := make([]byte, 2)
	binary.BigEndian.PutUint16(credLen, uint16(len(credID)))
	buf.Write(credLen)
	buf.Write(credID)
	return buf.Bytes()
}

func TestAppleVerifier_HappyPath(t *testing.T) {
	f := buildValidAttestation(t)
	v := NewAppleVerifier(testAppID, f.roots)

	pubDER, receipt, err := v.VerifyAttestation(context.Background(), f.keyID, f.attestationOb, f.challenge)
	if err != nil {
		t.Fatalf("VerifyAttestation returned error: %v", err)
	}

	if !bytes.Equal(receipt, f.receipt) {
		t.Errorf("receipt = %q, want %q", receipt, f.receipt)
	}

	wantDER, err := x509.MarshalPKIXPublicKey(&f.deviceKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal want pubkey: %v", err)
	}
	if !bytes.Equal(pubDER, wantDER) {
		t.Errorf("returned public key DER does not match device public key")
	}
}

func TestAppleVerifier_BadCBOR(t *testing.T) {
	f := buildValidAttestation(t)
	v := NewAppleVerifier(testAppID, f.roots)

	_, _, err := v.VerifyAttestation(context.Background(), f.keyID, []byte{0xff, 0x00, 0x13, 0x37}, f.challenge)
	if !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("err = %v, want ErrAttestationInvalid", err)
	}
}

func TestAppleVerifier_EmptyX5C(t *testing.T) {
	f := buildValidAttestation(t)
	att, err := cbor.Marshal(attestationObject{
		Fmt:      "apple-appattest",
		AttStmt:  attestStatement{X5C: nil, Receipt: f.receipt},
		AuthData: f.authData,
	})
	if err != nil {
		t.Fatalf("cbor marshal: %v", err)
	}
	v := NewAppleVerifier(testAppID, f.roots)

	if _, _, err := v.VerifyAttestation(context.Background(), f.keyID, att, f.challenge); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("err = %v, want ErrAttestationInvalid", err)
	}
}

func TestAppleVerifier_WrongFmt(t *testing.T) {
	f := buildValidAttestation(t)
	att, err := cbor.Marshal(attestationObject{
		Fmt:      "not-apple",
		AttStmt:  attestStatement{X5C: [][]byte{f.credCertDER, f.interCertDER}, Receipt: f.receipt},
		AuthData: f.authData,
	})
	if err != nil {
		t.Fatalf("cbor marshal: %v", err)
	}
	v := NewAppleVerifier(testAppID, f.roots)

	if _, _, err := v.VerifyAttestation(context.Background(), f.keyID, att, f.challenge); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("err = %v, want ErrAttestationInvalid", err)
	}
}

func TestAppleVerifier_NonceMismatch(t *testing.T) {
	f := buildValidAttestation(t)
	v := NewAppleVerifier(testAppID, f.roots)

	// A different challenge yields a different nonce than the one baked into
	// the credCert extension.
	wrongChallenge := make([]byte, 32)
	if _, err := rand.Read(wrongChallenge); err != nil {
		t.Fatalf("rand: %v", err)
	}
	if _, _, err := v.VerifyAttestation(context.Background(), f.keyID, f.attestationOb, wrongChallenge); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("err = %v, want ErrAttestationInvalid", err)
	}
}

func TestAppleVerifier_WrongAppID(t *testing.T) {
	f := buildValidAttestation(t)
	// Verifier configured with a different appID → rpIdHash mismatch.
	v := NewAppleVerifier("ZZZZZ99999.com.other.app", f.roots)

	if _, _, err := v.VerifyAttestation(context.Background(), f.keyID, f.attestationOb, f.challenge); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("err = %v, want ErrAttestationInvalid", err)
	}
}

func TestAppleVerifier_KeyIDMismatch(t *testing.T) {
	f := buildValidAttestation(t)
	v := NewAppleVerifier(testAppID, f.roots)

	bogus := sha256.Sum256([]byte("not the key"))
	bogusKeyID := base64.StdEncoding.EncodeToString(bogus[:])

	if _, _, err := v.VerifyAttestation(context.Background(), bogusKeyID, f.attestationOb, f.challenge); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("err = %v, want ErrAttestationInvalid", err)
	}
}

func TestAppleVerifier_UntrustedRoot(t *testing.T) {
	f := buildValidAttestation(t)
	// Empty root pool → chain cannot be verified.
	v := NewAppleVerifier(testAppID, x509.NewCertPool())

	if _, _, err := v.VerifyAttestation(context.Background(), f.keyID, f.attestationOb, f.challenge); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("err = %v, want ErrAttestationInvalid", err)
	}
}

func TestDefaultAppleRoots_Parses(t *testing.T) {
	pool := DefaultAppleRoots()
	if pool == nil {
		t.Fatal("DefaultAppleRoots returned nil")
	}
}

// buildCredCertWithExtensions builds a fresh device key + credCert (signed by
// a fresh, self-issued intermediate under its own root) carrying whatever
// extraExtensions the caller supplies in place of the usual nonce extension,
// so nonceFromCert's error branches (missing/malformed/trailing-bytes) can be
// exercised directly, independent of VerifyAttestation's other steps.
func buildCredCertWithExtensions(t *testing.T, extraExtensions []pkix.Extension) *x509.Certificate {
	t.Helper()
	now := time.Now()

	rootKey := mustGenKey(t)
	rootTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test Root"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTmpl, rootTmpl, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	rootCert, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatalf("parse root: %v", err)
	}

	credKey := mustGenKey(t)
	credTmpl := &x509.Certificate{
		SerialNumber:    big.NewInt(2),
		Subject:         pkix.Name{CommonName: "Test Cred"},
		NotBefore:       now.Add(-time.Hour),
		NotAfter:        now.Add(24 * time.Hour),
		ExtraExtensions: extraExtensions,
	}
	credDER, err := x509.CreateCertificate(rand.Reader, credTmpl, rootCert, &credKey.PublicKey, rootKey)
	if err != nil {
		t.Fatalf("create credCert: %v", err)
	}
	credCert, err := x509.ParseCertificate(credDER)
	if err != nil {
		t.Fatalf("parse credCert: %v", err)
	}
	return credCert
}

func TestNonceFromCert_ExtensionNotFound(t *testing.T) {
	cert := buildCredCertWithExtensions(t, nil)
	if _, err := nonceFromCert(cert); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("err = %v, want ErrAttestationInvalid", err)
	}
}

func TestNonceFromCert_MalformedASN1(t *testing.T) {
	cert := buildCredCertWithExtensions(t, []pkix.Extension{
		{Id: oidAppAttestNonce, Value: []byte("not valid asn1 at all")},
	})
	if _, err := nonceFromCert(cert); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("err = %v, want ErrAttestationInvalid", err)
	}
}

func TestNonceFromCert_TrailingBytes(t *testing.T) {
	nonce := sha256.Sum256([]byte("some nonce"))
	valid, err := asn1.Marshal(nonceExtension{Nonce: nonce[:]})
	if err != nil {
		t.Fatalf("marshal nonce ext: %v", err)
	}
	withTrailing := append(valid, 0xDE, 0xAD, 0xBE, 0xEF)
	cert := buildCredCertWithExtensions(t, []pkix.Extension{
		{Id: oidAppAttestNonce, Value: withTrailing},
	})
	if _, err := nonceFromCert(cert); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("err = %v, want ErrAttestationInvalid", err)
	}
}

func TestValidateAuthData_TooShort(t *testing.T) {
	f := buildValidAttestation(t)
	v := NewAppleVerifier(testAppID, f.roots)
	if err := v.validateAuthData(f.authData[:10], f.keyIDBytes); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("err = %v, want ErrAttestationInvalid", err)
	}
}

func TestValidateAuthData_UnexpectedAAGUID(t *testing.T) {
	f := buildValidAttestation(t)
	v := NewAppleVerifier(testAppID, f.roots)
	badAuthData := buildAuthData(testAppID, []byte("not-a-known-aaguid"), f.keyIDBytes)
	if err := v.validateAuthData(badAuthData, f.keyIDBytes); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("err = %v, want ErrAttestationInvalid", err)
	}
}

func TestValidateAuthData_CredentialIDLongerThanAuthData(t *testing.T) {
	f := buildValidAttestation(t)
	v := NewAppleVerifier(testAppID, f.roots)
	// authData declares a 16-byte credentialId (credIdLen) but the buffer
	// actually ends right after the length field, so authData is shorter than
	// the declared credentialId.
	rpIDHash := sha256.Sum256([]byte(testAppID))
	buf := bytes.Buffer{}
	buf.Write(rpIDHash[:])
	buf.WriteByte(0x00)
	buf.Write([]byte{0, 0, 0, 0})
	buf.Write(aaguidProd)
	buf.Write([]byte{0, 16}) // declares 16 bytes of credentialId
	// ... but no credentialId bytes follow.
	if err := v.validateAuthData(buf.Bytes(), f.keyIDBytes); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("err = %v, want ErrAttestationInvalid", err)
	}
}

func TestValidateAuthData_CredentialIDMismatch(t *testing.T) {
	f := buildValidAttestation(t)
	v := NewAppleVerifier(testAppID, f.roots)
	wrongCredID := sha256.Sum256([]byte("not the right key"))
	badAuthData := buildAuthData(testAppID, aaguidProd, wrongCredID[:])
	if err := v.validateAuthData(badAuthData, f.keyIDBytes); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("err = %v, want ErrAttestationInvalid", err)
	}
}

// TestAppleVerifier_ParseCredCertFails confirms an unparsable X5C[0] entry
// (malformed DER) is rejected before any chain verification is attempted.
func TestAppleVerifier_ParseCredCertFails(t *testing.T) {
	f := buildValidAttestation(t)
	att, err := cbor.Marshal(attestationObject{
		Fmt:      "apple-appattest",
		AttStmt:  attestStatement{X5C: [][]byte{[]byte("not a real certificate"), f.interCertDER}, Receipt: f.receipt},
		AuthData: f.authData,
	})
	if err != nil {
		t.Fatalf("cbor marshal: %v", err)
	}
	v := NewAppleVerifier(testAppID, f.roots)
	if _, _, err := v.VerifyAttestation(context.Background(), f.keyID, att, f.challenge); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("err = %v, want ErrAttestationInvalid", err)
	}
}

// TestAppleVerifier_ParseIntermediateFails confirms an unparsable X5C[1]
// entry (malformed DER) is rejected before chain verification.
func TestAppleVerifier_ParseIntermediateFails(t *testing.T) {
	f := buildValidAttestation(t)
	att, err := cbor.Marshal(attestationObject{
		Fmt:      "apple-appattest",
		AttStmt:  attestStatement{X5C: [][]byte{f.credCertDER, []byte("not a real certificate")}, Receipt: f.receipt},
		AuthData: f.authData,
	})
	if err != nil {
		t.Fatalf("cbor marshal: %v", err)
	}
	v := NewAppleVerifier(testAppID, f.roots)
	if _, _, err := v.VerifyAttestation(context.Background(), f.keyID, att, f.challenge); !errors.Is(err, ErrAttestationInvalid) {
		t.Errorf("err = %v, want ErrAttestationInvalid", err)
	}
}
