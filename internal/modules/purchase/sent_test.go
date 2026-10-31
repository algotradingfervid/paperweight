package purchase_test

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
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

// PO spec §23's messages, copied exactly (CD-147), and the audit entries'
// actions.
const (
	msgSentNotApproved = "Only an approved order can be marked as sent."
	msgSentFuture      = "Sent on can't be in the future."
	msgSentBeforeOrder = "Sent on can't be before the order date."
	msgSentNoMethod    = "Choose how it was sent."

	auditMarkedSent    = "Marked as sent"
	auditMarkedNotSent = "Marked as not sent"
)

// sentTable is the Sent record's table (spec §23).
const sentTable = "purchase_order_sent"

func sentDate(y int, m time.Month, d int) types.Date { return types.Date{Year: y, Month: m, Day: d} }

// sentStale is DOC's stale refusal for display (doc/messages.go msgStale).
func sentStale(display string) string {
	return "Someone else changed " + display + " after you opened it. Reload it and make your changes again."
}

// sentAudits are the PO's audit entries, oldest first.
func sentAudits(t *testing.T, env *purchasetest.Env, id int64) []audit.Entry {
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

// sentLoadPO is the stored PO.
func sentLoadPO(t *testing.T, env *purchasetest.Env, id int64) doc.Doc {
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

// sentInfo is the stored Sent record read with purchase.SentInfo.
func sentInfo(t *testing.T, env *purchasetest.Env, id int64) (purchase.Sent, bool) {
	t.Helper()
	var (
		got purchase.Sent
		ok  bool
	)
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		got, ok, err = purchase.SentInfo(t.Context(), tx, id)
		return err
	}); err != nil {
		t.Fatalf("SentInfo(%d): %v", id, err)
	}
	return got, ok
}

// sentRowState is the stored row as plain SQL sees it; ok false when there is
// no row.
type sentRowState struct {
	on, via, note string
	by            int64
	version       int
	ok            bool
}

func sentRow(t *testing.T, env *purchasetest.Env, id int64) sentRowState {
	t.Helper()
	var r sentRowState
	err := env.DB.SQL().QueryRowContext(t.Context(),
		`SELECT sent_on, sent_via, note, sent_by, version FROM purchase_order_sent WHERE po_id = ?`, id).
		Scan(&r.on, &r.via, &r.note, &r.by, &r.version)
	switch {
	case err == nil:
		r.ok = true
	case errors.Is(err, sql.ErrNoRows):
	default:
		t.Fatalf("read %s row of PO %d: %v", sentTable, id, err)
	}
	return r
}

// sentCounts are the row counts a refused Sent write must leave alone: every
// Sent row, the PO's audit entries and the PO's version.
type sentCounts struct{ sent, audits, poVersion int }

func sentCountsOf(t *testing.T, env *purchasetest.Env, id int64) sentCounts {
	t.Helper()
	return sentCounts{
		sent:      countRows(t, env.DB, sentTable, "1 = 1"),
		audits:    len(sentAudits(t, env, id)),
		poVersion: sentLoadPO(t, env, id).Version,
	}
}

// sentWantRefused checks err is an HTTPError with status and exactly msg.
func sentWantRefused(t *testing.T, what string, err error, status int, msg string) {
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

// sentWantField checks err's field errors are exactly msg on field, so the
// Send page shows it at its field (UX 5.13, PUX-018).
func sentWantField(t *testing.T, what string, err error, field, msg string) {
	t.Helper()
	var he doctype.HTTPError
	if !errors.As(err, &he) {
		return // sentWantRefused reports it
	}
	fe := he.FieldErrors()
	if len(fe) != 1 || fe[0].Field != field || fe[0].Message != msg {
		t.Errorf("%s field errors = %+v, want exactly {Field: %q, Message: %q}", what, fe, field, msg)
	}
}

// sentWantNotFound checks err is AUTH's 404 (CD-245: missing and hidden alike).
func sentWantNotFound(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("%s = %v, want auth.ErrNotFound", what, err)
	}
	sentWantRefused(t, what, err, http.StatusNotFound, auth.MsgNotFound)
}

