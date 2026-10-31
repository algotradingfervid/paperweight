package docscreen

import (
	"net/http"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// Module is what one DocType adds to the generic screens (hooks spec §3).
// Every field is optional; nil keeps today's behaviour.
type Module struct {
	StepLabels  map[string]string                           // step slug → label
	Steps       map[string]Step                             // step slug → custom step
	Line        *Line                                       // custom line page
	LineCard    func(c Ctx, l LineView) components.LineCard // a line's summary when its first Link isn't the item
	CardsOnly   bool                                        // Items step without the desktop columns
	NoLinePages bool                                        // line URLs answer 404; lines come from a custom step
	Hide        func(c Ctx, f doctype.Field) bool           // field not rendered, posted or summarised
	Check       func(c Ctx) (CheckExtra, error)             // check page additions
	Page        func(c Ctx) (PageExtra, error)              // document page additions
	Reason      func(c Ctx, a doctype.Action) (ReasonExtra, error)
}

// Ctx is what a hook reads. View is nil only on a new document's first step.
type Ctx struct {
	Req       *http.Request
	Principal auth.Principal
	DT        *doctype.DocType
	View      *DocView
	Deps      Deps // for FieldInput and the rest
}

// Step replaces a wizard step's body, its save, or both.
type Step struct {
	Body func(c Ctx, s StepState) (templ.Component, error) // nil → the generic form
	Save func(c Ctx) (Save, error)                         // nil → the generic post
}

// StepState is the redraw input: Posted is true on a 422 or 409, when the
// body reads typed values from c.Req; Errors holds the step's field errors.
type StepState struct {
	Posted bool
	Errors []doctype.FieldError
}

// Save is what a custom post asks docscreen to do.
type Save struct {
	Values map[string]string
	Lines  []map[string]string  // nil keeps the stored lines; non-nil and empty removes them all
	Errors []doctype.FieldError // parse errors: 422 with nothing written
}

// Line replaces the line page; Save returns this one line's values, and
// docscreen merges them into the loaded lines as linePost does.
type Line struct {
	Body func(c Ctx, no int, s StepState) (templ.Component, error)
	Save func(c Ctx, no int) (Save, error)
}

// CheckExtra is what Module.Check adds to the check page.
type CheckExtra struct {
	Notes    []string          // info banners under the approval preview
	Warnings []string          // components.Warnings
	Totals   []components.Pair // above the grand total
}

// PageExtra is what Module.Page adds to the document page.
type PageExtra struct {
	Warnings  []string
	LineNotes map[int]string    // line no → a note on that line
	Pairs     []components.Pair // added to Details, for a hidden field's display value
	Cards     []templ.Component // after Lines, before History
}

// ReasonExtra is what Module.Reason adds to a reason page.
type ReasonExtra struct {
	Note, Hint string // shown above the field; the field's hint
}
