package expenseweb

import (
	"bytes"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// s3HideCtx is a page Ctx for record id of dt whose registered Hide is the
// module's own (own) or-ed with hiding the fields in also, as the redraw
// sweep registers it.
func s3HideCtx(t *testing.T, e *expensetest.Env, p auth.Principal, dt string, id int64,
	own func(docscreen.Ctx, doctype.Field) bool, also ...string) docscreen.Ctx {
	t.Helper()
	typ, _ := doctype.Lookup(dt)
	rec := e.Load(t, dt, id)
	v := &docscreen.DocView{Info: rec.Info, Display: map[string]string{}, Inputs: rec.Values, Record: auth.RecordRef{
		DocType: dt, ID: id, CreatedBy: rec.Info.CreatedBy, EmployeeID: rec.Info.EmployeeID,
	}}
	hide := func(c docscreen.Ctx, f doctype.Field) bool { return slices.Contains(also, f.Name) || own(c, f) }
	return docscreen.Ctx{
		Req: httptest.NewRequest("GET", "/", nil), Principal: p, DT: typ, View: v,
		Deps: docscreen.Deps{DB: e.DB, Engine: e.Engine, Modules: map[string]docscreen.Module{dt: {Hide: hide}}},
	}
}

// labels are the pairs' labels.
func labels(ps []components.Pair) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Label)
	}
	return out
}

// CD-319, CD-233: the advance page draws Paid on, Used and Left itself, from
// the stored record. With the module's own Hide it draws all three (UX
// §5.11); when the registered Hide also hides used_amount, neither Used nor
// what reveals it (Left, the Other claims figure, the close note) is drawn;
// when it hides paid_on, Paid on isn't.
func TestS3_AdvancePairsHonourHide(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, "deepak@vardhan.test")
	d := Deps{Service: e.Service, Engine: e.Engine, Auth: e.Auth, Files: e.Files}
	adv := e.PaidAdvance(t, deepak, 1000000)
	e.ApprovedClaim(t, deepak, expensetest.Values{"advance_id": strconv.FormatInt(adv.ID, 10), "less_advance": "100.00"})
	// Deepak can't read the claim: it shows only as Other claims' figure.
	e.S4RevokeRead(t, "deepak@vardhan.test", expense.ClaimDocType)
	deepak = e.P(t, "deepak@vardhan.test")
	for _, tc := range []struct {
		hide             []string
		pairs            []string
		other, closeNote bool
	}{
		{nil, []string{"Paid on", "Used", "Left"}, true, true},
		{[]string{"used_amount"}, []string{"Paid on"}, false, false},
		{[]string{"paid_on"}, []string{"Used", "Left"}, true, true},
	} {
		c := s3HideCtx(t, e, deepak, expense.AdvanceDocType, adv.ID, advanceHide(d), tc.hide...)
		x, err := advanceExtra(c, d)
		if err != nil {
			t.Fatalf("hide %v: advanceExtra: %v", tc.hide, err)
		}
		if got := labels(x.Pairs); !slices.Equal(got, tc.pairs) {
			t.Errorf("hide %v: pairs %v, want %v", tc.hide, got, tc.pairs)
		}
		var buf bytes.Buffer
		if len(x.Cards) != 1 {
			t.Fatalf("hide %v: %d cards, want the claims card", tc.hide, len(x.Cards))
		}
		if err := x.Cards[0].Render(t.Context(), &buf); err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(buf.String(), claimsOther); got != tc.other {
			t.Errorf("hide %v: Other claims drawn = %v, want %v", tc.hide, got, tc.other)
		}
		r, err := advanceReason(d)(c, doctype.ActionClose)
		if err != nil {
			t.Fatalf("hide %v: advanceReason: %v", tc.hide, err)
		}
		if got := r.Note != ""; got != tc.closeNote {
			t.Errorf("hide %v: close note %q, want shown %v", tc.hide, r.Note, tc.closeNote)
		}
	}
}

