package approval_test

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// docStatus is the document's approval status line, read in one db.Read.
func docStatus(t *testing.T, e *at.Env, d at.Doc) approval.Status {
	t.Helper()
	var s approval.Status
	at.MustOK(t, e.DB.Read(t.Context(), func(tx *db.Tx) (err error) {
		s, err = e.A.Status(t.Context(), tx, d.Type, d.ID)
		return err
	}))
	return s
}

// myRequests is p's "My requests" page.
func myRequests(t *testing.T, e *at.Env, p auth.Principal, page types.Page) types.Result[approval.RequestItem] {
	t.Helper()
	res, err := e.A.MyRequests(t.Context(), p, page)
	at.MustOK(t, err)
	return res
}

func wantStatus(t *testing.T, label string, got approval.Status, line string, pending bool) {
	t.Helper()
	if got.Line != line || got.Pending != pending {
		t.Errorf("%s: Status = {Line:%q Pending:%v}, want {Line:%q Pending:%v}", label, got.Line, got.Pending, line, pending)
	}
}

func TestApprovalApprove_StampsApprover(t *testing.T) { // APR-056
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	wantStatus(t, "draft, never submitted", docStatus(t, e, d), "", false)
	at.MustOK(t, e.Submit(e.Priya, d))
	wantStatus(t, "pending", docStatus(t, e, d), "Waiting for approval from Purchase Manager or Director", true)

	at9 := time.Date(2026, 10, 24, 9, 15, 0, 0, time.UTC)
	e.Clock.Set(at9)
	at.MustOK(t, e.Approve(e.Ravi, d))
	r, ok := e.Latest(t, d)
	if !ok || r.Outcome != approval.OutcomeApproved || r.DecidedBy != e.Ravi.ID || !r.DecidedAt.Equal(at9) {
		t.Errorf("latest request %v %+v; want outcome approved, decided by Ravi (%d) at %s", ok, r, e.Ravi.ID, at9)
	}
	if e.Number(t, d) != "PO/26-27/00001" {
		t.Errorf("number %q, want PO/26-27/00001", e.Number(t, d))
	}
	wantStatus(t, "approved", docStatus(t, e, d), "Approved by Ravi Kulkarni on 24-10-2026 14:45", false) // types.FormatIST, CD-33

	// Below every rule: approved on submit.
	d41 := e.NewDoc(t, at.Order, 41, at.Rs(40000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d41))
	wantStatus(t, "approved on submit", docStatus(t, e, d41), "Approved on submit (no approval needed)", false)
}

func TestApprovalRevise_ToDraftKeepsReason(t *testing.T) { // APR-063
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	at.MustOK(t, e.Reject(e.Ravi, d, "Rate is 12% above last PO. Renegotiate with vendor."))
	wantStatus(t, "rejected", docStatus(t, e, d),
		"Rejected by Ravi Kulkarni on 24-10-2026 10:30: Rate is 12% above last PO. Renegotiate with vendor.", false)
	at.MustOK(t, e.Revise(e.Priya, d))
	if e.State(t, d) != doctype.StateDraft || e.Number(t, d) != "" || e.Title(t, d) != "Draft 42" {
		t.Errorf("state %s number %q title %q; want draft, no number, Draft 42", e.State(t, d), e.Number(t, d), e.Title(t, d))
	}
	wantStatus(t, "revised", docStatus(t, e, d),
		"Last rejected by Ravi Kulkarni: Rate is 12% above last PO. Renegotiate with vendor.", false)

	// Withdrawn (R2).
	d43 := e.NewDoc(t, at.Order, 43, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d43))
	at.MustOK(t, e.Withdraw(e.Priya, d43))
	wantStatus(t, "withdrawn", docStatus(t, e, d43), "Withdrawn by Priya Shah on 24-10-2026 10:30", false)
}

