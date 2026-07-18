package api

import (
	"database/sql"
	"net/http"
	"strconv"

	"spamfilter/internal/store"
)

// blocklistHandler serves GET /api/v1/blocklist: the delta of numbers an
// attested device should block or label since a cursor, widened by an
// optional neighbor-spoof prefix. It must run behind RequireDevice.
type blocklistHandler struct {
	db *sql.DB
}

type blocklistEntryResponse struct {
	Number         string  `json:"number"`
	Action         string  `json:"action"`
	Name           *string `json:"name"`
	SpoofSuspected bool    `json:"spoof_suspected"`
}

type blocklistResponse struct {
	Entries []blocklistEntryResponse `json:"entries"`
	Count   int                      `json:"count"`
	Cursor  int64                    `json:"cursor"`
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

	since := parseSinceParam(r.URL.Query().Get("since"))
	limit := parseLimitParam(r.URL.Query().Get("limit"))

	entries, cursor, err := store.BlocklistDelta(r.Context(), h.db, since, prefix, limit)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, requestID,
			APIError{Message: "failed to load blocklist", Code: "internal_error"})
		return
	}

	resp := blocklistResponse{
		Entries: make([]blocklistEntryResponse, 0, len(entries)),
		Count:   len(entries),
		Cursor:  cursor,
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

// parseSinceParam parses the since cursor query param, defaulting to (and
// clamping negative/non-numeric values to) 0, a full snapshot.
func parseSinceParam(raw string) int64 {
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || v < 0 {
		return 0
	}
	return v
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
