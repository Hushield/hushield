// Command server runs the spamfilter API server: it loads config, connects
// to MySQL, applies pending migrations, and serves /api/v1.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"spamfilter/internal/api"
	"spamfilter/internal/config"
	"spamfilter/internal/db"
)

func main() {
	// SIGINT/SIGTERM trigger a graceful shutdown so in-flight requests finish.
	// systemd sends SIGTERM on `systemctl restart`, and with the previous bare
	// ListenAndServe the process was killed mid-request on every deploy.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, nil); err != nil {
		log.Fatalf("%v", err)
	}
}

// run wires and serves the API, blocking until ctx is cancelled.
//
// It returns an error instead of exiting so the wiring -- config validation, the
// database connection, migrations, the port bind -- can be tested. That wiring
// is exactly where a deploy breaks: config.Load hard-fails on a bad production
// config, and before this was testable the only way to see the message was to
// run the binary and read the journal.
//
// ready, when non-nil, receives the bound address once the listener is
// accepting connections. Tests use it to avoid racing startup and to discover
// the port when binding :0. main passes nil.
func run(ctx context.Context, ready chan<- string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
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
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer sqlDB.Close()

	if err := db.Migrate(sqlDB); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	// Listen explicitly rather than via ListenAndServe, so a bind failure is
	// reported as such instead of after "listening on ..." has been logged, and
	// so tests can bind :0 and discover the port.
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", cfg.Addr, err)
	}

	srv := &http.Server{
		Handler:           api.NewRouter(sqlDB, cfg),
		ReadHeaderTimeout: 15 * time.Second,
	}

	log.Printf("spamfilter server listening on %s", ln.Addr())
	if ready != nil {
		ready <- ln.Addr().String()
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		log.Printf("spamfilter server shutting down")
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	}
}
