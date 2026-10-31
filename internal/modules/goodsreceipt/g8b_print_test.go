package goodsreceipt_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/masters/allmasters"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// G8b · Reverse-charge note (GRN spec §7: "Reverse charge lives on the PO.
// The GRN print copies the note."; AD-13, CD-316 (1)). Prints go through the
// real print handler over the real auth.Service; the note is copied only
// when the viewer can read the PO (CD-233).

// g8bRCPO is an approved PO by Priya with reverse charge on: one line of
// 100 kg MS-ANGLE-50 at ₹62.50, GST18, so its GST is ₹1,125.00.
func g8bRCPO(t *testing.T, env *grntest.Env) doc.Doc {
	t.Helper()
	return purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved(),
		purchasetest.OrderHeader(map[string]string{"reverse_charge": "true"}))
}

// g8bGRN is a draft GRN by Suresh receiving 100 kg on po's first line.
func g8bGRN(t *testing.T, env *grntest.Env, po doc.Doc) int64 {
	t.Helper()
	return g2MustCreate(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "100", "", "")).ID
}

// g8bPONote is purchase.ReverseChargeNote for PO id, the PO print's text.
func g8bPONote(t *testing.T, env *grntest.Env, id int64) string {
	t.Helper()
	var note string
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		note, err = purchase.ReverseChargeNote(t.Context(), tx, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return note
}

// A GRN on a reverse-charge PO prints the PO print's note exactly; a GRN on
// a normal PO prints none.
func TestGRN_PrintReverseChargeNote(t *testing.T) {
	env := grntest.New(t)
	po := g8bRCPO(t, env)
	note := g8bPONote(t, env, po.ID)
	if want := "Reverse charge applies. GST of ₹1,125.00 is payable by us, not to the vendor."; note != want {
		t.Fatalf("PO note = %q, want %q", note, want)
	}
	html := g8Print(t, env, env.Suresh, g8bGRN(t, env, po))
	g8Want(t, "reverse-charge PO", html, ".reverse-charge", note)

	plain := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved())
	g8Want(t, "normal PO", g8Print(t, env, env.Suresh, g8bGRN(t, env, plain)), ".reverse-charge")
}

// CD-233: a viewer who can read the GRN but not its PO gets no note. An
// administrator takes PO read away from Stores (GRN spec §10 keeps Stores'
// GRN rights), so Suresh still prints the GRN.
func TestGRN_PrintReverseChargeNoteNeedsPORead(t *testing.T) {
	env := grntest.New(t)
	po := g8bRCPO(t, env)
	id := g8bGRN(t, env, po)
	if err := env.Auth.SetRights(t.Context(), env.Admin, "Stores", purchase.DocType, map[auth.Right]auth.Scope{}); err != nil {
		t.Fatal(err)
	}
	if err := g6LinkErr(t, env, env.Suresh, po.ID); err == nil {
		t.Fatal("Suresh can still read the PO")
	}
	g8Want(t, "no PO read", g8Print(t, env, env.Suresh, id), ".reverse-charge")
	g8Want(t, "admin", g8Print(t, env, env.Admin, id), ".reverse-charge", g8bPONote(t, env, po.ID))
}

// A GRN with no PO stored (a draft saved before step 1's PO) prints, with no
// note. The test blanks the column after the save.
func TestGRN_PrintReverseChargeNoPO(t *testing.T) {
	env := grntest.New(t)
	id := g8bGRN(t, env, g8bRCPO(t, env))
	if _, err := env.DB.SQL().ExecContext(t.Context(), `UPDATE goods_receipt SET purchase_order_id = NULL WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	g8Want(t, "no PO", g8Print(t, env, env.Suresh, id), ".reverse-charge")
}

// The print view called with no principal in its context (outside the
// print handler) leaves the note out rather than guessing a reader.
func TestGRN_PrintReverseChargeNoPrincipal(t *testing.T) {
	env := grntest.New(t)
	id := g8bGRN(t, env, g8bRCPO(t, env))
	view, ok := print.Default.Lookup(goodsreceipt.DocType)
	if !ok {
		t.Fatal("no GRN print view")
	}
	kit := mstkit.Env{DB: env.DB, Auth: env.Auth, Engine: env.Engine, Auditor: audit.Default, Clock: env.Clock}
	ctx := print.WithLetterhead(context.Background(), allmasters.New(kit).Letterhead)
	var buf bytes.Buffer
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		c, err := view(ctx, tx, id)
		if err != nil {
			return err
		}
		return c.Render(ctx, &buf)
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "reverse-charge") || !strings.Contains(buf.String(), "Goods Receipt Note") {
		t.Errorf("print with no principal: want the page without a note:\n%s", buf.String())
	}
}
