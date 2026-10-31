package print

import (
	"time"

	"github.com/vardhan/internaltools/internal/types"
)

// AdvanceDoc is the model an employee advance print view fills for
// AdvanceDocument (Expense spec §10).
type AdvanceDoc struct {
	Letterhead LetterheadInfo
	Employee   string
	Date       types.Date
	Purpose    string
	Lines      []AdvanceLine
	Total      types.Money
	ApprovedBy string
	ApprovedAt time.Time
}

// AdvanceLine is one purpose the advance is asked for.
type AdvanceLine struct {
	Sr                         int
	Type, Description, Project string
	Amount                     types.Money
}

// words is the total in words, or "" when it can't be computed.
func (d AdvanceDoc) words() string {
	w, err := InWordsINR(d.Total)
	if err != nil {
		return ""
	}
	return w
}
