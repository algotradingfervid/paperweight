package purchase_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-PO P20 (CD-308, CD-302): the Order's Validate checks each computed
// header total (net, grand, rounded) against the money maximum,
// ₹10,00,00,00,000.00. Over it, Validate returns one field error on the
// lines (Field "lines", Line 0, as DOC's line-count errors and MOD-GRN's
// plan use) and writes the header totals cleared (0: the columns are NOT
// NULL), never the overflowed sum and never the stored value from before.
// Each line stays under the line limit, so only the total check can refuse.
//
// The Request has no money field (PO spec §20.2: no price, amount or total;
// validateRequest computes none), so CD-308 has nothing to check there.

// p20TotalTooLarge is CD-308's text with the Order's DocType label.
const p20TotalTooLarge = "The total is too large. The most a Purchase Order can be is ₹10,00,00,00,000.00. Split it into more than one Purchase Order."

// p20Err is the CD-308 refusal on the lines.
var p20Err = doctype.FieldError{Field: "lines", Line: 0, Message: p20TotalTooLarge}

// p20TotalCols are the header totals Validate writes.
var p20TotalCols = []string{"net_total", "cgst_total", "sgst_total", "igst_total", "grand_total", "rounded_total"}

// p20Over is two MS-ANGLE-50 lines of 100 kg at ₹6,00,00,000.0000 (18%):
// each ₹6,00,00,00,000.00, under the line limit; the net is
// ₹12,00,00,00,000.00.
func p20Over(t *testing.T, env *purchasetest.Env) []map[string]string {
	t.Helper()
	return []map[string]string{p2Line(t, env, "MS-ANGLE-50", "KGS", "100", "60000000"), p2Line(t, env, "MS-ANGLE-50", "KGS", "100", "60000000")}
}

// p20GrandOver is one line of 100 kg at ₹9,00,00,000.0000 (18%): the net,
// ₹9,00,00,00,000.00, is under the maximum, but with GST the grand total is
// ₹10,62,00,00,000.00.
func p20GrandOver(t *testing.T, env *purchasetest.Env) []map[string]string {
	t.Helper()
	return []map[string]string{p2Line(t, env, "MS-ANGLE-50", "KGS", "100", "90000000")}
}

// p20WantCleared asserts every header total of order id is 0, and that
// before (the stored totals ahead of the write) held a non-zero total, so a
// kept old value can't pass as cleared.
func p20WantCleared(t *testing.T, what string, env *purchasetest.Env, id int64, before map[string]string) {
	t.Helper()
	if before != nil && p2Cleared(before["grand_total"]) {
		t.Fatalf("%s: grand_total before is %q, want a non-zero stored total to start from", what, before["grand_total"])
	}
	got := p2Snap(t, env, id, p20TotalCols...)
	for _, c := range p20TotalCols {
		if got[c] != "0" {
			t.Errorf("%s: %s = %q, want 0 (cleared, CD-308)", what, c, got[c])
		}
	}
}

// p20WantAmounts asserts the stored line amounts of order id, in paise: each
// line is valid, so CD-302 writes its amount as computed.
func p20WantAmounts(t *testing.T, what string, env *purchasetest.Env, id int64, want ...string) {
	t.Helper()
	got := p2LineSnap(t, env, id, "amount")
	if len(got) != len(want) {
		t.Fatalf("%s: %d lines, want %d", what, len(got), len(want))
	}
	for i, w := range want {
		if got[i]["amount"] != w {
			t.Errorf("%s: line %d amount = %q, want %s", what, i+1, got[i]["amount"], w)
		}
	}
}

