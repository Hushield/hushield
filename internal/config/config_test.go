package config

import (
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
}

func TestLoad_AttestOverrides(t *testing.T) {
	t.Setenv("ATTEST_MODE", "apple")
	t.Setenv("APP_ID", "ABCDE12345.com.example.spamfilter")
	t.Setenv("DEVICE_TOKEN_SECRET", "a-real-secret")
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
