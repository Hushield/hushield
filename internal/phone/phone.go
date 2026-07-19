// Package phone normalizes and inspects phone numbers, with NANP (US/Canada)
// treated as the first-class case.
package phone

import (
	"errors"
	"strings"
)

// ErrInvalidNumber is returned by Normalize when raw cannot be parsed into a
// valid E.164 phone number.
var ErrInvalidNumber = errors.New("phone: invalid number")

// Normalize converts raw into a validated E.164 phone number (e.g.
// "+14155552671"). It strips spaces, dashes, parens, and dots, then accepts:
//   - "+<digits>"
//   - 10 NANP digits (assumed +1)
//   - 11 digits starting with "1" (assumed +1)
//
// The result must be "+" followed by 8-15 digits, and any number beginning
// with country code 1 (NANP) must be exactly 11 digits total. Anything else
// returns ErrInvalidNumber. Normalize never stores or logs raw input.
func Normalize(raw string) (string, error) {
	cleaned := stripFormatting(raw)
	if cleaned == "" {
		return "", ErrInvalidNumber
	}

	var digits string
	if strings.HasPrefix(cleaned, "+") {
		digits = cleaned[1:]
	} else {
		switch len(cleaned) {
		case 10:
			digits = "1" + cleaned
		case 11:
			if cleaned[0] != '1' {
				return "", ErrInvalidNumber
			}
			digits = cleaned
		default:
			return "", ErrInvalidNumber
		}
	}

	if !isAllDigits(digits) {
		return "", ErrInvalidNumber
	}
	if len(digits) < 8 || len(digits) > 15 {
		return "", ErrInvalidNumber
	}
	if digits[0] == '1' && len(digits) != 11 {
		return "", ErrInvalidNumber
	}

	return "+" + digits, nil
}

// Prefix returns a leading prefix of e164 suitable for neighbor-spoof
// matching. For +1 (NANP) numbers this is the 6-digit NPA-NXX (area code +
// exchange, the digits immediately after "+1"). For other country codes it
// is a best-effort leading-6-significant-digit prefix of the full number.
//
// Kept for client/serve-path parity (the blocklist neighbor-spoof prefix
// format); it has no direct Go caller yet.
func Prefix(e164 string) string {
	digits := strings.TrimPrefix(e164, "+")

	if strings.HasPrefix(e164, "+1") {
		digits = digits[1:]
	}

	if len(digits) > 6 {
		return digits[:6]
	}
	return digits
}

func stripFormatting(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		switch r {
		case ' ', '-', '(', ')', '.':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
