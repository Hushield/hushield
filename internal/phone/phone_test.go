package phone

import (
	"errors"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{"already e164 NANP", "+14155552671", "+14155552671", false},
		{"formatted with parens dashes spaces", "(415) 555-2671", "+14155552671", false},
		{"formatted with dashes only", "415-555-2671", "+14155552671", false},
		{"formatted with dots", "415.555.2671", "+14155552671", false},
		{"10 digit NANP", "4155552671", "+14155552671", false},
		{"11 digit NANP leading 1", "14155552671", "+14155552671", false},
		{"plus with spaces and dashes", "+1 415-555-2671", "+14155552671", false},
		{"international e164", "+442071838750", "+442071838750", false},

		{"empty string", "", "", true},
		{"just a plus sign", "+", "", true},
		{"too short digits", "12345", "", true},
		{"9 digit local", "215555267", "", true},
		{"11 digit not leading 1", "24155552671", "", true},
		{"plus 1 with only 10 digits total", "+1415555267", "", true},
		{"plus with too many digits", "+1234567890123456", "", true},
		{"plus with too few digits", "+1234567", "", true},
		{"letters", "notanumber", "", true},
		{"plus with letters", "+1415abc2671", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Normalize(tc.raw)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidNumber) {
					t.Fatalf("Normalize(%q) err = %v, want ErrInvalidNumber", tc.raw, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Normalize(%q) unexpected error: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("Normalize(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestPrefix(t *testing.T) {
	cases := []struct {
		name string
		e164 string
		want string
	}{
		{"nanp number", "+14155552671", "415555"},
		{"nanp spoofed neighbor", "+14155559999", "415555"},
		{"international number", "+442071838750", "442071"},
		{"short international number", "+12345", "2345"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Prefix(tc.e164)
			if got != tc.want {
				t.Errorf("Prefix(%q) = %q, want %q", tc.e164, got, tc.want)
			}
		})
	}
}
