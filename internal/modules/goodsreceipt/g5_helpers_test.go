package goodsreceipt_test

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-GRN G5 test helpers (GRN spec §4, §5; PO spec §14.2; CD-234, CD-238,
// CD-272 Gaps 4 and 13, CD-282 Q9). Warnings, Notes and LineWarnings are
// read through their public functions inside a read transaction on the
// test's database; every GRN and PO is made through the engine.

// G5's texts, copied from GRN spec §4 and §7 (v0.5) and CD-272 Gaps 4 and 13.
const (
	g5NoteAll    = "All lines received as ordered."
	g5NoteUnreg  = "Unregistered vendor: the e-way bill may be ours to generate. Check with your CA."
	g5NoteReject = "Reject the e-way bill on the portal within 72 hours if the goods went back."
)

// g5Over is the over-delivery warning (GRN spec §4): x and y are
// types.FormatQtyUoM texts ("1,250 KGS"), po the PO number.
func g5Over(n int, x, y, po string) string {
	return fmt.Sprintf("Line %d: Accepted %s is more than the %s pending on %s.", n, x, y, po)
}

// g5BeforeOrder is the receipt-before-order warning (GRN spec §4), date as
// dd-mm-yyyy.
func g5BeforeOrder(date string) string {
	return "Receipt date is before the order date " + date + "."
}

// g5LockText is M0-7's period-lock text (modkit, PO spec §14.2) for a Goods
// Receipt shown as display (naming.Label: "Draft 7" or its number), dated
// date, under the lock 30-09-2026 (CD-142: the producing package's text).
func g5LockText(display, date string) string {
	return "Goods Receipt " + display + " is dated " + date + ", which is locked. The period is locked up to 30-09-2026."
}

// g5Warnings is goodsreceipt.Warnings for GRN id.
func g5Warnings(t *testing.T, env *grntest.Env, id int64) []string {
	t.Helper()
	var out []string
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		out, err = goodsreceipt.Warnings(t.Context(), tx, id)
		return err
	}); err != nil {
		t.Fatalf("Warnings(%d): %v", id, err)
	}
	return out
}

// g5Notes is goodsreceipt.Notes for GRN id.
func g5Notes(t *testing.T, env *grntest.Env, id int64) []string {
	t.Helper()
	var out []string
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		out, err = goodsreceipt.Notes(t.Context(), tx, id)
		return err
	}); err != nil {
		t.Fatalf("Notes(%d): %v", id, err)
	}
	return out
}

// g5LineWarnings is goodsreceipt.LineWarnings for typed accepted quantities
// (keyed by PO line id) on PO poID, with GRN grnID (0 for none) not counted.
func g5LineWarnings(t *testing.T, env *grntest.Env, poID, grnID int64, accepted map[int64]types.Qty) map[int64]string {
	t.Helper()
	var out map[int64]string
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		out, err = goodsreceipt.LineWarnings(t.Context(), tx, poID, grnID, accepted)
		return err
	}); err != nil {
		t.Fatalf("LineWarnings(PO %d, GRN %d, %v): %v", poID, grnID, accepted, err)
	}
	return out
}

// g5WantList asserts got is exactly want, in order (nil and empty are the
// same).
func g5WantList(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !slices.Equal(got, want) {
		t.Errorf("%s:\n got %q\nwant %q", what, got, want)
	}
}

// g5WantMap asserts got is exactly want (nil and empty are the same).
func g5WantMap(t *testing.T, what string, got, want map[int64]string) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !maps.Equal(got, want) {
		t.Errorf("%s:\n got %v\nwant %v", what, got, want)
	}
}

// g5Facts is g3Snapshot plus the rows a refused action must not add beyond
// the audit log: approval history, issued numbers and module comments
// (DOC-077).
func g5Facts(t *testing.T, env *grntest.Env) string {
	t.Helper()
	return g3Snapshot(t, env) + fmt.Sprint(g2Query(t, env.DB, `SELECT
		(SELECT count(*) FROM approval_history), (SELECT count(*) FROM naming_issued),
		(SELECT count(*) FROM module_comment)`))
}

// g5WantRefusal asserts err is a whole-document refusal (422, no field
// errors) whose message is text.
func g5WantRefusal(t *testing.T, what string, err error, text string) {
	t.Helper()
	var he doctype.HTTPError
	if err == nil || !errors.As(err, &he) {
		t.Errorf("%s: err = %v, want a 422 refusal %q", what, err, text)
		return
	}
	if he.Status() != 422 || err.Error() != text || len(he.FieldErrors()) != 0 {
		t.Errorf("%s: status %d message %q field errors %+v, want 422 %q with no field errors",
			what, he.Status(), err.Error(), he.FieldErrors(), text)
	}
}
