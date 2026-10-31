package purchase_test

import (
	"database/sql"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// PO spec §27.1's messages, copied exactly (CD-147), and DOC's stale wording
// with the PO's display (plan P18; DOC exports no constructor).
const (
	msgExpectedNotApproved = "Only an approved order can have an expected date."
	msgExpectedBlank       = "Enter the expected date."
	msgExpectedBeforeOrder = "Expected on can't be before the order date."

	// History's words for the two audit entries (spec §27.1).
	auditExpectedCleared = "Expected on cleared"
)

// expectedTable is the Expected on record's table (spec §27.1).
const expectedTable = "purchase_order_expected"

func expDate(y int, m time.Month, d int) types.Date { return types.Date{Year: y, Month: m, Day: d} }

// expStale is DOC's stale refusal for display.
func expStale(display string) string {
	return "Someone else changed " + display + " after you opened it. Reload it and make your changes again."
}

// expAuditSet is the audit entry of a set to on, in History's words.
func expAuditSet(on types.Date) string { return "Expected on set to " + types.FormatDate(on) }

// expHistoryWords is an audit entry as History shows it (internal/app's
// auditAction for an action outside DOC's table, then ": " and the reason).
func expHistoryWords(e audit.Entry) string {
	if e.Reason != "" {
		return e.Action + ": " + e.Reason
	}
	return e.Action
}

// expAudits are the PO's audit entries, oldest first.
func expAudits(t *testing.T, env *purchasetest.Env, id int64) []audit.Entry {
	t.Helper()
	var out []audit.Entry
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		out, err = audit.ForDoc(t.Context(), tx, purchase.DocType, id)
		return err
	}); err != nil {
		t.Fatalf("audit of PO %d: %v", id, err)
	}
	return out
}

// expLoadPO is the stored PO.
func expLoadPO(t *testing.T, env *purchasetest.Env, id int64) doc.Doc {
	t.Helper()
	var d doc.Doc
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		d, err = doc.LoadDoc(t.Context(), tx, purchase.DocType, id)
		return err
	}); err != nil {
		t.Fatalf("load PO %d: %v", id, err)
	}
	return d
}

// expectedInfo is the stored Expected on record read with ExpectedInfo.
func expectedInfo(t *testing.T, env *purchasetest.Env, id int64) (purchase.Expected, bool) {
	t.Helper()
	var (
		got purchase.Expected
		ok  bool
	)
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		got, ok, err = purchase.ExpectedInfo(t.Context(), tx, id)
		return err
	}); err != nil {
		t.Fatalf("ExpectedInfo(%d): %v", id, err)
	}
	return got, ok
}

// expRowState is the stored row as plain SQL sees it: expected_on (ISO), note,
// set_by and version; ok false when there is no row.
type expRowState struct {
	on      string
	note    string
	setBy   int64
	version int
	ok      bool
}

func expectedRow(t *testing.T, env *purchasetest.Env, id int64) expRowState {
	t.Helper()
	var r expRowState
	err := env.DB.SQL().QueryRowContext(t.Context(),
		`SELECT expected_on, note, set_by, version FROM purchase_order_expected WHERE po_id = ?`, id).
		Scan(&r.on, &r.note, &r.setBy, &r.version)
	switch {
	case err == nil:
		r.ok = true
	case errors.Is(err, sql.ErrNoRows):
	default:
		t.Fatalf("read %s row of PO %d: %v", expectedTable, id, err)
	}
	return r
}

// expCounts are the row expCounts a refused Expected on write must leave alone:
// the Expected rows and the PO's audit entries.
type expCounts struct{ expected, audits int }

func expCountsOf(t *testing.T, env *purchasetest.Env, id int64) expCounts {
	t.Helper()
	return expCounts{
		expected: countRows(t, env.DB, expectedTable, "1 = 1"),
		audits:   len(expAudits(t, env, id)),
	}
}

// expWantRefused checks err is an HTTPError with status and exactly msg.
func expWantRefused(t *testing.T, what string, err error, status int, msg string) {
	t.Helper()
	var he doctype.HTTPError
	if !errors.As(err, &he) {
		t.Errorf("%s = %v (%T), want a doctype.HTTPError %d %q", what, err, err, status, msg)
		return
	}
	if he.Status() != status || err.Error() != msg {
		t.Errorf("%s = %d %q, want %d %q", what, he.Status(), err.Error(), status, msg)
	}
}

// expWantNotFound checks err is AUTH's 404 (CD-245: missing and hidden alike).
func expWantNotFound(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("%s = %v, want auth.ErrNotFound", what, err)
	}
	expWantRefused(t, what, err, http.StatusNotFound, auth.MsgNotFound)
}

