package naming

import (
	"fmt"
	"slices"
	"sync"
	"testing"
)

// TestAssign_ConcurrentApprovals covers NAM-033: POs approved at once by
// many goroutines get unique, consecutive numbers with no BUSY errors
// (db.Write is BEGIN IMMEDIATE with busy_timeout, so writers wait).
func TestAssign_ConcurrentApprovals(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    int
	}{{"two_goroutines", 2}, {"fifty_goroutines", 50}} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			ids := make([]int64, tc.n)
			for i := range ids {
				ids[i] = h.create(dtPO, day(2026, 10, 24))
				must(t, h.submit(ids[i], true))
			}

			start := make(chan struct{})
			errs := make(chan error, tc.n)
			var wg sync.WaitGroup
			for _, id := range ids {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					errs <- h.approve(id)
				}()
			}
			close(start)
			wg.Wait()
			close(errs)

			for err := range errs {
				if err != nil {
					t.Errorf("approve: %v, want nil (no SQLITE_BUSY)", err)
				}
			}

			got := make([]string, 0, tc.n)
			for _, id := range ids {
				d := h.doc(id)
				if !d.Number.Valid {
					t.Errorf("doc %d (state %s) has no number", id, d.State)
					continue
				}
				got = append(got, d.Number.String)
			}
			slices.Sort(got)
			want := make([]string, tc.n)
			for i := range want {
				want[i] = fmt.Sprintf("PO/26-27/%05d", i+1)
			}
			if !slices.Equal(got, want) {
				t.Errorf("sorted numbers = %v, want %v", got, want)
			}

			wantCounter(t, h, dtPO, "PO/26-27/", int64(tc.n))
			if n := h.count(`SELECT COUNT(DISTINCT number) FROM naming_issued`); n != tc.n {
				t.Errorf("distinct numbers in naming_issued = %d, want %d", n, tc.n)
			}
		})
	}
}
