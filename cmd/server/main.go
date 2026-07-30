// Command server runs the spamfilter API server: it loads config, connects
// to MySQL, applies pending migrations, and serves /api/v1.
package main

import (
	"log"
	"net/http"

	"spamfilter/internal/api"
	"spamfilter/internal/config"
	"spamfilter/internal/db"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	if cfg.DeviceTokenSecretIsDefault {
		log.Printf("WARNING: DEVICE_TOKEN_SECRET is unset; using an insecure dev default. Set DEVICE_TOKEN_SECRET in production.")
	}

	// The memory challenge store is process-local. That is correct for a single
	// instance and silently wrong behind a load balancer: a device fetches its
	// challenge from instance A, submits the attestation to instance B, and B has
	// never heard of that challenge -- so enrolment fails intermittently in a way
	// that looks like a client bug. Say so at startup rather than leaving it to be
	// discovered in production.
	if cfg.ChallengeStore != "redis" {
		log.Printf("NOTE: CHALLENGE_STORE=%s is process-local and supports a SINGLE instance only. "+
			"Running more than one replica behind a load balancer will cause intermittent "+
			"attestation failures. Set CHALLENGE_STORE=redis with REDIS_URL for multi-instance.", cfg.ChallengeStore)
	}

	sqlDB, err := db.Open(cfg.DBDsn)
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}
	defer sqlDB.Close()

	if err := db.Migrate(sqlDB); err != nil {
		log.Fatalf("running migrations: %v", err)
	}

	router := api.NewRouter(sqlDB, cfg)

	log.Printf("spamfilter server listening on %s", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, router); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
