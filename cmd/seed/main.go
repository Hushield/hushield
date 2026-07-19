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
	"log"

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
	sourceFlag := flag.String("source", "", `public data source: "ftc" or "fcc" (required)`)
	fileFlag := flag.String("file", "", "path to the local CSV file to import (required)")
	numberColumnFlag := flag.String("number-column", "", "CSV header name of the phone-number column (default depends on -source; see top-of-file comment)")
	trustFlag := flag.Float64("trust", 1.5, "trust_weight assigned to this source's seed device")
	categoryFlag := flag.String("category", "robocall", "default scoring category assigned to imported numbers")
	flag.Parse()

	if *sourceFlag != "ftc" && *sourceFlag != "fcc" {
		log.Fatalf(`-source must be "ftc" or "fcc" (got %q)`, *sourceFlag)
	}
	if *fileFlag == "" {
		log.Fatalf("-file is required")
	}
	if !validSeedCategories[*categoryFlag] {
		log.Fatalf("-category must be one of scam, robocall, telemarketer, other (got %q)", *categoryFlag)
	}

	numberColumn := *numberColumnFlag
	if numberColumn == "" {
		numberColumn = defaultNumberColumns[*sourceFlag]
	}

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

	category := scoring.Category(*categoryFlag)
	src := seed.CSVSource{
		Path:            *fileFlag,
		NumberColumn:    numberColumn,
		DefaultCategory: category,
	}
	seeder := seed.Seeder{DB: sqlDB}

	imported, skipped, err := seeder.Seed(context.Background(), src, *sourceFlag, *trustFlag, category)
	if err != nil {
		log.Fatalf("seed failed after imported=%d skipped=%d: %v", imported, skipped, err)
	}

	log.Printf("imported=%d skipped=%d source=%s", imported, skipped, *sourceFlag)
}
