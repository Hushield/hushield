package push

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"spamfilter/internal/store"
)

func TestFCMNotifier_SendSilentRefresh_success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"name": "projects/hushield/messages/0:1234"}`))
	}))
	defer server.Close()

	n := &FCMNotifier{httpClient: server.Client(), endpoint: server.URL, accessToken: "test-token"}
	err := n.SendSilentRefresh(context.Background(), store.PushTarget{DeviceID: 1, Token: "fcm-token-1", Platform: "fcm"})
	if err != nil {
		t.Fatalf("SendSilentRefresh: %v", err)
	}
}

func TestFCMNotifier_SendSilentRefresh_serverError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": {"status": "UNAUTHENTICATED"}}`))
	}))
	defer server.Close()

	n := &FCMNotifier{httpClient: server.Client(), endpoint: server.URL, accessToken: "bad-token"}
	err := n.SendSilentRefresh(context.Background(), store.PushTarget{DeviceID: 1, Token: "fcm-token-1", Platform: "fcm"})
	if err == nil {
		t.Fatal("SendSilentRefresh returned nil for a non-2xx FCM response")
	}
}
