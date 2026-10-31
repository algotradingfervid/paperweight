package goodsreceipt_test

import (
	"maps"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/types"
)

// g6Today is "today" under the clock GRN-003 sets: 18:31 UTC on 24 October
// is 00:01 IST on the 25th, so a UTC date would be a day early (AD-13).
var g6Today = types.Date{Year: 2026, Month: time.October, Day: 25}

// g6WantQty asserts form value got parses to want.
func g6WantQty(t *testing.T, what, got, want string) {
	t.Helper()
	g, err1 := types.ParseQty(got)
	w, err2 := types.ParseQty(want)
	if err1 != nil || err2 != nil || g != w {
		t.Errorf("%s = %q, want %s", what, got, want)
	}
}

// g6WantDraftLines asserts DraftFromPO's lines are exactly one per want
// entry, in order: po_line_id, received_qty = that line's pending and
// rejected_qty 0, and no other key (plan G6 Do 2).
func g6WantDraftLines(t *testing.T, what string, got []map[string]string, want [][2]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d lines %+v, want %d", what, len(got), got, len(want))
	}
	for i, w := range want {
		keys := slices.Sorted(maps.Keys(got[i]))
		if !slices.Equal(keys, []string{"po_line_id", "received_qty", "rejected_qty"}) {
			t.Errorf("%s line %d: keys %v, want po_line_id, received_qty, rejected_qty", what, i+1, keys)
		}
		if got[i]["po_line_id"] != w[0] {
			t.Errorf("%s line %d: po_line_id %q, want %s", what, i+1, got[i]["po_line_id"], w[0])
		}
		g6WantQty(t, what+" line "+strconv.Itoa(i+1)+" received_qty", got[i]["received_qty"], w[1])
		g6WantQty(t, what+" line "+strconv.Itoa(i+1)+" rejected_qty", got[i]["rejected_qty"], "0")
	}
}

// g6WantDraftValues asserts DraftFromPO's header values are exactly the PO,
// its site and project, and receipt date today (plan G6 Do 2). The date may
// be posted as ISO or as the display date; both are form dates.
func g6WantDraftValues(t *testing.T, what string, got map[string]string, po doc.Doc, today types.Date) {
	t.Helper()
	want := map[string]string{
		"purchase_order_id": strconv.FormatInt(po.ID, 10),
		"site_id":           g1Int(t, po.Header["site_id"]),
		"project_id":        g1Int(t, po.Header["project_id"]),
	}
	rd, ok := got["receipt_date"]
	if !ok || (rd != today.ISO() && rd != types.FormatDate(today)) {
		t.Errorf("%s: receipt_date %q, want today %s", what, rd, today.ISO())
	}
	rest := maps.Clone(got)
	delete(rest, "receipt_date")
	if !maps.Equal(rest, want) {
		t.Errorf("%s: values %v, want %v and receipt_date", what, got, want)
	}
}

// GRN-002 (GRN spec §8, §10; AUTH-101; CD-240, CD-312): an approved PO on
// TECH-SITE, raised by admin. Restricted Suresh (Stores, read on POs for
// PUNE-WH and METRO-SITE only), on the real auth.Service, gets AUTH-101's
// 403 with CD-240's text both from DraftFromPO (step 1) and from a direct
// Engine.Do(create) posting METRO-SITE as the site; no draft is written.
func TestGRN_PONotVisibleReal(t *testing.T) {
	env := grntest.New(t)
	tech := g6TechPO(t, env)
	want := g6LinkErr(t, env, env.Suresh, tech.ID)
	g6WantForbidden(t, "auth.CheckLinkTx itself", want)
	before := env.RowCounts(t)

	values, lines, err := env.Service.DraftFromPO(t.Context(), env.Suresh, tech.ID)
	g6WantForbidden(t, "DraftFromPO", err)
	if err == nil || err.Error() != want.Error() {
		t.Errorf("DraftFromPO err %v, want what auth.CheckLinkTx returns: %v", err, want)
	}
	if values != nil || lines != nil {
		t.Errorf("DraftFromPO refused but returned values %v lines %v", values, lines)
	}

	header := map[string]string{
		"purchase_order_id": strconv.FormatInt(tech.ID, 10),
		"site_id":           g1MasterID(t, env.DB, "site", "METRO-SITE"),
		"project_id":        g1MasterID(t, env.DB, "project", "PRJ-METRO"),
		"receipt_date":      types.Today(env.Clock).ISO(),
	}
	_, err = g2Create(t, env, env.Suresh, header, g2Line(tech.Lines[0], "10", "", ""))
	g6WantForbidden(t, "Engine.Do create", err)

	if after := env.RowCounts(t); after != before {
		t.Errorf("rows %+v after the refusals, want %+v: nothing written", after, before)
	}
}

