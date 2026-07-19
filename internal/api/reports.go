package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"spamfilter/internal/phone"
	"spamfilter/internal/scoring"
	"spamfilter/internal/store"
)

// reportsHandler serves POST /api/v1/reports: an attested device reporting
// a phone number as spam/not-spam (with an optional community caller name).
// It must run behind RequireDevice.
type reportsHandler struct {
	db  *sql.DB
	now func() time.Time
}

func (h *reportsHandler) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

type createReportRequest struct {
	Number   string `json:"number"`
	Category string `json:"category"`
	Vote     string `json:"vote"`
	Name     string `json:"name"`
}

type reportResponse struct {
	Number string `json:"number"`
	Status string `json:"status"`
}

var validReportCategories = map[string]bool{
	string(scoring.CategoryScam):         true,
	string(scoring.CategoryRobocall):     true,
	string(scoring.CategoryTelemarketer): true,
	string(scoring.CategoryOther):        true,
}

const maxNameRunes = 100

// handleCreate validates and persists a single report, then responds with
// the number's freshly recomputed status.
func (h *reportsHandler) handleCreate(w http.ResponseWriter, r *http.Request) {
	requestID := RequestIDFromContext(r.Context())
	now := h.clock()

	deviceID, ok := DeviceIDFromContext(r.Context())
	if !ok {
		unauthorized(w, requestID)
		return
	}

	var body createReportRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	if err := dec.Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, requestID,
			APIError{Message: "invalid request body", Code: "bad_request"})
		return
	}

	category := body.Category
	if category == "" {
		category = string(scoring.CategoryOther)
	}
	vote := body.Vote
	if vote == "" {
		vote = string(scoring.VoteSpam)
	}

	e164, err := phone.Normalize(body.Number)
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, requestID,
			APIError{Field: "number", Message: "invalid phone number", Code: "invalid"})
		return
	}
	if !validReportCategories[category] {
		WriteError(w, http.StatusUnprocessableEntity, requestID,
			APIError{Field: "category", Message: "invalid category", Code: "invalid"})
		return
	}
	if vote != string(scoring.VoteSpam) && vote != string(scoring.VoteNotSpam) {
		WriteError(w, http.StatusUnprocessableEntity, requestID,
			APIError{Field: "vote", Message: "invalid vote", Code: "invalid"})
		return
	}
	name := strings.TrimSpace(body.Name)
	if body.Name != "" {
		runeCount := len([]rune(name))
		if runeCount < 1 || runeCount > maxNameRunes {
			WriteError(w, http.StatusUnprocessableEntity, requestID,
				APIError{Field: "name", Message: "name must be 1-100 characters", Code: "invalid"})
			return
		}
	}

	status, err := h.writeReport(r.Context(), deviceID, e164, scoring.Category(category), scoring.Vote(vote), name, now)
	if err != nil {
		logInternalError(requestID, "save report", err)
		WriteError(w, http.StatusInternalServerError, requestID,
			APIError{Message: "failed to save report", Code: "internal_error"})
		return
	}

	WriteSuccess(w, http.StatusCreated, reportResponse{Number: e164, Status: string(status)}, requestID)
}

// writeReport persists the phone number, report, optional caller name, and
// device touch, then recomputes the number's cached score -- all in a single
// transaction. Recomputing inside the transaction (after UpsertPhoneNumber has
// taken the phone_numbers row lock) serializes concurrent reports to the same
// number, so a recompute can never miss a sibling report's write. It returns
// the number's freshly recomputed status.
func (h *reportsHandler) writeReport(ctx context.Context, deviceID uint64, e164 string, category scoring.Category, vote scoring.Vote, name string, now time.Time) (scoring.Status, error) {
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	phoneNumberID, err := store.UpsertPhoneNumber(ctx, tx, e164, now)
	if err != nil {
		return "", err
	}

	inserted, err := store.UpsertReport(ctx, tx, deviceID, phoneNumberID, category, vote, now)
	if err != nil {
		return "", err
	}

	if name != "" {
		if err := store.UpsertCallerName(ctx, tx, deviceID, phoneNumberID, name, now); err != nil {
			return "", err
		}
	}

	if err := store.TouchDevice(ctx, tx, deviceID, inserted, now); err != nil {
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
