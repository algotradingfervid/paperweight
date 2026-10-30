package paymentterms

import (
	"testing"

	"github.com/vardhan/internaltools/internal/types"
)

// TestPaymentTerms_ScheduleSumsExactly: the MST-129 values, then a total whose
// rounded portions leave a paisa over for the last row to absorb.
func TestPaymentTerms_ScheduleSumsExactly(t *testing.T) { // MST-129
	rows := []Row{{Portion: pct(t, "33.33")}, {Portion: pct(t, "33.33")}, {Portion: pct(t, "33.34")}}
	for _, c := range []struct {
		total string
		want  []string
	}{
		{"1000.00", []string{"333.30", "333.30", "333.40"}},
		{"100.01", []string{"33.33", "33.33", "33.35"}},
	} {
		total := money(t, c.total)
		due := Schedule(rows, types.Date{Year: 2026, Month: 10, Day: 24}, total)
		if len(due) != len(c.want) {
			t.Fatalf("Schedule(%s) has %d rows; want %d", c.total, len(due), len(c.want))
		}
		var sum types.Money
		for i, d := range due {
			if want := money(t, c.want[i]); d.Amount != want {
				t.Errorf("Schedule(%s)[%d] = %d; want %d", c.total, i, d.Amount, want)
			}
			sum += d.Amount
		}
		if sum != total {
			t.Errorf("Schedule(%s) sums to %d; want %d", c.total, sum, total)
		}
	}
}
