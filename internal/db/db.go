// Package db provides the MySQL connection pool and embedded migration runner.
package db

import (
	"database/sql"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// Open opens a MySQL connection pool for the given DSN, applies sane pool
// settings, and verifies connectivity with Ping.
func Open(dsn string) (*sql.DB, error) {
	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}

	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(25)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, err
	}

	return sqlDB, nil
}
