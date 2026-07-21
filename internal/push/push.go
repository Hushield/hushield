// Package push sends server-initiated silent (content-available) APNs pushes
// that nudge a device to refresh its cached blocklist. It exposes a small
// Notifier interface with a real APNs implementation (token-based auth over
// HTTP/2) plus Mock/Noop implementations for tests and the creds-absent
// fail-safe path. Per-device relevance targeting is deferred to Spec 4; this
// package only provides broadcast-to-all plumbing.
package push

import (
	"context"
	"log"

	"spamfilter/internal/store"
)

// Notifier sends a single silent refresh push to one device.
type Notifier interface {
	// SendSilentRefresh sends a background (content-available) push to the
	// target device. It returns nil on an APNs 2xx response and an error
	// otherwise.
	SendSilentRefresh(ctx context.Context, target store.PushTarget) error
}

// BroadcastRefresh sends a silent refresh to every target, tallying successes
// and failures. A per-target error is logged and counted but never aborts the
// batch, so one bad token cannot starve the rest.
func BroadcastRefresh(ctx context.Context, n Notifier, targets []store.PushTarget) (sent, failed int) {
	for _, target := range targets {
		if err := n.SendSilentRefresh(ctx, target); err != nil {
			failed++
			log.Printf("push: silent refresh failed for device_id=%d: %v", target.DeviceID, err)
			continue
		}
		sent++
	}
	return sent, failed
}

// NoopNotifier is the fail-safe Notifier used when APNs credentials are
// absent. It does nothing and always succeeds.
type NoopNotifier struct{}

// SendSilentRefresh does nothing and returns nil.
func (NoopNotifier) SendSilentRefresh(ctx context.Context, target store.PushTarget) error {
	return nil
}
