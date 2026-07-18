package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestID_ReusesIncomingHeader(t *testing.T) {
	var gotFromContext string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFromContext = RequestIDFromContext(r.Context())
	})

	handler := RequestIDMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-Id", "incoming-id-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if gotFromContext != "incoming-id-123" {
		t.Errorf("RequestIDFromContext = %q, want incoming-id-123", gotFromContext)
	}
	if got := rec.Header().Get("X-Request-Id"); got != "incoming-id-123" {
		t.Errorf("response X-Request-Id = %q, want incoming-id-123", got)
	}
}

func TestRequestID_GeneratesWhenAbsent(t *testing.T) {
	var gotFromContext string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFromContext = RequestIDFromContext(r.Context())
	})

	handler := RequestIDMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if gotFromContext == "" {
		t.Errorf("RequestIDFromContext = empty, want generated id")
	}
	// 16 bytes hex-encoded = 32 hex chars
	if len(gotFromContext) != 32 {
		t.Errorf("generated request id length = %d, want 32 (16 bytes hex)", len(gotFromContext))
	}

	respHeader := rec.Header().Get("X-Request-Id")
	if respHeader != gotFromContext {
		t.Errorf("response X-Request-Id = %q, want match context value %q", respHeader, gotFromContext)
	}
}

func TestRequestID_GeneratesUniqueIDs(t *testing.T) {
	ids := map[string]bool{}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids[RequestIDFromContext(r.Context())] = true
	})
	handler := RequestIDMiddleware(next)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	if len(ids) != 5 {
		t.Errorf("got %d unique ids, want 5", len(ids))
	}
}

func TestRequestIDFromContext_EmptyWhenUnset(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := RequestIDFromContext(req.Context()); got != "" {
		t.Errorf("RequestIDFromContext on bare context = %q, want empty", got)
	}
}
