package attest

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/asn1"
	"encoding/base64"
	"encoding/binary"
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

//go:embed apple_app_attest_root_ca.pem
var appleAppAttestRootPEM []byte

// oidAppAttestNonce is the credCert extension OID whose value wraps the
// App Attest nonce.
var oidAppAttestNonce = asn1.ObjectIdentifier{1, 2, 840, 113635, 100, 8, 2}

// aaguidProd and aaguidDev are the accepted App Attest AAGUID values found in
// authData's attested credential data.
var (
	aaguidProd = []byte("appattest\x00\x00\x00\x00\x00\x00\x00")
	aaguidDev  = []byte("appattestdevelop")
)

// DefaultAppleRoots returns a cert pool containing Apple's App Attest Root CA.
func DefaultAppleRoots() *x509.CertPool {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(appleAppAttestRootPEM) {
		// The embedded PEM is a compile-time constant; failing to parse it is
		// a build/packaging bug, not a runtime condition.
		panic("attest: failed to parse embedded Apple App Attest Root CA")
	}
	return pool
}

// AppleVerifier performs real Apple App Attest attestation verification. It
// fails closed: any deviation from Apple's documented steps returns an error.
type AppleVerifier struct {
	appID   string
	roots   *x509.CertPool
	appHash [32]byte
}

// NewAppleVerifier constructs an AppleVerifier for the given appID
// ("<TeamID>.<BundleID>"). If roots is nil, Apple's embedded root is used.
func NewAppleVerifier(appID string, roots *x509.CertPool) *AppleVerifier {
	if roots == nil {
		roots = DefaultAppleRoots()
	}
	return &AppleVerifier{
		appID:   appID,
		roots:   roots,
		appHash: sha256.Sum256([]byte(appID)),
	}
}

// attestationObject is the CBOR top-level structure.
type attestationObject struct {
	Fmt      string          `cbor:"fmt"`
	AttStmt  attestStatement `cbor:"attStmt"`
	AuthData []byte          `cbor:"authData"`
}

type attestStatement struct {
	X5C     [][]byte `cbor:"x5c"`
	Receipt []byte   `cbor:"receipt"`
}

// nonceExtension models SEQUENCE { [1] EXPLICIT OCTET STRING } inside the
// credCert's App Attest extension.
type nonceExtension struct {
	Nonce []byte `asn1:"explicit,tag:1"`
}

// VerifyAttestation implements Verifier using Apple's documented algorithm.
func (v *AppleVerifier) VerifyAttestation(ctx context.Context, keyID string, attestationCBOR, challenge []byte) ([]byte, []byte, error) {
	// Step 1: decode CBOR and validate the format tag.
	var obj attestationObject
	if err := cbor.Unmarshal(attestationCBOR, &obj); err != nil {
		return nil, nil, fmt.Errorf("%w: cbor decode: %v", ErrAttestationInvalid, err)
	}
	if obj.Fmt != "apple-appattest" {
		return nil, nil, fmt.Errorf("%w: unexpected fmt %q", ErrAttestationInvalid, obj.Fmt)
	}
	if len(obj.AttStmt.X5C) < 2 {
		return nil, nil, fmt.Errorf("%w: x5c must contain credCert and intermediate", ErrAttestationInvalid)
	}

	// Step 2: verify the x5c chain up to roots.
	credCert, err := x509.ParseCertificate(obj.AttStmt.X5C[0])
	if err != nil {
		return nil, nil, fmt.Errorf("%w: parse credCert: %v", ErrAttestationInvalid, err)
	}
	intermediates := x509.NewCertPool()
	for _, der := range obj.AttStmt.X5C[1:] {
		interCert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: parse intermediate: %v", ErrAttestationInvalid, err)
		}
		intermediates.AddCert(interCert)
	}
	if _, err := credCert.Verify(x509.VerifyOptions{
		Roots:         v.roots,
		Intermediates: intermediates,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}); err != nil {
		return nil, nil, fmt.Errorf("%w: chain verify: %v", ErrAttestationInvalid, err)
	}

	// Step 3: compute the expected nonce.
	clientDataHash := sha256.Sum256(challenge)
	h := sha256.New()
	h.Write(obj.AuthData)
	h.Write(clientDataHash[:])
	nonce := h.Sum(nil)

	// Step 4: extract and compare the nonce from the credCert extension.
	certNonce, err := nonceFromCert(credCert)
	if err != nil {
		return nil, nil, err
	}
	if !bytes.Equal(certNonce, nonce) {
		return nil, nil, fmt.Errorf("%w: nonce mismatch", ErrAttestationInvalid)
	}

	// Step 5: verify the key identifier matches keyID.
	pubKey, ok := credCert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, nil, fmt.Errorf("%w: credCert public key is not ECDSA", ErrAttestationInvalid)
	}
	ecdhKey, err := pubKey.ECDH()
	if err != nil {
		return nil, nil, fmt.Errorf("%w: credCert key not on P-256: %v", ErrAttestationInvalid, err)
	}
	rawPoint := ecdhKey.Bytes() // uncompressed: 0x04 || X || Y
	keyIdentifier := sha256.Sum256(rawPoint)
	if !keyIDMatches(keyID, keyIdentifier[:]) {
		return nil, nil, fmt.Errorf("%w: keyID mismatch", ErrAttestationInvalid)
	}

	// Step 6: parse and validate authData.
	if err := v.validateAuthData(obj.AuthData, keyIdentifier[:]); err != nil {
		return nil, nil, err
	}

	// Step 7: return the credCert public key in PKIX-DER form plus the receipt.
	pkixDER, err := x509.MarshalPKIXPublicKey(pubKey)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: marshal public key: %v", ErrAttestationInvalid, err)
	}
	return pkixDER, obj.AttStmt.Receipt, nil
}

