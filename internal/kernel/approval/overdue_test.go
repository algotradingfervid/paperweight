package approval_test

import (
	"slices"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/types"
)

// Reminders (spec §14.3, CD-234): "Waiting {n} days" on My requests and the
// Administrator's Overdue approvals list. Days are calendar days in IST
// between the submitted time's business date and today; both features start
// at approval.OverdueAfterDays (3).

const overdueRefusal = "You don't have permission to see overdue approvals. Ask an Administrator."

// waitingByDoc is p's My requests as document id → WaitingDays.
func waitingByDoc(t *testing.T, e *at.Env, p auth.Principal) map[int64]int {
	t.Helper()
	out := map[int64]int{}
	for _, r := range myRequests(t, e, p, types.Page{Number: 1, Size: 100}).Rows {
		out[r.DocID] = r.WaitingDays
	}
	return out
}

func wantWaiting(t *testing.T, label string, got, want map[int64]int) {
	t.Helper()
	for id, w := range want {
		if g, ok := got[id]; !ok || g != w {
			t.Errorf("%s: Draft %d WaitingDays = %d (listed %v), want %d", label, id, g, ok, w)
		}
	}
}

func TestApprovalWaiting_Days(t *testing.T) { // PO-042
	if approval.OverdueAfterDays != 3 {
		t.Fatalf("OverdueAfterDays = %d, want 3 (spec §14.3)", approval.OverdueAfterDays)
	}
	e := at.New(t)
	// 23:30 IST on 24 Oct: the UTC date and the IST date differ from here on.
	e.Clock.Set(time.Date(2026, 10, 24, 18, 0, 0, 0, time.UTC))
	docs := map[int64]at.Doc{}
	for _, id := range []int64{42, 43, 44, 45} {
		docs[id] = e.NewDoc(t, at.Order, id, at.Rs(60000), e.Priya)
		at.MustOK(t, e.Submit(e.Priya, docs[id]))
	}
	wantWaiting(t, "day 0", waitingByDoc(t, e, e.Priya), map[int64]int{42: 0, 43: 0, 44: 0, 45: 0})

	// 23:59 IST on 26 Oct: day 2, nothing shows.
	e.Clock.Set(time.Date(2026, 10, 26, 18, 29, 0, 0, time.UTC))
	wantWaiting(t, "day 2 (23:59 IST 26 Oct)", waitingByDoc(t, e, e.Priya), map[int64]int{42: 0, 43: 0, 44: 0, 45: 0})

	// 00:01 IST on 27 Oct: day 3 by the IST calendar, though only 48h31m have
	// passed and the UTC date is still 26 Oct.
	e.Clock.Set(time.Date(2026, 10, 26, 18, 31, 0, 0, time.UTC))
	wantWaiting(t, "day 3 (00:01 IST 27 Oct)", waitingByDoc(t, e, e.Priya), map[int64]int{42: 3, 43: 3, 44: 3, 45: 3})

	// A decision or a withdrawal clears it; the open one keeps counting.
	e.Clock.Set(time.Date(2026, 10, 27, 5, 0, 0, 0, time.UTC))
	at.MustOK(t, e.Approve(e.Ravi, docs[43]))
	at.MustOK(t, e.Reject(e.Ravi, docs[44], "Wrong vendor"))
	at.MustOK(t, e.Withdraw(e.Priya, docs[45]))
	wantWaiting(t, "after decisions on 27 Oct", waitingByDoc(t, e, e.Priya), map[int64]int{42: 3, 43: 0, 44: 0, 45: 0})

	e.Clock.Set(time.Date(2026, 10, 29, 5, 0, 0, 0, time.UTC))
	d46 := e.NewDoc(t, at.Order, 46, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d46))
	wantWaiting(t, "29 Oct", waitingByDoc(t, e, e.Priya), map[int64]int{42: 5, 43: 0, 44: 0, 45: 0, 46: 0})

	at.MustOK(t, e.Approve(e.Ravi, docs[42]))
	wantWaiting(t, "after Draft 42 is approved", waitingByDoc(t, e, e.Priya), map[int64]int{42: 0, 46: 0})
}

// overdueOf is p's Overdue approvals list; any error fails the test.
func overdueOf(t *testing.T, a *approval.Approvals, p auth.Principal) []approval.OverdueItem {
	t.Helper()
	items, err := a.Overdue(t.Context(), p)
	at.MustOK(t, err)
	return items
}

// overdueRow is what one OverdueItem is checked on.
type overdueRow struct {
	Title     string
	Days      int
	Approvers []string
}

func overdueRows(items []approval.OverdueItem) []overdueRow {
	var out []overdueRow
	for _, it := range items {
		out = append(out, overdueRow{it.Title, it.Days, it.Approvers})
	}
	return out
}

func wantOverdue(t *testing.T, label string, got []approval.OverdueItem, want []overdueRow) {
	t.Helper()
	if g := overdueRows(got); !slices.EqualFunc(g, want, func(a, b overdueRow) bool {
		return a.Title == b.Title && a.Days == b.Days && slices.Equal(a.Approvers, b.Approvers)
	}) {
		t.Errorf("%s: Overdue = %+v\nwant %+v", label, g, want)
	}
}

