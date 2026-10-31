package purchaseweb

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// P15a's Module functions: the Order wizard (UX §5.4 to §5.6, §5.8).

// Terms step and line texts (UX §5.5, §5.6).
const (
	noVendorText     = "Choose a vendor on Details first."
	goToDetailsLabel = "Go to Details"
	// noAddressFormat is "{vendor} has no addresses. Add one in Parties, then
	// come back."
	noAddressFormat = "%s has no addresses. Add one in Parties, then come back."
	noGSTIN         = "No GSTIN"
	earlierSummary  = "Earlier paper order"
	// lastPriceFormat is "Last bought at ₹{rate} from {vendor} on {date}
	// ({PO})" (spec §24.2).
	lastPriceFormat = "Last bought at ₹%s from %s on %s (%s)"
)

// addresses reads a vendor's addresses inside the caller's tx; ForParty uses
// no Env field.
var addresses = address.New(mstkit.Env{})

// perRequest holds one value per request, dropped when the request's context
// ends, for the hooks docscreen calls many times on one page (Hide per field,
// LineCard per line) that can't return an error.
type perRequest[T any] struct{ m sync.Map }

// get is r's value, made by load on r's first call.
func (p *perRequest[T]) get(r *http.Request, load func() T) T {
	if v, ok := p.m.Load(r); ok {
		return v.(T)
	}
	v := load()
	if _, loaded := p.m.LoadOrStore(r, v); !loaded {
		context.AfterFunc(r.Context(), func() { p.m.Delete(r) })
	}
	return v
}

var (
	rulesOf  perRequest[bool]                                    // Approvals.HasRules for purchase.Order
	pricesOf perRequest[map[purchase.ItemUoM]purchase.LastPrice] // LastPricesFor every line of c.View
)

// orderTermsStep is the Order's Terms step: the vendor address as radios of
// the vendor's own addresses (UX §5.5); the generic post saves it.
func (m *module) orderTermsStep() docscreen.Step {
	return docscreen.Step{Body: m.termsBody}
}

// termsBody renders the Terms fields: the address radios, the standard
// FieldInput for the rest, and the earlier-PO fields in a disclosure, open
// when either has a value or an error.
func (m *module) termsBody(c docscreen.Ctx, st docscreen.StepState) (templ.Component, error) {
	steps, err := docscreen.FormSteps(c.DT)
	if err != nil {
		return nil, err
	}
	var fields []string
	for _, s := range steps {
		if s.Slug == "terms" {
			fields = s.Fields
		}
	}
	value := func(name string) string {
		if st.Posted {
			return c.Req.PostFormValue(name)
		}
		return c.View.Inputs[name]
	}
	var shown, earlier []templ.Component
	open := false
	for _, name := range fields {
		f, _ := c.DT.Field(name)
		if hides(c, name) {
			continue
		}
		msg := ""
		for _, e := range st.Errors {
			if e.Line == 0 && e.Field == name {
				msg = e.Message
				break
			}
		}
		switch name {
		case "vendor_address_id":
			in, err := addressInput(c, f, value(name), msg, steps[0].Slug)
			if err != nil {
				return nil, err
			}
			shown = append(shown, in)
		case "earlier_po_no", "earlier_po_date":
			open = open || value(name) != "" || msg != ""
			earlier = append(earlier, docscreen.FieldInput(f, components.FieldID(name), value(name), msg, c.Deps))
		default:
			shown = append(shown, docscreen.FieldInput(f, components.FieldID(name), value(name), msg, c.Deps))
		}
	}
	return termsView(shown, earlier, open), nil
}

// declaredProps is f's control props with the element id id: its name,
// label, hint, tip and Required, as docscreen's FieldInput fills them
// (CD-341 (2), CD-350).
func declaredProps(f doctype.Field, id string) components.FieldProps {
	return components.FieldProps{ID: id, Name: f.Name, Label: f.Label, Hint: f.Hint, Tip: f.Tip, Required: f.Required}
}

