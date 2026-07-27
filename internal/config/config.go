// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config holds runtime configuration for the server.
type Config struct {
	DBDsn      string
	Addr       string
	AdminToken string

	// AttestMode selects the App Attest verifier: "mock" (default, dev/test)
	// or "apple" (real Apple App Attest verification).
	AttestMode string
	// AppID is the App Attest app identifier "<TeamID>.<BundleID>". Required
	// when AttestMode == "apple".
	AppID string
	// DeviceTokenSecret is the HMAC secret used to sign stateless device
	// tokens. When unset a documented insecure dev default is used and
	// DeviceTokenSecretIsDefault is set so startup can warn.
	DeviceTokenSecret string
	// DeviceTokenSecretIsDefault reports whether the insecure dev default
	// secret is in use (i.e. DEVICE_TOKEN_SECRET was unset).
	DeviceTokenSecretIsDefault bool
	// DeviceTokenTTL is how long an issued device token remains valid.
	DeviceTokenTTL time.Duration
	// ChallengeTTL is how long an issued attestation challenge remains valid.
	ChallengeTTL time.Duration
	// ChallengeStore selects the attestation ChallengeStore backend: "memory"
	// (default, process-local) or "redis" (shared, required for a
	// multi-instance deployment).
	ChallengeStore string
	// RedisURL is the Redis connection URL (e.g. redis://localhost:6379/0),
	// used when ChallengeStore == "redis".
	RedisURL string

	// APNSKeyPath is the filesystem path to the Apple token-based auth key
	// (.p8, PKCS8 EC private key). When empty, silent-push is disabled and a
	// NoopNotifier is used (fail-safe).
	APNSKeyPath string
	// APNSKeyID is the Key ID of the APNs auth key (APNS_KEY_ID).
	APNSKeyID string
	// APNSTeamID is the Apple Team ID used as the provider JWT issuer
	// (APNS_TEAM_ID).
	APNSTeamID string
	// APNSTopic is the app bundle id sent as the apns-topic header
	// (APNS_TOPIC).
	APNSTopic string
}

const (
	defaultDBDsn = "root@tcp(127.0.0.1:3306)/spamfilter_dev?parseTime=true&multiStatements=true"
	defaultAddr  = ":8080"

	defaultAttestMode = "mock"

	// devDefaultTokenSecret is an intentionally insecure fallback used only
	// for local development when DEVICE_TOKEN_SECRET is unset. Production
	// deployments MUST set DEVICE_TOKEN_SECRET.
	devDefaultTokenSecret = "dev-insecure-device-token-secret-change-me"

	defaultDeviceTokenTTL = 720 * time.Hour // 30 days
	defaultChallengeTTL   = 5 * time.Minute

	defaultChallengeStore = "memory"

	// minSecretLength is the shortest accepted DEVICE_TOKEN_SECRET or
	// ADMIN_TOKEN when ATTEST_MODE=apple. `openssl rand -hex 32` produces 64
	// characters, so this leaves generous headroom while still rejecting
	// hand-typed values short enough to be guessable.
	minSecretLength = 32
)

// validAttestModes enumerates every accepted ATTEST_MODE. Anything else is a
// startup error: an unrecognized value used to fall through to the mock
// verifier -- which accepts any fabricated attestation -- while skipping all
// apple-mode production checks, so a typo like "Apple" silently disabled
// authentication in production.
var validAttestModes = map[string]bool{
	"mock":  true,
	"apple": true,
}

// placeholderMarkers are substrings that betray an unedited example value. The
// previous guard compared against a single literal, so the placeholder shipped
// in .env.example ("change-me-generate-with-openssl-rand-hex-32") passed
// validation and would have been accepted as a production secret.
var placeholderMarkers = []string{"change-me", "changeme", "insecure", "placeholder"}

