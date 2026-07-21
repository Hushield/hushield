package push

import (
	"context"
	"errors"
	"sync"

	"spamfilter/internal/store"
)

// MockNotifier records every target it is asked to notify. When Err is
// non-nil, SendSilentRefresh returns it (after recording the target) so tests
// can exercise the failure path. FailFor lets a test fail only a subset of
// targets (by DeviceID) to exercise a mixed success/failure batch. It is safe
// for concurrent use.
type MockNotifier struct {
	// Err, when set, is returned by every SendSilentRefresh call whose target
	// is not otherwise selected by FailFor.
	Err error

	// FailFor, when non-nil, makes SendSilentRefresh return an error only for
	// targets whose DeviceID is present (value true); other targets succeed.
	// This overrides Err's all-or-nothing behavior for a mixed batch.
	FailFor map[uint64]bool

	mu    sync.Mutex
	calls []store.PushTarget
}

// SendSilentRefresh records the target and returns an error per Err/FailFor.
func (m *MockNotifier) SendSilentRefresh(ctx context.Context, target store.PushTarget) error {
	m.mu.Lock()
	m.calls = append(m.calls, target)
	m.mu.Unlock()
	if m.FailFor != nil {
		if m.FailFor[target.DeviceID] {
			return errors.New("mock: send failed")
		}
		return nil
	}
	return m.Err
}

// Calls returns a copy of the targets recorded so far, in call order.
func (m *MockNotifier) Calls() []store.PushTarget {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]store.PushTarget, len(m.calls))
	copy(out, m.calls)
	return out
}