// Test 1, create and full save (CD-308): a draft whose lines, each valid,
// sum over the maximum is refused with exactly the CD-308 error, and nothing
// is written. Both the net (p20Over) and the grand total alone (p20GrandOver)
// are checked.
func TestPO_TotalLimitSaveRefused(t *testing.T) {
	cases := []struct {
		name  string
		lines func(*testing.T, *purchasetest.Env) []map[string]string
	}{
		{"net over", p20Over},
		{"grand over, net under", p20GrandOver},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := purchasetest.New(t)

			before := p2CountAll(t, env)
			_, err := p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"), c.lines(t, env))
			p2WantInvalid(t, "create", err, p20Err)
			p2SameCounts(t, "create", before, env)

			po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p2RefLines(t, env)...))
			version := p2Version(t, env, po.ID)
			totals := p2Snap(t, env, po.ID, p20TotalCols...)
			lines := p2LineSnap(t, env, po.ID, "qty", "price", "amount", "cgst", "sgst", "igst")
			before = p2CountAll(t, env)
			_, err = p2Save(t, env, env.Priya, po.ID, map[string]string{"remarks": "Big order"}, c.lines(t, env))
			p2WantInvalid(t, "save", err, p20Err)
			p2SameCounts(t, "save", before, env)
			if v := p2Version(t, env, po.ID); v != version {
				t.Errorf("save: version %d, want %d unchanged", v, version)
			}
			if got := p2Snap(t, env, po.ID, p20TotalCols...); !maps.Equal(got, totals) {
				t.Errorf("save: totals %v, want %v unchanged (rolled back)", got, totals)
			}
			if got := p2LineSnap(t, env, po.ID, "qty", "price", "amount", "cgst", "sgst", "igst"); !slices.EqualFunc(got, lines, maps.Equal) {
				t.Errorf("save: lines %v, want %v unchanged (rolled back)", got, lines)
			}
		})
	}
}

// Test 5, the boundary (CD-308): lines summing to exactly
// ₹10,00,00,00,000.00 (two NIL lines of 100 kg at ₹5,00,00,000.0000) save,
// with the totals written as computed; one paisa more is refused.
func TestPO_TotalLimitExactlyMax(t *testing.T) {
	env := purchasetest.New(t)
	half := p2Line(t, env, "MS-ANGLE-50", "KGS", "100", "50000000", "NIL")
	res, err := p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"),
		[]map[string]string{half, p2Line(t, env, "MS-ANGLE-50", "KGS", "100", "50000000", "NIL")})
	if err != nil {
		t.Fatalf("create at exactly the maximum: %v, want saved", err)
	}
	if len(res.Problems) > 0 {
		t.Errorf("create at exactly the maximum: problems %+v, want none", res.Problems)
	}
	limit := types.FormatMoney(types.MaxMoney)
	got := p2Snap(t, env, res.ID, p20TotalCols...)
	for _, c := range []string{"net_total", "grand_total", "rounded_total"} {
		if got[c] != "1000000000000" {
			t.Errorf("at the maximum: %s = %q, want 1000000000000 (%s)", c, got[c], limit)
		}
	}

	// ₹5,00,00,000.0001 × 100 kg is ₹5,00,00,00,000.01: the total is one
	// paisa over.
	_, err = p2Save(t, env, env.Priya, res.ID, nil,
		[]map[string]string{half, p2Line(t, env, "MS-ANGLE-50", "KGS", "100", "50000000.0001", "NIL")})
	p2WantInvalid(t, "one paisa over the maximum", err, p20Err)
}

// Test 2, step save from a header input (CD-308, CD-302, CD-205): a draft
// under reverse charge (grand = net = ₹9,00,00,00,000.00) is saved on the
// Terms step with reverse charge turned off. The grand total becomes
// ₹10,62,00,00,000.00; the error is on "lines", outside Only, so the save
// commits with it as a problem, and the totals are cleared, not the
// overflowed sum and not the ₹9,00,00,00,000.00 stored before. Submit then
// runs Validate again and refuses with the same error.
func TestPO_TotalLimitStepSaveReverseCharge(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(),
		purchasetest.OrderHeader(map[string]string{"reverse_charge": "true"}), purchasetest.OrderLines(p20GrandOver(t, env)...))
	before := p2Snap(t, env, po.ID, p20TotalCols...)
	if before["grand_total"] != "900000000000" {
		t.Fatalf("reverse-charge draft: grand_total %q, want 900000000000", before["grand_total"])
	}

	res, err := p2Step(t, env, env.Priya, po.ID, []string{"reverse_charge"}, map[string]string{"reverse_charge": "false"}, nil)
	p2WantProblems(t, "reverse charge off", res, err, p20Err)
	p20WantCleared(t, "reverse charge off", env, po.ID, before)
	p20WantAmounts(t, "reverse charge off", env, po.ID, "900000000000")
	if got := p2Snap(t, env, po.ID, "reverse_charge")["reverse_charge"]; got != "0" {
		t.Errorf("reverse charge off: stored reverse_charge %q, want 0 (the step save committed)", got)
	}

	p20WantSubmitRefused(t, env, po.ID)
}

