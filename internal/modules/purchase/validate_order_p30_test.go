package purchase_test

import (
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// MOD-PO P30 (CD-337 (2); PO spec:94): each PO line's hsn_sac is checked with
// the item master's rule for the line item's type (gstin.ValidateGoodsHSN or
// gstin.ValidateSAC), after a blank code takes the item's. The texts are
// gstin's, behind the line prefix.
const (
	p30GoodsLen = "HSN code must be 4, 6 or 8 digits."
	p30Goods99  = "Codes starting 99 are service (SAC) codes. Set the item type to Service or use a goods HSN."
	p30SAC      = "SAC code must be 6 digits starting with 99."
)

// p30HSNError is the field error for line n's hsn_sac with gstin's text msg.
func p30HSNError(n int, msg string) doctype.FieldError {
	return doctype.FieldError{Field: "hsn_sac", Line: n, Message: fmt.Sprintf("Line %d: %s", n, msg)}
}

// p30Legacy sets the stored hsn_sac of order id's lines to code, standing in
// for a PO approved before P30, when no check refused it: no engine path can
// store such a line now. Like p2SetCopyNotes it changes one column of
// existing rows and inserts nothing.
func p30Legacy(t *testing.T, env *purchasetest.Env, id int64, code string) {
	t.Helper()
	if _, err := env.DB.SQL().ExecContext(t.Context(), `UPDATE purchase_order_line SET hsn_sac = ? WHERE parent_id = ?`, code, id); err != nil {
		t.Fatalf("set hsn_sac of order %d's lines: %v", id, err)
	}
}

// Save (create a draft): line 2's code against its item's type; line 1 is a
// valid MS-ANGLE-50 line, so the prefix names the right line.
func TestPO_LineHSNSAC_Save(t *testing.T) {
	env := purchasetest.New(t)
	for _, c := range []struct {
		name, item, unit, hsn, want, stored string // want "" accepts; stored is the line's code after an accepted save
	}{
		{"goods 4 digits", "MS-ANGLE-50", "KGS", "7216", "", "7216"},
		{"goods 6 digits", "MS-ANGLE-50", "KGS", "721610", "", "721610"},
		{"goods 8 digits", "MS-ANGLE-50", "KGS", "72161000", "", "72161000"},
		{"goods 3 digits", "MS-ANGLE-50", "KGS", "721", p30GoodsLen, ""},
		{"goods 5 digits", "MS-ANGLE-50", "KGS", "72161", p30GoodsLen, ""},
		{"goods 7 digits", "MS-ANGLE-50", "KGS", "1234567", p30GoodsLen, ""},
		{"goods 9 digits", "MS-ANGLE-50", "KGS", "721610001", p30GoodsLen, ""},
		{"goods letters", "MS-ANGLE-50", "KGS", "72AB", p30GoodsLen, ""},
		{"goods 9954", "MS-ANGLE-50", "KGS", "9954", p30Goods99, ""},
		{"goods 995411", "MS-ANGLE-50", "KGS", "995411", p30Goods99, ""},
		{"goods blank takes the item's", "MS-ANGLE-50", "KGS", "", "", "7216"},
		{"service 995411", "SVC-CRANE", "HRS", "995411", "", "995411"},
		{"service 9954", "SVC-CRANE", "HRS", "9954", p30SAC, ""},
		{"service 99541", "SVC-CRANE", "HRS", "99541", p30SAC, ""},
		{"service 9954111", "SVC-CRANE", "HRS", "9954111", p30SAC, ""},
		{"service goods HSN", "SVC-CRANE", "HRS", "721610", p30SAC, ""},
		{"service blank takes the item's", "SVC-CRANE", "HRS", "", "", "997313"},
	} {
		t.Run(c.name, func(t *testing.T) {
			line := p2Line(t, env, c.item, c.unit, "8", "100")
			line["hsn_sac"] = c.hsn
			res, err := p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"),
				[]map[string]string{p2Line(t, env, "MS-ANGLE-50", "KGS", "10", "62.5"), line})
			if c.want != "" {
				p2WantInvalid(t, "save with "+c.hsn, err, p30HSNError(2, c.want))
				return
			}
			if err != nil {
				t.Fatalf("save with %q: %v", c.hsn, err)
			}
			if got := p2LineSnap(t, env, res.ID, "hsn_sac")[1]["hsn_sac"]; got != c.stored {
				t.Errorf("line 2 hsn_sac = %q, want %q", got, c.stored)
			}
		})
	}
}

// Step save: a lines change scoped to another step commits with the line's
// code error as a problem (CD-205).
func TestPO_LineHSNSAC_StepSave(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	lines := p2KeepLines(t, env, po.ID)
	lines[0]["hsn_sac"] = "1234567"
	res, err := p2Step(t, env, env.Priya, po.ID, []string{"remarks"}, map[string]string{"remarks": "Check"}, lines)
	p2WantProblems(t, "step save with 1234567", res, err, p30HSNError(1, p30GoodsLen))
}

// Amend: an approved PO from before P30 holds MS-ANGLE-50 at 9954 and is
// cancelled. Amend copies the line's 9954 (amend keeps Validate's writes and
// ignores its errors), and the amendment's save is refused at line 1.
func TestPO_LineHSNSAC_Amend(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env)
	p30Legacy(t, env, po.ID, "9954")
	p2Must(t, "Ravi cancels")(p2Do(t, env, env.Ravi, po.ID, doc.ActionCancel, "Wrong angle"))

	res := p2Must(t, "Priya amends")(p2Do(t, env, env.Priya, po.ID, doc.ActionAmend, ""))
	if res.ID == po.ID || res.State != doctype.StateDraft {
		t.Fatalf("amend = %+v, want a new draft", res)
	}
	if got := p2LineSnap(t, env, res.ID, "hsn_sac")[0]["hsn_sac"]; got != "9954" {
		t.Errorf("amended line hsn_sac = %q, want 9954 copied", got)
	}
	_, err := p2Save(t, env, env.Priya, res.ID, map[string]string{"remarks": "Check"}, nil)
	p2WantInvalid(t, "save the amendment", err, p30HSNError(1, p30Goods99))
}

// Copy (a module route through Validate): the copy of an approved PO from
// before P30 holding MS-ANGLE-50 at 9954 is refused at line 1 and creates
// nothing.
func TestPO_LineHSNSAC_Copy(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env)
	p30Legacy(t, env, po.ID, "9954")
	before := p2CountAll(t, env)

	newID, err := env.Service.Copy(t.Context(), env.Priya, po.ID)
	if newID != 0 {
		t.Errorf("Copy = new id %d, want refused", newID)
	}
	p2WantInvalid(t, "copy of the 9954 PO", err, p30HSNError(1, p30Goods99))
	p2SameCounts(t, "refused copy", before, env)
}

// Create from request: request rows carry no code, so each PO line takes its
// item's, which passes (SVC-CRANE's SAC under the service rule).
func TestPO_LineHSNSAC_FromRequest(t *testing.T) {
	env := purchasetest.New(t)
	req := p13Request(t, env, p13Row(t, env, "SVC-CRANE", "HRS", "8", "Crane for the slab"))
	id, err := p13Create(t, env, env.Priya, req.ID, p5Version(t, env, req.ID), "SHREE")
	if err != nil {
		t.Fatalf("CreateFromRequest: %v %+v", err, p5Fields(err))
	}
	if got := p2LineSnap(t, env, id, "hsn_sac")[0]["hsn_sac"]; got != "997313" {
		t.Errorf("line 1 hsn_sac = %q, want SVC-CRANE's 997313", got)
	}
}
