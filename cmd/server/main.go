// Command server runs the spamfilter API server: it loads config, connects
// to MySQL, applies pending migrations, and serves /api/v1.
package main

import (
	"log"
	"net/http"

	"spamfilter/internal/api"
	"spamfilter/internal/config"
	"spamfilter/internal/db"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	sqlDB, err := db.Open(cfg.DBDsn)
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}
	defer sqlDB.Close()

	if err := db.Migrate(sqlDB); err != nil {
		log.Fatalf("running migrations: %v", err)
	}

	router := api.NewRouter(sqlDB, cfg)

	log.Printf("spamfilter server listening on %s", cfg.Addr)
	if err := http.ListenAndServe(cfg.Addr, router); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
