// Package api provides the /api/v1 JSON envelope, middleware, and router.
package api

import (
	"encoding/json"
	"net/http"
	"time"
)

// APIError describes a single error entry in the envelope's errors array.
type APIError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

// Meta carries response metadata common to every envelope.
type Meta struct {
	Timestamp string `json:"timestamp"`
	RequestID string `json:"request_id"`
}

// Envelope is the exact JSON response shape used by every /api/v1 endpoint.
type Envelope struct {
	Success bool       `json:"success"`
	Data    any        `json:"data"`
	Errors  []APIError `json:"errors"`
	Meta    Meta       `json:"meta"`
}

func newMeta(requestID string) Meta {
	return Meta{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		RequestID: requestID,
	}
}

func writeEnvelope(w http.ResponseWriter, status int, env Envelope) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(env)
}

// WriteSuccess writes a success envelope with the given HTTP status and data.
func WriteSuccess(w http.ResponseWriter, status int, data any, requestID string) {
	writeEnvelope(w, status, Envelope{
		Success: true,
		Data:    data,
		Errors:  []APIError{},
		Meta:    newMeta(requestID),
	})
}

// WriteError writes a failure envelope with the given HTTP status and errors.
func WriteError(w http.ResponseWriter, status int, requestID string, errs ...APIError) {
	if errs == nil {
		errs = []APIError{}
	}
	writeEnvelope(w, status, Envelope{
		Success: false,
		Data:    nil,
		Errors:  errs,
		Meta:    newMeta(requestID),
	})
}
