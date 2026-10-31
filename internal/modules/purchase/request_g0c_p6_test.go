package purchase_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// PO-069, amendment part (PO spec §20.3, §20.5; CD-279 (10) G0c; AD-13
// ruling: the kernel's exemption wins over §20.5's older wording). Priya's
// approved PO for 10 Bag of a 40 Bag row is cancelled; her amendment is
// submitted; Suresh closes the request; Ravi approves the amendment, which
// goes through because it keeps the row its amended-from PO held. The row
// then reads 10 part_ordered (lines already ordered stay ordered) with 0
// pending. A new PO linking the closed request's row is still refused with
// DOC's closed text, and so is an amendment that adds a row its amended-from
// PO didn't hold.
func TestRequest_AmendKeepsClosedRow(t *testing.T) {
	t.Run("amendment approved after the close", func(t *testing.T) {
		t.Parallel()
		env := purchasetest.New(t, purchasetest.WithNoRules())
		p6RuleFromZero(t, env)
		req := p6CementRequest(t, env, "40")
		row := req.Lines[0].ID
		po := p6Order(t, env, env.Priya, nil, []map[string]string{p6Bag(t, env, "10", row)}, doctype.StateApproved)
		p2Must(t, "Ravi cancels")(p2Do(t, env, env.Ravi, po.ID, doc.ActionCancel, p6CancelReason))
		am := p2Must(t, "Priya amends")(p2Do(t, env, env.Priya, po.ID, doc.ActionAmend, ""))
		p2Must(t, "Priya submits the amendment")(p2Do(t, env, env.Priya, am.ID, doc.ActionSubmit, ""))
		p5Must(t, "Suresh closes the request")(p5Do(t, env, env.Suresh, req.ID, doctype.ActionClose, p5CloseReason))

		if _, err := p2Do(t, env, env.Ravi, am.ID, doc.ActionApprove, ""); err != nil {
			t.Fatalf("Ravi approves the amendment of a PO that held the closed request's row: %v %+v, want approved (G0c)", err, p5Fields(err))
		}
		if d := p2Load(t, env, am.ID); d.State != doctype.StateApproved || d.Number == "" {
			t.Errorf("amendment is %s %q, want approved with a number", d.State, d.Number)
		}
		if got := p6ReadRow(t, env, row); got.ordered != p6Q(t, "10") || got.status != "part_ordered" {
			t.Errorf("closed request row = %s %q, want 10 part_ordered (the approved amendment orders 10 of 40)", got.ordered.Plain(), got.status)
		}
		if got, ok := p6Pending(t, env, row)[row]; !ok || got != 0 {
			t.Errorf("pending to order of the closed request's row = %s (present %v), want 0", got.Plain(), ok)
		}

		// Contrast: a new PO linking the same closed row is refused.
		before := p2CountAll(t, env)
		_, err := p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"),
			[]map[string]string{p6Bag(t, env, "10", row)})
		p2WantInvalid(t, "save a new PO on the closed row", err,
			doctype.FieldError{Field: "request_line_id", Line: 1, Message: p6Fmt(p6DocClosed, 1, req.Number)})
		p2SameCounts(t, "refused save", before, env)
	})

	t.Run("amendment adding a row its amended-from PO didn't hold", func(t *testing.T) {
		t.Parallel()
		env := purchasetest.New(t, purchasetest.WithNoRules())
		p6RuleFromZero(t, env)
		req := p6Request(t, env, env.Suresh, p5Header(t, env, "METRO-SITE", "PRJ-METRO"), []map[string]string{
			p5Line(t, env, "CEMENT-OPC", "BAG", "40"), p5Line(t, env, "MS-ANGLE-50", "KGS", "100")})
		bags, kg := req.Lines[0].ID, req.Lines[1].ID
		po := p6Order(t, env, env.Priya, nil, []map[string]string{p6Bag(t, env, "10", bags)}, doctype.StateApproved)
		p2Must(t, "Ravi cancels")(p2Do(t, env, env.Ravi, po.ID, doc.ActionCancel, p6CancelReason))
		am := p2Must(t, "Priya amends")(p2Do(t, env, env.Priya, po.ID, doc.ActionAmend, ""))
		p5Must(t, "Suresh closes the request")(p5Do(t, env, env.Suresh, req.ID, doctype.ActionClose, p5CloseReason))

		_, err := p2Save(t, env, env.Priya, am.ID, nil, []map[string]string{
			p6Bag(t, env, "10", bags), p6Link(p2Line(t, env, "MS-ANGLE-50", "KGS", "50", "62.5"), kg)})
		p2WantInvalid(t, "save the amendment with a new row", err,
			doctype.FieldError{Field: "request_line_id", Line: 2, Message: p6Fmt(p6DocClosed, 2, req.Number)})
		if got := p2LineSnap(t, env, am.ID, "request_line_id"); len(got) != 1 || got[0]["request_line_id"] != p5Plain(bags) {
			t.Errorf("amendment lines after the refused save %v, want only the held row %d", got, bags)
		}
	})
}
