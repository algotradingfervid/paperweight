package goodsreceipt_test

import (
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// MOD-GRN G6 test helpers. POs and GRNs are made through Engine.Do with
// purchasetest and grntest builders (research §2 pattern 2, CD-125); every
// principal is a fixture user on the real auth.Service (CD-213).

// g6MsgNotAvailable is AUTH-101's refusal as CD-240 amends it, for a PO.
const g6MsgNotAvailable = "You can't use this Purchase Order. It isn't available to you."

// g6TechPO is an approved PO on TECH-SITE / PRJ-TECHPARK raised by admin
// (Priya is restricted to PRJ-METRO; 00-fixtures.md GRN test seed), which
// Suresh (PUNE-WH, METRO-SITE) can't read.
func g6TechPO(t *testing.T, env *grntest.Env) doc.Doc {
	t.Helper()
	return grntest.POAs(t, env, env.Admin, doctype.StateApproved, map[string]string{
		"site_id": g1MasterID(t, env.DB, "site", "TECH-SITE"), "project_id": g1MasterID(t, env.DB, "project", "PRJ-TECHPARK"),
	})
}

// g6Approve submits draft PO id as Priya; with no PO rule it approves and
// is numbered then (grntest seed).
func g6Approve(t *testing.T, env *grntest.Env, id int64) doc.Doc {
	t.Helper()
	res, err := env.Engine.Do(t.Context(), env.Priya, purchase.DocType, id, doctype.ActionSubmit,
		doc.ActionInput{Version: env.Load(t, purchase.DocType, id).Version})
	if err != nil || res.State != doctype.StateApproved {
		t.Fatalf("approve PO %d: %v (state %s)", id, err, res.State)
	}
	return env.Load(t, purchase.DocType, id)
}

// g6Order is purchasetest.NewOrder with header values over its defaults.
func g6Order(t *testing.T, env *grntest.Env, header map[string]string, opts ...purchasetest.OrderOpt) doc.Doc {
	t.Helper()
	return purchasetest.NewOrder(t, env.Env, append([]purchasetest.OrderOpt{purchasetest.OrderHeader(header)}, opts...)...)
}

// g6LinkErr is what auth.CheckLinkTx returns for p on PO id, read in one
// transaction: the error the plan says DraftFromPO passes on (G6 Do 2).
func g6LinkErr(t *testing.T, env *grntest.Env, p auth.Principal, id int64) error {
	t.Helper()
	var out error
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		out = env.Auth.CheckLinkTx(t.Context(), tx, p, purchase.DocType, id)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// g6WantForbidden asserts err is a 403 whose text is exactly CD-240's.
func g6WantForbidden(t *testing.T, what string, err error) {
	t.Helper()
	var he doctype.HTTPError
	if err == nil || !errors.As(err, &he) {
		t.Errorf("%s: err = %v, want a 403 %q", what, err, g6MsgNotAvailable)
		return
	}
	if he.Status() != 403 || err.Error() != g6MsgNotAvailable {
		t.Errorf("%s: %d %q, want 403 %q (CD-240)", what, he.Status(), err.Error(), g6MsgNotAvailable)
	}
}
