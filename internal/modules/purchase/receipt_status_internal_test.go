package purchase

import (
	"testing"

	"github.com/vardhan/internaltools/internal/types"
)

// mustQty parses a quantity for a table literal; a bad literal is a test bug.
func mustQty(s string) types.Qty {
	q, err := types.ParseQty(s)
	if err != nil {
		panic("mustQty(" + s + "): " + err.Error())
	}
	return q
}

// MOD-PO P4 (PO spec §6.3; PO-030's rules, which P2's Validate reuses on every
// draft save, PO-032): receiptStatusOf is not_received when no line has
// anything received, fully_received when every line's pending is 0, and
// part_received otherwise. already_received counts as received, so a cutover
// line with it is part or fully received with no receipt.
func TestReceiptStatusOf(t *testing.T) {
	line := func(qty, already, received string) ReceiptLine {
		return ReceiptLine{Qty: mustQty(qty), Already: mustQty(already), Received: mustQty(received)}
	}
	cases := []struct {
		what  string
		lines []ReceiptLine
		want  string
	}{
		{"one line, nothing received", []ReceiptLine{line("10", "0", "0")}, "not_received"},
		{"two lines, nothing received", []ReceiptLine{line("10", "0", "0"), line("4", "0", "0")}, "not_received"},
		{"one line, part received", []ReceiptLine{line("10", "0", "3")}, "part_received"},
		{"one line, a fraction received", []ReceiptLine{line("10", "0", "0.001")}, "part_received"},
		{"one line, fully received", []ReceiptLine{line("10", "0", "10")}, "fully_received"},
		{"one line, over-received", []ReceiptLine{line("10", "0", "12")}, "fully_received"},
		{"one line full, one nothing", []ReceiptLine{line("10", "0", "10"), line("4", "0", "0")}, "part_received"},
		{"both lines part", []ReceiptLine{line("10", "0", "2"), line("4", "0", "1")}, "part_received"},
		{"both lines full", []ReceiptLine{line("10", "0", "10"), line("4", "0", "4")}, "fully_received"},
		{"one line over, one short", []ReceiptLine{line("10", "0", "15"), line("4", "0", "3")}, "part_received"},
		// already_received counts (spec §6.3).
		{"already part, nothing received", []ReceiptLine{line("10", "3", "0")}, "part_received"},
		{"already the whole qty", []ReceiptLine{line("10", "10", "0")}, "fully_received"},
		{"already above qty", []ReceiptLine{line("10", "12", "0")}, "fully_received"},
		{"already and received together full", []ReceiptLine{line("10", "6", "4")}, "fully_received"},
		{"already and received together short", []ReceiptLine{line("10", "6", "3")}, "part_received"},
		{"already on one line only", []ReceiptLine{line("10", "2", "0"), line("4", "0", "0")}, "part_received"},
		{"already covers one line, the other received", []ReceiptLine{line("10", "10", "0"), line("4", "0", "4")}, "fully_received"},
	}
	for _, c := range cases {
		if got := receiptStatusOf(c.lines); got != c.want {
			t.Errorf("%s: receiptStatusOf(%+v) = %q, want %q", c.what, c.lines, got, c.want)
		}
	}
}
