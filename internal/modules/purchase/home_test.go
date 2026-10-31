package purchase_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
)

// p14World is PO-066's data: POs and requests in each queue's states, with
// one record per queue that a viewer owns or could see but can't read
// (CD-299, CD-233/240/248: never counted, never listed). Built at T0
// (24-10-2026); the comments are posted on 30-10 and the world is viewed on
// 31-10-2026 10:30 IST.
type p14World struct {
	env *purchasetest.Env

	// Priya's PRJ-METRO POs.
	draft, rejected, pending, sent, unsent, full int64
	// Arjun's approved POs, neither sent.
	arjunMetro, arjunTech int64
	// Priya's PRJ-TECHPARK records, made while she was allowed PRJ-TECHPARK:
	// she owns them but can't read them.
	hidDraft, hidPending, hidApproved, hidRequest int64

	// Requests.
	sureshReq, sureshDraft, sureshClosed int64 // METRO-SITE
	deepakReq, deepakDraft, deepakCancel int64 // TECH-SITE / PRJ-TECHPARK
	priyaReq                             int64 // METRO-SITE draft

	// Comments on 30-10: Ravi on Priya's draft and on Deepak's request,
	// Arjun on Priya's hidden draft.
	onDraft, onDeepak, onHidden int64
}

func newP14World(t *testing.T) *p14World {
	t.Helper()
	env := purchasetest.New(t)
	w := &p14World{env: env}
	small, big := p3SmallLine(t, env), p2RefLines(t, env)
	due := map[string]string{"required_by": "2026-10-28"}

	w.draft = p14Metro(t, env, env.Priya, small, doctype.StateDraft, nil)
	w.rejected = p14Metro(t, env, env.Priya, big, doctype.StatePending, nil)
	p2Must(t, "Ravi rejects")(p2Do(t, env, env.Ravi, w.rejected, doc.ActionReject, "Price too high"))
	w.pending = p14Metro(t, env, env.Priya, big, doctype.StatePending, nil)
	w.sent = p14Metro(t, env, env.Priya, small, doctype.StateApproved, nil)
	p14Sent(t, env, env.Priya, w.sent)
	w.unsent = p14Metro(t, env, env.Priya, small, doctype.StateApproved, due)
	w.full = p14Metro(t, env, env.Priya, small, doctype.StateApproved, nil)
	p14Sent(t, env, env.Priya, w.full)
	p14Receive(t, env, w.full, "100")
	w.arjunMetro = p14Metro(t, env, env.Arjun, small, doctype.StateApproved, nil)
	w.arjunTech = p14Tech(t, env, env.Arjun, small, doctype.StateApproved, nil)
	p14Techpark(t, env, func() {
		w.hidDraft = p14Tech(t, env, env.Priya, small, doctype.StateDraft, nil)
		w.hidPending = p14Tech(t, env, env.Priya, big, doctype.StatePending, nil)
		w.hidApproved = p14Tech(t, env, env.Priya, small, doctype.StateApproved, due)
		w.hidRequest = p14Request(t, env, env.Priya, "TECH-SITE", "PRJ-TECHPARK", nil, true, nil)
	})

	w.sureshReq = p14Request(t, env, env.Suresh, "METRO-SITE", "PRJ-METRO", nil, true, nil)
	w.sureshDraft = p14Request(t, env, env.Suresh, "METRO-SITE", "PRJ-METRO", nil, false, nil)
	w.sureshClosed = p14Request(t, env, env.Suresh, "METRO-SITE", "PRJ-METRO", nil, true, nil)
	p5Must(t, "Suresh closes his request")(p5Do(t, env, env.Suresh, w.sureshClosed, doctype.ActionClose, p5CloseReason))
	w.deepakReq = p14Request(t, env, env.Deepak, "TECH-SITE", "PRJ-TECHPARK", nil, true, nil)
	w.deepakDraft = p14Request(t, env, env.Deepak, "TECH-SITE", "PRJ-TECHPARK", nil, false, nil)
	w.deepakCancel = p14Request(t, env, env.Deepak, "TECH-SITE", "PRJ-TECHPARK", nil, true, nil)
	p5Must(t, "Deepak cancels his request")(p5Do(t, env, env.Deepak, w.deepakCancel, doctype.ActionCancel, p5CancelReason))
	w.priyaReq = p14Request(t, env, env.Priya, "METRO-SITE", "PRJ-METRO", nil, false, nil)

	env.SetNow(t, p14At(time.October, 30, 16, 5))
	w.onDraft = p14Comment(t, env, env.Ravi, purchase.DocType, w.draft, "Use Bharat Tools for the bolts.")
	w.onDeepak = p14Comment(t, env, env.Ravi, purchase.RequestDocType, w.deepakReq, "Which size do you need?")
	w.onHidden = p14Comment(t, env, env.Arjun, purchase.DocType, w.hidDraft, "Checked the rate.")

	env.SetNow(t, p14At(time.October, 31, 10, 30))
	return w
}

