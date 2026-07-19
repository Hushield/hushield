package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"spamfilter/internal/phone"
	"spamfilter/internal/scoring"
	"spamfilter/internal/store"
)

// adminOverridesHandler serves POST /api/v1/admin/overrides: an admin
// forcing a phone number to always-allow or always-block, overriding the
// community score. It must run behind RequireAdmin.
type adminOverridesHandler struct {
	db  *sql.DB
	now func() time.Time
}

func (h *adminOverridesHandler) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

type createOverrideRequest struct {
	Number string `json:"number"`
	Mode   string `json:"mode"`
	Reason string `json:"reason"`
	Admin  string `json:"admin"`
}

type overrideResponse struct {
	Number string `json:"number"`
	Status string `json:"status"`
}

const (
	maxOverrideReasonRunes = 255
	maxOverrideAdminRunes  = 100
	defaultOverrideAdmin   = "admin"
)

var validOverrideModes = map[string]bool{
	"allow": true,
	"block": true,
}

// handleCreate validates and persists an admin override, then responds with
// the number's freshly recomputed status.
func (h *adminOverridesHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	requestID := RequestIDFromContext(r.Context())
	now := h.clock()

	var body createOverrideRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	if err := dec.Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, requestID,
			APIError{Message: "invalid request body", Code: "bad_request"})
		return
	}

	e164, err := phone.Normalize(body.Number)
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, requestID,
			APIError{Field: "number", Message: "invalid phone number", Code: "invalid"})
		return
	}
	if !validOverrideModes[body.Mode] {
		WriteError(w, http.StatusUnprocessableEntity, requestID,
			APIError{Field: "mode", Message: "mode must be allow or block", Code: "invalid"})
		return
	}
	if len([]rune(body.Reason)) > maxOverrideReasonRunes {
		WriteError(w, http.StatusUnprocessableEntity, requestID,
			APIError{Field: "reason", Message: "reason must be at most 255 characters", Code: "invalid"})
		return
	}
	admin := body.Admin
	if admin == "" {
		admin = defaultOverrideAdmin
	}
	if len([]rune(admin)) > maxOverrideAdminRunes {
		WriteError(w, http.StatusUnprocessableEntity, requestID,
			APIError{Field: "admin", Message: "admin must be at most 100 characters", Code: "invalid"})
		return
	}

	status, err := h.writeOverride(r.Context(), e164, body.Mode, body.Reason, admin, now)
	if err != nil {
		logInternalError(requestID, "save override", err)
		WriteError(w, http.StatusInternalServerError, requestID,
			APIError{Message: "failed to save override", Code: "internal_error"})
		return
	}

	WriteSuccess(w, http.StatusOK, overrideResponse{Number: e164, Status: string(status)}, requestID)
}

// writeOverride persists the phone number and its override, then recomputes
// the number's cached score -- all in a single transaction. Recomputing inside
// the transaction (after UpsertPhoneNumber has taken the phone_numbers row
// lock) serializes it against concurrent writes to the same number. It returns
// the number's freshly recomputed status.
func (h *adminOverridesHandler) writeOverride(ctx context.Context, e164, mode, reason, admin string, now time.Time) (scoring.Status, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	phoneNumberID, err := store.UpsertPhoneNumber(ctx, tx, e164, now)
	if err != nil {
		return "", err
	}

	if err := store.UpsertOverride(ctx, tx, phoneNumberID, mode, reason, admin, now); err != nil {
		return "", err
	}

	status, err := store.RecomputeNumber(ctx, tx, phoneNumberID, now)
	if err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}

	return status, nil
}
