package api

import (
	"database/sql"
	"net/http"

	"spamfilter/internal/phone"
	"spamfilter/internal/scoring"
	"spamfilter/internal/store"
)

// numbersHandler serves GET /api/v1/numbers/{e164}: an on-demand reputation
// lookup for a single number (the iOS SMS filter's optional network path and
// a future web reverse-lookup). It must run behind RequireDevice.
//
// Spoof divergence from /blocklist (client-contract note): for a
// status=unknown, cached_score>0 number that matches the caller's own NPA-NXX
// prefix, the two endpoints report the neighbor-spoof signal differently. The
// /blocklist delta bakes the spoof bonus into the entry itself, surfacing it
// as action:"label". /numbers does NOT fold the bonus into action -- it
// returns action derived from the raw stored status (so "none" for unknown)
// PLUS spoof_suspected:true. Clients calling /numbers must therefore treat
// spoof_suspected as a label signal in its own right, not rely on action.
type numbersHandler struct {
	db *sql.DB
}

// numberLookupResponse serializes a single number's reputation. Action is
// one of "block", "label", "allow", or "none", derived from Status the same
// way the /blocklist delta derives its Action.
type numberLookupResponse struct {
	Number         string  `json:"number"`
	Status         string  `json:"status"`
	Action         string  `json:"action"`
	Category       *string `json:"category"`
	Name           *string `json:"name"`
	SpoofSuspected bool    `json:"spoof_suspected"`
}

// handleGet validates the path number and optional prefix, loads the
// reputation from the store, and responds with the lookup envelope. A number
// absent from phone_numbers is simply unknown to the community -- it
// responds 200 with status "unknown"/action "none", not a 404.
func (h *numbersHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	requestID := RequestIDFromContext(r.Context())

	e164, err := phone.Normalize(r.PathValue("e164"))
	if err != nil {
		WriteError(w, http.StatusUnprocessableEntity, requestID,
			APIError{Field: "number", Message: "invalid phone number", Code: "invalid"})
		return
	}

	prefix := r.URL.Query().Get("prefix")
	if prefix != "" && !isValidBlocklistPrefix(prefix) {
		WriteError(w, http.StatusUnprocessableEntity, requestID,
			APIError{Field: "prefix", Message: "prefix must be exactly 6 digits", Code: "invalid"})
		return
	}

	result, found, err := store.LookupNumber(r.Context(), h.db, e164, prefix)
	if err != nil {
		logInternalError(requestID, "lookup number", err)
		WriteError(w, http.StatusInternalServerError, requestID,
			APIError{Message: "failed to look up number", Code: "internal_error"})
		return
	}

	if !found {
		WriteSuccess(w, http.StatusOK, numberLookupResponse{
			Number:         e164,
			Status:         string(scoring.StatusUnknown),
			Action:         "none",
			SpoofSuspected: store.SpoofSuspected(e164, prefix),
		}, requestID)
		return
	}

	resp := numberLookupResponse{
		Number:         result.Number,
		Status:         string(result.Status),
		Action:         actionForStatus(result.Status),
		Name:           result.Name,
		SpoofSuspected: result.SpoofSuspected,
	}
	if result.Category != nil {
		category := string(*result.Category)
		resp.Category = &category
	}

	WriteSuccess(w, http.StatusOK, resp, requestID)
}

// actionForStatus maps a number's Status to the client-facing action, the
// same mapping the /blocklist delta uses for its own Action field.
func actionForStatus(status scoring.Status) string {
	switch status {
	case scoring.StatusBlocked, scoring.StatusOverriddenBlock:
		return "block"
	case scoring.StatusSuspected:
		return "label"
	case scoring.StatusAllowlisted:
		return "allow"
	default:
		return "none"
	}
}
