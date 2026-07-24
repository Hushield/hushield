package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"spamfilter/internal/config"
	"spamfilter/internal/dbtest"
	"spamfilter/internal/token"
)

// TestBlocklistEndpoint_AdminAllowSurfacesAsUnblock confirms the store's
// action:"unblock" value flows unmodified through the HTTP handler and JSON
// envelope: a community-blocked number that an admin override then
// allowlists must appear in the next delta as {"action":"unblock"}, not as a
// leftover "block"/"label" entry with the new status.
func TestBlocklistEndpoint_AdminAllowSurfacesAsUnblock(t *testing.T) {
	database := dbtest.SetupDB(t)

	cfg := config.Config{
		AttestMode:        "mock",
		DeviceTokenSecret: "blocklist-unblock-secret",
		DeviceTokenTTL:    time.Hour,
		ChallengeTTL:      5 * time.Minute,
		AdminToken:        "unblock-test-admin-token",
	}
	router := NewRouter(database, cfg)
	signer := token.NewSigner([]byte(cfg.DeviceTokenSecret))

	_, callerToken := mintDeviceToken(t, database, signer, 1.0)

	number := uniquePhoneNumber()
	_, d1 := mintDeviceToken(t, database, signer, 1.0)
	_, d2 := mintDeviceToken(t, database, signer, 1.0)
	_, d3 := mintDeviceToken(t, database, signer, 1.0)
	for _, tok := range []string{d1, d2, d3} {
		rec := postReport(router, tok, reportRequestBody{Number: number, Category: "scam", Vote: "spam"})
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed report status = %d, want 201; body=%s", rec.Code, rec.Body)
		}
	}

	overrideRec := postOverride(router, cfg.AdminToken, overrideRequestBody{Number: number, Mode: "allow"})
	if overrideRec.Code != http.StatusOK {
		t.Fatalf("admin override status = %d, want 200; body=%s", overrideRec.Code, overrideRec.Body)
	}

	// A full snapshot (since=0) taken after both the block and the
	// override reflects the number's current, final state: it must be
	// action:"unblock", not a leftover "block"/"label" entry. (The store
	// package's tests already cover the delta/cursor mechanics of the
	// block -> unblock transition in depth; this test only confirms the
	// store's "unblock" action value survives the HTTP handler's JSON
	// envelope unmodified.)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/blocklist", nil)
	req.Header.Set("Authorization", "Bearer "+callerToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	_, data := decodeEnvelope(t, rec.Body.Bytes())
	var resp blocklistResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	var found *blocklistEntryResponse
	for i := range resp.Entries {
		if resp.Entries[i].Number == number {
			found = &resp.Entries[i]
		}
	}
	if found == nil {
		t.Fatalf("number missing from post-override snapshot: %+v", resp.Entries)
	}
	if found.Action != "unblock" {
		t.Errorf("action = %q, want %q", found.Action, "unblock")
	}
}

func TestBlocklistEndpoint_MissingToken(t *testing.T) {
	router := NewRouter(nil, config.Config{DeviceTokenSecret: "secret"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/blocklist", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body)
	}
}

func TestBlocklistEndpoint_InvalidPrefix(t *testing.T) {
	secret := "blocklist-prefix-secret"
	router := NewRouter(nil, config.Config{DeviceTokenSecret: secret, DeviceTokenTTL: time.Hour})
	signer := token.NewSigner([]byte(secret))
	tok := signer.Issue(1, time.Hour, time.Now())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/blocklist?prefix=abc", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body)
	}
	success, errs := decodeEnvelopeErrors(t, rec.Body.Bytes())
	if success {
		t.Error("success = true, want false")
	}
	if len(errs) != 1 || errs[0].Field != "prefix" {
		t.Errorf("errors = %+v, want single error on field=prefix", errs)
	}
}

func TestBlocklistEndpoint_InvalidPrefixLength(t *testing.T) {
	secret := "blocklist-prefix-len-secret"
	router := NewRouter(nil, config.Config{DeviceTokenSecret: secret, DeviceTokenTTL: time.Hour})
	signer := token.NewSigner([]byte(secret))
	tok := signer.Issue(1, time.Hour, time.Now())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/blocklist?prefix=41555", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body)
	}
}

