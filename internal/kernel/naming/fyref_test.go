package naming

import (
	"fmt"
	"time"

	"github.com/vardhan/internaltools/internal/types"
)

// refFiscalYear is the test reference for the Indian fiscal year
// (1 April – 31 March). TestMain uses it only while WP1-NUM's
// types.FiscalYear is not merged; Task 17 removes it.
func refFiscalYear(d types.Date) types.FY {
	start := d.Year
	if d.Month < time.April {
		start--
	}
	return types.FY{
		Start: types.Date{Year: start, Month: time.April, Day: 1},
		End:   types.Date{Year: start + 1, Month: time.March, Day: 31},
		Label: fmt.Sprintf("%d-%02d", start, (start+1)%100),
		Short: fmt.Sprintf("%02d-%02d", start%100, (start+1)%100),
	}
}
