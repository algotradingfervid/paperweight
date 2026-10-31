package goodsreceipt_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/types"
)

// g7IST0001 is 18:31 UTC on 23 October: 00:01 IST on 24-10-2026, just after
// IST midnight, so "today" is 24-10-2026 only when read in IST (lessons:
// date tests include a clock just after IST midnight).
var g7IST0001 = time.Date(2026, 10, 23, 18, 31, 0, 0, time.UTC)

// g7Oct is a date in October 2026.
func g7Oct(day int) types.Date { return types.Date{Year: 2026, Month: time.October, Day: day} }

// g7WantView asserts the identifying fields of a bill view.
func g7WantView(t *testing.T, what string, got goodsreceipt.BillView, version int, no string, date types.Date, amount *types.Money, note string) {
	t.Helper()
	if got.Version != version || got.InvoiceNo != no || got.InvoiceDate != date || got.Note != note {
		t.Errorf("%s: view v%d %q %s note %q, want v%d %q %s note %q",
			what, got.Version, got.InvoiceNo, got.InvoiceDate.ISO(), got.Note, version, no, date.ISO(), note)
	}
	switch {
	case amount == nil && got.InvoiceAmount.Valid:
		t.Errorf("%s: invoice amount %d, want none", what, got.InvoiceAmount.V)
	case amount != nil && (!got.InvoiceAmount.Valid || got.InvoiceAmount.V != *amount):
		t.Errorf("%s: invoice amount %v, want %d", what, got.InvoiceAmount, *amount)
	}
}

// g7Money is a pointer to m, for g7WantView.
func g7Money(m types.Money) *types.Money { return &m }

