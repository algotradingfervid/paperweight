package gstin

import (
	"regexp"
	"strings"
	"unicode"
)

// Invisible characters pasted from PDFs and chat apps (Review Focus 1).
const (
	zeroWidthSpace = "\u200b" // ZERO WIDTH SPACE
	byteOrderMark  = "\ufeff" // ZERO WIDTH NO-BREAK SPACE (BOM)
)

// alphabet is the check-digit code point table
// (utils/__init__.py:560, validate_gstin_check_digit).
const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

// GSTIN patterns, india_compliance/gst_india/constants/__init__.py:1444-1457.
var (
	normalPattern  = regexp.MustCompile(`^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z]{1}[1-9A-Z]{1}[Z1-9ABD-J]{1}[0-9A-Z]{1}$`) // :1444 NORMAL
	govtDeptID     = regexp.MustCompile(`^[0-9]{2}[A-Z]{4}[0-9]{5}[A-Z]{1}[0-9]{1}[Z]{1}[0-9]{1}$`)            // :1447 GOVT_DEPTID
	nriID          = regexp.MustCompile(`^[0-9]{4}[A-Z]{3}[0-9]{5}[N][R][0-9A-Z]{1}$`)                         // :1451 NRI_ID
	oidarPattern   = regexp.MustCompile(`^[9][9][0-9]{2}[A-Z]{3}[0-9]{5}[O][S][0-9A-Z]{1}$`)                   // :1452 OIDAR
	unbodyPattern  = regexp.MustCompile(`^[0-9]{4}[A-Z]{3}[0-9]{5}[UO]{1}[N][A-Z0-9]{1}$`)                      // :1455 UNBODY
	tdsPattern     = regexp.MustCompile(`^[0-9]{2}[A-Z]{4}[A-Z0-9]{1}[0-9]{4}[A-Z]{1}[1-9A-Z]{1}[D][0-9A-Z]$`) // :1456 TDS
	tcsPattern     = regexp.MustCompile(`^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z]{1}[1-9A-Z]{1}[C]{1}[0-9A-Z]{1}$`)      // :1457 TCS
	panPattern     = regexp.MustCompile(`^[A-Z]{5}[0-9]{4}[A-Z]{1}$`)                                          // :1473 PAN_NUMBER
	pincodePattern = regexp.MustCompile(`^[1-9][0-9]{5}$`)                                                     // :1474 PINCODE_FORMAT
)

// registered is REGISTERED (constants/__init__.py:1448): NORMAL | GOVT_DEPTID.
func registered(g string) bool { return normalPattern.MatchString(g) || govtDeptID.MatchString(g) }

// overseas is OVERSEAS (constants/__init__.py:1453): NRI_ID | OIDAR.
func overseas(g string) bool { return nriID.MatchString(g) || oidarPattern.MatchString(g) }

// Normalize removes zero-width spaces and BOMs, trims Unicode space (including
// U+00A0) and upper-cases s. Spaces inside s are kept, so the length check
// still catches them.
func Normalize(s string) string {
	s = strings.ReplaceAll(s, zeroWidthSpace, "")
	s = strings.ReplaceAll(s, byteOrderMark, "")
	return strings.ToUpper(strings.TrimFunc(s, unicode.IsSpace))
}

// CheckDigit returns the check character for a 14-character GSTIN prefix over
// [0-9A-Z] (utils/__init__.py:554, validate_gstin_check_digit).
func CheckDigit(prefix14 string) byte {
	const mod = len(alphabet)
	factor, total := 1, 0
	for i := range len(prefix14) {
		digit := factor * strings.IndexByte(alphabet, prefix14[i])
		total += digit/mod + digit%mod
		factor = 3 - factor // alternate 1, 2
	}
	return alphabet[(mod-total%mod)%mod]
}
