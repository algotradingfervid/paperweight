package expense_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// EXP-054, Mark as paid and Undo paid on the real auth.Service (spec §4,
// CD-271 G1, CD-281). Clock 2026-10-24 10:30 IST.

const (
	e9MsgNotApproved = "Only an approved advance can be marked as paid."
	e9MsgFuture      = "Paid on can't be in the future."
	e9MsgBeforeDate  = "Paid on can't be before the advance date."
	e9MsgAlreadyPaid = "This advance is already marked as paid."
	e9MsgClaimUses   = "A claim already uses this advance, so it stays paid."
	e9MsgNotPaid     = "This advance isn't marked as paid."
	e9MsgCancelPaid  = "This advance is marked as paid. Undo paid first, or close it with the balance noted."

	// AUTH's 403 for a user with no cancel right on Employee Advance (CD-142).
	e9MsgNoCancel = "You don't have permission to cancel Employee Advance. Ask your administrator for a role that allows it."
	// DOC's stale text for the advance EA/26-27/00001 (DOC-069).
	e9MsgStale = "Someone else changed EA/26-27/00001 after you opened it. Reload it and make your changes again."

	e9Deepak = "deepak@vardhan.test"
	e9Priya  = "priya@vardhan.test" // Deepak's reporting manager
	e9Meena  = "meena@vardhan.test" // Director
	e9Kavya  = "kavya@vardhan.test" // reports to Meena: can't read Deepak's advance
	e9Ravi   = "ravi@vardhan.test"  // given Director in the concurrency case

	e9AdvanceDate = "2026-10-20"
	e9PaidOn      = "2026-10-21"
)

// e9Stored is an advance's row as stored.
type e9Stored struct {
	state   string
	version int
	paidOn  sql.NullString
}

func e9Read(t *testing.T, e *expensetest.Env, id int64) e9Stored {
	t.Helper()
	var s e9Stored
	err := e.DB.Read(context.Background(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(context.Background(),
			`SELECT state, version, paid_on FROM employee_advance WHERE id = ?`, id).Scan(&s.state, &s.version, &s.paidOn)
	})
	if err != nil {
		t.Fatalf("read advance %d: %v", id, err)
	}
	return s
}

// e9Audit is one audit row of the advance.
type e9Audit struct {
	userID  int64
	changes string
}

// e9AuditRows are the advance's audit rows with action, oldest first.
func e9AuditRows(t *testing.T, e *expensetest.Env, id int64, action string) []e9Audit {
	t.Helper()
	var out []e9Audit
	err := e.DB.Read(context.Background(), func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(context.Background(),
			`SELECT user_id, changes FROM audit_log WHERE doc_type = ? AND doc_id = ? AND action = ? ORDER BY id`,
			expense.AdvanceDocType, id, action)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var a e9Audit
			if err := rows.Scan(&a.userID, &a.changes); err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("read the audit of advance %d: %v", id, err)
	}
	return out
}

func e9Date(t *testing.T, s string) types.Date {
	t.Helper()
	d, err := types.ParseDate(s)
	if err != nil {
		t.Fatalf("ParseDate(%q): %v", s, err)
	}
	return d
}

func e9Do(t *testing.T, e *expensetest.Env, email, dt string, id int64, a doc.Action, in doc.ActionInput) doc.Result {
	t.Helper()
	res, err := e.Engine.Do(context.Background(), e.P(t, email), dt, id, a, in)
	if err != nil {
		t.Fatalf("%s %s %d as %s: %v", a, dt, id, email, err)
	}
	return res
}

