package gallery

import (
	"context"
	"io"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// hookedIDs are the seeded docscreentest.Hooked documents. Hooked isn't
// approvable, so it has no pending or rejected one.
var hookedIDs = docscreen.SampleIDs{Draft: 142, Approved: 144, Cancelled: 146, Closed: 147, Lines500: 150}

// hookedReadOnly is a Hooked draft someone else owns: Priya may read it but
// not write it, so it shows as the read-only document page (CD-263 G1).
const hookedReadOnly int64 = 148

// hookedOwner is the other user who owns hookedReadOnly.
const hookedOwner auth.UserID = 9

// What the gallery's Hooked module shows. Each text is worst case, so the
// sweep proves the hook's region holds it.
const (
	hookedDetailsLabel = "Vendor, site"
	hookedItemsLabel   = "Materials"
	hookedBodyTitle    = "Who supplies it, and where it goes"
	hookedLeadField    = "lead_days"
	hookedLeadLabel    = "Delivery in days"
	hookedLeadError    = "Enter the delivery time as a whole number of days."
	hookedBarPrimary   = "Save the vendor and continue"
	hookedTop          = "Terms print on the order exactly as typed here, under the vendor's address."
	hookedLineTitle    = "One material line"
	hookedLineNote     = "Due on site within 7 days of the order date; the vendor has confirmed stock."
	hookedCheckNote    = "This order goes to the vendor as soon as it is submitted."
	hookedCheckWarn    = "The vendor's GST number has not been checked this year."
	hookedCheckTotal   = "Freight (estimated)"
	hookedCheckCard    = "Delivery"
	hookedPageWarn     = "Some lines are due later than the site needs them."
	hookedPageNote     = "Owned by Priya Shah. Only Priya Shah can change this order."
	hookedPageMore     = "Copy to a new order"
	hookedPagePrimary  = "Send to vendor"
	hookedPagePair     = "Vendor contact"
	hookedPageCard     = "Delivery schedule"
	hookedReasonNote   = "The vendor is told by email, with the reason you give here."
	hookedReasonHint   = "Say what the vendor should do with goods already on the way."
)

// hookedModule is a Module that uses every hook the pages show: a custom
// first step (Body, Save and Bar), a Top on the terms step, a custom line
// page, line summaries with a note, and the check, document and reason page
// additions.
func hookedModule() docscreen.Module {
	return docscreen.Module{
		StepLabels: map[string]string{"details": hookedDetailsLabel, "items": hookedItemsLabel},
		Steps: map[string]docscreen.Step{
			"details": {Body: hookedDetailsBody, Save: hookedDetailsSave, Bar: hookedDetailsBar},
			"terms":   {Top: hookedTermsTop},
		},
		Line:     &docscreen.Line{Body: hookedLineBody, Save: hookedLineSave},
		LineCard: hookedLineCard,
		Check:    hookedCheck,
		Page:     hookedPage,
		Reason:   hookedReason,
	}
}

// html is a component writing s, which the caller has escaped.
func html(s string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, s)
		return err
	})
}

// group renders cs one after another.
func group(cs ...templ.Component) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		for _, c := range cs {
			if err := c.Render(ctx, w); err != nil {
				return err
			}
		}
		return nil
	})
}

// titledCard is components.Card holding body.
func titledCard(title string, body templ.Component) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		return components.Card(title, nil).Render(templ.WithChildren(ctx, body), w)
	})
}

func para(text string) templ.Component { return html("<p>" + templ.EscapeString(text) + "</p>") }

func hookedField(c docscreen.Ctx, name string) doctype.Field {
	if name == hookedLeadField {
		return doctype.Field{Name: hookedLeadField, Label: hookedLeadLabel, Type: doctype.Int, Required: true}
	}
	f, _ := c.DT.Field(name)
	return f
}

// hookedValue is the field's value: typed on a redraw, else stored, else the
// default.
func hookedValue(c docscreen.Ctx, s docscreen.StepState, name, def string) string {
	switch {
	case s.Posted:
		return c.Req.PostFormValue(name)
	case c.View != nil && c.View.Inputs[name] != "":
		return c.View.Inputs[name]
	}
	return def
}

