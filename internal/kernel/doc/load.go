package doc

import (
	"time"

	"github.com/vardhan/internaltools/internal/types"
)

// Doc is the typed internal view of a stored document.
type Doc struct {
	DocType                        string
	ID                             int64
	Number                         string
	State                          State
	StateReason                    string
	Version                        int
	AmendedFromID, AmendmentRootID int64
	AmendmentNo                    int
	CreatedBy, UpdatedBy           types.UserID
	CreatedAt, UpdatedAt           time.Time
	Header                         Values
	Lines                          []Line
}

// Line is one stored document line.
type Line struct {
	ID     int64
	LineNo int
	Values Values
}