// GRN-003 (GRN spec §3.1, §3.2, §11): the reference PO with 400 kg of line 1
// already received on an approved GRN. DraftFromPO for Suresh gives the PO,
// its site and project, today's date (an IST-midnight clock) and one line
// per PO line with pending: 800 and 50, rejected 0. The draft the engine
// creates from them has vendor SHREE, METRO-SITE, PRJ-METRO and every
// snapshot from the PO (CD-302). A fully received PO gives no lines and a
// draft with none. DraftFromPO checks no state: a closed PO's values come
// back and the engine refuses it (GRN-037, DOC-017 is G4's).
func TestGRN_CopiesFromPO(t *testing.T) {
	env := grntest.New(t)
	env.SetNow(t, time.Date(2026, time.October, 24, 18, 31, 0, 0, time.UTC))
	po := g2RefPO(t, env)
	env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "400"}}, doctype.StateApproved)

	values, lines, err := env.Service.DraftFromPO(t.Context(), env.Suresh, po.ID)
	if err != nil {
		t.Fatalf("DraftFromPO: %v", err)
	}
	g6WantDraftValues(t, "reference PO", values, po, g6Today)
	g6WantDraftLines(t, "reference PO", lines, [][2]string{
		{strconv.FormatInt(po.Lines[0].ID, 10), "800"},
		{strconv.FormatInt(po.Lines[1].ID, 10), "50"},
	})

	res, err := env.Engine.Do(t.Context(), env.Suresh, goodsreceipt.DocType, 0, doctype.ActionSave, doc.ActionInput{Values: values, Lines: lines})
	if err != nil {
		t.Fatalf("create from DraftFromPO: %v %+v", err, g1FieldErrors(err))
	}
	draft := env.Load(t, goodsreceipt.DocType, res.ID)
	if draft.State != doctype.StateDraft || draft.CreatedBy != env.Suresh.ID {
		t.Errorf("draft state %s by %d, want a draft by Suresh", draft.State, draft.CreatedBy)
	}
	g2CheckDerived(t, "draft from PO", env, draft.ID)
	for col, want := range map[string]string{
		"vendor_id":        g1MasterID(t, env.DB, "party", "SHREE"),
		"site_id":          g1MasterID(t, env.DB, "site", "METRO-SITE"),
		"project_id":       g1MasterID(t, env.DB, "project", "PRJ-METRO"),
		"snap_po_number":   "PO/26-27/00001",
		"snap_po_date":     g6Today.ISO(),
		"snap_vendor_name": "Shree Steel Traders",
		"receipt_date":     g6Today.ISO(),
	} {
		if got := g2Column(t, env, draft.ID, col); got != want {
			t.Errorf("draft %s = %q, want %q", col, got, want)
		}
	}
	for _, col := range []string{"snap_vendor_gstin", "snap_vendor_address", "snap_site_name"} {
		if g2Column(t, env, draft.ID, col) == "" {
			t.Errorf("draft %s is blank, want the PO's", col)
		}
	}
	if got := g2LineColumn(t, env, draft.ID, "received_qty"); !slices.Equal(got, []string{"800000", "50000"}) {
		t.Errorf("draft received_qty %v, want [800000 50000] (800.000 and 50.000 kg)", got)
	}
	if got := g2LineColumn(t, env, draft.ID, "rejected_qty"); !slices.Equal(got, []string{"0", "0"}) {
		t.Errorf("draft rejected_qty %v, want [0 0]", got)
	}

	// A fully received PO: values, no lines, and a draft with none.
	full := g6Order(t, env, nil)
	env.GRN(t, env.Suresh, full.ID, []grntest.Line{{POLineID: full.Lines[0].ID, Received: "100"}}, doctype.StateApproved)
	values, lines, err = env.Service.DraftFromPO(t.Context(), env.Suresh, full.ID)
	if err != nil {
		t.Fatalf("DraftFromPO of a fully received PO: %v", err)
	}
	g6WantDraftValues(t, "fully received PO", values, full, g6Today)
	if len(lines) != 0 {
		t.Errorf("fully received PO: lines %+v, want none", lines)
	}
	res, err = env.Engine.Do(t.Context(), env.Suresh, goodsreceipt.DocType, 0, doctype.ActionSave, doc.ActionInput{Values: values, Lines: lines})
	if err != nil {
		t.Fatalf("create from a fully received PO: %v %+v", err, g1FieldErrors(err))
	}
	if n := len(env.Load(t, goodsreceipt.DocType, res.ID).Lines); n != 0 {
		t.Errorf("draft from a fully received PO has %d lines, want none", n)
	}

	// No state check: a closed PO's values come back; the engine refuses.
	closed := g6Order(t, env, nil)
	env.ClosePO(t, env.Priya, closed.ID, "Vendor stopped supplying")
	values, lines, err = env.Service.DraftFromPO(t.Context(), env.Suresh, closed.ID)
	if err != nil {
		t.Fatalf("DraftFromPO of a closed PO: %v, want its values (the engine refuses, GRN-037)", err)
	}
	g6WantDraftValues(t, "closed PO", values, closed, g6Today)
	before := env.RowCounts(t)
	if _, err := env.Engine.Do(t.Context(), env.Suresh, goodsreceipt.DocType, 0, doctype.ActionSave, doc.ActionInput{Values: values, Lines: lines}); err == nil {
		t.Errorf("create from a closed PO's draft values succeeded, want the engine's source-state refusal")
	}
	if after := env.RowCounts(t); after != before {
		t.Errorf("rows %+v after the closed PO's refusal, want %+v", after, before)
	}
}

