package naming

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// TestTemplate_ErrorFields checks every template rule's error belongs to the
// "template" form field with status 422 (Task 1 Error interface, CD-44).
func TestTemplate_ErrorFields(t *testing.T) {
	cases := map[string]string{ // one template per rule -> message
		"PO/{FY/{###}":        msgBraces,
		"PO {FY}/{###}":       msgBadChars,
		"PO/{XYZ}/{###}":      fmt.Sprintf(msgUnknownToken, "{XYZ}"),
		"PO/{FY}/":            msgMissingNumber,
		"PO/{###}/{#####}":    msgTwoNumbers,
		"PO/{FY}/{##}":        msgPadding,
		"PO/{#####}/{FY}":     msgNumberNotLast,
		"/PO/{###}":           msgFirstChar,
		"SCPL/PO/{FY}/{####}": fmt.Sprintf(msgTooLong, 18),
	}
	for tpl, msg := range cases {
		err := new(Namer).ValidateTemplate(tpl)
		var e *Error
		if !errors.As(err, &e) {
			t.Errorf("ValidateTemplate(%q) = %v, want *Error", tpl, err)
			continue
		}
		if got := e.Status(); got != 422 {
			t.Errorf("ValidateTemplate(%q).Status() = %d, want 422", tpl, got)
		}
		want := []doctype.FieldError{{Field: "template", Message: msg}}
		if got := e.FieldErrors(); !reflect.DeepEqual(got, want) {
			t.Errorf("ValidateTemplate(%q).FieldErrors() = %+v, want %+v", tpl, got, want)
		}
	}
}

// TestTemplate_Braces covers NAM-016.
func TestTemplate_Braces(t *testing.T) {
	for name, tpl := range map[string]string{
		"unclosed":      "PO/{FY/{###}",
		"stray":         "PO/FY}/{###}",
		"nested":        "PO/{F{Y}}/{###}",
		"trailing_open": "PO/{FY}/{###}{",
	} {
		t.Run(name, func(t *testing.T) {
			err := new(Namer).ValidateTemplate(tpl)
			if err == nil {
				t.Fatalf("ValidateTemplate(%q) = nil, want %q", tpl, msgBraces)
			}
			if got := err.Error(); got != msgBraces {
				t.Errorf("ValidateTemplate(%q) message = %q, want %q", tpl, got, msgBraces)
			}
			if !errors.Is(err, ErrInvalidTemplate) {
				t.Errorf("ValidateTemplate(%q) = %v, want errors.Is(err, ErrInvalidTemplate)", tpl, err)
			}
		})
	}
}
