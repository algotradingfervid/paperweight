package printtest

import (
	"context"
	"sync"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// FixtureDocTypes are the DocTypes DOC's internal/fixture registers (CD-58).
// They are constants because FILE may not import that package.
var FixtureDocTypes = []string{"fixture.Order", "fixture.Receipt", "fixture.Claim"}

var defaultOnce sync.Once

// RegisterFixtureViews registers a print view for each FixtureDocTypes name
// in r. The views read no tables; each prints its reference document on
// PUNE's letterhead from LetterheadFor. Repeat calls on print.Default are
// no-ops.
func RegisterFixtureViews(r *print.Registry) {
	if r == print.Default {
		defaultOnce.Do(func() { registerFixtureViews(r) })
		return
	}
	registerFixtureViews(r)
}

func registerFixtureViews(r *print.Registry) {
	for name, ref := range map[string]func() print.TradeDoc{
		"fixture.Order":   ReferencePO,
		"fixture.Receipt": ReferenceReceipt,
		"fixture.Claim":   ReferenceClaim,
	} {
		r.Register(name, func(ctx context.Context, tx *db.Tx, _ int64) (templ.Component, error) {
			lh, err := print.LetterheadFor(ctx, tx, PuneID)
			if err != nil {
				return nil, err
			}
			d := ref()
			d.Letterhead = lh
			return print.TradeDocument(d), nil
		})
	}
}
