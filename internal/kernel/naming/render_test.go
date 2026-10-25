package naming

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/types"
)

// gstRe is the GST document-number format (NAM-020, NAM-021).
var gstRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9/-]{0,15}$`)

func mustParse(t testing.TB, s string) template {
	t.Helper()
	tp, err := parseTemplate(s)
	if err != nil {
		t.Fatalf("parseTemplate(%q): %v", s, err)
	}
	return tp
}

// TestTemplate_DefaultsRender covers NAM-001.
func TestTemplate_DefaultsRender(t *testing.T) {
	for tpl, want := range map[string]string{
		"PO/{FY}/{#####}": "PO/26-27/00001",
		"GR/{FY}/{#####}": "GR/26-27/00001",
		"EX/{FY}/{#####}": "EX/26-27/00001",
	} {
		got := mustParse(t, tpl).render(day(2026, 4, 1), 1)
		if got != want {
			t.Errorf("render(%q, 2026-04-01, 1) = %q, want %q", tpl, got, want)
		}
		if len(got) > 14 {
			t.Errorf("render(%q) = %q has %d characters, want 14 or fewer", tpl, got, len(got))
		}
	}
}

// TestRender_FYToken covers NAM-002.
func TestRender_FYToken(t *testing.T) {
	tp := mustParse(t, "PO/{FY}/{#####}")
	for _, c := range []struct {
		d    types.Date
		want string
	}{
		{day(2025, 4, 1), "PO/25-26/00001"},
		{day(2026, 3, 31), "PO/25-26/00001"},
		{day(2026, 4, 1), "PO/26-27/00001"},
		{day(2028, 2, 29), "PO/27-28/00001"},
		{day(2000, 1, 15), "PO/99-00/00001"},
		{day(2099, 4, 1), "PO/99-00/00001"},
	} {
		if got := tp.render(c.d, 1); got != c.want {
			t.Errorf("render(PO/{FY}/{#####}, %v, 1) = %q, want %q", c.d, got, c.want)
		}
	}
}

// TestRender_CalendarTokens covers NAM-003.
func TestRender_CalendarTokens(t *testing.T) {
	for tpl, want := range map[string]string{
		"PO/{YYYY}/{####}": "PO/2026/0001",
		"P{YY}{MM}/{###}":  "P2604/001",
	} {
		if got := mustParse(t, tpl).render(day(2026, 4, 5), 1); got != want {
			t.Errorf("render(%q, 2026-04-05, 1) = %q, want %q", tpl, got, want)
		}
	}
}

// TestTemplate_Prefix covers NAM-019.
func TestTemplate_Prefix(t *testing.T) {
	for tpl, want := range map[string]string{
		"PO/{FY}/{#####}": "PO/26-27/",
		"P{YY}{MM}{####}": "P2604",
		"GRN{######}":     "GRN",
	} {
		if got := mustParse(t, tpl).prefix(day(2026, 4, 5)); got != want {
			t.Errorf("prefix(%q, 2026-04-05) = %q, want %q", tpl, got, want)
		}
	}
}

// Fuzz template grammar. raw[0] picks the padding (3 + raw[0]%4); each later
// byte is a date token (high bit set, low two bits pick it) or a literal
// (litChars[b&63]). The number part always comes last.
const litChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-/"

var fuzzTokens = [4]string{"{FY}", "{YYYY}", "{YY}", "{MM}"}

func genTemplate(raw []byte) string {
	digits := 3
	if len(raw) > 0 {
		digits += int(raw[0]) % 4
		raw = raw[1:]
	}
	var b strings.Builder
	for _, c := range raw {
		if c&0x80 != 0 {
			b.WriteString(fuzzTokens[c&3])
		} else {
			b.WriteByte(litChars[c&63])
		}
	}
	b.WriteString("{" + strings.Repeat("#", digits) + "}")
	return b.String()
}

// encodeTemplate is the inverse of genTemplate, for seeds. pieces are
// literal strings or the tokens in fuzzTokens; digits is 3..6.
func encodeTemplate(digits int, pieces ...string) []byte {
	raw := []byte{byte(digits - 3)} //nolint:gosec // G115: digits is 3..6
	for _, p := range pieces {
		tok := -1
		for i, ft := range fuzzTokens {
			if p == ft {
				tok = i
			}
		}
		if tok >= 0 {
			raw = append(raw, 0x80|byte(tok))
			continue
		}
		for i := 0; i < len(p); i++ {
			raw = append(raw, byte(strings.IndexByte(litChars, p[i]))) //nolint:gosec // G115: index into a short literal table
		}
	}
	return raw
}

// clampDate maps fuzz inputs to a valid date in year y (2000..2099).
func clampDate(y int, m, dd uint8) types.Date {
	month := time.Month(1 + int(m)%12)
	days := time.Date(y, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return types.Date{Year: y, Month: month, Day: 1 + int(dd)%days}
}

// FuzzRender_GSTSafe covers NAM-020.
func FuzzRender_GSTSafe(f *testing.F) {
	seeds := []struct {
		raw  []byte
		want string
	}{
		{encodeTemplate(5, "PO/", "{FY}", "/"), "PO/{FY}/{#####}"},
		{encodeTemplate(3, "{FY}", "/PO/"), "{FY}/PO/{###}"},
		{encodeTemplate(4, "P", "{YY}", "{MM}"), "P{YY}{MM}{####}"},
		{encodeTemplate(6, "GRN"), "GRN{######}"},
		{encodeTemplate(4, "PO/", "{YYYY}", "/"), "PO/{YYYY}/{####}"},
		{encodeTemplate(3, "P", "{YY}", "{MM}", "/"), "P{YY}{MM}/{###}"},
	}
	for _, s := range seeds {
		if got := genTemplate(s.raw); got != s.want {
			f.Fatalf("seed encodes %q, want %q", got, s.want)
		}
		f.Add(s.raw, uint16(26), uint8(3), uint8(0), uint32(0), uint8(0))      // 2026-04-01, n=1
		f.Add(s.raw, uint16(99), uint8(11), uint8(30), ^uint32(0), uint8(9))   // 2099-12-31, large n, -9
		f.Add(s.raw, uint16(0), uint8(0), uint8(0), uint32(999998), uint8(1))  // 2000-01-01
		f.Add(s.raw, uint16(28), uint8(1), uint8(28), uint32(99998), uint8(5)) // 2028-02-29
	}
	f.Fuzz(func(t *testing.T, raw []byte, y uint16, m, dd uint8, n uint32, k uint8) {
		tpl := genTemplate(raw)
		tp, err := parseTemplate(tpl)
		if err != nil {
			t.Skip()
		}
		d := clampDate(2000+int(y)%100, m, dd)
		c := 1 + int64(n)%tp.maxCounter()
		base := tp.render(d, c)
		if len(base) > 14 || !gstRe.MatchString(base) {
			t.Fatalf("render(%q, %v, %d) = %q: want 14 or fewer characters matching %s", tpl, d, c, base, gstRe)
		}
		if amd := int(k % 10); amd > 0 {
			s := amendmentNumber(base, amd)
			if len(s) > 16 || !gstRe.MatchString(s) {
				t.Fatalf("amendmentNumber(%q, %d) = %q: want 16 or fewer characters matching %s", base, amd, s, gstRe)
			}
		}
	})
}
