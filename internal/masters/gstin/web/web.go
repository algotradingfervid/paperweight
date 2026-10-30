// Package gstinweb is the parsers behind components.CodeInput (CD-198). They
// live here because internal/ui can't import internal/masters (CD-201(3)). WP3
// merges the map into components.ValidateHandler and docscreen.Deps.Parse.
package gstinweb

import (
	"fmt"
	"strings"

	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// Parsers returns one parser per components.CodeKind, keyed by its string.
// The messages are the gstin package's.
func Parsers() map[string]components.ParseFunc {
	return map[string]components.ParseFunc{
		string(components.GSTIN):   parseGSTIN,
		string(components.PAN):     noEcho(gstin.ValidatePAN),
		string(components.HSN):     noEcho(validateHSN),
		string(components.Pincode): noEcho(gstin.ValidatePincode),
	}
}

func noEcho(validate func(string) (string, error)) components.ParseFunc {
	return func(in string) (string, string, error) {
		v, err := validate(in)
		return v, "", err
	}
}

func parseGSTIN(in string) (string, string, error) {
	g := gstin.Normalize(in)
	if err := gstin.ValidateGSTIN(g); err != nil {
		return "", "", err
	}
	return g, gstinEcho(g), nil
}

// gstinEcho is the GSTIN's state and PAN. Special shapes that pass the check
// digit may have neither (MST-036, MST-038), so each part is optional.
func gstinEcho(g string) string {
	var parts []string
	if s, ok := gstin.StateByCode(gstin.StateCode(g)); ok {
		parts = append(parts, fmt.Sprintf("%s (%s)", s.Name, s.Code))
	}
	if pan := gstin.PANFromGSTIN(g); pan != "" {
		parts = append(parts, "PAN "+pan)
	}
	return strings.Join(parts, " · ")
}

// validateHSN accepts a SAC or a goods HSN, because the kind can't know the
// item type: a code starting 99 is checked as a SAC, any other as a goods HSN
// (expensetype.Validate's rule). Save decides.
func validateHSN(code string) (string, error) {
	if strings.HasPrefix(strings.TrimSpace(code), "99") {
		return gstin.ValidateSAC(code)
	}
	return gstin.ValidateGoodsHSN(code)
}
