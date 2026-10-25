package approvaltest

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// The fake-vs-real contract suite (WP3 Task 1, CD-182). Run drives an
// approval.Service through the CD-23 calls DOC makes (Evaluate, Open,
// CheckApprover, Record) and checks the §5 behaviours DOC depends on. Every
// refusal is checked by sentinel (errors.Is) and exact message, so a fake that
// refuses differently, or not at all, fails the same row as a broken real
// service (CD-125, CD-163, CD-173).

// Setup arranges the state a behaviour needs, through the real service's
// Administrator API or the fake's helpers. Principals are the canonical
// fixture users (testkit.Principal).
type Setup interface {
	// DB is the database each protocol step's db.Write runs in.
	DB() *db.DB
	// SetRules replaces every rule of dt with rules (none: dt needs no
	// approval) and returns the new rules' ids in order.
	SetRules(t testing.TB, dt string, rules ...approval.RuleInput) []int64
	// NewDoc creates a draft of dt with id, amount and creator (an Order on
	// PRJ-METRO; a Claim for the creator's employee).
	NewDoc(t testing.TB, dt string, id int64, amount types.Money, creator auth.Principal)
	// SetState puts the document in state with number ("" = none), as DOC
	// does after each protocol step.
	SetState(t testing.TB, dt string, id int64, state doctype.State, number string)
}

// errRollback is the caller's failure after Record (APR-053).
var errRollback = errors.New("approvaltest: caller fails after Record")

// Seed-shaped rules (00-fixtures R1, R2, E1).
func roleRule(dt string, from types.Money, role string) approval.RuleInput {
	return approval.RuleInput{DocType: dt, FromAmount: from, ApproverType: approval.ApproverRole, Role: role}
}

func managerRule(from types.Money, fallback string) approval.RuleInput {
	return approval.RuleInput{DocType: Claim, FromAmount: from, ApproverType: approval.ApproverManager, FallbackRole: fallback}
}

// contract is one behaviour's service, setup and fixture principals.
type contract struct {
	t                                *testing.T
	svc                              approval.Service
	s                                Setup
	anil, meena, ravi, priya, suresh auth.Principal
	deepak                           auth.Principal
	r1, r2                           int64 // the Order rules from orderRules
}

func newContract(t *testing.T, newSvc func(t *testing.T) (approval.Service, Setup)) *contract {
	t.Helper()
	svc, s := newSvc(t)
	return &contract{
		t: t, svc: svc, s: s,
		anil:   testkit.Principal(t, "admin@vardhan.test"),
		meena:  testkit.Principal(t, "meena@vardhan.test"),
		ravi:   testkit.Principal(t, "ravi@vardhan.test"),
		priya:  testkit.Principal(t, "priya@vardhan.test"),
		suresh: testkit.Principal(t, "suresh@vardhan.test"),
		deepak: testkit.Principal(t, "deepak@vardhan.test"),
	}
}

// orderRules sets the seed Order rules R1 (₹50,000.00 → Purchase Manager)
// and R2 (₹5,00,000.00 → Director).
func (c *contract) orderRules() {
	ids := c.s.SetRules(c.t, Order, roleRule(Order, Rs(50_000), "Purchase Manager"), roleRule(Order, Rs(5_00_000), "Director"))
	if len(ids) != 2 {
		c.t.Fatalf("SetRules returned %d ids, want 2", len(ids))
	}
	c.r1, c.r2 = ids[0], ids[1]
}

func (c *contract) write(fn func(ctx context.Context, tx *db.Tx) error) error {
	ctx := context.Background()
	return c.s.DB().Write(ctx, func(tx *db.Tx) error { return fn(ctx, tx) })
}

// evaluate runs Evaluate alone (in a write that keeps nothing).
func (c *contract) evaluate(dt string, id int64, amount types.Money, p auth.Principal) (approval.Requirement, error) {
	var req approval.Requirement
	err := c.write(func(ctx context.Context, tx *db.Tx) (err error) {
		req, err = c.svc.Evaluate(ctx, tx, dt, id, amount, p)
		return err
	})
	return req, err
}