// p14Want is one card a user should see and the rows its queue lists.
type p14Want struct {
	key   string
	count int
	rows  []int64 // the queue's rows as a set; nil for To approve and Overdue
}

// p14Wants are each user's cards in UX §5.1 order, with counts and queue rows.
func (w *p14World) wants() map[string][]p14Want {
	return map[string][]p14Want{
		// Purchase User + Employee, PRJ-METRO only: no To approve.
		"Priya": {
			{purchase.QueueDrafts, 2, []int64{w.draft, w.rejected}},
			{purchase.QueueToSend, 1, []int64{w.unsent}},
			{purchase.QueueToClose, 0, []int64{}},
			{purchase.QueueRequestsToOrder, 1, []int64{w.sureshReq}},
			{purchase.QueueWaiting, 1, []int64{w.pending}},
			{purchase.QueueMyRequests, 1, []int64{w.priyaReq}},
			{purchase.QueueAwaitingDelivery, 3, []int64{w.sent, w.unsent, w.arjunMetro}},
			{purchase.CardOverdue, 1, nil},
		},
		// Purchase Manager + Employee, unrestricted: To send lists all.
		"Ravi": {
			{purchase.CardToApprove, 2, nil},
			{purchase.QueueToSend, 4, []int64{w.unsent, w.arjunMetro, w.arjunTech, w.hidApproved}},
			{purchase.QueueMyRequests, 0, []int64{}},
			{purchase.QueueAwaitingDelivery, 5, []int64{w.sent, w.unsent, w.arjunMetro, w.arjunTech, w.hidApproved}},
			{purchase.CardOverdue, 2, nil},
		},
		// Stores + Employee, PUNE-WH and METRO-SITE only.
		"Suresh": {
			{purchase.QueueMyRequests, 2, []int64{w.sureshReq, w.sureshDraft}},
			{purchase.QueueAwaitingDelivery, 3, []int64{w.sent, w.unsent, w.arjunMetro}},
			{purchase.CardOverdue, 1, nil},
		},
		// Employee only.
		"Deepak": {
			{purchase.QueueMyRequests, 2, []int64{w.deepakReq, w.deepakDraft}},
		},
		// Purchase User + Employee, unrestricted: Requests to order includes
		// PRJ-TECHPARK.
		"Arjun": {
			{purchase.QueueDrafts, 0, []int64{}},
			{purchase.QueueToSend, 2, []int64{w.arjunMetro, w.arjunTech}},
			{purchase.QueueToClose, 0, []int64{}},
			{purchase.QueueRequestsToOrder, 3, []int64{w.sureshReq, w.hidRequest, w.deepakReq}},
			{purchase.QueueWaiting, 0, []int64{}},
			{purchase.QueueMyRequests, 0, []int64{}},
			{purchase.QueueAwaitingDelivery, 5, []int64{w.sent, w.unsent, w.arjunMetro, w.arjunTech, w.hidApproved}},
			{purchase.CardOverdue, 2, nil},
		},
	}
}