// Test 2, step save from posted lines (CD-308; Only scopes errors, not
// values, as in TestPO_StepSaveLineAmountTooLarge): a save naming only
// remarks that posts lines summing over the maximum commits with the CD-308
// error as a problem, and the totals are cleared.
func TestPO_TotalLimitStepSaveLines(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p2RefLines(t, env)...))
	before := p2Snap(t, env, po.ID, p20TotalCols...)

	res, err := p2Step(t, env, env.Priya, po.ID, []string{"remarks"}, map[string]string{"remarks": "Big order"}, p20Over(t, env))
	p2WantProblems(t, "lines over the maximum", res, err, p20Err)
	p20WantCleared(t, "lines over the maximum", env, po.ID, before)
	p20WantAmounts(t, "lines over the maximum", env, po.ID, "600000000000", "600000000000")

	p20WantSubmitRefused(t, env, po.ID)
}

// p20WantSubmitRefused: submit runs Validate (D-APR-15), so the stored
// over-limit draft is refused with exactly the CD-308 error and stays a
// draft.
func p20WantSubmitRefused(t *testing.T, env *purchasetest.Env, id int64) {
	t.Helper()
	_, err := p2Do(t, env, env.Priya, id, doc.ActionSubmit, "")
	p2WantInvalid(t, "submit", err, p20Err)
	if d := p2Load(t, env, id); d.State != doctype.StateDraft {
		t.Errorf("submit: state %q, want draft", d.State)
	}
}

// p20SetPrice writes one stored line's price with one UPDATE, leaving the
// version alone. Like purchasetest.SetReceived it stands in for a write no
// engine path can make once CD-308 is built: a PO stored before CD-308 whose
// lines sum over the maximum (an approved or cancelled PO never runs
// Validate again, and a tax code's rate is locked once a non-draft uses it,
// so no master edit can raise an unchanged PO's total).
func p20SetPrice(t *testing.T, env *purchasetest.Env, id int64, lineNo int, price string) {
	t.Helper()
	r, err := types.ParseRate(price)
	if err != nil {
		t.Fatalf("price %q: %v", price, err)
	}
	if err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(t.Context(), `UPDATE purchase_order_line SET price = ? WHERE parent_id = ? AND line_no = ?`, int64(r), id, lineNo)
		return err
	}); err != nil {
		t.Fatalf("set price of order %d line %d: %v", id, lineNo, err)
	}
}

// Test 2, amend (CD-308, CD-302; PO spec §15 item 8): a cancelled PO stored
// before CD-308 with lines of ₹6,00,00,00,000.00 (1,200 kg at
// ₹50,00,000.0000) and ₹5,00,00,00,000.00 (50 kg at ₹10,00,00,000.0000) is
// amended. Amend keeps Validate's writes and ignores its errors, so the new
// draft exists with its totals cleared, never the ₹12,98,00,00,000.00 grand
// total; a save of it is refused with the CD-308 error.
func TestPO_TotalLimitAmend(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved(), purchasetest.OrderLines(p2RefLines(t, env)...))
	p2Must(t, "Ravi cancels")(p2Do(t, env, env.Ravi, po.ID, doc.ActionCancel, "Wrong vendor"))
	p20SetPrice(t, env, po.ID, 1, "5000000")
	p20SetPrice(t, env, po.ID, 2, "100000000")

	res := p2Must(t, "Priya amends")(p2Do(t, env, env.Priya, po.ID, doc.ActionAmend, ""))
	if res.ID == po.ID || res.State != doctype.StateDraft {
		t.Fatalf("amend = %+v, want a new draft", res)
	}
	p20WantCleared(t, "amended draft", env, res.ID, nil)
	p20WantAmounts(t, "amended draft", env, res.ID, "600000000000", "500000000000")

	_, err := p2Save(t, env, env.Priya, res.ID, map[string]string{"remarks": "Check"}, nil)
	p2WantInvalid(t, "save the amended draft", err, p20Err)
}
