package push

import (
	"context"
	"sync"

	"spamfilter/internal/store"
)

// MockNotifier records every target it is asked to notify. When Err is
// non-nil, SendSilentRefresh returns it (after recording the target) so tests
// can exercise the failure path. It is safe for concurrent use.
type MockNotifier struct {
	// Err, when set, is returned by every SendSilentRefresh call.
	Err error

	mu    sync.Mutex
	calls []store.PushTarget
}

// SendSilentRefresh records the target and returns m.Err.
func (m *MockNotifier) SendSilentRefresh(ctx context.Context, target store.PushTarget) error {
	m.mu.Lock()
	m.calls = append(m.calls, target)
	m.mu.Unlock()
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
