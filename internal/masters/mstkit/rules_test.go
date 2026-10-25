package mstkit_test

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/mstkit"
)

// MST-002: code is trimmed and stored upper-case.
func TestMaster_CodeNormalised(t *testing.T) {
	for raw, want := range map[string]string{
		"  shree ":    "SHREE",
		"  v-sharma ": "V-SHARMA",
	} {
		got, err := mstkit.NormalizeCode(raw)
		if err != nil || got != want {
			t.Errorf("NormalizeCode(%q) = %q, %v; want %q, nil", raw, got, err, want)
		}
	}
}

// MST-004: code format is enforced.
func TestMaster_CodeFormat(t *testing.T) {
	const msg = "Code can use letters, digits and . _ / - only, must start with a letter or digit, and be at most 20 characters."
	for _, bad := range []string{"A B", "-ABC", "ABC#1", strings.Repeat("A", 21), "ÄBC"} {
		if _, err := mstkit.NormalizeCode(bad); err == nil || err.Error() != msg {
			t.Errorf("NormalizeCode(%q) error = %v; want %q", bad, err, msg)
		}
	}
	for _, ok := range []string{"MS-ANGLE-50", "PO/HSN.1", "A_1", strings.Repeat("A", 20)} {
		got, err := mstkit.NormalizeCode(ok)
		if err != nil || got != ok {
			t.Errorf("NormalizeCode(%q) = %q, %v; want %q, nil", ok, got, err, ok)
		}
	}
	// A blank code gets MST-001's message (Task 16's TestMaster_CodeRequired uses it per master).
	const required = "Code is required. Enter a short code such as SHREE."
	for _, blank := range []string{"", "   "} {
		if _, err := mstkit.NormalizeCode(blank); err == nil || err.Error() != required {
			t.Errorf("NormalizeCode(%q) error = %v; want %q", blank, err, required)
		}
	}
}

// MST-017: name is required and trimmed.
func TestMaster_NameRequiredTrimmed(t *testing.T) {
	const msg = "Name is required."
	for _, blank := range []string{"   ", ""} {
		if _, err := mstkit.NormalizeName(blank); err == nil || err.Error() != msg {
			t.Errorf("NormalizeName(%q) error = %v; want %q", blank, err, msg)
		}
	}
	got, err := mstkit.NormalizeName("  Shree Steel  ")
	if err != nil || got != "Shree Steel" {
		t.Errorf("NormalizeName = %q, %v; want %q, nil", got, err, "Shree Steel")
	}
}
