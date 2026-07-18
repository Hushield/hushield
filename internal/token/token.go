// Package token issues and verifies stateless, tamper-evident device tokens.
//
// A token is an opaque string of the form:
//
//	base64url(deviceID) "." base64url(expiryUnix) "." base64url(HMAC-SHA256)
//
// The HMAC is computed over "deviceID.expiryUnix" (the first two segments)
// with a server-held secret, so the token carries its own identity and
// expiry without any server-side session state.
package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

// ErrTokenInvalid is returned when a token is malformed or fails HMAC
// verification (including wrong-secret and tampered tokens).
var ErrTokenInvalid = errors.New("token: invalid")

// ErrTokenExpired is returned when a token's HMAC is valid but its expiry
// has passed.
var ErrTokenExpired = errors.New("token: expired")

// Signer issues and verifies device tokens with a fixed HMAC secret.
type Signer struct {
	secret []byte
}

// NewSigner returns a Signer that signs and verifies with the given secret.
func NewSigner(secret []byte) *Signer {
	// Copy so callers cannot mutate the secret out from under us.
	s := make([]byte, len(secret))
	copy(s, secret)
	return &Signer{secret: s}
}

var enc = base64.RawURLEncoding

// Issue returns a token authenticating deviceID that expires ttl after now.
func (s *Signer) Issue(deviceID uint64, ttl time.Duration, now time.Time) string {
	expiry := now.Add(ttl).Unix()
	payload := s.payload(deviceID, expiry)
	mac := s.sign(payload)
	return payload + "." + enc.EncodeToString(mac)
}

// Parse verifies a token's HMAC (constant-time) and expiry, returning the
// encoded deviceID. It returns ErrTokenInvalid for malformed or unauthentic
// tokens and ErrTokenExpired for authentic-but-expired tokens.
func (s *Signer) Parse(tok string, now time.Time) (uint64, error) {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return 0, ErrTokenInvalid
	}

	idBytes, err := enc.DecodeString(parts[0])
	if err != nil {
		return 0, ErrTokenInvalid
	}
	expBytes, err := enc.DecodeString(parts[1])
	if err != nil {
		return 0, ErrTokenInvalid
	}
	gotMAC, err := enc.DecodeString(parts[2])
	if err != nil {
		return 0, ErrTokenInvalid
	}

	payload := parts[0] + "." + parts[1]
	wantMAC := s.sign(payload)
	if !hmac.Equal(gotMAC, wantMAC) {
		return 0, ErrTokenInvalid
	}

	deviceID, err := strconv.ParseUint(string(idBytes), 10, 64)
	if err != nil {
		return 0, ErrTokenInvalid
	}
	expiry, err := strconv.ParseInt(string(expBytes), 10, 64)
	if err != nil {
		return 0, ErrTokenInvalid
	}

	if now.Unix() >= expiry {
		return 0, ErrTokenExpired
	}

	return deviceID, nil
}

func (s *Signer) payload(deviceID uint64, expiry int64) string {
	id := enc.EncodeToString([]byte(strconv.FormatUint(deviceID, 10)))
	exp := enc.EncodeToString([]byte(strconv.FormatInt(expiry, 10)))
	return id + "." + exp
}

func (s *Signer) sign(payload string) []byte {
	m := hmac.New(sha256.New, s.secret)
	m.Write([]byte(payload))
	return m.Sum(nil)
}
