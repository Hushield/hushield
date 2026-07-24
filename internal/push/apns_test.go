package push

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
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"spamfilter/internal/store"
)

// fakeDoer captures the most recent request (and its body) and returns a
// canned response, so tests assert the outbound APNs request without hitting
// Apple.
type fakeDoer struct {
	status  int
	body    string
	gotReq  *http.Request
	gotBody string
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.gotReq = req
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		f.gotBody = string(b)
	}
	status := f.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Header:     make(http.Header),
	}, nil
}

// writeTestP8 generates a throwaway P-256 key, writes it as a PEM PKCS8 ".p8"
// to a temp file, and returns the path and the public key for verification.
func writeTestP8(t *testing.T) (string, *ecdsa.PublicKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	path := filepath.Join(t.TempDir(), "AuthKey_TEST.p8")
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatalf("write p8: %v", err)
	}
	return path, &key.PublicKey
}

func newTestNotifier(t *testing.T, doer httpDoer) (*APNsNotifier, *ecdsa.PublicKey) {
	t.Helper()
	path, pub := writeTestP8(t)
	n, err := NewAPNsNotifier(path, "KEYID12345", "TEAMID6789", "com.example.spamfilter")
	if err != nil {
		t.Fatalf("NewAPNsNotifier: %v", err)
	}
	n.client = doer
	return n, pub
}

func TestAPNsNotifier_SendSilentRefresh_RequestShape(t *testing.T) {
	doer := &fakeDoer{status: http.StatusOK}
	n, pub := newTestNotifier(t, doer)

	target := store.PushTarget{DeviceID: 1, Token: "devicetoken123", Environment: "production"}
	if err := n.SendSilentRefresh(context.Background(), target); err != nil {
		t.Fatalf("SendSilentRefresh: %v", err)
	}

	req := doer.gotReq
	if req == nil {
		t.Fatal("no request captured")
	}
	if got, want := req.URL.String(), "https://api.push.apple.com/3/device/devicetoken123"; got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
	if got := req.Header.Get("apns-topic"); got != "com.example.spamfilter" {
		t.Errorf("apns-topic = %q, want com.example.spamfilter", got)
	}
	if got := req.Header.Get("apns-push-type"); got != "background" {
		t.Errorf("apns-push-type = %q, want background", got)
	}
	if got := req.Header.Get("apns-priority"); got != "5" {
		t.Errorf("apns-priority = %q, want 5", got)
	}
	if got := doer.gotBody; got != `{"aps":{"content-available":1}}` {
		t.Errorf("body = %q, want content-available payload", got)
	}

	authz := req.Header.Get("authorization")
	if !strings.HasPrefix(authz, "bearer ") {
		t.Fatalf("authorization = %q, want 'bearer <jwt>'", authz)
	}
	jwt := strings.TrimPrefix(authz, "bearer ")
	verifyES256JWT(t, jwt, pub)
}

func TestAPNsNotifier_SandboxHost(t *testing.T) {
	doer := &fakeDoer{status: http.StatusOK}
	n, _ := newTestNotifier(t, doer)

	target := store.PushTarget{DeviceID: 2, Token: "sbtoken", Environment: "sandbox"}
	if err := n.SendSilentRefresh(context.Background(), target); err != nil {
		t.Fatalf("SendSilentRefresh: %v", err)
	}
	if got, want := doer.gotReq.URL.String(), "https://api.sandbox.push.apple.com/3/device/sbtoken"; got != want {
		t.Errorf("sandbox URL = %q, want %q", got, want)
	}
}

func TestAPNsNotifier_Non2xxIsError(t *testing.T) {
	doer := &fakeDoer{status: http.StatusGone, body: `{"reason":"BadDeviceToken"}`}
	n, _ := newTestNotifier(t, doer)

	err := n.SendSilentRefresh(context.Background(), store.PushTarget{DeviceID: 3, Token: "x", Environment: "production"})
	if err == nil {
		t.Fatal("SendSilentRefresh returned nil, want error on non-2xx")
	}
	if !strings.Contains(err.Error(), "410") || !strings.Contains(err.Error(), "BadDeviceToken") {
		t.Errorf("error = %v, want status 410 and reason BadDeviceToken", err)
	}
}

// verifyES256JWT asserts the JWT is a well-formed ES256 JWS whose raw R‖S
// (64-byte) signature verifies against pub, and whose header/claims match the
// configured key id, team id, and an ES256 alg.
func verifyES256JWT(t *testing.T, jwt string, pub *ecdsa.PublicKey) {
	t.Helper()
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt has %d segments, want 3", len(parts))
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		t.Fatalf("unmarshal header: %v", err)
	}
	if header.Alg != "ES256" {
		t.Errorf("header alg = %q, want ES256", header.Alg)
	}
	if header.Kid != "KEYID12345" {
		t.Errorf("header kid = %q, want KEYID12345", header.Kid)
	}

	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	var claims struct {
		Iss string `json:"iss"`
		Iat int64  `json:"iat"`
	}
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	if claims.Iss != "TEAMID6789" {
		t.Errorf("claims iss = %q, want TEAMID6789", claims.Iss)
	}
	if claims.Iat <= 0 || claims.Iat > time.Now().Unix()+5 {
		t.Errorf("claims iat = %d, want a recent unix timestamp", claims.Iat)
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if len(sig) != 64 {
		t.Fatalf("signature is %d bytes, want 64 (raw R‖S, not ASN.1/DER)", len(sig))
	}
	r := new(big.Int).SetBytes(sig[:32])
	s := new(big.Int).SetBytes(sig[32:])

	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(pub, digest[:], r, s) {
		t.Error("raw R‖S signature does not verify against the test public key")
	}
}

