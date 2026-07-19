package api

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"spamfilter/internal/attest"
	"spamfilter/internal/token"
)

// attestHandler serves the App Attest challenge/verify endpoints. It converts
// a genuine-device attestation into a stateless device token.
type attestHandler struct {
	db           *sql.DB
	store        attest.ChallengeStore
	verifier     attest.Verifier
	signer       *token.Signer
	challengeTTL time.Duration
	tokenTTL     time.Duration
	now          func() time.Time
}

func (h *attestHandler) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

type challengeResponse struct {
	Challenge string `json:"challenge"`
	ExpiresAt string `json:"expires_at"`
}

// handleChallenge issues a fresh single-use attestation challenge.
func (h *attestHandler) handleChallenge(w http.ResponseWriter, r *http.Request) {
	requestID := RequestIDFromContext(r.Context())
	now := h.clock()

	ch, err := h.store.Issue(now, h.challengeTTL)
	if err != nil {
		logInternalError(requestID, "issue challenge", err)
		WriteError(w, http.StatusInternalServerError, requestID,
			APIError{Message: "failed to issue challenge", Code: "internal_error"})
		return
	}

	WriteSuccess(w, http.StatusOK, challengeResponse{
		Challenge: base64.StdEncoding.EncodeToString(ch),
		ExpiresAt: now.Add(h.challengeTTL).UTC().Format(time.RFC3339),
	}, requestID)
}

type verifyRequest struct {
	KeyID       string `json:"key_id"`
	Attestation string `json:"attestation"`
	Challenge   string `json:"challenge"`
}

type verifyResponse struct {
	DeviceToken string `json:"device_token"`
	ExpiresAt   string `json:"expires_at"`
}

// handleVerify consumes the challenge, verifies the attestation, upserts the
// device, and returns a stateless device token.
func (h *attestHandler) handleVerify(w http.ResponseWriter, r *http.Request) {
	requestID := RequestIDFromContext(r.Context())
	now := h.clock()

	var body verifyRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	if err := dec.Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, requestID,
			APIError{Message: "invalid request body", Code: "bad_request"})
		return
	}

	if body.KeyID == "" {
		WriteError(w, http.StatusBadRequest, requestID,
			APIError{Field: "key_id", Message: "key_id is required", Code: "bad_request"})
		return
	}
	// The "seed:" prefix is reserved for synthetic seed devices (see
	// store.EnsureSeedDevice), which are excluded from trust recompute and
	// carry a fixed high trust_weight. Rejecting it here stops a client --
	// especially under ATTEST_MODE=mock -- from claiming a seed identity and
	// riding that fixed weight.
	if strings.HasPrefix(body.KeyID, "seed:") {
		WriteError(w, http.StatusBadRequest, requestID,
			APIError{Field: "key_id", Message: "key_id is reserved", Code: "bad_request"})
		return
	}
	attBytes, err := base64.StdEncoding.DecodeString(body.Attestation)
	if err != nil || len(attBytes) == 0 {
		WriteError(w, http.StatusBadRequest, requestID,
			APIError{Field: "attestation", Message: "attestation must be valid base64", Code: "bad_request"})
		return
	}
	chBytes, err := base64.StdEncoding.DecodeString(body.Challenge)
	if err != nil || len(chBytes) == 0 {
		WriteError(w, http.StatusBadRequest, requestID,
			APIError{Field: "challenge", Message: "challenge must be valid base64", Code: "bad_request"})
		return
	}

	// Consume the challenge first (single-use, replay-safe).
	if err := h.store.Consume(chBytes, now); err != nil {
		WriteError(w, http.StatusUnauthorized, requestID,
			APIError{Message: "invalid or expired challenge", Code: "unauthorized"})
		return
	}

	// Verify the attestation (fails closed).
	pubDER, receipt, err := h.verifier.VerifyAttestation(r.Context(), body.KeyID, attBytes, chBytes)
	if err != nil {
		WriteError(w, http.StatusUnauthorized, requestID,
			APIError{Message: "attestation verification failed", Code: "unauthorized"})
		return
	}

	deviceID, err := upsertDevice(r.Context(), h.db, body.KeyID, pubDER, receipt, now)
	if err != nil {
		logInternalError(requestID, "persist device", err)
		WriteError(w, http.StatusInternalServerError, requestID,
			APIError{Message: "failed to persist device", Code: "internal_error"})
		return
	}

	tok := h.signer.Issue(deviceID, h.tokenTTL, now)
	WriteSuccess(w, http.StatusOK, verifyResponse{
		DeviceToken: tok,
		ExpiresAt:   now.Add(h.tokenTTL).UTC().Format(time.RFC3339),
	}, requestID)
}

// upsertDevice inserts or updates the device row keyed by key_id and returns
// its device_id. New rows take the schema default trust_weight.
func upsertDevice(ctx context.Context, db *sql.DB, keyID string, publicKey, receipt []byte, now time.Time) (uint64, error) {
	if db == nil {
		return 0, errors.New("api: nil database handle")
	}

	const upsert = `INSERT INTO devices (key_id, public_key, receipt, last_seen_at)
VALUES (?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
    public_key = VALUES(public_key),
    receipt = VALUES(receipt),
    last_seen_at = VALUES(last_seen_at)`
	if _, err := db.ExecContext(ctx, upsert, keyID, publicKey, receipt, now.UTC()); err != nil {
		return 0, err
	}

	var deviceID uint64
	if err := db.QueryRowContext(ctx, "SELECT device_id FROM devices WHERE key_id = ?", keyID).Scan(&deviceID); err != nil {
		return 0, err
	}
	return deviceID, nil
}