func nonceFromCert(cert *x509.Certificate) ([]byte, error) {
	for _, ext := range cert.Extensions {
		if !ext.Id.Equal(oidAppAttestNonce) {
			continue
		}
		var ne nonceExtension
		rest, err := asn1.Unmarshal(ext.Value, &ne)
		if err != nil {
			return nil, fmt.Errorf("%w: decode nonce extension: %v", ErrAttestationInvalid, err)
		}
		if len(rest) != 0 {
			return nil, fmt.Errorf("%w: trailing bytes in nonce extension", ErrAttestationInvalid)
		}
		return ne.Nonce, nil
	}
	return nil, fmt.Errorf("%w: nonce extension not found", ErrAttestationInvalid)
}

// validateAuthData checks the rpIdHash, AAGUID, and credentialId per Apple's
// spec. authData layout:
//
//	rpIdHash[32] || flags[1] || signCount[4] ||
//	aaguid[16] || credIdLen[2] || credId[credIdLen] || COSE key...
func (v *AppleVerifier) validateAuthData(authData, keyIDBytes []byte) error {
	const minLen = 32 + 1 + 4 + 16 + 2
	if len(authData) < minLen {
		return fmt.Errorf("%w: authData too short", ErrAttestationInvalid)
	}

	rpIDHash := authData[0:32]
	if !bytes.Equal(rpIDHash, v.appHash[:]) {
		return fmt.Errorf("%w: rpIdHash != SHA256(appID)", ErrAttestationInvalid)
	}

	aaguid := authData[37:53]
	if !bytes.Equal(aaguid, aaguidProd) && !bytes.Equal(aaguid, aaguidDev) {
		return fmt.Errorf("%w: unexpected aaguid", ErrAttestationInvalid)
	}

	credIDLen := int(binary.BigEndian.Uint16(authData[53:55]))
	if len(authData) < 55+credIDLen {
		return fmt.Errorf("%w: authData shorter than declared credentialId", ErrAttestationInvalid)
	}
	credID := authData[55 : 55+credIDLen]
	if !bytes.Equal(credID, keyIDBytes) {
		return fmt.Errorf("%w: credentialId != keyId", ErrAttestationInvalid)
	}

	return nil
}

// keyIDMatches reports whether keyID (standard or raw base64) decodes to want.
func keyIDMatches(keyID string, want []byte) bool {
	for _, e := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if got, err := e.DecodeString(keyID); err == nil && bytes.Equal(got, want) {
			return true
		}
	}
	return false
}
