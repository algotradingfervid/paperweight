package csvio

import "strings"

// formulaTriggers are the first characters a spreadsheet treats as a formula (D-NUM-26).
const formulaTriggers = "=+-@\t\r"

func startsWithTrigger(s string) bool {
	return s != "" && strings.IndexByte(formulaTriggers, s[0]) >= 0
}

// EscapeFormula prefixes ' when s starts with a formula trigger.
func EscapeFormula(s string) string {
	if startsWithTrigger(s) {
		return "'" + s
	}
	return s
}

// UnescapeFormula removes a leading ' only when a formula trigger follows it.
func UnescapeFormula(s string) string {
	if strings.HasPrefix(s, "'") && startsWithTrigger(s[1:]) {
		return s[1:]
	}
	return s
}
