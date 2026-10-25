package types

import "testing"

func TestRound_HalfUpTies(t *testing.T) { // NUM-002
	m, err := Qty(2500).Times(Rate(333300)) // 2.5 × ₹33.33 = 83.325
	if err != nil || m != 8333 {
		t.Fatalf("got %d, %v; want 8333", m, err)
	}
	for in, want := range map[Money]Money{50: 100, 250: 300, -50: -100} {
		if got := in.RoundRupee(); got != want {
			t.Errorf("RoundRupee(%d) = %d, want %d", in, got, want)
		}
	}
}

func FuzzRound_SymmetricIdempotent(f *testing.F) { // NUM-003
	// The case's own examples at scale 3: 0.005 → 0.01 and −0.005 → −0.01.
	if got := RoundTo(5, 10); got != 10 {
		f.Fatalf("RoundTo(5, 10) = %d, want 10", got)
	}
	if got := RoundTo(-5, 10); got != -10 {
		f.Fatalf("RoundTo(-5, 10) = %d, want -10", got)
	}
	f.Add(int64(5), uint8(1))  // 0.005 → 0.01 at scale 3
	f.Add(int64(-5), uint8(1)) // −0.005 → −0.01
	f.Add(int64(5), uint8(0))  // unit 10: 5 → 10
	f.Add(int64(-5), uint8(0)) // unit 10: −5 → −10
	f.Fuzz(func(t *testing.T, x int64, k uint8) {
		if x > 1e18 || x < -1e18 {
			t.Skip()
		}
		unit := pow10(int(k%17) + 1)
		r := RoundTo(x, unit)
		if RoundTo(-x, unit) != -r {
			t.Fatalf("not symmetric: %d", x)
		}
		if RoundTo(r, unit) != r {
			t.Fatalf("not idempotent: %d", x)
		}
		// r is the nearest multiple of unit.
		if r%unit != 0 {
			t.Fatalf("RoundTo(%d, %d) = %d, not a multiple of the unit", x, unit, r)
		}
		if d := x - r; 2*d > unit || 2*d < -unit {
			t.Fatalf("RoundTo(%d, %d) = %d, not the nearest multiple", x, unit, r)
		}
	})
}

func TestRound_FloatTrapValues(t *testing.T) { // NUM-004
	for in, want := range map[int64]int64{2675: 268, 1005: 101, 1015: 102} {
		if got := Rescale(in, 3, 2); got != want {
			t.Errorf("Rescale(%d) = %d, want %d", in, got, want)
		}
	}
	if Money(10)+Money(20) != Money(30) {
		t.Fatal("0.10 + 0.20 != 0.30")
	}
	var s Money
	for range 10 {
		s += 10
	}
	if s != 100 {
		t.Fatalf("ten × 0.10 = %d", s)
	}
}
