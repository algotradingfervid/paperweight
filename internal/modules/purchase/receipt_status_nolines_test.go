package purchase

import "testing"

// MOD-PO P4 (PO spec §6.3, §3.2 R8; AD-13 review ruling): a PO with no lines
// (a draft may have none) has nothing received, so receiptStatusOf is
// not_received, for a nil and for an empty slice.
func TestReceiptStatusOf_NoLines(t *testing.T) {
	for _, c := range []struct {
		what  string
		lines []ReceiptLine
	}{
		{"nil", nil},
		{"empty", []ReceiptLine{}},
	} {
		if got := receiptStatusOf(c.lines); got != "not_received" {
			t.Errorf("%s: receiptStatusOf(%v) = %q, want %q", c.what, c.lines, got, "not_received")
		}
	}
}