// addressInput is the vendor address radios, `{city} · {GSTIN}` with the
// address as hint, value checked when it is one of them or else the primary
// billing address; with no vendor, or a vendor with no address, the text
// that says so instead, msg and an empty posted address.
func addressInput(c docscreen.Ctx, f doctype.Field, value, msg, details string) (templ.Component, error) {
	p := declaredProps(f, components.FieldID(f.Name))
	p.Error = msg
	vendor := c.View.Inputs["vendor_id"]
	if vendor == "" {
		href := "/doc/" + c.DT.Name + "/" + strconv.FormatInt(c.View.Info.ID, 10) + "/edit/" + details
		p.Hint = noVendorText
		return noVendorView(p, href), nil
	}
	id, err := strconv.ParseInt(vendor, 10, 64)
	if err != nil {
		return nil, err
	}
	ctx := c.Req.Context()
	var list []address.Address
	if err := c.Deps.DB.Read(ctx, func(tx *db.Tx) error {
		list, err = addresses.ForParty(ctx, tx, id, false)
		return err
	}); err != nil {
		return nil, err
	}
	if len(list) == 0 {
		_, name, _ := strings.Cut(c.View.Display["vendor_id"], " ")
		p.Hint = fmt.Sprintf(noAddressFormat, name)
		return noAddressView(p), nil
	}
	opts := make([]components.Option, 0, len(list))
	primary := ""
	for _, a := range list {
		v := strconv.FormatInt(a.ID, 10)
		if v == value {
			p.Value = v
		}
		if a.PrimaryBilling {
			primary = v
		}
		opts = append(opts, components.Option{Value: v, Label: a.City + " · " + gstinOrNone(a.GSTIN), Hint: addressHint(a)})
	}
	if p.Value == "" {
		p.Value = primary
	}
	return components.Radios(p, opts), nil
}

func gstinOrNone(g string) string {
	if g == "" {
		return noGSTIN
	}
	return g
}

