package gstin

import (
	"fmt"
	"strings"
)

// State is a GST state: its two-digit code and name.
type State struct{ Code, Name string }

// states is STATE_NUMBERS, india_compliance/gst_india/constants/__init__.py:96,
// verbatim and in the same order. migrations/01000_gst_state.sql seeds the same rows.
var states = []State{
	{"35", "Andaman and Nicobar Islands"},
	{"37", "Andhra Pradesh"},
	{"12", "Arunachal Pradesh"},
	{"18", "Assam"},
	{"10", "Bihar"},
	{"04", "Chandigarh"},
	{"22", "Chhattisgarh"},
	{"26", "Dadra and Nagar Haveli and Daman and Diu"},
	{"07", "Delhi"},
	{"30", "Goa"},
	{"24", "Gujarat"},
	{"06", "Haryana"},
	{"02", "Himachal Pradesh"},
	{"01", "Jammu and Kashmir"},
	{"20", "Jharkhand"},
	{"29", "Karnataka"},
	{"32", "Kerala"},
	{"38", "Ladakh"},
	{"31", "Lakshadweep Islands"},
	{"23", "Madhya Pradesh"},
	{"27", "Maharashtra"},
	{"14", "Manipur"},
	{"17", "Meghalaya"},
	{"15", "Mizoram"},
	{"13", "Nagaland"},
	{"21", "Odisha"},
	{"96", "Other Countries"},
	{"97", "Other Territory"},
	{"34", "Puducherry"},
	{"03", "Punjab"},
	{"08", "Rajasthan"},
	{"11", "Sikkim"},
	{"33", "Tamil Nadu"},
	{"36", "Telangana"},
	{"16", "Tripura"},
	{"09", "Uttar Pradesh"},
	{"05", "Uttarakhand"},
	{"19", "West Bengal"},
}

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
