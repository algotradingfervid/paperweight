package docscreen

import (
	"net/http"
	"slices"
	"strconv"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// Module is what one DocType adds to the generic screens (hooks spec §3).
// Every field is optional; nil keeps today's behaviour.
//
// LineCard's l.Card is the default card with the fields Hide hides already
// left out (CD-319 (9)): start from it. l.Display and l.Inputs still hold
// hidden fields; read them only behind a check with Hide, or not at all.
type Module struct {
	StepLabels  map[string]string                           // step slug → label
	StepIntros  map[string]string                           // step slug → the line under the stepper (UI v2 §5.6)
	StepIcons   map[string]string                           // step slug → its check card's tile, from the icon set (UI v2 §5.6)
	Steps       map[string]Step                             // step slug → custom step
	Line        *Line                                       // custom line page
	LineCard    func(c Ctx, l LineView) components.LineCard // a line's summary when its first Link isn't the item
	CardsOnly   bool                                        // Items step without the desktop columns
	NoLinePages bool                                        // line URLs answer 404; lines come from a custom step
	Hide        func(c Ctx, f doctype.Field) bool           // field not rendered, posted or summarised
	Check       func(c Ctx) (CheckExtra, error)             // check page additions
	Page        func(c Ctx) (PageExtra, error)              // document page additions
	Reason      func(c Ctx, a doctype.Action) (ReasonExtra, error)
	LineNoun    LineNoun                                      // the words for a line; zero keeps docscreen's (CD-275)
	AddTop      bool                                          // the add button above the cards, again below from 4 lines (CD-275)
	LineGrid    func(c Ctx, l LineView) []components.GridCell // one line's read-only cells from 64rem (CD-313)
	KeyFacts    []string                                      // header fields the document header shows, in order (UI v2 §5.13 item 3)
	LifeSteps   []string                                      // extra lifecycle steps after the approved one, by word; PageExtra.Life marks them (UI v2 §5.13 item 4)
	Home        components.Link                               // the module's home, "Go to {Label}" after a Submit; zero for none (UI v2 §5.13 item 2)
	FooterTotal string                                        // the header Money field the Items footer shows (UI v2 A-22, CD-376)
	TodayDates  []string                                      // header Date fields a new document opens on today (PO spec §3.2, expense UX §5.10)
}

// LineNoun names a module's lines (UX-E2, CD-270 Q1): Add labels the add
// button and the new-line page, Title prefixes a line's number on its page,
// Empty is the Items step's text with no lines. An empty field keeps
// docscreen's words.
type LineNoun struct {
	Add, Title, Empty string
}

// Ctx is what a hook reads. View is nil on a new document's first step, and
// wherever Listing is true: the list and search pages, the CSV export and
// the approval readers' party, where Hide is asked per listed field and per
// party in each request (CD-319 (7), CD-327 (1), (3), CD-334). Listing is
// false everywhere else. A Hide treats a nil View as "no record" and never
// dereferences it.
type Ctx struct {
	Req       *http.Request // with Listing, it may be the inbox path (PartyListed)
	Principal auth.Principal
	DT        *doctype.DocType
	View      *DocView
	Deps      Deps // for FieldInput and the rest
	Listing   bool // the list or search page: no View
}

// Step replaces a wizard step's body, its save, or both.
type Step struct {
	Body func(c Ctx, s StepState) (templ.Component, error) // nil, or no component → the generic form
	Save func(c Ctx) (Save, error)                         // nil → the generic post
	Bar  func(c Ctx, s StepState) (StepBar, error)         // nil → the generic bar
	Top  func(c Ctx) (templ.Component, error)              // above the generic body, when Body gives none (CD-280 D5)
}

// StepBar is what Step.Bar changes in the step's action bar (CD-263 G6).
// Each zero field keeps docscreen's: Primary "" keeps the label, Stay false
// keeps the default nav, Back nil keeps the Back button.
type StepBar struct {
	Primary string           // the primary button's label
	Stay    bool             // the primary posts nav=stay and redraws the step
	Back    *components.Link // a Back link in place of the Back button
	Summary templ.Component  // the footer's row above the buttons (CD-387 (7))
}

// StepState is the redraw input: Posted is true on a 422 or 409, when the
// body reads typed values from c.Req; Errors holds the step's field errors.
// Defaults, on a new document's first GET, holds the values the user may only
// fill one way (CD-256 (4), CD-245).
type StepState struct {
	Posted   bool
	Errors   []doctype.FieldError
	Defaults map[string]string
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
	Top  func(c Ctx, no int, s StepState) (templ.Component, error) // after the h1, outside the line form (UX-E1)
}

// CheckExtra is what Module.Check adds to the check page.
type CheckExtra struct {
	Notes    []string          // info banners under the approval preview
	Warnings []string          // components.Warnings
	Totals   []components.Pair // after the grand total and its words (UX-E6)
	Cards    []templ.Component // after Attachments, before the grand total
	Covers   []string          // fields Totals show, left out of the steps' details (CD-326 (4))
}

// PageExtra is what Module.Page adds to the document page. A LineNotes
// entry replaces the Note a Module.LineCard put on the same line; other
// lines keep theirs.
type PageExtra struct {
	Warnings  []string
	LineNotes map[int]string                 // line no → a note on that line
	Pairs     []components.Pair              // added to Details, for a hidden field's display value
	Covers    []string                       // header fields left out of Details: the page shows them elsewhere (R13)
	Cards     []templ.Component              // after Lines, before History
	Notice    *components.BannerProps        // last among the document banners
	More      []components.MoreAction        // before docscreen's own More actions
	Primary   *components.ButtonProps        // the main button when the bar has none
	Files     *FilesCard                     // replaces the read-only Attachments (UX-E7)
	Life      map[string]components.LifeStep // a Module.LifeSteps word → its mark, note and date (Label unused); a word with none is a later step
	Success   *components.SuccessProps       // set on the page the module's own action lands on: the flash shows as a success block with this Title, and Message after the flash's text (UI v2 §5.13 item 2); Links are ignored
}

// FilesCard is the read-only files a module shows in place of Attachments (UX-E7, CD-275).
type FilesCard struct {
	Title string
	Items []components.Attachment
}

// ReasonExtra is what Module.Reason adds to a reason page.
type ReasonExtra struct {
	Note, Hint string // shown above the field; the field's hint
	NoteWarn   bool   // Note is a warning, not information (CD-326 (7))
}

// LineFieldID is the id of line no's field input, f-lines-{no}-{field}: the
// id a line error with that Line and Field links to. no is the error's
// 1-based Line among the non-blank lines docscreen sent. A Body gives its
// line inputs these ids; an error links there only when the Body rendered
// the id, and to the step heading otherwise (CD-286 R1).
func LineFieldID(no int, field string) string {
	return components.FieldID("lines[" + strconv.Itoa(no) + "]." + field)
}

// modOf is dt's Module from Deps.Modules; the zero Module when it has none.
func (d Deps) modOf(dt *doctype.DocType) Module { return d.Modules[dt.Name] }

// ctxOf is the hooks' Ctx for a request on dt. v is nil on a new document;
// otherwise Deps carries v's stored inputs, so a stored link keeps its label
// (CD-256 (10), CD-217, CD-228). Deps also offers header Links recent picks
// (UI v2 §5.8).
func (d Deps) ctxOf(r *http.Request, dt *doctype.DocType, v *DocView) Ctx {
	p := d.principal(r)
	c := Ctx{Req: r, Principal: p, DT: dt, View: v, Deps: d.withRecent(dt, p)}
	if v != nil {
		c.Deps = c.Deps.WithStored(v.Inputs)
	}
	return c
}

// lineCtx is ctxOf for line no's page (0 = a new line): Deps carries that
// line's stored inputs instead of the header's.
func (d Deps) lineCtx(r *http.Request, dt *doctype.DocType, v *DocView, no int) Ctx {
	c := d.ctxOf(r, dt, v)
	c.Deps = d
	if no > 0 {
		c.Deps = d.WithStored(v.Lines[no-1].Inputs)
	}
	return c
}

// hidden reports whether the module hides f: not rendered, posted or
// summarised (hooks spec §4.3). It is false when Hide is nil.
func hidden(c Ctx, f doctype.Field) bool {
	h := c.Deps.modOf(c.DT).Hide
	return h != nil && h(c, f)
}

// unhidden is e, or only its message when e is on a field the module
// hides, so a summary never links to an input that isn't there (CD-256 (1),
// CD-319).
func unhidden(c Ctx, e doctype.FieldError) doctype.FieldError {
	if f, ok := c.DT.Field(e.Field); ok && hidden(c, f) {
		return doctype.FieldError{Message: e.Message}
	}
	return e
}

// partyHidden reports whether the module hides the header field DOC reads
// Info.Party from (CD-319 (4), (6)): the field decides, never its value.
func partyHidden(c Ctx) bool {
	f, ok := doc.PartyField(c.DT)
	return ok && hidden(c, f)
}

// hideSummary leaves out of v the party when its field is hidden, and the
// grand total and its words when the header Money field they come from
// (figure) is hidden (CD-319 (4), (8)).
func hideSummary(c Ctx, v *DocView) {
	if partyHidden(c) {
		v.Info.Party = ""
	}
	for _, f := range c.DT.HeaderFields() {
		if figure(f) && hidden(c, f) {
			v.GrandTotal, v.InWords = "", ""
		}
	}
}

// unhiddenAll is unhidden on each of errs.
func unhiddenAll(c Ctx, errs []doctype.FieldError) []doctype.FieldError {
	out := make([]doctype.FieldError, len(errs))
	for n, e := range errs {
		out[n] = unhidden(c, e)
	}
	return out
}

// applyLineCards gives each of v's lines the default card without the fields
// the module hides (CD-319; Hide sees the line's stored inputs in Deps), and
// the module's LineCard of that line, so it is handed the filtered card
// (CD-319 (9)) and the line's Ctx (CD-339); then it clears Received on a
// document that is neither approved nor closed (CD-242), after the override
// (CD-256 (11)).
// v.Lines is replaced, never changed in place.
func applyLineCards(c Ctx, v *DocView) {
	card := c.Deps.modOf(c.DT).LineCard
	lines := slices.Clone(v.Lines)
	for i := range lines {
		lc := c
		lc.Deps = c.Deps.WithStored(lines[i].Inputs)
		lines[i].Card = defaultCard(c.DT, lines[i], func(f doctype.Field) bool { return !hidden(lc, f) })
		if card != nil {
			lines[i].Card = card(lc, lines[i])
		}
		if v.Info.State != doctype.StateApproved && v.Info.State != doctype.StateClosed {
			lines[i].Card.Received = "" // only approved and closed documents show receipts (CD-242)
		}
	}
	v.Lines = lines
}
