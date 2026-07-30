// Command seed imports a public robocall/spam-complaint dataset (FTC or FCC)
// as reports from a synthetic per-source "seed device" (see internal/seed),
// so the blocklist is useful on day one. Imported numbers flow through the
// exact same scoring pipeline as community reports: they get a decaying
// score, age off on their own over time, and can still be reinforced or
// contradicted by real community votes.
//
// Download the source CSVs from:
//   - FTC Do Not Call complaint data: https://www.ftc.gov/site-information/open-government/data-sets
//     (Do Not Call complaint / robocall datasets)
//   - FCC Consumer Complaints Data Center: https://www.fcc.gov/consumer-help-center-data
//
// Both agencies periodically change their published CSV header names, so
// the -number-column defaults below are a best guess as of this writing;
// inspect the downloaded file's header row and override with -number-column
// if it differs.
//
// Example:
//
//	seed -source ftc -file ftc_dnc_complaints.csv -trust 1.5
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"spamfilter/internal/config"
	"spamfilter/internal/db"
	"spamfilter/internal/scoring"
	"spamfilter/internal/seed"
)

// defaultNumberColumns are best-guess CSV header names for each source's
// phone-number column; override with -number-column if the downloaded file
// uses a different header.
var defaultNumberColumns = map[string]string{
	"ftc": "Company_Phone_Number",
	"fcc": "Caller ID Number",
}

// validSeedCategories is the set of scoring categories a seeded number may be
// assigned via -category. Validated before any DB connection is opened.
var validSeedCategories = map[string]bool{
	string(scoring.CategoryScam):         true,
	string(scoring.CategoryRobocall):     true,
	string(scoring.CategoryTelemarketer): true,
	string(scoring.CategoryOther):        true,
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatalf("%v", err)
	}
}

// run is main's body without the process-exiting, so the argument validation
// can be tested. Every check below happens BEFORE any database connection is
// opened, which is deliberate: a typo in -source should fail instantly with a
// usable message, not after a connection timeout.
func run(args []string) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	sourceFlag := fs.String("source", "", `public data source: "ftc" or "fcc" (required)`)
	fileFlag := fs.String("file", "", "path to the local CSV file to import (required)")
	numberColumnFlag := fs.String("number-column", "", "CSV header name of the phone-number column (default depends on -source; see top-of-file comment)")
	trustFlag := fs.Float64("trust", 1.5, "trust_weight assigned to this source's seed device")
	categoryFlag := fs.String("category", "robocall", "default scoring category assigned to imported numbers")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parsing flags: %w", err)
	}

	if *sourceFlag != "ftc" && *sourceFlag != "fcc" {
		return fmt.Errorf(`-source must be "ftc" or "fcc" (got %q)`, *sourceFlag)
	}
	if *fileFlag == "" {
		return fmt.Errorf("-file is required")
	}
	if !validSeedCategories[*categoryFlag] {
		return fmt.Errorf("-category must be one of scam, robocall, telemarketer, other (got %q)", *categoryFlag)
	}

	numberColumn := *numberColumnFlag
	if numberColumn == "" {
		numberColumn = defaultNumberColumns[*sourceFlag]
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	sqlDB, err := db.Open(cfg.DBDsn)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer sqlDB.Close()

	if err := db.Migrate(sqlDB); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	category := scoring.Category(*categoryFlag)
	src := seed.CSVSource{
		Path:            *fileFlag,
		NumberColumn:    numberColumn,
		DefaultCategory: category,
	}
	seeder := seed.Seeder{DB: sqlDB}

	imported, skipped, err := seeder.Seed(context.Background(), src, *sourceFlag, *trustFlag, category)
	if err != nil {
		return fmt.Errorf("seed failed after imported=%d skipped=%d: %w", imported, skipped, err)
	}

	log.Printf("imported=%d skipped=%d source=%s", imported, skipped, *sourceFlag)
	return nil
}
