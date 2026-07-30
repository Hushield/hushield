package store

import (
	"context"
	"testing"
	"time"

	"spamfilter/internal/dbtest"
	"spamfilter/internal/scoring"
)

// TestStoreFunctions_DBErrorsPropagate exercises the "underlying query
// fails" branch of every simple store function, using a DB handle that has
// already been closed so every ExecContext/QueryContext/QueryRowContext call
// fails deterministically. This is the realistic, non-contrived way to
// exercise these functions' error-return branches (a real *sql.DB, just one
// that can no longer serve queries) without a fault-injection framework.
func TestStoreFunctions_DBErrorsPropagate(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	ctx := context.Background()
	now := time.Now()

	t.Run("UpsertPhoneNumber", func(t *testing.T) {
		if _, err := UpsertPhoneNumber(ctx, sqlDB, "+14155550001", now); err == nil {
			t.Error("want error on closed db, got nil")
		}
	})

	t.Run("UpsertReport", func(t *testing.T) {
		if _, err := UpsertReport(ctx, sqlDB, 1, 1, scoring.CategoryScam, scoring.VoteSpam, now); err == nil {
			t.Error("want error on closed db, got nil")
		}
	})

	t.Run("UpsertCallerName", func(t *testing.T) {
		if err := UpsertCallerName(ctx, sqlDB, 1, 1, "name", now); err == nil {
			t.Error("want error on closed db, got nil")
		}
	})

	t.Run("TouchDevice", func(t *testing.T) {
		if err := TouchDevice(ctx, sqlDB, 1, false, now); err == nil {
			t.Error("want error on closed db, got nil")
		}
	})

	t.Run("RecomputeNumber", func(t *testing.T) {
		if _, err := RecomputeNumber(ctx, sqlDB, 1, now); err == nil {
			t.Error("want error on closed db, got nil")
		}
	})

	t.Run("EnsureSeedDevice", func(t *testing.T) {
		if _, err := EnsureSeedDevice(ctx, sqlDB, "ftc", 1.0, now); err == nil {
			t.Error("want error on closed db, got nil")
		}
	})

	t.Run("UpsertSeedNumber", func(t *testing.T) {
		if _, err := UpsertSeedNumber(ctx, sqlDB, "+14155550002", "ftc", now); err == nil {
			t.Error("want error on closed db, got nil")
		}
	})

	t.Run("GetDeviceByKeyID", func(t *testing.T) {
		if _, _, _, err := GetDeviceByKeyID(ctx, sqlDB, "some-key"); err == nil {
			t.Error("want error on closed db, got nil")
		} else if err == ErrDeviceNotFound {
			t.Error("want a real DB error, not ErrDeviceNotFound")
		}
	})

	t.Run("UpdateDeviceSignCount", func(t *testing.T) {
		if err := UpdateDeviceSignCount(ctx, sqlDB, 1, 5, now); err == nil {
			t.Error("want error on closed db, got nil")
		}
	})

	t.Run("UpsertOverride", func(t *testing.T) {
		if err := UpsertOverride(ctx, sqlDB, 1, "block", "reason", "admin", now); err == nil {
			t.Error("want error on closed db, got nil")
		}
	})

	t.Run("UpsertPushToken", func(t *testing.T) {
		if err := UpsertPushToken(ctx, sqlDB, 1, "token", "production", now); err == nil {
			t.Error("want error on closed db, got nil")
		}
	})

	t.Run("ListPushTargets", func(t *testing.T) {
		if _, err := ListPushTargets(ctx, sqlDB); err == nil {
			t.Error("want error on closed db, got nil")
		}
	})

	t.Run("LookupNumber", func(t *testing.T) {
		if _, _, err := LookupNumber(ctx, sqlDB, "+14155550001", ""); err == nil {
			t.Error("want error on closed db, got nil")
		}
	})

	t.Run("BlocklistDelta", func(t *testing.T) {
		if _, _, _, err := BlocklistDelta(ctx, sqlDB, 0, 0, "", 10); err == nil {
			t.Error("want error on closed db, got nil")
		}
	})

	t.Run("RecomputeAllNumbers", func(t *testing.T) {
		if _, err := RecomputeAllNumbers(ctx, sqlDB, now); err == nil {
			t.Error("want error on closed db, got nil")
		}
	})

	t.Run("RecomputeAllTrust", func(t *testing.T) {
		if _, err := RecomputeAllTrust(ctx, sqlDB, now); err == nil {
			t.Error("want error on closed db, got nil")
		}
	})

	t.Run("RecomputeAll", func(t *testing.T) {
		if _, _, err := RecomputeAll(ctx, sqlDB, now); err == nil {
			t.Error("want error on closed db, got nil")
		}
	})
}

// TestRecomputeAllNumbers_PerNumberErrorPropagates seeds one real
// phone_numbers row (so the id-listing query succeeds) then renames away the
// reports table RecomputeNumber depends on, so the per-number recompute
// inside RecomputeAllNumbers's loop fails -- the loop-body error branch a
// wholesale closed-DB test can't reach, since that fails at the very first
// query instead.
func TestRecomputeAllNumbers_PerNumberErrorPropagates(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	if _, err := UpsertPhoneNumber(ctx, sqlDB, "+14155550099", now); err != nil {
		t.Fatalf("UpsertPhoneNumber: %v", err)
	}
	if _, err := sqlDB.Exec("RENAME TABLE reports TO reports_renamed_away"); err != nil {
		t.Fatalf("rename reports table: %v", err)
	}

	if _, err := RecomputeAllNumbers(ctx, sqlDB, now); err == nil {
		t.Error("want error when a per-number recompute fails, got nil")
	}
}

// TestRecomputeAllTrust_PerDeviceErrorPropagates seeds one real (non-seed)
// device row (so the id-listing query succeeds) then renames away the
// reports table the per-device report-count query depends on, exercising the
// loop-body error branch inside RecomputeAllTrust.
func TestRecomputeAllTrust_PerDeviceErrorPropagates(t *testing.T) {
	sqlDB := dbtest.SetupDB(t)
	ctx := context.Background()
	now := time.Now()

	insertDevice(t, sqlDB, "recompute-trust-error-device", 1.0)
	if _, err := sqlDB.Exec("RENAME TABLE reports TO reports_renamed_away"); err != nil {
		t.Fatalf("rename reports table: %v", err)
	}

	if _, err := RecomputeAllTrust(ctx, sqlDB, now); err == nil {
		t.Error("want error when a per-device report-count query fails, got nil")
	}
}

// TestIsValidBlocklistPrefix mirrors the API handler's own prefix validation
// test, pinning the store's independent second check (see the doc comment on
// isValidBlocklistPrefix) against the same NPA-NXX rule.
func TestIsValidBlocklistPrefix(t *testing.T) {
	cases := []struct {
		prefix string
		want   bool
	}{
		{"415555", true},
		{"000000", true},
		{"41555", false},   // too short
		{"4155555", false}, // too long
		{"41555a", false},  // non-digit
		{"", false},
	}
	for _, tc := range cases {
		if got := isValidBlocklistPrefix(tc.prefix); got != tc.want {
			t.Errorf("isValidBlocklistPrefix(%q) = %v, want %v", tc.prefix, got, tc.want)
		}
	}
}
