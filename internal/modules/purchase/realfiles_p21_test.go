package purchase_test

import (
	"bytes"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// p21Attach attaches a small CSV to order id as p through env.Files.
func p21Attach(t *testing.T, env *purchasetest.Env, p auth.Principal, id int64) {
	t.Helper()
	content := []byte("item,qty\nMS-ANGLE-50,100\n")
	sp, err := env.Files.Spool(bytes.NewReader(content), files.MaxFileBytes)
	if err != nil {
		t.Fatalf("spool: %v", err)
	}
	defer func() { _ = sp.Cleanup() }()
	if err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
		_, err := env.Files.Attach(t.Context(), tx, p, auth.RecordRef{DocType: purchase.DocType, ID: id}, "quote.csv", sp)
		return err
	}); err != nil {
		t.Fatalf("attach to order %d as %s: %v", id, p.Name, err)
	}
}

// p21Files lists order id's attachments in the real store.
func p21Files(t *testing.T, env *purchasetest.Env, id int64) []files.Meta {
	t.Helper()
	var ms []files.Meta
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		ms, err = env.Files.ListTx(t.Context(), tx, auth.RecordRef{DocType: purchase.DocType, ID: id})
		return err
	}); err != nil {
		t.Fatalf("list files of order %d: %v", id, err)
	}
	return ms
}

// With WithRealFiles the engine's CopyAll runs against the real store: an
// approved PO's attachment, after cancel and amend, is on the amended draft.
func TestPO_RealFiles_AmendCopiesAttachment(t *testing.T) {
	env := purchasetest.New(t, purchasetest.WithRealFiles())
	po := purchasetest.NewOrder(t, env)
	p21Attach(t, env, env.Priya, po.ID)

	p2Must(t, "Ravi cancels")(p2Do(t, env, env.Ravi, po.ID, doc.ActionCancel, "Wrong rate"))
	am := p2Must(t, "Priya amends")(p2Do(t, env, env.Priya, po.ID, doc.ActionAmend, ""))
	got := p21Files(t, env, am.ID)
	if len(got) != 1 || got[0].Name != "quote.csv" || got[0].UploadedBy != env.Priya.ID {
		t.Fatalf("amended draft %d files = %+v, want quote.csv uploaded by Priya", am.ID, got)
	}
}

// With WithRealFiles the engine's DeleteAll runs against the real store:
// deleting a draft removes its attachment.
func TestPO_RealFiles_DeleteDraftRemovesAttachment(t *testing.T) {
	env := purchasetest.New(t, purchasetest.WithRealFiles())
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	p21Attach(t, env, env.Priya, po.ID)
	if n := len(p21Files(t, env, po.ID)); n != 1 {
		t.Fatalf("draft has %d files before delete, want 1", n)
	}

	p2Must(t, "Priya deletes the draft")(p2Do(t, env, env.Priya, po.ID, doc.ActionDelete, ""))
	if got := p21Files(t, env, po.ID); len(got) != 0 {
		t.Fatalf("deleted draft %d files = %+v, want none", po.ID, got)
	}
}
