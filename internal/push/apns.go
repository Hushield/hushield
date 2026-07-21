package push

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"spamfilter/internal/store"
)

// APNs hosts, selected per target environment.
const (
	apnsProductionHost = "api.push.apple.com"
	apnsSandboxHost    = "api.sandbox.push.apple.com"
)

// jwtRefreshAfter is how old a provider JWT may get before it is regenerated.
// Apple accepts a token for up to ~1h; refreshing at ~50m stays well inside
// that window while still reusing a token across a broadcast.
const jwtRefreshAfter = 50 * time.Minute

// httpDoer is the minimal slice of *http.Client the APNsNotifier needs. Tests
// inject a fake to assert the outbound request without contacting Apple.
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// APNsNotifier sends silent pushes to Apple using token-based (provider JWT)
// authentication over HTTP/2.
type APNsNotifier struct {
	keyID  string
	teamID string
	topic  string
	signer *ecdsa.PrivateKey

	client httpDoer
	now    func() time.Time

	mu        sync.Mutex
	cachedJWT string
	jwtIssued time.Time
}

// NewAPNsNotifier builds an APNsNotifier from a token-based auth key. p8Path
// points at the Apple ".p8" (PEM-wrapped PKCS8 EC private key); keyID is the
// key's Key ID, teamID the Apple Team ID, topic the app bundle id sent as
// apns-topic.
func NewAPNsNotifier(p8Path, keyID, teamID, topic string) (*APNsNotifier, error) {
	pemBytes, err := os.ReadFile(p8Path)
	if err != nil {
		return nil, fmt.Errorf("push: reading APNs key %s: %w", p8Path, err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("push: APNs key %s is not PEM-encoded", p8Path)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("push: parsing APNs PKCS8 key: %w", err)
	}
	ecKey, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("push: APNs key is %T, want *ecdsa.PrivateKey", parsed)
	}

	return &APNsNotifier{
		keyID:  keyID,
		teamID: teamID,
		topic:  topic,
		signer: ecKey,
		// The stdlib http.Client negotiates HTTP/2 over TLS automatically.
		client: &http.Client{Timeout: 30 * time.Second},
		now:    time.Now,
	}, nil
}

func (a *APNsNotifier) clock() time.Time {
	if a.now != nil {
		return a.now()
	}
	return time.Now()
}

// SendSilentRefresh POSTs a background content-available push for the target
// device to the APNs host matching its environment. A 2xx returns nil; any
// other status returns an error including the apns status and reason.
func (a *APNsNotifier) SendSilentRefresh(ctx context.Context, target store.PushTarget) error {
	jwt, err := a.providerJWT()
	if err != nil {
		return err
	}

	host := apnsProductionHost
	if target.Environment == "sandbox" {
		host = apnsSandboxHost
	}
	url := fmt.Sprintf("https://%s/3/device/%s", host, target.Token)
	body := []byte(`{"aps":{"content-available":1}}`)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("authorization", "bearer "+jwt)
	req.Header.Set("apns-topic", a.topic)
	req.Header.Set("apns-push-type", "background")
	req.Header.Set("apns-priority", "5")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("push: apns request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	return fmt.Errorf("push: apns status=%d reason=%s", resp.StatusCode, apnsReason(respBody))
}

// apnsReason extracts the "reason" field from an APNs error body, falling back
// to the raw body when it is not the expected JSON shape.
func apnsReason(body []byte) string {
	var parsed struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && parsed.Reason != "" {
		return parsed.Reason
	}
	return strings.TrimSpace(string(body))
}

// providerJWT returns a cached provider JWT, regenerating it when it is older
// than jwtRefreshAfter (or has never been generated).
func (a *APNsNotifier) providerJWT() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := a.clock()
	if a.cachedJWT != "" && now.Sub(a.jwtIssued) < jwtRefreshAfter {
		return a.cachedJWT, nil
	}

	jwt, err := a.signJWT(now)
	if err != nil {
		return "", err
	}
	a.cachedJWT = jwt
	a.jwtIssued = now
	return jwt, nil
}

// signJWT builds and signs an ES256 provider JWT. Critically, the JWS
// signature is the raw R‖S concatenation (each coordinate left-padded to 32
// bytes, 64 bytes total), base64url-encoded -- NOT the ASN.1/DER form that
// ecdsa.SignASN1 emits, which APNs rejects.
func (a *APNsNotifier) signJWT(now time.Time) (string, error) {
	header := map[string]string{"alg": "ES256", "kid": a.keyID}
	claims := map[string]any{"iss": a.teamID, "iat": now.Unix()}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." +
		base64.RawURLEncoding.EncodeToString(claimsJSON)

	digest := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, a.signer, digest[:])
	if err != nil {
		return "", err
	}

	sig := jwsRawSignature(r, s)
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// jwsRawSignature encodes an ECDSA (r, s) pair as the fixed-width 64-byte
// R‖S concatenation JWS/ES256 requires: each coordinate is left-padded with
// zero bytes to 32 bytes (the P-256 field size).
func jwsRawSignature(r, s *big.Int) []byte {
	const coordLen = 32
	sig := make([]byte, 2*coordLen)
	r.FillBytes(sig[:coordLen])
	s.FillBytes(sig[coordLen:])
	return sig
}