// submit is DOC's submit (CD-23): Evaluate → Open (always) → if not needed,
// Record(approve) without CheckApprover. It moves the state as DOC would.
func (c *contract) submit(dt string, id int64, amount types.Money, p auth.Principal) (approval.Requirement, error) {
	var req approval.Requirement
	err := c.write(func(ctx context.Context, tx *db.Tx) (err error) {
		if req, err = c.svc.Evaluate(ctx, tx, dt, id, amount, p); err != nil {
			return err
		}
		if err := c.svc.Open(ctx, tx, dt, id, req); err != nil {
			return err
		}
		if req.Needed {
			return nil
		}
		return c.svc.Record(ctx, tx, dt, id, doctype.ActionApprove, p, "")
	})
	if err == nil {
		state := doctype.StatePending
		if !req.Needed {
			state = doctype.StateApproved
		}
		c.s.SetState(c.t, dt, id, state, "")
	}
	return req, err
}

// mustPending creates a draft and submits it into pending_approval.
func (c *contract) mustPending(dt string, id int64, amount types.Money, creator, submitter auth.Principal) approval.Requirement {
	c.t.Helper()
	c.s.NewDoc(c.t, dt, id, amount, creator)
	req, err := c.submit(dt, id, amount, submitter)
	MustOK(c.t, err)
	if !req.Needed {
		c.t.Fatalf("submit of %s %d at %s: Needed = false, want true", dt, id, types.FormatINR(amount))
	}
	return req
}

func (c *contract) check(p auth.Principal, dt string, id int64) error {
	return c.write(func(ctx context.Context, tx *db.Tx) error { return c.svc.CheckApprover(ctx, tx, p, dt, id) })
}

func (c *contract) record(dt string, id int64, a doctype.Action, p auth.Principal, reason string) error {
	return c.write(func(ctx context.Context, tx *db.Tx) error { return c.svc.Record(ctx, tx, dt, id, a, p, reason) })
}

// decide is DOC's approve or reject: CheckApprover, then Record, in one write.
func (c *contract) decide(dt string, id int64, a doctype.Action, p auth.Principal, reason string) error {
	return c.write(func(ctx context.Context, tx *db.Tx) error {
		if err := c.svc.CheckApprover(ctx, tx, p, dt, id); err != nil {
			return err
		}
		return c.svc.Record(ctx, tx, dt, id, a, p, reason)
	})
}

// wantRefused checks sentinel and exact message; forbidden adds
// auth.ErrForbidden (the permission kinds, errors.go).
func wantRefused(t testing.TB, what string, err, kind error, msg string, forbidden bool) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: error = nil, want %v %q", what, kind, msg)
		return
	}
	if !errors.Is(err, kind) || err.Error() != msg {
		t.Errorf("%s: error = %v (%T)\nwant %v with message %q", what, err, err, kind, msg)
	}
	if forbidden && !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("%s: error %v does not wrap auth.ErrForbidden", what, err)
	}
}

func wantOK(t testing.TB, what string, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: error = %v, want nil", what, err)
	}
}

func wantReq(t testing.TB, what string, got approval.Requirement, needed bool, rule int64, roles []string, manager auth.UserID, amount types.Money, requester auth.UserID) {
	t.Helper()
	if got.Needed != needed || got.RuleID != rule || !slices.Equal(got.ApproverRoles, roles) ||
		got.ManagerUser != manager || got.Amount != amount || got.Requester != requester {
		t.Errorf("%s: Requirement = %+v\nwant Needed %v, RuleID %d, ApproverRoles %q, ManagerUser %d, Amount %d, Requester %d",
			what, got, needed, rule, roles, manager, amount, requester)
	}
}

// Run runs the approval contract behaviours, each against a fresh service.
func Run(t *testing.T, newSvc func(t *testing.T) (approval.Service, Setup)) {
	for _, b := range behaviours {
		t.Run(b.name, func(t *testing.T) { b.run(newContract(t, newSvc)) })
	}
}

