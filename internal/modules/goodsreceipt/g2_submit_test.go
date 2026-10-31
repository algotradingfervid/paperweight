package goodsreceipt_test

import (
	"slices"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// g2WantSubmitRefused submits GRN id as Suresh and asserts the 422 with
// exactly want, all in one doc.Invalid (CD-264 (2)), with the state and
// version left as they were.
func g2WantSubmitRefused(t *testing.T, what string, env *grntest.Env, id int64, want ...doctype.FieldError) {
	t.Helper()
	before := env.Load(t, goodsreceipt.DocType, id)
	_, err := g2Submit(t, env, env.Suresh, id)
	g2WantInvalid(t, what, err, want...)
	if after := env.Load(t, goodsreceipt.DocType, id); after.State != before.State || after.Version != before.Version {
		t.Errorf("%s: after the refusal state %s version %d, want %s %d unchanged", what, after.State, after.Version, before.State, before.Version)
	}
}

// g2WantApproved submits GRN id as Suresh and asserts it is approved (no
// approval rule in grntest).
func g2WantApproved(t *testing.T, what string, env *grntest.Env, id int64) {
	t.Helper()
	res, err := g2Submit(t, env, env.Suresh, id)
	if err != nil {
		t.Errorf("%s: submit: %v %+v, want approved", what, err, g1FieldErrors(err))
		return
	}
	if res.State != doctype.StateApproved {
		t.Errorf("%s: state %s, want approved", what, res.State)
	}
}

// GRN-007 (GRN spec §3.1, §4; CD-264 (2)): a GRN with a goods line and a
// blank challan no. saves as a draft, and submit refuses it on challan_no
// (422, state and version unchanged). A GRN whose only line is SVC-CRANE
// is approved with a blank challan.
func TestGRN_ChallanRule(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	h := g2Header(t, env, po)
	h["challan_no"] = ""
	d, err := g2Create(t, env, env.Suresh, h, g2Line(po.Lines[0], "100", "", ""))
	if err != nil {
		t.Fatalf("draft with a blank challan: %v %+v, want it saved (the rule is submit-only)", err, g1FieldErrors(err))
	}
	g2WantSubmitRefused(t, "goods GRN with a blank challan", env, d.ID, doctype.FieldError{Field: "challan_no", Message: g2MsgChallanNo})

	svc := g2ServicePO(t, env)
	sh := g2Header(t, env, svc)
	delete(sh, "challan_no")
	delete(sh, "challan_kind")
	s := g2MustCreate(t, env, env.Suresh, sh, g2Line(svc.Lines[0], "8", "", ""))
	if got := g2LineColumn(t, env, s.ID, "snap_is_service"); len(got) != 1 || got[0] != "1" {
		t.Errorf("SVC-CRANE line snap_is_service %v, want [1]", got)
	}
	g2WantApproved(t, "service-only GRN with a blank challan", env, s.ID)
}

// GRN-008 (GRN spec §3.2, §4): rejected above received is refused on save
// on that line's rejected_qty; rejected with a blank reason saves as a
// draft and submit refuses it on reject_reason. Line 2 carries the case, so
// {n} is the line's number. Submit returns every submit-only error
// together: a blank reason and a blank challan come back in one 422.
func TestGRN_RejectRules(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	_, err := g2Create(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "100", "", ""), g2Line(po.Lines[1], "100", "120", "Wet"))
	g2WantInvalid(t, "rejected 120 of 100", err,
		doctype.FieldError{Field: "rejected_qty", Line: 2, Message: "Line 2: Rejected can't be more than received."})

	d, err := g2Create(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "100", "", ""), g2Line(po.Lines[1], "100", "20", ""))
	if err != nil {
		t.Fatalf("draft with rejected 20 and no reason: %v %+v, want it saved", err, g1FieldErrors(err))
	}
	g2WantSubmitRefused(t, "rejected with no reason", env, d.ID,
		doctype.FieldError{Field: "reject_reason", Line: 2, Message: "Line 2: Reason for rejecting is required."})

	h := g2Header(t, env, po)
	h["challan_no"] = ""
	both := g2MustCreate(t, env, env.Suresh, h, g2Line(po.Lines[0], "100", "5", ""))
	g2WantSubmitRefused(t, "no reason and no challan", env, both.ID,
		doctype.FieldError{Field: "reject_reason", Line: 1, Message: "Line 1: Reason for rejecting is required."},
		doctype.FieldError{Field: "challan_no", Message: g2MsgChallanNo})
}

