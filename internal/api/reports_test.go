package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"spamfilter/internal/config"
	"spamfilter/internal/token"
)

// uniquePhoneNumber returns a syntactically valid NANP E.164 number that is
// (with overwhelming probability) unused by any other test run, so tests
// don't need to truncate shared tables between runs.
func uniquePhoneNumber() string {
	n := time.Now().UnixNano() % 10000
	return fmt.Sprintf("+1415555%04d", n)
}

// mintDeviceToken inserts a devices row with the given trust_weight and
// returns its device_id and a bearer token authenticating it.
func mintDeviceToken(t *testing.T, database *sql.DB, signer *token.Signer, trustWeight float64) (uint64, string) {
	t.Helper()
	keyID := fmt.Sprintf("reports-device-%d", time.Now().UnixNano())
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
	deviceID := uint64(id)
	return deviceID, signer.Issue(deviceID, time.Hour, time.Now())
}

type reportRequestBody struct {
	Number   string `json:"number"`
	Category string `json:"category,omitempty"`
	Vote     string `json:"vote,omitempty"`
	Name     string `json:"name,omitempty"`
}

func postReport(router http.Handler, tok string, body reportRequestBody) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports", strings.NewReader(string(b)))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestReportsEndpoint_ScoringLifecycle_DB(t *testing.T) {
	database := connectAPITestDB(t)
	defer database.Close()
	prepareDevicesTable(t, database)

	cfg := config.Config{
		AttestMode:        "mock",
		DeviceTokenSecret: "reports-test-secret",
		DeviceTokenTTL:    time.Hour,
		ChallengeTTL:      5 * time.Minute,
	}
	router := NewRouter(database, cfg)
	signer := token.NewSigner([]byte(cfg.DeviceTokenSecret))

	number := uniquePhoneNumber()

	device1, token1 := mintDeviceToken(t, database, signer, 1.0)
	device2, token2 := mintDeviceToken(t, database, signer, 1.0)
	device3, token3 := mintDeviceToken(t, database, signer, 1.0)
	device4, token4 := mintDeviceToken(t, database, signer, 1.0)
	device5, token5 := mintDeviceToken(t, database, signer, 1.0)

	// 1. A single fresh scam report puts the number at suspected (score 2.0).
	rec := postReport(router, token1, reportRequestBody{Number: number, Category: "scam", Vote: "spam"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("report 1 status = %d, want 201; body=%s", rec.Code, rec.Body)
	}
	_, data := decodeEnvelope(t, rec.Body.Bytes())
	var resp1 reportResponse
	if err := json.Unmarshal(data, &resp1); err != nil {
		t.Fatalf("unmarshal report 1 response: %v", err)
	}
	if resp1.Number != number {
		t.Errorf("report 1 response number = %q, want %q", resp1.Number, number)
	}
	if resp1.Status != "suspected" {
		t.Errorf("report 1 response status = %q, want suspected", resp1.Status)
	}

	// 2. Two more distinct devices report scam -> blocked (score ~6.0).
	rec = postReport(router, token2, reportRequestBody{Number: number, Category: "scam", Vote: "spam", Name: "Robo Caller"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("report 2 status = %d, want 201; body=%s", rec.Code, rec.Body)
	}
	rec = postReport(router, token3, reportRequestBody{Number: number, Category: "scam", Vote: "spam"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("report 3 status = %d, want 201; body=%s", rec.Code, rec.Body)
	}
	_, data = decodeEnvelope(t, rec.Body.Bytes())
	var resp3 reportResponse
	if err := json.Unmarshal(data, &resp3); err != nil {
		t.Fatalf("unmarshal report 3 response: %v", err)
	}
	if resp3.Status != "blocked" {
		t.Errorf("report 3 response status = %q, want blocked", resp3.Status)
	}

	// Caller name from report 2 is stored and retrievable.
	var callerName string
	if err := database.QueryRow(
		"SELECT caller_names.name FROM caller_names WHERE caller_names.device_id = ? AND caller_names.phone_number_id = (SELECT phone_numbers.phone_number_id FROM phone_numbers WHERE phone_numbers.number = ?)",
		device2, number,
	).Scan(&callerName); err != nil {
		t.Fatalf("select caller_name: %v", err)
	}
	if callerName != "Robo Caller" {
		t.Errorf("caller_name = %q, want %q", callerName, "Robo Caller")
	}

	// 3. Same device (device1) re-reports with vote=not_spam: no duplicate
	// row, and the recomputed score reflects the change (back to suspected).
	rec = postReport(router, token1, reportRequestBody{Number: number, Category: "robocall", Vote: "not_spam"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("report 1 update status = %d, want 201; body=%s", rec.Code, rec.Body)
	}
	_, data = decodeEnvelope(t, rec.Body.Bytes())
	var resp1b reportResponse
	if err := json.Unmarshal(data, &resp1b); err != nil {
		t.Fatalf("unmarshal report 1 update response: %v", err)
	}
	if resp1b.Status != "suspected" {
		t.Errorf("after device1 flips to not_spam, status = %q, want suspected", resp1b.Status)
	}

	var phoneNumberID uint64
	if err := database.QueryRow("SELECT phone_numbers.phone_number_id FROM phone_numbers WHERE phone_numbers.number = ?", number).Scan(&phoneNumberID); err != nil {
		t.Fatalf("select phone_number_id: %v", err)
	}
	assertSingleReportRow(t, database, device1, phoneNumberID)

	// 4. Enough not_spam from distinct devices drives it back to unknown.
	rec = postReport(router, token4, reportRequestBody{Number: number, Vote: "not_spam"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("report 4 status = %d, want 201; body=%s", rec.Code, rec.Body)
	}
	rec = postReport(router, token5, reportRequestBody{Number: number, Vote: "not_spam"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("report 5 status = %d, want 201; body=%s", rec.Code, rec.Body)
	}
	_, data = decodeEnvelope(t, rec.Body.Bytes())
	var resp5 reportResponse
	if err := json.Unmarshal(data, &resp5); err != nil {
		t.Fatalf("unmarshal report 5 response: %v", err)
	}
	if resp5.Status != "unknown" {
		t.Errorf("after enough not_spam votes, status = %q, want unknown", resp5.Status)
	}

	// Reports uniqueness holds for every device that reported this number.
	for _, deviceID := range []uint64{device1, device2, device3, device4, device5} {
		assertSingleReportRow(t, database, deviceID, phoneNumberID)
	}
}

func assertSingleReportRow(t *testing.T, database *sql.DB, deviceID, phoneNumberID uint64) {
	t.Helper()
	var count int
	if err := database.QueryRow(
		"SELECT COUNT(*) FROM reports WHERE reports.device_id = ? AND reports.phone_number_id = ?",
		deviceID, phoneNumberID,
	).Scan(&count); err != nil {
		t.Fatalf("count reports for device %d: %v", deviceID, err)
	}
	if count > 1 {
		t.Errorf("reports count for device %d = %d, want <= 1", deviceID, count)
	}
}

func TestReportsEndpoint_MissingToken(t *testing.T) {
	router := NewRouter(nil, config.Config{DeviceTokenSecret: "secret"})

	body := reportRequestBody{Number: "+14155552671"}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports", strings.NewReader(string(b)))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body)
	}
}

func TestReportsEndpoint_BadNumber(t *testing.T) {
	h := &reportsHandler{}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports", strings.NewReader(`{"number":"not-a-number"}`))
	req = req.WithContext(deviceCtx(42))
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

func TestReportsEndpoint_InvalidCategory(t *testing.T) {
	h := &reportsHandler{}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports", strings.NewReader(`{"number":"+14155552671","category":"nonsense"}`))
	req = req.WithContext(deviceCtx(42))
	rec := httptest.NewRecorder()
	h.handleCreate(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body)
	}
}

func TestReportsEndpoint_InvalidVote(t *testing.T) {
	h := &reportsHandler{}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports", strings.NewReader(`{"number":"+14155552671","vote":"maybe"}`))
	req = req.WithContext(deviceCtx(42))
	rec := httptest.NewRecorder()
	h.handleCreate(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body)
	}
}

func TestReportsEndpoint_NameTooLong(t *testing.T) {
	h := &reportsHandler{}

	longName := strings.Repeat("a", 101)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports", strings.NewReader(`{"number":"+14155552671","name":"`+longName+`"}`))
	req = req.WithContext(deviceCtx(42))
	rec := httptest.NewRecorder()
	h.handleCreate(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body)
	}
}

func TestReportsEndpoint_MalformedJSON(t *testing.T) {
	h := &reportsHandler{}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports", strings.NewReader(`{not-json`))
	req = req.WithContext(deviceCtx(42))
	rec := httptest.NewRecorder()
	h.handleCreate(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body)
	}
}

// deviceCtx returns a context carrying both a request id and an
// authenticated device_id, mimicking RequestIDMiddleware + RequireDevice.
func deviceCtx(deviceID uint64) context.Context {
	ctx := context.WithValue(context.Background(), requestIDContextKey, "test-request-id")
	return context.WithValue(ctx, deviceIDContextKey, deviceID)
}

func decodeEnvelopeErrors(t *testing.T, body []byte) (success bool, errs []APIError) {
	t.Helper()
	var env struct {
		Success bool       `json:"success"`
		Errors  []APIError `json:"errors"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("failed to unmarshal envelope: %v; body=%s", err, body)
	}
	return env.Success, env.Errors
}