// GRN-025 (GRN spec §3.3; CD-234, CD-279 (4), CD-94; plan G7 Do 2, 3, 5):
// bill details are refused on a draft and a pending GRN with the state text
// (422) and nothing written; on the approved GRN (800 kg of line 1 at
// ₹62.5000 = ₹50,000.00 accepted) each field rule refuses with its exact
// text, at the field, and writes nothing; a valid save is stored at version
// 1 with the information line, and each later save bumps the version and
// audits only the columns it changed, by the saver. A 40-character number
// (one of them a two-byte rune) saves; 41 is refused with DOC's length text.
// Today saves and tomorrow is refused, on a clock just after IST midnight.
// A total of 0 and a blank total save. After cancel the bill is still read
// and a save is refused with the state text (spec: "live on after cancel,
// read-only").
func TestGRNBill_StateRules(t *testing.T) {
	env := grntest.New(t)
	env.SetNow(t, g7IST0001)
	po := g2RefPO(t, env)
	approved := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "800"), doctype.StateApproved)
	draft := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "100"), doctype.StateDraft)
	g3GRNRule(t, env) // from ₹10,000.00: 200 kg (₹12,500.00) waits for the Administrator
	pending := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "200"), doctype.StatePending)
	valid := g7In(g7Invoice, "24-10-2026", "61,714.00")

	for _, c := range []struct {
		what  string
		id    int64
		state doctype.State
	}{
		{"draft", draft.ID, doctype.StateDraft},
		{"pending", pending.ID, doctype.StatePending},
	} {
		g5WantState(t, c.what, env, c.id, c.state)
		before := g7Facts(t, env)
		_, err := g7Save(t, env, env.Suresh, c.id, 0, valid)
		g7WantStatus(t, "save on the "+c.what+" GRN", err, http.StatusUnprocessableEntity, g7MsgNotApproved)
		g7WantUnchanged(t, "the refused save on the "+c.what+" GRN", env, before)
	}

	// The state check comes before the field rules: an empty form on the
	// draft gets the state text, not field errors.
	_, err := g7Save(t, env, env.Suresh, draft.ID, 0, g7In("", "", ""))
	g7WantStatus(t, "empty form on the draft GRN", err, http.StatusUnprocessableEntity, g7MsgNotApproved)

	_, badDate := types.ParseDate("31-02-2026")
	_, badAmount := types.ParseMoney("12.345")
	long := strings.Repeat("A", 41)
	for _, c := range []struct {
		what string
		in   goodsreceipt.BillInput
		want []doctype.FieldError
	}{
		{"blank invoice no.", g7In("", "24-10-2026", ""), []doctype.FieldError{{Field: "invoice_no", Message: g7MsgNoRequired}}},
		{"41-character invoice no.", g7In(long, "24-10-2026", ""), []doctype.FieldError{{Field: "invoice_no", Message: g7MsgNoMaxLen}}},
		{"blank invoice date", g7In(g7Invoice, "", ""), []doctype.FieldError{{Field: "invoice_date", Message: g7MsgDateRequired}}},
		{"invoice date tomorrow (IST)", g7In(g7Invoice, "25-10-2026", ""), []doctype.FieldError{{Field: "invoice_date", Message: g7MsgDateFuture}}},
		{"unreadable invoice date", g7In(g7Invoice, "31-02-2026", ""), []doctype.FieldError{{Field: "invoice_date", Message: badDate.Error()}}},
		{"negative invoice total", g7In(g7Invoice, "24-10-2026", "-0.01"), []doctype.FieldError{{Field: "invoice_amount", Message: g7MsgNegative}}},
		{"unreadable invoice total", g7In(g7Invoice, "24-10-2026", "12.345"), []doctype.FieldError{{Field: "invoice_amount", Message: badAmount.Error()}}},
		{"every field wrong", g7In("", "", "-5"), []doctype.FieldError{
			{Field: "invoice_no", Message: g7MsgNoRequired},
			{Field: "invoice_date", Message: g7MsgDateRequired},
			{Field: "invoice_amount", Message: g7MsgNegative},
		}},
	} {
		before := g7Facts(t, env)
		_, err := g7Save(t, env, env.Suresh, approved.ID, 0, c.in)
		g2WantInvalid(t, c.what, err, c.want...)
		g7WantUnchanged(t, "the refused "+c.what, env, before)
	}

	// Insert: version 1, every column audited, the information line.
	w := g7MustSave(t, "first save", env, env.Suresh, approved.ID, 0, valid)
	g7WantWarnings(t, "first save", w, g7Accepted("50,000.00"))
	v := g7Bill(t, env, approved.ID)
	g7WantView(t, "after the first save", v, 1, g7Invoice, g7Oct(24), g7Money(61_714_00), "")
	if v.UpdatedBy != env.Suresh.ID || !v.UpdatedAt.Equal(g7IST0001) {
		t.Errorf("after the first save: updated by %d at %s, want %d at %s", v.UpdatedBy, v.UpdatedAt, env.Suresh.ID, g7IST0001)
	}
	entries := g7BillEntries(t, env, approved.ID)
	if len(entries) != 1 {
		t.Fatalf("after the first save: %d bill audit entries on the GRN, want 1", len(entries))
	}
	g7WantEntry(t, "first save", entries[0], env.Suresh.ID, g7BillFields...)
	if c := g7Change(t, entries[0], "invoice_no"); c.After != g7Invoice {
		t.Errorf("first save: invoice_no change After = %q, want %q", c.After, g7Invoice)
	}

	// Updates: each bumps the version and audits only what changed.
	forty := strings.Repeat("A", 39) + "é" // 40 runes, 41 bytes
	steps := []struct {
		what   string
		in     goodsreceipt.BillInput
		no     string
		amount *types.Money
		field  string
	}{
		{"total changed", g7In(g7Invoice, "24-10-2026", "61,800.00"), g7Invoice, g7Money(61_800_00), "invoice_amount"},
		{"40-character invoice no.", g7In(forty, "24-10-2026", "61,800.00"), forty, g7Money(61_800_00), "invoice_no"},
		{"total 0", g7In(forty, "24-10-2026", "0"), forty, g7Money(0), "invoice_amount"},
		{"total blank", g7In(forty, "24-10-2026", ""), forty, nil, "invoice_amount"},
	}
	for i, s := range steps {
		w := g7MustSave(t, s.what, env, env.Suresh, approved.ID, i+1, s.in)
		g7WantWarnings(t, s.what, w, g7Accepted("50,000.00"))
		g7WantView(t, s.what, g7Bill(t, env, approved.ID), i+2, s.no, g7Oct(24), s.amount, "")
		entries := g7BillEntries(t, env, approved.ID)
		if len(entries) != i+2 {
			t.Fatalf("%s: %d bill audit entries, want %d", s.what, len(entries), i+2)
		}
		g7WantEntry(t, s.what, entries[i+1], env.Suresh.ID, s.field)
	}
	if c := g7Change(t, g7BillEntries(t, env, approved.ID)[2], "invoice_no"); c.Before != g7Invoice || c.After != forty {
		t.Errorf("invoice no. change: %q → %q, want %q → %q", c.Before, c.After, g7Invoice, forty)
	}

	// After cancel: still read, refused with the state text.
	env.Cancel(t, env.Ravi, approved.ID, g4CancelReason)
	g7WantView(t, "after cancel", g7Bill(t, env, approved.ID), 5, forty, g7Oct(24), nil, "")
	before := g7Facts(t, env)
	_, err = g7Save(t, env, env.Suresh, approved.ID, 5, valid)
	g7WantStatus(t, "save on the cancelled GRN", err, http.StatusUnprocessableEntity, g7MsgNotApproved)
	g7WantUnchanged(t, "the refused save on the cancelled GRN", env, before)
}

