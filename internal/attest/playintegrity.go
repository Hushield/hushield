package attest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
)

// integrityVerdict is the subset of Google's decoded Play Integrity token
// this verifier checks. See
// https://developer.android.com/google/play/integrity/verdict for the full
// shape; fields not needed for a pass/fail decision here are omitted.
type integrityVerdict struct {
	PackageName               string
	RequestHash               string
	AppRecognitionVerdict     string
	DeviceRecognitionVerdicts []string
}

// integrityTokenDecoder decodes a raw Play Integrity token into its verdict.
// The seam that lets tests avoid a real call to Google's API.
type integrityTokenDecoder interface {
	Decode(ctx context.Context, integrityToken string) (*integrityVerdict, error)
}

// androidIntegrityEnvelope is what the Android client sends as
// VerifyAttestation's attestationEnvelope argument: the raw Play Integrity
// token plus the client's own public key, base64-encoded. Google's token has
// no way to carry an arbitrary application-defined public key itself, so this
// envelope is this server's own wire format, not Google's.
//
// Named distinctly from the androidAttestationEnvelope test helper in
// playintegrity_test.go, which builds this same JSON shape but as a function,
// not a type -- the two names would otherwise collide in this package.
type androidIntegrityEnvelope struct {
	IntegrityToken string `json:"integrity_token"`
	PublicKeyDER   string `json:"public_key_der"`
}

// hasIntegrityVerdict reports whether verdicts contains any recognized
// "meets integrity" value. Google documents several tiers
// (MEETS_DEVICE_INTEGRITY, MEETS_BASIC_INTEGRITY, MEETS_STRONG_INTEGRITY,
// MEETS_VIRTUAL_INTEGRITY); this verifier accepts any non-empty verdict list
// as a v1 floor and defers tightening to a specific tier to a follow-up once
// real device data shows what legitimate users actually present.
func hasIntegrityVerdict(verdicts []string) bool {
	return len(verdicts) > 0
}

// VerifyAttestation implements Verifier for Android's Play Integrity flow.
func (v *PlayIntegrityVerifier) VerifyAttestation(ctx context.Context, keyID string, attestationEnvelope, challenge []byte) ([]byte, []byte, error) {
	var envelope androidIntegrityEnvelope
	if err := json.Unmarshal(attestationEnvelope, &envelope); err != nil {
		return nil, nil, fmt.Errorf("%w: decode attestation envelope: %v", ErrAttestationInvalid, err)
	}

	pubDER, err := base64.StdEncoding.DecodeString(envelope.PublicKeyDER)
	if err != nil || len(pubDER) == 0 {
		return nil, nil, fmt.Errorf("%w: public_key_der must be valid non-empty base64", ErrAttestationInvalid)
	}

	verdict, err := v.decoder.Decode(ctx, envelope.IntegrityToken)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: decode integrity token: %v", ErrAttestationInvalid, err)
	}

	if verdict.PackageName != v.packageName {
		return nil, nil, fmt.Errorf("%w: token package name %q does not match %q", ErrAttestationInvalid, verdict.PackageName, v.packageName)
	}

	wantNonce := sha256.Sum256(append(append([]byte{}, challenge...), pubDER...))
	gotNonce, err := base64.StdEncoding.DecodeString(verdict.RequestHash)
	if err != nil || !bytes.Equal(gotNonce, wantNonce[:]) {
		return nil, nil, fmt.Errorf("%w: token nonce does not bind to challenge and public key", ErrAttestationInvalid)
	}

	if verdict.AppRecognitionVerdict != "PLAY_RECOGNIZED" {
		return nil, nil, fmt.Errorf("%w: app recognition verdict %q", ErrAttestationInvalid, verdict.AppRecognitionVerdict)
	}
	if !hasIntegrityVerdict(verdict.DeviceRecognitionVerdicts) {
		return nil, nil, fmt.Errorf("%w: device recognition verdict does not meet any integrity tier", ErrAttestationInvalid)
	}

	// The verified token itself is the receipt, mirroring what AppleVerifier
	// stores from obj.AttStmt.Receipt -- an auditable record of what was
	// verified, without being PII (it describes the app/device, not a person).
	return pubDER, attestationEnvelope, nil
}

// googlePlayIntegrityDecoder calls Google's Play Integrity API to decode a
// token. This talks to a real Google endpoint; there is no way to unit-test
// it without either a live credential or faking the HTTP layer (which would
// only test the fake), so Decode is intentionally unimplemented here -- see
// the TODO below. packageName is the Android package this decoder decodes
// tokens for, used to build the decodeIntegrityToken URL once implemented.
type googlePlayIntegrityDecoder struct {
	packageName string
	httpClient  *http.Client
}

// newGooglePlayIntegrityDecoder constructs a googlePlayIntegrityDecoder for
// packageName. It does not yet perform any credential loading or network
// setup -- see the TODO on Decode.
func newGooglePlayIntegrityDecoder(packageName string, httpClient *http.Client) *googlePlayIntegrityDecoder {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &googlePlayIntegrityDecoder{packageName: packageName, httpClient: httpClient}
}

// TODO(android): implement this against Google's current Play Integrity API
// docs before ATTEST_MODE=android or ATTEST_MODE=both is used against real
// devices. This must POST to
// playintegrity.googleapis.com/v1/{packageName}:decodeIntegrityToken,
// authenticated via OAuth2 using a service-account credential scoped to
// https://www.googleapis.com/auth/playintegrity, and unmarshal the response
// into an integrityVerdict. Until this is implemented, those two modes fail
// closed at startup or at first use (this error), rather than silently
// accepting bad attestations.
func (d *googlePlayIntegrityDecoder) Decode(ctx context.Context, integrityToken string) (*integrityVerdict, error) {
	return nil, fmt.Errorf("attest: googlePlayIntegrityDecoder.Decode not yet implemented -- see TODO")
}