func TestNewAPNsNotifier_Errors(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		_, err := NewAPNsNotifier(filepath.Join(t.TempDir(), "nope.p8"), "kid", "tid", "topic")
		if err == nil {
			t.Fatal("want error for missing file, got nil")
		}
	})

	t.Run("not PEM", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "not-pem.p8")
		if err := os.WriteFile(path, []byte("this is not a pem file"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		_, err := NewAPNsNotifier(path, "kid", "tid", "topic")
		if err == nil {
			t.Fatal("want error for non-PEM content, got nil")
		}
	})

	t.Run("not PKCS8", func(t *testing.T) {
		// A well-formed PEM block whose payload is not a valid PKCS8 key.
		pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("not-a-real-pkcs8-payload")})
		path := filepath.Join(t.TempDir(), "bad-pkcs8.p8")
		if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		_, err := NewAPNsNotifier(path, "kid", "tid", "topic")
		if err == nil {
			t.Fatal("want error for malformed PKCS8 payload, got nil")
		}
	})

	t.Run("non-ECDSA key", func(t *testing.T) {
		rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("generate rsa key: %v", err)
		}
		der, err := x509.MarshalPKCS8PrivateKey(rsaKey)
		if err != nil {
			t.Fatalf("marshal pkcs8: %v", err)
		}
		pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
		path := filepath.Join(t.TempDir(), "rsa.p8")
		if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		_, err = NewAPNsNotifier(path, "kid", "tid", "topic")
		if err == nil {
			t.Fatal("want error for non-ECDSA key, got nil")
		}
		if !strings.Contains(err.Error(), "*ecdsa.PrivateKey") {
			t.Errorf("error = %v, want it to mention *ecdsa.PrivateKey", err)
		}
	})
}

// TestAPNsNotifier_DefaultClockUsesRealTime confirms clock() falls back to
// time.Now when now is unset (the zero-value APNsNotifier field), by
// checking the JWT's iat claim lands within a few seconds of the real clock.
func TestAPNsNotifier_DefaultClockUsesRealTime(t *testing.T) {
	doer := &fakeDoer{status: http.StatusOK}
	n, pub := newTestNotifier(t, doer)
	n.now = nil // exercise the default (real time.Now) branch of clock()

	target := store.PushTarget{DeviceID: 1, Token: "tok", Environment: "production"}
	if err := n.SendSilentRefresh(context.Background(), target); err != nil {
		t.Fatalf("SendSilentRefresh: %v", err)
	}
	jwt := strings.TrimPrefix(doer.gotReq.Header.Get("authorization"), "bearer ")
	verifyES256JWT(t, jwt, pub)
}

// TestAPNsNotifier_RequestError confirms a transport-level failure (the
// underlying http.Client.Do returning an error, e.g. DNS/connection refused)
// is wrapped and returned rather than swallowed.
func TestAPNsNotifier_RequestError(t *testing.T) {
	n, _ := newTestNotifier(t, &erroringDoer{})
	err := n.SendSilentRefresh(context.Background(), store.PushTarget{DeviceID: 1, Token: "x", Environment: "production"})
	if err == nil {
		t.Fatal("SendSilentRefresh: want error when the transport fails, got nil")
	}
	if !strings.Contains(err.Error(), "apns request") {
		t.Errorf("error = %v, want it to mention 'apns request'", err)
	}
}

type erroringDoer struct{}

func (erroringDoer) Do(req *http.Request) (*http.Response, error) {
	return nil, errors.New("simulated transport failure")
}

// TestApnsReason_FallsBackToRawBodyForNonJSON confirms apnsReason falls back
// to the trimmed raw body when the APNs error body isn't the expected
// {"reason": "..."} JSON shape.
func TestApnsReason_FallsBackToRawBodyForNonJSON(t *testing.T) {
	doer := &fakeDoer{status: http.StatusBadGateway, body: "  not json at all  "}
	n, _ := newTestNotifier(t, doer)

	err := n.SendSilentRefresh(context.Background(), store.PushTarget{DeviceID: 1, Token: "x", Environment: "production"})
	if err == nil {
		t.Fatal("want error for non-2xx status")
	}
	if !strings.Contains(err.Error(), "not json at all") {
		t.Errorf("error = %v, want fallback raw body 'not json at all'", err)
	}
}

func TestAPNsNotifier_JWTCachedAndRefreshed(t *testing.T) {
	doer := &fakeDoer{status: http.StatusOK}
	n, _ := newTestNotifier(t, doer)

	base := time.Unix(1_700_000_000, 0)
	current := base
	n.now = func() time.Time { return current }

	first, err := n.providerJWT()
	if err != nil {
		t.Fatalf("providerJWT: %v", err)
	}
	// Within the refresh window the same token is reused.
	current = base.Add(10 * time.Minute)
	second, err := n.providerJWT()
	if err != nil {
		t.Fatalf("providerJWT: %v", err)
	}
	if first != second {
		t.Error("JWT changed within refresh window, want cached reuse")
	}
	// Past the refresh window a fresh token is minted.
	current = base.Add(51 * time.Minute)
	third, err := n.providerJWT()
	if err != nil {
		t.Fatalf("providerJWT: %v", err)
	}
	if third == first {
		t.Error("JWT not refreshed after 51m, want a new token")
	}
}
