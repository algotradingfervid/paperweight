package gstin

import "errors"

// ValidatePAN normalises s and checks it against PAN_NUMBER
// (constants/__init__.py:1473). A blank PAN is returned as "" with no error.
func ValidatePAN(s string) (string, error) {
	p := Normalize(s)
	if p != "" && !panPattern.MatchString(p) {
		return "", errors.New("PAN must be 10 characters: 5 letters, 4 digits, 1 letter (e.g. AAFCV2449G).")
	}
	return p, nil
}
