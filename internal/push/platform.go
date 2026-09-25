package push

import (
	"context"
	"errors"

	"spamfilter/internal/store"
)

// ErrUnknownPushPlatform is returned when a PushTarget's Platform matches
// neither notifier PlatformNotifier holds. Failing closed here means a data
// bug (an unexpected platform value slipping into the devices table) is
// logged as a failed send, not silently dropped or misrouted.
var ErrUnknownPushPlatform = errors.New("push: unknown platform")

// PlatformNotifier dispatches SendSilentRefresh to APNs or FCM based on
// target.Platform ("apns" or "fcm"), so BroadcastRefresh's caller does not
// need to branch on platform itself.
type PlatformNotifier struct {
	APNs Notifier
	FCM  Notifier
}

func (n *PlatformNotifier) SendSilentRefresh(ctx context.Context, target store.PushTarget) error {
	switch target.Platform {
	case "apns":
		return n.APNs.SendSilentRefresh(ctx, target)
	case "fcm":
		return n.FCM.SendSilentRefresh(ctx, target)
	default:
		return ErrUnknownPushPlatform
	}
}
