package domain

import (
	"fmt"
	"regexp"
)

// e164Pattern matches E.164 phone numbers: + followed by 7-15 digits.
var e164Pattern = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)

// PhoneNumber is a value object representing a phone number in E.164 format.
// Always valid in memory — use NewPhoneNumber to construct.
type PhoneNumber struct {
	value string
}

// NewPhoneNumber creates a PhoneNumber from a raw string, validating E.164 format.
// E.164 requires: '+' prefix, 7-15 digits, no leading zero after country code.
func NewPhoneNumber(raw string) (PhoneNumber, error) {
	if raw == "" {
		return PhoneNumber{}, fmt.Errorf("phone number cannot be empty: %w", ErrInvalidPhoneNumber)
	}
	if !e164Pattern.MatchString(raw) {
		return PhoneNumber{}, fmt.Errorf("phone number %q is not valid E.164: %w", raw, ErrInvalidPhoneNumber)
	}
	return PhoneNumber{value: raw}, nil
}

// MustPhoneNumber creates a PhoneNumber, panicking on invalid input. Use only in tests.
func MustPhoneNumber(raw string) PhoneNumber {
	p, err := NewPhoneNumber(raw)
	if err != nil {
		panic(err)
	}
	return p
}

func (p PhoneNumber) String() string { return p.value }
func (p PhoneNumber) IsZero() bool   { return p.value == "" }