var behaviours = []struct {
	name string
	run  func(c *contract)
}{
	{"NoRule_SubmitApprovesWithoutRequest", noRule},            // APR-015
	{"OtherDocTypeRulesIgnored", otherDocType},                 // APR-022
	{"Threshold_AtAndAboveFromAmount", threshold},              // APR-017, APR-018
	{"HighestThresholdWins", highestWins},                      // APR-021
	{"EligibleApproverPasses", eligiblePasses},                 // APR-029, APR-032
	{"NonApproverRefused", nonApprover},                        // APR-030, APR-061
	{"LowerTierRefused", lowerTier},                            // APR-033
	{"SelfApprovalBlocked", selfApproval},                      // APR-035, APR-036
	{"NoOtherApproverRefusesSubmit", noOtherApprover},          // APR-040
	{"ReportingManagerApproves", reportingManager},             // APR-044, APR-045
	{"DecisionClosesRequest", decisionCloses},                  // APR-071, APR-029
	{"FirstDecisionWins_RejectThenApprove", rejectThenApprove}, // APR-071, APR-060
	{"RejectReasonRequired", reasonRequired},                   // APR-058
	{"RejectReasonLengthInCharacters", reasonLength},           // APR-059, CD-157 A
	{"Withdraw_ClosesRequestAndResubmits", withdraw},           // APR-091, APR-096
	{"Withdraw_RefusedOutsidePending", withdrawOutside},        // APR-094, APR-055, APR-057
	{"Revise_ResubmitOpensFreshRequest", revise},               // APR-063, APR-067, APR-068
	{"OutOfProtocolOrderRefused", outOfOrder},                  // CD-23, APR-055
	{"RecordRollsBackWithCallerTx", rollback},                  // APR-053
}

// APR-015: no rule for the DocType → not needed, Open records no request,
// Record(approve) without CheckApprover; nobody can decide afterwards.
func noRule(c *contract) {
	t := c.t
	c.s.SetRules(t, Receipt)
	c.s.NewDoc(t, Receipt, 43, Rs(1_20_000), c.suresh)
	req, err := c.submit(Receipt, 43, Rs(1_20_000), c.suresh)
	MustOK(t, err)
	wantReq(t, "Evaluate", req, false, 0, nil, 0, Rs(1_20_000), c.suresh.ID)
	c.s.SetState(t, Receipt, 43, doctype.StateApproved, "GR/26-27/00001")
	wantRefused(t, "CheckApprover after a no-rule approval", c.check(c.ravi, Receipt, 43),
		approval.ErrNotPending, "GR/26-27/00001 is not waiting for approval. It is Approved.", false)
}

// APR-022: only the Claim rule from ₹0.00 exists → a ₹10,00,000.00 Order
// needs no approval.
func otherDocType(c *contract) {
	t := c.t
	c.s.SetRules(t, Order)
	c.s.SetRules(t, Claim, managerRule(0, "Director"))
	c.s.NewDoc(t, Order, 41, Rs(10_00_000), c.priya)
	req, err := c.evaluate(Order, 41, Rs(10_00_000), c.priya)
	MustOK(t, err)
	wantReq(t, "Evaluate", req, false, 0, nil, 0, Rs(10_00_000), c.priya.ID)
}

// APR-017, APR-018: needed when amount ≥ from amount, compared in paise.
func threshold(c *contract) {
	t := c.t
	c.orderRules()
	rows := []struct {
		amount types.Money
		needed bool
	}{
		{Rs(50_000) - 1, false}, // ₹49,999.99
		{Rs(50_000), true},
		{Rs(50_000) + 1, true},
		{0, false},
	}
	for i, r := range rows {
		id := int64(60 + i)
		c.s.NewDoc(t, Order, id, r.amount, c.priya)
		req, err := c.evaluate(Order, id, r.amount, c.priya)
		MustOK(t, err)
		if r.needed {
			wantReq(t, "Evaluate at "+types.FormatINR(r.amount), req, true, c.r1, []string{"Purchase Manager", "Director"}, 0, r.amount, c.priya.ID)
		} else {
			wantReq(t, "Evaluate at "+types.FormatINR(r.amount), req, false, 0, nil, 0, r.amount, c.priya.ID)
		}
	}
	// A rule from ₹0.00 makes ₹0.00 need approval.
	ids := c.s.SetRules(t, Order, roleRule(Order, 0, "Purchase Manager"))
	c.s.NewDoc(t, Order, 70, 0, c.priya)
	req, err := c.evaluate(Order, 70, 0, c.priya)
	MustOK(t, err)
	wantReq(t, "Evaluate at ₹0.00 under a ₹0.00 rule", req, true, ids[0], []string{"Purchase Manager"}, 0, 0, c.priya.ID)
}

