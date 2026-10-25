package naming

import (
	"errors"
	"testing"
)

func strPtr(s string) *string { return &s }

// TestValidateGSTDocNumber covers NAM-021.
func TestValidateGSTDocNumber(t *testing.T) {
	bad := map[string]string{
		"SI$1231":            msgGSTFormat,
		"012345678901234567": msgGSTTooLong,
		"SI 2020 05":         msgGSTFormat,
		"SI.2020.0001":       msgGSTFormat,
		"PI2021 - 001":       msgGSTFormat,
	}
	for in, want := range bad {
		err := ValidateGSTDocNumber(in)
		if err == nil {
			t.Errorf("ValidateGSTDocNumber(%q) = nil, want %q", in, want)
			continue
		}
		if err.Error() != want {
			t.Errorf("ValidateGSTDocNumber(%q) = %q, want %q", in, err.Error(), want)
		}
		if !errors.Is(err, ErrInvalidGSTNumber) {
			t.Errorf("ValidateGSTDocNumber(%q): errors.Is(err, ErrInvalidGSTNumber) = false (err %v)", in, err)
		}
	}
	good := []string{"012345678901236", "SI/2020/0001", "SI/2020-0001", "2020-PI-0001", "PI2020-0001"}
	for _, in := range good {
		if err := ValidateGSTDocNumber(in); err != nil {
			t.Errorf("ValidateGSTDocNumber(%q) = %v, want nil", in, err)
		}
	}
}

// TestRender_AmendmentLength covers NAM-046.
func TestRender_AmendmentLength(t *testing.T) {
	s := amendmentNumber("PO/26-27/00007", 9)
	if s != "PO/26-27/00007-9" {
		t.Fatalf("amendmentNumber = %q, want %q", s, "PO/26-27/00007-9")
	}
	if len(s) != 16 {
		t.Errorf("len(%q) = %d, want 16", s, len(s))
	}
	if err := ValidateGSTDocNumber(s); err != nil {
		t.Errorf("ValidateGSTDocNumber(%q) = %v, want nil", s, err)
	}
}

// TestLabel pins WP0's Label (CD-53) for every caller.
func TestLabel(t *testing.T) {
	if got := Label("", 42); got != "Draft 42" {
		t.Errorf(`Label("", 42) = %q, want "Draft 42"`, got)
	}
	if got := Label("PO/26-27/00001", 42); got != "PO/26-27/00001" {
		t.Errorf(`Label("PO/26-27/00001", 42) = %q, want "PO/26-27/00001"`, got)
	}
}

// TestGuardNumberField covers the guard DOC calls on save, update and import.
func TestGuardNumberField(t *testing.T) {
	cases := []struct {
		name             string
		stored, supplied *string
		wantKind         error // nil = no error
		wantMsg          string
	}{
		{"none_supplied", nil, nil, nil, ""},
		{"client_number", nil, strPtr("PO/26-27/09999"), ErrClientNumber, msgClientNumber},
		{"changed", strPtr("PO/26-27/00007"), strPtr("PO/26-27/00008"), ErrNumberImmutable, msgImmutable},
		{"equal_echo", strPtr("PO/26-27/00007"), strPtr("PO/26-27/00007"), nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := GuardNumberField(c.stored, c.supplied)
			if c.wantKind == nil {
				if err != nil {
					t.Fatalf("GuardNumberField = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("GuardNumberField = nil, want %q", c.wantMsg)
			}
			if !errors.Is(err, c.wantKind) {
				t.Errorf("errors.Is(err, %v) = false (err %v)", c.wantKind, err)
			}
			if err.Error() != c.wantMsg {
				t.Errorf("GuardNumberField = %q, want %q", err.Error(), c.wantMsg)
			}
		})
	}
}
