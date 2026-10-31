package purchase_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
)

// PO-019 (PO spec §5, §6.1, §18): the reference PO (₹94,164.00) raised by
// Priya waits on the Purchase Manager rule with no number; Ravi's approval
// numbers it PO/26-27/00001.
func TestPO_ApprovalAndNumber(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtPending(), purchasetest.OrderLines(p2RefLines(t, env)...))
	if po.State != doctype.StatePending || po.Number != "" || po.CreatedBy != env.Priya.ID {
		t.Fatalf("submitted reference PO: state %q number %q by %d, want pending, unnumbered, by Priya", po.State, po.Number, po.CreatedBy)
	}
	if got, want := p3Display(t, env, po.ID), fmt.Sprintf("Draft %d", po.ID); got != want {
		t.Errorf("pending PO display = %q, want %q (no number before approval)", got, want)
	}
	pm := p3Rule(t, env, 50_000_00)
	req, ok := p3Latest(t, env, po.ID)
	if !ok || req.RuleID != pm.ID || req.Outcome != "" || types.FormatMoney(req.Amount) != "94,164.00" {
		t.Fatalf("approval request = %+v (found %v), want an open request for rule %d (Purchase Manager) of 94,164.00", req, ok, pm.ID)
	}

	res := p2Must(t, "Ravi approves")(p2Do(t, env, env.Ravi, po.ID, doc.ActionApprove, ""))
	got := p2Load(t, env, po.ID)
	if res.State != doctype.StateApproved || got.State != doctype.StateApproved || got.Number != "PO/26-27/00001" {
		t.Errorf("after Ravi approves: state %q number %q, want approved PO/26-27/00001", got.State, got.Number)
	}
}

// PO-020 (PO spec §5, §18): a PO of exactly ₹5,00,000.00 goes to the
// Director rule, the highest that applies; Ravi (Purchase Manager) can't
// approve it, Meena (Director) can.
func TestPO_DirectorThreshold(t *testing.T) {
	env := purchasetest.New(t)
	// 8,000 kg at ₹62.50, nil-rated: ₹5,00,000.00 with no tax.
	lines := []map[string]string{p2Line(t, env, "MS-ANGLE-50", "KGS", "8000", "62.5", "NIL")}
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtPending(), purchasetest.OrderLines(lines...))
	if got := p2Snap(t, env, po.ID, "rounded_total")["rounded_total"]; got != "50000000" {
		t.Fatalf("rounded_total = %s paise, want 50000000 (₹5,00,000.00)", got)
	}
	dir := p3Rule(t, env, 5_00_000_00)
	req, ok := p3Latest(t, env, po.ID)
	if !ok || req.RuleID != dir.ID || req.Outcome != "" || types.FormatMoney(req.Amount) != "5,00,000.00" ||
		len(req.ApproverRoles) != 1 || req.ApproverRoles[0] != "Director" {
		t.Fatalf("approval request = %+v (found %v), want open, rule %d (Director), 5,00,000.00, roles [Director]", req, ok, dir.ID)
	}
	_, err := p2Do(t, env, env.Ravi, po.ID, doc.ActionApprove, "")
	if !errors.Is(err, approval.ErrNotEligible) {
		t.Errorf("Ravi approves the Director-level PO: err = %v, want APR's not-eligible refusal", err)
	}
	if got := p2Load(t, env, po.ID); got.State != doctype.StatePending || got.Number != "" {
		t.Errorf("after Ravi's refused approve: state %q number %q, want still pending and unnumbered", got.State, got.Number)
	}
	p2Must(t, "Meena approves")(p2Do(t, env, env.Meena, po.ID, doc.ActionApprove, ""))
	if got := p2Load(t, env, po.ID); got.State != doctype.StateApproved || got.Number != "PO/26-27/00001" {
		t.Errorf("after Meena approves: state %q number %q, want approved PO/26-27/00001", got.State, got.Number)
	}
}

