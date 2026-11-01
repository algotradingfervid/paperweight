package goodsreceipt_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// MOD-GRN G7 test helpers (bill details, GRN spec §3.3, CD-234). GRNs and
// POs are made through Engine.Do with the grntest builders; the bill is
// written only through Service.SaveBill and the amendment's approval
// (copyBillOnAmend), never by inserting rows, except where a case needs a
// state no public path can reach (said at the call).

// G7's texts: spec §3.3 and CD-279 (3), (4), (11); DOC-069's msgStale with
// the label Bill details (as CD-262 did for Business settings); DOC's
// msgMaxLen with the field's label (UX §5.7); AUTH's write refusal for the
// Goods Receipt (00-fixtures.md).
const (
	g7MsgNotApproved  = "Add bill details after the receipt is approved."
	g7MsgNoRequired   = "Vendor invoice no. is required."
	g7MsgNoMaxLen     = "Vendor invoice no. can be at most 40 characters."
	g7MsgDateRequired = "Invoice date is required."
	g7MsgDateFuture   = "Invoice date can't be in the future."
	g7MsgNegative     = "Invoice total can't be negative."
	g7MsgStale        = "Someone else changed Bill details after you opened it. Reload it and make your changes again."
	g7MsgNoWrite      = "You don't have permission to change Goods Receipt. Ask your administrator for a role that allows it."
	g7MsgCopiedNote   = "Copied from the tax invoice entered at the gate. Check and save."
	g7Invoice         = "SST/2627/0457" // the vendor invoice of grn.md
)

// g7Accepted is spec §3.3's information line for an accepted value x
// (types.FormatMoney form).
func g7Accepted(x string) string { return "Accepted value before tax is ₹" + x + "." }

// g7Dup is the duplicate-invoice warning naming GRN gr (spec §3.3).
func g7Dup(no, gr string) string { return "Invoice " + no + " is also on " + gr + "." }

// g7DupHidden is the duplicate-invoice warning when the viewer can't read
// the other GRN (CD-279 (3); CD-240, CD-248).
func g7DupHidden(no string) string { return "Invoice " + no + " is also on another receipt." }

// g7In is a BillInput as the form posts it.
func g7In(no, date, amount string) goodsreceipt.BillInput {
	return goodsreceipt.BillInput{InvoiceNo: no, InvoiceDate: date, InvoiceAmount: amount}
}

// g7Save saves GRN id's bill details as p at version and returns the
// card's warnings and information lines as p then reads them (BillCard):
// SaveBill returns none, so a save and later renders agree (S3b review).
func g7Save(t *testing.T, env *grntest.Env, p auth.Principal, id int64, version int, in goodsreceipt.BillInput) ([]string, error) {
	t.Helper()
	if err := env.Service.SaveBill(t.Context(), p, id, version, in); err != nil {
		return nil, err
	}
	c, err := env.Service.BillCard(t.Context(), p, id)
	if err != nil {
		t.Fatalf("BillCard(GRN %d) as %s after a save: %v", id, p.Name, err)
	}
	return append(c.Warnings, c.Info...), nil
}

// g7MustSave is g7Save that stops the test on any error.
func g7MustSave(t *testing.T, what string, env *grntest.Env, p auth.Principal, id int64, version int, in goodsreceipt.BillInput) []string {
	t.Helper()
	w, err := g7Save(t, env, p, id, version, in)
	if err != nil {
		t.Fatalf("%s: SaveBill on GRN %d as %s at version %d: %v %+v", what, id, p.Name, version, err, g1FieldErrors(err))
	}
	return w
}

// g7Bill reads GRN id's bill view in one read transaction.
func g7Bill(t *testing.T, env *grntest.Env, id int64) goodsreceipt.BillView {
	t.Helper()
	var v goodsreceipt.BillView
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		v, err = goodsreceipt.Bill(t.Context(), tx, id)
		return err
	}); err != nil {
		t.Fatalf("Bill(GRN %d): %v", id, err)
	}
	return v
}

// g7Facts is g3Snapshot plus every grn_bill row: what a refused save, or a
// read, must leave as it was.
func g7Facts(t *testing.T, env *grntest.Env) string {
	t.Helper()
	return g3Snapshot(t, env) + fmt.Sprint(g2Query(t, env.DB,
		`SELECT grn_id, invoice_no, invoice_date, invoice_amount, version, created_by, updated_by, updated_at FROM grn_bill ORDER BY id`))
}

