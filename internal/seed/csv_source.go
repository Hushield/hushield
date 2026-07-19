package seed

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"

	"spamfilter/internal/scoring"
)

// CSVSource implements Source by reading records from a local CSV file: the
// raw phone number comes from the column named NumberColumn, and every
// yielded Record is tagged with DefaultCategory.
type CSVSource struct {
	Path            string
	NumberColumn    string
	DefaultCategory scoring.Category
}

// Records opens s.Path, locates NumberColumn in the header row (returning an
// error if it is absent), and yields one Record per data row. Rows that are
// entirely blank (every cell empty after trimming whitespace) are skipped.
func (s CSVSource) Records(ctx context.Context) ([]Record, error) {
	f, err := os.Open(s.Path)
	if err != nil {
		return nil, fmt.Errorf("seed: opening %s: %w", s.Path, err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1 // rows may be ragged; blank/missing-column rows are handled below

	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("seed: reading %s header: %w", s.Path, err)
	}

	column := -1
	for i, name := range header {
		if strings.TrimSpace(name) == s.NumberColumn {
			column = i
			break
		}
	}
	if column == -1 {
		return nil, fmt.Errorf("seed: %s has no column named %q", s.Path, s.NumberColumn)
	}

	var records []Record
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("seed: reading %s: %w", s.Path, err)
		}

		if rowIsBlank(row) {
			continue
		}
		if column >= len(row) {
			continue // ragged row missing the target column
		}

		records = append(records, Record{RawNumber: row[column], Category: s.DefaultCategory})
	}

	return records, nil
}

func rowIsBlank(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}
