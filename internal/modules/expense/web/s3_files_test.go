package expenseweb

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// EUX-031 (UX-E7): a claim with 3 lines holding 2 distinct receipts and one
// file no line holds shows only that file under Other files; with no such
// file the card has no items, so docscreen draws none.
func TestS3_OtherFiles(t *testing.T) {
	t.Parallel()
	e := expensetest.New(t)
	deepak := e.P(t, "deepak@vardhan.test")
	line := expensetest.Values{"no_receipt_reason": ""}
	claim := e.DraftClaim(t, deepak, nil, line, line, line)
	r1 := e.MustAttach(t, deepak, expense.ClaimDocType, claim.ID, "bill-1.pdf", []byte("%PDF-1.4 bill one"))
	r2 := e.MustAttach(t, deepak, expense.ClaimDocType, claim.ID, "bill-2.pdf", []byte("%PDF-1.4 bill two"))
	for no, sha := range map[int]string{1: r1.SHA256, 2: r2.SHA256, 3: r1.SHA256} {
		e.E8SetReceipt(t, claim.ID, no, sha)
	}
	d := Deps{Service: e.Service, Engine: e.Engine, Auth: e.Auth, Files: e.Files}
	page := func() *docscreen.FilesCard {
		t.Helper()
		rec := e.Load(t, expense.ClaimDocType, claim.ID)
		v := &docscreen.DocView{Info: rec.Info, Record: auth.RecordRef{
			DocType: expense.ClaimDocType, ID: claim.ID, CreatedBy: rec.Info.CreatedBy, EmployeeID: rec.Info.EmployeeID,
		}}
		for i, l := range rec.Lines {
			v.Lines = append(v.Lines, docscreen.LineView{No: i + 1, Inputs: l})
		}
		for _, m := range e.FilesOf(t, expense.ClaimDocType, claim.ID) {
			v.Attachments = append(v.Attachments, components.Attachment{ID: m.ID, Name: m.Name})
		}
		req := httptest.NewRequestWithContext(context.Background(), "GET", "/", nil)
		dt, _ := doctype.Lookup(expense.ClaimDocType)
		x, err := claimPage(d)(docscreen.Ctx{Req: req, Principal: deepak, DT: dt, View: v})
		if err != nil {
			t.Fatalf("claimPage: %v", err)
		}
		if x.Files == nil || x.Files.Title != "Other files" {
			t.Fatalf("Files = %+v, want the Other files card", x.Files)
		}
		return x.Files
	}
	if f := page(); len(f.Items) != 0 {
		t.Errorf("every file is a receipt, yet Other files lists %+v", f.Items)
	}
	other := e.MustAttach(t, deepak, expense.ClaimDocType, claim.ID, "hotel-folio.pdf", []byte("%PDF-1.4 folio"))
	if f := page(); len(f.Items) != 1 || f.Items[0].ID != other.ID {
		t.Errorf("Other files = %+v, want only %s", f.Items, other.Name)
	}
}

// UX §5 intro, CD-319 (4): when the module hides the employee from the
// viewer, docscreen drops the party line, so Details names the employee;
// when the field shows, it isn't named twice.
func TestS3_EmployeePair(t *testing.T) {
	t.Parallel()
	dt, _ := doctype.Lookup(expense.ClaimDocType)
	v := &docscreen.DocView{Info: doctype.Info{Party: "Deepak Nair"}}
	for _, hidden := range []bool{true, false} {
		mods := map[string]docscreen.Module{expense.ClaimDocType: {Hide: func(_ docscreen.Ctx, f doctype.Field) bool {
			return hidden && f.Name == "employee_id"
		}}}
		got := employeePair(docscreen.Ctx{DT: dt, View: v, Deps: docscreen.Deps{Modules: mods}})
		want := 0
		if hidden {
			want = 1
		}
		if len(got) != want || (hidden && got[0] != components.Pair{Label: "Employee", Value: "Deepak Nair"}) {
			t.Errorf("hidden %v: employeePair = %+v", hidden, got)
		}
	}
	if got := employeePair(docscreen.Ctx{DT: dt, View: v}); got != nil {
		t.Errorf("no Module: employeePair = %+v, want none", got)
	}
}