func TestApprovalInbox_OnlyActionable(t *testing.T) { // APR-077
	e := at.New(t)
	e.Auth.AddRole(e.Ravi.ID, "Purchase User")
	steps := []struct {
		d  at.Doc
		by auth.Principal
	}{
		{e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya, at.Party("Shree Steel Traders")), e.Priya},
		{e.NewDoc(t, at.Order, 50, at.Rs(600000), e.Priya), e.Priya},
		{e.NewDoc(t, at.Order, 43, at.Rs(60000), e.Ravi), e.Ravi},
		{e.NewDoc(t, at.Claim, 10, at.Rs(3450), e.Deepak), e.Deepak},
	}
	for i, s := range steps {
		e.Clock.Set(time.Date(2026, 10, 24, 5, i, 0, 0, time.UTC))
		at.MustOK(t, e.Submit(s.by, s.d))
	}
	for _, c := range []struct {
		p    auth.Principal
		want []string
	}{
		{e.Ravi, []string{"Draft 42"}},
		{e.Meena, []string{"Draft 42", "Draft 50", "Draft 43", "Draft 10"}},
		{e.Priya, []string{"Draft 10"}},
	} {
		if got := e.InboxTitles(t, c.p); !slices.Equal(got, c.want) {
			t.Errorf("%s's inbox = %q, want %q", c.p.Name, got, c.want)
		}
	}

	// The card's facts (party from Info.Party, CD-55).
	res, err := e.A.Inbox(t.Context(), e.Ravi, types.Page{Number: 1, Size: 25})
	at.MustOK(t, err)
	if res.Total != 1 || len(res.Rows) != 1 {
		t.Fatalf("Ravi's inbox = %+v, want one row, total 1", res)
	}
	it := res.Rows[0]
	if it.DocType != at.Order || it.Label != "Purchase Order" || it.DocID != 42 || it.Title != "Draft 42" ||
		it.Party != "Shree Steel Traders" || it.Amount != at.Rs(60000) || it.SubmittedBy != e.Priya.ID ||
		it.SubmittedByName != "Priya Shah" || !it.SubmittedAt.Equal(time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)) || it.RequestID == 0 {
		t.Errorf("Ravi's card = %+v", it)
	}

	// Paging: Total counts every included row; zero page = Number 1, Size 25.
	for _, c := range []struct {
		page  types.Page
		want  []string
		total int
	}{
		{types.Page{}, []string{"Draft 42", "Draft 50", "Draft 43", "Draft 10"}, 4},
		{types.Page{Number: 1, Size: 3}, []string{"Draft 42", "Draft 50", "Draft 43"}, 4},
		{types.Page{Number: 2, Size: 3}, []string{"Draft 10"}, 4},
		{types.Page{Number: 3, Size: 3}, nil, 4},
		{types.Page{Number: math.MaxInt, Size: 25}, nil, 4},
		{types.Page{Number: 1<<61 + 1, Size: 4}, nil, 4}, // (Number-1)*Size wraps to math.MinInt
	} {
		res, err := e.A.Inbox(t.Context(), e.Meena, c.page)
		at.MustOK(t, err)
		var got []string
		for _, r := range res.Rows {
			got = append(got, r.Title)
		}
		if !slices.Equal(got, c.want) || res.Total != c.total {
			t.Errorf("Meena's inbox page %+v = %q total %d, want %q total %d", c.page, got, res.Total, c.want, c.total)
		}
	}
}

func TestApprovalStatus_PendingClaimNamesManager(t *testing.T) {
	e := at.New(t)
	d := e.NewDoc(t, at.Claim, 10, at.Rs(3450), e.Deepak)
	at.MustOK(t, e.Submit(e.Deepak, d))
	wantStatus(t, "pending claim", docStatus(t, e, d), "Waiting for approval from Priya Shah or Director", true)
}

func TestApprovalInbox_ClosesAndReopens(t *testing.T) { // APR-080
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	if got := e.InboxTitles(t, e.Ravi); !slices.Equal(got, []string{"Draft 42"}) {
		t.Fatalf("before approval Ravi's inbox = %q, want [Draft 42]", got)
	}
	at.MustOK(t, e.Approve(e.Meena, d))
	if got := e.InboxTitles(t, e.Ravi); len(got) != 0 {
		t.Errorf("approved item must leave Ravi's list, got %q", got)
	}

	e2 := at.New(t)
	d2 := e2.NewDoc(t, at.Order, 42, at.Rs(60000), e2.Priya)
	at.MustOK(t, e2.Submit(e2.Priya, d2))
	at.MustOK(t, e2.Reject(e2.Meena, d2, "Fix rate"))
	if got := e2.InboxTitles(t, e2.Ravi); len(got) != 0 {
		t.Errorf("rejected item must leave Ravi's list, got %q", got)
	}
	at.MustOK(t, e2.Revise(e2.Priya, d2))
	at.MustOK(t, e2.Submit(e2.Priya, d2))
	if got := e2.InboxTitles(t, e2.Ravi); !slices.Equal(got, []string{"Draft 42"}) {
		t.Errorf("re-submitted: Ravi's inbox = %q, want [Draft 42] exactly once", got)
	}
}

