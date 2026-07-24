package token

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSigner_IssueParseRoundTrip(t *testing.T) {
	s := NewSigner([]byte("secret-key"))
	now := time.Unix(1_700_000_000, 0)

	tok := s.Issue(42, time.Hour, now)
	if tok == "" {
		t.Fatal("Issue returned empty token")
	}

	deviceID, err := s.Parse(tok, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if deviceID != 42 {
		t.Errorf("deviceID = %d, want 42", deviceID)
	}
}

func TestSigner_ParseTamperedRejected(t *testing.T) {
	s := NewSigner([]byte("secret-key"))
	now := time.Unix(1_700_000_000, 0)
	tok := s.Issue(42, time.Hour, now)

	// Flip a character in the middle of the token.
	b := []byte(tok)
	mid := len(b) / 2
	if b[mid] == 'A' {
		b[mid] = 'B'
	} else {
		b[mid] = 'A'
	}
	tampered := string(b)

	if _, err := s.Parse(tampered, now); !errors.Is(err, ErrTokenInvalid) {
		t.Errorf("Parse(tampered) err = %v, want ErrTokenInvalid", err)
	}
}

func TestSigner_ParseExpiredRejected(t *testing.T) {
	s := NewSigner([]byte("secret-key"))
	now := time.Unix(1_700_000_000, 0)
	tok := s.Issue(42, time.Hour, now)

	if _, err := s.Parse(tok, now.Add(2*time.Hour)); !errors.Is(err, ErrTokenExpired) {
		t.Errorf("Parse(expired) err = %v, want ErrTokenExpired", err)
	}
}

func TestSigner_ParseWrongSecretRejected(t *testing.T) {
	issuer := NewSigner([]byte("secret-key"))
	verifier := NewSigner([]byte("different-key"))
	now := time.Unix(1_700_000_000, 0)
	tok := issuer.Issue(42, time.Hour, now)

	if _, err := verifier.Parse(tok, now); !errors.Is(err, ErrTokenInvalid) {
		t.Errorf("Parse(wrong secret) err = %v, want ErrTokenInvalid", err)
	}
}

// TestSigner_ParseAuthenticButUnparsablePayload crafts tokens whose HMAC is
// genuinely valid for a payload that nonetheless fails to parse as a
// deviceID/expiry pair, exercising the post-HMAC-check parse error branches
// that a tampered (HMAC-invalid) token can never reach.
func TestSigner_ParseAuthenticButUnparsablePayload(t *testing.T) {
	s := NewSigner([]byte("secret-key"))
	now := time.Unix(1_700_000_000, 0)

	buildToken := func(idPart, expPart string) string {
		payload := idPart + "." + expPart
		mac := s.sign(payload)
		return payload + "." + enc.EncodeToString(mac)
	}

	t.Run("non-numeric deviceID", func(t *testing.T) {
		idPart := enc.EncodeToString([]byte("not-a-number"))
		expPart := enc.EncodeToString([]byte("9999999999"))
		tok := buildToken(idPart, expPart)
		if _, err := s.Parse(tok, now); !errors.Is(err, ErrTokenInvalid) {
			t.Errorf("Parse err = %v, want ErrTokenInvalid", err)
		}
	})

	t.Run("non-numeric expiry", func(t *testing.T) {
		idPart := enc.EncodeToString([]byte("42"))
		expPart := enc.EncodeToString([]byte("not-a-timestamp"))
		tok := buildToken(idPart, expPart)
		if _, err := s.Parse(tok, now); !errors.Is(err, ErrTokenInvalid) {
			t.Errorf("Parse err = %v, want ErrTokenInvalid", err)
		}
	})
}

// TestSigner_ParseMalformedSegments exercises the base64-decode error
// branches for each of the token's three dot-separated segments.
func TestSigner_ParseMalformedSegments(t *testing.T) {
	s := NewSigner([]byte("secret-key"))
	now := time.Unix(1_700_000_000, 0)

	cases := []struct {
		name string
		tok  string
	}{
		{"bad base64 deviceID segment", "not!base64.AAAA.AAAA"},
		{"bad base64 expiry segment", "AAAA.not!base64.AAAA"},
		{"bad base64 mac segment", "AAAA.AAAA.not!base64"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.Parse(tc.tok, now); !errors.Is(err, ErrTokenInvalid) {
				t.Errorf("Parse(%q) err = %v, want ErrTokenInvalid", tc.tok, err)
			}
		})
	}
}

func TestSigner_ParseMalformedRejected(t *testing.T) {
	s := NewSigner([]byte("secret-key"))
	now := time.Unix(1_700_000_000, 0)

	cases := []string{
		"",
		"not-a-token",
		"a.b",                              // too few parts
		"a.b.c.d",                          // too many parts
		strings.Repeat("x", 10) + ".1.abc", // bad deviceID base
	}
	for _, tc := range cases {
		if _, err := s.Parse(tc, now); !errors.Is(err, ErrTokenInvalid) {
			t.Errorf("Parse(%q) err = %v, want ErrTokenInvalid", tc, err)
		}
	}
}
