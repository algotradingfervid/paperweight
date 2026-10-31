package expenseweb

import (
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// claimLine is the expense line page (UX §5.5): the receipt first, outside
// the line form (Top, UX-E1), then the line form (Body), whose Save posts
// this one line.
func claimLine(d Deps) *docscreen.Line {
	return &docscreen.Line{
		Top: func(c docscreen.Ctx, no int, s docscreen.StepState) (templ.Component, error) {
			return lineTop(d, c, no, s)
		},
		Body: func(c docscreen.Ctx, no int, s docscreen.StepState) (templ.Component, error) {
			return lineBody(d, c, no, s)
		},
		Save: lineSave,
	}
}
