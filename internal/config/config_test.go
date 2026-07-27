package config

import (
	"strings"
	"testing"
	"time"
)

// strongSecret and strongAdminToken stand in for `openssl rand -hex 32`
// output: 64 hex characters, long enough to clear minSecretLength.
const (
	strongSecret     = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	strongAdminToken = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("DB_DSN", "")
	t.Setenv("ADDR", "")
	t.Setenv("ADMIN_TOKEN", "")
	t.Setenv("ATTEST_MODE", "")
	t.Setenv("APP_ID", "")
	t.Setenv("DEVICE_TOKEN_SECRET", "")
	t.Setenv("DEVICE_TOKEN_TTL", "")
	t.Setenv("CHALLENGE_TTL", "")
	t.Setenv("CHALLENGE_STORE", "")
	t.Setenv("REDIS_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	wantDSN := "root@tcp(127.0.0.1:3306)/spamfilter_dev?parseTime=true&multiStatements=true"
	if cfg.DBDsn != wantDSN {
		t.Errorf("DBDsn = %q, want %q", cfg.DBDsn, wantDSN)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("Addr = %q, want %q", cfg.Addr, ":8080")
	}
	if cfg.AdminToken != "" {
		t.Errorf("AdminToken = %q, want empty string", cfg.AdminToken)
	}
	if cfg.AttestMode != "mock" {
		t.Errorf("AttestMode = %q, want mock", cfg.AttestMode)
	}
	if cfg.AppID != "" {
		t.Errorf("AppID = %q, want empty string", cfg.AppID)
	}
	if cfg.DeviceTokenSecret != devDefaultTokenSecret {
		t.Errorf("DeviceTokenSecret = %q, want dev default", cfg.DeviceTokenSecret)
	}
	if !cfg.DeviceTokenSecretIsDefault {
		t.Errorf("DeviceTokenSecretIsDefault = false, want true when secret unset")
	}
	if cfg.DeviceTokenTTL != 720*time.Hour {
		t.Errorf("DeviceTokenTTL = %v, want 720h", cfg.DeviceTokenTTL)
	}
	if cfg.ChallengeTTL != 5*time.Minute {
		t.Errorf("ChallengeTTL = %v, want 5m", cfg.ChallengeTTL)
	}
	if cfg.APNSKeyPath != "" || cfg.APNSKeyID != "" || cfg.APNSTeamID != "" || cfg.APNSTopic != "" {
		t.Errorf("APNs fields should default empty, got path=%q id=%q team=%q topic=%q",
			cfg.APNSKeyPath, cfg.APNSKeyID, cfg.APNSTeamID, cfg.APNSTopic)
	}
	if cfg.ChallengeStore != "memory" {
		t.Errorf("ChallengeStore = %q, want memory", cfg.ChallengeStore)
	}
	if cfg.RedisURL != "" {
		t.Errorf("RedisURL = %q, want empty string", cfg.RedisURL)
	}
}

func TestLoad_ChallengeStoreRedisOverrides(t *testing.T) {
	t.Setenv("CHALLENGE_STORE", "redis")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.ChallengeStore != "redis" {
		t.Errorf("ChallengeStore = %q, want redis", cfg.ChallengeStore)
	}
	if cfg.RedisURL != "redis://localhost:6379/0" {
		t.Errorf("RedisURL = %q, want override", cfg.RedisURL)
	}
}

func TestLoad_ChallengeStoreRedisRequiresRedisURL(t *testing.T) {
	t.Setenv("CHALLENGE_STORE", "redis")
	t.Setenv("REDIS_URL", "")

	if _, err := Load(); err == nil {
		t.Fatalf("Load() with CHALLENGE_STORE=redis and empty REDIS_URL should return an error")
	}
}

func TestLoad_APNSOverrides(t *testing.T) {
	t.Setenv("APNS_KEY_PATH", "/etc/apns/AuthKey.p8")
	t.Setenv("APNS_KEY_ID", "ABC123KEY")
	t.Setenv("APNS_TEAM_ID", "TEAM456789")
	t.Setenv("APNS_TOPIC", "com.example.spamfilter")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.APNSKeyPath != "/etc/apns/AuthKey.p8" {
		t.Errorf("APNSKeyPath = %q, want override", cfg.APNSKeyPath)
	}
	if cfg.APNSKeyID != "ABC123KEY" {
		t.Errorf("APNSKeyID = %q, want ABC123KEY", cfg.APNSKeyID)
	}
	if cfg.APNSTeamID != "TEAM456789" {
		t.Errorf("APNSTeamID = %q, want TEAM456789", cfg.APNSTeamID)
	}
	if cfg.APNSTopic != "com.example.spamfilter" {
		t.Errorf("APNSTopic = %q, want com.example.spamfilter", cfg.APNSTopic)
	}
}

func TestLoad_AttestOverrides(t *testing.T) {
	t.Setenv("ATTEST_MODE", "apple")
	t.Setenv("APP_ID", "ABCDE12345.com.example.spamfilter")
	t.Setenv("DEVICE_TOKEN_SECRET", strongSecret)
	t.Setenv("ADMIN_TOKEN", strongAdminToken)
	t.Setenv("DEVICE_TOKEN_TTL", "48h")
	t.Setenv("CHALLENGE_TTL", "90s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.AttestMode != "apple" {
		t.Errorf("AttestMode = %q, want apple", cfg.AttestMode)
	}
	if cfg.AppID != "ABCDE12345.com.example.spamfilter" {
		t.Errorf("AppID = %q, want override", cfg.AppID)
	}
	if cfg.DeviceTokenSecret != strongSecret {
		t.Errorf("DeviceTokenSecret = %q, want strongSecret", cfg.DeviceTokenSecret)
	}
	if cfg.DeviceTokenSecretIsDefault {
		t.Errorf("DeviceTokenSecretIsDefault = true, want false when secret set")
	}
	if cfg.DeviceTokenTTL != 48*time.Hour {
		t.Errorf("DeviceTokenTTL = %v, want 48h", cfg.DeviceTokenTTL)
	}
	if cfg.ChallengeTTL != 90*time.Second {
		t.Errorf("ChallengeTTL = %v, want 90s", cfg.ChallengeTTL)
	}
}

func TestLoad_AppleModeRequiresAppID(t *testing.T) {
	t.Setenv("ATTEST_MODE", "apple")
	t.Setenv("APP_ID", "")

	if _, err := Load(); err == nil {
		t.Fatalf("Load() with mode=apple and empty APP_ID should return an error")
	}
}

func TestLoad_AppleModeSucceedsWithSecurePosture(t *testing.T) {
	t.Setenv("ATTEST_MODE", "apple")
	t.Setenv("APP_ID", "ABCDE12345.com.example.spamfilter")
	t.Setenv("DEVICE_TOKEN_SECRET", strongSecret)
	t.Setenv("ADMIN_TOKEN", strongAdminToken)

	if _, err := Load(); err != nil {
		t.Fatalf("Load() with secure apple posture returned error: %v", err)
	}
}

func TestLoad_AppleModeRequiresNonDefaultDeviceTokenSecret(t *testing.T) {
	t.Setenv("ATTEST_MODE", "apple")
	t.Setenv("APP_ID", "ABCDE12345.com.example.spamfilter")
	t.Setenv("ADMIN_TOKEN", strongAdminToken)
	t.Setenv("DEVICE_TOKEN_SECRET", "")

	_, err := Load()
	if err == nil {
		t.Fatalf("Load() with mode=apple and unset DEVICE_TOKEN_SECRET should return an error")
	}
	if !strings.Contains(err.Error(), "DEVICE_TOKEN_SECRET") {
		t.Errorf("error = %q, want it to name DEVICE_TOKEN_SECRET", err.Error())
	}

	// Also reject an explicit value equal to the known-insecure dev default.
	t.Setenv("DEVICE_TOKEN_SECRET", devDefaultTokenSecret)
	_, err = Load()
	if err == nil {
		t.Fatalf("Load() with mode=apple and DEVICE_TOKEN_SECRET set to the insecure dev default should return an error")
	}
	if !strings.Contains(err.Error(), "DEVICE_TOKEN_SECRET") {
		t.Errorf("error = %q, want it to name DEVICE_TOKEN_SECRET", err.Error())
	}
}

func TestLoad_AppleModeRequiresAdminToken(t *testing.T) {
	t.Setenv("ATTEST_MODE", "apple")
	t.Setenv("APP_ID", "ABCDE12345.com.example.spamfilter")
	t.Setenv("DEVICE_TOKEN_SECRET", strongSecret)
	t.Setenv("ADMIN_TOKEN", "")

	_, err := Load()
	if err == nil {
		t.Fatalf("Load() with mode=apple and empty ADMIN_TOKEN should return an error")
	}
	if !strings.Contains(err.Error(), "ADMIN_TOKEN") {
		t.Errorf("error = %q, want it to name ADMIN_TOKEN", err.Error())
	}
}

func TestLoad_MockModeAllowsInsecureDefaults(t *testing.T) {
	t.Setenv("ATTEST_MODE", "mock")
	t.Setenv("DEVICE_TOKEN_SECRET", "")
	t.Setenv("ADMIN_TOKEN", "")
	t.Setenv("APP_ID", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() in mock mode with insecure defaults returned error: %v", err)
	}
	if !cfg.DeviceTokenSecretIsDefault {
		t.Errorf("DeviceTokenSecretIsDefault = false, want true in mock mode with unset secret")
	}
	if cfg.AdminToken != "" {
		t.Errorf("AdminToken = %q, want empty", cfg.AdminToken)
	}
}

// A malformed duration must be a startup error, not a silent fallback.
// Previously DEVICE_TOKEN_TTL=30d (not a valid Go duration) quietly became
// 720h, so an operator who thought they had set a 30-day TTL and an operator
// who had set nothing were indistinguishable. Failing loudly is the only way
// the mistake is visible.
func TestLoad_InvalidDurationReturnsError(t *testing.T) {
	for _, bad := range []string{"not-a-duration", "30d", "1y", "720"} {
		t.Run(bad, func(t *testing.T) {
			t.Setenv("DEVICE_TOKEN_TTL", bad)
			t.Setenv("CHALLENGE_TTL", "")

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() with DEVICE_TOKEN_TTL=%q should return an error, not fall back", bad)
			}
			if !strings.Contains(err.Error(), "DEVICE_TOKEN_TTL") {
				t.Errorf("error = %q, want it to name DEVICE_TOKEN_TTL", err.Error())
			}
		})
	}
}

