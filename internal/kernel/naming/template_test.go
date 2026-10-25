package naming

import (
	"errors"
	"fmt"
	"testing"
)

// TestTemplate_Validate covers NAM-009..015, NAM-017, NAM-018 and Review Focus 3/4.
func TestTemplate_Validate(t *testing.T) {
	groups := []struct {
		name  string
		cases map[string]string // template -> want error ("" = valid)
	}{
		{"missing_number", map[string]string{
			"PO/{FY}/": msgMissingNumber,
			"":         msgMissingNumber, // Review Focus 4
		}},
		{"two_numbers", map[string]string{
			"PO/{###}/{#####}": msgTwoNumbers,
		}},
		{"number_not_last", map[string]string{
			"PO/{#####}/{FY}":       msgNumberNotLast,
			"TEST1-{#####}-2021-22": msgNumberNotLast,
		}},
		{"padding_bounds", map[string]string{
			"PO/{FY}/{##}":  msgPadding,
			"PO/{#######}":  msgPadding,
			"PO/{FY}/{###}": "",
			"PO{######}":    "",
		}},
		{"unknown_token", map[string]string{
			"PO/{XYZ}/{###}":      "Unknown part {XYZ} in series. Use {FY}, {YYYY}, {YY}, {MM} or a number part like {#####}.",
			"PO/{DD}/{###}":       "Unknown part {DD} in series. Use {FY}, {YYYY}, {YY}, {MM} or a number part like {#####}.",
			"PO/{WW}/{###}":       "Unknown part {WW} in series. Use {FY}, {YYYY}, {YY}, {MM} or a number part like {#####}.",
			"PO/{supplier}/{###}": "Unknown part {supplier} in series. Use {FY}, {YYYY}, {YY}, {MM} or a number part like {#####}.",
		}},
		{"lowercase_token", map[string]string{ // Review Focus 3
			"PO/{fy}/{###}": "Unknown part {fy} in series. Use {FY}, {YYYY}, {YY}, {MM} or a number part like {#####}.",
		}},
		{"bad_characters", map[string]string{
			"$INV/{#####}":        msgBadChars,
			`WINDOWS\NAMING{###}`: msgBadChars,
			"PO {FY}/{###}":       msgBadChars,
			"PO.{FY}.{###}":       msgBadChars,
			"PO_{###}":            msgBadChars,
			"PO<>{###}":           msgBadChars,
			"ПО/{###}":            msgBadChars,
		}},
		{"first_char", map[string]string{
			"/PO/{###}":     msgFirstChar,
			"-PO{###}":      msgFirstChar,
			"{FY}/PO/{###}": "",
		}},
		{"max_length", map[string]string{
			"PO/{FY}/{#####}":     "",
			"POR/{FY}/{#####}":    fmt.Sprintf(msgTooLong, 15),
			"SCPL/PO/{FY}/{####}": fmt.Sprintf(msgTooLong, 18),
		}},
		{"valid", map[string]string{
			"PO/{FY}/{#####}":   "",
			"PO-{FY}-{#####}":   "",
			"{FY}/PO/{###}":     "",
			"P{YY}{MM}{####}":   "",
			"GRN{######}":       "",
			"EXP/{YYYY}/{####}": "",
		}},
	}
	for _, g := range groups {
		t.Run(g.name, func(t *testing.T) {
			for tpl, want := range g.cases {
				err := new(Namer).ValidateTemplate(tpl)
				if want == "" {
					if err != nil {
						t.Errorf("ValidateTemplate(%q) = %v, want nil", tpl, err)
					}
					continue
				}
				if err == nil {
					t.Errorf("ValidateTemplate(%q) = nil, want %q", tpl, want)
					continue
				}
				if got := err.Error(); got != want {
					t.Errorf("ValidateTemplate(%q) message = %q, want %q", tpl, got, want)
				}
				if !errors.Is(err, ErrInvalidTemplate) {
					t.Errorf("ValidateTemplate(%q) = %v, want errors.Is(err, ErrInvalidTemplate)", tpl, err)
				}
			}
		})
	}
}