// APR-021: ₹60,000.00 uses R1, ₹6,00,000.00 uses R2 (Director only).
func highestWins(c *contract) {
	t := c.t
	c.orderRules()
	c.s.NewDoc(t, Order, 42, Rs(60_000), c.priya)
	c.s.NewDoc(t, Order, 50, Rs(6_00_000), c.priya)
	req, err := c.evaluate(Order, 42, Rs(60_000), c.priya)
	MustOK(t, err)
	wantReq(t, "Evaluate ₹60,000.00", req, true, c.r1, []string{"Purchase Manager", "Director"}, 0, Rs(60_000), c.priya.ID)
	req, err = c.evaluate(Order, 50, Rs(6_00_000), c.priya)
	MustOK(t, err)
	wantReq(t, "Evaluate ₹6,00,000.00", req, true, c.r2, []string{"Director"}, 0, Rs(6_00_000), c.priya.ID)
}

// APR-029, APR-032: the rule's role holder and a higher-tier holder pass;
// the role holder's approve is recorded.
func eligiblePasses(c *contract) {
	t := c.t
	c.orderRules()
	c.mustPending(Order, 42, Rs(60_000), c.priya, c.priya)
	wantOK(t, "CheckApprover Ravi (Purchase Manager)", c.check(c.ravi, Order, 42))
	wantOK(t, "CheckApprover Meena (Director, higher tier)", c.check(c.meena, Order, 42))
	wantOK(t, "approve by Ravi", c.decide(Order, 42, doctype.ActionApprove, c.ravi, ""))
}

// APR-030, APR-061: a user without the role is refused at CheckApprover, and
// Record refuses the same approve or reject.
func nonApprover(c *contract) {
	t := c.t
	c.orderRules()
	c.mustPending(Order, 43, Rs(60_000), c.ravi, c.ravi)
	const msg = "Only a user with the Purchase Manager or Director role can approve Draft 43 (₹60,000.00)."
	wantRefused(t, "CheckApprover Priya", c.check(c.priya, Order, 43), approval.ErrNotEligible, msg, true)
	wantRefused(t, "Record(approve) Priya", c.record(Order, 43, doctype.ActionApprove, c.priya, ""), approval.ErrNotEligible, msg, true)
	wantRefused(t, "Record(reject) Priya", c.record(Order, 43, doctype.ActionReject, c.priya, "Too costly."), approval.ErrNotEligible, msg, true)
	wantOK(t, "CheckApprover Meena", c.check(c.meena, Order, 43))
}

// APR-033: a Purchase Manager can't approve a request under R2.
func lowerTier(c *contract) {
	t := c.t
	c.orderRules()
	c.mustPending(Order, 50, Rs(6_00_000), c.priya, c.priya)
	wantRefused(t, "CheckApprover Ravi", c.check(c.ravi, Order, 50),
		approval.ErrNotEligible, "Only a user with the Director role can approve Draft 50 (₹6,00,000.00).", true)
	wantOK(t, "CheckApprover Meena", c.check(c.meena, Order, 50))
}

// APR-035, APR-036: the requester (and creator) holding the role is blocked
// at CheckApprover and at Record; another holder may approve.
func selfApproval(c *contract) {
	t := c.t
	c.orderRules()
	c.mustPending(Order, 43, Rs(70_000), c.ravi, c.ravi)
	const msg = "You can't approve your own Purchase Order. Ask another Purchase Manager or Director to approve it."
	wantRefused(t, "CheckApprover Ravi on his own", c.check(c.ravi, Order, 43), approval.ErrSelfApproval, msg, true)
	wantRefused(t, "Record(approve) Ravi on his own", c.record(Order, 43, doctype.ActionApprove, c.ravi, ""), approval.ErrSelfApproval, msg, true)
	// APR-036: created by Priya, submitted by Ravi → still Ravi's own.
	c.mustPending(Order, 44, Rs(70_000), c.priya, c.ravi)
	wantRefused(t, "CheckApprover Ravi as submitter", c.check(c.ravi, Order, 44), approval.ErrSelfApproval, msg, true)
	wantOK(t, "CheckApprover Meena", c.check(c.meena, Order, 44))
}

