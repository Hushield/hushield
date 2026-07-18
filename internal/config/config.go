// Package config loads runtime configuration from environment variables.
package config

import "os"

// Config holds runtime configuration for the server.
type Config struct {
	DBDsn      string
	Addr       string
	AdminToken string
}

const (
	defaultDBDsn = "root@tcp(127.0.0.1:3306)/spamfilter_dev?parseTime=true&multiStatements=true"
	defaultAddr  = ":8080"
)

// Load reads configuration from environment variables, falling back to
// defaults when a variable is unset or empty.
func Load() (Config, error) {
	return Config{
		DBDsn:      getEnv("DB_DSN", defaultDBDsn),
		Addr:       getEnv("ADDR", defaultAddr),
		AdminToken: getEnv("ADMIN_TOKEN", ""),
	}, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