// g7WantUnchanged asserts nothing was written since before (g7Facts).
func g7WantUnchanged(t *testing.T, what string, env *grntest.Env, before string) {
	t.Helper()
	if after := g7Facts(t, env); after != before {
		t.Errorf("%s wrote:\nbefore %s\nafter  %s", what, before, after)
	}
}

// g7WantWarnings asserts the warnings are exactly want, in any order.
func g7WantWarnings(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	g, w := slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(want))
	if !slices.Equal(g, w) {
		t.Errorf("%s: warnings\n got %q\nwant %q", what, g, w)
	}
}

// g7WantStatus asserts err is an HTTP error with status and message and no
// field errors.
func g7WantStatus(t *testing.T, what string, err error, status int, msg string) {
	t.Helper()
	var he doctype.HTTPError
	if err == nil || !errors.As(err, &he) {
		t.Errorf("%s: err = %v (%T), want %d %q", what, err, err, status, msg)
		return
	}
	if he.Status() != status || err.Error() != msg || len(he.FieldErrors()) != 0 {
		t.Errorf("%s: %d %q field errors %+v, want %d %q with no field errors",
			what, he.Status(), err.Error(), he.FieldErrors(), status, msg)
	}
}

// g7WantNotFound asserts err is AUTH's 404 (CD-245: hidden and missing
// alike).
func g7WantNotFound(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("%s: err = %v, want AUTH's 404 (auth.ErrNotFound)", what, err)
	}
}

// g7WantDenied asserts err is AUTH's write refusal on the Goods Receipt.
func g7WantDenied(t *testing.T, what string, err error) {
	t.Helper()
	var de *auth.DeniedError
	if !errors.As(err, &de) || err.Error() != g7MsgNoWrite {
		t.Errorf("%s: err = %v (%T), want AUTH's 403 %q", what, err, err, g7MsgNoWrite)
	}
}

// g7BillFields are the bill columns an audit change may name.
var g7BillFields = []string{"invoice_amount", "invoice_date", "invoice_no"}

// g7BillEntries are GRN id's audit entries that change a bill column,
// oldest first.
func g7BillEntries(t *testing.T, env *grntest.Env, id int64) []audit.Entry {
	t.Helper()
	var out []audit.Entry
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		all, err := audit.ForDoc(t.Context(), tx, goodsreceipt.DocType, id)
		for _, e := range all {
			if slices.ContainsFunc(e.Changes, func(c audit.Change) bool { return slices.Contains(g7BillFields, c.Field) }) {
				out = append(out, e)
			}
		}
		return err
	}); err != nil {
		t.Fatalf("audit of GRN %d: %v", id, err)
	}
	return out
}

// g7WantEntry asserts e is a bill save by by whose changes are exactly the
// columns fields (sorted), each an OpSet.
func g7WantEntry(t *testing.T, what string, e audit.Entry, by auth.UserID, fields ...string) {
	t.Helper()
	var got []string
	for _, c := range e.Changes {
		got = append(got, c.Field)
		if c.Op != audit.OpSet {
			t.Errorf("%s: change %s Op = %q, want %q", what, c.Field, c.Op, audit.OpSet)
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, fields) {
		t.Errorf("%s: audit changes on %v, want exactly %v (changed columns only)", what, got, fields)
	}
	if e.By != by {
		t.Errorf("%s: audit By = %d, want %d", what, e.By, by)
	}
}

// g7Change is e's change of field.
func g7Change(t *testing.T, e audit.Entry, field string) audit.Change {
	t.Helper()
	for _, c := range e.Changes {
		if c.Field == field {
			return c
		}
	}
	t.Fatalf("audit entry has no change of %s: %+v", field, e.Changes)
	return audit.Change{}
}

// g7Copied are GRN id's audit entries whose Reason starts "Copied from "
// (CD-279 (11)).
func g7Copied(t *testing.T, env *grntest.Env, id int64) []audit.Entry {
	t.Helper()
	var out []audit.Entry
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		all, err := audit.ForDoc(t.Context(), tx, goodsreceipt.DocType, id)
		for _, e := range all {
			if strings.HasPrefix(e.Reason, "Copied from ") {
				out = append(out, e)
			}
		}
		return err
	}); err != nil {
		t.Fatalf("audit of GRN %d: %v", id, err)
	}
	return out
}
