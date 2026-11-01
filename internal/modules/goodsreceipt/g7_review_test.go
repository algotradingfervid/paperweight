package goodsreceipt_test

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
)

// MOD-GRN G7 review rows (AD rulings in the G7 fix loop).

// G7 review 1 (AD ruling: UX §5.7's list; CD-240, CD-248, CD-279 (3)):
// several readable duplicates are named in GRN id order, joined with commas
// and "and"; when any is unreadable, only the "another receipt" sentence is
// given. Bills are saved out of id order, so an order by bill row, a
// reversed order or naming only one GRN fails.
func TestGRNBill_ReviewDuplicateList(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	var g [5]int64
	var num [5]string
	for i := range g {
		d := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "100"), doctype.StateApproved)
		g[i], num[i] = d.ID, d.Number
	}
	in := g7In(g7Invoice, "24-10-2026", "") // no total: no accepted value line (S3b)

	g7MustSave(t, "GRN 3", env, env.Suresh, g[2], 0, in)
	g7MustSave(t, "GRN 1", env, env.Suresh, g[0], 0, in)
	w := g7MustSave(t, "GRN 2", env, env.Suresh, g[1], 0, in)
	g7WantWarnings(t, "two readable duplicates", w, g7Dup(g7Invoice, num[0]+" and "+num[2]))
	w = g7MustSave(t, "GRN 4", env, env.Suresh, g[3], 0, in)
	g7WantWarnings(t, "three readable duplicates", w, g7Dup(g7Invoice, num[0]+", "+num[1]+" and "+num[2]))

	// A fresh number on one readable GRN (GRN 1) and one hidden: the
	// TECH-SITE GRN is the Administrator's, which Suresh can't read.
	const mixed = "MX/0042"
	tech := g6TechPO(t, env)
	hidden := env.GRN(t, env.Admin, tech.ID, g4Line1(tech, "100"), doctype.StateApproved)
	g7MustSave(t, "readable GRN 1 with the mixed number", env, env.Suresh, g[0], 1, g7In(mixed, "24-10-2026", ""))
	g7MustSave(t, "hidden GRN with the mixed number", env, env.Admin, hidden.ID, 0, g7In(mixed, "24-10-2026", ""))
	w = g7MustSave(t, "GRN 5, one readable and one hidden", env, env.Suresh, g[4], 0, g7In(mixed, "24-10-2026", ""))
	g7WantWarnings(t, "one readable and one hidden duplicate", w, g7DupHidden(mixed))
}

// G7 review 2 (AD ruling: trimmed, as DOC's Required and G2): an invoice
// no. of only spaces is required; a padded number is stored trimmed, counts
// trimmed against the 40-character limit, and warns against the same
// number on another GRN.
func TestGRNBill_ReviewTrim(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	first := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "100"), doctype.StateApproved)
	second := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "100"), doctype.StateApproved)
	g7MustSave(t, "the first GRN", env, env.Suresh, first.ID, 0, g7In(g7Invoice, "24-10-2026", ""))

	before := g7Facts(t, env)
	_, err := g7Save(t, env, env.Suresh, second.ID, 0, g7In("   ", "24-10-2026", ""))
	g2WantInvalid(t, "invoice no. of only spaces", err, doctype.FieldError{Field: "invoice_no", Message: g7MsgNoRequired})
	g7WantUnchanged(t, "the refused save of only spaces", env, before)

	w, err := g7Save(t, env, env.Suresh, second.ID, 0, g7In("  "+g7Invoice+" ", "24-10-2026", ""))
	if err != nil {
		t.Fatalf("padded invoice no.: %v %+v, want saved trimmed", err, g1FieldErrors(err))
	}
	g7WantWarnings(t, "padded invoice no.", w, g7Dup(g7Invoice, first.Number)) // no total: no accepted value line (S3b)
	g7WantView(t, "padded invoice no.", g7Bill(t, env, second.ID), 1, g7Invoice, g7Oct(24), nil, "")

	forty := strings.Repeat("B", 40)
	if _, err := g7Save(t, env, env.Suresh, second.ID, 1, g7In(" "+forty+" ", "24-10-2026", "")); err != nil {
		t.Fatalf("padded 40-character invoice no.: %v %+v, want saved trimmed", err, g1FieldErrors(err))
	}
	g7WantView(t, "padded 40-character invoice no.", g7Bill(t, env, second.ID), 2, forty, g7Oct(24), nil, "")
}

// G7 review 4 (CD-245; plan G7 Do 2: CanTx(write) with the GRN's record):
// a user holding read, create, write and submit on the GRN at scope own
// saves bill details on a GRN they created, and gets AUTH's 404 on Suresh's
// (they can't read it), with nothing written. Dropping CreatedBy from the
// record refuses their own GRN.
func TestGRNBill_ReviewOwnScope(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	own := env.OwnReceiver(t)
	mine := env.GRN(t, own, po.ID, g4Line1(po, "100"), doctype.StateApproved)
	theirs := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "100"), doctype.StateApproved)
	in := g7In(g7Invoice, "24-10-2026", "")

	g7MustSave(t, "own-scope user on their own GRN", env, own, mine.ID, 0, in)
	if v := g7Bill(t, env, mine.ID); v.Version != 1 || v.UpdatedBy != own.ID {
		t.Errorf("own GRN's bill: v%d by %d, want v1 by %d", v.Version, v.UpdatedBy, own.ID)
	}
	before := g7Facts(t, env)
	_, err := g7Save(t, env, own, theirs.ID, 0, in)
	g7WantNotFound(t, "own-scope user on Suresh's GRN", err)
	g7WantUnchanged(t, "the refused save on Suresh's GRN", env, before)
}

// G7 review 5 and 6 (AD ruling on action names): the first save's audit
// entry has Before "" for every column, and saves are action "bill_saved";
// the move to an amendment is "bill_copied".
func TestGRNBill_ReviewAudit(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	grn := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "800"), doctype.StateApproved)
	g7MustSave(t, "first save", env, env.Suresh, grn.ID, 0, g7In(g7Invoice, "24-10-2026", "61,714.00"))
	g7MustSave(t, "second save", env, env.Suresh, grn.ID, 1, g7In(g7Invoice, "24-10-2026", "61,800.00"))

	entries := g7BillEntries(t, env, grn.ID)
	if len(entries) != 2 {
		t.Fatalf("%d bill audit entries, want 2", len(entries))
	}
	for _, f := range g7BillFields {
		if c := g7Change(t, entries[0], f); c.Before != "" {
			t.Errorf("first save: %s Before = %q, want \"\" (no row before)", f, c.Before)
		}
	}
	for i, e := range entries {
		if e.Action != "bill_saved" {
			t.Errorf("save %d: audit Action = %q, want %q", i+1, e.Action, "bill_saved")
		}
	}

	env.Cancel(t, env.Ravi, grn.ID, g4CancelReason)
	am := env.Amend(t, env.Suresh, grn.ID)
	g7Approve(t, env, po.ID, am.ID, "")
	copied := g7Copied(t, env, am.ID)
	if len(copied) != 1 || copied[0].Action != "bill_copied" {
		t.Errorf("copy audit on the amendment = %+v, want one entry with Action %q", copied, "bill_copied")
	}
}
