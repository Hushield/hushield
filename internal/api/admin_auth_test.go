package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireAdmin(t *testing.T) {
	cases := []struct {
		name       string
		configured string // AdminAuth's configured token; "" means admin API disabled
		header     string
		wantStatus int
	}{
		{"disabled", "", "Bearer whatever", http.StatusServiceUnavailable},
		{"disabled, no header", "", "", http.StatusServiceUnavailable},
		{"missing header", "correct-token", "", http.StatusUnauthorized},
		{"no bearer prefix", "correct-token", "correct-token", http.StatusUnauthorized},
		{"wrong token", "correct-token", "Bearer wrong-token", http.StatusUnauthorized},
		{"empty bearer", "correct-token", "Bearer ", http.StatusUnauthorized},
		{"correct token", "correct-token", "Bearer correct-token", http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			auth := NewAdminAuth(tc.configured)

			var reached bool
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reached = true
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/overrides", nil)
			req = req.WithContext(reqCtx())
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()

			auth.RequireAdmin(next).ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.wantStatus, rec.Body)
			}
			if tc.wantStatus == http.StatusOK {
				if !reached {
					t.Errorf("next handler was not called on valid token")
				}
			} else if reached {
				t.Errorf("next handler was called despite auth failure")
			}
		})
	}
}

func TestRequireAdmin_DisabledResponseCode(t *testing.T) {
	auth := NewAdminAuth("")
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler must not be called when admin API is disabled")
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/overrides", nil)
	req = req.WithContext(reqCtx())
	req.Header.Set("Authorization", "Bearer anything")
	rec := httptest.NewRecorder()

	auth.RequireAdmin(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body)
	}
	success, errs := decodeEnvelopeErrors(t, rec.Body.Bytes())
	if success {
		t.Error("success = true, want false")
	}
	if len(errs) != 1 || errs[0].Code != "admin_disabled" {
		t.Errorf("errors = %+v, want single error with code=admin_disabled", errs)
	}
}