func TestParseSinceParam(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantSec int64
		wantID  uint64
	}{
		{"empty", "", 0, 0},
		{"zero", "0", 0, 0},
		{"no dot", "12345", 0, 0},
		{"negative sec", "-5.10", 0, 0},
		{"non-numeric sec", "abc.10", 0, 0},
		{"non-numeric id", "100.xyz", 0, 0},
		{"valid compound cursor", "1700000000.42", 1700000000, 42},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sec, id := parseSinceParam(tc.raw)
			if sec != tc.wantSec || id != tc.wantID {
				t.Errorf("parseSinceParam(%q) = (%d, %d), want (%d, %d)", tc.raw, sec, id, tc.wantSec, tc.wantID)
			}
		})
	}
}

func TestIsValidBlocklistPrefix(t *testing.T) {
	cases := []struct {
		prefix string
		want   bool
	}{
		{"415555", true},
		{"000000", true},
		{"41555", false},
		{"4155555", false},
		{"41555a", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isValidBlocklistPrefix(tc.prefix); got != tc.want {
			t.Errorf("isValidBlocklistPrefix(%q) = %v, want %v", tc.prefix, got, tc.want)
		}
	}
}

func TestParseLimitParam(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"empty defaults", "", defaultBlocklistLimit},
		{"non-numeric defaults", "abc", defaultBlocklistLimit},
		{"zero defaults", "0", defaultBlocklistLimit},
		{"negative defaults", "-5", defaultBlocklistLimit},
		{"within range", "100", 100},
		{"above max clamps", "5000", maxBlocklistLimit},
		{"exactly max", "1000", maxBlocklistLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseLimitParam(tc.raw); got != tc.want {
				t.Errorf("parseLimitParam(%q) = %d, want %d", tc.raw, got, tc.want)
			}
		})
	}
}

// TestBlocklistEndpoint_StoreError_500 confirms handleList surfaces
// store.BlocklistDelta's error (a closed DB) as a 500.
func TestBlocklistEndpoint_StoreError_500(t *testing.T) {
	database := dbtest.SetupDB(t)
	database.Close()

	h := &blocklistHandler{db: database}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/blocklist", nil)
	req = req.WithContext(deviceCtx(1))
	rec := httptest.NewRecorder()
	h.handleList(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500; body=%s", rec.Code, rec.Body)
	}
}

func TestBlocklistEndpoint_DB(t *testing.T) {
	database := dbtest.SetupDB(t)

	cfg := config.Config{
		AttestMode:        "mock",
		DeviceTokenSecret: "blocklist-test-secret",
		DeviceTokenTTL:    time.Hour,
		ChallengeTTL:      5 * time.Minute,
	}
	router := NewRouter(database, cfg)
	signer := token.NewSigner([]byte(cfg.DeviceTokenSecret))

	_, callerToken := mintDeviceToken(t, database, signer, 1.0)

	blockedNumber := uniquePhoneNumber()
	_, d1 := mintDeviceToken(t, database, signer, 1.0)
	_, d2 := mintDeviceToken(t, database, signer, 1.0)
	_, d3 := mintDeviceToken(t, database, signer, 1.0)
	for i, tok := range []string{d1, d2, d3} {
		var name string
		if i == 0 {
			name = "Robo Caller" // only one distinct-device name report: below MinNameAgreement
		}
		rec := postReport(router, tok, reportRequestBody{Number: blockedNumber, Category: "scam", Vote: "spam", Name: name})
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed report %d status = %d, want 201; body=%s", i, rec.Code, rec.Body)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/blocklist", nil)
	req.Header.Set("Authorization", "Bearer "+callerToken)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}

	_, data := decodeEnvelope(t, rec.Body.Bytes())
	var resp blocklistResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal blocklist response: %v", err)
	}

	if resp.Count != len(resp.Entries) {
		t.Errorf("count = %d, want len(entries) = %d", resp.Count, len(resp.Entries))
	}
	if resp.Cursor == "" || resp.Cursor == "0.0" {
		t.Errorf("cursor = %q, want a non-zero compound cursor", resp.Cursor)
	}

	var found *blocklistEntryResponse
	for i := range resp.Entries {
		if resp.Entries[i].Number == blockedNumber {
			found = &resp.Entries[i]
		}
	}
	if found == nil {
		t.Fatalf("blocked number missing from response entries: %+v", resp.Entries)
	}
	if found.Action != "block" {
		t.Errorf("action = %q, want %q", found.Action, "block")
	}
	if found.SpoofSuspected {
		t.Errorf("spoof_suspected = true, want false (no prefix param)")
	}
	if found.Name != nil {
		t.Errorf("name = %q, want nil (only 1 distinct-device report, below agreement floor)", *found.Name)
	}
}

