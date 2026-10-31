package app_test

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/modules/expense"
)

// CD-327 (2), CD-334: expense hides employee_id from a non-Administrator
// only as an input, on a new document or a draft, never on a listing. Each
// row is one case of the Hide predicate, for both DocTypes: a listing (no
// View, Listing set), a new page (no View), a draft, a submitted document;
// each as a non-Administrator and as the Administrator.
func TestExp_EmployeeHiddenOnlyAsInput(t *testing.T) {
	e := newExpWorld(t)
	draftClaim := e.claim(0, "", true, "100.00", "2026-10-24")
	claim := e.claim(0, "", false, "100.00", "2026-10-24")
	draftAdv := e.draftAdvance()
	adv := e.advance("900.00")
	for _, tc := range []struct {
		dt            string
		draft, posted int64
		csvRow        string // the posted record's CSV row
	}{
		{expense.ClaimDocType, draftClaim.ID, claim.ID, "EX/26-27/00001,EMP-007,2026-10-24,Pune Metro site visit,,,100.00,100.00,approved,"},
		{expense.AdvanceDocType, draftAdv.ID, adv.ID, "EA/26-27/00001,EMP-007,2026-10-20,Pune Metro site visit,900.00,approved"},
	} {
		// Listing: the CSV export names the employee for a reader with
		// export (Meena, Director) as for the Administrator.
		for _, who := range []string{meenaEmail, adminEmail} {
			r := e.signedIn(who).get("/doc/" + tc.dt + "/export.csv")
			if r.status != http.StatusOK {
				t.Fatalf("%s: %s export = %d %q", who, tc.dt, r.status, r.pageText())
			}
			recs, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix([]byte(r.body), []byte(listingBOM)))).ReadAll()
			if err != nil || len(recs) == 0 {
				t.Fatalf("%s: %s export: %v %q", who, tc.dt, err, r.body)
			}
			if !slices.Contains(recs[0], "Employee") {
				t.Errorf("%s: %s export header %q, want an Employee column", who, tc.dt, recs[0])
			}
			if !slices.ContainsFunc(recs[1:], func(row []string) bool { return strings.Join(row, ",") == tc.csvRow }) {
				t.Errorf("%s: %s export rows %q, want %q", who, tc.dt, recs[1:], tc.csvRow)
			}
		}
		// Listing: a sort or filter on the employee reaches the list
		// kernel for every user, so a non-Administrator gets the
		// Administrator's answer, never a silently dropped key.
		for _, q := range []string{"?sort=employee_id.asc", "?f=employee_id:=:7"} {
			path := "/doc/" + tc.dt + q
			want := e.signedIn(adminEmail).get(path)
			for _, who := range []string{deepakEmail, meenaEmail} {
				got := e.signedIn(who).get(path)
				if got.status != want.status || !strings.Contains(got.pageText(), "by 'employee_id'") {
					t.Errorf("%s: GET %s = %d %q, want the Administrator's %d %q", who, path, got.status, got.pageText(), want.status, want.pageText())
				}
			}
		}
		// Input pages: a non-Administrator gets no employee input on a new
		// document or a draft; the Administrator does.
		for _, page := range []string{"/doc/" + tc.dt + "/new", docPath(tc.dt, tc.draft) + "/edit/details"} {
			for who, want := range map[string]bool{deepakEmail: false, adminEmail: true} {
				r := e.signedIn(who).get(page)
				if r.status != http.StatusOK {
					t.Fatalf("%s: GET %s = %d %q", who, page, r.status, r.pageText())
				}
				if got := r.doc.Find(`[name="employee_id"]`).Length() > 0; got != want {
					t.Errorf("%s: GET %s: employee input %v, want %v", who, page, got, want)
				}
			}
		}
		// Past draft: everyone reads the claimant.
		for _, who := range []string{deepakEmail, adminEmail} {
			if got := pairValue(t, e.signedIn(who).get(docPath(tc.dt, tc.posted)), "Employee"); got != "EMP-007 Deepak Nair" {
				t.Errorf("%s: %s %d: Employee = %q, want EMP-007 Deepak Nair", who, tc.dt, tc.posted, got)
			}
		}
	}
}

// listingBOM is the UTF-8 byte order mark, written as an escape (CLAUDE.md).
const listingBOM = "\ufeff"
