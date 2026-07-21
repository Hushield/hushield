package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"spamfilter/internal/config"
	"spamfilter/internal/dbtest"
)

// TestEndToEnd_FullLifecycle drives the entire router through real HTTP
// calls against an httptest.NewServer, exactly as an iOS client (or the
// smoke script) would: attest devices, report a number to blocked, counter
// -report it back down, surface a community caller name, flag a
// neighbor-spoof match, and exercise admin overrides. This is the durable
// end-to-end guard for the whole backend, not just its individual units.
func TestEndToEnd_FullLifecycle(t *testing.T) {
	database := dbtest.SetupDB(t)

	cfg := config.Config{
		AttestMode:        "mock",
		DeviceTokenSecret: "e2e-test-secret",
		DeviceTokenTTL:    time.Hour,
		ChallengeTTL:      5 * time.Minute,
		AdminToken:        "e2e-admin-token",
	}
	server := httptest.NewServer(NewRouter(database, cfg))
	t.Cleanup(server.Close)
	client := server.Client()
	base := server.URL

	// --- 1. Attest 3 distinct devices -> 3 distinct device tokens. ---
	keyIDs := []string{"e2e-device-1", "e2e-device-2", "e2e-device-3"}
	tokens := make([]string, len(keyIDs))
	for i, keyID := range keyIDs {
		tokens[i] = e2eAttestDevice(t, client, base, keyID)
		if tokens[i] == "" {
			t.Fatalf("attest device %s: empty device token", keyID)
		}
	}
	if tokens[0] == tokens[1] || tokens[0] == tokens[2] || tokens[1] == tokens[2] {
		t.Fatalf("attested device tokens are not distinct: %v", tokens)
	}

	// --- 2. All 3 devices report numberA as scam/spam -> blocked. ---
	numberA := "+14155550188"
	for i, tok := range tokens {
		status, body := e2ePostJSON(t, client, base+"/api/v1/reports", tok, map[string]string{
			"number":   numberA,
			"category": "scam",
			"vote":     "spam",
		})
		if status != http.StatusCreated {
			t.Fatalf("report %d on numberA: status = %d, want 201; body=%s", i, status, body)
		}
	}

	entries := e2eBlocklist(t, client, base, tokens[0], "since=0")
	entryA, ok := e2eFindEntry(entries, numberA)
	if !ok {
		t.Fatalf("numberA %q missing from blocklist after 3 scam reports; entries=%+v", numberA, entries)
	}
	if entryA.Action != "block" {
		t.Errorf("numberA action = %q, want %q (3 fresh scam x trust 1.0 = 6.0 >= 5.0 block threshold)", entryA.Action, "block")
	}

	// --- 3. Same 3 devices counter-report not_spam -> no longer a block entry. ---
	for i, tok := range tokens {
		status, body := e2ePostJSON(t, client, base+"/api/v1/reports", tok, map[string]string{
			"number": numberA,
			"vote":   "not_spam",
		})
		if status != http.StatusCreated {
			t.Fatalf("counter-report %d on numberA: status = %d, want 201; body=%s", i, status, body)
		}
	}

	entries = e2eBlocklist(t, client, base, tokens[0], "since=0")
	entryA, ok = e2eFindEntry(entries, numberA)
	if !ok {
		t.Fatalf("numberA %q missing after counter-reports, want an unblock tombstone (it was previously blocked)", numberA)
	}
	if entryA.Action != "unblock" {
		t.Errorf("numberA action = %q, want %q (score floored to 0, status unknown, below suspect threshold, but was previously blockable)", entryA.Action, "unblock")
	}

	// --- 4 & 5. Two devices report numberB with a caller name, driving it to
	// suspected/blocked; assert the caller name surfaces and the
	// neighbor-spoof flag is prefix-scoped. numberB shares numberA's
	// "415555" NPA-NXX so a prefix=415555 query exercises the spoof flag.
	numberB := "+14155559876"
	for i, tok := range tokens[:2] {
		status, body := e2ePostJSON(t, client, base+"/api/v1/reports", tok, map[string]string{
			"number":   numberB,
			"category": "scam",
			"vote":     "spam",
			"name":     "Test Spammer",
		})
		if status != http.StatusCreated {
			t.Fatalf("named report %d on numberB: status = %d, want 201; body=%s", i, status, body)
		}
	}

	entries = e2eBlocklist(t, client, base, tokens[2], "since=0")
	entryB, ok := e2eFindEntry(entries, numberB)
	if !ok {
		t.Fatalf("numberB %q missing from blocklist after 2 named scam reports; entries=%+v", numberB, entries)
	}
	if entryB.Name == nil || *entryB.Name != "Test Spammer" {
		t.Errorf("numberB name = %v, want %q", entryB.Name, "Test Spammer")
	}
	if entryB.SpoofSuspected {
		t.Errorf("numberB spoof_suspected = true without a prefix param, want false")
	}

	prefixedEntries := e2eBlocklist(t, client, base, tokens[2], "since=0&prefix=415555")
	prefixedEntryB, ok := e2eFindEntry(prefixedEntries, numberB)
	if !ok {
		t.Fatalf("numberB %q missing from prefix-scoped blocklist; entries=%+v", numberB, prefixedEntries)
	}
	if !strings.HasPrefix(numberB, "+1415555") {
		t.Fatalf("test setup error: numberB %q does not start with +1415555", numberB)
	}
	if !prefixedEntryB.SpoofSuspected {
		t.Errorf("numberB spoof_suspected = false with prefix=415555, want true")
	}

	// --- 6. Admin override: allow numberA (drops it from the blocklist),
	// block a fresh numberC (adds it), and reject a missing admin token. ---
	overrideStatus, overrideBody := e2ePostJSON(t, client, base+"/api/v1/admin/overrides", cfg.AdminToken, map[string]string{
		"number": numberA,
		"mode":   "allow",
	})
	if overrideStatus != http.StatusOK {
		t.Fatalf("admin allow override on numberA: status = %d, want 200; body=%s", overrideStatus, overrideBody)
	}

	entries = e2eBlocklist(t, client, base, tokens[0], "since=0")
	entryA, ok = e2eFindEntry(entries, numberA)
	if !ok {
		t.Fatalf("numberA %q missing after allow override, want an unblock tombstone (allowlisted, previously blockable)", numberA)
	}
	if entryA.Action != "unblock" {
		t.Errorf("numberA action = %q, want %q (allowlisted after admin override)", entryA.Action, "unblock")
	}

	numberC := "+14085551234"
	overrideStatus, overrideBody = e2ePostJSON(t, client, base+"/api/v1/admin/overrides", cfg.AdminToken, map[string]string{
		"number": numberC,
		"mode":   "block",
	})
	if overrideStatus != http.StatusOK {
		t.Fatalf("admin block override on numberC: status = %d, want 200; body=%s", overrideStatus, overrideBody)
	}

	entries = e2eBlocklist(t, client, base, tokens[0], "since=0")
	entryC, ok := e2eFindEntry(entries, numberC)
	if !ok {
		t.Fatalf("numberC %q missing from blocklist after block override; entries=%+v", numberC, entries)
	}
	if entryC.Action != "block" {
		t.Errorf("numberC action = %q, want %q", entryC.Action, "block")
	}

	noAuthStatus, noAuthBody := e2ePostJSON(t, client, base+"/api/v1/admin/overrides", "", map[string]string{
		"number": "+14155550001",
		"mode":   "block",
	})
	if noAuthStatus != http.StatusUnauthorized {
		t.Errorf("admin override with no token: status = %d, want 401; body=%s", noAuthStatus, noAuthBody)
	}
}