// expWantDenied checks err is AUTH's 403 for amend or approve on Purchase Order
// (spec §27.1: anyone without either gets AUTH's 403 text).
func expWantDenied(t *testing.T, what string, err error) {
	t.Helper()
	var de *auth.DeniedError
	if !errors.As(err, &de) || !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("%s = %v (%T), want AUTH's *auth.DeniedError (403)", what, err, err)
		return
	}
	if de.Right != auth.RightAmend && de.Right != auth.RightApprove {
		t.Errorf("%s denied right %q, want amend or approve", what, de.Right)
	}
	want := (&auth.DeniedError{Right: de.Right, Label: "Purchase Order", Plural: "Purchase Orders"}).Error()
	expWantRefused(t, what, err, http.StatusForbidden, want)
}

// expTechparkOrder is an approved PRJ-TECHPARK / TECH-SITE PO made by Arjun
// (Purchase User, unrestricted) through Engine.Do, outside Priya's PRJ-METRO
// restriction. The builders always save as Priya, who may not create it.
func expTechparkOrder(t *testing.T, env *purchasetest.Env) int64 {
	t.Helper()
	ctx := t.Context()
	in := doc.ActionInput{
		Values: map[string]string{
			"vendor_id":  masterID(t, env.DB, "party", "SHREE"),
			"site_id":    masterID(t, env.DB, "site", "TECH-SITE"),
			"project_id": masterID(t, env.DB, "project", "PRJ-TECHPARK"),
			"order_date": types.Today(env.Clock).ISO(),
		},
		Lines: []map[string]string{{
			"item_id": masterID(t, env.DB, "item", "MS-ANGLE-50"), "uom_id": masterID(t, env.DB, "uom", "KGS"),
			"qty": "10", "price": "62.5", "tax_code_id": masterID(t, env.DB, "tax_code", "GST18"), "hsn_sac": "7216",
		}},
	}
	res, err := env.Engine.Do(ctx, env.Arjun, purchase.DocType, 0, doctype.ActionSave, in)
	if err != nil {
		t.Fatalf("save PRJ-TECHPARK PO as Arjun: %v", err)
	}
	res, err = env.Engine.Do(ctx, env.Arjun, purchase.DocType, res.ID, doctype.ActionSubmit, doc.ActionInput{Version: res.Version})
	if err != nil {
		t.Fatalf("submit PRJ-TECHPARK PO as Arjun: %v", err)
	}
	if res.State != doctype.StateApproved {
		t.Fatalf("PRJ-TECHPARK PO is %s after submit, want approved (below every rule)", res.State)
	}
	return res.ID
}

