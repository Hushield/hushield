package api

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"

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
		store:        buildChallengeStore(cfg),
		verifier:     buildVerifier(cfg),
		signer:       signer,
		challengeTTL: cfg.ChallengeTTL,
		tokenTTL:     cfg.DeviceTokenTTL,
	}

	mux.HandleFunc("POST /api/v1/attest/challenge", attestH.handleChallenge)
	mux.HandleFunc("POST /api/v1/attest/verify", attestH.handleVerify)
	mux.HandleFunc("POST /api/v1/attest/assert", attestH.handleAssert)

	// deviceAuth guards routes that require an attested device token. It is
	// intentionally not applied to the attest routes above.
	deviceAuth := NewDeviceAuth(signer)

	reportsH := &reportsHandler{db: db}
	mux.Handle("POST /api/v1/reports", deviceAuth.RequireDevice(http.HandlerFunc(reportsH.handleCreate)))

	blocklistH := &blocklistHandler{db: db}
	mux.Handle("GET /api/v1/blocklist", deviceAuth.RequireDevice(http.HandlerFunc(blocklistH.handleList)))

	numbersH := &numbersHandler{db: db}
	mux.Handle("GET /api/v1/numbers/{e164}", deviceAuth.RequireDevice(http.HandlerFunc(numbersH.handleGet)))

	pushTokenH := &pushTokenHandler{db: db}
	mux.Handle("POST /api/v1/devices/push-token", deviceAuth.RequireDevice(http.HandlerFunc(pushTokenH.handleRegister)))

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

// buildChallengeStore selects the attest.ChallengeStore per config: the
// process-local MemoryChallengeStore (default), or a RedisChallengeStore
// when CHALLENGE_STORE=redis, required for a multi-instance deployment.
// A redis selection with an unparseable or unreachable REDIS_URL fails
// startup with a clear error rather than silently falling back.
func buildChallengeStore(cfg config.Config) attest.ChallengeStore {
	if cfg.ChallengeStore != "redis" {
		return attest.NewMemoryChallengeStore()
	}

	opts, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		log.Fatalf("config: invalid REDIS_URL for CHALLENGE_STORE=redis: %v", err)
	}
	client := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		log.Fatalf("challenge store: cannot reach redis at %s: %v", cfg.RedisURL, err)
	}

	return attest.NewRedisChallengeStore(client)
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	WriteSuccess(w, http.StatusOK, map[string]string{"status": "ok"}, RequestIDFromContext(r.Context()))
}