// e2eAttestDevice drives the challenge -> verify flow for keyID and returns
// the resulting device token.
func e2eAttestDevice(t *testing.T, client *http.Client, base, keyID string) string {
	t.Helper()

	chStatus, chBody := e2ePostJSON(t, client, base+"/api/v1/attest/challenge", "", nil)
	if chStatus != http.StatusOK {
		t.Fatalf("attest challenge for %s: status = %d, want 200; body=%s", keyID, chStatus, chBody)
	}
	_, chData := decodeEnvelope(t, chBody)
	var ch challengeResponse
	if err := json.Unmarshal(chData, &ch); err != nil {
		t.Fatalf("unmarshal challenge response for %s: %v", keyID, err)
	}

	vStatus, vBody := e2ePostJSON(t, client, base+"/api/v1/attest/verify", "", map[string]string{
		"key_id":      keyID,
		"attestation": base64.StdEncoding.EncodeToString([]byte("mock-attestation-" + keyID)),
		"challenge":   ch.Challenge,
	})
	if vStatus != http.StatusOK {
		t.Fatalf("attest verify for %s: status = %d, want 200; body=%s", keyID, vStatus, vBody)
	}
	_, vData := decodeEnvelope(t, vBody)
	var v verifyResponse
	if err := json.Unmarshal(vData, &v); err != nil {
		t.Fatalf("unmarshal verify response for %s: %v", keyID, err)
	}
	return v.DeviceToken
}

// e2eBlocklist GETs /api/v1/blocklist with the given raw query string (e.g.
// "since=0" or "since=0&prefix=415555") and returns its decoded entries.
func e2eBlocklist(t *testing.T, client *http.Client, base, deviceToken, rawQuery string) []blocklistEntryResponse {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/blocklist?%s", base, rawQuery), nil)
	if err != nil {
		t.Fatalf("build blocklist request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+deviceToken)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET blocklist: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read blocklist response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET blocklist: status = %d, want 200; body=%s", resp.StatusCode, body)
	}

	_, data := decodeEnvelope(t, body)
	var payload blocklistResponse
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal blocklist response: %v", err)
	}
	return payload.Entries
}

func e2eFindEntry(entries []blocklistEntryResponse, number string) (blocklistEntryResponse, bool) {
	for _, e := range entries {
		if e.Number == number {
			return e, true
		}
	}
	return blocklistEntryResponse{}, false
}

// e2ePostJSON POSTs payload (or an empty body if nil) as JSON to url, with an
// "Authorization: Bearer <bearer>" header when bearer is non-empty, and
// returns the response status code and raw body.
func e2ePostJSON(t *testing.T, client *http.Client, url, bearer string, payload any) (int, []byte) {
	t.Helper()

	var reader io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(http.MethodPost, url, reader)
	if err != nil {
		t.Fatalf("build request for %s: %v", url, err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body from %s: %v", url, err)
	}
	return resp.StatusCode, body
}