// APR-040: the only Purchase Manager submits under a Purchase Manager-only
// rule → Evaluate refuses the submit.
func noOtherApprover(c *contract) {
	t := c.t
	c.s.SetRules(t, Order, roleRule(Order, Rs(50_000), "Purchase Manager"))
	c.s.NewDoc(t, Order, 43, Rs(70_000), c.ravi)
	_, err := c.submit(Order, 43, Rs(70_000), c.ravi)
	wantRefused(t, "submit by Ravi", err, approval.ErrNoApprover,
		"No one else can approve this Purchase Order. Give the Purchase Manager role to another active user, then submit again.", false)
}

// APR-044, APR-045: Deepak's claim names his manager Priya; she passes, and
// Anil (sees every claim, holds no approver role) is refused naming her.
func reportingManager(c *contract) {
	t := c.t
	c.s.SetRules(t, Claim, managerRule(0, "Director"))
	req := c.mustPending(Claim, 10, Rs(3_450), c.deepak, c.deepak)
	if req.ManagerUser != c.priya.ID || !slices.Equal(req.ApproverRoles, []string{"Director"}) {
		t.Errorf("Requirement = %+v, want ManagerUser %d (Priya) and ApproverRoles [Director]", req, c.priya.ID)
	}
	wantOK(t, "CheckApprover Priya (manager)", c.check(c.priya, Claim, 10))
	wantRefused(t, "CheckApprover Anil", c.check(c.anil, Claim, 10),
		approval.ErrNotEligible, "Only Priya Shah or a user with the Director role can approve Draft 10 (₹3,450.00).", true)
}

// APR-071: after Ravi's approve, every later decision on the request is
// refused naming him.
func decisionCloses(c *contract) {
	t := c.t
	c.orderRules()
	c.mustPending(Order, 42, Rs(60_000), c.priya, c.priya)
	MustOK(t, c.decide(Order, 42, doctype.ActionApprove, c.ravi, ""))
	c.s.SetState(t, Order, 42, doctype.StateApproved, "PO/26-27/00001")
	const msg = "PO/26-27/00001 is no longer waiting for approval. It was approved by Ravi Kulkarni."
	wantRefused(t, "CheckApprover Meena", c.check(c.meena, Order, 42), approval.ErrNotPending, msg, false)
	wantRefused(t, "Record(reject) Meena", c.record(Order, 42, doctype.ActionReject, c.meena, "Too late."), approval.ErrNotPending, msg, false)
	wantRefused(t, "Record(approve) Meena", c.record(Order, 42, doctype.ActionApprove, c.meena, ""), approval.ErrNotPending, msg, false)
	wantRefused(t, "Record(withdraw) Priya", c.record(Order, 42, doctype.ActionWithdraw, c.priya, ""), approval.ErrNotPending, msg, false)
}

// APR-071, APR-060: Ravi's reject closes the request; Meena's approve is refused.
func rejectThenApprove(c *contract) {
	t := c.t
	c.orderRules()
	c.mustPending(Order, 42, Rs(60_000), c.priya, c.priya)
	MustOK(t, c.decide(Order, 42, doctype.ActionReject, c.ravi, "Rate is 12% above last PO. Renegotiate with vendor."))
	c.s.SetState(t, Order, 42, doctype.StateRejected, "")
	const msg = "Draft 42 is no longer waiting for approval. It was rejected by Ravi Kulkarni."
	wantRefused(t, "CheckApprover Meena", c.check(c.meena, Order, 42), approval.ErrNotPending, msg, false)
	wantRefused(t, "Record(approve) Meena", c.record(Order, 42, doctype.ActionApprove, c.meena, ""), approval.ErrNotPending, msg, false)
}

// APR-058: an empty or blank reason is refused and the request stays open.
func reasonRequired(c *contract) {
	t := c.t
	c.orderRules()
	c.mustPending(Order, 42, Rs(60_000), c.priya, c.priya)
	for _, r := range []string{"", "   "} {
		wantRefused(t, "reject with reason "+strconv.Quote(r), c.decide(Order, 42, doctype.ActionReject, c.ravi, r),
			approval.ErrReasonRequired, "Enter a reason for rejecting.", false)
	}
	wantOK(t, "approve by Meena after the refused rejects", c.decide(Order, 42, doctype.ActionApprove, c.meena, ""))
}