// addressHint is the address's lines on one line: `12 Dock Road, Mumbai
// 400010`.
func addressHint(a address.Address) string {
	var parts []string
	for _, s := range []string{a.Line1, a.Line2, strings.TrimSpace(a.City + " " + a.Pincode)} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// orderHide always hides the fields module routes set (request_line_id,
// copied_from_no, copied_from_id, copy_notes). Earlier PO no. and date show
// while purchase.Order has no approval rule (CD-258 Q7 (a)) or when either
// has a value; Already received only when Earlier PO no. is set (UX §5.6).
// Receipt hides while the order is edited (P34). A nil View is no record
// (CD-327).
func (m *module) orderHide(c docscreen.Ctx, f doctype.Field) bool {
	switch f.Name {
	case "receipt_status":
		return editing(c)
	case "request_line_id", "copied_from_no", "copied_from_id", "copy_notes":
		return true
	case "earlier_po_no", "earlier_po_date":
		if stored(c, "earlier_po_no") != "" || stored(c, "earlier_po_date") != "" {
			return false
		}
		return m.hasRules(c)
	case "already_received":
		return stored(c, "earlier_po_no") == ""
	}
	return false
}

// editing reports whether c is a new record or a draft, which the wizard
// edits; on the list and search pages (Listing) there is no record (CD-327
// (1), CD-334).
func editing(c docscreen.Ctx) bool {
	return !c.Listing && (c.View == nil || c.View.Info.State == doctype.StateDraft)
}

// stored is the record's header value name, "" with no record.
func stored(c docscreen.Ctx, name string) string {
	if c.View == nil {
		return ""
	}
	return c.View.Inputs[name]
}

// hasRules reports whether an approval rule exists for purchase.Order, read
// once per request. On an error the fields show (false) and it is logged.
// With no request (c.Req nil) it reads every time.
func (m *module) hasRules(c docscreen.Ctx) bool {
	ctx := context.Background()
	if c.Req != nil {
		ctx = c.Req.Context()
	}
	load := func() bool {
		var has bool
		err := c.Deps.DB.Read(ctx, func(tx *db.Tx) (err error) {
			has, err = m.d.Approvals.HasRules(ctx, tx, purchase.DocType)
			return err
		})
		if err != nil {
			slog.ErrorContext(ctx, "purchaseweb: approval rules not read, earlier-PO fields shown", "err", err)
			return false
		}
		return has
	}
	if c.Req == nil {
		return load()
	}
	return rulesOf.get(c.Req, load)
}

// orderCheck adds to the check page (UX §5.8): the copy notes, the place
// reason and the reverse-charge note; OrderWarnings; the Owner card (to the
// owner only) and the Comments card.
func (m *module) orderCheck(c docscreen.Ctx) (docscreen.CheckExtra, error) {
	ctx := c.Req.Context()
	id := c.View.Info.ID
	var x docscreen.CheckExtra
	if n := c.View.Inputs["copy_notes"]; n != "" {
		x.Notes = strings.Split(n, "\n")
	}
	err := c.Deps.DB.Read(ctx, func(tx *db.Tx) error {
		place, err := purchase.PlaceReason(ctx, tx, id)
		if err != nil {
			return err
		}
		if place != "" {
			x.Notes = append(x.Notes, place)
		}
		rc, err := purchase.ReverseChargeNote(ctx, tx, id)
		if rc != "" {
			x.Notes = append(x.Notes, rc)
		}
		return err
	})
	if err != nil {
		return docscreen.CheckExtra{}, err
	}
	if x.Warnings, err = m.d.Service.OrderWarnings(ctx, c.Principal, id); err != nil {
		return docscreen.CheckExtra{}, err
	}
	if owner := ownerCard(c, id); owner != nil {
		x.Cards = append(x.Cards, owner)
	}
	comments, err := m.commentsCard(c, purchase.DocType, id)
	if err != nil {
		return docscreen.CheckExtra{}, err
	}
	x.Cards = append(x.Cards, comments)
	return x, nil
}

// orderLineCard is an Order line's card: `40 BAG × ₹350.00` and `Received
// 25 of 40 BAG` with the unit (CD-258 G8), the tax, and the last-price note
// (CD-258 Q6). An Order line has no note field of its own, so the note is
// the last price alone. A field the screen's Hide hides is left out.
func (m *module) orderLineCard(c docscreen.Ctx, l docscreen.LineView) components.LineCard {
	l = shownLine(c, l)
	card := l.Card
	var parts []string
	if q := l.Display["qty"]; q != "" {
		parts = append(parts, strings.TrimSpace(q+" "+unitOf(l)))
	}
	if r := l.Display["price"]; r != "" {
		parts = append(parts, r)
	}
	card.QtyRate, card.Tax, card.Note = strings.Join(parts, " × "), taxOf(l), m.lastPriceNote(c, l)
	if card.Received != "" {
		card.Received = ""
		if r := l.Display["received_qty"]; r != "" {
			parts := []string{"Received", r}
			if q := l.Display["qty"]; q != "" {
				parts = append(parts, "of", q)
			}
			if u := unitOf(l); u != "" {
				parts = append(parts, u)
			}
			card.Received = strings.Join(parts, " ")
		}
	}
	return card
}

// hides reports whether the screen's Hide for c.DT (the Module docscreen
// was given, so a wrapper's too) hides field name.
func hides(c docscreen.Ctx, name string) bool {
	h := c.Deps.Modules[c.DT.Name].Hide
	f, ok := c.DT.Field(name)
	return ok && h != nil && h(c, f)
}

// lineCtx is c for line l's fields: Deps carries l's stored inputs, as
// docscreen's own line cards do.
func lineCtx(c docscreen.Ctx, l docscreen.LineView) docscreen.Ctx {
	c.Deps = c.Deps.WithStored(l.Inputs)
	return c
}

// shownLine is l without the values of the line fields the screen hides.
// l.Card is docscreen's, which leaves them out itself (M0-47, CD-319 (9)).
func shownLine(c docscreen.Ctx, l docscreen.LineView) docscreen.LineView {
	lc := lineCtx(c, l)
	l.Inputs, l.Display = maps.Clone(l.Inputs), maps.Clone(l.Display)
	for _, f := range c.DT.Fields {
		if f.OnLine && hides(lc, f.Name) {
			delete(l.Inputs, f.Name)
			delete(l.Display, f.Name)
		}
	}
	return l
}

// taxOf is line l's tax code name, from its "{Code} {Name}" display.
func taxOf(l docscreen.LineView) string {
	_, name, _ := strings.Cut(l.Display["tax_code_id"], " ")
	return name
}

// lineKey is line l's item and unit, ok false when either is blank.
func lineKey(l docscreen.LineView) (purchase.ItemUoM, bool) {
	item, err1 := strconv.ParseInt(l.Inputs["item_id"], 10, 64)
	uom, err2 := strconv.ParseInt(l.Inputs["uom_id"], 10, 64)
	return purchase.ItemUoM{ItemID: item, UoMID: uom}, err1 == nil && err2 == nil
}

// lastPriceShown reports whether a PO in state s shows the last-price note:
// draft, pending and rejected (plan P15a); an approved or closed PO's line
// notes are the pending figures (P15b).
func lastPriceShown(s doctype.State) bool {
	return s == doctype.StateDraft || s == doctype.StatePending || s == doctype.StateRejected
}

// lastPriceNote is `Last bought at ₹{rate} from {vendor} on {date} ({PO})`,
// the newest price for l's item and unit that the viewer may read, from one
// LastPricesFor call per request for every line of c.View; "" when there is
// none, or when the screen hides the item, unit, rate or vendor it names. On
// an error the note is left out and the error is logged.
func (m *module) lastPriceNote(c docscreen.Ctx, l docscreen.LineView) string {
	k, ok := lineKey(l)
	if !ok || !lastPriceShown(c.View.Info.State) || hides(c, "vendor_id") || hides(lineCtx(c, l), "price") {
		return ""
	}
	// The memo's key set is the lines of the first View this request asks
	// about; a later View in the same request reuses it.
	prices := pricesOf.get(c.Req, func() map[purchase.ItemUoM]purchase.LastPrice {
		var keys []purchase.ItemUoM
		for _, line := range c.View.Lines {
			if k, ok := lineKey(line); ok {
				keys = append(keys, k)
			}
		}
		ctx := c.Req.Context()
		prices, err := m.d.Service.LastPricesFor(ctx, c.Principal, keys)
		if err != nil {
			slog.ErrorContext(ctx, "purchaseweb: last prices not read, notes left out", "po", c.View.Info.ID, "err", err)
			return nil
		}
		return prices
	})
	p, ok := prices[k]
	if !ok {
		return ""
	}
	return fmt.Sprintf(lastPriceFormat, types.FormatRate(p.Rate), p.VendorName, types.FormatDate(p.OrderDate), p.PODisplay)
}

// orderLineGrid is an Order line's five display cells from 64rem (CD-313):
// Item (name, code under it), Qty, UoM, Rate and Tax; docscreen adds #,
// Amount, the actions and the note. A hidden field's cell is blank.
func (m *module) orderLineGrid(c docscreen.Ctx, l docscreen.LineView) []components.GridCell {
	lc := lineCtx(c, l)
	l = shownLine(c, l)
	item := components.GridCell{Label: "Item", Text: l.Card.Item, Sub: l.Card.Code}
	if hides(lc, "item_id") {
		item.Text, item.Sub = "", ""
	}
	return []components.GridCell{
		item,
		{Label: "Qty", Text: l.Display["qty"], Kind: "qty"},
		{Label: "UoM", Text: unitOf(l)},
		{Label: "Rate", Text: l.Display["price"], Kind: "rate"},
		{Label: "Tax", Text: taxOf(l)},
	}
}
