package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"spamfilter/internal/token"
)

// reqCtx returns a context carrying a request id, mimicking RequestIDMiddleware.
func reqCtx() context.Context {
	return context.WithValue(context.Background(), requestIDContextKey, "test-request-id")
}

func TestRequireDevice(t *testing.T) {
	secret := []byte("device-auth-secret")
	signer := token.NewSigner(secret)
	now := time.Unix(1_700_000_000, 0)

	validToken := signer.Issue(99, time.Hour, now)
	expiredToken := signer.Issue(99, time.Hour, now.Add(-2*time.Hour))
	wrongSecretToken := token.NewSigner([]byte("other-secret")).Issue(99, time.Hour, now)

	auth := &DeviceAuth{signer: signer, now: func() time.Time { return now }}

	cases := []struct {
		name       string
		header     string
		wantStatus int
		wantDevice uint64
	}{
		{"valid", "Bearer " + validToken, http.StatusOK, 99},
		{"missing header", "", http.StatusUnauthorized, 0},
		{"no bearer prefix", validToken, http.StatusUnauthorized, 0},
		{"garbage token", "Bearer not-a-real-token", http.StatusUnauthorized, 0},
		{"empty bearer", "Bearer ", http.StatusUnauthorized, 0},
		{"whitespace-only bearer", "Bearer    ", http.StatusUnauthorized, 0},
		{"expired token", "Bearer " + expiredToken, http.StatusUnauthorized, 0},
		{"wrong secret", "Bearer " + wrongSecretToken, http.StatusUnauthorized, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotDevice uint64
			var reached bool
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reached = true
				id, ok := DeviceIDFromContext(r.Context())
				if !ok {
					t.Errorf("device_id not in context")
				}
				gotDevice = id
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/api/v1/reports", nil)
			req = req.WithContext(reqCtx())
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()

			auth.RequireDevice(next).ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body)
			}
			if tc.wantStatus == http.StatusOK {
				if !reached {
					t.Errorf("next handler was not called on valid token")
				}
				if gotDevice != tc.wantDevice {
					t.Errorf("device_id = %d, want %d", gotDevice, tc.wantDevice)
				}
			} else if reached {
				t.Errorf("next handler was called despite auth failure")
			}
		})
	}
}