func (w *p14World) users() map[string]auth.Principal {
	e := w.env
	return map[string]auth.Principal{"Priya": e.Priya, "Ravi": e.Ravi, "Suresh": e.Suresh, "Deepak": e.Deepak, "Arjun": e.Arjun}
}

// p14Href is the Open link a card of key goes to (UX §5.1).
func p14Href(key string) string {
	switch key {
	case purchase.CardToApprove:
		return p14HrefApprove
	case purchase.CardOverdue:
		return p14HrefOverdue
	}
	return p14HrefQueue + key
}

// PO-066 (PO spec §25, §26; CD-258 Q10 (c), CD-262, CD-299; real
// auth.Service): each user's home has exactly the cards their rights allow,
// in UX §5.1's order, each with its count through Auth.Scope and its link.
// Every queue a user can't read a row of leaves that row out of the count.
func TestPurchaseHome_Cards(t *testing.T) {
	w := newP14World(t)
	env := w.env
	for name, p := range w.users() {
		t.Run(name, func(t *testing.T) {
			v := p14HomeOf(t, env, p)
			var keys []string
			for _, c := range w.wants()[name] {
				keys = append(keys, c.key)
			}
			if got := p14Keys(v); !reflect.DeepEqual(got, keys) {
				t.Fatalf("cards = %v, want %v", got, keys)
			}
			for _, want := range w.wants()[name] {
				c, _ := p14Card(v, want.key)
				if c.Count != want.count || c.Href != p14Href(want.key) {
					t.Errorf("card %s = count %d href %q, want %d %q", want.key, c.Count, c.Href, want.count, p14Href(want.key))
				}
			}
		})
	}

	t.Run("Overdue deliveries is P9's OverdueCount", func(t *testing.T) {
		for name, p := range w.users() {
			c, ok := p14Card(p14HomeOf(t, env, p), purchase.CardOverdue)
			if !ok {
				continue
			}
			n, err := env.Service.OverdueCount(t.Context(), p)
			if err != nil || c.Count != n {
				t.Errorf("%s: Overdue deliveries = %d, OverdueCount = %d, %v", name, c.Count, n, err)
			}
		}
	})

	t.Run("My drafts marks the rejected one; Waiting has the oldest's days", func(t *testing.T) {
		v := p14HomeOf(t, env, env.Priya)
		if c, _ := p14Card(v, purchase.QueueDrafts); c.NeedsChanges != 1 {
			t.Errorf("My drafts NeedsChanges = %d, want 1 (one rejected)", c.NeedsChanges)
		}
		// Submitted 24-10, viewed 31-10: 7 calendar days (Approvals.WaitingDays).
		if c, _ := p14Card(v, purchase.QueueWaiting); c.WaitingDays != 7 {
			t.Errorf("Waiting for approval WaitingDays = %d, want 7", c.WaitingDays)
		}
	})

	t.Run("Recent comments are RecentComments: mine, readable", func(t *testing.T) {
		for name, want := range map[string][]int64{
			"Priya":  {w.onDraft}, // not w.onHidden: she owns the PO but can't read it
			"Ravi":   {},
			"Suresh": {},
			"Deepak": {w.onDeepak},
		} {
			p := w.users()[name]
			got := []int64{}
			for _, c := range p14HomeOf(t, env, p).Comments {
				got = append(got, c.ID)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s: recent comments %v, want %v", name, got, want)
			}
			rc, err := env.Comments.RecentComments(t.Context(), p)
			if err != nil {
				t.Fatalf("RecentComments(%s): %v", name, err)
			}
			if len(rc) != len(got) {
				t.Errorf("%s: home has %d comments, RecentComments %d", name, len(got), len(rc))
			}
		}
		c := p14HomeOf(t, env, env.Priya).Comments
		if len(c) == 1 && (c[0].Body != "Use Bharat Tools for the bolts." || c[0].CreatedByName != "Ravi Kulkarni" || c[0].Display != p14Display(t, env, purchase.DocType, w.draft)) {
			t.Errorf("Priya's recent comment = %+v, want Ravi's on her draft with its display", c[0])
		}
	})
}

