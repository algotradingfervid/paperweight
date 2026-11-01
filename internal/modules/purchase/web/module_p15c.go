package purchaseweb

import (
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// P15c: Add from requests on the Order's Items step (PO spec §20.4 item 2;
// UX §5.6, §5.7; CD-280 D5, D6).

// The view's query on GET, and its marker on the step form's POST.
const (
	fromKey   = "from"
	fromValue = "requests"
	fromTick  = "rq_line"  // a ticked request row's checkbox, valued its line id
	fromQty   = "rq_qty_"  // + line id: that row's Quantity to order
	fromIDs   = "rq-line-" // + line id: the row's input ids before it is ticked
)

// orderItemsStep is the Order's Items step: the generic body under the Add
// from requests link, or on ?from=requests the view listing open request
// rows, whose Add selected appends the ticked ones.
func (m *module) orderItemsStep() docscreen.Step {
	return docscreen.Step{Top: m.fromTop, Body: m.fromBody, Bar: m.fromBar, Save: m.fromSave}
}

// fromView is the Add from requests body's content.
type fromView struct {
	intro, empty string
	tickable     bool      // a row can be ticked, so the post is this view's
	gone         []fromRow // ticked rows no longer open, kept ticked on a redraw
	cards        []fromCard
}

type fromCard struct {
	title, meta string
	rows        []fromRow
}

// fromRow is one request row: tick is false for a row with nothing pending
// (and not ticked in the post), which shows held instead of a checkbox and a
// quantity.
type fromRow struct {
	tick                    bool
	label, hint, value, uom string
	box, qty                components.FieldProps
	checked, held           []string
	whole                   bool
}

// fromRequests reports whether r is for the Add from requests view: its
// query on GET, its marker on POST.
func fromRequests(r *http.Request) bool {
	if r.Method == http.MethodPost {
		return r.PostFormValue(fromKey) == fromValue
	}
	return r.URL.Query().Get(fromKey) == fromValue
}

// fromPlace is the draft's site and project ids, ok false when either is
// blank. Items is never a new document's first step, so c.View is set.
func fromPlace(c docscreen.Ctx) (site, project int64, ok bool) {
	site, _ = strconv.ParseInt(c.View.Inputs["site_id"], 10, 64)
	project, _ = strconv.ParseInt(c.View.Inputs["project_id"], 10, 64)
	return site, project, site > 0 && project > 0
}

// fromOpen is the open request rows for the draft's site and project that
// the viewer may read (P6's OpenRequestLines), ok false with no site or
// project.
func (m *module) fromOpen(c docscreen.Ctx) ([]purchase.OpenRequest, bool, error) {
	site, project, ok := fromPlace(c)
	if !ok {
		return nil, false, nil
	}
	open, err := m.d.Service.OpenRequestLines(c.Req.Context(), c.Principal, site, project)
	return open, true, err
}

// fromTop is the Add from requests ({n} open) link above the generic Items
// body, n counting rows with pending to order above 0. No link when n is 0
// or the draft has no site or project; an error hides it and is logged.
func (m *module) fromTop(c docscreen.Ctx) (templ.Component, error) {
	open, ok, err := m.fromOpen(c)
	if err != nil {
		slog.WarnContext(c.Req.Context(), "purchase: Add from requests link hidden", "po", c.View.Info.ID, "err", err)
		return templ.NopComponent, nil
	}
	n := 0
	for _, o := range open {
		for _, r := range o.Rows {
			if r.Pending > 0 {
				n++
			}
		}
	}
	if !ok || n == 0 {
		return templ.NopComponent, nil
	}
	return fromLinkView(fmt.Sprintf(fromLinkText, n), itemsURL(c)+"?"+fromKey+"="+fromValue), nil
}

// itemsURL is the draft's bare Items step URL.
func itemsURL(c docscreen.Ctx) string {
	return "/doc/" + c.DT.Name + "/" + strconv.FormatInt(c.View.Info.ID, 10) + "/edit/items"
}

// fromBar is Add selected (nav=stay) and Back to items on the view, with
// no Add selected when nothing can be ticked; docscreen's bar otherwise.
func (m *module) fromBar(c docscreen.Ctx, _ docscreen.StepState) (docscreen.StepBar, error) {
	if !fromRequests(c.Req) {
		return docscreen.StepBar{}, nil
	}
	open, ok, err := m.fromOpen(c)
	if err != nil || !ok {
		return docscreen.StepBar{}, err
	}
	b := docscreen.StepBar{Back: &components.Link{Label: fromBack, Href: itemsURL(c)}}
	if slices.ContainsFunc(open, hasPending) {
		b.Primary, b.Stay = fromPrimary, true
	}
	return b, nil
}

func hasPending(o purchase.OpenRequest) bool {
	return slices.ContainsFunc(o.Rows, func(r purchase.OpenRequestRow) bool { return r.Pending > 0 })
}

// fromBody is the Add from requests view on ?from=requests (or its post),
// nil otherwise. A redraw (422, 409) keeps the posted ticks and quantities;
// a ticked row's inputs carry the ids of the line it would become, so the
// summary links reach them.
func (m *module) fromBody(c docscreen.Ctx, s docscreen.StepState) (templ.Component, error) {
	if !fromRequests(c.Req) {
		return nil, nil
	}
	open, ok, err := m.fromOpen(c)
	if err != nil || !ok {
		return nil, err
	}
	site, project, err := placeCodes(c)
	if err != nil {
		return nil, err
	}
	v := fromView{intro: fmt.Sprintf(fromIntro, site, project), empty: fmt.Sprintf(fromEmpty, site, project)}
	ticked := c.Req.PostForm[fromTick] // nil on GET
	listed := map[string]bool{}
	for _, o := range open {
		card := fromCard{title: o.Display, meta: fmt.Sprintf(fromNoNeededBy, o.RequesterName)}
		if o.NeededBy != (types.Date{}) {
			card.meta = fmt.Sprintf(fromNeededBy, o.RequesterName, types.FormatDate(o.NeededBy))
		}
		for _, r := range o.Rows {
			row, err := m.fromRowOf(c, s, r, ticked)
			if err != nil {
				return nil, err
			}
			listed[row.value] = true
			v.tickable = v.tickable || row.tick
			card.rows = append(card.rows, row)
		}
		v.cards = append(v.cards, card)
	}
	for i, id := range ticked {
		if listed[id] {
			continue
		}
		n := len(c.View.Lines) + i + 1
		row := fromRow{label: fromGoneLabel, value: id, checked: []string{id},
			box: components.FieldProps{ID: docscreen.LineFieldID(n, fromTick), Name: fromTick}}
		for _, e := range s.Errors {
			if e.Line == n {
				row.box.Error = e.Message
			}
		}
		v.tickable = true
		v.gone = append(v.gone, row)
	}
	return fromRequestsView(v), nil
}

// fromRowOf is request row r as the view shows it. A ticked r is the line
// after the draft's lines and the ids ticked before it in the post.
func (m *module) fromRowOf(c docscreen.Ctx, s docscreen.StepState, r purchase.OpenRequestRow, ticked []string) (fromRow, error) {
	id := strconv.FormatInt(r.RequestLineID, 10)
	// A ticked row stays tickable on a redraw even when taken meanwhile, so
	// the tick and quantity the refusal names are kept.
	row := fromRow{label: r.Description + " " + r.ItemCode, value: id, uom: r.UoMCode, whole: r.UoMWhole, tick: r.Pending > 0 || slices.Contains(ticked, id)}
	if !row.tick {
		var other bool
		for _, h := range r.Holders {
			label, ok, err := readableLabel(c, purchase.DocType, h.POID)
			if err != nil {
				return fromRow{}, err
			}
			if !ok {
				if !other {
					row.held = append(row.held, fromHeldOther)
				}
				other = true
				continue
			}
			owner, err := ownerName(c, h.Owner)
			if err != nil {
				return fromRow{}, err
			}
			row.held = append(row.held, fmt.Sprintf(fromHeldOn, label, owner))
		}
		return row, nil
	}
	row.hint = fmt.Sprintf(fromPending, types.FormatQty(r.Pending), r.UoMCode)
	row.box = components.FieldProps{ID: components.FieldID(fromIDs + id), Name: fromTick}
	qtyField, _ := c.DT.Field("qty")
	row.qty = declaredProps(qtyField, components.FieldID(fromIDs+id+"-qty"))
	row.qty.Name, row.qty.Label, row.qty.Value = fromQty+id, fromQtyLabel, r.Pending.Plain()
	if c.Req.PostForm.Has(fromQty + id) { // a redraw keeps the typed quantity
		row.qty.Value = c.Req.PostForm.Get(fromQty + id)
	}
	if i := slices.Index(ticked, id); i >= 0 {
		n := len(c.View.Lines) + i + 1
		row.checked = []string{id}
		row.box.ID, row.qty.ID = docscreen.LineFieldID(n, "request_line_id"), docscreen.LineFieldID(n, "qty")
		for _, e := range s.Errors {
			switch {
			case e.Line != n:
			case e.Field == "qty" || e.Field == "request_line_id": // §20.5's taken row shows at its quantity (UX §5.7)
				row.qty.Error = e.Message
			default:
				row.box.Error = e.Message
			}
		}
	}
	return row, nil
}

// fromSave appends the ticked rows to the draft's lines, as P13's
// CreateFromRequest copies them: item, description and unit from the
// request row, the posted quantity, request_line_id, and discount_pct posted
// blank so lineDefaults applies the last price's discount. A row taken by
// another order meanwhile is still appended: checkRequestRows refuses it
// inside the save. A ticked id the viewer's open rows no longer hold is
// refused at its line. Each ticked id is the line after the draft's lines
// and the ids ticked before it in the post. A generic Items post, a stale
// one or one to an order past draft saves as docscreen's does (Save{}), so
// the engine answers the conflict or the state before any tick (CD-259,
// CD-335).
func (m *module) fromSave(c docscreen.Ctx) (docscreen.Save, error) {
	if !fromRequests(c.Req) {
		return docscreen.Save{}, nil
	}
	if v, _ := strconv.Atoi(c.Req.PostFormValue("version")); v != c.View.Version || c.View.Info.State != doctype.StateDraft {
		return docscreen.Save{}, nil
	}
	ticked := c.Req.PostForm[fromTick]
	if len(ticked) == 0 {
		return docscreen.Save{Errors: []doctype.FieldError{{Message: msgFromNoTick}}}, nil
	}
	open, _, err := m.fromOpen(c)
	if err != nil {
		return docscreen.Save{}, err
	}
	qtyField, _ := c.DT.Field("qty")
	lines := make([]map[string]string, 0, len(c.View.Lines)+len(ticked))
	for _, l := range c.View.Lines {
		lines = append(lines, map[string]string{"id": l.ID})
	}
	rows := map[string]purchase.OpenRequestRow{}
	for _, o := range open {
		for _, r := range o.Rows {
			rows[strconv.FormatInt(r.RequestLineID, 10)] = r
		}
	}
	var errs []doctype.FieldError
	for i, id := range ticked {
		n := len(c.View.Lines) + i + 1
		r, ok := rows[id]
		if !ok {
			msg, err := goneMessage(c, n, id)
			if err != nil {
				return docscreen.Save{}, err
			}
			errs = append(errs, doctype.FieldError{Line: n, Field: fromTick, Message: msg})
			continue
		}
		qty, fe := docscreen.ToPlain(qtyField, c.Req.PostFormValue(fromQty+id))
		if fe != nil {
			errs = append(errs, doctype.FieldError{Line: n, Field: "qty", Message: fmt.Sprintf("Line %d: %s", n, fe.Message)})
		}
		lines = append(lines, map[string]string{
			"item_id": strconv.FormatInt(r.ItemID, 10), "description": r.Description,
			"uom_id": strconv.FormatInt(r.UoMID, 10), "qty": qty,
			"request_line_id": id, "discount_pct": "",
		})
	}
	if len(errs) > 0 {
		return docscreen.Save{Errors: errs}, nil
	}
	return docscreen.Save{Lines: lines}, nil
}

// goneMessage is the refusal for ticked id, line n, that the open rows no
// longer hold: naming its request and row when the viewer may read the
// request, unnamed otherwise and for an id no request line has.
func goneMessage(c docscreen.Ctx, n int, id string) (string, error) {
	lineID, _ := strconv.ParseInt(id, 10, 64)
	var prID int64
	var k int
	var ok bool
	ctx := c.Req.Context()
	err := c.Deps.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		prID, k, ok, err = purchase.RequestRowAt(ctx, tx, lineID)
		return err
	})
	if err != nil || !ok {
		return fmt.Sprintf(msgFromGoneUnread, n), err
	}
	pr, readable, err := readableLabel(c, purchase.RequestDocType, prID)
	if err != nil || !readable {
		return fmt.Sprintf(msgFromGoneUnread, n), err
	}
	return fmt.Sprintf(msgFromGone, n, pr, k), nil
}

// placeCodes is the codes of the draft's site and project, in one read.
func placeCodes(c docscreen.Ctx) (site, project string, err error) {
	ctx := c.Req.Context()
	err = c.Deps.DB.Read(ctx, func(tx *db.Tx) error {
		for name, code := range map[string]*string{"site_id": &site, "project_id": &project} {
			f, _ := c.DT.Field(name)
			dt, _ := doctype.Lookup(f.Link)
			id, _ := strconv.ParseInt(c.View.Inputs[name], 10, 64)
			mi, _, err := dt.Master.Lookup(ctx, tx, id)
			if err != nil {
				return err
			}
			*code = mi.Code
		}
		return nil
	})
	return site, project, err
}
