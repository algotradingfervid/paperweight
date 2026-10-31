package modkit_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/testkit"
)

// Default PO terms hold at most 2,000 characters (runes): 2,000 is saved,
// 2,001 is 422 on default_po_terms with its own message, and nothing is
// stored or audited.
func TestSettings_DefaultPOTermsMaxLen(t *testing.T) {
	const msgTooLong = "Default PO terms can be at most 2,000 characters."
	e := newEnv(t)

	tooLong := strings.Repeat("₹", 2001) // multi-byte: the limit counts runes
	err := e.kit.SaveSettings(t.Context(), testkit.Principal(t, adminEmail), 1, modkit.SettingsInput{DefaultPOTerms: tooLong})
	wantRefusal(t, "SaveSettings with 2,001 characters of terms", err, 422, msgTooLong)
	var he doctype.HTTPError
	if errors.As(err, &he) {
		fe := he.FieldErrors()
		if len(fe) != 1 || fe[0].Field != "default_po_terms" || fe[0].Message != msgTooLong {
			t.Errorf("field errors %+v, want one on default_po_terms with %q", fe, msgTooLong)
		}
	}
	wantSettings(t, "settings after the refused save", e.get(t), modkit.Settings{Version: 1})
	if n := len(e.rec.Entries()); n != 0 {
		t.Errorf("%d audit entries after the refused save, want 0", n)
	}

	full := strings.Repeat("₹", 2000)
	e.save(t, 1, modkit.SettingsInput{DefaultPOTerms: full})
	wantSettings(t, "after saving 2,000 characters", e.get(t), modkit.Settings{DefaultPOTerms: full, Version: 2})

	// A refused user hears the 403 first, whatever the terms.
	err = e.kit.SaveSettings(t.Context(), testkit.Principal(t, deepakEmail), 2, modkit.SettingsInput{DefaultPOTerms: tooLong})
	wantForbidden(t, "SaveSettings as Deepak with 2,001 characters", err)
}