// PO-070 (PO spec §25, §26; CD-299; real auth.Service): every queue a user
// has the card for lists exactly the user's scoped rows, and its Total is the
// card's count. A queue whose card's right the user lacks, and an unknown
// queue, is AUTH's 404 (UX §5.2).
func TestPurchaseHome_Queues(t *testing.T) {
	w := newP14World(t)
	env := w.env
	queues := []string{
		purchase.QueueDrafts, purchase.QueueWaiting, purchase.QueueToSend, purchase.QueueToClose,
		purchase.QueueAwaitingDelivery, purchase.QueueRequestsToOrder, purchase.QueueMyRequests,
	}
	for name, p := range w.users() {
		t.Run(name, func(t *testing.T) {
			has := map[string]bool{}
			for _, want := range w.wants()[name] {
				if want.rows == nil {
					continue
				}
				has[want.key] = true
				v := p14QueueOf(t, env, p, want.key, 1)
				if got := p14Sorted(p14IDs(v)...); !reflect.DeepEqual(got, p14Sorted(want.rows...)) {
					t.Errorf("queue %s rows %v, want %v", want.key, got, p14Sorted(want.rows...))
				}
				if v.Name != want.key || v.Total != want.count || v.Total != len(want.rows) {
					t.Errorf("queue %s = name %q total %d, want %q and the card's %d", want.key, v.Name, v.Total, want.key, want.count)
				}
			}
			for _, q := range queues {
				if has[q] {
					continue
				}
				_, err := p14Queue(env.Service, t.Context(), p, q, 1)
				p5WantNotFound(t, name+" opens "+q+" without its right", err)
			}
		})
	}

	t.Run("unknown queue", func(t *testing.T) {
		for _, q := range []string{"", "to-approve", "overdue", "Drafts", "nope"} {
			_, err := p14Queue(env.Service, t.Context(), env.Admin, q, 1)
			p5WantNotFound(t, "Administrator opens queue "+q, err)
		}
	})
}

// CD-258 Q10 (c), CD-280 D4: To approve is the total of
// Approvals.InboxOfType(p, purchase.Order): open requests only, purchase
// orders only, and it links to /approvals. The row for another DocType in
// Ravi's inbox is a direct approval_requests row on Suresh's request (the
// purchasetest world has no second approvable DocType); Inbox shows it, so a
// Home that used the whole inbox would count it.
func TestPurchaseHome_ToApprove(t *testing.T) {
	w := newP14World(t)
	env := w.env
	rule := p3Rule(t, env, 50_000_00)
	at := types.TimestampOf(env.Clock.Now()).String()
	if _, err := env.DB.SQL().ExecContext(t.Context(), `INSERT INTO approval_requests
		(doctype, doc_id, rule_id, rule_from_amount, amount, approver_roles, allow_self, requester, requester_name, creator,
		 submitted_at, created_at, created_by, updated_at, updated_by)
		VALUES (?, ?, ?, 0, 0, '["Purchase Manager"]', 0, ?, 'Suresh Patil', ?, ?, ?, ?, ?, ?)`,
		purchase.RequestDocType, w.sureshReq, rule.ID, env.Suresh.ID, env.Suresh.ID, at, at, env.Suresh.ID, at, env.Suresh.ID); err != nil {
		t.Fatalf("insert the other DocType's open request: %v", err)
	}
	all, err := env.Approvals.Inbox(t.Context(), env.Ravi, types.Page{Number: 1, Size: 25})
	if err != nil || all.Total != 3 {
		t.Fatalf("Ravi's whole inbox = %d, %v; want 3 (two open POs and the request row)", all.Total, err)
	}
	c, ok := p14Card(p14HomeOf(t, env, env.Ravi), purchase.CardToApprove)
	// The rejected PO's request is decided: only w.pending and w.hidPending.
	if !ok || c.Count != 2 || c.Href != p14HrefApprove {
		t.Errorf("Ravi's To approve = %+v (shown %v), want count 2 linking to %s", c, ok, p14HrefApprove)
	}
	po, _, err := env.Approvals.InboxOfType(t.Context(), env.Meena, purchase.DocType, types.Page{Number: 1, Size: 25})
	if err != nil {
		t.Fatalf("Meena's PO inbox: %v", err)
	}
	if c, ok := p14Card(p14HomeOf(t, env, env.Meena), purchase.CardToApprove); !ok || c.Count != po.Total {
		t.Errorf("Meena's To approve = %+v (shown %v), want InboxOfType's %d", c, ok, po.Total)
	}

	// Approving one PO decides its request: the count drops.
	p2Must(t, "Ravi approves")(p2Do(t, env, env.Ravi, w.pending, doc.ActionApprove, ""))
	if c, _ := p14Card(p14HomeOf(t, env, env.Ravi), purchase.CardToApprove); c.Count != 1 {
		t.Errorf("Ravi's To approve after approving one = %d, want 1", c.Count)
	}
}

