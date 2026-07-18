// Command recompute is a one-shot background job that re-applies time-decay
// and recomputes every phone number's cached status, and recomputes every
// device's trust_weight from its reporting history (see
// internal/store.RecomputeAll). It is meant to be run periodically by
// system cron or launchd, e.g. every 15 minutes:
//
//	*/15 * * * * /path/to/recompute >> /var/log/spamfilter-recompute.log 2>&1
package main

import (
	"context"
	"log"
	"time"

	"spamfilter/internal/config"
	"spamfilter/internal/db"
	"spamfilter/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	sqlDB, err := db.Open(cfg.DBDsn)
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}
	defer sqlDB.Close()

	if err := db.Migrate(sqlDB); err != nil {
		log.Fatalf("running migrations: %v", err)
	}

	start := time.Now()
	numbers, devices, err := store.RecomputeAll(context.Background(), sqlDB, time.Now())
	duration := time.Since(start)
	if err != nil {
		log.Fatalf("recompute failed after numbers=%d devices=%d duration=%s: %v", numbers, devices, duration, err)
	}

	log.Printf("numbers=%d devices=%d duration=%s", numbers, devices, duration)
}