func TestLoad_InvalidChallengeTTLReturnsError(t *testing.T) {
	t.Setenv("DEVICE_TOKEN_TTL", "")
	t.Setenv("CHALLENGE_TTL", "5min")

	_, err := Load()
	if err == nil {
		t.Fatalf("Load() with CHALLENGE_TTL=5min should return an error, not fall back")
	}
	if !strings.Contains(err.Error(), "CHALLENGE_TTL") {
		t.Errorf("error = %q, want it to name CHALLENGE_TTL", err.Error())
	}
}

func TestLoad_ValidDurationStillParses(t *testing.T) {
	t.Setenv("DEVICE_TOKEN_TTL", "48h")
	t.Setenv("CHALLENGE_TTL", "90s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.DeviceTokenTTL != 48*time.Hour {
		t.Errorf("DeviceTokenTTL = %v, want 48h", cfg.DeviceTokenTTL)
	}
	if cfg.ChallengeTTL != 90*time.Second {
		t.Errorf("ChallengeTTL = %v, want 90s", cfg.ChallengeTTL)
	}
}

// An unrecognized ATTEST_MODE previously fell through to the MOCK verifier,
// which accepts any fabricated attestation, while skipping every apple-mode
// production check -- and logged nothing. A single typo turned production into
// an open door, so unknown values must abort startup.
func TestLoad_RejectsUnknownAttestMode(t *testing.T) {
	for _, mode := range []string{"Apple", "APPLE", "appl", "real", "production", "Mock", "live"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("ATTEST_MODE", mode)
			t.Setenv("APP_ID", "ABCDE12345.com.example.hushield")
			t.Setenv("DEVICE_TOKEN_SECRET", strongSecret)
			t.Setenv("ADMIN_TOKEN", strongAdminToken)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() with ATTEST_MODE=%q should return an error, not silently use the mock verifier", mode)
			}
			if !strings.Contains(err.Error(), "ATTEST_MODE") {
				t.Errorf("error = %q, want it to name ATTEST_MODE", err.Error())
			}
		})
	}
}

