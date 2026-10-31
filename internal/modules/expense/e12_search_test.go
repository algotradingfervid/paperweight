package expense_test

import (
	"context"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
)

// E12 · Claims and advances declare SearchFields number and purpose
// (CD-317): the list's q finds a record by its number and by a purpose
// word, and never lists a record the viewer can't read. Through the kernel
// list service on the real auth.Service.

// e12Approved saves a dt for owner with purpose, submits it as owner and
// approves it as approver, so it has a number.
func e12Approved(t *testing.T, e *expensetest.Env, owner, approver auth.Principal, dt, purpose string) doc.Result {
	t.Helper()
	header := expensetest.Values{"purpose": purpose}
	var res doc.Result
	if dt == expense.AdvanceDocType {
		res = e.DraftAdvance(t, owner, header)
	} else {
		res = e.DraftClaim(t, owner, header)
	}
	res = e5MustSubmit(t, e, owner, dt, res)
	res = e5Must(t, e, approver, dt, res.ID, doc.ActionApprove, doc.ActionInput{Version: res.Version})
	if res.State != doc.StateApproved {
		t.Fatalf("approve %s %d: state %s, want approved", dt, res.ID, res.State)
	}
	return res
}

// e12Search is p's list of dt for q through the real list service, ids sorted.
func e12Search(t *testing.T, e *expensetest.Env, p auth.Principal, dt, q string) []int64 {
	t.Helper()
	typ, ok := doctype.Lookup(dt)
	if !ok {
		t.Fatalf("%s not registered", dt)
	}
	res, err := (&list.Service{DB: e.DB, Auth: e.Auth}).List(context.Background(), p, typ,
		list.Query{Search: q, Page: 1, PageSize: 500})
	if err != nil {
		t.Fatalf("list %s as %s, q=%q: %v", dt, p.Name, q, err)
	}
	ids := []int64{}
	for _, r := range res.Rows {
		ids = append(ids, r.ID)
	}
	slices.Sort(ids)
	return ids
}

func e12WantSearch(t *testing.T, e *expensetest.Env, p auth.Principal, dt, q string, ids ...int64) {
	t.Helper()
	want := slices.Clone(ids)
	slices.Sort(want)
	if got := e12Search(t, e, p, dt, q); !slices.Equal(got, want) {
		t.Errorf("%s's %s list, q=%q = %v, want %v", p.Name, dt, q, got, want)
	}
}

// Deepak (Priya's report) and Kavya (Meena's report, outside Priya's team)
// each have an approved record with "Chennai" in its purpose; Deepak has a
// third with another purpose. Priya finds Deepak's by its number and by
// "chennai", never Kavya's, not even by Kavya's number; Meena, who reads
// every record, finds both by "chennai".
func TestE12_SearchNumberAndPurpose(t *testing.T) {
	for _, dt := range []string{expense.ClaimDocType, expense.AdvanceDocType} {
		t.Run(dt, func(t *testing.T) {
			e := expensetest.New(t)
			deepak, priya, kavya, meena := e.P(t, e5Deepak), e.P(t, e5Priya), e.P(t, e5Kavya), e.P(t, e5Meena)
			his := e12Approved(t, e, deepak, priya, dt, "Chennai vendor audit")
			theirs := e12Approved(t, e, kavya, meena, dt, "Chennai client meeting")
			other := e12Approved(t, e, deepak, priya, dt, "Pune Metro site visit")
			hisNo, theirNo := e4Number(t, e, dt, his.ID), e4Number(t, e, dt, theirs.ID)
			if hisNo == "" || theirNo == "" {
				t.Fatalf("setup: numbers %q and %q, want both set", hisNo, theirNo)
			}
			e5WantList(t, e, priya, dt, his.ID, other.ID) // Kavya's is outside Priya's read scope

			e12WantSearch(t, e, priya, dt, hisNo, his.ID)
			e12WantSearch(t, e, priya, dt, "chennai", his.ID)
			e12WantSearch(t, e, priya, dt, theirNo)
			e12WantSearch(t, e, meena, dt, "chennai", his.ID, theirs.ID)
			e12WantSearch(t, e, meena, dt, theirNo, theirs.ID)
		})
	}
}