// PO-021 (PO spec §5, §18): with the test seed's rules in place, a PO below
// every rule (₹7,375.00) is approved and numbered on submit, with no
// approval request and no second person: the history names Priya.
func TestPO_NoRuleAutoApproved(t *testing.T) {
	env := purchasetest.New(t)
	id := p3Order(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"), p3SmallLine(t, env), true)
	if got := p2Snap(t, env, id, "rounded_total")["rounded_total"]; got != "737500" {
		t.Fatalf("rounded_total = %s paise, want 737500 (₹7,375.00, below ₹50,000)", got)
	}
	res := p2Must(t, "Priya submits")(p2Do(t, env, env.Priya, id, doc.ActionSubmit, ""))
	got := p2Load(t, env, id)
	if res.State != doctype.StateApproved || got.State != doctype.StateApproved || got.Number != "PO/26-27/00001" {
		t.Errorf("after submit: state %q number %q, want approved PO/26-27/00001 at once", got.State, got.Number)
	}
	if req, ok := p3Latest(t, env, id); ok {
		t.Errorf("approval request %+v, want none (no rule applies)", req)
	}
	if hist := p3History(t, env, id); !p3NoRuleHistory(hist, env.Priya) {
		t.Errorf("approval history = %+v, want submitted then %s, both by Priya (no second person)", hist, approval.HistAutoApproved)
	}
}

// PO-022 (PO spec §5, §10, §18): a Purchase Manager who raised the PO can't
// approve it (self-approval off); it stays pending, and another Purchase
// Manager (Ravi) can.
func TestPO_NoSelfApproval(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	// A buyer who is also a Purchase Manager (production rights give the
	// Purchase Manager role no create, PO spec §10).
	uid, _, err := env.Auth.CreateUser(ctx, env.Admin, auth.NewUser{Email: "nikhil@vardhan.test", Name: "Nikhil Joshi",
		Password: "Correct-Horse-42", Roles: []string{"Employee", "Purchase Manager", "Purchase User"}})
	if err != nil {
		t.Fatalf("create the buying manager: %v", err)
	}
	mgr, _, err := env.Auth.LoadPrincipal(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	id := p3Order(t, env, mgr, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"), p2RefLines(t, env), false)
	pm := p3Rule(t, env, 50_000_00)
	if req, ok := p3Latest(t, env, id); !ok || req.RuleID != pm.ID || req.AllowSelf {
		t.Fatalf("approval request = %+v (found %v), want the Purchase Manager rule %d, self-approval off", req, ok, pm.ID)
	}
	_, err = p2Do(t, env, mgr, id, doc.ActionApprove, "")
	if !errors.Is(err, approval.ErrSelfApproval) {
		t.Errorf("the raiser approves: err = %v, want APR's self-approval refusal", err)
	}
	got := p2Load(t, env, id)
	if got.State != doctype.StatePending || got.Number != "" {
		t.Errorf("after the refused self-approval: state %q number %q, want pending, unnumbered", got.State, got.Number)
	}
	if req, ok := p3Latest(t, env, id); !ok || req.Outcome != "" {
		t.Errorf("approval request after the refusal = %+v, want still open", req)
	}
	p2Must(t, "Ravi approves")(p2Do(t, env, env.Ravi, id, doc.ActionApprove, ""))
	if got := p2Load(t, env, id); got.State != doctype.StateApproved || got.Number != "PO/26-27/00001" {
		t.Errorf("after Ravi approves: state %q number %q, want approved PO/26-27/00001", got.State, got.Number)
	}
}

// PO-023 (PO spec §3.1, §6.1, §18; D-NAM-04): the FY in the number comes from
// order_date (Hooks.Date), not the clock: 31-03-2026 gives PO/25-26/00001
// while today (24-10-2026) is in 26-27.
func TestPO_NumberFYFromOrderDate(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved(), purchasetest.OrderHeader(map[string]string{"order_date": "31-03-2026"}))
	if po.Number != "PO/25-26/00001" {
		t.Errorf("PO dated 31-03-2026 numbered %q, want PO/25-26/00001", po.Number)
	}
	next := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved())
	if next.Number != "PO/26-27/00001" {
		t.Errorf("PO dated today numbered %q, want PO/26-27/00001 (its own counter)", next.Number)
	}
}

// PO-040 (PO spec §12, §18; NAM-056, NAM-057): with PO/26-27/'s last number
// used set to 437, the next PO approved is PO/26-27/00438; setting a lower
// number afterwards is refused.
func TestPO_CutoverCounter(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	namer := p3Namer(env)
	if err := namer.SetCounter(ctx, env.Admin, purchase.DocType, "PO/26-27/", 437); err != nil {
		t.Fatalf("set PO/26-27/ to 437: %v", err)
	}
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved())
	if po.Number != "PO/26-27/00438" {
		t.Errorf("first PO after cutover numbered %q, want PO/26-27/00438", po.Number)
	}
	for _, last := range []int64{437, 100} {
		err := namer.SetCounter(ctx, env.Admin, purchase.DocType, "PO/26-27/", last)
		if !errors.Is(err, naming.ErrCounterBelowIssued) {
			t.Errorf("set PO/26-27/ to %d after 00438 was issued: err = %v, want the below-issued refusal", last, err)
		}
	}
	if next := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved()); next.Number != "PO/26-27/00439" {
		t.Errorf("next PO numbered %q, want PO/26-27/00439 (the refused set changed nothing)", next.Number)
	}
}

// PO-046 (PO spec §12, §18; CD-235): with no approval rules (back-entry), a
// reference-sized PO dated 15-04-2026 is approved on submit as
// PO/26-27/00001, and one dated 31-03-2026 takes PO/25-26/00001 with its own
// counter. Each has the approval history's approved_no_rule entry naming the
// submitter, which the print reads (P8).
func TestPO_BackEntry(t *testing.T) {
	env := purchasetest.New(t, purchasetest.WithNoRules())
	for _, c := range []struct{ date, number string }{
		{"2026-04-15", "PO/26-27/00001"},
		{"2026-03-31", "PO/25-26/00001"},
	} {
		h := p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO")
		h["order_date"] = c.date
		id := p3Order(t, env, env.Priya, h, p2RefLines(t, env), true)
		res := p2Must(t, "Priya submits the "+c.date+" PO")(p2Do(t, env, env.Priya, id, doc.ActionSubmit, ""))
		got := p2Load(t, env, id)
		if res.State != doctype.StateApproved || got.State != doctype.StateApproved || got.Number != c.number {
			t.Errorf("PO dated %s after submit: state %q number %q, want approved %s", c.date, got.State, got.Number, c.number)
		}
		if req, ok := p3Latest(t, env, id); ok {
			t.Errorf("PO dated %s: approval request %+v, want none (no rules)", c.date, req)
		}
		if hist := p3History(t, env, id); !p3NoRuleHistory(hist, env.Priya) {
			t.Errorf("PO dated %s: approval history = %+v, want submitted then %s, both by Priya", c.date, hist, approval.HistAutoApproved)
		}
	}
}

// p3NoRuleHistory reports whether hist is APR's no-rule submit by p: the
// submitted entry, then approved_no_rule (the entry the print reads for the
// approver), both naming p and no one else.
func p3NoRuleHistory(hist []approval.HistoryRow, p auth.Principal) bool {
	if len(hist) != 2 || hist[0].Action != approval.HistSubmitted || hist[1].Action != approval.HistAutoApproved {
		return false
	}
	for _, h := range hist {
		if h.By != p.ID || h.ByName != p.Name {
			return false
		}
	}
	return true
}
