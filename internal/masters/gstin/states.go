package gstin

import (
	"fmt"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/gstcalc"
)

// State is a GST state: its two-digit code and name.
type State struct{ Code, Name string }

// stateCodes is STATE_NUMBERS, india_compliance/gst_india/constants/__init__.py:96,
// in the same order. migrations/01000_gst_state.sql seeds the same rows.
var stateCodes = []string{
	"35", "37", "12", "18", "10", "04", "22", "26", "07", "30", "24", "06", "02",
	"01", "20", "29", "32", "38", "31", "23", "27", "14", "17", "15", "13", "21",
	"96", "97", "34", "03", "08", "11", "33", "36", "16", "09", "05", "19",
}

// states takes its names from gstcalc, which owns the state table (CD-130);
// 96 (Other Countries) is not a place of supply, so gstin adds it itself.
var states = func() []State {
	out := make([]State, 0, len(stateCodes))
	for _, code := range stateCodes {
		name, ok := gstcalc.StateName(code)
		if code == otherCountries {
			name, ok = "Other Countries", true
		}
		if !ok {
			panic("gstin: gstcalc has no state " + code + " (CD-130)")
		}
		out = append(out, State{code, name})
	}
	return out
}()

// otherCountries is the place-of-supply code for overseas parties; it is not
// an Indian state (INDIAN_STATES, constants/__init__.py:137).
const otherCountries = "96"

// States returns the 38 GST states in STATE_NUMBERS order.
func States() []State { return append([]State(nil), states...) }

// StateByCode finds a state by its two-digit code.
func StateByCode(code string) (State, bool) {
	for _, s := range states {
		if s.Code == code {
			return s, true
		}
	}
	return State{}, false
}

// StateByName finds a state by name, ignoring case.
func StateByName(name string) (State, bool) {
	for _, s := range states {
		if strings.EqualFold(s.Name, name) {
			return s, true
		}
	}
	return State{}, false
}

// IsIndianState reports whether code is a listed state other than Other Countries.
func IsIndianState(code string) bool {
	_, ok := StateByCode(code)
	return ok && code != otherCountries
}

// ResolveState returns the state code for a record (owner "branch" or
// "address") with a normalised GSTIN: the GSTIN's state when stateCode is
// blank, else stateCode if they agree (overrides/address.py:71, :100).
// Indian addresses only; callers skip it for foreign addresses (MST-057).
func ResolveState(gstin, stateCode, owner string) (string, error) {
	if gstin == "" {
		return stateCode, nil
	}
	code := StateCode(gstin)
	s, ok := StateByCode(code)
	if !ok || !IsIndianState(code) {
		return "", fmt.Errorf("GSTIN state code %s is not a current GST state code. Check the GSTIN.", code)
	}
	if stateCode == "" || stateCode == code {
		return code, nil
	}
	chosen, _ := StateByCode(stateCode)
	return "", fmt.Errorf("GSTIN %s is for %s (%s) but the %s state is %s (%s). Fix the GSTIN or the state.",
		gstin, s.Name, s.Code, owner, chosen.Name, stateCode)
}