// APR-059, CD-157 A: the limit is 1,000 characters, not bytes ("₹" is 3 bytes).
func reasonLength(c *contract) {
	t := c.t
	c.orderRules()
	c.mustPending(Order, 42, Rs(60_000), c.priya, c.priya)
	wantRefused(t, "reject with 1,001 characters", c.decide(Order, 42, doctype.ActionReject, c.ravi, strings.Repeat("₹", 1001)),
		approval.ErrReasonTooLong, "Reason must be 1,000 characters or fewer.", false)
	wantOK(t, "reject with 1,000 characters (3,000 bytes) and trimmed spaces",
		c.decide(Order, 42, doctype.ActionReject, c.ravi, "  "+strings.Repeat("₹", 1000)+"  "))
}

// APR-091, APR-096: withdraw closes the open request; a later submit opens a
// new one on the current amount and rules.
func withdraw(c *contract) {
	t := c.t
	c.orderRules()
	c.mustPending(Order, 42, Rs(60_000), c.priya, c.priya)
	MustOK(t, c.record(Order, 42, doctype.ActionWithdraw, c.priya, ""))
	c.s.SetState(t, Order, 42, doctype.StateDraft, "")
	const msg = "Draft 42 is no longer waiting for approval. It was withdrawn by Priya Shah."
	wantRefused(t, "CheckApprover Ravi after withdraw", c.check(c.ravi, Order, 42), approval.ErrNotPending, msg, false)
	wantRefused(t, "Record(approve) Ravi after withdraw", c.record(Order, 42, doctype.ActionApprove, c.ravi, ""), approval.ErrNotPending, msg, false)
	req, err := c.submit(Order, 42, Rs(5_50_000), c.priya)
	MustOK(t, err)
	wantReq(t, "re-submit at ₹5,50,000.00", req, true, c.r2, []string{"Director"}, 0, Rs(5_50_000), c.priya.ID)
	wantRefused(t, "CheckApprover Ravi on the new request", c.check(c.ravi, Order, 42),
		approval.ErrNotEligible, "Only a user with the Director role can approve Draft 42 (₹5,50,000.00).", true)
	wantOK(t, "CheckApprover Meena on the new request", c.check(c.meena, Order, 42))
}

// APR-094, APR-055, APR-057: withdraw and decisions outside pending_approval
// are refused by APR too (behind DOC's guard).
func withdrawOutside(c *contract) {
	t := c.t
	c.orderRules()
	c.s.NewDoc(t, Order, 44, Rs(60_000), c.priya)
	wantRefused(t, "Record(withdraw) on a draft", c.record(Order, 44, doctype.ActionWithdraw, c.priya, ""),
		approval.ErrNotPending, "Draft 44 is not waiting for approval. It is Draft.", false)
	c.s.NewDoc(t, Order, 45, Rs(60_000), c.priya)
	c.s.SetState(t, Order, 45, doctype.StateApproved, "PO/26-27/00001")
	const msg = "PO/26-27/00001 is not waiting for approval. It is Approved."
	wantRefused(t, "Record(withdraw) on an approved PO", c.record(Order, 45, doctype.ActionWithdraw, c.priya, ""), approval.ErrNotPending, msg, false)
	wantRefused(t, "Record(reject) on an approved PO", c.record(Order, 45, doctype.ActionReject, c.ravi, "Wrong vendor."), approval.ErrNotPending, msg, false)
	wantRefused(t, "CheckApprover on an approved PO", c.check(c.ravi, Order, 45), approval.ErrNotPending, msg, false)
}

// APR-063, APR-067, APR-068: reject → revise → submit opens a fresh request
// that the earlier rejecter may approve.
func revise(c *contract) {
	t := c.t
	c.orderRules()
	c.mustPending(Order, 42, Rs(60_000), c.priya, c.priya)
	MustOK(t, c.decide(Order, 42, doctype.ActionReject, c.ravi, "Rate is 12% above last PO. Renegotiate with vendor."))
	c.s.SetState(t, Order, 42, doctype.StateRejected, "")
	wantOK(t, "Record(revise) Priya", c.record(Order, 42, doctype.ActionRevise, c.priya, ""))
	c.s.SetState(t, Order, 42, doctype.StateDraft, "")
	req, err := c.submit(Order, 42, Rs(60_000), c.priya)
	MustOK(t, err)
	wantReq(t, "re-submit", req, true, c.r1, []string{"Purchase Manager", "Director"}, 0, Rs(60_000), c.priya.ID)
	wantOK(t, "approve by Ravi on the fresh request", c.decide(Order, 42, doctype.ActionApprove, c.ravi, ""))
}

