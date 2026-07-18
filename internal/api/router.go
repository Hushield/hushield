package api

import (
	"database/sql"
	"net/http"

	"spamfilter/internal/config"
)

// NewRouter builds the top-level HTTP handler: a ServeMux wrapped in the
// request_id middleware, with /healthz mounted directly.
func NewRouter(db *sql.DB, cfg config.Config) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", healthzHandler)

	// /api/v1/... routes attach here in later tasks (reports, blocklist,
	// admin overrides, etc). db and cfg are threaded through NewRouter so
	// those handlers can be constructed and mounted below.
	_ = db
	_ = cfg

	return RequestIDMiddleware(mux)
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	WriteSuccess(w, http.StatusOK, map[string]string{"status": "ok"}, RequestIDFromContext(r.Context()))
}
