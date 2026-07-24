package config

import (
	"strings"
	"testing"
	"time"
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
	t.Setenv("DEVICE_TOKEN_SECRET", "a-real-secret")
	t.Setenv("ADMIN_TOKEN", "super-secret-admin-token")
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
	if cfg.DeviceTokenSecret != "a-real-secret" {
		t.Errorf("DeviceTokenSecret = %q, want a-real-secret", cfg.DeviceTokenSecret)
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
	t.Setenv("DEVICE_TOKEN_SECRET", "a-real-strong-secret-value")
	t.Setenv("ADMIN_TOKEN", "a-real-admin-token")

	if _, err := Load(); err != nil {
		t.Fatalf("Load() with secure apple posture returned error: %v", err)
	}
}

func TestLoad_AppleModeRequiresNonDefaultDeviceTokenSecret(t *testing.T) {
	t.Setenv("ATTEST_MODE", "apple")
	t.Setenv("APP_ID", "ABCDE12345.com.example.spamfilter")
	t.Setenv("ADMIN_TOKEN", "a-real-admin-token")
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
	t.Setenv("DEVICE_TOKEN_SECRET", "a-real-strong-secret-value")
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

func TestLoad_InvalidDurationFallsBack(t *testing.T) {
	t.Setenv("DEVICE_TOKEN_TTL", "not-a-duration")
	t.Setenv("CHALLENGE_TTL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.DeviceTokenTTL != 720*time.Hour {
		t.Errorf("DeviceTokenTTL = %v, want fallback 720h on invalid input", cfg.DeviceTokenTTL)
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