// looksLikePlaceholder reports whether s appears to be an unedited example
// value rather than a real generated secret.
func looksLikePlaceholder(s string) bool {
	lower := strings.ToLower(s)
	for _, marker := range placeholderMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// validateSecret enforces the production rules for a single secret-bearing
// variable. name is the environment variable name, used in the error so an
// operator reading journalctl knows exactly which value to fix.
func validateSecret(name, value string) error {
	if value == "" {
		return fmt.Errorf("config: %s is required when ATTEST_MODE=apple; generate one with `openssl rand -hex 32`", name)
	}
	if looksLikePlaceholder(value) {
		return fmt.Errorf("config: %s still contains an example placeholder value; generate a real secret with `openssl rand -hex 32`", name)
	}
	if len(value) < minSecretLength {
		return fmt.Errorf("config: %s must be at least %d characters when ATTEST_MODE=apple (got %d); generate one with `openssl rand -hex 32`", name, minSecretLength, len(value))
	}
	return nil
}

// Load reads configuration from environment variables, falling back to
// defaults when a variable is unset or empty.
func Load() (Config, error) {
	secret := os.Getenv("DEVICE_TOKEN_SECRET")
	secretIsDefault := secret == ""
	if secretIsDefault {
		secret = devDefaultTokenSecret
	}

	deviceTokenTTL, err := getEnvDuration("DEVICE_TOKEN_TTL", defaultDeviceTokenTTL)
	if err != nil {
		return Config{}, err
	}
	challengeTTL, err := getEnvDuration("CHALLENGE_TTL", defaultChallengeTTL)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		DBDsn:                      getEnv("DB_DSN", defaultDBDsn),
		Addr:                       getEnv("ADDR", defaultAddr),
		AdminToken:                 getEnv("ADMIN_TOKEN", ""),
		AttestMode:                 getEnv("ATTEST_MODE", defaultAttestMode),
		AppID:                      getEnv("APP_ID", ""),
		DeviceTokenSecret:          secret,
		DeviceTokenSecretIsDefault: secretIsDefault,
		DeviceTokenTTL:             deviceTokenTTL,
		ChallengeTTL:               challengeTTL,
		ChallengeStore:             getEnv("CHALLENGE_STORE", defaultChallengeStore),
		RedisURL:                   getEnv("REDIS_URL", ""),
		APNSKeyPath:                getEnv("APNS_KEY_PATH", ""),
		APNSKeyID:                  getEnv("APNS_KEY_ID", ""),
		APNSTeamID:                 getEnv("APNS_TEAM_ID", ""),
		APNSTopic:                  getEnv("APNS_TOPIC", ""),
	}

	// Reject unknown modes before any mode-specific branch, so a typo can never
	// be treated as "not apple" and thus skip the production checks below.
	if !validAttestModes[cfg.AttestMode] {
		return Config{}, fmt.Errorf("config: ATTEST_MODE=%q is not a recognized mode (want \"mock\" or \"apple\"); refusing to start rather than defaulting to the mock verifier, which accepts any attestation", cfg.AttestMode)
	}

	if cfg.AttestMode == "apple" {
		if cfg.AppID == "" {
			return Config{}, fmt.Errorf("config: APP_ID is required when ATTEST_MODE=apple")
		}
		if err := validateSecret("DEVICE_TOKEN_SECRET", cfg.DeviceTokenSecret); err != nil {
			return Config{}, err
		}
		if err := validateSecret("ADMIN_TOKEN", cfg.AdminToken); err != nil {
			return Config{}, err
		}
	}

	if cfg.ChallengeStore == "redis" && cfg.RedisURL == "" {
		return Config{}, fmt.Errorf("config: REDIS_URL is required when CHALLENGE_STORE=redis")
	}

	if err := validateAPNSGroup(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// validateAPNSGroup enforces that the four APNs variables are either all set or
// all unset. A partial group previously passed validation and then failed at
// send time, making "misconfigured push" indistinguishable from "push
// deliberately disabled".
func validateAPNSGroup(cfg Config) error {
	fields := []struct {
		name  string
		value string
	}{
		{"APNS_KEY_PATH", cfg.APNSKeyPath},
		{"APNS_KEY_ID", cfg.APNSKeyID},
		{"APNS_TEAM_ID", cfg.APNSTeamID},
		{"APNS_TOPIC", cfg.APNSTopic},
	}

	var set, missing []string
	for _, f := range fields {
		if f.value == "" {
			missing = append(missing, f.name)
		} else {
			set = append(set, f.name)
		}
	}

	// All unset means push is intentionally disabled; all set means push is
	// configured. Anything between is a mistake.
	if len(set) == 0 || len(missing) == 0 {
		return nil
	}

	return fmt.Errorf("config: APNs settings are all-or-nothing; %s set but %s missing (set all four to enable silent push, or none to disable it)",
		strings.Join(set, ", "), strings.Join(missing, ", "))
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getEnvDuration parses a Go duration from the environment, returning fallback
// only when the variable is unset. A malformed value is an error rather than a
// silent fallback: DEVICE_TOKEN_TTL=30d is not a valid Go duration, and quietly
// substituting 720h made a misconfigured deployment indistinguishable from a
// correctly defaulted one.
func getEnvDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s=%q is not a valid duration (want a Go duration such as \"720h\", \"48h\", or \"90s\"): %w", key, v, err)
	}
	return d, nil
}
