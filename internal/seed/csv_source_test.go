package seed

import (
	"context"
	"reflect"
	"testing"

	"spamfilter/internal/scoring"
)

func TestCSVSource_Records(t *testing.T) {
	src := CSVSource{
		Path:            "testdata/sample.csv",
		NumberColumn:    "Company_Phone_Number",
		DefaultCategory: scoring.CategoryRobocall,
	}

	records, err := src.Records(context.Background())
	if err != nil {
		t.Fatalf("Records: %v", err)
	}

	want := []Record{
		{RawNumber: "+14155551234", Category: scoring.CategoryRobocall},
		{RawNumber: "4155555678", Category: scoring.CategoryRobocall},
	}
	if !reflect.DeepEqual(records, want) {
		t.Errorf("Records = %+v, want %+v", records, want)
	}
}

func TestCSVSource_Records_MissingColumn(t *testing.T) {
	src := CSVSource{
		Path:         "testdata/sample.csv",
		NumberColumn: "Nonexistent_Column",
	}

	_, err := src.Records(context.Background())
	if err == nil {
		t.Fatal("Records: want error for missing column, got nil")
	}
}

func TestCSVSource_Records_MissingFile(t *testing.T) {
	src := CSVSource{
		Path:         "testdata/does-not-exist.csv",
		NumberColumn: "Company_Phone_Number",
	}

	_, err := src.Records(context.Background())
	if err == nil {
		t.Fatal("Records: want error for missing file, got nil")
	}
}

// TestCSVSource_Records_RaggedRowMissingTargetColumn confirms a ragged row
// that ends before the target column is silently skipped (not an error),
// distinct from the already-covered "entirely blank row" skip case.
func TestCSVSource_Records_RaggedRowMissingTargetColumn(t *testing.T) {
	src := CSVSource{
		Path:         "testdata/ragged.csv",
		NumberColumn: "Company_Phone_Number",
	}

	records, err := src.Records(context.Background())
	if err != nil {
		t.Fatalf("Records: %v", err)
	}
	want := []Record{{RawNumber: "+14155551234"}}
	if !reflect.DeepEqual(records, want) {
		t.Errorf("Records = %+v, want %+v (the ragged row missing the target column must be skipped, not errored)", records, want)
	}
}

// TestCSVSource_Records_MalformedCSVReturnsError confirms a mid-file CSV
// syntax error (an unterminated quoted field) is surfaced as an error, not
// silently truncating the import.
func TestCSVSource_Records_MalformedCSVReturnsError(t *testing.T) {
	src := CSVSource{
		Path:         "testdata/malformed.csv",
		NumberColumn: "Company_Phone_Number",
	}

	_, err := src.Records(context.Background())
	if err == nil {
		t.Fatal("Records: want error for malformed CSV content, got nil")
	}
}
