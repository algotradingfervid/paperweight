package doc_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
)

// CD-272 Gap 2: a failed read of the amended-from document's links is an
// error, never "not held". The receipt lines become a view whose
// order_line_id overflows on the original receipt's rows only, so loading
// the amendment works and the held-link read fails.
func TestDoc_AmendClosedLink_HeldReadFailureIsAnError(t *testing.T) {
	h := fixturetest.New(t)
	_, r := amcReceiptOnClosedOrder(t, h)
	n := amcAmend(t, h, fixture.Receipt, r.ID)
	h.Exec(t, `ALTER TABLE fixture_receipt_line RENAME TO fixture_receipt_line_base`)
	h.Exec(t, `CREATE VIEW fixture_receipt_line AS SELECT id, parent_id, line_no,
		CASE WHEN parent_id = `+strconv.FormatInt(r.ID, 10)+` THEN abs(-9223372036854775807 - 1) ELSE order_line_id END AS order_line_id, qty
		FROM fixture_receipt_line_base`) // a view takes no parameters

	got, err := osProblems(t, h, fixture.Receipt, n.ID)
	if err == nil || !strings.Contains(err.Error(), "read links of") {
		t.Errorf("Problems error = %v, want the failed held-link read", err)
	}
	if got != nil {
		t.Errorf("Problems = %#v, want no list", got)
	}
}
