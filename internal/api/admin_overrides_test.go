package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"spamfilter/internal/config"
	"spamfilter/internal/dbtest"
	"spamfilter/internal/scoring"
	"spamfilter/internal/store"
)

// insertOverrideTestDevice inserts a minimal devices row and returns its
// device_id, for tests that only need a device to attach reports to (no
// device token required).
func insertOverrideTestDevice(t *testing.T, database *sql.DB, keyID string, trustWeight float64) uint64 {
	t.Helper()
	res, err := database.Exec(
		"INSERT INTO devices (key_id, public_key, trust_weight) VALUES (?, ?, ?)",
		keyID, []byte("pubkey-"+keyID), trustWeight,
	)
	if err != nil {
		t.Fatalf("insert device: %v", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("last insert id: %v", err)
	}
	return uint64(id)
}

type overrideRequestBody struct {
	Number string `json:"number"`
	Mode   string `json:"mode"`
	Reason string `json:"reason,omitempty"`
	Admin  string `json:"admin,omitempty"`
}

func postOverride(router http.Handler, adminToken string, body overrideRequestBody) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/overrides", strings.NewReader(string(b)))
	if adminToken != "" {
		req.Header.Set("Authorization", "Bearer "+adminToken)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestAdminOverridesEndpoint_AllowWinsOverCommunityBlock_DB(t *testing.T) {
	database := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	cfg := config.Config{
		AttestMode:        "mock",
		DeviceTokenSecret: "admin-overrides-test-secret",
		DeviceTokenTTL:    time.Hour,
		ChallengeTTL:      5 * time.Minute,
		AdminToken:        "test-admin-token",
	}
	router := NewRouter(database, cfg)

	number := uniquePhoneNumber()
	numberID, err := store.UpsertPhoneNumber(ctx, database, number, now)
	if err != nil {
		t.Fatalf("UpsertPhoneNumber: %v", err)
	}
	for i, keyID := range []string{"admin-override-device-1", "admin-override-device-2", "admin-override-device-3"} {
		deviceID := insertOverrideTestDevice(t, database, keyID, 1.0)
		if _, err := store.UpsertReport(ctx, database, deviceID, numberID, scoring.CategoryScam, scoring.VoteSpam, now); err != nil {
			t.Fatalf("UpsertReport #%d: %v", i, err)
		}
	}
	status, err := store.RecomputeNumber(ctx, database, numberID, now)
	if err != nil {
		t.Fatalf("RecomputeNumber: %v", err)
	}
	if status != scoring.StatusBlocked {
		t.Fatalf("precondition: status = %s, want blocked", status)
	}

	rec := postOverride(router, cfg.AdminToken, overrideRequestBody{Number: number, Mode: "allow", Reason: "verified legit business"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	_, data := decodeEnvelope(t, rec.Body.Bytes())
	var resp overrideResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Number != number {
		t.Errorf("response number = %q, want %q", resp.Number, number)
	}
	if resp.Status != string(scoring.StatusAllowlisted) {
		t.Errorf("response status = %q, want %q", resp.Status, scoring.StatusAllowlisted)
	}

	var dbStatus string
	if err := database.QueryRow("SELECT status FROM phone_numbers WHERE phone_number_id = ?", numberID).Scan(&dbStatus); err != nil {
		t.Fatalf("select phone_numbers.status: %v", err)
	}
	if dbStatus != string(scoring.StatusAllowlisted) {
		t.Errorf("db status = %q, want %q", dbStatus, scoring.StatusAllowlisted)
	}

	// An allowlisted number must never appear in the blocklist delta as a
	// block or label entry.
	entries, _, _, err := store.BlocklistDelta(ctx, database, 0, 0, "", 500)
	if err != nil {
		t.Fatalf("BlocklistDelta: %v", err)
	}
	for _, e := range entries {
		if e.Number == number {
			t.Errorf("allowlisted number %q unexpectedly present in BlocklistDelta: %+v", number, e)
		}
	}
}

func TestAdminOverridesEndpoint_BlockOnUnknownNumber_DB(t *testing.T) {
	database := dbtest.SetupDB(t)

	cfg := config.Config{
		AttestMode:        "mock",
		DeviceTokenSecret: "admin-overrides-block-test-secret",
		DeviceTokenTTL:    time.Hour,
		ChallengeTTL:      5 * time.Minute,
		AdminToken:        "test-admin-token-2",
	}
	router := NewRouter(database, cfg)

	// No reports at all: the number starts out entirely unknown to the
	// community.
	number := uniquePhoneNumber()

	rec := postOverride(router, cfg.AdminToken, overrideRequestBody{Number: number, Mode: "block", Reason: "known scammer", Admin: "jbrahy"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	_, data := decodeEnvelope(t, rec.Body.Bytes())
	var resp overrideResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Status != string(scoring.StatusOverriddenBlock) {
		t.Errorf("response status = %q, want %q", resp.Status, scoring.StatusOverriddenBlock)
	}

	entries, _, _, err := store.BlocklistDelta(context.Background(), database, 0, 0, "", 500)
	if err != nil {
		t.Fatalf("BlocklistDelta: %v", err)
	}
	var found *store.BlocklistEntry
	for i := range entries {
		if entries[i].Number == number {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("overridden_block number %q not present in BlocklistDelta", number)
	}
	if found.Action != "block" {
		t.Errorf("action = %q, want block", found.Action)
	}
}

func TestAdminOverridesEndpoint_ReflipUpdatesSingleRow_DB(t *testing.T) {
	database := dbtest.SetupDB(t)

	cfg := config.Config{
		AttestMode:        "mock",
		DeviceTokenSecret: "admin-overrides-reflip-test-secret",
		DeviceTokenTTL:    time.Hour,
		ChallengeTTL:      5 * time.Minute,
		AdminToken:        "test-admin-token-3",
	}
	router := NewRouter(database, cfg)
	number := uniquePhoneNumber()

	rec := postOverride(router, cfg.AdminToken, overrideRequestBody{Number: number, Mode: "block"})
	if rec.Code != http.StatusOK {
		t.Fatalf("block status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	_, data := decodeEnvelope(t, rec.Body.Bytes())
	var blockResp overrideResponse
	if err := json.Unmarshal(data, &blockResp); err != nil {
		t.Fatalf("unmarshal block response: %v", err)
	}
	if blockResp.Status != string(scoring.StatusOverriddenBlock) {
		t.Errorf("block response status = %q, want %q", blockResp.Status, scoring.StatusOverriddenBlock)
	}

	rec = postOverride(router, cfg.AdminToken, overrideRequestBody{Number: number, Mode: "allow"})
	if rec.Code != http.StatusOK {
		t.Fatalf("allow status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	_, data = decodeEnvelope(t, rec.Body.Bytes())
	var allowResp overrideResponse
	if err := json.Unmarshal(data, &allowResp); err != nil {
		t.Fatalf("unmarshal allow response: %v", err)
	}
	if allowResp.Status != string(scoring.StatusAllowlisted) {
		t.Errorf("allow response status = %q, want %q", allowResp.Status, scoring.StatusAllowlisted)
	}

	var numberID uint64
	if err := database.QueryRow("SELECT phone_number_id FROM phone_numbers WHERE number = ?", number).Scan(&numberID); err != nil {
		t.Fatalf("select phone_number_id: %v", err)
	}
	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM admin_overrides WHERE phone_number_id = ?", numberID).Scan(&count); err != nil {
		t.Fatalf("count admin_overrides: %v", err)
	}
	if count != 1 {
		t.Errorf("admin_overrides row count = %d, want 1 (re-flip must update, not duplicate)", count)
	}
}

func TestAdminOverridesEndpoint_MissingToken(t *testing.T) {
	router := NewRouter(nil, config.Config{DeviceTokenSecret: "secret", AdminToken: "configured-token"})

	rec := postOverride(router, "", overrideRequestBody{Number: "+14155552671", Mode: "block"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body)
	}
}

func TestAdminOverridesEndpoint_WrongToken(t *testing.T) {
	router := NewRouter(nil, config.Config{DeviceTokenSecret: "secret", AdminToken: "configured-token"})

	rec := postOverride(router, "wrong-token", overrideRequestBody{Number: "+14155552671", Mode: "block"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body)
	}
}

func TestAdminOverridesEndpoint_AdminDisabled(t *testing.T) {
	router := NewRouter(nil, config.Config{DeviceTokenSecret: "secret"}) // AdminToken left empty

	rec := postOverride(router, "any-token", overrideRequestBody{Number: "+14155552671", Mode: "block"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body)
	}
}

func TestAdminOverridesEndpoint_InvalidNumber(t *testing.T) {
	h := &adminOverridesHandler{}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/overrides", strings.NewReader(`{"number":"not-a-number","mode":"block"}`))
	req = req.WithContext(reqCtx())
	rec := httptest.NewRecorder()
	h.handleCreate(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body)
	}
	success, errs := decodeEnvelopeErrors(t, rec.Body.Bytes())
	if success {
		t.Error("success = true, want false")
	}
	if len(errs) != 1 || errs[0].Field != "number" {
		t.Errorf("errors = %+v, want single error on field=number", errs)
	}
}

func TestAdminOverridesEndpoint_InvalidMode(t *testing.T) {
	h := &adminOverridesHandler{}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/overrides", strings.NewReader(`{"number":"+14155552671","mode":"nonsense"}`))
	req = req.WithContext(reqCtx())
	rec := httptest.NewRecorder()
	h.handleCreate(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body)
	}
	success, errs := decodeEnvelopeErrors(t, rec.Body.Bytes())
	if success {
		t.Error("success = true, want false")
	}
	if len(errs) != 1 || errs[0].Field != "mode" {
		t.Errorf("errors = %+v, want single error on field=mode", errs)
	}
}

func TestAdminOverridesEndpoint_ReasonTooLong(t *testing.T) {
	h := &adminOverridesHandler{}

	longReason := strings.Repeat("a", 256)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/overrides", strings.NewReader(`{"number":"+14155552671","mode":"block","reason":"`+longReason+`"}`))
	req = req.WithContext(reqCtx())
	rec := httptest.NewRecorder()
	h.handleCreate(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body)
	}
}

func TestAdminOverridesEndpoint_AdminTooLong(t *testing.T) {
	h := &adminOverridesHandler{}

	longAdmin := strings.Repeat("a", 101)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/overrides", strings.NewReader(`{"number":"+14155552671","mode":"block","admin":"`+longAdmin+`"}`))
	req = req.WithContext(reqCtx())
	rec := httptest.NewRecorder()
	h.handleCreate(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body)
	}
}

func TestAdminOverridesEndpoint_MalformedJSON(t *testing.T) {
	h := &adminOverridesHandler{}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/overrides", strings.NewReader(`{not-json`))
	req = req.WithContext(reqCtx())
	rec := httptest.NewRecorder()
	h.handleCreate(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body)
	}
}