func TestLoad_AcceptsKnownAttestModes(t *testing.T) {
	t.Setenv("ATTEST_MODE", "mock")
	if _, err := Load(); err != nil {
		t.Fatalf("Load() with ATTEST_MODE=mock returned error: %v", err)
	}

	t.Setenv("ATTEST_MODE", "apple")
	t.Setenv("APP_ID", "ABCDE12345.com.example.hushield")
	t.Setenv("DEVICE_TOKEN_SECRET", strongSecret)
	t.Setenv("ADMIN_TOKEN", strongAdminToken)
	if _, err := Load(); err != nil {
		t.Fatalf("Load() with ATTEST_MODE=apple returned error: %v", err)
	}
}

// The old guard compared against exactly one literal, so the placeholder
// published in .env.example passed validation as a production secret.
func TestLoad_AppleModeRejectsPlaceholderSecrets(t *testing.T) {
	placeholders := []string{
		"change-me-generate-with-openssl-rand-hex-32",
		"change-me",
		"CHANGE-ME-generate-with-openssl-rand-hex-32",
		"changeme",
	}

	for _, p := range placeholders {
		t.Run("device_token_secret/"+p, func(t *testing.T) {
			t.Setenv("ATTEST_MODE", "apple")
			t.Setenv("APP_ID", "ABCDE12345.com.example.hushield")
			t.Setenv("ADMIN_TOKEN", strongAdminToken)
			t.Setenv("DEVICE_TOKEN_SECRET", p)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() should reject placeholder DEVICE_TOKEN_SECRET %q", p)
			}
			if !strings.Contains(err.Error(), "DEVICE_TOKEN_SECRET") {
				t.Errorf("error = %q, want it to name DEVICE_TOKEN_SECRET", err.Error())
			}
		})

		t.Run("admin_token/"+p, func(t *testing.T) {
			t.Setenv("ATTEST_MODE", "apple")
			t.Setenv("APP_ID", "ABCDE12345.com.example.hushield")
			t.Setenv("DEVICE_TOKEN_SECRET", strongSecret)
			t.Setenv("ADMIN_TOKEN", p)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() should reject placeholder ADMIN_TOKEN %q", p)
			}
			if !strings.Contains(err.Error(), "ADMIN_TOKEN") {
				t.Errorf("error = %q, want it to name ADMIN_TOKEN", err.Error())
			}
		})
	}
}

