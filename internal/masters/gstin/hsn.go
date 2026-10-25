package gstin

import (
	"errors"
	"strings"
)

// HSN rules: VALID_HSN_LENGTHS and SERVICE_HSN_PREFIX,
// india_compliance/gst_india/constants/__init__.py:1524-1525.
const serviceHSNPrefix = "99"

// ValidateGoodsHSN checks a goods item's HSN: required, 4, 6 or 8 digits, and
// not a service (SAC) code (MST-070…MST-072).
func ValidateGoodsHSN(code string) (string, error) {
	c := strings.TrimSpace(code)
	switch {
	case c == "":
		return "", errors.New("HSN code is required for goods. Enter a 4, 6 or 8 digit HSN code.")
	case !allDigits(c) || len(c) != 4 && len(c) != 6 && len(c) != 8:
		return "", errors.New("HSN code must be 4, 6 or 8 digits.")
	case strings.HasPrefix(c, serviceHSNPrefix):
		return "", errors.New("Codes starting 99 are service (SAC) codes. Set the item type to Service or use a goods HSN.")
	}
	return c, nil
}

// ValidateSAC checks a service's SAC: blank, or 6 digits starting 99 (MST-073).
func ValidateSAC(code string) (string, error) {
	c := strings.TrimSpace(code)
	if c != "" && (len(c) != 6 || !allDigits(c) || !strings.HasPrefix(c, serviceHSNPrefix)) {
		return "", errors.New("SAC code must be 6 digits starting with 99.")
	}
	return c, nil
}

func allDigits(s string) bool {
	return strings.Trim(s, "0123456789") == ""
}
