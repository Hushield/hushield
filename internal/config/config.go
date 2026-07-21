// Package config loads runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
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
)

// Load reads configuration from environment variables, falling back to
// defaults when a variable is unset or empty.
func Load() (Config, error) {
	secret := os.Getenv("DEVICE_TOKEN_SECRET")
	secretIsDefault := secret == ""
	if secretIsDefault {
		secret = devDefaultTokenSecret
	}

	cfg := Config{
		DBDsn:                      getEnv("DB_DSN", defaultDBDsn),
		Addr:                       getEnv("ADDR", defaultAddr),
		AdminToken:                 getEnv("ADMIN_TOKEN", ""),
		AttestMode:                 getEnv("ATTEST_MODE", defaultAttestMode),
		AppID:                      getEnv("APP_ID", ""),
		DeviceTokenSecret:          secret,
		DeviceTokenSecretIsDefault: secretIsDefault,
		DeviceTokenTTL:             getEnvDuration("DEVICE_TOKEN_TTL", defaultDeviceTokenTTL),
		ChallengeTTL:               getEnvDuration("CHALLENGE_TTL", defaultChallengeTTL),
		APNSKeyPath:                getEnv("APNS_KEY_PATH", ""),
		APNSKeyID:                  getEnv("APNS_KEY_ID", ""),
		APNSTeamID:                 getEnv("APNS_TEAM_ID", ""),
		APNSTopic:                  getEnv("APNS_TOPIC", ""),
	}

	if cfg.AttestMode == "apple" && cfg.AppID == "" {
		return Config{}, fmt.Errorf("config: APP_ID is required when ATTEST_MODE=apple")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
