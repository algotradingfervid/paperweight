package goodsreceipt_test

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// g7Approve sets draft amendment amID's line 1 received to received (blank
// keeps the copied lines) as Suresh and submits it; with no GRN rule it
// approves.
func g7Approve(t *testing.T, env *grntest.Env, poID, amID int64, received string) {
	t.Helper()
	if received != "" {
		po := env.Load(t, purchase.DocType, poID)
		if _, err := g2Save(t, env, env.Suresh, amID, g2Header(t, env, po),
			g2StoredLines(t, env, amID, map[int]map[string]string{1: {"received_qty": received}})); err != nil {
			t.Fatalf("save the amendment at %s: %v %+v", received, err, g1FieldErrors(err))
		}
	}
	if _, err := g2Submit(t, env, env.Suresh, amID); err != nil {
		t.Fatalf("submit the amendment %d: %v %+v", amID, err, g1FieldErrors(err))
	}
	g5WantState(t, "the amendment", env, amID, doctype.StateApproved)
}

// GRN-042 (research Gap 1, S52: ERPNext test_purchase_order.py:781, Odoo
// test_purchase_order.py:83; GRN spec §3.3, §5; CD-272 Gap 1, CD-279 (11),
// (12); plan G7 Do 7): cancel and amend as the return. GR/26-27/00001
// accepts 800 kg of line 1 and gets bill details (saved by the
// Administrator); Ravi cancels it, Suresh amends it to 700 and submits. The
// walk reads pending 1,200 → 400 → 1,200 → 500. The cancelled GRN keeps its
// row and refuses a save; the draft amendment has none (the move is at
// approval); the approved GR/26-27/00001-1 has a copy at version 1 and one
// audit entry "Copied from GR/26-27/00001" by its updated_by, and the old
// row is as it was. Locked up to the GRN's date, the amendment's cancel is
// refused with the lock text and nothing changes. No copy is made when the
// original has no bill, nor over a row the amendment already holds.
func TestGRN_CancelAmendReturn(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	g3CheckPO(t, "ordered", env, po.ID, g3Want{received: []string{"0", "0"}, pending: []string{"1200", "50"}, status: "not_received"})
	grn := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "800"), doctype.StateApproved)
	g3CheckPO(t, "received", env, po.ID, g3Want{received: []string{"800", "0"}, pending: []string{"400", "50"}, status: "part_received"})
	g7MustSave(t, "the Administrator's bill", env, env.Admin, grn.ID, 0, g7In(g7Invoice, "24-10-2026", "61,714.00"))

	env.Cancel(t, env.Ravi, grn.ID, g4CancelReason)
	g3CheckPO(t, "cancelled", env, po.ID, g3Want{received: []string{"0", "0"}, pending: []string{"1200", "50"}, status: "not_received"})
	orig := g7Bill(t, env, grn.ID)
	g7WantView(t, "the cancelled GRN", orig, 1, g7Invoice, g7Oct(24), g7Money(61_714_00), "")
	before := g7Facts(t, env)
	_, err := g7Save(t, env, env.Suresh, grn.ID, 1, g7In(g7Invoice, "24-10-2026", "1"))
	g7WantStatus(t, "save on the cancelled GRN", err, http.StatusUnprocessableEntity, g7MsgNotApproved)
	g7WantUnchanged(t, "the refused save on the cancelled GRN", env, before)

	am := env.Amend(t, env.Suresh, grn.ID)
	g7WantView(t, "the draft amendment", g7Bill(t, env, am.ID), 0, "", g7NoDate, nil, "")
	if n := env.RowCounts(t).Bills; n != 1 {
		t.Errorf("grn_bill rows after amend = %d, want 1 (the move is at approval, CD-279 (12))", n)
	}
	g7Approve(t, env, po.ID, am.ID, "700")
	if got := env.Load(t, goodsreceipt.DocType, am.ID).Number; got != g4FirstAmend {
		t.Errorf("amendment number %q, want %q", got, g4FirstAmend)
	}
	g3CheckPO(t, "amended", env, po.ID, g3Want{received: []string{"700", "0"}, pending: []string{"500", "50"}, status: "part_received"})

	g7WantView(t, "the approved amendment", g7Bill(t, env, am.ID), 1, g7Invoice, g7Oct(24), g7Money(61_714_00), "")
	copied := g7Copied(t, env, am.ID)
	updatedBy := g2Int(t, g2Column(t, env, am.ID, "updated_by"))
	if len(copied) != 1 || copied[0].Reason != "Copied from "+g4FirstGRN || int64(copied[0].By) != updatedBy || updatedBy != int64(env.Suresh.ID) {
		t.Errorf("copy audit on the amendment = %+v, want one entry %q by its updated_by %d (Suresh %d)",
			copied, "Copied from "+g4FirstGRN, updatedBy, env.Suresh.ID)
	}
	old := g7Bill(t, env, grn.ID)
	g7WantView(t, "the original after the move", old, 1, g7Invoice, g7Oct(24), g7Money(61_714_00), "")
	if old.UpdatedBy != env.Admin.ID || !old.UpdatedAt.Equal(orig.UpdatedAt) {
		t.Errorf("the original's row changed: by %d at %s, want by %d at %s", old.UpdatedBy, old.UpdatedAt, env.Admin.ID, orig.UpdatedAt)
	}
	if n := env.RowCounts(t).Bills; n != 2 {
		t.Errorf("grn_bill rows after the amendment's approval = %d, want 2 (the old row stays)", n)
	}
	if n := len(g7Copied(t, env, grn.ID)); n != 0 {
		t.Errorf("the original has %d copy entries, want 0 (the entry is on the new GRN)", n)
	}

	// The lock refuses the amendment's cancel (G5's checkLock in
	// ReverseEffects): nothing changes.
	env.Lock(t, types.Today(env.Clock))
	before = g7Facts(t, env)
	_, err = g5Cancel(t, env, am.ID)
	// M0-7's lock text (modkit; CD-142), locked up to the GRN's own date.
	g5WantRefusal(t, "cancel under the lock", err,
		"Goods Receipt "+g4FirstAmend+" is dated 24-10-2026, which is locked. The period is locked up to 24-10-2026.")
	g7WantUnchanged(t, "the refused cancel", env, before)
	g5WantState(t, "the amendment after the refused cancel", env, am.ID, doctype.StateApproved)
	env.Unlock(t)

	// No copy when the original has no bill.
	plain := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[1].ID, Received: "50"}}, doctype.StateApproved)
	env.Cancel(t, env.Ravi, plain.ID, g4CancelReason)
	plainAm := env.Amend(t, env.Suresh, plain.ID)
	bills := env.RowCounts(t).Bills
	g7Approve(t, env, po.ID, plainAm.ID, "")
	g7WantView(t, "amendment of a GRN with no bill", g7Bill(t, env, plainAm.ID), 0, "", g7NoDate, nil, "")
	if n := env.RowCounts(t).Bills; n != bills || len(g7Copied(t, env, plainAm.ID)) != 0 {
		t.Errorf("amendment of a GRN with no bill: grn_bill rows %d (want %d), copy entries %d (want 0)",
			n, bills, len(g7Copied(t, env, plainAm.ID)))
	}

	// No copy over a row the amendment already holds. No public path gives
	// a draft amendment a row (SaveBill refuses drafts), so the row is
	// inserted directly to reach plan G7 Do 7's "no bill row" condition.
	po2 := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved())
	held := env.GRN(t, env.Suresh, po2.ID, g4Line1(po2, "40"), doctype.StateApproved)
	g7MustSave(t, "the bill to be held", env, env.Suresh, held.ID, 0, g7In("C/INV/1", "24-10-2026", ""))
	env.Cancel(t, env.Ravi, held.ID, g4CancelReason)
	heldAm := env.Amend(t, env.Suresh, held.ID)
	at := types.TimestampOf(env.Clock.Now()).String()
	if err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(t.Context(), `INSERT INTO grn_bill
			(grn_id, invoice_no, invoice_date, invoice_amount, version, created_at, created_by, updated_at, updated_by)
			VALUES (?, 'PRESET/9', '2026-10-24', NULL, 3, ?, ?, ?, ?)`, heldAm.ID, at, env.Suresh.ID, at, env.Suresh.ID)
		return err
	}); err != nil {
		t.Fatalf("give the draft amendment a row: %v", err)
	}
	g7Approve(t, env, po2.ID, heldAm.ID, "")
	g7WantView(t, "amendment already holding a row", g7Bill(t, env, heldAm.ID), 3, "PRESET/9", g7Oct(24), nil, "")
	if n := len(g7Copied(t, env, heldAm.ID)); n != 0 {
		t.Errorf("amendment already holding a row: %d copy entries, want 0", n)
	}
}