func errorFor(errs []doctype.FieldError, name string) string {
	for _, e := range errs {
		if e.Field == name {
			return e.Message
		}
	}
	return ""
}

// hookedDetailsBody is the first step: the vendor and site pickers and a
// delivery time in whole days, which the sweep's 422 types into.
func hookedDetailsBody(c docscreen.Ctx, s docscreen.StepState) (templ.Component, error) {
	var cs []templ.Component
	cs = append(cs, para(hookedBodyTitle))
	for _, name := range []string{"supplier_id", "site_id", hookedLeadField} {
		def := ""
		if name == hookedLeadField {
			def = "7"
		}
		cs = append(cs, docscreen.FieldInput(hookedField(c, name), components.FieldID(name), hookedValue(c, s, name, def), errorFor(s.Errors, name), c.Deps))
	}
	return group(cs...), nil
}

// hookedDetailsSave keeps the vendor and site and refuses a delivery time
// that isn't a whole number.
func hookedDetailsSave(c docscreen.Ctx) (docscreen.Save, error) {
	out := docscreen.Save{Values: map[string]string{
		"supplier_id": c.Req.PostFormValue("supplier_id"), "site_id": c.Req.PostFormValue("site_id"),
	}}
	if _, err := strconv.Atoi(strings.TrimSpace(c.Req.PostFormValue(hookedLeadField))); err != nil {
		out.Errors = []doctype.FieldError{{Field: hookedLeadField, Message: hookedLeadError}}
	}
	return out, nil
}

func hookedDetailsBar(docscreen.Ctx, docscreen.StepState) (docscreen.StepBar, error) {
	return docscreen.StepBar{Primary: hookedBarPrimary}, nil
}

func hookedTermsTop(docscreen.Ctx) (templ.Component, error) {
	return components.Notice(components.Info, hookedTop), nil
}

// hookedLineBody is the line page: item, quantity and note.
func hookedLineBody(c docscreen.Ctx, no int, s docscreen.StepState) (templ.Component, error) {
	stored := map[string]string{}
	if no > 0 && c.View != nil {
		stored = c.View.Lines[no-1].Inputs
	}
	cs := []templ.Component{para(hookedLineTitle)}
	for _, name := range []string{"item_id", "qty", "note"} {
		v := stored[name]
		if s.Posted {
			v = c.Req.PostFormValue(name)
		}
		cs = append(cs, docscreen.FieldInput(hookedField(c, name), components.FieldID(name), v, errorFor(s.Errors, name), c.Deps))
	}
	return group(cs...), nil
}

// hookedLineSave parses the line's quantity as the generic page does.
func hookedLineSave(c docscreen.Ctx, _ int) (docscreen.Save, error) {
	out := docscreen.Save{Values: map[string]string{"item_id": c.Req.PostFormValue("item_id"), "note": c.Req.PostFormValue("note")}}
	qty, ferr := docscreen.ToPlain(hookedField(c, "qty"), c.Req.PostFormValue("qty"))
	if ferr != nil {
		out.Errors = []doctype.FieldError{*ferr}
	}
	out.Values["qty"] = qty
	return out, nil
}

// hookedLineCard names the line's item, not its project, and adds a note.
func hookedLineCard(_ docscreen.Ctx, l docscreen.LineView) components.LineCard {
	card := l.Card
	card.Item, card.Note = l.Display["item_id"], hookedLineNote
	return card
}

func hookedCheck(docscreen.Ctx) (docscreen.CheckExtra, error) {
	return docscreen.CheckExtra{
		Notes:    []string{hookedCheckNote},
		Warnings: []string{hookedCheckWarn, uitest.LongError},
		Totals:   []components.Pair{{Label: hookedCheckTotal, Value: "₹99,99,99,999.99", Num: true}},
		Cards:    []templ.Component{titledCard(hookedCheckCard, para(uitest.LongError))},
	}, nil
}