// e9Advance is Deepak's advance dated 2026-10-20 (₹5,000.00), taken to
// state: draft, pending (submitted) or approved (by Priya).
func e9Advance(t *testing.T, e *expensetest.Env, state doc.State) doc.Result {
	t.Helper()
	res := e.DraftAdvance(t, e.P(t, e9Deepak), expensetest.Values{"advance_date": e9AdvanceDate})
	if state == doc.StateDraft {
		return res
	}
	res = e9Do(t, e, e9Deepak, expense.AdvanceDocType, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
	if state == doc.StatePending {
		if res.State != doc.StatePending {
			t.Fatalf("submitted advance is %q, want pending", res.State)
		}
		return res
	}
	if res.State == doc.StatePending {
		res = e9Do(t, e, e9Priya, expense.AdvanceDocType, res.ID, doc.ActionApprove, doc.ActionInput{Version: res.Version})
	}
	if res.State != doc.StateApproved || res.Number != "EA/26-27/00001" {
		t.Fatalf("advance is %q %q, want approved EA/26-27/00001", res.State, res.Number)
	}
	return res
}

// e9Mark marks the advance paid on on as meena and fails the test on any error.
func e9Mark(t *testing.T, e *expensetest.Env, id int64, on string) {
	t.Helper()
	v := e9Read(t, e, id).version
	if err := e.Service.MarkPaid(context.Background(), e.P(t, e9Meena), id, v, e9Date(t, on)); err != nil {
		t.Fatalf("MarkPaid(%d, version %d, %s) as meena: %v", id, v, on, err)
	}
}

// e9Refused runs do, which must fail with text and status and write nothing
// (state, version, paid on, used amount and audit count: WantRefused).
func e9Refused(t *testing.T, e *expensetest.Env, text string, status int, do func() error) {
	t.Helper()
	var got error
	e.WantRefused(t, text, func() error { got = do(); return got })
	if got == nil || status == 0 {
		return
	}
	var se interface{ Status() int }
	if !errors.As(got, &se) {
		t.Errorf("err %q (%T) has no status, want %d", got, got, status)
	} else if se.Status() != status {
		t.Errorf("err %q: status %d, want %d", got, se.Status(), status)
	}
}

// e9Claim is Deepak's draft claim (₹1,000.00) set against advanceID for
// ₹1,000.00, taken to state: draft, pending, approved, closed or rejected.
func e9Claim(t *testing.T, e *expensetest.Env, advanceID int64, state doc.State) doc.Result {
	t.Helper()
	deepak := e.P(t, e9Deepak)
	c := e.DraftClaim(t, deepak, nil)
	c = e.Against(t, deepak, c.ID, advanceID, types.Money(100000))
	if state == doc.StateDraft {
		return c
	}
	c = e9Do(t, e, e9Deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, doc.ActionInput{Version: c.Version})
	if c.State != doc.StatePending {
		t.Fatalf("submitted claim is %q, want pending", c.State)
	}
	switch state {
	case doc.StatePending:
		return c
	case doc.StateRejected:
		c = e9Do(t, e, e9Priya, expense.ClaimDocType, c.ID, doc.ActionReject, doc.ActionInput{Version: c.Version, Reason: "Receipt unclear."})
	default:
		c = e9Do(t, e, e9Priya, expense.ClaimDocType, c.ID, doc.ActionApprove, doc.ActionInput{Version: c.Version})
		if state == doc.StateClosed {
			c = e9Do(t, e, e9Meena, expense.ClaimDocType, c.ID, doc.ActionClose, doc.ActionInput{Version: c.Version, Reason: "Paid by NEFT 30-10-2026"})
		}
	}
	if c.State != state {
		t.Fatalf("claim is %q, want %q", c.State, state)
	}
	return c
}

func TestAdv_MarkAsPaid(t *testing.T) {
	ctx := context.Background()

	// deepak (own) and priya (team) read the advance but hold no cancel:
	// AUTH's 403 on both actions. meena marks it paid on 2026-10-21: paid_on
	// set, version up by 1, one audit entry `paid` by meena carrying the date.
	t.Run("rights, version and audit", func(t *testing.T) {
		t.Parallel()
		e := expensetest.New(t)
		adv := e9Advance(t, e, doc.StateApproved)
		before := e9Read(t, e, adv.ID)
		for _, email := range []string{e9Deepak, e9Priya} {
			p := e.P(t, email)
			e9Refused(t, e, e9MsgNoCancel, 403, func() error {
				return e.Service.MarkPaid(ctx, p, adv.ID, before.version, e9Date(t, e9PaidOn))
			})
			e9Refused(t, e, e9MsgNoCancel, 403, func() error { // a right before a version
				return e.Service.MarkPaid(ctx, p, adv.ID, before.version-1, e9Date(t, e9PaidOn))
			})
		}

		if err := e.Service.MarkPaid(ctx, e.P(t, e9Meena), adv.ID, before.version, e9Date(t, e9PaidOn)); err != nil {
			t.Fatalf("MarkPaid as meena: %v", err)
		}
		after := e9Read(t, e, adv.ID)
		if after.paidOn.String != e9PaidOn || after.version != before.version+1 || after.state != "approved" {
			t.Errorf("after MarkPaid: paid_on %v, version %d, state %q; want %s, %d, approved",
				after.paidOn, after.version, after.state, e9PaidOn, before.version+1)
		}
		rows := e9AuditRows(t, e, adv.ID, "paid")
		if len(rows) != 1 {
			t.Fatalf("audit rows with action paid = %d, want 1", len(rows))
		}
		if meena := int64(e.P(t, e9Meena).ID); rows[0].userID != meena {
			t.Errorf("paid audit row by user %d, want meena %d", rows[0].userID, meena)
		}
		if !strings.Contains(rows[0].changes, `"paid_on"`) || !strings.Contains(rows[0].changes, e9PaidOn) {
			t.Errorf("paid audit changes = %s, want the paid_on change to %s", rows[0].changes, e9PaidOn)
		}

		// deepak and priya can't undo either.
		for _, email := range []string{e9Deepak, e9Priya} {
			p := e.P(t, email)
			e9Refused(t, e, e9MsgNoCancel, 403, func() error {
				return e.Service.UndoPaid(ctx, p, adv.ID, after.version)
			})
		}

		// Undo paid with no claim on it: paid_on cleared, version up by 1,
		// one audit entry `unpaid` carrying the old date.
		if err := e.Service.UndoPaid(ctx, e.P(t, e9Meena), adv.ID, after.version); err != nil {
			t.Fatalf("UndoPaid as meena: %v", err)
		}
		undone := e9Read(t, e, adv.ID)
		if undone.paidOn.Valid || undone.version != after.version+1 || undone.state != "approved" {
			t.Errorf("after UndoPaid: paid_on %v, version %d, state %q; want NULL, %d, approved",
				undone.paidOn, undone.version, undone.state, after.version+1)
		}
		rows = e9AuditRows(t, e, adv.ID, "unpaid")
		if len(rows) != 1 {
			t.Fatalf("audit rows with action unpaid = %d, want 1", len(rows))
		}
		if !strings.Contains(rows[0].changes, `"paid_on"`) || !strings.Contains(rows[0].changes, e9PaidOn) {
			t.Errorf("unpaid audit changes = %s, want the paid_on change from %s", rows[0].changes, e9PaidOn)
		}
	})

	// The date rules, and their boundaries: the advance date and today pass.
	t.Run("dates", func(t *testing.T) {
		t.Parallel()
		e := expensetest.New(t)
		adv := e9Advance(t, e, doc.StateApproved)
		v := e9Read(t, e, adv.ID).version
		meena := e.P(t, e9Meena)
		e9Refused(t, e, e9MsgFuture, 0, func() error { return e.Service.MarkPaid(ctx, meena, adv.ID, v, e9Date(t, "2026-10-25")) })
		e9Refused(t, e, e9MsgBeforeDate, 0, func() error { return e.Service.MarkPaid(ctx, meena, adv.ID, v, e9Date(t, "2026-10-19")) })

		for _, on := range []string{e9AdvanceDate, "2026-10-24"} {
			e9Mark(t, e, adv.ID, on)
			if got := e9Read(t, e, adv.ID).paidOn.String; got != on {
				t.Errorf("paid_on = %q, want %q", got, on)
			}
			v = e9Read(t, e, adv.ID).version
			if err := e.Service.UndoPaid(ctx, meena, adv.ID, v); err != nil {
				t.Fatalf("UndoPaid: %v", err)
			}
		}
	})

	t.Run("already paid", func(t *testing.T) {
		t.Parallel()
		e := expensetest.New(t)
		adv := e9Advance(t, e, doc.StateApproved)
		e9Mark(t, e, adv.ID, e9PaidOn)
		v := e9Read(t, e, adv.ID).version
		e9Refused(t, e, e9MsgAlreadyPaid, 0, func() error {
			return e.Service.MarkPaid(ctx, e.P(t, e9Meena), adv.ID, v, e9Date(t, "2026-10-22"))
		})
	})

	t.Run("stale version", func(t *testing.T) {
		t.Parallel()
		e := expensetest.New(t)
		adv := e9Advance(t, e, doc.StateApproved)
		meena := e.P(t, e9Meena)
		v := e9Read(t, e, adv.ID).version
		e9Refused(t, e, e9MsgStale, 409, func() error { return e.Service.MarkPaid(ctx, meena, adv.ID, v-1, e9Date(t, e9PaidOn)) })
		e9Mark(t, e, adv.ID, e9PaidOn)
		e9Refused(t, e, e9MsgStale, 409, func() error { return e.Service.UndoPaid(ctx, meena, adv.ID, v) })
	})

	// Only an approved advance: pending, rejected, closed and cancelled are
	// refused.
	for _, tc := range []struct {
		name string
		to   func(t *testing.T, e *expensetest.Env) int64
	}{
		{"pending", func(t *testing.T, e *expensetest.Env) int64 { return e9Advance(t, e, doc.StatePending).ID }},
		{"rejected", func(t *testing.T, e *expensetest.Env) int64 {
			a := e9Advance(t, e, doc.StatePending)
			return e9Do(t, e, e9Priya, expense.AdvanceDocType, a.ID, doc.ActionReject, doc.ActionInput{Version: a.Version, Reason: "Not needed."}).ID
		}},
		{"closed", func(t *testing.T, e *expensetest.Env) int64 {
			a := e9Advance(t, e, doc.StateApproved)
			return e9Do(t, e, e9Meena, expense.AdvanceDocType, a.ID, doc.ActionClose, doc.ActionInput{Version: a.Version, Reason: "Trip called off."}).ID
		}},
		{"cancelled", func(t *testing.T, e *expensetest.Env) int64 {
			a := e9Advance(t, e, doc.StateApproved)
			return e9Do(t, e, e9Meena, expense.AdvanceDocType, a.ID, doc.ActionCancel, doc.ActionInput{Version: a.Version, Reason: "Trip called off."}).ID
		}},
	} {
		t.Run("not approved: "+tc.name, func(t *testing.T) {
			t.Parallel()
			e := expensetest.New(t)
			id := tc.to(t, e)
			v := e9Read(t, e, id).version
			e9Refused(t, e, e9MsgNotApproved, 0, func() error {
				return e.Service.MarkPaid(ctx, e.P(t, e9Meena), id, v, e9Date(t, e9PaidOn))
			})
		})
	}

	// kavya can't read Deepak's advance: the same 404 as a missing id
	// (CD-245), also with a stale version.
	t.Run("hidden or missing", func(t *testing.T) {
		t.Parallel()
		e := expensetest.New(t)
		adv := e9Advance(t, e, doc.StateApproved)
		v := e9Read(t, e, adv.ID).version
		kavya, meena := e.P(t, e9Kavya), e.P(t, e9Meena)
		for _, c := range []struct {
			name string
			do   func() error
		}{
			{"kavya marks", func() error { return e.Service.MarkPaid(ctx, kavya, adv.ID, v, e9Date(t, e9PaidOn)) }},
			{"kavya marks, stale", func() error { return e.Service.MarkPaid(ctx, kavya, adv.ID, v-1, e9Date(t, e9PaidOn)) }},
			{"meena marks a missing id", func() error { return e.Service.MarkPaid(ctx, meena, adv.ID+100, v, e9Date(t, e9PaidOn)) }},
		} {
			var got error
			e9Refused(t, e, auth.MsgNotFound, 404, func() error { got = c.do(); return got })
			if got != nil && !errors.Is(got, auth.ErrNotFound) {
				t.Errorf("%s: err %q isn't auth.ErrNotFound", c.name, got)
			}
		}
		e9Mark(t, e, adv.ID, e9PaidOn)
		v = e9Read(t, e, adv.ID).version
		e9Refused(t, e, auth.MsgNotFound, 404, func() error { return e.Service.UndoPaid(ctx, kavya, adv.ID, v) })
		e9Refused(t, e, auth.MsgNotFound, 404, func() error { return e.Service.UndoPaid(ctx, meena, adv.ID+100, v) })
	})

	t.Run("undo on an advance not marked as paid", func(t *testing.T) {
		t.Parallel()
		e := expensetest.New(t)
		adv := e9Advance(t, e, doc.StateApproved)
		v := e9Read(t, e, adv.ID).version
		e9Refused(t, e, e9MsgNotPaid, 0, func() error { return e.Service.UndoPaid(ctx, e.P(t, e9Meena), adv.ID, v) })
	})

	// Draft and rejected claims don't use the advance: undo is allowed.
	t.Run("undo with only draft and rejected claims", func(t *testing.T) {
		t.Parallel()
		e := expensetest.New(t)
		adv := e9Advance(t, e, doc.StateApproved)
		e9Mark(t, e, adv.ID, e9PaidOn)
		e9Claim(t, e, adv.ID, doc.StateDraft)
		e9Claim(t, e, adv.ID, doc.StateRejected)
		v := e9Read(t, e, adv.ID).version
		if err := e.Service.UndoPaid(ctx, e.P(t, e9Meena), adv.ID, v); err != nil {
			t.Fatalf("UndoPaid with a draft and a rejected claim naming the advance: %v", err)
		}
		if s := e9Read(t, e, adv.ID); s.paidOn.Valid || s.version != v+1 {
			t.Errorf("after UndoPaid: paid_on %v, version %d; want NULL, %d", s.paidOn, s.version, v+1)
		}
		if n := len(e9AuditRows(t, e, adv.ID, "unpaid")); n != 1 {
			t.Errorf("audit rows with action unpaid = %d, want 1", n)
		}
	})

	// A pending, approved or closed claim uses it: undo is refused.
	for _, state := range []doc.State{doc.StatePending, doc.StateApproved, doc.StateClosed} {
		t.Run("undo with a "+string(state)+" claim", func(t *testing.T) {
			t.Parallel()
			e := expensetest.New(t)
			adv := e9Advance(t, e, doc.StateApproved)
			e9Mark(t, e, adv.ID, e9PaidOn)
			e9Claim(t, e, adv.ID, state)
			v := e9Read(t, e, adv.ID).version
			e9Refused(t, e, e9MsgClaimUses, 0, func() error { return e.Service.UndoPaid(ctx, e.P(t, e9Meena), adv.ID, v) })
		})
	}

	// Cancelling a paid advance is refused by its ReverseEffects
	// (checkNotPaid); after Undo paid it cancels. PaidAdvance marks it with a
	// fixture query, so the refusal doesn't depend on MarkPaid.
	t.Run("cancel a paid advance", func(t *testing.T) {
		t.Parallel()
		e := expensetest.New(t)
		adv := e.PaidAdvance(t, e.P(t, e9Deepak), types.Money(500000))
		meena := e.P(t, e9Meena)
		v := e9Read(t, e, adv.ID).version
		// left is amount − used; the amount is E2's computed header, so the
		// case compares with the values before the cancel (research T3).
		used0, left0 := e.UsedLeft(t, adv.ID)
		if used0 != 0 {
			t.Fatalf("paid advance: used %d, want 0", used0)
		}
		e9Refused(t, e, e9MsgCancelPaid, 0, func() error {
			_, err := e.Engine.Do(ctx, meena, expense.AdvanceDocType, adv.ID, doc.ActionCancel, doc.ActionInput{Version: v, Reason: "Trip called off."})
			return err
		})
		if used, left := e.UsedLeft(t, adv.ID); used != 0 || left != left0 {
			t.Errorf("after the refused cancel: used %d, left %d; want 0, %d", used, left, left0)
		}

		if err := e.Service.UndoPaid(ctx, meena, adv.ID, v); err != nil {
			t.Fatalf("UndoPaid: %v", err)
		}
		v = e9Read(t, e, adv.ID).version
		res, err := e.Engine.Do(ctx, meena, expense.AdvanceDocType, adv.ID, doc.ActionCancel, doc.ActionInput{Version: v, Reason: "Trip called off."})
		if err != nil {
			t.Fatalf("cancel after UndoPaid: %v", err)
		}
		if s := e9Read(t, e, adv.ID); res.State != doc.StateCancelled || s.state != "cancelled" {
			t.Errorf("after cancel: result %q, stored %q; want cancelled", res.State, s.state)
		}
		if used, left := e.UsedLeft(t, adv.ID); used != 0 || left != left0 {
			t.Errorf("after cancel: used %d, left %d; want 0, %d", used, left, left0)
		}
	})

	// Two Directors (meena, and ravi given Director) mark the same advance at
	// once with the same version, each on its own connection: one wins, the
	// other gets DOC's stale text with 409 (CD-94, CD-213: non-admin).
	t.Run("two directors at once", func(t *testing.T) {
		t.Parallel()
		e := expensetest.New(t)
		e.AddRoles(t, e9Ravi, "Director")
		adv := e9Advance(t, e, doc.StateApproved)
		before := e9Read(t, e, adv.ID)
		users := []auth.Principal{e.P(t, e9Meena), e.P(t, e9Ravi)}
		errs := make([]error, len(users))
		on := e9Date(t, e9PaidOn)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i, p := range users {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs[i] = e.Service.MarkPaid(ctx, p, adv.ID, before.version, on)
			}()
		}
		close(start)
		wg.Wait()

		var won, stale int
		for i, err := range errs {
			var se interface{ Status() int }
			switch {
			case err == nil:
				won++
			case err.Error() == e9MsgStale && errors.As(err, &se) && se.Status() == 409:
				stale++
			default:
				t.Errorf("user %d: err %q (%T), want nil or the stale text with 409", i, err, err)
			}
		}
		if won != 1 || stale != 1 {
			t.Errorf("%d won and %d got the stale text, want 1 and 1", won, stale)
		}
		after := e9Read(t, e, adv.ID)
		if after.paidOn.String != e9PaidOn || after.version != before.version+1 {
			t.Errorf("after both: paid_on %v, version %d; want %s, %d", after.paidOn, after.version, e9PaidOn, before.version+1)
		}
		if n := len(e9AuditRows(t, e, adv.ID, "paid")); n != 1 {
			t.Errorf("audit rows with action paid = %d, want 1", n)
		}
	})
}
