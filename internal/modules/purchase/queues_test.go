package purchase_test

import (
	"fmt"
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
// date being Expected on when set, else required by; meta vendor, receipt
// status, "Expected on {date}" or "Required by {date}", and "Overdue" when
// the due date is past. A fully received PO isn't listed.
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
	env.SetNow(t, p14At(time.October, 31, 10, 30))

	v := p14QueueOf(t, env, env.Priya, purchase.QueueAwaitingDelivery, 1)
	want := []p14Row{
		p14OrderRow(t, env, expectedEarly, doctype.StateApproved, p14Shree, "Not received", "Expected on 27-10-2026", "Overdue"),
		p14OrderRow(t, env, pastRequired, doctype.StateApproved, p14Shree, "Not received", "Required by 28-10-2026", "Overdue"),
		p14OrderRow(t, env, expectedLate, doctype.StateApproved, p14Shree, "Not received", "Expected on 05-11-2026"),
		p14OrderRow(t, env, part, doctype.StateApproved, p14Shree, "Part received", "Required by 06-11-2026"),
	}
	// The PO with no due date has no due meta; where it sorts is not fixed.
	var dated []p14Row
	var undated *purchase.QueueRow
	for i, r := range v.Rows {
		if r.ID == noDue {
			undated = &v.Rows[i]
			continue
		}
		dated = append(dated, p14Row{DocType: r.DocType, ID: r.ID, Title: r.Title, Total: r.Total, State: r.State, Meta: r.Meta})
	}
	p14WantRows(t, "awaiting-delivery (dated)", purchase.QueueView{Rows: toQueueRows(dated), Total: v.Total - 1}, want)
	if undated == nil || fmt.Sprint(undated.Meta) != fmt.Sprint([]string{p14Shree, "Not received"}) {
		t.Errorf("the PO with no due date = %+v, want listed with meta [%s Not received]", undated, p14Shree)
	}
	if v.Total != 5 {
		t.Errorf("awaiting-delivery Total = %d, want 5 (the fully received PO isn't listed)", v.Total)
	}
}

// toQueueRows turns expected-shape rows back into QueueRows.
func toQueueRows(rows []p14Row) []purchase.QueueRow {
	out := []purchase.QueueRow{}
	for _, r := range rows {
		out = append(out, purchase.QueueRow{DocType: r.DocType, ID: r.ID, Title: r.Title, Total: r.Total, State: r.State, Meta: r.Meta})
	}
	return out
}

// UX §5.2 requests-to-order and spec §25: submitted requests with pending to
// order, oldest needed-by first; meta requester, "{site} · {needed by}" (the
// site alone without one) and "{k} rows to order" (singular for 1). Drafts
// aren't listed.
func TestPurchaseHome_RequestsToOrderQueue(t *testing.T) {
	env := purchasetest.New(t)
	two := []map[string]string{p5Line(t, env, "MS-ANGLE-50", "KGS", "100"), p5Line(t, env, "WIRE-18G", "KGS", "50")}
	later := p14Request(t, env, env.Suresh, "METRO-SITE", "PRJ-METRO", two, true, map[string]string{"required_by": "2026-11-05"})
	sooner := p14Request(t, env, env.Suresh, "PUNE-WH", "PRJ-METRO", nil, true, map[string]string{"required_by": "2026-10-30"})
	none := p14Request(t, env, env.Suresh, "METRO-SITE", "PRJ-METRO", nil, true, nil)
	p14Request(t, env, env.Suresh, "METRO-SITE", "PRJ-METRO", nil, false, nil) // a draft

	v := p14QueueOf(t, env, env.Priya, purchase.QueueRequestsToOrder, 1)
	if v.Total != 3 || len(v.Rows) != 3 {
		t.Fatalf("requests-to-order = %+v, want 3 rows", v)
	}
	var dated []p14Row
	for _, r := range v.Rows {
		if r.ID == none {
			if want := []string{"Suresh Patil", "METRO-SITE", "1 row to order"}; fmt.Sprint(r.Meta) != fmt.Sprint(want) || r.State != doctype.StateApproved {
				t.Errorf("request with no needed-by = %+v, want meta %v, approved", r, want)
			}
			continue
		}
		dated = append(dated, p14Row{DocType: r.DocType, ID: r.ID, Title: r.Title, Total: r.Total, State: r.State, Meta: r.Meta})
	}
	p14WantRows(t, "requests-to-order (dated)", purchase.QueueView{Rows: toQueueRows(dated), Total: 2}, []p14Row{
		p14RequestRow(t, env, sooner, doctype.StateApproved, "Suresh Patil", "PUNE-WH · 30-10-2026", "1 row to order"),
		p14RequestRow(t, env, later, doctype.StateApproved, "Suresh Patil", "METRO-SITE · 05-11-2026", "2 rows to order"),
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
