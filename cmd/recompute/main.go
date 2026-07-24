// Command recompute is a background job that re-applies time-decay and
// recomputes every phone number's cached status, and recomputes every
// device's trust_weight from its reporting history (see
// internal/store.RecomputeAll). By default it runs once and exits, meant to
// be run periodically by system cron or launchd, e.g. every 15 minutes:
//
//	*/15 * * * * /path/to/recompute >> /var/log/spamfilter-recompute.log 2>&1
//
// Alternatively, pass -interval to have the process itself run continuously,
// repeating on that schedule until it receives SIGINT/SIGTERM — useful when
// running under a supervisor (systemd, a container orchestrator) instead of
// cron/launchd. See scripts/com.brahy.spamfilter.recompute.plist and
// scripts/recompute.cron for ready-to-use scheduler configs.
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os/signal"
	"syscall"
	"time"

	"spamfilter/internal/config"
	"spamfilter/internal/db"
	"spamfilter/internal/push"
	"spamfilter/internal/store"
)

func main() {
	notify := flag.Bool("notify", true, "send a silent APNs refresh push to registered devices after recompute (no-op when APNs creds are absent)")
	interval := flag.Duration("interval", 0, "if > 0, run continuously, repeating every interval, until SIGINT/SIGTERM (0 = run once and exit, the default)")
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

	if *interval <= 0 {
		ctx := context.Background()
		if err := runCycle(ctx, sqlDB, cfg, *notify); err != nil {
			log.Fatalf("%v", err)
		}
		return
	}

	// Interval mode: run immediately, then every interval, until SIGINT/SIGTERM
	// requests a graceful shutdown. A single cycle's error is logged but must
	// not kill the loop — the job keeps trying on the next tick.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(*interval)
	defer ticker.Stop()

	runPeriodic(ctx, ticker.C, func() error {
		log.Printf("recompute: cycle starting")
		err := runCycle(ctx, sqlDB, cfg, *notify)
		if err != nil {
			log.Printf("recompute: cycle finished with error: %v", err)
		} else {
			log.Printf("recompute: cycle finished")
		}
		return err
	})

	log.Printf("recompute: shutting down")
}

// runCycle performs a single recompute-and-notify cycle: it re-applies
// time-decay, recomputes cached number/device status via store.RecomputeAll,
// and (when notify is true) broadcasts a silent refresh push to registered
// devices. It returns an error instead of exiting the process so callers can
// decide how to react (fatal for the one-shot path, logged-and-continue for
// the interval loop).
func runCycle(ctx context.Context, sqlDB *sql.DB, cfg config.Config, notify bool) error {
	start := time.Now()
	numbers, devices, err := store.RecomputeAll(ctx, sqlDB, time.Now())
	duration := time.Since(start)
	if err != nil {
		return fmt.Errorf("recompute failed after numbers=%d devices=%d duration=%s: %w", numbers, devices, duration, err)
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
	if notify {
		notifier, realAPNs, err := push.NewNotifier(cfg.APNSKeyPath, cfg.APNSKeyID, cfg.APNSTeamID, cfg.APNSTopic)
		if err != nil {
			return fmt.Errorf("building push notifier: %w", err)
		}
		if realAPNs {
			targets, err := store.ListPushTargets(ctx, sqlDB)
			if err != nil {
				return fmt.Errorf("listing push targets: %w", err)
			}
			sent, failed := push.BroadcastRefresh(ctx, notifier, targets)
			log.Printf("pushed sent=%d failed=%d targets=%d", sent, failed, len(targets))
		}
	}
	return nil
}