// The claim page's Period pair comes from period_from and period_to, which
// the module hides only because the pair shows them: hidden for any other
// reason, the pair isn't drawn (CD-319).
func TestS3_ClaimPeriodHonoursHide(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, "deepak@vardhan.test")
	d := Deps{Service: e.Service, Engine: e.Engine, Auth: e.Auth, Files: e.Files}
	claim := e.DraftClaim(t, deepak, nil)
	for _, tc := range []struct {
		hide   []string
		period bool
	}{{nil, true}, {[]string{"period_from"}, false}, {[]string{"period_to"}, false}} {
		c := s3HideCtx(t, e, deepak, expense.ClaimDocType, claim.ID, claimHide(d), tc.hide...)
		c.View.Display["period_from"], c.View.Display["period_to"] = "20-10-2026", "24-10-2026"
		x, err := claimPage(d)(c)
		if err != nil {
			t.Fatalf("hide %v: claimPage: %v", tc.hide, err)
		}
		if got := slices.Contains(labels(x.Pairs), "Period"); got != tc.period {
			t.Errorf("hide %v: Period drawn = %v, want %v", tc.hide, got, tc.period)
		}
	}
}

// The claim check page's totals show the total, what the advance takes and
// the net: none is drawn when the registered Hide hides any field they come
// from (CD-319).
func TestS1_CheckTotalsHonourHide(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, "deepak@vardhan.test")
	d := Deps{Service: e.Service, Engine: e.Engine, Auth: e.Auth, Files: e.Files}
	adv := e.PaidAdvance(t, deepak, 1000000)
	claim := e.DraftClaim(t, deepak, expensetest.Values{"advance_id": strconv.FormatInt(adv.ID, 10), "less_advance": "100.00"})
	for _, hidden := range []string{"", "advance_id", "total", "less_advance", "net_payable"} {
		c := s3HideCtx(t, e, deepak, expense.ClaimDocType, claim.ID, claimHide(d), hidden)
		ex, err := claimCheck(d)(c)
		if err != nil {
			t.Fatalf("hide %q: claimCheck: %v", hidden, err)
		}
		if got := len(ex.Totals) > 0; got != (hidden == "") {
			t.Errorf("hide %q: totals %v, want drawn %v", hidden, labels(ex.Totals), hidden == "")
		}
	}
	// A blank total or net (not yet computed) draws no totals either.
	for _, blank := range []string{"total", "net_payable"} {
		c := s3HideCtx(t, e, deepak, expense.ClaimDocType, claim.ID, claimHide(d))
		c.View.Inputs[blank] = ""
		ex, err := claimCheck(d)(c)
		if err != nil {
			t.Fatalf("%s blank: claimCheck: %v", blank, err)
		}
		if len(ex.Totals) > 0 {
			t.Errorf("%s blank: totals %v, want none", blank, labels(ex.Totals))
		}
	}
}

// The close note shows from ₹0.01 left; with nothing left, the claims' hint
// alone (EXP-059).
func TestS3_CloseNoteBoundary(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, "deepak@vardhan.test")
	d := Deps{Service: e.Service, Engine: e.Engine, Auth: e.Auth, Files: e.Files}
	for _, tc := range []struct {
		less string
		note bool
	}{{"1000.00", false}, {"999.99", true}} {
		adv := e.PaidAdvance(t, deepak, 100000)
		e.ApprovedClaim(t, deepak, expensetest.Values{"advance_id": strconv.FormatInt(adv.ID, 10), "less_advance": tc.less})
		c := s3HideCtx(t, e, deepak, expense.AdvanceDocType, adv.ID, advanceHide(d))
		r, err := advanceReason(d)(c, doctype.ActionClose)
		if err != nil {
			t.Fatalf("less %s: advanceReason: %v", tc.less, err)
		}
		if got := r.Note != ""; got != tc.note {
			t.Errorf("less %s: close note %q, want shown %v", tc.less, r.Note, tc.note)
		}
	}
}
