// Package seed imports public robocall/spam-complaint datasets (e.g. FTC or
// FCC) as reports from a synthetic per-source "seed device", so imported
// numbers flow through the exact same store.RecomputeNumber/scoring
// pipeline as community reports: they get a decaying score, age off on
// their own, and can still be reinforced or contradicted by real community
// votes. No special-casing lives in the scoring package.
package seed

import (
	"context"
	"database/sql"
	"time"

	"spamfilter/internal/phone"
	"spamfilter/internal/scoring"
	"spamfilter/internal/store"
)

// Record is one row yielded by a Source: an unnormalized raw phone number
// and its spam category. Category "" tells Seeder to use the job's default
// category instead.
type Record struct {
	RawNumber string
	Category  scoring.Category
}

// Source yields the records to import. It is an interface so tests (and
// alternate data formats) can supply records without going through a real
// file.
type Source interface {
	Records(ctx context.Context) ([]Record, error)
}

// Seeder imports records from a Source as spam reports from a per-source
// seed device.
type Seeder struct {
	DB *sql.DB
}

// Seed ensures a seed device exists for sourceName with trust_weight
// seedTrust, then imports every record src yields as a spam report from
// that device: each record's RawNumber is normalized (invalid numbers are
// skipped, not stored) and upserted with source=sourceName, and a report is
// filed with the record's Category (or defaultCategory, when the record
// left it blank). After the import it runs store.RecomputeAllNumbers once so
// seeded numbers get their cached status immediately, rather than waiting
// for the next periodic recompute.
//
// Seed is idempotent: re-running it upserts the existing device, numbers,
// and reports rather than creating duplicates (imported/skipped still count
// every record processed, including ones that already existed).
func (s Seeder) Seed(ctx context.Context, src Source, sourceName string, seedTrust float64, defaultCategory scoring.Category) (imported, skipped int, err error) {
	now := time.Now()

	deviceID, err := store.EnsureSeedDevice(ctx, s.DB, sourceName, seedTrust, now)
	if err != nil {
		return 0, 0, err
	}

	records, err := src.Records(ctx)
	if err != nil {
		return 0, 0, err
	}

	for _, record := range records {
		number, err := phone.Normalize(record.RawNumber)
		if err != nil {
			skipped++
			continue
		}

		category := record.Category
		if category == "" {
			category = defaultCategory
		}

		phoneNumberID, err := store.UpsertSeedNumber(ctx, s.DB, number, sourceName, now)
		if err != nil {
			return imported, skipped, err
		}

		if _, err := store.UpsertReport(ctx, s.DB, deviceID, phoneNumberID, category, scoring.VoteSpam, now); err != nil {
			return imported, skipped, err
		}

		imported++
	}

	if _, err := store.RecomputeAllNumbers(ctx, s.DB, now); err != nil {
		return imported, skipped, err
	}

	return imported, skipped, nil
}
