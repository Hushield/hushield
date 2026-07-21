package push

import "log"

// NewNotifier selects a Notifier from APNs configuration. When keyPath is
// empty (no credentials configured) it returns a NoopNotifier and logs a
// one-line warning, keeping the whole push feature fail-safe: registration and
// broadcast still run, they just send nothing. Otherwise it builds a real
// APNsNotifier. The bool reports whether a real (APNs) notifier was returned,
// so callers can skip broadcasting when push is disabled.
func NewNotifier(keyPath, keyID, teamID, topic string) (Notifier, bool, error) {
	if keyPath == "" {
		log.Printf("push: APNs disabled (APNS_KEY_PATH unset); silent refresh pushes will not be sent")
		return NoopNotifier{}, false, nil
	}
	n, err := NewAPNsNotifier(keyPath, keyID, teamID, topic)
	if err != nil {
		return nil, false, err
	}
	return n, true, nil
}
