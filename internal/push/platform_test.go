package push

import (
	"context"
	"errors"
	"testing"

	"spamfilter/internal/store"
)

type spyNotifier struct {
	calls []store.PushTarget
	err   error
}

func (s *spyNotifier) SendSilentRefresh(ctx context.Context, target store.PushTarget) error {
	s.calls = append(s.calls, target)
	return s.err
}

func TestPlatformNotifier_dispatchesByPlatform(t *testing.T) {
	apns := &spyNotifier{}
	fcm := &spyNotifier{}
	n := &PlatformNotifier{APNs: apns, FCM: fcm}

	if err := n.SendSilentRefresh(context.Background(), store.PushTarget{DeviceID: 1, Platform: "apns"}); err != nil {
		t.Fatalf("SendSilentRefresh(apns): %v", err)
	}
	if err := n.SendSilentRefresh(context.Background(), store.PushTarget{DeviceID: 2, Platform: "fcm"}); err != nil {
		t.Fatalf("SendSilentRefresh(fcm): %v", err)
	}

	if len(apns.calls) != 1 || apns.calls[0].DeviceID != 1 {
		t.Errorf("apns notifier calls = %+v, want exactly device 1", apns.calls)
	}
	if len(fcm.calls) != 1 || fcm.calls[0].DeviceID != 2 {
		t.Errorf("fcm notifier calls = %+v, want exactly device 2", fcm.calls)
	}
}

func TestPlatformNotifier_unknownPlatformFailsClosed(t *testing.T) {
	apns := &spyNotifier{}
	fcm := &spyNotifier{}
	n := &PlatformNotifier{APNs: apns, FCM: fcm}

	err := n.SendSilentRefresh(context.Background(), store.PushTarget{DeviceID: 3, Platform: "carrier-pigeon"})
	if !errors.Is(err, ErrUnknownPushPlatform) {
		t.Errorf("err = %v, want ErrUnknownPushPlatform", err)
	}
	if len(apns.calls) != 0 || len(fcm.calls) != 0 {
		t.Error("an unknown platform must not fall through to either real notifier")
	}
}
