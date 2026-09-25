package api

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"spamfilter/internal/attest"
	"spamfilter/internal/store"
	"spamfilter/internal/token"
)

// attestHandler serves the App Attest challenge/verify endpoints. It converts
// a genuine-device attestation into a stateless device token.
type attestHandler struct {
	db           *sql.DB
	store        attest.ChallengeStore
	verifiers    map[string]attest.Verifier
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
	// Platform selects which Verifier checks Attestation: "apple" or
	// "android". Empty defaults to "apple" -- every iOS client shipped
	// before this field existed never sends it, and must keep working
	// identically.
	Platform string `json:"platform"`
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
	platform := body.Platform
	if platform == "" {
		platform = "apple"
	}
	verifier, ok := h.verifiers[platform]
	if !ok {
		WriteError(w, http.StatusBadRequest, requestID,
			APIError{Field: "platform", Message: fmt.Sprintf("unknown platform %q", platform), Code: "bad_request"})
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
	pubDER, receipt, err := verifier.VerifyAttestation(r.Context(), body.KeyID, attBytes, chBytes)
	if err != nil {
		WriteError(w, http.StatusUnauthorized, requestID,
			APIError{Message: "attestation verification failed", Code: "unauthorized"})
		return
	}

	deviceID, err := store.UpsertDevicePlatform(r.Context(), h.db, body.KeyID, pubDER, receipt, platform, now)
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

type assertRequest struct {
	KeyID     string `json:"key_id"`
	Assertion string `json:"assertion"`
	Challenge string `json:"challenge"`
}

// handleAssert refreshes a device token from an App Attest assertion signed by
// a previously attested key. It consumes the challenge, verifies the assertion
// (fail-closed, strictly-increasing counter), advances the persisted counter,
// and issues a fresh device token.
func (h *attestHandler) handleAssert(w http.ResponseWriter, r *http.Request) {
	requestID := RequestIDFromContext(r.Context())
	now := h.clock()

	var body assertRequest
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
	// Reserved "seed:" namespace, same rule as /verify: reject before any work
	// so a client cannot claim a synthetic seed device's fixed trust_weight.
	if strings.HasPrefix(body.KeyID, "seed:") {
		WriteError(w, http.StatusBadRequest, requestID,
			APIError{Field: "key_id", Message: "key_id is reserved", Code: "bad_request"})
		return
	}
	asrtBytes, err := base64.StdEncoding.DecodeString(body.Assertion)
	if err != nil || len(asrtBytes) == 0 {
		WriteError(w, http.StatusBadRequest, requestID,
			APIError{Field: "assertion", Message: "assertion must be valid base64", Code: "bad_request"})
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

	// The device must have attested a key before it can assert with it.
	deviceID, pubDER, signCount, platform, err := store.GetDeviceByKeyID(r.Context(), h.db, body.KeyID)
	if err != nil {
		if errors.Is(err, store.ErrDeviceNotFound) {
			WriteError(w, http.StatusUnauthorized, requestID,
				APIError{Message: "unknown device", Code: "unauthorized"})
			return
		}
		logInternalError(requestID, "lookup device", err)
		WriteError(w, http.StatusInternalServerError, requestID,
			APIError{Message: "failed to look up device", Code: "internal_error"})
		return
	}

	verifier, ok := h.verifiers[platform]
	if !ok {
		// A device's stored platform not matching any configured verifier
		// means server config changed after this device enrolled (e.g. the
		// "android" verifier was removed from ATTEST_MODE). Fail closed
		// rather than silently picking a verifier the device never attested
		// against.
		logInternalError(requestID, "assert", fmt.Errorf("device %d has unconfigured platform %q", deviceID, platform))
		WriteError(w, http.StatusInternalServerError, requestID,
			APIError{Message: "device platform not available", Code: "internal_error"})
		return
	}

	// Verify the assertion (fails closed, enforces strictly-increasing counter).
	clientDataHash := sha256.Sum256(chBytes)
	newCounter, err := verifier.VerifyAssertion(r.Context(), pubDER, asrtBytes, clientDataHash[:], signCount)
	if err != nil {
		WriteError(w, http.StatusUnauthorized, requestID,
			APIError{Message: "assertion verification failed", Code: "unauthorized"})
		return
	}

	if err := store.UpdateDeviceSignCount(r.Context(), h.db, deviceID, newCounter, now); err != nil {
		logInternalError(requestID, "update sign count", err)
		WriteError(w, http.StatusInternalServerError, requestID,
			APIError{Message: "failed to update device", Code: "internal_error"})
		return
	}

	tok := h.signer.Issue(deviceID, h.tokenTTL, now)
	WriteSuccess(w, http.StatusOK, verifyResponse{
		DeviceToken: tok,
		ExpiresAt:   now.Add(h.tokenTTL).UTC().Format(time.RFC3339),
	}, requestID)
}
