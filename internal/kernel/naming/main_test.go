package naming

import (
	"fmt"
	"os"
	"testing"

	"github.com/vardhan/internaltools/internal/types"
)

func TestMain(m *testing.M) {
	if !numFYReady() {
		fiscalYear = refFiscalYear
		fmt.Fprintln(os.Stderr, "naming: WP1-NUM FiscalYear not merged; using test reference")
	}
	os.Exit(m.Run())
}

// numFYReady reports whether types.FiscalYear is WP1-NUM's real one. A stub
// that panics or returns the wrong value counts as not ready.
func numFYReady() (ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return types.FiscalYear(types.Date{Year: 2026, Month: 4, Day: 1}).Short == "26-27"
}