// sentRef is the default METRO-SITE / PRJ-METRO PO as AUTH sees it.
func sentRef(t *testing.T, env *purchasetest.Env, po doc.Doc) *auth.RecordRef {
	t.Helper()
	id := func(table, code string) int64 {
		n, err := strconv.ParseInt(masterID(t, env.DB, table, code), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	return &auth.RecordRef{
		DocType: purchase.DocType, ID: po.ID, CreatedBy: po.CreatedBy,
		ProjectIDs: []int64{id("project", "PRJ-METRO")}, SiteIDs: []int64{id("site", "METRO-SITE")},
	}
}

// sentWantDenied checks err is AUTH's 403 (spec §23: anyone who is neither
// the owner, with write, nor holds approve gets AUTH's 403 text). The text is
// exactly what the real auth.Service answers p for that right on the record.
func sentWantDenied(t *testing.T, env *purchasetest.Env, what string, err error, p auth.Principal, rec *auth.RecordRef) {
	t.Helper()
	var de *auth.DeniedError
	if !errors.As(err, &de) || !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("%s = %v (%T), want AUTH's *auth.DeniedError (403)", what, err, err)
		return
	}
	if de.Right != auth.RightWrite && de.Right != auth.RightApprove {
		t.Errorf("%s denied right %q, want write or approve", what, de.Right)
		return
	}
	authErr := env.Auth.Can(t.Context(), p, purchase.DocType, de.Right, rec)
	if authErr == nil {
		t.Errorf("%s: AUTH lets %s %s the PO, yet the call was denied", what, p.Name, de.Right)
		return
	}
	sentWantRefused(t, what, err, http.StatusForbidden, authErr.Error())
}

// sentTechparkOrder is an approved PRJ-TECHPARK / TECH-SITE PO made by Arjun
// (Purchase User, unrestricted) through Engine.Do: hidden from Priya
// (PRJ-METRO only). The builders always save as Priya, who may not create it.
func sentTechparkOrder(t *testing.T, env *purchasetest.Env) int64 {
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

// PO-063 (PO spec §23, §26; CD-247 (6), CD-253 (3), CD-94, CD-245, CD-259;
// R7): the Sent record of an approved PO, on the real auth.Service. Priya
// (the owner) marks it as sent with posted version 0 (insert, row version 1),
// Ravi (Purchase Manager, approve) changes it with 1 (version 2), and Mark
// as not sent with 2 deletes it; each writes `Marked as sent` or `Marked as
// not sent` on the PO, and the PO row's version and updated_at never move.
// Meena (Director) and the Administrator may do it too; Arjun (another
// Purchase User, not the owner) and Suresh (Stores) get AUTH's 403, and a
// missing or hidden PO AUTH's 404. Each refusal is its exact text and leaves
// the row counts unchanged. Checks run load, right, version, state, then
// dates (CD-259). Two non-admin posts with version 0 race: one inserts, the
// other is a 409. sent_via takes only the five values.
func TestPO_MarkSent(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	svc := env.Service

	po := purchasetest.NewOrder(t, env) // ordered 24-10-2026 (authtest.T0)
	display := naming.Label(po.Number, po.ID)
	rec := sentRef(t, env, po)
	draft := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	hidden := sentTechparkOrder(t, env)

	// Today is 31-10-2026 (10:30 IST) from here on.
	env.SetNow(t, time.Date(2026, time.October, 31, 5, 0, 0, 0, time.UTC))
	oct31, oct30, oct24 := sentDate(2026, time.October, 31), sentDate(2026, time.October, 30), sentDate(2026, time.October, 24)

	t.Run("do and undo", func(t *testing.T) {
		before := sentLoadPO(t, env, po.ID)
		audits0 := len(sentAudits(t, env, po.ID))

		if _, ok := sentInfo(t, env, po.ID); ok {
			t.Fatalf("SentInfo before any mark = ok, want no row")
		}

		// Priya marks it on today, by WhatsApp, posted version 0: insert at 1.
		if err := svc.MarkSent(ctx, env.Priya, po.ID, 0, purchase.SentInput{SentOn: oct31, SentVia: "whatsapp", Note: "Sent to Mr Shah"}); err != nil {
			t.Fatalf("MarkSent as Priya, version 0: %v", err)
		}
		if r := sentRow(t, env, po.ID); !r.ok || r.on != "2026-10-31" || r.via != "whatsapp" || r.note != "Sent to Mr Shah" || r.by != int64(env.Priya.ID) || r.version != 1 {
			t.Errorf("row after Priya's mark = %+v, want 2026-10-31, whatsapp, the note, sent_by Priya, version 1", r)
		}
		if got, ok := sentInfo(t, env, po.ID); !ok || got.SentOn != oct31 || got.SentVia != "whatsapp" || got.Note != "Sent to Mr Shah" || got.SentBy != env.Priya.ID || got.Version != 1 || !got.SentAt.Equal(env.Clock.Now()) {
			t.Errorf("SentInfo after mark = %+v, %v; want 31-10-2026, whatsapp, the note, by Priya at %v, version 1", got, ok, env.Clock.Now())
		}

		// Ravi changes it to the order date by courier with posted 1: version 2.
		if err := svc.MarkSent(ctx, env.Ravi, po.ID, 1, purchase.SentInput{SentOn: oct24, SentVia: "courier"}); err != nil {
			t.Fatalf("MarkSent as Ravi, version 1: %v", err)
		}
		if r := sentRow(t, env, po.ID); !r.ok || r.on != "2026-10-24" || r.via != "courier" || r.note != "" || r.by != int64(env.Ravi.ID) || r.version != 2 {
			t.Errorf("row after Ravi's change = %+v, want 2026-10-24, courier, no note, sent_by Ravi, version 2", r)
		}
		if n := countRows(t, env.DB, sentTable, "po_id = ?", po.ID); n != 1 {
			t.Errorf("rows after the update = %d, want 1 (update, not a second row)", n)
		}

		// Mark as not sent with posted 2 deletes the row.
		if err := svc.UnmarkSent(ctx, env.Priya, po.ID, 2); err != nil {
			t.Fatalf("UnmarkSent as Priya, version 2: %v", err)
		}
		if r := sentRow(t, env, po.ID); r.ok {
			t.Errorf("row after Mark as not sent = %+v, want none", r)
		}
		if got, ok := sentInfo(t, env, po.ID); ok {
			t.Errorf("SentInfo after Mark as not sent = %+v, true; want no row", got)
		}

		// One audit entry per change, on the PO, by its user.
		got := sentAudits(t, env, po.ID)[audits0:]
		want := []struct {
			action string
			by     auth.UserID
		}{{auditMarkedSent, env.Priya.ID}, {auditMarkedSent, env.Ravi.ID}, {auditMarkedNotSent, env.Priya.ID}}
		if len(got) != len(want) {
			t.Fatalf("new audit entries = %d (%+v), want %d", len(got), got, len(want))
		}
		for i, w := range want {
			if got[i].Action != w.action || got[i].By != w.by || got[i].DocType != purchase.DocType || got[i].DocID != po.ID {
				t.Errorf("audit entry %d = %q by %d on %s %d, want %q by %d on the PO", i, got[i].Action, got[i].By, got[i].DocType, got[i].DocID, w.action, w.by)
			}
		}

		// R7: the PO row itself never changes.
		after := sentLoadPO(t, env, po.ID)
		if after.Version != before.Version || !after.UpdatedAt.Equal(before.UpdatedAt) || after.UpdatedBy != before.UpdatedBy || after.State != doctype.StateApproved {
			t.Errorf("PO after mark, change and unmark: version %d updated_at %v by %d state %s; want %d %v %d approved (R7)",
				after.Version, after.UpdatedAt, after.UpdatedBy, after.State, before.Version, before.UpdatedAt, before.UpdatedBy)
		}

		// With the row gone a new mark posts 0 again.
		if err := svc.MarkSent(ctx, env.Priya, po.ID, 0, purchase.SentInput{SentOn: oct30, SentVia: "email"}); err != nil {
			t.Fatalf("MarkSent again with version 0 after the delete: %v", err)
		}
		if r := sentRow(t, env, po.ID); !r.ok || r.version != 1 || r.on != "2026-10-30" {
			t.Errorf("row after marking again = %+v, want 2026-10-30 at version 1", r)
		}
		if err := svc.UnmarkSent(ctx, env.Priya, po.ID, 1); err != nil {
			t.Fatalf("UnmarkSent, version 1: %v", err)
		}
	})

	t.Run("who may", func(t *testing.T) {
		// Meena (Director, approve) and the Administrator mark and unmark.
		for _, p := range []auth.Principal{env.Meena, env.Admin} {
			if err := svc.MarkSent(ctx, p, po.ID, 0, purchase.SentInput{SentOn: oct31, SentVia: "by_hand"}); err != nil {
				t.Errorf("MarkSent as %s: %v", p.Name, err)
				continue
			}
			if r := sentRow(t, env, po.ID); !r.ok || r.by != int64(p.ID) || r.version != 1 {
				t.Errorf("row after %s's mark = %+v, want sent_by %d at version 1", p.Name, r, p.ID)
			}
			if err := svc.UnmarkSent(ctx, p, po.ID, 1); err != nil {
				t.Errorf("UnmarkSent as %s: %v", p.Name, err)
			}
		}
		if r := sentRow(t, env, po.ID); r.ok {
			t.Errorf("row after Meena and the Administrator = %+v, want none", r)
		}
	})

	t.Run("sent_via", func(t *testing.T) {
		version := 0
		for _, via := range []string{"email", "whatsapp", "courier", "by_hand", "other"} {
			if err := svc.MarkSent(ctx, env.Priya, po.ID, version, purchase.SentInput{SentOn: oct31, SentVia: via}); err != nil {
				t.Errorf("MarkSent by %q: %v", via, err)
				continue
			}
			version++
			if r := sentRow(t, env, po.ID); r.via != via || r.version != version {
				t.Errorf("row after %q = %+v, want sent_via %q at version %d", via, r, via, version)
			}
		}
		for _, via := range []string{"fax", "Email", "WhatsApp", "by hand", " email"} {
			before, row := sentCountsOf(t, env, po.ID), sentRow(t, env, po.ID)
			err := svc.MarkSent(ctx, env.Priya, po.ID, version, purchase.SentInput{SentOn: oct31, SentVia: via})
			if err == nil {
				t.Errorf("MarkSent by %q = nil, want it refused (only email, whatsapp, courier, by_hand and other)", via)
			}
			// AD-13 fix-1: the table's CHECK is the backstop, a plain 500;
			// no user-facing refusal is invented.
			var he doctype.HTTPError
			if errors.As(err, &he) {
				t.Errorf("MarkSent by %q = %d %q, want a plain error, not a doctype.HTTPError", via, he.Status(), err.Error())
			}
			if after := sentCountsOf(t, env, po.ID); after != before {
				t.Errorf("counts after %q = %+v, want %+v", via, after, before)
			}
			if after := sentRow(t, env, po.ID); after != row {
				t.Errorf("row after %q = %+v, want %+v", via, after, row)
			}
		}
		if err := svc.UnmarkSent(ctx, env.Priya, po.ID, version); err != nil {
			t.Fatalf("UnmarkSent, version %d: %v", version, err)
		}
	})

	// A row at version 1 on po for the refusals, so stale cases have a
	// current version.
	if err := svc.MarkSent(ctx, env.Priya, po.ID, 0, purchase.SentInput{SentOn: oct30, SentVia: "email"}); err != nil {
		t.Errorf("MarkSent for the refusal cases: %v (the cases below then fail too)", err)
	}
	draftDisplay := naming.Label(draft.Number, draft.ID)
	in := func(on types.Date, via string) purchase.SentInput {
		return purchase.SentInput{SentOn: on, SentVia: via}
	}
	nov1, oct23 := sentDate(2026, time.November, 1), sentDate(2026, time.October, 23)

	refusals := []struct {
		name   string
		call   func() error
		id     int64
		status int
		msg    string
		field  string // "" = no field check
	}{
		{"mark a draft", func() error { return svc.MarkSent(ctx, env.Priya, draft.ID, 0, in(oct31, "email")) }, draft.ID, http.StatusUnprocessableEntity, msgSentNotApproved, ""},
		{"future date", func() error { return svc.MarkSent(ctx, env.Priya, po.ID, 1, in(nov1, "email")) }, po.ID, http.StatusUnprocessableEntity, msgSentFuture, "sent_on"},
		{"before the order date", func() error { return svc.MarkSent(ctx, env.Priya, po.ID, 1, in(oct23, "email")) }, po.ID, http.StatusUnprocessableEntity, msgSentBeforeOrder, "sent_on"},
		{"no method chosen", func() error { return svc.MarkSent(ctx, env.Priya, po.ID, 1, in(oct31, "")) }, po.ID, http.StatusUnprocessableEntity, msgSentNoMethod, "sent_via"},
		{"mark stale: posted 0 with a row", func() error { return svc.MarkSent(ctx, env.Ravi, po.ID, 0, in(oct31, "email")) }, po.ID, http.StatusConflict, sentStale(display), ""},
		{"mark stale: posted 2 at version 1", func() error { return svc.MarkSent(ctx, env.Ravi, po.ID, 2, in(oct31, "email")) }, po.ID, http.StatusConflict, sentStale(display), ""},
		{"unmark stale: posted 2 at version 1", func() error { return svc.UnmarkSent(ctx, env.Priya, po.ID, 2) }, po.ID, http.StatusConflict, sentStale(display), ""},
		{"unmark stale: posted 0 at version 1", func() error { return svc.UnmarkSent(ctx, env.Priya, po.ID, 0) }, po.ID, http.StatusConflict, sentStale(display), ""},
		{"unmark stale: posted 1 with no row", func() error { return svc.UnmarkSent(ctx, env.Priya, draft.ID, 1) }, draft.ID, http.StatusConflict, sentStale(draftDisplay), ""},
		// CD-259 order: version before state, state before the date rules.
		{"stale draft: version first", func() error { return svc.MarkSent(ctx, env.Priya, draft.ID, 3, in(oct31, "email")) }, draft.ID, http.StatusConflict, sentStale(draftDisplay), ""},
		{"stale and future: version first", func() error { return svc.MarkSent(ctx, env.Priya, po.ID, 5, in(nov1, "email")) }, po.ID, http.StatusConflict, sentStale(display), ""},
		{"stale and no method: version first", func() error { return svc.MarkSent(ctx, env.Priya, po.ID, 5, in(oct31, "")) }, po.ID, http.StatusConflict, sentStale(display), ""},
		{"draft and future: state first", func() error { return svc.MarkSent(ctx, env.Priya, draft.ID, 0, in(nov1, "email")) }, draft.ID, http.StatusUnprocessableEntity, msgSentNotApproved, ""},
		{"draft and before the order date: state first", func() error { return svc.MarkSent(ctx, env.Priya, draft.ID, 0, in(oct23, "email")) }, draft.ID, http.StatusUnprocessableEntity, msgSentNotApproved, ""},
		{"draft and no method: state first", func() error { return svc.MarkSent(ctx, env.Priya, draft.ID, 0, in(oct31, "")) }, draft.ID, http.StatusUnprocessableEntity, msgSentNotApproved, ""},
	}
	for _, c := range refusals {
		t.Run(c.name, func(t *testing.T) {
			before, row := sentCountsOf(t, env, c.id), sentRow(t, env, po.ID)
			err := c.call()
			sentWantRefused(t, c.name, err, c.status, c.msg)
			if c.field != "" {
				sentWantField(t, c.name, err, c.field, c.msg)
			}
			if after := sentCountsOf(t, env, c.id); after != before {
				t.Errorf("counts after the refusal = %+v, want %+v", after, before)
			}
			if after := sentRow(t, env, po.ID); after != row {
				t.Errorf("po's row after the refusal = %+v, want %+v", after, row)
			}
		})
	}

	t.Run("rights", func(t *testing.T) {
		before, row := sentCountsOf(t, env, po.ID), sentRow(t, env, po.ID)
		// Arjun (Purchase User, not the owner) and Suresh (Stores) read the PO
		// but are neither its owner nor hold approve.
		for _, p := range []auth.Principal{env.Arjun, env.Suresh} {
			sentWantDenied(t, env, "MarkSent as "+p.Name, svc.MarkSent(ctx, p, po.ID, 1, in(oct31, "email")), p, rec)
			sentWantDenied(t, env, "UnmarkSent as "+p.Name, svc.UnmarkSent(ctx, p, po.ID, 1), p, rec)
			// CD-259: the right comes before the version and the date rules.
			sentWantDenied(t, env, "MarkSent as "+p.Name+" with a stale version", svc.MarkSent(ctx, p, po.ID, 7, in(nov1, "")), p, rec)
			sentWantDenied(t, env, "UnmarkSent as "+p.Name+" with a stale version", svc.UnmarkSent(ctx, p, po.ID, 7), p, rec)
		}
		// A missing PO and a hidden one (Priya, PRJ-METRO only, on a
		// PRJ-TECHPARK PO) are AUTH's 404, before right and version (CD-245).
		sentWantNotFound(t, "MarkSent on a missing PO", svc.MarkSent(ctx, env.Priya, 999_999, 0, in(oct31, "email")))
		sentWantNotFound(t, "UnmarkSent on a missing PO", svc.UnmarkSent(ctx, env.Priya, 999_999, 1))
		sentWantNotFound(t, "MarkSent as Priya on a PRJ-TECHPARK PO", svc.MarkSent(ctx, env.Priya, hidden, 0, in(oct31, "email")))
		sentWantNotFound(t, "MarkSent as Priya on a PRJ-TECHPARK PO, stale", svc.MarkSent(ctx, env.Priya, hidden, 4, in(nov1, "")))
		sentWantNotFound(t, "UnmarkSent as Priya on a PRJ-TECHPARK PO", svc.UnmarkSent(ctx, env.Priya, hidden, 1))
		if after := sentCountsOf(t, env, po.ID); after != before {
			t.Errorf("counts after the refused rights = %+v, want %+v", after, before)
		}
		if after := sentRow(t, env, po.ID); after != row {
			t.Errorf("po's row after the refused rights = %+v, want %+v", after, row)
		}
		if n := countRows(t, env.DB, sentTable, "po_id = ?", hidden); n != 0 {
			t.Errorf("rows on the hidden PO = %d, want 0", n)
		}
	})

	t.Run("SendState", func(t *testing.T) {
		// 404 and 403 are the returned error.
		_, err := svc.SendState(ctx, env.Priya, 999_999)
		sentWantNotFound(t, "SendState on a missing PO", err)
		_, err = svc.SendState(ctx, env.Priya, hidden)
		sentWantNotFound(t, "SendState as Priya on a PRJ-TECHPARK PO", err)
		for _, p := range []auth.Principal{env.Arjun, env.Suresh} {
			_, err = svc.SendState(ctx, p, po.ID)
			sentWantDenied(t, env, "SendState as "+p.Name, err, p, rec)
		}

		// The sent PO: its summary, record and version, no refusal.
		st, err := svc.SendState(ctx, env.Ravi, po.ID)
		if err != nil {
			t.Fatalf("SendState on the sent PO: %v", err)
		}
		if st.Display != display || st.Version != 1 || st.Refusal != nil {
			t.Errorf("SendState on the sent PO = %+v; want Display %q, version 1, no Refusal", st, display)
		}
		if st.Sent == nil || st.Sent.SentOn != oct30 || st.Sent.SentVia != "email" || st.Sent.SentBy != env.Priya.ID || st.Sent.Version != 1 {
			t.Errorf("SendState.Sent = %+v, want 30-10-2026 by email, by Priya, version 1", st.Sent)
		}

		// Refusal holds only the not-approved refusal: the same error the POST
		// returns.
		st, err = svc.SendState(ctx, env.Priya, draft.ID)
		if err != nil {
			t.Fatalf("SendState on a draft: %v", err)
		}
		if st.Version != 0 || st.Sent != nil {
			t.Errorf("SendState on a draft = %+v, want version 0 and no record", st)
		}
		sentWantRefused(t, "SendState.Refusal on a draft", st.Refusal, http.StatusUnprocessableEntity, msgSentNotApproved)

		// An approved PO with no row: version 0, no record, no refusal.
		fresh := purchasetest.NewOrder(t, env)
		st, err = svc.SendState(ctx, env.Admin, fresh.ID)
		if err != nil || st.Version != 0 || st.Sent != nil || st.Refusal != nil || st.Display != naming.Label(fresh.Number, fresh.ID) {
			t.Errorf("SendState on an unsent approved PO = %+v, %v; want its display, version 0, no record, no Refusal", st, err)
		}
	})

	// Concurrency (CD-94; lessons §11: as non-admins, since Can returns early
	// for an Administrator): two MarkSent calls with posted version 0 on a PO
	// with no row race; one inserts at version 1 and the other is DOC's stale
	// 409, never a second row, an overwrite or a raw constraint error.
	t.Run("concurrent version 0", func(t *testing.T) {
		env.SetNow(t, time.Date(2026, time.October, 24, 5, 0, 0, 0, time.UTC))
		po := purchasetest.NewOrder(t, env)
		display := naming.Label(po.Number, po.ID)
		users := []auth.Principal{env.Priya, env.Ravi}
		vias := []string{"email", "courier"}
		errs := make([]error, len(users))
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range users {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs[i] = env.Service.MarkSent(ctx, users[i], po.ID, 0, purchase.SentInput{SentOn: oct24, SentVia: vias[i]})
			}()
		}
		close(start)
		wg.Wait()

		won := -1
		for i, err := range errs {
			if err == nil {
				if won >= 0 {
					t.Fatalf("both MarkSent calls with version 0 succeeded, want one 409")
				}
				won = i
			}
		}
		if won < 0 {
			t.Fatalf("both MarkSent calls failed: %v; want one to insert", errs)
		}
		sentWantRefused(t, "the losing MarkSent", errs[1-won], http.StatusConflict, sentStale(display))
		if n := countRows(t, env.DB, sentTable, "po_id = ?", po.ID); n != 1 {
			t.Errorf("rows = %d, want 1", n)
		}
		if r := sentRow(t, env, po.ID); r.version != 1 || r.via != vias[won] || r.by != int64(users[won].ID) {
			t.Errorf("row = %+v, want the winner's %s at version 1", r, vias[won])
		}
		var marks int
		for _, e := range sentAudits(t, env, po.ID) {
			if e.Action == auditMarkedSent {
				marks++
			}
		}
		if marks != 1 {
			t.Errorf("Marked as sent audit entries = %d, want 1", marks)
		}
	})
}
