package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestRunPeriodic_ImmediateThenTicksThenCancel verifies runPeriodic runs
// runOnce once immediately, then once per tick, and stops as soon as ctx is
// canceled: N ticks + cancel => N+1 calls total.
func TestRunPeriodic_ImmediateThenTicksThenCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time)
	called := make(chan struct{})

	var calls int
	runOnce := func() error {
		calls++
		called <- struct{}{}
		return nil
	}

	done := make(chan struct{})
	go func() {
		runPeriodic(ctx, ticks, runOnce)
		close(done)
	}()

	// Immediate call.
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for immediate call")
	}

	const n = 3
	for i := 0; i < n; i++ {
		ticks <- time.Now()
		select {
		case <-called:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for call after tick %d", i+1)
		}
	}

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runPeriodic to return after cancel")
	}

	if calls != n+1 {
		t.Errorf("calls = %d, want %d (immediate + %d ticks)", calls, n+1, n)
	}
}

// TestRunPeriodic_ErrorDoesNotStopLoop verifies a runOnce error is not fatal
// to the loop: subsequent ticks still invoke runOnce.
func TestRunPeriodic_ErrorDoesNotStopLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time)
	called := make(chan struct{})

	var calls int
	runOnce := func() error {
		calls++
		called <- struct{}{}
		if calls == 1 {
			return errors.New("boom")
		}
		return nil
	}

	done := make(chan struct{})
	go func() {
		runPeriodic(ctx, ticks, runOnce)
		close(done)
	}()

	// Immediate call (errors).
	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for immediate call")
	}

	// Two more ticks should still invoke runOnce despite the prior error.
	for i := 0; i < 2; i++ {
		ticks <- time.Now()
		select {
		case <-called:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for call after tick %d", i+1)
		}
	}

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runPeriodic to return after cancel")
	}

	if calls != 3 {
		t.Errorf("calls = %d, want 3 (loop must continue past a runOnce error)", calls)
	}
}

// TestRunPeriodic_CancelBeforeAnyTick verifies that even if ctx is already
// canceled before any tick arrives, the immediate call still happens exactly
// once before runPeriodic returns.
func TestRunPeriodic_CancelBeforeAnyTick(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ticks := make(chan time.Time)
	called := make(chan struct{}, 1)

	var calls int
	runOnce := func() error {
		calls++
		called <- struct{}{}
		return nil
	}

	cancel() // canceled before runPeriodic ever starts

	done := make(chan struct{})
	go func() {
		runPeriodic(ctx, ticks, runOnce)
		close(done)
	}()

	select {
	case <-called:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for immediate call")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runPeriodic to return")
	}

	if calls != 1 {
		t.Errorf("calls = %d, want 1 (immediate call only, no ticks)", calls)
	}
}
