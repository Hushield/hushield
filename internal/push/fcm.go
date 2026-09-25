package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"spamfilter/internal/store"
)

// FCMNotifier sends silent (data-only) FCM pushes that nudge an Android
// device to refresh its cached blocklist, mirroring APNsNotifier's role for
// iOS. accessToken is a short-lived OAuth2 token from a service account
// scoped to https://www.googleapis.com/auth/firebase.messaging; refreshing
// it is the caller's responsibility (mirror however cmd/recompute's wiring
// refreshes credentials for other Google-API callers in this plan, e.g. the
// Play Integrity decoder in Task 4).
type FCMNotifier struct {
	httpClient  *http.Client
	endpoint    string // e.g. "https://fcm.googleapis.com/v1/projects/hushield/messages:send"
	accessToken string
}

// NewFCMNotifier builds an FCMNotifier for projectID (the Firebase project
// id), sending requests through httpClient using accessToken as the bearer
// credential.
func NewFCMNotifier(httpClient *http.Client, projectID, accessToken string) *FCMNotifier {
	return &FCMNotifier{
		httpClient:  httpClient,
		endpoint:    fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", projectID),
		accessToken: accessToken,
	}
}

// SendSilentRefresh POSTs a data-only (silent) FCM message to target's
// token. A 2xx response returns nil; any other status returns an error
// including the FCM status code and response body.
func (n *FCMNotifier) SendSilentRefresh(ctx context.Context, target store.PushTarget) error {
	// TODO(android): the request body shape below (message.data,
	// message.android.priority) is a reasonable approximation of Firebase's
	// HTTP v1 API, not verified against Firebase's current live API
	// reference. Check it against the current docs before this is used for a
	// real production send.
	body, err := json.Marshal(map[string]any{
		"message": map[string]any{
			"token": target.Token,
			"data":  map[string]string{"type": "blocklist_refresh"},
			"android": map[string]any{
				"priority": "high",
			},
		},
	})
	if err != nil {
		return fmt.Errorf("push: marshal FCM message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("push: build FCM request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+n.accessToken)

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("push: fcm request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	return fmt.Errorf("push: fcm status=%d body=%s", resp.StatusCode, respBody)
}