func TestApprovalOverdue_List(t *testing.T) { // PO-043
	e := at.New(t)
	// A Director whose name sorts first but whose id (1001) is last.
	e.AddUser(t, "Aarti Desai", "aarti@vardhan.test", "Director")
	submit := func(id int64, amount types.Money, when time.Time) at.Doc {
		t.Helper()
		e.Clock.Set(when)
		d := e.NewDoc(t, at.Order, id, amount, e.Priya)
		at.MustOK(t, e.Submit(e.Priya, d))
		return d
	}
	day := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, time.UTC) }

	d46 := submit(46, at.Rs(60000), day(24, 5)) // decided below
	d43 := submit(43, at.Rs(60000), day(24, 5)) // its document row is dropped below
	e.Clock.Set(day(25, 5))
	at.MustOK(t, e.Reject(e.Ravi, d46, "Wrong vendor"))
	submit(44, at.Rs(6_00_000), day(25, 5)) // Director only
	submit(45, at.Rs(60000), day(26, 5))
	// Submitted after 44 (higher request id) but older: listed before it.
	submit(42, at.Rs(60000), day(24, 6))
	submit(48, at.Rs(60000), day(24, 6)) // same time as 42, later id
	e.DropDocRow(t, d43)

	// Nothing is overdue on the day of submission.
	e.Clock.Set(day(24, 7))
	if got := overdueOf(t, e.A, e.Anil); len(got) != 0 {
		t.Errorf("Overdue on 24 Oct = %+v, want empty", overdueRows(got))
	}

	// 28 Oct: 42 and 48 are 4 days old, 44 is 3, 45 is 2 (not listed), 46 is
	// decided and 43's document can't be read.
	e.Clock.Set(day(28, 5))
	all3 := []string{"Aarti Desai", "Meena Iyer", "Ravi Kulkarni"}
	directors := []string{"Aarti Desai", "Meena Iyer"}
	got := overdueOf(t, e.A, e.Anil)
	wantOverdue(t, "28 Oct", got, []overdueRow{
		{"Draft 42", 4, all3},
		{"Draft 48", 4, all3},
		{"Draft 44", 3, directors},
	})
	if len(got) > 0 {
		if it := got[0]; it.DocType != at.Order || it.DocID != 42 || it.SubmittedByName != "Priya Shah" || it.Amount != at.Rs(60000) {
			t.Errorf("first overdue item %+v, want Draft 42 (%s 42) by Priya Shah for ₹60,000.00", it.InboxItem, at.Order)
		}
	}

	// 29 Oct: 45 reaches day 3 and joins the end of the list.
	e.Clock.Set(day(29, 5))
	wantOverdue(t, "29 Oct", overdueOf(t, e.A, e.Anil), []overdueRow{
		{"Draft 42", 5, all3},
		{"Draft 48", 5, all3},
		{"Draft 44", 4, directors},
		{"Draft 45", 3, all3},
	})

	// Approvers are the eligible ones: a disabled Director drops off; an
	// approved request leaves the list.
	e.Disable(t, e.Meena.ID)
	at.MustOK(t, e.Approve(e.Ravi, at.Doc{Type: at.Order, ID: 48}))
	wantOverdue(t, "29 Oct, Meena disabled, 48 approved", overdueOf(t, e.A, e.Anil), []overdueRow{
		{"Draft 42", 5, []string{"Aarti Desai", "Ravi Kulkarni"}},
		{"Draft 44", 4, []string{"Aarti Desai"}},
		{"Draft 45", 3, []string{"Aarti Desai", "Ravi Kulkarni"}},
	})

	// Only an Administrator may see the list (ErrNotAdmin, 403, its own text).
	for _, p := range []auth.Principal{e.Priya, e.Ravi, e.Meena} {
		items, err := e.A.Overdue(t.Context(), p)
		at.WantErr(t, err, approval.ErrNotAdmin, overdueRefusal)
		at.WantStatus(t, err, 403)
		if len(items) != 0 {
			t.Errorf("%s's refused Overdue returned %+v, want nothing", p.Name, overdueRows(items))
		}
	}

	// The same check on the real auth.Service (lessons §5, §10).
	a := auth.New(auth.Deps{
		DB: e.DB, Clock: e.Clock, Directory: e.Dir, Audit: auditfake.New(),
		Refs: &authtest.Refs{}, Info: &authtest.Info{},
	})
	principal := func(u auth.UserID) auth.Principal {
		t.Helper()
		p, _, err := a.LoadPrincipal(t.Context(), u)
		at.MustOK(t, err)
		return p
	}
	admin, priya, ravi := principal(e.Anil.ID), principal(e.Priya.ID), principal(e.Ravi.ID)
	for _, role := range []string{"Director", "Purchase Manager"} {
		at.MustOK(t, a.SetRights(t.Context(), admin, role, at.Order, map[auth.Right]auth.Scope{"read": auth.ScopeAll, "approve": auth.ScopeAll}))
	}
	onReal := approval.New(approval.Deps{DB: e.DB, Clock: e.Clock, Auth: a, Info: e.Docs, Directory: e.Dir, Audit: e.Audit})
	var titles []string
	for _, it := range overdueOf(t, onReal, admin) {
		titles = append(titles, it.Title)
	}
	if want := []string{"Draft 42", "Draft 44", "Draft 45"}; !slices.Equal(titles, want) {
		t.Errorf("real auth: Administrator's Overdue titles = %q, want %q", titles, want)
	}
	for _, p := range []auth.Principal{priya, ravi} {
		items, err := onReal.Overdue(t.Context(), p)
		at.WantErr(t, err, approval.ErrNotAdmin, overdueRefusal)
		at.WantStatus(t, err, 403)
		if len(items) != 0 {
			t.Errorf("real auth: %s's refused Overdue returned %+v, want nothing", p.Name, overdueRows(items))
		}
	}
}
