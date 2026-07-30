package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every check in run() below the flag parsing happens BEFORE a database
// connection is opened. These tests pin that: a bad argument must fail
// immediately with a usable message rather than after a connection attempt.
// None of them touch MySQL, which is why they need no dbtest setup.

func TestRun_RejectsMissingSource(t *testing.T) {
	err := run([]string{"-file", "x.csv"})
	if err == nil {
		t.Fatal("expected an error when -source is absent")
	}
	if !strings.Contains(err.Error(), "-source") {
		t.Errorf("error = %q, want it to name -source", err)
	}
}

func TestRun_RejectsUnknownSource(t *testing.T) {
	err := run([]string{"-source", "irs", "-file", "x.csv"})
	if err == nil {
		t.Fatal("expected an error for an unrecognized -source")
	}
	if !strings.Contains(err.Error(), "ftc") || !strings.Contains(err.Error(), "fcc") {
		t.Errorf("error = %q, want it to list the valid sources", err)
	}
	// The rejected value should appear, so the operator can see their typo.
	if !strings.Contains(err.Error(), "irs") {
		t.Errorf("error = %q, want it to echo the offending value", err)
	}
}

func TestRun_RejectsMissingFile(t *testing.T) {
	err := run([]string{"-source", "ftc"})
	if err == nil {
		t.Fatal("expected an error when -file is absent")
	}
	if !strings.Contains(err.Error(), "-file") {
		t.Errorf("error = %q, want it to name -file", err)
	}
}

func TestRun_RejectsUnknownCategory(t *testing.T) {
	err := run([]string{"-source", "ftc", "-file", "x.csv", "-category", "phishing"})
	if err == nil {
		t.Fatal("expected an error for a category outside the scoring enum")
	}
	for _, want := range []string{"scam", "robocall", "telemarketer", "other", "phishing"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %q, want it to mention %q", err, want)
		}
	}
}

func TestRun_AcceptsEveryValidCategory(t *testing.T) {
	// Point at a file that does not exist so validation passes and the run fails
	// later; the assertion is only that the CATEGORY was not what stopped it.
	for _, cat := range []string{"scam", "robocall", "telemarketer", "other"} {
		err := run([]string{"-source", "ftc", "-file", "/nonexistent.csv", "-category", cat})
		if err != nil && strings.Contains(err.Error(), "-category") {
			t.Errorf("category %q was rejected but is valid: %v", cat, err)
		}
	}
}

func TestRun_RejectsUnknownFlag(t *testing.T) {
	err := run([]string{"-nonsense"})
	if err == nil {
		t.Fatal("expected an error for an unknown flag")
	}
	if !strings.Contains(err.Error(), "parsing flags") {
		t.Errorf("error = %q, want it to mention flag parsing", err)
	}
}

// A bad config must be reported as a config problem. Validation of the flags
// still has to come first, though -- an operator with both a typo'd -source and
// a bad config should hear about the typo, which is the thing they just typed.
func TestRun_FlagValidationPrecedesConfigLoad(t *testing.T) {
	t.Setenv("ATTEST_MODE", "Apple") // would fail config.Load

	err := run([]string{"-source", "nope", "-file", "x.csv"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "loading config") {
		t.Errorf("error = %q, want the -source problem reported before the config problem", err)
	}
	if !strings.Contains(err.Error(), "-source") {
		t.Errorf("error = %q, want it to name -source", err)
	}
}

func TestRun_SurfacesConfigError(t *testing.T) {
	t.Setenv("ATTEST_MODE", "Apple")

	// Flags are all valid here, so config.Load is reached.
	err := run([]string{"-source", "ftc", "-file", "x.csv"})
	if err == nil {
		t.Fatal("expected a config error")
	}
	if !strings.Contains(err.Error(), "loading config") {
		t.Errorf("error = %q, want it to mention loading config", err)
	}
	if !strings.Contains(err.Error(), "ATTEST_MODE") {
		t.Errorf("error = %q, want it to name the offending variable", err)
	}
}

func TestRun_SurfacesDatabaseError(t *testing.T) {
	t.Setenv("ATTEST_MODE", "mock")
	t.Setenv("DB_DSN", "root@tcp(127.0.0.1:1)/nope?parseTime=true&timeout=1s")

	dir := t.TempDir()
	csv := filepath.Join(dir, "in.csv")
	if err := os.WriteFile(csv, []byte("Company_Phone_Number\n+14155550100\n"), 0o600); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}

	err := run([]string{"-source", "ftc", "-file", csv})
	if err == nil {
		t.Fatal("expected an error for an unreachable database")
	}
	if !strings.Contains(err.Error(), "database") && !strings.Contains(err.Error(), "migrations") {
		t.Errorf("error = %q, want it to mention the database or migrations", err)
	}
}

// The default number column is source-dependent; a wrong default silently
// imports zero rows, which looks like an empty dataset rather than a bug.
func TestDefaultNumberColumns_CoverBothSources(t *testing.T) {
	for _, src := range []string{"ftc", "fcc"} {
		if defaultNumberColumns[src] == "" {
			t.Errorf("no default number column for source %q", src)
		}
	}
}