// g7Photo attaches a small JPEG named name to GRN id as p through the real
// files.Store (env.Files).
func g7Photo(t *testing.T, env *grntest.Env, p auth.Principal, id int64, name string) error {
	t.Helper()
	sp, err := env.Files.Spool(bytes.NewReader(filestest.JPEG(2048)), files.MaxFileBytes)
	if err != nil {
		t.Fatalf("spool: %v", err)
	}
	defer func() { _ = sp.Cleanup() }()
	return env.DB.Write(t.Context(), func(tx *db.Tx) error {
		_, err := env.Files.Attach(t.Context(), tx, p, auth.RecordRef{DocType: goodsreceipt.DocType, ID: id}, name, sp)
		return err
	})
}

// g7Files lists GRN id's attachments in the real store.
func g7Files(t *testing.T, env *grntest.Env, id int64) []files.Meta {
	t.Helper()
	var ms []files.Meta
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		ms, err = env.Files.ListTx(t.Context(), tx, auth.RecordRef{DocType: goodsreceipt.DocType, ID: id})
		return err
	}); err != nil {
		t.Fatalf("list files of GRN %d: %v", id, err)
	}
	return ms
}

// GRN-028 (GRN spec §3.3; 05-files-print.md D-FILE-06; CD-243; plan G7
// Tests): Suresh attaches the invoice photo to an approved GRN through the
// real files.Store and it is listed on the GRN; he can't remove it after
// approval (files' own text) and it stays. No new code. files.Store itself
// doesn't read Attachable: the Attachments card with its upload control on
// the document page is drawn only for an Attachable DocType (docscreen
// attachOf, CD-241, CD-243), so the registered DocType is asserted too, and
// the test fails if Attachable is removed from register.go.
func TestGRN_LatePhoto(t *testing.T) {
	if dt, ok := doctype.Lookup(goodsreceipt.DocType); !ok || !dt.Attachable {
		t.Errorf("%s registered %v, Attachable = %v, want true (the page offers no upload otherwise)", goodsreceipt.DocType, ok, ok && dt.Attachable)
	}
	env := grntest.New(t)
	po := g2RefPO(t, env)
	grn := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "800"), doctype.StateApproved)

	if err := g7Photo(t, env, env.Suresh, grn.ID, "invoice.jpg"); err != nil {
		t.Fatalf("Suresh attaches the invoice photo to the approved GRN: %v", err)
	}
	got := g7Files(t, env, grn.ID)
	if len(got) != 1 || got[0].Name != "invoice.jpg" || got[0].MIME != "image/jpeg" || got[0].UploadedBy != env.Suresh.ID {
		t.Fatalf("GRN %d files = %+v, want invoice.jpg (image/jpeg) by Suresh", grn.ID, got)
	}

	err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
		return env.Files.Remove(t.Context(), tx, env.Suresh, got[0].ID)
	})
	const locked = "Files can't be removed after the document is submitted. Cancel and amend it to change files."
	if err == nil || err.Error() != locked {
		t.Errorf("remove after approval: err = %v, want %q", err, locked)
	}
	if n := len(g7Files(t, env, grn.ID)); n != 1 {
		t.Errorf("GRN %d has %d files after the refused remove, want 1", grn.ID, n)
	}
}
