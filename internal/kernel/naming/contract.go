// Package naming assigns document numbers from series templates (spec §5,
// CD-28). NAM owns it.
package naming

import (
	"context"
	"strconv"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

type Service interface {
	Assign(ctx context.Context, tx *db.Tx, doctype string, docID int64, d types.Date) (string, error)
	AssignAmendment(ctx context.Context, tx *db.Tx, doctype string, docID int64, root string, n int) (string, error)
	Preview(ctx context.Context, doctype string, d types.Date) (string, error)
	ValidateTemplate(tpl string) error
}

type SeriesInfo struct {
	DocType, Label, Template string
	Counters                 map[string]int64
} // rendered prefix → last number

type Admin interface {
	List(ctx context.Context) ([]SeriesInfo, error)
	SetTemplate(ctx context.Context, p auth.Principal, doctype, tpl string) error
	SetCounter(ctx context.Context, p auth.Principal, doctype, prefix string, last int64) error
	EnsureSeries(ctx context.Context, doctype, label, defaultTpl string) error
	PreviewTemplate(ctx context.Context, tpl string, d types.Date) (string, error) // CD-57
}

// Label returns number, or "Draft 42" when number == "" (CD-53).
//
// Working WP0 body; the owning WP may move it out of contract.go (CD-84).
func Label(number string, id int64) string {
	if number == "" {
		return "Draft " + strconv.FormatInt(id, 10)
	}
	return number
}