// PO-080 (PO spec §27.1; CD-272 Gap 7; CD-94, CD-245, CD-259; R7): the
// Expected on record of an approved PO, on the real auth.Service. Arjun
// (Purchase User, amend) sets it with posted version 0 (row version 1), Ravi
// (Purchase Manager, approve) changes it with 1 (version 2), and a clear with
// 2 deletes it; each writes History's audit entry on the PO, and the PO row's
// version and updated_at never move. Suresh (Stores, read only) gets AUTH's
// 403, and a missing or hidden PO AUTH's 404. Each refusal is its exact text
// and leaves the row expCounts unchanged. Checks run load, right, version, then
// state and date (CD-259). Two non-admin SetExpected calls with version 0
// race: one inserts, the other is a 409.
func TestPO_ExpectedOn(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	svc := env.Service

	po := purchasetest.NewOrder(t, env)
	display := naming.Label(po.Number, po.ID)
	nov5, oct24 := expDate(2026, time.November, 5), expDate(2026, time.October, 24)

	t.Run("do and undo", func(t *testing.T) {
		before := expLoadPO(t, env, po.ID)
		audits0 := len(expAudits(t, env, po.ID))

		if st, err := svc.ExpectedState(ctx, env.Arjun, po.ID); err != nil || st.Version != 0 || st.Refusal != "" {
			t.Errorf("ExpectedState before any set = %+v, %v; want version 0, no refusal", st, err)
		}

		// Arjun sets 05-11-2026 with posted version 0: insert at version 1.
		if err := svc.SetExpected(ctx, env.Arjun, po.ID, 0, nov5, "Vendor called"); err != nil {
			t.Fatalf("SetExpected as Arjun, version 0: %v", err)
		}
		if r := expectedRow(t, env, po.ID); !r.ok || r.on != "2026-11-05" || r.note != "Vendor called" || r.setBy != int64(env.Arjun.ID) || r.version != 1 {
			t.Errorf("row after Arjun's set = %+v, want 2026-11-05, note, set_by Arjun, version 1", r)
		}
		if got, ok := expectedInfo(t, env, po.ID); !ok || got.ExpectedOn != nov5 || got.Note != "Vendor called" || got.Version != 1 {
			t.Errorf("ExpectedInfo after set = %+v, %v; want 05-11-2026, the note, version 1", got, ok)
		}
		if st, err := svc.ExpectedState(ctx, env.Arjun, po.ID); err != nil || st.Version != 1 || st.Refusal != "" {
			t.Errorf("ExpectedState after set = %+v, %v; want version 1, no refusal", st, err)
		}

		// Ravi changes it to the order date with posted version 1: version 2.
		if err := svc.SetExpected(ctx, env.Ravi, po.ID, 1, oct24, ""); err != nil {
			t.Fatalf("SetExpected as Ravi, version 1: %v", err)
		}
		if r := expectedRow(t, env, po.ID); !r.ok || r.on != "2026-10-24" || r.note != "" || r.setBy != int64(env.Ravi.ID) || r.version != 2 {
			t.Errorf("row after Ravi's change = %+v, want 2026-10-24, no note, set_by Ravi, version 2", r)
		}

		// Clear with posted version 2 deletes the row.
		if err := svc.ClearExpected(ctx, env.Arjun, po.ID, 2); err != nil {
			t.Fatalf("ClearExpected as Arjun, version 2: %v", err)
		}
		if r := expectedRow(t, env, po.ID); r.ok {
			t.Errorf("row after clear = %+v, want none", r)
		}
		if got, ok := expectedInfo(t, env, po.ID); ok {
			t.Errorf("ExpectedInfo after clear = %+v, true; want no row", got)
		}
		if st, err := svc.ExpectedState(ctx, env.Arjun, po.ID); err != nil || st.Version != 0 {
			t.Errorf("ExpectedState after clear = %+v, %v; want version 0", st, err)
		}

		// One audit entry per change, on the PO, by its user, in History's words.
		got := expAudits(t, env, po.ID)[audits0:]
		want := []struct {
			words string
			by    auth.UserID
		}{{expAuditSet(nov5), env.Arjun.ID}, {expAuditSet(oct24), env.Ravi.ID}, {auditExpectedCleared, env.Arjun.ID}}
		if len(got) != len(want) {
			t.Fatalf("new audit entries = %d (%+v), want %d", len(got), got, len(want))
		}
		for i, w := range want {
			if expHistoryWords(got[i]) != w.words || got[i].By != w.by || got[i].DocType != purchase.DocType || got[i].DocID != po.ID {
				t.Errorf("audit entry %d = %q by %d on %s %d, want %q by %d on the PO", i, expHistoryWords(got[i]), got[i].By, got[i].DocType, got[i].DocID, w.words, w.by)
			}
		}

		// R7: the PO row itself never changes.
		after := expLoadPO(t, env, po.ID)
		if after.Version != before.Version || !after.UpdatedAt.Equal(before.UpdatedAt) || after.State != doctype.StateApproved {
			t.Errorf("PO after set, change and clear: version %d updated_at %v state %s; want %d %v approved (R7)",
				after.Version, after.UpdatedAt, after.State, before.Version, before.UpdatedAt)
		}

		// After the clear the record is gone, so a new set posts 0 again.
		if err := svc.SetExpected(ctx, env.Ravi, po.ID, 0, nov5, ""); err != nil {
			t.Fatalf("SetExpected again with version 0 after the clear: %v", err)
		}
		if r := expectedRow(t, env, po.ID); !r.ok || r.version != 1 {
			t.Errorf("row after the set again = %+v, want version 1", r)
		}
		if err := svc.ClearExpected(ctx, env.Ravi, po.ID, 1); err != nil {
			t.Fatalf("ClearExpected as Ravi, version 1: %v", err)
		}
	})

	t.Run("past date allowed", func(t *testing.T) {
		env.SetNow(t, time.Date(2026, time.November, 10, 10, 30, 0, 0, time.UTC))
		if err := svc.SetExpected(ctx, env.Arjun, po.ID, 0, expDate(2026, time.November, 6), ""); err != nil {
			t.Fatalf("SetExpected 06-11-2026 on 10-11-2026 (a late update): %v", err)
		}
		if r := expectedRow(t, env, po.ID); !r.ok || r.on != "2026-11-06" || r.version != 1 {
			t.Errorf("row after the late update = %+v, want 2026-11-06 at version 1", r)
		}
		if err := svc.ClearExpected(ctx, env.Arjun, po.ID, 1); err != nil {
			t.Fatalf("ClearExpected: %v", err)
		}
	})

	draft := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	hidden := expTechparkOrder(t, env)

	// Each refusal by its exact text, with the row expCounts unchanged. With a
	// row at version 1 on po, so "stale" cases have a current version.
	if err := svc.SetExpected(ctx, env.Arjun, po.ID, 0, nov5, ""); err != nil {
		t.Fatalf("SetExpected for the refusal cases: %v", err)
	}
	refusals := []struct {
		name   string
		call   func() error
		id     int64
		status int
		msg    string
	}{
		{"set on a draft", func() error { return svc.SetExpected(ctx, env.Priya, draft.ID, 0, nov5, "") }, draft.ID, http.StatusUnprocessableEntity, msgExpectedNotApproved},
		{"blank date", func() error { return svc.SetExpected(ctx, env.Arjun, po.ID, 1, types.Date{}, "") }, po.ID, http.StatusUnprocessableEntity, msgExpectedBlank},
		{"before the order date", func() error { return svc.SetExpected(ctx, env.Arjun, po.ID, 1, expDate(2026, time.October, 23), "") }, po.ID, http.StatusUnprocessableEntity, msgExpectedBeforeOrder},
		{"set stale: posted 0 with a row", func() error { return svc.SetExpected(ctx, env.Ravi, po.ID, 0, oct24, "") }, po.ID, http.StatusConflict, expStale(display)},
		{"set stale: posted 2 at version 1", func() error { return svc.SetExpected(ctx, env.Ravi, po.ID, 2, oct24, "") }, po.ID, http.StatusConflict, expStale(display)},
		{"set stale: posted 3 with no row", func() error { return svc.SetExpected(ctx, env.Arjun, draft.ID, 3, nov5, "") }, draft.ID, http.StatusConflict, expStale(naming.Label(draft.Number, draft.ID))},
		{"clear stale: posted 2 at version 1", func() error { return svc.ClearExpected(ctx, env.Ravi, po.ID, 2) }, po.ID, http.StatusConflict, expStale(display)},
		// CD-259 order: version before state and date rules.
		{"stale and blank: version first", func() error { return svc.SetExpected(ctx, env.Arjun, po.ID, 5, types.Date{}, "") }, po.ID, http.StatusConflict, expStale(display)},
		{"stale and before the order date: version first", func() error { return svc.SetExpected(ctx, env.Arjun, po.ID, 5, expDate(2026, time.October, 1), "") }, po.ID, http.StatusConflict, expStale(display)},
	}
	for _, c := range refusals {
		t.Run(c.name, func(t *testing.T) {
			before, row := expCountsOf(t, env, c.id), expectedRow(t, env, po.ID)
			expWantRefused(t, c.name, c.call(), c.status, c.msg)
			if after := expCountsOf(t, env, c.id); after != before {
				t.Errorf("row expCounts after the refusal = %+v, want %+v", after, before)
			}
			if after := expectedRow(t, env, po.ID); after != row {
				t.Errorf("po's row after the refusal = %+v, want %+v", after, row)
			}
		})
	}

	t.Run("rights", func(t *testing.T) {
		before, row := expCountsOf(t, env, po.ID), expectedRow(t, env, po.ID)
		// Suresh (Stores) reads the PO but holds neither amend nor approve.
		expWantDenied(t, "SetExpected as Suresh", svc.SetExpected(ctx, env.Suresh, po.ID, 1, nov5, ""))
		expWantDenied(t, "ClearExpected as Suresh", svc.ClearExpected(ctx, env.Suresh, po.ID, 1))
		// CD-259: the right comes before the version.
		expWantDenied(t, "SetExpected as Suresh with a stale version", svc.SetExpected(ctx, env.Suresh, po.ID, 7, nov5, ""))
		expWantDenied(t, "ClearExpected as Suresh with a stale version", svc.ClearExpected(ctx, env.Suresh, po.ID, 7))
		// A missing PO and a hidden one (Priya, PRJ-METRO only, on a
		// PRJ-TECHPARK PO) are AUTH's 404, before right and version (CD-245).
		expWantNotFound(t, "SetExpected on a missing PO", svc.SetExpected(ctx, env.Arjun, 999_999, 0, nov5, ""))
		expWantNotFound(t, "ClearExpected on a missing PO", svc.ClearExpected(ctx, env.Arjun, 999_999, 1))
		expWantNotFound(t, "SetExpected as Priya on a PRJ-TECHPARK PO", svc.SetExpected(ctx, env.Priya, hidden, 0, nov5, ""))
		expWantNotFound(t, "SetExpected as Priya on a PRJ-TECHPARK PO, stale", svc.SetExpected(ctx, env.Priya, hidden, 4, nov5, ""))
		expWantNotFound(t, "ClearExpected as Priya on a PRJ-TECHPARK PO", svc.ClearExpected(ctx, env.Priya, hidden, 1))
		if after := expCountsOf(t, env, po.ID); after != before {
			t.Errorf("row expCounts after the refused rights = %+v, want %+v", after, before)
		}
		if after := expectedRow(t, env, po.ID); after != row {
			t.Errorf("po's row after the refused rights = %+v, want %+v", after, row)
		}
		if n := countRows(t, env.DB, expectedTable, "po_id = ?", hidden); n != 0 {
			t.Errorf("rows on the hidden PO = %d, want 0", n)
		}
		// The owner (Priya, Purchase User) may set it on her own PO.
		if err := svc.SetExpected(ctx, env.Priya, po.ID, 1, oct24, ""); err != nil {
			t.Errorf("SetExpected as the owner Priya: %v", err)
		}
	})

	t.Run("ExpectedState", func(t *testing.T) {
		if _, err := svc.ExpectedState(ctx, env.Suresh, po.ID); err == nil {
			t.Error("ExpectedState as Suresh = nil error, want AUTH's 403")
		} else {
			expWantDenied(t, "ExpectedState as Suresh", err)
		}
		_, err := svc.ExpectedState(ctx, env.Arjun, 999_999)
		expWantNotFound(t, "ExpectedState on a missing PO", err)
		_, err = svc.ExpectedState(ctx, env.Priya, hidden)
		expWantNotFound(t, "ExpectedState as Priya on a PRJ-TECHPARK PO", err)

		// Refusal holds only the state refusal.
		if st, err := svc.ExpectedState(ctx, env.Priya, draft.ID); err != nil || st.Refusal != msgExpectedNotApproved || st.Version != 0 {
			t.Errorf("ExpectedState on a draft = %+v, %v; want Refusal %q, version 0", st, err, msgExpectedNotApproved)
		}
		if st, err := svc.ExpectedState(ctx, env.Ravi, po.ID); err != nil || st.Refusal != "" || st.Version != 2 {
			t.Errorf("ExpectedState on the approved PO = %+v, %v; want no Refusal, version 2", st, err)
		}
	})

	// Concurrency (CD-94; lessons §11: as non-admins, since Can returns early
	// for an Administrator): two SetExpected calls with posted version 0 on a
	// PO with no row race; one inserts at version 1 and the other is DOC's
	// stale 409, never a second row, an overwrite or a raw constraint error.
	t.Run("concurrent version 0", func(t *testing.T) {
		po := purchasetest.NewOrder(t, env)
		display := naming.Label(po.Number, po.ID)

		users := []auth.Principal{env.Arjun, env.Ravi}
		ons := []types.Date{expDate(2026, time.November, 5), expDate(2026, time.November, 9)}
		errs := make([]error, len(users))
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range users {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs[i] = env.Service.SetExpected(ctx, users[i], po.ID, 0, ons[i], "")
			}()
		}
		close(start)
		wg.Wait()

		won := -1
		for i, err := range errs {
			if err == nil {
				if won >= 0 {
					t.Fatalf("both SetExpected calls with version 0 succeeded, want one 409")
				}
				won = i
			}
		}
		if won < 0 {
			t.Fatalf("both SetExpected calls failed: %v; want one to insert", errs)
		}
		expWantRefused(t, "the losing SetExpected", errs[1-won], http.StatusConflict, expStale(display))
		if n := countRows(t, env.DB, expectedTable, "po_id = ?", po.ID); n != 1 {
			t.Errorf("rows = %d, want 1", n)
		}
		if r := expectedRow(t, env, po.ID); r.version != 1 || r.on != ons[won].ISO() || r.setBy != int64(users[won].ID) {
			t.Errorf("row = %+v, want the winner's %s at version 1", r, ons[won].ISO())
		}
		var sets int
		for _, e := range expAudits(t, env, po.ID) {
			if expHistoryWords(e) == expAuditSet(ons[0]) || expHistoryWords(e) == expAuditSet(ons[1]) {
				sets++
			}
		}
		if sets != 1 {
			t.Errorf("Expected on audit entries = %d, want 1", sets)
		}
	})
}