func TestLoad_AppleModeRejectsShortSecrets(t *testing.T) {
	short := strings.Repeat("a", minSecretLength-1)

	t.Run("device_token_secret", func(t *testing.T) {
		t.Setenv("ATTEST_MODE", "apple")
		t.Setenv("APP_ID", "ABCDE12345.com.example.hushield")
		t.Setenv("ADMIN_TOKEN", strongAdminToken)
		t.Setenv("DEVICE_TOKEN_SECRET", short)

		_, err := Load()
		if err == nil {
			t.Fatalf("Load() should reject a DEVICE_TOKEN_SECRET shorter than %d chars", minSecretLength)
		}
		if !strings.Contains(err.Error(), "DEVICE_TOKEN_SECRET") {
			t.Errorf("error = %q, want it to name DEVICE_TOKEN_SECRET", err.Error())
		}
	})

	t.Run("admin_token", func(t *testing.T) {
		t.Setenv("ATTEST_MODE", "apple")
		t.Setenv("APP_ID", "ABCDE12345.com.example.hushield")
		t.Setenv("DEVICE_TOKEN_SECRET", strongSecret)
		t.Setenv("ADMIN_TOKEN", short)

		_, err := Load()
		if err == nil {
			t.Fatalf("Load() should reject an ADMIN_TOKEN shorter than %d chars", minSecretLength)
		}
		if !strings.Contains(err.Error(), "ADMIN_TOKEN") {
			t.Errorf("error = %q, want it to name ADMIN_TOKEN", err.Error())
		}
	})

	t.Run("exactly_minimum_is_accepted", func(t *testing.T) {
		t.Setenv("ATTEST_MODE", "apple")
		t.Setenv("APP_ID", "ABCDE12345.com.example.hushield")
		t.Setenv("DEVICE_TOKEN_SECRET", strings.Repeat("b", minSecretLength))
		t.Setenv("ADMIN_TOKEN", strings.Repeat("c", minSecretLength))

		if _, err := Load(); err != nil {
			t.Fatalf("Load() should accept secrets of exactly %d chars: %v", minSecretLength, err)
		}
	})
}

