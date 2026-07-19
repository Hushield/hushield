package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"spamfilter/internal/token"
)

const deviceIDContextKey contextKey = "device_id"

// DeviceAuth builds the RequireDevice middleware using a token.Signer to
// validate bearer device tokens.
type DeviceAuth struct {
	signer *token.Signer
	now    func() time.Time
}

// NewDeviceAuth returns a DeviceAuth backed by the given signer.
func NewDeviceAuth(signer *token.Signer) *DeviceAuth {
	return &DeviceAuth{signer: signer}
}

func (d *DeviceAuth) clock() time.Time {
	if d.now != nil {
		return d.now()
	}
	return time.Now()
}

// RequireDevice authenticates the "Authorization: Bearer <token>" header. On
// success it stores the device_id in the request context; otherwise it writes
// a 401 envelope error and does not call next.
func (d *DeviceAuth) RequireDevice(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := RequestIDFromContext(r.Context())

		tok, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			unauthorized(w, requestID)
			return
		}

		deviceID, err := d.signer.Parse(tok, d.clock())
		if err != nil {
			unauthorized(w, requestID)
			return
		}

		ctx := context.WithValue(r.Context(), deviceIDContextKey, deviceID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// DeviceIDFromContext returns the device_id stored by RequireDevice.
func DeviceIDFromContext(ctx context.Context) (uint64, bool) {
	id, ok := ctx.Value(deviceIDContextKey).(uint64)
	return id, ok
}

func bearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	tok := strings.TrimSpace(header[len(prefix):])
	if tok == "" {
		return "", false
	}
	return tok, true
}

func unauthorized(w http.ResponseWriter, requestID string) {
	WriteError(w, http.StatusUnauthorized, requestID,
		APIError{Message: "device authentication required", Code: "unauthorized"})
}
