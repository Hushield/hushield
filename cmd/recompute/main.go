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
	"flag"
	"log"
	"time"

	"spamfilter/internal/config"
	"spamfilter/internal/db"
	"spamfilter/internal/push"
	"spamfilter/internal/store"
)

func main() {
	notify := flag.Bool("notify", true, "send a silent APNs refresh push to registered devices after recompute (no-op when APNs creds are absent)")
	flag.Parse()

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

	ctx := context.Background()

	start := time.Now()
	numbers, devices, err := store.RecomputeAll(ctx, sqlDB, time.Now())
	duration := time.Since(start)
	if err != nil {
		log.Fatalf("recompute failed after numbers=%d devices=%d duration=%s: %v", numbers, devices, duration, err)
	}

	log.Printf("numbers=%d devices=%d duration=%s", numbers, devices, duration)

	// After recompute, nudge devices to refresh their cached blocklist via a
	// silent push. This is a simple broadcast-to-all: every device with a
	// registered token gets pinged regardless of whether its own blocklist
	// actually changed. Per-device relevance targeting is deferred to Spec 4.
	//
	// The entire notifier build + broadcast is guarded by -notify so a
	// push-only misconfig (e.g. APNS_KEY_PATH set but unreadable/malformed,
	// which makes NewNotifier fatal) can never fail a recompute that already
	// succeeded. When -notify is off we simply skip pushing.
	if *notify {
		notifier, realAPNs, err := push.NewNotifier(cfg.APNSKeyPath, cfg.APNSKeyID, cfg.APNSTeamID, cfg.APNSTopic)
		if err != nil {
			log.Fatalf("building push notifier: %v", err)
		}
		if realAPNs {
			targets, err := store.ListPushTargets(ctx, sqlDB)
			if err != nil {
				log.Fatalf("listing push targets: %v", err)
			}
			sent, failed := push.BroadcastRefresh(ctx, notifier, targets)
			log.Printf("pushed sent=%d failed=%d targets=%d", sent, failed, len(targets))
		}
	}
}
