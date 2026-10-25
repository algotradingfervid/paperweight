package approval

import (
	"math/rand/v2"
	"testing"

	"github.com/vardhan/internaltools/internal/types"
)

// FuzzApprovalMatch_Monotonic (APR-090): for any set of enabled rules of one
// DocType and amounts a ≤ b, the rule matched for b has a from amount ≥ the
// one matched for a, and if a needs approval then b does too.
func FuzzApprovalMatch_Monotonic(f *testing.F) {
	f.Add(int64(1), int64(4999999), int64(5000000))
	f.Add(int64(2), int64(0), int64(0))
	f.Add(int64(3), int64(60000000), int64(6000000))
	f.Fuzz(func(t *testing.T, seed, a, b int64) {
		a, b = max(a, 0), max(b, 0)
		if a > b {
			a, b = b, a
		}
		rules := monotonicRules(seed, a, b)
		ma, _, okA := match(rules, types.Money(a))
		mb, _, okB := match(rules, types.Money(b))
		if okA && !okB {
			t.Fatalf("rules %v: amount %d needs approval but larger amount %d doesn't", rules, a, b)
		}
		if okA && okB && mb.FromAmount < ma.FromAmount {
			t.Fatalf("rules %v: amount %d matched from %d, larger amount %d matched lower from %d", rules, a, ma.FromAmount, b, mb.FromAmount)
		}
	})
}

// monotonicRules builds 0–6 enabled rules with distinct non-negative from
// amounts from seed. Some from amounts sit on or next to a and b so the
// boundaries get exercised.
func monotonicRules(seed, a, b int64) []Rule {
	r := rand.New(rand.NewPCG(uint64(seed), 0))
	n := r.IntN(7)
	seen := map[types.Money]bool{}
	var rules []Rule
	for len(rules) < n {
		var from int64
		switch r.IntN(4) {
		case 0:
			from = a + r.Int64N(3) - 1
		case 1:
			from = b + r.Int64N(3) - 1
		default:
			from = r.Int64N(100_000_000) // up to ₹10,00,000.00
		}
		m := types.Money(max(from, 0))
		if seen[m] {
			continue
		}
		seen[m] = true
		rules = append(rules, po(int64(len(rules)+1), m, "Purchase Manager"))
	}
	return rules
}