// APNs settings are all-or-nothing. A partial group means push silently does
// nothing at runtime, which looks identical to push being deliberately off.
func TestLoad_APNSRequiresCompleteGroup(t *testing.T) {
	cases := []struct {
		name                  string
		path, id, team, topic string
	}{
		{"path_only", "/etc/apns/k.p8", "", "", ""},
		{"missing_key_id", "/etc/apns/k.p8", "", "TEAM456789", "com.example.hushield"},
		{"missing_team", "/etc/apns/k.p8", "ABC123KEY", "", "com.example.hushield"},
		{"missing_topic", "/etc/apns/k.p8", "ABC123KEY", "TEAM456789", ""},
		{"id_without_path", "", "ABC123KEY", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APNS_KEY_PATH", tc.path)
			t.Setenv("APNS_KEY_ID", tc.id)
			t.Setenv("APNS_TEAM_ID", tc.team)
			t.Setenv("APNS_TOPIC", tc.topic)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() should reject a partial APNs group (%+v)", tc)
			}
			if !strings.Contains(err.Error(), "APNS_") {
				t.Errorf("error = %q, want it to name an APNS_ variable", err.Error())
			}
		})
	}
}

func TestLoad_APNSFullyUnsetIsAllowed(t *testing.T) {
	t.Setenv("APNS_KEY_PATH", "")
	t.Setenv("APNS_KEY_ID", "")
	t.Setenv("APNS_TEAM_ID", "")
	t.Setenv("APNS_TOPIC", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() with APNs fully unset should succeed (push disabled): %v", err)
	}
	if cfg.APNSKeyPath != "" {
		t.Errorf("APNSKeyPath = %q, want empty", cfg.APNSKeyPath)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("DB_DSN", "user:pass@tcp(db:3306)/custom?parseTime=true")
	t.Setenv("ADDR", ":9090")
	t.Setenv("ADMIN_TOKEN", "super-secret-token")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.DBDsn != "user:pass@tcp(db:3306)/custom?parseTime=true" {
		t.Errorf("DBDsn = %q, want override value", cfg.DBDsn)
	}
	if cfg.Addr != ":9090" {
		t.Errorf("Addr = %q, want :9090", cfg.Addr)
	}
	if cfg.AdminToken != "super-secret-token" {
		t.Errorf("AdminToken = %q, want super-secret-token", cfg.AdminToken)
	}
}
