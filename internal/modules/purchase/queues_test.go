package purchase_test

import (
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// UX §5.2 drafts: rejected first, then newest; meta vendor, "{order date} ·
// {project}", and "Needs changes" on the rejected one.
func TestPurchaseHome_DraftsQueue(t *testing.T) {
	env := purchasetest.New(t)
	small := p3SmallLine(t, env)
	rejected := p14Metro(t, env, env.Priya, p2RefLines(t, env), doctype.StatePending, nil)
	p2Must(t, "Ravi rejects")(p2Do(t, env, env.Ravi, rejected, doc.ActionReject, "Price too high"))
	older := p14Metro(t, env, env.Priya, small, doctype.StateDraft, nil)
	env.SetNow(t, p14At(time.October, 25, 11, 0))
	newer := p14Metro(t, env, env.Priya, small, doctype.StateDraft, nil)
	p14Metro(t, env, env.Arjun, small, doctype.StateDraft, nil) // not Priya's

	p14WantRows(t, "Priya's drafts", p14QueueOf(t, env, env.Priya, purchase.QueueDrafts, 1), []p14Row{
		p14OrderRow(t, env, rejected, doctype.StateRejected, p14Shree, "24-10-2026 · PRJ-METRO", "Needs changes"),
		p14OrderRow(t, env, newer, doctype.StateDraft, p14Shree, "25-10-2026 · PRJ-METRO"),
		p14OrderRow(t, env, older, doctype.StateDraft, p14Shree, "24-10-2026 · PRJ-METRO"),
	})
	// The card counts all three; NeedsChanges is only the rejected one.
	if c, _ := p14Card(p14HomeOf(t, env, env.Priya), purchase.QueueDrafts); c.Count != 3 || c.NeedsChanges != 1 {
		t.Errorf("My drafts card = count %d, NeedsChanges %d; want 3, 1 (two drafts, one rejected)", c.Count, c.NeedsChanges)
	}
}

// UX §5.2 waiting: oldest submitted first; meta vendor, APR's status line,
// and "Waiting {n} days" from Approvals.WaitingDays (CD-280 D4): 7 and 3
// days show it, 1 day doesn't (P14 never copies the 3-day rule).
func TestPurchaseHome_WaitingQueue(t *testing.T) {
	env := purchasetest.New(t)
	big := p2RefLines(t, env)
	three := p14Metro(t, env, env.Priya, big, doctype.StateDraft, nil)
	one := p14Metro(t, env, env.Priya, big, doctype.StateDraft, nil)
	seven := p14Metro(t, env, env.Priya, big, doctype.StatePending, nil) // submitted 24-10
	env.SetNow(t, p14At(time.October, 28, 9, 0))
	p2Must(t, "submit")(p2Do(t, env, env.Priya, three, doc.ActionSubmit, ""))
	env.SetNow(t, p14At(time.October, 30, 9, 0))
	p2Must(t, "submit")(p2Do(t, env, env.Priya, one, doc.ActionSubmit, ""))
	env.SetNow(t, p14At(time.October, 31, 10, 30))

	line := p14StatusLine(t, env, seven)
	if !strings.HasPrefix(line, "Waiting for approval from ") {
		t.Fatalf("APR's status line = %q, want Waiting for approval from …", line)
	}
	p14WantRows(t, "Priya's waiting", p14QueueOf(t, env, env.Priya, purchase.QueueWaiting, 1), []p14Row{
		p14OrderRow(t, env, seven, doctype.StatePending, p14Shree, line, "Waiting 7 days"),
		p14OrderRow(t, env, three, doctype.StatePending, p14Shree, p14StatusLine(t, env, three), "Waiting 3 days"),
		p14OrderRow(t, env, one, doctype.StatePending, p14Shree, p14StatusLine(t, env, one)),
	})
	if c, _ := p14Card(p14HomeOf(t, env, env.Priya), purchase.QueueWaiting); c.Count != 3 || c.WaitingDays != 7 {
		t.Errorf("Waiting for approval card = %+v, want count 3, oldest 7 days", c)
	}
}

// UX §5.2 to-send: oldest approved first; meta vendor, "Approved on {date}",
// and "Raised by {owner}" in an approver's view only. Priya sees her own;
// Ravi (approve) sees all. Marking one sent drops it (spec §23).
func TestPurchaseHome_ToSendQueue(t *testing.T) {
	env := purchasetest.New(t)
	small := p3SmallLine(t, env)
	late := p14Metro(t, env, env.Priya, p2RefLines(t, env), doctype.StateDraft, nil) // lowest id, approved last
	first := p14Metro(t, env, env.Priya, small, doctype.StateApproved, nil)          // approved on submit, 24-10
	env.SetNow(t, p14At(time.October, 25, 11, 0))
	arjun := p14Metro(t, env, env.Arjun, small, doctype.StateApproved, nil)
	env.SetNow(t, p14At(time.October, 26, 11, 0))
	p2Must(t, "submit")(p2Do(t, env, env.Priya, late, doc.ActionSubmit, ""))
	p2Must(t, "Ravi approves")(p2Do(t, env, env.Ravi, late, doc.ActionApprove, ""))
	// Approved, never sent, then closed or cancelled: not approved any more,
	// so neither is to send (spec §23, §25).
	closed := p14Metro(t, env, env.Priya, small, doctype.StateApproved, nil)
	p2Must(t, "Priya closes")(p2Do(t, env, env.Priya, closed, doc.ActionClose, p14CloseReason))
	cancelled := p14Metro(t, env, env.Priya, small, doctype.StateApproved, nil)
	p2Must(t, "Ravi cancels")(p2Do(t, env, env.Ravi, cancelled, doc.ActionCancel, "Vendor can't supply"))
	for name, p := range map[string]auth.Principal{"Priya": env.Priya, "Ravi": env.Ravi} {
		want := map[string]int{"Priya": 2, "Ravi": 3}[name]
		if c, _ := p14Card(p14HomeOf(t, env, p), purchase.QueueToSend); c.Count != want {
			t.Errorf("%s's To send card = %d, want %d (not the closed or cancelled PO)", name, c.Count, want)
		}
	}

	p14WantRows(t, "Ravi's to-send", p14QueueOf(t, env, env.Ravi, purchase.QueueToSend, 1), []p14Row{
		p14OrderRow(t, env, first, doctype.StateApproved, p14Shree, "Approved on 24-10-2026", "Raised by Priya Shah"),
		p14OrderRow(t, env, arjun, doctype.StateApproved, p14Shree, "Approved on 25-10-2026", "Raised by Arjun Mehta"),
		p14OrderRow(t, env, late, doctype.StateApproved, p14Shree, "Approved on 26-10-2026", "Raised by Priya Shah"),
	})
	p14WantRows(t, "Priya's to-send", p14QueueOf(t, env, env.Priya, purchase.QueueToSend, 1), []p14Row{
		p14OrderRow(t, env, first, doctype.StateApproved, p14Shree, "Approved on 24-10-2026"),
		p14OrderRow(t, env, late, doctype.StateApproved, p14Shree, "Approved on 26-10-2026"),
	})
	p14Sent(t, env, env.Priya, first)
	if got := p14IDs(p14QueueOf(t, env, env.Priya, purchase.QueueToSend, 1)); len(got) != 1 || got[0] != late {
		t.Errorf("Priya's to-send after marking one sent = %v, want [%d]", got, late)
	}
}

// UX §5.2 awaiting-delivery and spec §27.1: earliest due date first, the due
// date being Expected on when set, else required by, POs with no due date
// last (AD-13 ruling, as P9 and P6); meta vendor, receipt status, "Expected
// on {date}" or "Required by {date}", and "Overdue" when the due date is
// before today (due yesterday is overdue, due today isn't). Only approved POs
// are listed: a fully received one and a closed one not fully received
// aren't.
func TestPurchaseHome_AwaitingDeliveryQueue(t *testing.T) {
	env := purchasetest.New(t)
	small := p3SmallLine(t, env)
	req := func(on string) map[string]string { return map[string]string{"required_by": on} }
	pastRequired := p14Metro(t, env, env.Priya, small, doctype.StateApproved, req("2026-10-28"))
	expectedEarly := p14Metro(t, env, env.Priya, small, doctype.StateApproved, req("2026-11-10"))
	expectedLate := p14Metro(t, env, env.Priya, small, doctype.StateApproved, req("2026-10-28"))
	part := p14Metro(t, env, env.Priya, small, doctype.StateApproved, req("2026-11-06"))
	noDue := p14Metro(t, env, env.Priya, small, doctype.StateApproved, nil)
	full := p14Metro(t, env, env.Priya, small, doctype.StateApproved, req("2026-10-28"))
	yesterday := p14Metro(t, env, env.Priya, small, doctype.StateApproved, req("2026-10-30"))
	today := p14Metro(t, env, env.Priya, small, doctype.StateApproved, req("2026-10-31"))
	closed := p14Metro(t, env, env.Priya, small, doctype.StateApproved, req("2026-10-29"))
	for id, on := range map[int64]string{expectedEarly: "27-10-2026", expectedLate: "05-11-2026"} {
		d, err := purchase.ParseExpectedOn(on)
		if err != nil {
			t.Fatalf("ParseExpectedOn(%s): %v", on, err)
		}
		if err := env.Service.SetExpected(t.Context(), env.Priya, id, 0, d, ""); err != nil {
			t.Fatalf("SetExpected(%d, %s): %v", id, on, err)
		}
	}
	p14Receive(t, env, part, "40")
	p14Receive(t, env, full, "100")
	p14Receive(t, env, closed, "40")
	p2Must(t, "Priya closes the part-received PO")(p2Do(t, env, env.Priya, closed, doc.ActionClose, p14CloseReason))
	env.SetNow(t, p14At(time.October, 31, 10, 30))

	p14WantRows(t, "awaiting-delivery", p14QueueOf(t, env, env.Priya, purchase.QueueAwaitingDelivery, 1), []p14Row{
		p14OrderRow(t, env, expectedEarly, doctype.StateApproved, p14Shree, "Not received", "Expected on 27-10-2026", "Overdue"),
		p14OrderRow(t, env, pastRequired, doctype.StateApproved, p14Shree, "Not received", "Required by 28-10-2026", "Overdue"),
		p14OrderRow(t, env, yesterday, doctype.StateApproved, p14Shree, "Not received", "Required by 30-10-2026", "Overdue"),
		p14OrderRow(t, env, today, doctype.StateApproved, p14Shree, "Not received", "Required by 31-10-2026"),
		p14OrderRow(t, env, expectedLate, doctype.StateApproved, p14Shree, "Not received", "Expected on 05-11-2026"),
		p14OrderRow(t, env, part, doctype.StateApproved, p14Shree, "Part received", "Required by 06-11-2026"),
		p14OrderRow(t, env, noDue, doctype.StateApproved, p14Shree, "Not received"),
	})
	if c, _ := p14Card(p14HomeOf(t, env, env.Priya), purchase.QueueAwaitingDelivery); c.Count != 7 {
		t.Errorf("Awaiting delivery card = %d, want 7 (not the fully received or the closed PO)", c.Count)
	}
}

// UX §5.2 requests-to-order and spec §25: submitted requests with pending to
// order, oldest needed-by first, those with none last (AD-13 ruling, as P9
// and P6); meta requester, "{site} · {needed by}" (the site alone without
// one) and "{k} rows to order" (singular for 1). Drafts aren't listed.
func TestPurchaseHome_RequestsToOrderQueue(t *testing.T) {
	env := purchasetest.New(t)
	two := []map[string]string{p5Line(t, env, "MS-ANGLE-50", "KGS", "100"), p5Line(t, env, "WIRE-18G", "KGS", "50")}
	later := p14Request(t, env, env.Suresh, "METRO-SITE", "PRJ-METRO", two, true, map[string]string{"required_by": "2026-11-05"})
	sooner := p14Request(t, env, env.Suresh, "PUNE-WH", "PRJ-METRO", nil, true, map[string]string{"required_by": "2026-10-30"})
	none := p14Request(t, env, env.Suresh, "METRO-SITE", "PRJ-METRO", nil, true, nil)
	p14Request(t, env, env.Suresh, "METRO-SITE", "PRJ-METRO", nil, false, nil) // a draft

	p14WantRows(t, "requests-to-order", p14QueueOf(t, env, env.Priya, purchase.QueueRequestsToOrder, 1), []p14Row{
		p14RequestRow(t, env, sooner, doctype.StateApproved, "Suresh Patil", "PUNE-WH · 30-10-2026", "1 row to order"),
		p14RequestRow(t, env, later, doctype.StateApproved, "Suresh Patil", "METRO-SITE · 05-11-2026", "2 rows to order"),
		p14RequestRow(t, env, none, doctype.StateApproved, "Suresh Patil", "METRO-SITE", "1 row to order"),
	})
}

// UX §5.2 my-requests: my open requests (draft or submitted; not closed or
// cancelled), newest first; meta "{site} · {needed by}" and the order status.
// Before any PO links them, the status is Not ordered.
func TestPurchaseHome_MyRequestsQueue(t *testing.T) {
	env := purchasetest.New(t)
	submitted := p14Request(t, env, env.Suresh, "METRO-SITE", "PRJ-METRO", nil, true, map[string]string{"required_by": "2026-11-05"})
	closed := p14Request(t, env, env.Suresh, "METRO-SITE", "PRJ-METRO", nil, true, nil)
	p5Must(t, "close")(p5Do(t, env, env.Suresh, closed, doctype.ActionClose, p5CloseReason))
	env.SetNow(t, p14At(time.October, 25, 11, 0))
	draft := p14Request(t, env, env.Suresh, "METRO-SITE", "PRJ-METRO", nil, false, nil)
	p14Request(t, env, env.Deepak, "TECH-SITE", "PRJ-TECHPARK", nil, true, nil) // not Suresh's

	p14WantRows(t, "Suresh's my-requests", p14QueueOf(t, env, env.Suresh, purchase.QueueMyRequests, 1), []p14Row{
		p14RequestRow(t, env, draft, doctype.StateDraft, "METRO-SITE", "Not ordered"),
		p14RequestRow(t, env, submitted, doctype.StateApproved, "METRO-SITE · 05-11-2026", "Not ordered"),
	})
}

// UX §5.2 my-requests is the viewer's own requests even when Auth.Scope lets
// them see every request: the Administrator (unrestricted, create on
// purchase.Request) lists and counts only theirs while Suresh has one open.
func TestPurchaseHome_MyRequestsOwnOnly(t *testing.T) {
	env := purchasetest.New(t)
	suresh := p14Request(t, env, env.Suresh, "METRO-SITE", "PRJ-METRO", nil, true, nil)
	mine := p14Request(t, env, env.Admin, "METRO-SITE", "PRJ-METRO", nil, false, nil)

	p14WantRows(t, "Administrator's my-requests", p14QueueOf(t, env, env.Admin, purchase.QueueMyRequests, 1), []p14Row{
		p14RequestRow(t, env, mine, doctype.StateDraft, "METRO-SITE", "Not ordered"),
	})
	if c, _ := p14Card(p14HomeOf(t, env, env.Admin), purchase.QueueMyRequests); c.Count != 1 {
		t.Errorf("Administrator's My requests = %d, want 1 (not Suresh's)", c.Count)
	}
	if got := p14IDs(p14QueueOf(t, env, env.Suresh, purchase.QueueMyRequests, 1)); len(got) != 1 || got[0] != suresh {
		t.Errorf("Suresh's my-requests = %v, want [%d]", got, suresh)
	}
}

// UX §5.2 pagination: 25 rows a page; Total counts every page.
func TestPurchaseHome_QueuePages(t *testing.T) {
	env := purchasetest.New(t)
	var ids []int64
	for range purchase.QueuePageSize + 1 {
		ids = append(ids, p14Request(t, env, env.Deepak, "TECH-SITE", "PRJ-TECHPARK", nil, false, nil))
	}
	p1 := p14QueueOf(t, env, env.Deepak, purchase.QueueMyRequests, 1)
	p2 := p14QueueOf(t, env, env.Deepak, purchase.QueueMyRequests, 2)
	if p1.Total != 26 || len(p1.Rows) != 25 || p1.Rows[0].ID != ids[25] {
		t.Errorf("page 1 = total %d, %d rows, first %v; want 26, 25, newest %d", p1.Total, len(p1.Rows), p14IDs(p1)[:1], ids[25])
	}
	if p2.Total != 26 || len(p2.Rows) != 1 || p2.Rows[0].ID != ids[0] {
		t.Errorf("page 2 = total %d, rows %v; want 26, [oldest %d]", p2.Total, p14IDs(p2), ids[0])
	}
}

// PO-083 (PO spec §25, §27.2; CD-272 Gap 8; CD-240 family; real
// auth.Service): two approved PRJ-METRO POs with rest rows from 25-10 and
// 27-10 (P19's SetRestNotComing; the grn id is only a key), one approved PO
// without, and an approved PRJ-TECHPARK PO with a row. Priya's To close
// counts 2 and the queue lists both, oldest row first, each with the oldest
// row's date and no GRN named. Users without amend get no card and a 404.
// Closing one PO drops it, and cancelling the other leaves none.
func TestPurchaseHome_ToClose(t *testing.T) {
	env := purchasetest.New(t)
	small := p3SmallLine(t, env)
	later := p14Metro(t, env, env.Priya, small, doctype.StateApproved, nil) // lower id, newer row
	sooner := p14Metro(t, env, env.Priya, small, doctype.StateApproved, nil)
	p14Metro(t, env, env.Priya, small, doctype.StateApproved, nil) // no rest row
	tech := p14Tech(t, env, env.Arjun, small, doctype.StateApproved, nil)

	env.SetNow(t, p14At(time.October, 25, 11, 0))
	p14Rest(t, env, sooner, 90501)
	p14Rest(t, env, tech, 90502)
	env.SetNow(t, p14At(time.October, 27, 11, 0))
	p14Rest(t, env, later, 90503)
	env.SetNow(t, p14At(time.October, 28, 11, 0))
	p14Rest(t, env, later, 90504) // a second row: the meta keeps the oldest, the PO counts once

	if c, ok := p14Card(p14HomeOf(t, env, env.Priya), purchase.QueueToClose); !ok || c.Count != 2 || c.Href != p14HrefQueue+purchase.QueueToClose {
		t.Errorf("Priya's To close = %+v (shown %v), want 2 linking to its queue", c, ok)
	}
	v := p14QueueOf(t, env, env.Priya, purchase.QueueToClose, 1)
	p14WantRows(t, "Priya's to-close", v, []p14Row{
		p14OrderRow(t, env, sooner, doctype.StateApproved, p14Shree, p14RestMeta+"25-10-2026"),
		p14OrderRow(t, env, later, doctype.StateApproved, p14Shree, p14RestMeta+"27-10-2026"),
	})
	for _, r := range v.Rows {
		for _, s := range append([]string{r.Title}, r.Meta...) {
			if strings.Contains(s, "GRN") || strings.Contains(s, "9050") {
				t.Errorf("row %d names a GRN: %q", r.ID, s)
			}
		}
	}
	if c, _ := p14Card(p14HomeOf(t, env, env.Arjun), purchase.QueueToClose); c.Count != 3 {
		t.Errorf("Arjun's To close = %d, want 3 (unrestricted: PRJ-TECHPARK too)", c.Count)
	}
	for _, p := range []struct {
		name string
		v    purchase.HomeView
		err  error
	}{
		{"Suresh", p14HomeOf(t, env, env.Suresh), queueErr(t, env, env.Suresh)},
		{"Ravi", p14HomeOf(t, env, env.Ravi), queueErr(t, env, env.Ravi)},
		{"Deepak", p14HomeOf(t, env, env.Deepak), queueErr(t, env, env.Deepak)},
	} {
		if _, ok := p14Card(p.v, purchase.QueueToClose); ok {
			t.Errorf("%s (no amend) has a To close card", p.name)
		}
		p5WantNotFound(t, p.name+" opens to-close", p.err)
	}

	p2Must(t, "Priya closes")(p2Do(t, env, env.Priya, sooner, doc.ActionClose, p14CloseReason))
	if c, _ := p14Card(p14HomeOf(t, env, env.Priya), purchase.QueueToClose); c.Count != 1 {
		t.Errorf("To close after closing one = %d, want 1", c.Count)
	}
	p14WantRows(t, "to-close after closing one", p14QueueOf(t, env, env.Priya, purchase.QueueToClose, 1), []p14Row{
		p14OrderRow(t, env, later, doctype.StateApproved, p14Shree, p14RestMeta+"27-10-2026"),
	})
	p2Must(t, "Ravi cancels")(p2Do(t, env, env.Ravi, later, doc.ActionCancel, "Vendor can't supply"))
	if v := p14QueueOf(t, env, env.Priya, purchase.QueueToClose, 1); v.Total != 0 || len(v.Rows) != 0 {
		t.Errorf("to-close after cancelling the other = %+v, want empty", v)
	}
}

// queueErr is the error of opening to-close as p.
func queueErr(t *testing.T, env *purchasetest.Env, p auth.Principal) error {
	t.Helper()
	_, err := p14Queue(env.Service, t.Context(), p, purchase.QueueToClose, 1)
	return err
}