func hookedPage(c docscreen.Ctx) (docscreen.PageExtra, error) {
	notes := map[int]string{}
	if len(c.View.Lines) > 0 {
		notes[1] = uitest.LongError
	}
	return docscreen.PageExtra{
		Warnings:  []string{hookedPageWarn},
		LineNotes: notes,
		Pairs:     []components.Pair{{Label: hookedPagePair, Value: uitest.LongName100}},
		Cards: []templ.Component{titledCard(hookedPageCard, components.SummaryList([]components.Pair{
			{Label: "First delivery", Value: "24-10-2026"}, {Label: "Last delivery", Value: uitest.LongError},
		}))},
		Notice:  &components.BannerProps{Message: hookedPageNote, Link: &components.Link{Label: "Hand over", Href: "/_gallery/buttons"}},
		More:    []components.MoreAction{{Label: hookedPageMore, Href: "/_gallery/buttons"}},
		Primary: &components.ButtonProps{Label: hookedPagePrimary, Href: "/_gallery/buttons", Variant: components.Primary},
	}, nil
}

func hookedReason(docscreen.Ctx, doctype.Action) (docscreen.ReasonExtra, error) {
	return docscreen.ReasonExtra{Note: hookedReasonNote, Hint: hookedReasonHint}, nil
}

// init registers Hooked once, before Screens or Handler can run: Screens
// reads its steps and states from the registered DocType, and several tests
// call Screens without building the Handler first.
func init() { docscreentest.RegisterHooked() }

// seedHooked stores Hooked's documents and the records its links name in
// docs, and gives Buyer every right on it, write only on its own drafts.
func seedHooked(docs *docfake.Fake, az interface {
	Grant(role, doctype string, r auth.Right, s auth.Scope)
}) []docscreen.ListRow {
	for _, l := range []struct{ dt, display string }{
		{"fixture.Party", uitest.LongCode60 + " " + uitest.LongName100},
		{"Site", "TECH-SITE " + uitest.LongDevanagari},
		{"fixture.Project", "PRJ-METRO " + uitest.LongName100},
		{"fixture.Item", "ST-001 " + uitest.LongDevanagari},
	} {
		docs.SetInfo(doctype.Info{DocType: l.dt, ID: 1, Display: l.display})
	}
	var rows []docscreen.ListRow
	for _, d := range []struct {
		id    int64
		state doctype.State
		by    auth.UserID
	}{
		{hookedIDs.Draft, doctype.StateDraft, priya.ID},
		{hookedIDs.Approved, doctype.StateApproved, priya.ID},
		{hookedIDs.Cancelled, doctype.StateCancelled, priya.ID},
		{hookedIDs.Closed, doctype.StateClosed, priya.ID},
		{hookedReadOnly, doctype.StateDraft, hookedOwner},
		{hookedIDs.Lines500, doctype.StateDraft, priya.ID},
	} {
		info := doctype.Info{
			DocType: docscreentest.Hooked, DocLabel: "Hooked Order", ID: d.id, State: d.state,
			Display: "Draft " + strconv.FormatInt(d.id, 10), Party: uitest.LongName100, CreatedBy: d.by,
		}
		if d.state != doctype.StateDraft {
			info.Number = "HK/26-27/" + uitest.LongCode60[:5] + strconv.FormatInt(d.id, 10)
			info.Display = info.Number
		}
		values := map[string]string{"supplier_id": "1", "site_id": "1", "ref": "PO-REF-0001", "remarks": uitest.LongError}
		n := 3
		if d.id == hookedIDs.Lines500 {
			n = 500
		}
		lines := make([]map[string]string, n)
		for i := range lines {
			lines[i] = map[string]string{"project_id": "1", "item_id": "1", "qty": "100000", "note": uitest.LongName100}
		}
		docscreentest.Seed(docs, info, doc.Record{Version: 1, Values: values, Lines: lines})
		rows = append(rows, docscreen.ListRow{Info: info, Values: values})
	}
	// The sweep's 409 screen saves the draft with an old version; its 422
	// screen is refused by the module's own Save before DOC is called.
	docs.SetResult(docscreentest.Hooked, hookedIDs.Draft, doctype.ActionSave, doc.Result{}, &doc.Error{
		Kind:    doc.KindStale,
		Message: "Someone else changed Draft 142 after you opened it. Reload it and make your changes again.",
	})
	for _, r := range auth.AllRights {
		s := auth.ScopeAll
		if r == auth.RightWrite {
			s = auth.ScopeOwn
		}
		az.Grant("Buyer", docscreentest.Hooked, r, s)
	}
	return rows
}