// PO spec §25 "Who sees it", one right at a time (lessons §5): each probe's
// only role holds read on purchase.Order plus the one right a card names (and
// read on purchase.Request, no create), so swapping a card's right for
// another fails here. A queue's right decides, not its count.
func TestPurchaseHome_CardRights(t *testing.T) {
	env := purchasetest.New(t)
	ad, od := purchase.QueueAwaitingDelivery, purchase.CardOverdue
	for _, c := range []struct {
		name  string
		right auth.Right
		cards []string
	}{
		{"read", "", []string{ad, od}},
		{"create", auth.RightCreate, []string{purchase.QueueDrafts, purchase.QueueToSend, purchase.QueueRequestsToOrder, purchase.QueueWaiting, ad, od}},
		{"approve", auth.RightApprove, []string{purchase.CardToApprove, purchase.QueueToSend, ad, od}},
		{"amend", auth.RightAmend, []string{purchase.QueueToClose, ad, od}},
	} {
		t.Run(c.name, func(t *testing.T) {
			rights := []auth.Right{auth.RightRead}
			if c.right != "" {
				rights = append(rights, c.right)
			}
			p := p14Probe(t, env, c.name, rights...)
			if got := p14Keys(p14HomeOf(t, env, p)); !reflect.DeepEqual(got, c.cards) {
				t.Errorf("cards = %v, want %v", got, c.cards)
			}
			for _, q := range c.cards {
				if q == purchase.CardToApprove || q == purchase.CardOverdue {
					continue
				}
				if v := p14QueueOf(t, env, p, q, 1); v.Total != 0 || len(v.Rows) != 0 {
					t.Errorf("empty world: queue %s = %+v, want no rows", q, v)
				}
			}
		})
	}

	// Meena (Director only) has no create on purchase.Request: no My requests.
	if got, want := p14Keys(p14HomeOf(t, env, env.Meena)), []string{purchase.CardToApprove, purchase.QueueToSend, ad, od}; !reflect.DeepEqual(got, want) {
		t.Errorf("Meena's cards = %v, want %v", got, want)
	}
	_, err := p14Queue(env.Service, t.Context(), env.Meena, purchase.QueueMyRequests, 1)
	p5WantNotFound(t, "Meena opens my-requests", err)
	// The Administrator holds every right: every card.
	all := []string{purchase.CardToApprove, purchase.QueueDrafts, purchase.QueueToSend, purchase.QueueToClose, purchase.QueueRequestsToOrder,
		purchase.QueueWaiting, purchase.QueueMyRequests, ad, od}
	if got := p14Keys(p14HomeOf(t, env, env.Admin)); !reflect.DeepEqual(got, all) {
		t.Errorf("Administrator's cards = %v, want %v", got, all)
	}
}
