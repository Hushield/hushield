package config

import (
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("DB_DSN", "")
	t.Setenv("ADDR", "")
	t.Setenv("ADMIN_TOKEN", "")

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