func TestApprovalInbox_MyRequests(t *testing.T) { // APR-081
	e := at.New(t)
	e.Auth.AddRole(e.Ravi.ID, "Purchase User")
	step := 0
	tick := func() { step++; e.Clock.Set(time.Date(2026, 10, 24, 5, step, 0, 0, time.UTC)) }
	tick()
	d42 := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d42))
	tick()
	d44 := e.NewDoc(t, at.Order, 44, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d44))
	tick()
	at.MustOK(t, e.Approve(e.Ravi, d44))
	tick()
	d45 := e.NewDoc(t, at.Order, 45, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d45))
	tick()
	rejectedAt := e.Clock.Now()
	at.MustOK(t, e.Reject(e.Ravi, d45, "Wrong vendor"))
	tick()
	d43 := e.NewDoc(t, at.Order, 43, at.Rs(60000), e.Ravi) // someone else's request
	at.MustOK(t, e.Submit(e.Ravi, d43))

	res := myRequests(t, e, e.Priya, types.Page{Number: 1, Size: 25})
	var titles []string
	got := map[string]approval.RequestItem{}
	for _, r := range res.Rows {
		titles = append(titles, r.Title)
		got[r.Title] = r
	}
	if res.Total != 3 || !slices.Equal(titles, []string{"Draft 45", "PO/26-27/00001", "Draft 42"}) {
		t.Fatalf("Priya's requests = %q total %d, want [Draft 45 PO/26-27/00001 Draft 42] (newest first) total 3", titles, res.Total)
	}
	if r := got["Draft 42"]; r.Outcome != "" || r.State != string(doctype.StatePending) {
		t.Errorf("Draft 42 = %+v, want open, pending_approval", r)
	}
	if r := got["PO/26-27/00001"]; r.Outcome != approval.OutcomeApproved || r.State != string(doctype.StateApproved) || r.DecidedByName != "Ravi Kulkarni" {
		t.Errorf("PO/26-27/00001 = %+v, want approved by Ravi Kulkarni, state approved", r)
	}
	if r := got["Draft 45"]; r.Outcome != approval.OutcomeRejected || r.State != string(doctype.StateRejected) ||
		r.Reason != "Wrong vendor" || r.DecidedByName != "Ravi Kulkarni" || !r.DecidedAt.Equal(rejectedAt) {
		t.Errorf("Draft 45 = %+v, want rejected by Ravi Kulkarni at %s: Wrong vendor", r, rejectedAt)
	}

	// Latest request per document only.
	e2 := at.New(t)
	d := e2.NewDoc(t, at.Order, 42, at.Rs(60000), e2.Priya)
	at.MustOK(t, e2.Submit(e2.Priya, d))
	at.MustOK(t, e2.Reject(e2.Ravi, d, "Fix rate"))
	at.MustOK(t, e2.Revise(e2.Priya, d))
	at.MustOK(t, e2.Submit(e2.Priya, d))
	res2 := myRequests(t, e2, e2.Priya, types.Page{Number: 1, Size: 25})
	if res2.Total != 1 || len(res2.Rows) != 1 || res2.Rows[0].Outcome != "" || res2.Rows[0].State != string(doctype.StatePending) {
		t.Errorf("re-submitted Draft 42: Priya's requests = %+v, want one open pending row", res2)
	}
}

func TestApprovalInbox_ExcludesOwnUnlessAllowed(t *testing.T) { // APR-082
	e := at.New(t)
	e.Auth.AddRole(e.Ravi.ID, "Purchase User")
	d := e.NewDoc(t, at.Order, 43, at.Rs(60000), e.Ravi)
	at.MustOK(t, e.Submit(e.Ravi, d))
	if got := e.InboxTitles(t, e.Ravi); slices.Contains(got, "Draft 43") {
		t.Errorf("own document listed: %q", got)
	}

	e2 := at.New(t)
	e2.Auth.AddRole(e2.Ravi.ID, "Purchase User")
	e2.RemoveRules(t, at.Order)
	e2.AddRule(t, approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(50000), ApproverType: approval.ApproverRole, Role: "Purchase Manager", AllowSelfApproval: true})
	d2 := e2.NewDoc(t, at.Order, 43, at.Rs(60000), e2.Ravi)
	at.MustOK(t, e2.Submit(e2.Ravi, d2))
	if got := e2.InboxTitles(t, e2.Ravi); !slices.Contains(got, "Draft 43") {
		t.Errorf("allowed self-approval must list it, got %q", got)
	}
}

func TestApprovalInbox_SkipsUnresolvableDocument(t *testing.T) { // Review Focus 4
	e := at.New(t)
	for _, id := range []int64{42, 43} {
		d := e.NewDoc(t, at.Order, id, at.Rs(60000), e.Priya)
		at.MustOK(t, e.Submit(e.Priya, d))
	}
	// Data drift: Draft 42's document row is gone, its open request stays.
	at.MustOK(t, e.DB.Write(t.Context(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(t.Context(), `DELETE FROM aprtest_docs WHERE doctype = ? AND id = ?`, at.Order, 42)
		return err
	}))
	if got := e.InboxTitles(t, e.Ravi); !slices.Equal(got, []string{"Draft 43"}) {
		t.Errorf("Ravi's inbox = %q, want [Draft 43]", got)
	}
}
