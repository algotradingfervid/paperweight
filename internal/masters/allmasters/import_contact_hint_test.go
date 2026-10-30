package allmasters_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/masters/address"
)

// csvio finds the cell by the FieldError's Field, so the contact's own
// emails and phones fields must come back as the Email and Mobile cells.
func TestImport_ContactExcelHint(t *testing.T) {
	const hint = " Excel may have changed this value. Format the column as Text and retype it."
	cases := []struct {
		name, row, message string
	}{
		{"mobile", "SHREE,Vijay,,Nair,,9.82E+09,No", "Enter a 10-digit Indian mobile number starting 6–9, optionally with +91."},
		{"email", "SHREE,Anita,,Rao,1.5E+3,,No", "Enter a valid email address, e.g. accounts@shreesteel.test."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := partiesSetup(t)
			w.createVendor(t, "SHREE")
			rep := w.importFile(t, address.ContactDocType, partiesCSV(partiesContactHeader, c.row))
			partiesWantErrors(t, rep, "Row 2: "+c.message+hint)
			partiesWantNotImported(t, rep, 1, 0)
		})
	}
}
