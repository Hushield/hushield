package api

import (
	"database/sql"
	"net/http"

	"spamfilter/internal/attest"
	"spamfilter/internal/config"
	"spamfilter/internal/token"
)

// NewRouter builds the top-level HTTP handler: a ServeMux wrapped in the
// request_id middleware, with /healthz and the attest endpoints mounted.
func NewRouter(db *sql.DB, cfg config.Config) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", healthzHandler)

	signer := token.NewSigner([]byte(cfg.DeviceTokenSecret))

	attestH := &attestHandler{
		db:           db,
		store:        attest.NewMemoryChallengeStore(),
		verifier:     buildVerifier(cfg),
		signer:       signer,
		challengeTTL: cfg.ChallengeTTL,
		tokenTTL:     cfg.DeviceTokenTTL,
	}

	mux.HandleFunc("POST /api/v1/attest/challenge", attestH.handleChallenge)
	mux.HandleFunc("POST /api/v1/attest/verify", attestH.handleVerify)

	// deviceAuth guards routes that require an attested device token. It is
	// intentionally not applied to the attest routes above.
	deviceAuth := NewDeviceAuth(signer)

	reportsH := &reportsHandler{db: db}
	mux.Handle("POST /api/v1/reports", deviceAuth.RequireDevice(http.HandlerFunc(reportsH.handleCreate)))

	blocklistH := &blocklistHandler{db: db}
	mux.Handle("GET /api/v1/blocklist", deviceAuth.RequireDevice(http.HandlerFunc(blocklistH.handleList)))

	// adminAuth guards the admin override route with a static bearer token,
	// independent of device attestation.
	adminAuth := NewAdminAuth(cfg.AdminToken)

	adminOverridesH := &adminOverridesHandler{db: db}
	mux.Handle("POST /api/v1/admin/overrides", adminAuth.RequireAdmin(http.HandlerFunc(adminOverridesH.handleCreate)))

	return RequestIDMiddleware(mux)
}

// buildVerifier selects the App Attest verifier per config: a MockVerifier
// for dev/tests, or the real AppleVerifier when ATTEST_MODE=apple.
func buildVerifier(cfg config.Config) attest.Verifier {
	if cfg.AttestMode == "apple" {
		return attest.NewAppleVerifier(cfg.AppID, attest.DefaultAppleRoots())
	}
	return attest.NewMockVerifier([]byte("mock-public-key-der"), nil)
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	WriteSuccess(w, http.StatusOK, map[string]string{"status": "ok"}, RequestIDFromContext(r.Context()))
}