func TestBlocklistEndpoint_DeltaAndSpoofPrefix(t *testing.T) {
	database := dbtest.SetupDB(t)

	cfg := config.Config{
		AttestMode:        "mock",
		DeviceTokenSecret: "blocklist-delta-secret",
		DeviceTokenTTL:    time.Hour,
		ChallengeTTL:      5 * time.Minute,
	}
	router := NewRouter(database, cfg)
	signer := token.NewSigner([]byte(cfg.DeviceTokenSecret))

	_, callerToken := mintDeviceToken(t, database, signer, 1.0)

	// One fresh "other"-category spam report scores 0.75: below the 2.0
	// suspect threshold (stays "unknown") but above 0, the sparse-signal
	// case the spoof query targets. uniquePhoneNumber always yields the
	// "415555" NPA-NXX prefix, matched by the prefix param below.
	spoofNumber := uniquePhoneNumber()
	_, spoofDeviceToken := mintDeviceToken(t, database, signer, 1.0)
	rec := postReport(router, spoofDeviceToken, reportRequestBody{Number: spoofNumber, Category: "other", Vote: "spam"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed spoof report status = %d, want 201; body=%s", rec.Code, rec.Body)
	}

	// Without a prefix, the sparse-signal number must not appear.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/blocklist", nil)
	req.Header.Set("Authorization", "Bearer "+callerToken)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req)
	if rec1.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec1.Code, rec1.Body)
	}
	_, data1 := decodeEnvelope(t, rec1.Body.Bytes())
	var resp1 blocklistResponse
	if err := json.Unmarshal(data1, &resp1); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, e := range resp1.Entries {
		if e.Number == spoofNumber {
			t.Errorf("spoof number present without a prefix param")
		}
	}

	// With the caller's own matching prefix, it appears as a label with
	// spoof_suspected true.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/blocklist?prefix=415555", nil)
	req2.Header.Set("Authorization", "Bearer "+callerToken)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec2.Code, rec2.Body)
	}
	_, data2 := decodeEnvelope(t, rec2.Body.Bytes())
	var resp2 blocklistResponse
	if err := json.Unmarshal(data2, &resp2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var found *blocklistEntryResponse
	for i := range resp2.Entries {
		if resp2.Entries[i].Number == spoofNumber {
			found = &resp2.Entries[i]
		}
	}
	if found == nil {
		t.Fatalf("spoof number missing with matching prefix param: %+v", resp2.Entries)
	}
	if found.Action != "label" {
		t.Errorf("action = %q, want %q", found.Action, "label")
	}
	if !found.SpoofSuspected {
		t.Errorf("spoof_suspected = false, want true")
	}

	// Delta: calling again with since=cursor and no new activity returns no
	// entries, echoing the same cursor back.
	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/blocklist?prefix=415555&since="+resp2.Cursor, nil)
	req3.Header.Set("Authorization", "Bearer "+callerToken)
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec3.Code, rec3.Body)
	}
	_, data3 := decodeEnvelope(t, rec3.Body.Bytes())
	var resp3 blocklistResponse
	if err := json.Unmarshal(data3, &resp3); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp3.Entries) != 0 {
		t.Errorf("delta entries = %d, want 0 (no activity since cursor)", len(resp3.Entries))
	}
	if resp3.Cursor != resp2.Cursor {
		t.Errorf("delta cursor = %q, want echoed %q", resp3.Cursor, resp2.Cursor)
	}
}
