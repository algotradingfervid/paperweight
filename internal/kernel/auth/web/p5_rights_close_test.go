package authweb_test

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// MOD-PO P5, CD-289 (1) (follows CD-280 D2, CD-253, CD-255): on the role
// rights page, approve on a DocType that isn't approvable and whose
// CloseRight is approve only closes, so its line reads `Close {plural
// label}.` with the plural label in lower case: `Close purchase requests.`
// for purchase.Request. Approvable DocTypes keep `Approve or reject
// requests.`, also when their CloseRight is approve.
const (
	p5Request         = "p5.Request" // as purchase.Request: Submittable, not Approvable, CloseRight approve
	p5Indent          = "p5.Indent"  // the same rule under another plural label
	p5ApprovableClose = "p5.ApprovableClose"

	p5ApproveMeaning = "Approve or reject requests."
)

// p5Register adds the P5 DocTypes once (the registry is global). Call it
// before authtest.NewWorld.
func p5Register() {
	for _, dt := range []doctype.DocType{
		{Name: p5Request, Label: "Purchase Request", PluralLabel: "Purchase Requests", Submittable: true, CloseRight: "approve"},
		{Name: p5Indent, Label: "Site Indent", PluralLabel: "Site Indents", Submittable: true, CloseRight: "approve"},
		{Name: p5ApprovableClose, Label: "Close Order", PluralLabel: "Close Orders", Submittable: true, Approvable: true, CloseRight: "approve"},
	} {
		if _, ok := doctype.Lookup(dt.Name); !ok {
			doctype.Register(dt)
		}
	}
}

func TestRightsPage_ApproveMeaningForCloseRight(t *testing.T) {
	p5Register()
	w, _, admin := p6World(t)
	for _, tc := range []struct {
		dt, want, notWant string
	}{
		{p5Request, "Close purchase requests.", p5ApproveMeaning},
		{p5Indent, "Close site indents.", p5ApproveMeaning},
		{authtest.DTOrder, p5ApproveMeaning, "Close "},
		{p6Full, p5ApproveMeaning, "Close "},
		{p5ApprovableClose, p5ApproveMeaning, "Close "},
	} {
		if !auth.Applies(p6Lookup(t, tc.dt), auth.RightApprove) {
			t.Fatalf("%s: approve doesn't apply, so the page has no approve row (M0-24)", tc.dt)
		}
		_, form := p6Open(t, w, admin, p6Stores, tc.dt)
		row := p1Text(p6Row(t, form, auth.RightApprove))
		if !strings.Contains(row, tc.want) {
			t.Errorf("%s: approve row %q lacks %q", tc.dt, row, tc.want)
		}
		if strings.Contains(row, tc.notWant) {
			t.Errorf("%s: approve row %q has %q", tc.dt, row, tc.notWant)
		}
	}
}
