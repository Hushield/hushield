package db

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const createSchemaMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INT NOT NULL,
    applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`

type migration struct {
	version int
	name    string
	sql     string
}

// Migrate applies every embedded *.up.sql migration whose version is not
// yet recorded in schema_migrations, in ascending numeric order. It is
// idempotent: re-running it applies nothing new.
func Migrate(sqlDB *sql.DB) error {
	if _, err := sqlDB.Exec(createSchemaMigrationsTable); err != nil {
		return fmt.Errorf("creating schema_migrations table: %w", err)
	}

	applied, err := appliedVersions(sqlDB)
	if err != nil {
		return fmt.Errorf("reading applied migrations: %w", err)
	}

	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("loading migrations: %w", err)
	}

	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		if _, err := sqlDB.Exec(m.sql); err != nil {
			return fmt.Errorf("applying migration %s: %w", m.name, err)
		}
		if _, err := sqlDB.Exec("INSERT INTO schema_migrations (version) VALUES (?)", m.version); err != nil {
			return fmt.Errorf("recording migration %s: %w", m.name, err)
		}
	}

	return nil
}

func appliedVersions(sqlDB *sql.DB) (map[int]bool, error) {
	rows, err := sqlDB.Query("SELECT version FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := map[int]bool{}
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		applied[version] = true
	}
	return applied, rows.Err()
}

func loadMigrations() ([]migration, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}

	var migrations []migration
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		versionStr := strings.SplitN(name, "_", 2)[0]
		version, err := strconv.Atoi(versionStr)
		if err != nil {
			return nil, fmt.Errorf("migration file %s has non-numeric version prefix: %w", name, err)
		}
		contents, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, migration{version: version, name: name, sql: string(contents)})
	}

	if err := checkDuplicateVersions(migrations); err != nil {
		return nil, err
	}

	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })

	return migrations, nil
}

// checkDuplicateVersions returns an error if two migrations in migrations
// share the same version number. Two migration files claiming the same
// version is a numbering collision (e.g. two branches each adding their own
// "0007_*.sql"): silently keeping both would mean only one of them actually
// ever gets recorded as applied, and the other's schema change never
// happens. Failing loudly here is far cheaper than debugging that in
// production.
func checkDuplicateVersions(migrations []migration) error {
	seen := make(map[int]string, len(migrations))
	for _, m := range migrations {
		if existing, ok := seen[m.version]; ok {
			return fmt.Errorf("db: duplicate migration version %d: %s and %s", m.version, existing, m.name)
		}
		seen[m.version] = m.name
	}
	return nil
}
