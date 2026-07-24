package main

import (
	"context"
	"time"
)

// runPeriodic runs runOnce once immediately, then once per received tick,
// until ctx is done. It never stops the loop because of a runOnce error —
// callers that want cycle errors logged (or otherwise handled) must do so
// inside runOnce itself.
func runPeriodic(ctx context.Context, ticks <-chan time.Time, runOnce func() error) {
	runOnce()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			runOnce()
		}
	}
}
