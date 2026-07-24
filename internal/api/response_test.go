package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWriteSuccess_ExactShape(t *testing.T) {
	rec := httptest.NewRecorder()
	data := map[string]string{"status": "ok"}

	WriteSuccess(rec, 200, data, "req-123")

	if rec.Code != 200 {
		t.Fatalf("status code = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var env Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("failed to unmarshal response body: %v", err)
	}

	if !env.Success {
		t.Errorf("Success = false, want true")
	}
	if env.Errors == nil {
		t.Errorf("Errors = nil, want empty slice (JSON array)")
	}
	if len(env.Errors) != 0 {
		t.Errorf("Errors = %v, want empty", env.Errors)
	}
	if env.Meta.RequestID != "req-123" {
		t.Errorf("Meta.RequestID = %q, want req-123", env.Meta.RequestID)
	}
	if _, err := time.Parse(time.RFC3339, env.Meta.Timestamp); err != nil {
		t.Errorf("Meta.Timestamp %q is not valid RFC3339: %v", env.Meta.Timestamp, err)
	}

	// Verify raw JSON has the exact top-level keys expected.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to unmarshal raw body: %v", err)
	}
	for _, key := range []string{"success", "data", "errors", "meta"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("response missing top-level key %q", key)
		}
	}

	var gotData map[string]string
	if err := json.Unmarshal(raw["data"], &gotData); err != nil {
		t.Fatalf("failed to unmarshal data: %v", err)
	}
	if gotData["status"] != "ok" {
		t.Errorf("data.status = %q, want ok", gotData["status"])
	}
}

func TestWriteError_ExactShape(t *testing.T) {
	rec := httptest.NewRecorder()
	apiErr := APIError{Field: "number", Message: "invalid phone number", Code: "invalid_format"}

	WriteError(rec, 422, "req-456", apiErr)

	if rec.Code != 422 {
		t.Fatalf("status code = %d, want 422", rec.Code)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to unmarshal raw body: %v", err)
	}

	var success bool
	if err := json.Unmarshal(raw["success"], &success); err != nil {
		t.Fatalf("failed to unmarshal success: %v", err)
	}
	if success {
		t.Errorf("success = true, want false")
	}

	if string(raw["data"]) != "null" {
		t.Errorf("data = %s, want null", raw["data"])
	}

	var errs []APIError
	if err := json.Unmarshal(raw["errors"], &errs); err != nil {
		t.Fatalf("failed to unmarshal errors: %v", err)
	}
	if len(errs) != 1 {
		t.Fatalf("len(errors) = %d, want 1", len(errs))
	}
	if errs[0] != apiErr {
		t.Errorf("errors[0] = %+v, want %+v", errs[0], apiErr)
	}

	var meta struct {
		Timestamp string `json:"timestamp"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(raw["meta"], &meta); err != nil {
		t.Fatalf("failed to unmarshal meta: %v", err)
	}
	if meta.RequestID != "req-456" {
		t.Errorf("meta.request_id = %q, want req-456", meta.RequestID)
	}
	if _, err := time.Parse(time.RFC3339, meta.Timestamp); err != nil {
		t.Errorf("meta.timestamp %q is not valid RFC3339: %v", meta.Timestamp, err)
	}
}

func TestWriteError_MultipleErrors(t *testing.T) {
	rec := httptest.NewRecorder()
	err1 := APIError{Field: "number", Message: "required", Code: "required"}
	err2 := APIError{Field: "category", Message: "invalid", Code: "invalid_enum"}

	WriteError(rec, 400, "req-789", err1, err2)

	var env Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if len(env.Errors) != 2 {
		t.Fatalf("len(errors) = %d, want 2", len(env.Errors))
	}
}

// TestWriteError_NoErrorsStillProducesEmptyArray confirms WriteError's nil
// -> []APIError{} defaulting (for a caller that passes zero error values)
// still serializes errors as an empty JSON array, never a JSON null.
func TestWriteError_NoErrorsStillProducesEmptyArray(t *testing.T) {
	rec := httptest.NewRecorder()

	WriteError(rec, 500, "req-nil-errs")

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if string(raw["errors"]) != "[]" {
		t.Errorf("errors = %s, want [] (never null, even with zero error values)", raw["errors"])
	}
}

// TestLogInternalError_WritesRequestIDActionAndErr confirms logInternalError
// records the request id, action, and underlying error in the log line, so a
// 500 response's cause is traceable server-side even though the client-facing
// envelope never includes it.
func TestLogInternalError_WritesRequestIDActionAndErr(t *testing.T) {
	var buf bytes.Buffer
	origOutput := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(origOutput)
		log.SetFlags(origFlags)
	})

	logInternalError("req-log-123", "save report", errors.New("boom: connection reset"))

	line := buf.String()
	for _, want := range []string{"req-log-123", "save report", "boom: connection reset"} {
		if !strings.Contains(line, want) {
			t.Errorf("log line = %q, want it to contain %q", line, want)
		}
	}
}
