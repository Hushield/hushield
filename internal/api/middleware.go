package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type contextKey string

const requestIDContextKey contextKey = "request_id"

const requestIDHeader = "X-Request-Id"

// RequestIDMiddleware reuses the incoming X-Request-Id header if present,
// otherwise generates a new one. The id is stashed in the request context
// and echoed back on the response.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get(requestIDHeader)
		if requestID == "" {
			requestID = generateRequestID()
		}

		w.Header().Set(requestIDHeader, requestID)

		ctx := context.WithValue(r.Context(), requestIDContextKey, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext returns the request id stashed by RequestIDMiddleware,
// or "" if none is present.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDContextKey).(string)
	return id
}

func generateRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read failing is effectively unrecoverable; fall back
		// to a fixed non-empty value rather than panicking mid-request.
		return "00000000000000000000000000000000"[:32]
	}
	return hex.EncodeToString(b)
}