// GRN-045 (research Gap 5, S66: CGST Rules 55(1), 55A, 138A(1)(a); GRN spec
// §3.1, §4; CD-272): a goods GRN needs the kind of document at submit. A
// number with no kind is refused on challan_kind; both blank gives both
// errors, each on its field, in one 422; with Tax invoice it is approved. A
// service-only GRN with neither is approved. challan_kind takes only its
// two options (G1's Select).
func TestGRN_ChallanKind(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	h := g2Header(t, env, po)
	h["challan_no"], h["challan_kind"] = "SST/2627/0457", ""
	d, err := g2Create(t, env, env.Suresh, h, g2Line(po.Lines[0], "100", "", ""))
	if err != nil {
		t.Fatalf("draft with no kind: %v %+v, want it saved (the rule is submit-only)", err, g1FieldErrors(err))
	}
	g2WantSubmitRefused(t, "number and no kind", env, d.ID, doctype.FieldError{Field: "challan_kind", Message: g2MsgChallanKind})

	h2 := g2Header(t, env, po)
	h2["challan_no"], h2["challan_kind"] = "", ""
	blank := g2MustCreate(t, env, env.Suresh, h2, g2Line(po.Lines[0], "100", "", ""))
	g2WantSubmitRefused(t, "number and kind blank", env, blank.ID,
		doctype.FieldError{Field: "challan_kind", Message: g2MsgChallanKind},
		doctype.FieldError{Field: "challan_no", Message: g2MsgChallanNo})

	if _, err := g2Save(t, env, env.Suresh, d.ID, map[string]string{"challan_kind": "Tax invoice"}, g2StoredLines(t, env, d.ID, nil)); err != nil {
		t.Fatalf("choose Tax invoice: %v %+v", err, g1FieldErrors(err))
	}
	g2WantApproved(t, "goods GRN with a tax invoice", env, d.ID)

	svc := g2ServicePO(t, env)
	sh := g2Header(t, env, svc)
	sh["challan_no"], sh["challan_kind"] = "", ""
	s := g2MustCreate(t, env, env.Suresh, sh, g2Line(svc.Lines[0], "8", "", ""))
	g2WantApproved(t, "service-only GRN with no number and no kind", env, s.ID)

	bad := g2Header(t, env, po)
	bad["challan_kind"] = "Invoice"
	_, err = g2Create(t, env, env.Suresh, bad, g2Line(po.Lines[0], "100", "", ""))
	if fe := g1FieldErrors(err); err == nil || len(fe) == 0 || fe[0].Field != "challan_kind" {
		t.Errorf("challan_kind Invoice: err = %v %+v, want refused on challan_kind", err, fe)
	}
}

// GRN-022, G2's row (CD-302, CD-303; M0-10 amend runs Validate; G6 adds the
// restricted-visibility rows to this test): Ravi cancels Suresh's approved
// GRN and Suresh amends it on the real auth.Service. The amendment's header
// and line snapshots, vendor, site, project and units are filled from the
// current PO and its PO lines before any further save, not copied from the
// cancelled GRN: its stored copies are made wrong first, so a copy shows.
func TestGRN_AmendRestrictedReal(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	grn := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "100"}, {POLineID: po.Lines[1].ID, Received: "10"}}, doctype.StateApproved)
	env.Cancel(t, env.Ravi, grn.ID, "Wrong quantity entered")

	bharat, puneWH, mtr := g1MasterID(t, env.DB, "party", "BHARAT"), g1MasterID(t, env.DB, "site", "PUNE-WH"), g1MasterID(t, env.DB, "uom", "MTR")
	if err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
		if _, err := tx.SQL().ExecContext(t.Context(), `UPDATE goods_receipt SET vendor_id = ?, site_id = ?,
			snap_vendor_name = 'Stale Traders', snap_po_number = 'PO/26-27/09999', snap_site_name = 'Stale site',
			snap_project_code = 'STALE', accepted_value = 1 WHERE id = ?`, bharat, puneWH, grn.ID); err != nil {
			return err
		}
		_, err := tx.SQL().ExecContext(t.Context(), `UPDATE goods_receipt_line SET uom_id = ?, snap_rate = 1,
			snap_item_name = 'Stale item', snap_uom_code = 'MTR' WHERE parent_id = ?`, mtr, grn.ID)
		return err
	}); err != nil {
		t.Fatalf("make the cancelled GRN's copies wrong: %v", err)
	}

	amended := env.Amend(t, env.Suresh, grn.ID)
	if amended.State != doctype.StateDraft || amended.AmendedFromID != grn.ID || amended.CreatedBy != env.Suresh.ID {
		t.Fatalf("amendment: state %s from %d by %d, want a draft amended from %d by Suresh", amended.State, amended.AmendedFromID, amended.CreatedBy, grn.ID)
	}
	if got := g1Int(t, amended.Header["purchase_order_id"]); got != strconv.FormatInt(po.ID, 10) {
		t.Errorf("amendment purchase_order_id %s, want %d", got, po.ID)
	}
	g2CheckDerived(t, "amendment", env, amended.ID)
	if got := g2Column(t, env, amended.ID, "snap_po_number"); got != "PO/26-27/00001" {
		t.Errorf("amendment snap_po_number %q, want PO/26-27/00001", got)
	}
	if got := g2Column(t, env, amended.ID, "vendor_id"); got != g1MasterID(t, env.DB, "party", "SHREE") {
		t.Errorf("amendment vendor_id %q, want SHREE from the PO", got)
	}
	if got := g2Column(t, env, amended.ID, "accepted_value"); got != "721000" {
		t.Errorf("amendment accepted_value %q, want 721000 (100 × ₹62.5000 + 10 × ₹96.0000)", got)
	}

	// G6's rows (GRN spec §5, §10; CD-247; CD-213): the cancel was Ravi's,
	// a Purchase Manager (Stores has no cancel). The amendment carries the
	// cancelled GRN's lines, restricted Suresh can open it on the real
	// auth.Service, and his submit numbers it GR/26-27/00001-1.
	if got, want := g2LineColumn(t, env, amended.ID, "po_line_id"), []string{strconv.FormatInt(po.Lines[0].ID, 10), strconv.FormatInt(po.Lines[1].ID, 10)}; !slices.Equal(got, want) {
		t.Errorf("amendment po_line_id %v, want %v copied from the cancelled GRN", got, want)
	}
	if err := env.Auth.CheckLink(t.Context(), env.Suresh, goodsreceipt.DocType, amended.ID); err != nil {
		t.Errorf("Suresh can't read his amendment: %v", err)
	}
	if res, err := g2Submit(t, env, env.Suresh, amended.ID); err != nil || res.State != doctype.StateApproved {
		t.Fatalf("Suresh submits the amendment: %v %+v (state %s)", err, g1FieldErrors(err), res.State)
	}
	if got := env.Load(t, goodsreceipt.DocType, amended.ID).Number; got != "GR/26-27/00001-1" {
		t.Errorf("amendment number %q, want GR/26-27/00001-1", got)
	}
}
