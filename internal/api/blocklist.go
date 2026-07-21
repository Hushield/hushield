package api

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"spamfilter/internal/store"
)

// blocklistHandler serves GET /api/v1/blocklist: the delta of numbers an
// attested device should block or label since a cursor, widened by an
// optional neighbor-spoof prefix. It must run behind RequireDevice.
type blocklistHandler struct {
	db *sql.DB
}

// blocklistEntryResponse serializes one delta entry. Action is one of
// "block", "label", or "unblock". Client contract: apply entries in cursor
// order (the order they're returned in, which is the store's
// (updated_at, phone_number_id) keyset order) -- "block"/"label" mean
// add/label the number in the device's local block/label sets, and
// "unblock" means remove it from both sets (a number that ages off, gets an
// admin allow override, or is walked back down by not_spam votes stops
// being blockable and must be actively removed, not just left stale).
type blocklistEntryResponse struct {
	Number         string  `json:"number"`
	Action         string  `json:"action"`
	Name           *string `json:"name"`
	SpoofSuspected bool    `json:"spoof_suspected"`
}

type blocklistResponse struct {
	Entries []blocklistEntryResponse `json:"entries"`
	Count   int                      `json:"count"`
	Cursor  string                   `json:"cursor"`
}

const (
	defaultBlocklistLimit = 500
	maxBlocklistLimit     = 1000
	prefixLength          = 6
)

// handleList validates the since/prefix/limit query params, loads the
// delta from the store, and responds with the block/label envelope.
func (h *blocklistHandler) handleList(w http.ResponseWriter, r *http.Request) {
	requestID := RequestIDFromContext(r.Context())

	if _, ok := DeviceIDFromContext(r.Context()); !ok {
		unauthorized(w, requestID)
		return
	}

	prefix := r.URL.Query().Get("prefix")
	if prefix != "" && !isValidBlocklistPrefix(prefix) {
		WriteError(w, http.StatusUnprocessableEntity, requestID,
			APIError{Field: "prefix", Message: "prefix must be exactly 6 digits", Code: "invalid"})
		return
	}

	sinceSec, sinceID := parseSinceParam(r.URL.Query().Get("since"))
	limit := parseLimitParam(r.URL.Query().Get("limit"))

	entries, nextSec, nextID, err := store.BlocklistDelta(r.Context(), h.db, sinceSec, sinceID, prefix, limit)
	if err != nil {
		logInternalError(requestID, "load blocklist", err)
		WriteError(w, http.StatusInternalServerError, requestID,
			APIError{Message: "failed to load blocklist", Code: "internal_error"})
		return
	}

	resp := blocklistResponse{
		Entries: make([]blocklistEntryResponse, 0, len(entries)),
		Count:   len(entries),
		Cursor:  formatCursor(nextSec, nextID),
	}
	for _, e := range entries {
		resp.Entries = append(resp.Entries, blocklistEntryResponse{
			Number:         e.Number,
			Action:         e.Action,
			Name:           e.Name,
			SpoofSuspected: e.SpoofSuspected,
		})
	}

	WriteSuccess(w, http.StatusOK, resp, requestID)
}

// parseSinceParam parses the since cursor query param -- an opaque
// "<sec>.<id>" compound cursor as returned in a prior response's cursor
// field -- into its (sec, id) parts. Empty, "0", "0.0", or any malformed
// value defaults to (0, 0), a full snapshot.
func parseSinceParam(raw string) (int64, uint64) {
	if raw == "" || raw == "0" {
		return 0, 0
	}
	secPart, idPart, ok := strings.Cut(raw, ".")
	if !ok {
		return 0, 0
	}
	sec, err := strconv.ParseInt(secPart, 10, 64)
	if err != nil || sec < 0 {
		return 0, 0
	}
	id, err := strconv.ParseUint(idPart, 10, 64)
	if err != nil {
		return 0, 0
	}
	return sec, id
}

// formatCursor renders a compound (sec, id) cursor as the opaque "<sec>.<id>"
// string returned in the response's cursor field and accepted back by
// parseSinceParam.
func formatCursor(sec int64, id uint64) string {
	return strconv.FormatInt(sec, 10) + "." + strconv.FormatUint(id, 10)
}

// parseLimitParam parses the limit query param, defaulting non-numeric or
// non-positive values to defaultBlocklistLimit and clamping anything above
// maxBlocklistLimit down to it.
func parseLimitParam(raw string) int {
	if raw == "" {
		return defaultBlocklistLimit
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return defaultBlocklistLimit
	}
	if v > maxBlocklistLimit {
		return maxBlocklistLimit
	}
	return v
}

// isValidBlocklistPrefix reports whether prefix is exactly 6 ASCII digits
// (an NPA-NXX).
func isValidBlocklistPrefix(prefix string) bool {
	if len(prefix) != prefixLength {
		return false
	}
	for _, r := range prefix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
