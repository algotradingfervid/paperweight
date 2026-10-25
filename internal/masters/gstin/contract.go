// Package gstin validates GSTINs (spec §5). MST owns it.
package gstin

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ValidateGSTIN normalises s and checks its length and check digit; the
// category is checked separately by ValidateCategory. A blank GSTIN is valid
// (india_compliance/gst_india/utils/__init__.py:375, validate_gstin).
func ValidateGSTIN(s string) error {
	g := Normalize(s)
	if g == "" {
		return nil
	}
	if utf8.RuneCountInString(g) != 15 {
		return fmt.Errorf("GSTIN %s must have 15 characters.", g)
	}
	if strings.Trim(g, alphabet) != "" || CheckDigit(g[:14]) != g[14] {
		return fmt.Errorf("GSTIN %s is not valid: the last character doesn't match. Check for typing mistakes.", g)
	}
	return nil
}

// StateCode returns the GSTIN's first two characters, its GST state code.
func StateCode(gstin string) string {
	if len(gstin) < 2 {
		return ""
	}
	return gstin[:2]
}

// PANFromGSTIN returns characters 3–12 when they form a PAN, else ""
// (utils/__init__.py:457, get_pan_from_gstin).
func PANFromGSTIN(gstin string) string {
	if len(gstin) < 12 || !panPattern.MatchString(gstin[2:12]) {
		return ""
	}
	return gstin[2:12]
}
