package api

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"spamfilter/internal/store"
)

// pushTokenHandler serves POST /api/v1/devices/push-token: an attested device
// registering its APNs push token so the server can nudge it with a silent
// blocklist-refresh push. It must run behind RequireDevice.
type pushTokenHandler struct {
	db  *sql.DB
	now func() time.Time
}

func (h *pushTokenHandler) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

type registerPushTokenRequest struct {
	PushToken   string `json:"push_token"`
	Environment string `json:"environment"`
}

type registerPushTokenResponse struct {
	Status string `json:"status"`
}

const maxPushTokenLen = 255

// handleRegister validates and persists a device's APNs push token.
func (h *pushTokenHandler) handleRegister(w http.ResponseWriter, r *http.Request) {
	requestID := RequestIDFromContext(r.Context())

	deviceID, ok := DeviceIDFromContext(r.Context())
	if !ok {
		unauthorized(w, requestID)
		return
	}

	var body registerPushTokenRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	if err := dec.Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, requestID,
			APIError{Message: "invalid request body", Code: "bad_request"})
		return
	}

	// push_token must be present, hex-encoded, and within the column width.
	if body.PushToken == "" || len(body.PushToken) > maxPushTokenLen {
		WriteError(w, http.StatusUnprocessableEntity, requestID,
			APIError{Field: "push_token", Message: "push_token must be 1-255 hex characters", Code: "invalid"})
		return
	}
	if _, err := hex.DecodeString(body.PushToken); err != nil {
		WriteError(w, http.StatusUnprocessableEntity, requestID,
			APIError{Field: "push_token", Message: "push_token must be hex-encoded", Code: "invalid"})
		return
	}
	if body.Environment != "sandbox" && body.Environment != "production" {
		WriteError(w, http.StatusUnprocessableEntity, requestID,
			APIError{Field: "environment", Message: "environment must be sandbox or production", Code: "invalid"})
		return
	}

	if err := store.UpsertPushToken(r.Context(), h.db, deviceID, body.PushToken, body.Environment, h.clock()); err != nil {
		logInternalError(requestID, "register push token", err)
		WriteError(w, http.StatusInternalServerError, requestID,
			APIError{Message: "failed to register push token", Code: "internal_error"})
		return
	}

	WriteSuccess(w, http.StatusOK, registerPushTokenResponse{Status: "registered"}, requestID)
}
