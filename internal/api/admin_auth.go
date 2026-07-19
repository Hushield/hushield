package api

import (
	"crypto/subtle"
	"net/http"
)

// AdminAuth builds the RequireAdmin middleware guarding admin-only routes
// with a single static bearer token.
type AdminAuth struct {
	token string
}

// NewAdminAuth returns an AdminAuth backed by the given admin token (from
// config.Config.AdminToken). An empty token means the admin API is not
// configured, and RequireAdmin fails closed for every request.
func NewAdminAuth(token string) *AdminAuth {
	return &AdminAuth{token: token}
}

// RequireAdmin authenticates the "Authorization: Bearer <token>" header
// against the configured admin token, using a constant-time comparison so
// the response timing does not leak how much of the token matched. It fails
// closed: if no admin token is configured, every request gets a 503
// "admin_disabled" error without calling next. A missing or incorrect token
// gets 401. Only an exact match calls next.
func (a *AdminAuth) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := RequestIDFromContext(r.Context())

		if a.token == "" {
			WriteError(w, http.StatusServiceUnavailable, requestID,
				APIError{Message: "admin API is not configured", Code: "admin_disabled"})
			return
		}

		tok, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok || subtle.ConstantTimeCompare([]byte(tok), []byte(a.token)) != 1 {
			WriteError(w, http.StatusUnauthorized, requestID,
				APIError{Message: "admin authentication required", Code: "unauthorized"})
			return
		}

		next.ServeHTTP(w, r)
	})
}