// CD-23: calls out of the protocol order are refused.
func outOfOrder(c *contract) {
	t := c.t
	c.orderRules()
	c.s.NewDoc(t, Order, 45, Rs(60_000), c.priya)
	err := c.write(func(ctx context.Context, tx *db.Tx) error {
		return c.svc.Open(ctx, tx, Order, 45, approval.Requirement{})
	})
	wantRefused(t, "Open without Evaluate", err, approval.ErrNotEvaluated, approval.ErrNotEvaluated.Error(), false)

	req := c.mustPending(Order, 46, Rs(60_000), c.priya, c.priya)
	err = c.write(func(ctx context.Context, tx *db.Tx) error { return c.svc.Open(ctx, tx, Order, 46, req) })
	if err == nil {
		t.Errorf("second Open while a request is open: error = nil, want a refusal")
	}

	// APR-055: Record(approve) on a draft that was never submitted.
	c.s.NewDoc(t, Order, 47, Rs(60_000), c.priya)
	wantRefused(t, "Record(approve) with nothing submitted", c.record(Order, 47, doctype.ActionApprove, c.ravi, ""),
		approval.ErrNotPending, "Draft 47 is not waiting for approval. It is Draft.", false)

	// Record takes only the approval actions (CD-23, CD-54).
	wantRefused(t, "Record(save)", c.record(Order, 46, doctype.ActionSave, c.ravi, ""),
		approval.ErrUnknownAction, approval.ErrUnknownAction.Error(), false)
}

// APR-053: Record's writes roll back with the caller's transaction, so the
// request stays open for the next approver.
func rollback(c *contract) {
	t := c.t
	c.orderRules()
	c.mustPending(Order, 42, Rs(60_000), c.priya, c.priya)
	err := c.write(func(ctx context.Context, tx *db.Tx) error {
		if err := c.svc.CheckApprover(ctx, tx, c.ravi, Order, 42); err != nil {
			return err
		}
		if err := c.svc.Record(ctx, tx, Order, 42, doctype.ActionApprove, c.ravi, ""); err != nil {
			return err
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("approve then caller failure: error = %v, want errRollback", err)
	}
	wantOK(t, "CheckApprover Meena after the rollback", c.check(c.meena, Order, 42))
	wantOK(t, "approve by Meena after the rollback", c.decide(Order, 42, doctype.ActionApprove, c.meena, ""))
}

// ContractSetup is the real service's Setup: rules through the Administrator
// API (Rules, DeleteRule, CreateRule as Anil), documents through NewDoc.
func (e *Env) ContractSetup() Setup { return envSetup{e} }

type envSetup struct{ e *Env }

func (s envSetup) DB() *db.DB { return s.e.DB }

func (s envSetup) SetRules(t testing.TB, dt string, rules ...approval.RuleInput) []int64 {
	t.Helper()
	ctx := context.Background()
	all, err := s.e.A.Rules(ctx, s.e.Anil)
	MustOK(t, err)
	for _, r := range all {
		if r.DocType == dt {
			MustOK(t, s.e.A.DeleteRule(ctx, s.e.Anil, r.ID))
		}
	}
	var ids []int64
	for _, in := range rules {
		r, err := s.e.A.CreateRule(ctx, s.e.Anil, in)
		MustOK(t, err)
		ids = append(ids, r.ID)
	}
	return ids
}

func (s envSetup) NewDoc(t testing.TB, dt string, id int64, amount types.Money, creator auth.Principal) {
	t.Helper()
	s.e.NewDoc(t, dt, id, amount, creator)
}

func (s envSetup) SetState(t testing.TB, dt string, id int64, state doctype.State, number string) {
	t.Helper()
	if s.e.eng != nil {
		d, ok := doctype.Lookup(dt)
		if !ok {
			t.Fatalf("approvaltest: %s is not registered", dt)
		}
		s.e.exec(t, `UPDATE `+d.Table+` SET state = ?, number = NULLIF(?, '') WHERE id = ?`, string(state), number, id)
		return
	}
	s.e.exec(t, `UPDATE aprtest_docs SET state = ?, number = ? WHERE doctype = ? AND id = ?`, string(state), number, dt, id)
}