// GRN-029, the service rows (GRN spec §3.1, §8, §10; CD-240; CD-245): Suresh
// on the real auth.Service. A blank PO is "Choose a purchase order." as a
// 422 on purchase_order_id from DraftFromPO, not the engine's 404 for a
// restricted create with no site. He creates a GRN for the reference PO
// from DraftFromPO and it is on METRO-SITE and readable by him; a TECH-SITE
// PO gets CD-240's 403; his draft saved with site_id PUNE-WH (a site he may
// use) is reset to the PO's METRO-SITE by Validate. S1 adds the preselect
// and ReceiveLink rows.
func TestGRN_RestrictedCreateReal(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	tech := g6TechPO(t, env)
	metro := g1MasterID(t, env.DB, "site", "METRO-SITE")
	before := env.RowCounts(t)

	values, lines, err := env.Service.DraftFromPO(t.Context(), env.Suresh, 0)
	g2WantInvalid(t, "DraftFromPO blank PO", err, doctype.FieldError{Field: "purchase_order_id", Message: g2MsgChoosePO})
	if values != nil || lines != nil {
		t.Errorf("blank PO refused but returned values %v lines %v", values, lines)
	}

	values, lines, err = env.Service.DraftFromPO(t.Context(), env.Suresh, tech.ID)
	g6WantForbidden(t, "DraftFromPO TECH-SITE PO", err)
	if values != nil || lines != nil {
		t.Errorf("TECH-SITE PO refused but returned values %v lines %v", values, lines)
	}
	if after := env.RowCounts(t); after != before {
		t.Errorf("rows %+v after the refusals, want %+v", after, before)
	}

	values, lines, err = env.Service.DraftFromPO(t.Context(), env.Suresh, po.ID)
	if err != nil {
		t.Fatalf("DraftFromPO reference PO: %v", err)
	}
	res, err := env.Engine.Do(t.Context(), env.Suresh, goodsreceipt.DocType, 0, doctype.ActionSave, doc.ActionInput{Values: values, Lines: lines})
	if err != nil {
		t.Fatalf("Suresh creates from DraftFromPO: %v %+v", err, g1FieldErrors(err))
	}
	if got := g2Column(t, env, res.ID, "site_id"); got != metro {
		t.Errorf("created GRN site_id %s, want METRO-SITE %s", got, metro)
	}
	if err := env.Auth.CheckLink(t.Context(), env.Suresh, goodsreceipt.DocType, res.ID); err != nil {
		t.Errorf("Suresh can't read the GRN he created: %v", err)
	}

	// A tampered site_id (PUNE-WH, which Suresh may use, so the rights
	// check passes) is reset from the PO by Validate (spec §3.1).
	tampered := maps.Clone(values)
	tampered["site_id"] = g1MasterID(t, env.DB, "site", "PUNE-WH")
	if _, err := g2Save(t, env, env.Suresh, res.ID, tampered, g2StoredLines(t, env, res.ID, nil)); err != nil {
		t.Fatalf("save with site_id PUNE-WH: %v %+v", err, g1FieldErrors(err))
	}
	if got := g2Column(t, env, res.ID, "site_id"); got != metro {
		t.Errorf("site_id after posting PUNE-WH = %s, want the PO's METRO-SITE %s", got, metro)
	}
}
