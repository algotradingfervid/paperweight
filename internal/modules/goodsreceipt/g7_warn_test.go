package goodsreceipt_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
)

// GRN-027 (GRN spec §3.3; CD-240, CD-248, CD-279 (3); plan G7 Do 2's
// warnings, read in the same transaction): the same vendor and invoice no.
// on another GRN that isn't cancelled saves with a warning naming it when
// the saver can read it, and "another receipt" when they can't (Suresh,
// restricted to PUNE-WH and METRO-SITE on the real auth.Service, against a
// TECH-SITE GRN the Administrator made; the Administrator saving the same
// number is told the METRO-SITE GRN's number). A GRN never warns about its
// own row, a cancelled GRN's row doesn't warn, and the same number from
// another vendor doesn't warn. Every save carries the information line
// (each GRN accepts 100 kg at ₹62.50 = ₹6,250.00).
func TestGRNBill_DuplicateWarns(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	first := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "100"), doctype.StateApproved)  // GR/26-27/00001
	second := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "100"), doctype.StateApproved) // GR/26-27/00002
	third := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "100"), doctype.StateApproved)  // GR/26-27/00003
	if first.Number != g4FirstGRN || second.Number != "GR/26-27/00002" || third.Number != "GR/26-27/00003" {
		t.Fatalf("GRN numbers %q %q %q, want GR/26-27/00001 to 00003", first.Number, second.Number, third.Number)
	}
	accepted := g7Accepted("6,250.00")
	in := g7In(g7Invoice, "24-10-2026", "")

	w := g7MustSave(t, "the first GRN, alone", env, env.Suresh, first.ID, 0, in)
	g7WantWarnings(t, "the first GRN, alone", w, accepted)
	w = g7MustSave(t, "the first GRN saved again", env, env.Suresh, first.ID, 1, in)
	g7WantWarnings(t, "the first GRN saved again (its own row)", w, accepted)

	env.Cancel(t, env.Ravi, first.ID, g4CancelReason)
	w = g7MustSave(t, "the second GRN, the first cancelled", env, env.Suresh, second.ID, 0, in)
	g7WantWarnings(t, "the second GRN (the first, cancelled, holds the number)", w, accepted)

	w = g7MustSave(t, "the third GRN", env, env.Suresh, third.ID, 0, in)
	g7WantWarnings(t, "the third GRN (the second, readable, holds the number)", w, g7Dup(g7Invoice, second.Number), accepted)

	bharat := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved(),
		purchasetest.OrderHeader(map[string]string{"vendor_id": g1MasterID(t, env.DB, "party", "BHARAT")}),
		purchasetest.OrderLines(map[string]string{"qty": "100", "price": "62.5"}))
	other := env.GRN(t, env.Suresh, bharat.ID, g4Line1(bharat, "100"), doctype.StateApproved)
	w = g7MustSave(t, "another vendor's GRN", env, env.Suresh, other.ID, 0, in)
	g7WantWarnings(t, "another vendor's GRN with the same number", w, accepted)

	// Visibility: the TECH-SITE GRN is the Administrator's; Suresh can't
	// read it.
	tech := g6TechPO(t, env)
	hidden := env.GRN(t, env.Admin, tech.ID, g4Line1(tech, "100"), doctype.StateApproved)
	const techInvoice = "TT/0091"
	w = g7MustSave(t, "the TECH-SITE GRN", env, env.Admin, hidden.ID, 0, g7In(techInvoice, "24-10-2026", ""))
	g7WantWarnings(t, "the TECH-SITE GRN, alone with its number", w, accepted)
	w = g7MustSave(t, "Suresh, the TECH-SITE GRN holding the number", env, env.Suresh, second.ID, 1, g7In(techInvoice, "24-10-2026", ""))
	g7WantWarnings(t, "Suresh can't read the other GRN", w, g7DupHidden(techInvoice), accepted)
	w = g7MustSave(t, "the Administrator again", env, env.Admin, hidden.ID, 1, g7In(techInvoice, "24-10-2026", ""))
	g7WantWarnings(t, "the Administrator reads the other GRN", w, g7Dup(techInvoice, second.Number), accepted)
}

// GRN-046 (GRN spec §3.3; CD-272 Gap 5, CD-279 (11); plan G7 Do 1, 6):
// Bill on an approved GRN with no row is a zero view at version 0, and with
// challan_kind Tax invoice it offers the challan no. and date with the note,
// still at version 0, and writes nothing. Saving it stores the row, and from
// then on Bill returns the row with no note, even after the number changes.
// A delivery challan and a draft with a tax invoice offer nothing.
func TestGRNBill_CopyFromInvoice(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	h := g2Header(t, env, po)
	h["challan_kind"], h["challan_no"], h["challan_date"] = "Tax invoice", g7Invoice, "2026-10-23"
	taxID := g2MustCreate(t, env, env.Suresh, h, g2Line(po.Lines[0], "800", "", "")).ID
	if _, err := g2Submit(t, env, env.Suresh, taxID); err != nil {
		t.Fatalf("submit the tax-invoice GRN: %v %+v", err, g1FieldErrors(err))
	}
	g5WantState(t, "the tax-invoice GRN", env, taxID, doctype.StateApproved)
	draftTax := g2MustCreate(t, env, env.Suresh, h, g2Line(po.Lines[1], "10", "", "")).ID
	challan := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "100"), doctype.StateApproved) // Delivery challan DC-1042

	before := g7Facts(t, env)
	g7WantView(t, "approved, tax invoice, no row", g7Bill(t, env, taxID), 0, g7Invoice, g7Oct(23), nil, g7MsgCopiedNote)
	g7WantView(t, "approved, delivery challan, no row", g7Bill(t, env, challan.ID), 0, "", g7NoDate, nil, "")
	g7WantView(t, "draft, tax invoice", g7Bill(t, env, draftTax), 0, "", g7NoDate, nil, "")
	g7WantUnchanged(t, "reading the bill views", env, before)
	if n := env.RowCounts(t).Bills; n != 0 {
		t.Errorf("grn_bill rows after the reads = %d, want 0", n)
	}

	g7MustSave(t, "save the offered values", env, env.Suresh, taxID, 0, g7In(g7Invoice, "23-10-2026", ""))
	g7WantView(t, "after the save", g7Bill(t, env, taxID), 1, g7Invoice, g7Oct(23), nil, "")
	const changed = "SST/2627/0458"
	g7MustSave(t, "change the number", env, env.Suresh, taxID, 1, g7In(changed, "23-10-2026", ""))
	g7WantView(t, "the row, not the challan", g7Bill(t, env, taxID), 2, changed, g7Oct(23), nil, "")
}

// g7NoDate is the zero types.Date a view without a date holds.
var g7NoDate types.Date
