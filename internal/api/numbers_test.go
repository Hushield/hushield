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

func getNumber(router http.Handler, tok, e164, query string) *httptest.ResponseRecorder {
	url := "/api/v1/numbers/" + e164
	if query != "" {
		url += "?" + query
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestNumbersEndpoint_Blocked_DB(t *testing.T) {
	database := dbtest.SetupDB(t)

	cfg := config.Config{
		AttestMode:        "mock",
		DeviceTokenSecret: "numbers-blocked-secret",
		DeviceTokenTTL:    time.Hour,
		ChallengeTTL:      5 * time.Minute,
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

	rec := getNumber(router, callerToken, number, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	_, data := decodeEnvelope(t, rec.Body.Bytes())
	var resp numberLookupResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Number != number {
		t.Errorf("number = %q, want %q", resp.Number, number)
	}
	if resp.Status != "blocked" {
		t.Errorf("status = %q, want blocked", resp.Status)
	}
	if resp.Action != "block" {
		t.Errorf("action = %q, want block", resp.Action)
	}
	if resp.Category == nil || *resp.Category != "scam" {
		t.Errorf("category = %v, want scam", resp.Category)
	}
}

func TestNumbersEndpoint_Suspected_DB(t *testing.T) {
	database := dbtest.SetupDB(t)

	cfg := config.Config{
		AttestMode:        "mock",
		DeviceTokenSecret: "numbers-suspected-secret",
		DeviceTokenTTL:    time.Hour,
		ChallengeTTL:      5 * time.Minute,
	}
	router := NewRouter(database, cfg)
	signer := token.NewSigner([]byte(cfg.DeviceTokenSecret))

	_, callerToken := mintDeviceToken(t, database, signer, 1.0)

	number := uniquePhoneNumber()
	_, d1 := mintDeviceToken(t, database, signer, 1.0)
	rec := postReport(router, d1, reportRequestBody{Number: number, Category: "scam", Vote: "spam"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed report status = %d, want 201; body=%s", rec.Code, rec.Body)
	}

	rec = getNumber(router, callerToken, number, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	_, data := decodeEnvelope(t, rec.Body.Bytes())
	var resp numberLookupResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != "suspected" {
		t.Errorf("status = %q, want suspected", resp.Status)
	}
	if resp.Action != "label" {
		t.Errorf("action = %q, want label", resp.Action)
	}
}

func TestNumbersEndpoint_Allowlisted_DB(t *testing.T) {
	database := dbtest.SetupDB(t)

	cfg := config.Config{
		AttestMode:        "mock",
		DeviceTokenSecret: "numbers-allowlisted-secret",
		DeviceTokenTTL:    time.Hour,
		ChallengeTTL:      5 * time.Minute,
		AdminToken:        "numbers-allowlisted-admin-token",
	}
	router := NewRouter(database, cfg)
	signer := token.NewSigner([]byte(cfg.DeviceTokenSecret))

	_, callerToken := mintDeviceToken(t, database, signer, 1.0)

	number := uniquePhoneNumber()
	overrideRec := postOverride(router, cfg.AdminToken, overrideRequestBody{Number: number, Mode: "allow"})
	if overrideRec.Code != http.StatusOK {
		t.Fatalf("admin override status = %d, want 200; body=%s", overrideRec.Code, overrideRec.Body)
	}

	rec := getNumber(router, callerToken, number, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	_, data := decodeEnvelope(t, rec.Body.Bytes())
	var resp numberLookupResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != "allowlisted" {
		t.Errorf("status = %q, want allowlisted", resp.Status)
	}
	if resp.Action != "allow" {
		t.Errorf("action = %q, want allow", resp.Action)
	}
}

func TestNumbersEndpoint_NotFound_DB(t *testing.T) {
	database := dbtest.SetupDB(t)

	cfg := config.Config{
		AttestMode:        "mock",
		DeviceTokenSecret: "numbers-notfound-secret",
		DeviceTokenTTL:    time.Hour,
		ChallengeTTL:      5 * time.Minute,
	}
	router := NewRouter(database, cfg)
	signer := token.NewSigner([]byte(cfg.DeviceTokenSecret))

	_, callerToken := mintDeviceToken(t, database, signer, 1.0)

	number := uniquePhoneNumber()

	rec := getNumber(router, callerToken, number, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	_, data := decodeEnvelope(t, rec.Body.Bytes())
	var resp numberLookupResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != "unknown" {
		t.Errorf("status = %q, want unknown", resp.Status)
	}
	if resp.Action != "none" {
		t.Errorf("action = %q, want none", resp.Action)
	}
	if resp.Category != nil {
		t.Errorf("category = %v, want nil", resp.Category)
	}
	if resp.Name != nil {
		t.Errorf("name = %v, want nil", resp.Name)
	}
	if resp.SpoofSuspected {
		t.Errorf("spoof_suspected = true, want false")
	}
}

func TestNumbersEndpoint_NotFound_SpoofSuspected_DB(t *testing.T) {
	database := dbtest.SetupDB(t)

	cfg := config.Config{
		AttestMode:        "mock",
		DeviceTokenSecret: "numbers-notfound-spoof-secret",
		DeviceTokenTTL:    time.Hour,
		ChallengeTTL:      5 * time.Minute,
	}
	router := NewRouter(database, cfg)
	signer := token.NewSigner([]byte(cfg.DeviceTokenSecret))

	_, callerToken := mintDeviceToken(t, database, signer, 1.0)

	// uniquePhoneNumber always yields the "415555" NPA-NXX prefix.
	number := uniquePhoneNumber()

	rec := getNumber(router, callerToken, number, "prefix=415555")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	_, data := decodeEnvelope(t, rec.Body.Bytes())
	var resp numberLookupResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != "unknown" || resp.Action != "none" {
		t.Errorf("status/action = %q/%q, want unknown/none", resp.Status, resp.Action)
	}
	if !resp.SpoofSuspected {
		t.Errorf("spoof_suspected = false, want true for matching prefix even when not found")
	}
}

func TestNumbersEndpoint_NameAgreement_DB(t *testing.T) {
	database := dbtest.SetupDB(t)

	cfg := config.Config{
		AttestMode:        "mock",
		DeviceTokenSecret: "numbers-name-secret",
		DeviceTokenTTL:    time.Hour,
		ChallengeTTL:      5 * time.Minute,
	}
	router := NewRouter(database, cfg)
	signer := token.NewSigner([]byte(cfg.DeviceTokenSecret))

	_, callerToken := mintDeviceToken(t, database, signer, 1.0)

	number := uniquePhoneNumber()
	_, d1 := mintDeviceToken(t, database, signer, 1.0)
	_, d2 := mintDeviceToken(t, database, signer, 1.0)
	rec := postReport(router, d1, reportRequestBody{Number: number, Category: "scam", Vote: "spam", Name: "Acme Collections"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed report 1 status = %d, want 201; body=%s", rec.Code, rec.Body)
	}
	rec = postReport(router, d2, reportRequestBody{Number: number, Category: "scam", Vote: "spam", Name: "Acme Collections"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed report 2 status = %d, want 201; body=%s", rec.Code, rec.Body)
	}

	rec = getNumber(router, callerToken, number, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	_, data := decodeEnvelope(t, rec.Body.Bytes())
	var resp numberLookupResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Name == nil || *resp.Name != "Acme Collections" {
		t.Errorf("name = %v, want %q (2 devices agree)", resp.Name, "Acme Collections")
	}

	loneNumber := uniquePhoneNumber()
	_, d3 := mintDeviceToken(t, database, signer, 1.0)
	rec = postReport(router, d3, reportRequestBody{Number: loneNumber, Category: "scam", Vote: "spam", Name: "Solo Name"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed lone report status = %d, want 201; body=%s", rec.Code, rec.Body)
	}

	rec = getNumber(router, callerToken, loneNumber, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	_, data = decodeEnvelope(t, rec.Body.Bytes())
	var loneResp numberLookupResponse
	if err := json.Unmarshal(data, &loneResp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if loneResp.Name != nil {
		t.Errorf("name = %q, want nil (only 1 device, below agreement floor)", *loneResp.Name)
	}
}

func TestNumbersEndpoint_SpoofSuspected_DB(t *testing.T) {
	database := dbtest.SetupDB(t)

	cfg := config.Config{
		AttestMode:        "mock",
		DeviceTokenSecret: "numbers-spoof-secret",
		DeviceTokenTTL:    time.Hour,
		ChallengeTTL:      5 * time.Minute,
	}
	router := NewRouter(database, cfg)
	signer := token.NewSigner([]byte(cfg.DeviceTokenSecret))

	_, callerToken := mintDeviceToken(t, database, signer, 1.0)

	// uniquePhoneNumber always yields the "415555" NPA-NXX prefix.
	number := uniquePhoneNumber()
	_, d1 := mintDeviceToken(t, database, signer, 1.0)
	rec := postReport(router, d1, reportRequestBody{Number: number, Category: "scam", Vote: "spam"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed report status = %d, want 201; body=%s", rec.Code, rec.Body)
	}

	rec = getNumber(router, callerToken, number, "prefix=415555")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	_, data := decodeEnvelope(t, rec.Body.Bytes())
	var resp numberLookupResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.SpoofSuspected {
		t.Errorf("spoof_suspected = false, want true")
	}

	rec = getNumber(router, callerToken, number, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	_, data = decodeEnvelope(t, rec.Body.Bytes())
	var resp2 numberLookupResponse
	if err := json.Unmarshal(data, &resp2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp2.SpoofSuspected {
		t.Errorf("spoof_suspected = true, want false without a prefix param")
	}
}

func TestNumbersEndpoint_InvalidNumber(t *testing.T) {
	secret := "numbers-invalid-number-secret"
	router := NewRouter(nil, config.Config{DeviceTokenSecret: secret, DeviceTokenTTL: time.Hour})
	signer := token.NewSigner([]byte(secret))
	tok := signer.Issue(1, time.Hour, time.Now())

	rec := getNumber(router, tok, "not-a-number", "")
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

func TestNumbersEndpoint_InvalidPrefix(t *testing.T) {
	secret := "numbers-invalid-prefix-secret"
	router := NewRouter(nil, config.Config{DeviceTokenSecret: secret, DeviceTokenTTL: time.Hour})
	signer := token.NewSigner([]byte(secret))
	tok := signer.Issue(1, time.Hour, time.Now())

	rec := getNumber(router, tok, "+14155552671", "prefix=abc")
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

func TestNumbersEndpoint_MissingToken(t *testing.T) {
	router := NewRouter(nil, config.Config{DeviceTokenSecret: "secret"})

	rec := getNumber(router, "", "+14155552671", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body)
	}
}