// GRN-026 (GRN spec §3.3; CD-94, CD-245, CD-259; plan G7 Do 2's order:
// load, write right, version, state, fields): a stale version is DOC's 409
// for Bill details (posted 0 over a row, a number past the row, and a
// number with no row); Priya (Purchase User: read, no write) gets AUTH's 403
// even with a stale version or an unreadable date, and Suresh's stale post
// with an unreadable date is still the 409, because values are parsed after
// the checks. A user holding only read and write saves. With the PO closed,
// Suresh saves at the right version. A GRN Suresh can't see (TECH-SITE) and
// a missing id are AUTH's 404, before the version. On a cancelled GRN a
// stale post is the 409 and a current one the state text. Nothing is
// written by any refusal.
func TestGRNBill_VersionAndRights(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	grn := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "800"), doctype.StateApproved)
	noRow := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "100"), doctype.StateApproved)
	tech := g6TechPO(t, env)
	hidden := env.GRN(t, env.Admin, tech.ID, g4Line1(tech, "10"), doctype.StateApproved)
	valid := g7In(g7Invoice, "24-10-2026", "61,714.00")
	badDate := g7In(g7Invoice, "31-02-2026", "")
	g7MustSave(t, "first save", env, env.Suresh, grn.ID, 0, valid)

	for _, c := range []struct {
		what    string
		id      int64
		version int
		in      goodsreceipt.BillInput
	}{
		{"posted 0 over a row at version 1", grn.ID, 0, valid},
		{"posted 2 over a row at version 1", grn.ID, 2, valid},
		{"posted 1 with no row", noRow.ID, 1, valid},
		{"stale with an unreadable date", grn.ID, 0, badDate},
	} {
		before := g7Facts(t, env)
		_, err := g7Save(t, env, env.Suresh, c.id, c.version, c.in)
		g7WantStatus(t, c.what, err, http.StatusConflict, g7MsgStale)
		g7WantUnchanged(t, "the refused "+c.what, env, before)
	}

	for _, c := range []struct {
		what    string
		version int
		in      goodsreceipt.BillInput
	}{
		{"Priya at the right version", 1, valid},
		{"Priya at a stale version", 0, valid},
		{"Priya with an unreadable date", 1, badDate},
	} {
		before := g7Facts(t, env)
		_, err := g7Save(t, env, env.Priya, grn.ID, c.version, c.in)
		g7WantDenied(t, c.what, err)
		g7WantUnchanged(t, "the refused "+c.what, env, before)
	}

	clerk := env.BillClerk(t)
	g7MustSave(t, "a user holding only read and write", env, clerk, grn.ID, 1, g7In(g7Invoice, "24-10-2026", "61,800.00"))
	if v := g7Bill(t, env, grn.ID); v.Version != 2 || v.UpdatedBy != clerk.ID {
		t.Errorf("after the clerk's save: v%d by %d, want v2 by %d", v.Version, v.UpdatedBy, clerk.ID)
	}

	env.ClosePO(t, env.Priya, po.ID, g4CloseReason)
	g7MustSave(t, "PO closed", env, env.Suresh, grn.ID, 2, valid)
	if v := g7Bill(t, env, grn.ID); v.Version != 3 {
		t.Errorf("after the save on a closed PO: v%d, want v3", v.Version)
	}

	for _, c := range []struct {
		what    string
		id      int64
		version int
	}{
		{"hidden GRN", hidden.ID, 0},
		{"hidden GRN at a stale version", hidden.ID, 7},
		{"missing GRN", 999_999, 0},
	} {
		before := g7Facts(t, env)
		_, err := g7Save(t, env, env.Suresh, c.id, c.version, valid)
		g7WantNotFound(t, c.what, err)
		g7WantUnchanged(t, "the refused "+c.what, env, before)
	}

	env.Cancel(t, env.Ravi, grn.ID, g4CancelReason)
	before := g7Facts(t, env)
	_, err := g7Save(t, env, env.Suresh, grn.ID, 0, valid)
	g7WantStatus(t, "cancelled GRN, stale version", err, http.StatusConflict, g7MsgStale)
	_, err = g7Save(t, env, env.Suresh, grn.ID, 3, valid)
	g7WantStatus(t, "cancelled GRN, current version", err, http.StatusUnprocessableEntity, g7MsgNotApproved)
	g7WantUnchanged(t, "the refused saves on the cancelled GRN", env, before)
}
