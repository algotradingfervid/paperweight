package grnweb

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"sync"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// S1: Choose purchase order and Delivery details (UX doc §5.2, §5.3).

// Step 1 and 2 copy (UX doc §5.2, §5.3 copy tables).
const (
	labelPO           = "Purchase order"
	hintPORestricted  = "Orders with items still to receive at your sites."
	hintPOAll         = "Orders with items still to receive."
	hintPOWidened     = "Approved orders at your sites, including fully received ones."
	labelShowAll      = "Show fully received orders"
	labelShowPending  = "Show only orders with items pending"
	msgNoOrders       = "No approved orders are waiting for delivery."
	msgAskBuyer       = "Ask the buyer if an order should be here."
	msgDeleteToChange = "Delete this draft to receive against another order."
	deleteDraftLabel  = "Delete draft"
	labelSite         = "Site"
	labelReceiptDate  = "Receipt date"
	hintReceiptDate   = "The day the goods arrived."
	hintFullyReceived = "Fully received"

	hintNumber    = "As printed on the document. Type Not recorded if it's lost."
	hintDate      = "The date on the challan or invoice."
	hintRemarks   = "Printed on the GRN."
	hintRest      = "Tick only if the vendor told you nothing more will be sent on this order. The buyer is told when you submit."
	msgPhotoLater = "You can photograph the challan on the last step, before you submit."
	remarksMax    = 500
)

// The step slugs S1 builds; items is S2's (CD-256 (8)).
const (
	slugDetails  = "details"
	slugDelivery = "delivery"
)

// maxRadios is the most orders shown as radios; more is a Select (UX doc
// §5.2, design-system §7).
const maxRadios = 6

// kindOptions are challan_kind's stored values, which are their display
// texts (CD-175).
var kindOptions = []components.Option{{Value: "Delivery challan", Label: "Delivery challan"}, {Value: "Tax invoice", Label: "Tax invoice"}}

// restOptions is the tick as one row: its label is the field's label.
var restOptions = []components.Option{{Value: "1", Label: "Vendor says the rest won't come", Hint: hintRest}}

// documentFields are the delivery fields a receipt with only service lines
// leaves out (UX doc §5.3).
var documentFields = []string{"challan_kind", "challan_no", "challan_date", "vehicle_no", "transporter", "eway_bill_no"}

// stepLabels are the wizard's step labels (spec §11, hooks spec §4.1).
func (m *module) stepLabels() map[string]string {
	return map[string]string{slugDetails: "Choose purchase order", slugDelivery: "Delivery details", "items": "Received quantities"}
}

// detailsStep is step 1: the order and the receipt date.
func (m *module) detailsStep() docscreen.Step {
	return docscreen.Step{Body: m.detailsBody, Save: m.detailsSave}
}

// deliveryStep is step 2: the document with the goods, remarks and the tick.
func (m *module) deliveryStep() docscreen.Step {
	return docscreen.Step{Body: m.deliveryBody, Save: m.deliverySave}
}

// hide hides site_id, project_id and po_line_id everywhere (the steps post
// them, Validate resets them); rest_not_coming on a saved document past
// draft while it is off (UX doc §5.6, CD-332): a draft's pages show the
// pair, as step 2 draws and posts the tick; and the document
// fields of a receipt with only service lines (UX doc §5.3). A failed read
// shows the field.
func (m *module) hide(c docscreen.Ctx, f doctype.Field) bool {
	switch {
	case f.Name == "site_id" || f.Name == "project_id" || f.Name == "po_line_id":
		return true
	case f.Name == "rest_not_coming":
		return c.View != nil && c.View.Info.State != doctype.StateDraft && !ticked(c.View.Inputs[f.Name])
	case slices.Contains(documentFields, f.Name):
		only, err := m.serviceOnly(c)
		return err == nil && only
	}
	return false
}

// shown reports whether the Hide docscreen asks (the app's Module in
// c.Deps.Modules) shows field name: a hidden field stays out of the custom
// steps' renders and posts too (CD-319).
func shown(c docscreen.Ctx, name string) bool {
	f, _ := c.DT.Field(name)
	return !c.Deps.Modules[c.DT.Name].Hide(c, f)
}

// ticked reports whether a Check field's value is on.
func ticked(v string) bool { return v == "1" || v == "true" }

// serviceOnly reports whether the draft has lines and none of them is goods.
func (m *module) serviceOnly(c docscreen.Ctx) (bool, error) {
	if c.View == nil || len(c.View.Lines) == 0 {
		return false, nil
	}
	f, err := m.receiptFacts(c)
	return !f.HasGoods, err
}

// factsCache holds the last request's Facts: Hide asks for them once per
// field and render path, and one read per request is enough. Concurrent
// requests may replace each other's entry, which costs only a re-read.
type factsCache struct {
	mu    sync.Mutex
	req   *http.Request
	id    int64
	facts goodsreceipt.ReceiptFacts
}

// receiptFacts is Service.Facts for c's draft, read once per request.
func (m *module) receiptFacts(c docscreen.Ctx) (goodsreceipt.ReceiptFacts, error) {
	fc := &m.facts
	fc.mu.Lock()
	defer fc.mu.Unlock()
	if fc.req == c.Req && fc.id == c.View.Info.ID {
		return fc.facts, nil
	}
	f, err := m.d.Service.Facts(c.Req.Context(), c.Principal, c.View.Info.ID)
	if err != nil {
		return f, err
	}
	fc.req, fc.id, fc.facts = c.Req, c.View.Info.ID, f
	return f, nil
}

// poPick is step 1's choice of order.
type poPick struct {
	Field     components.FieldProps
	Options   []components.Option
	Select    bool
	Search    *components.PickerProps // the combobox, at minPOSearch orders or more (S1b)
	All       bool                    // fully received orders are listed
	Toggle    *components.Link        // widens or narrows the list
	EmptyLink *components.Link
}

// detailsBody draws step 1: the order fixed once the draft has lines, else
// the orders to choose from; then the receipt date.
func (m *module) detailsBody(c docscreen.Ctx, s docscreen.StepState) (templ.Component, error) {
	date := dateProps(c, s, components.FieldProps{Name: "receipt_date", Label: labelReceiptDate, Hint: hintReceiptDate})
	if date.Value == "" && !s.Posted && c.View == nil {
		date.Value = date.Today
	}
	dated := shown(c, "receipt_date")
	if c.View != nil && len(c.View.Lines) > 0 {
		return m.fixedPO(c, date, dated)
	}
	p, err := m.pick(c, s)
	if err != nil {
		return nil, err
	}
	return choosePOView(p, date, dated), nil
}

// fixedPO is step 1 of a draft with lines (spec §3.1): its order from the
// GRN's own snapshot, and Delete draft for a user with delete on it.
func (m *module) fixedPO(c docscreen.Ctx, date components.DateProps, dated bool) (templ.Component, error) {
	ctx := c.Req.Context()
	f, err := m.receiptFacts(c)
	if err != nil {
		return nil, err
	}
	href := ""
	switch err := c.Deps.Auth.Can(ctx, c.Principal, goodsreceipt.DocType, auth.RightDelete, &c.View.Record); {
	case err == nil:
		href = "/doc/" + goodsreceipt.DocType + "/" + strconv.FormatInt(c.View.Info.ID, 10) + "/delete"
	case !errors.Is(err, auth.ErrForbidden):
		return nil, err
	}
	po := []components.Pair{{Label: labelPO, Value: f.PONumber + " · " + f.VendorName}, {Label: labelSite, Value: f.SiteName}}
	return fixedPOView(po, c.View.Inputs["purchase_order_id"], href, date, dated), nil
}

// pick builds the choice of order (UX doc §5.2). The chosen order is the
// posted one on a redraw, the draft's own, the ?po= order when p may
// receive against it (CD-283), or the only one listed. The list widens to
// fully received orders on ?all=1, or when the chosen order is only there.
func (m *module) pick(c docscreen.Ctx, s docscreen.StepState) (poPick, error) {
	all := c.Req.FormValue("all") == "1"
	var chosen string
	switch {
	case s.Posted:
		chosen = c.Req.PostFormValue("purchase_order_id")
	case c.View != nil:
		chosen = c.View.Inputs["purchase_order_id"]
	default:
		var err error
		if chosen, err = m.preselect(c); err != nil {
			return poPick{}, err
		}
	}
	wide, err := m.d.Service.AllPOChoices(c.Req.Context(), c.Principal)
	if err != nil {
		return poPick{}, err
	}
	if chosen != "" && !all && !listed(pending(wide), chosen) && listed(wide, chosen) {
		all = true
	}
	list := wide
	if !all {
		list = pending(wide)
	}
	if chosen == "" && !s.Posted && len(list) == 1 {
		chosen = strconv.FormatInt(list[0].ID, 10) // one visible order is chosen (spec §11)
	}
	hint, err := poHint(c, all)
	if err != nil {
		return poPick{}, err
	}
	p := poPick{
		Field: declared(c.DT, "purchase_order_id", components.FieldProps{ID: components.FieldID("purchase_order_id"), Name: "purchase_order_id", Label: labelPO,
			Hint: hint, Error: errorOf(s.Errors, "purchase_order_id"), Value: chosen}),
		All: all,
	}
	if len(list) >= minPOSearch {
		p.Search = poCombobox(p.Field, list, all) // Options stay empty (UX doc §5.2)
	} else {
		p.Options, p.Select = poOptions(list)
	}
	switch {
	case len(list) > 0 && all:
		p.Toggle = &components.Link{Label: labelShowPending, Href: pickURL(c, false)}
	case len(list) > 0:
		p.Toggle = &components.Link{Label: labelShowAll, Href: pickURL(c, true)}
	case !all && len(wide) > 0:
		p.EmptyLink = &components.Link{Label: labelShowAll, Href: pickURL(c, true)}
	}
	return p, nil
}

// poOptions are list as radios, or as a Select after a blank first option
// when there are more than maxRadios.
func poOptions(list []goodsreceipt.POChoice) (opts []components.Option, sel bool) {
	sel = len(list) > maxRadios
	if sel {
		opts = append(opts, components.Option{})
	}
	for _, o := range list {
		id := strconv.FormatInt(o.ID, 10)
		if sel {
			opts = append(opts, components.Option{Value: id, Label: o.Display + " · " + o.VendorName + " · " + o.SiteName + " · " + pendingText(o)})
			continue
		}
		opts = append(opts, components.Option{Value: id, Label: o.Display + " · " + o.VendorName, Hint: o.SiteName + " · " + pendingText(o)})
	}
	return opts, sel
}

// preselect is the ?po= order when p may receive against it (CD-283), with
// the same check as ReceiveLink; otherwise nothing, and no error, so another
// site's order isn't named (CD-233).
func (m *module) preselect(c docscreen.Ctx) (string, error) {
	id, _ := strconv.ParseInt(c.Req.URL.Query().Get("po"), 10, 64) // not an id: 0, ignored
	if id <= 0 {
		return "", nil
	}
	ok, err := m.d.Service.Receivable(c.Req.Context(), c.Principal, id)
	if err != nil || !ok {
		return "", err
	}
	return strconv.FormatInt(id, 10), nil
}

// pending is the orders of list with something pending.
func pending(list []goodsreceipt.POChoice) []goodsreceipt.POChoice {
	var out []goodsreceipt.POChoice
	for _, o := range list {
		if o.Pending > 0 {
			out = append(out, o)
		}
	}
	return out
}

// listed reports whether order id is in list.
func listed(list []goodsreceipt.POChoice, id string) bool {
	return slices.ContainsFunc(list, func(o goodsreceipt.POChoice) bool { return strconv.FormatInt(o.ID, 10) == id })
}

// pendingText is an order's hint after its site: its lines with something
// pending, or Fully received.
func pendingText(o goodsreceipt.POChoice) string {
	switch {
	case o.Pending == 0:
		return hintFullyReceived
	case o.Lines == 1:
		return "1 of 1 line pending"
	}
	return fmt.Sprintf("%d of %d lines pending", o.Pending, o.Lines)
}

// poHint is the legend's hint: the widened text, or the pending text for a
// user restricted to some sites or for everyone else.
func poHint(c docscreen.Ctx, all bool) (string, error) {
	if all {
		return hintPOWidened, nil
	}
	var restricted bool
	err := c.Deps.DB.Read(c.Req.Context(), func(tx *db.Tx) (err error) {
		restricted, err = c.Deps.Auth.Restricted(c.Req.Context(), tx, c.Principal, string(auth.DimSite))
		return err
	})
	if restricted {
		return hintPORestricted, err
	}
	return hintPOAll, err
}

// pickURL is this step's URL with ?all=1 or without it, keeping ?po=.
func pickURL(c docscreen.Ctx, all bool) string {
	q := url.Values{}
	if po := c.Req.URL.Query().Get("po"); po != "" {
		q.Set("po", po)
	}
	if all {
		q.Set("all", "1")
	}
	if len(q) == 0 {
		return c.Req.URL.Path
	}
	return c.Req.URL.Path + "?" + q.Encode()
}

// detailsSave is step 1's save. On an existing draft docscreen has loaded
// it and checked the write right; a stale page or a document past draft
// saves nothing of its own, so DOC answers the conflict or the state before
// anything posted is parsed (CD-259). A draft with lines saves only the
// receipt date; a posted order other than its own, or none, is refused
// (spec §4). Otherwise the chosen order's values and pending lines come
// from DraftFromPO (plan G6 Do 3), with the typed receipt date; a blank
// order is its 422, another site's order its 403 (CD-240).
func (m *module) detailsSave(c docscreen.Ctx) (docscreen.Save, error) {
	if c.View != nil {
		n, err := strconv.Atoi(c.Req.PostFormValue("version")) // as docscreen reads it
		if err != nil || n != c.View.Version || c.View.Info.State != doctype.StateDraft {
			return docscreen.Save{Values: map[string]string{}}, nil
		}
	}
	dated := shown(c, "receipt_date")
	var date string
	if dated {
		var fe *doctype.FieldError
		if date, fe = posted(c, "receipt_date"); fe != nil {
			return docscreen.Save{Errors: []doctype.FieldError{*fe}}, nil
		}
	}
	if c.View != nil && len(c.View.Lines) > 0 {
		v := map[string]string{}
		if dated {
			v["receipt_date"] = date
		}
		po := c.Req.PostFormValue("purchase_order_id")
		if po != c.View.Inputs["purchase_order_id"] {
			return docscreen.Save{Errors: []doctype.FieldError{{Field: "purchase_order_id", Message: msgDeleteToChange}}}, nil
		}
		return docscreen.Save{Values: v}, nil
	}
	id, _ := strconv.ParseInt(c.Req.PostFormValue("purchase_order_id"), 10, 64) // not an id: 0, "Choose a purchase order."
	values, lines, err := m.d.Service.DraftFromPO(c.Req.Context(), c.Principal, id)
	var he doctype.HTTPError
	if errors.As(err, &he) && len(he.FieldErrors()) > 0 {
		return docscreen.Save{Errors: he.FieldErrors()}, nil
	}
	if err != nil {
		return docscreen.Save{}, err
	}
	if dated {
		values["receipt_date"] = date
	}
	return docscreen.Save{Values: values, Lines: lines}, nil
}

// deliveryForm is step 2's inputs: the shown ones in field order, the
// Field.More ones left out to More.
type deliveryForm struct {
	Inputs []templ.Component
	More   moreFields
}

// moreFields gathers step 2's Field.More inputs for one MoreDetails after
// the others, as docscreen's moreBox does (UI v2 §5.5 item 3).
type moreFields struct {
	labels []string
	inputs []templ.Component
	open   bool
}

// add puts f's input in, as shown with value and errMsg; either opens the
// group.
func (m *moreFields) add(f doctype.Field, value, errMsg string, in templ.Component) {
	m.labels = append(m.labels, f.Label)
	m.inputs = append(m.inputs, in)
	m.open = m.open || value != "" || errMsg != ""
}

// deliveryBody draws step 2 from the post on a redraw, else from the draft
// (UX doc §5.3). A field submit requires of this draft is marked required
// as a declared one is (goodsreceipt.SubmitRequired).
func (m *module) deliveryBody(c docscreen.Ctx, s docscreen.StepState) (templ.Component, error) {
	facts, err := m.receiptFacts(c)
	if err != nil {
		return nil, err
	}
	need := goodsreceipt.SubmitRequired(facts.HasGoods)
	field := func(name, label, hint string, attrs templ.Attributes) components.FieldProps {
		p := declared(c.DT, name, components.FieldProps{ID: components.FieldID(name), Name: name, Label: label, Hint: hint,
			Error: errorOf(s.Errors, name), Value: value(c, s, name), Attrs: attrs})
		p.Required = p.Required || slices.Contains(need, name) // submit requires it of this draft
		return p
	}
	upper := func(max string) templ.Attributes {
		return templ.Attributes{"maxlength": max, "autocapitalize": "characters", "spellcheck": "false"}
	}
	var d deliveryForm
	add := func(p components.FieldProps, in templ.Component) {
		f, _ := c.DT.Field(p.Name)
		switch {
		case !shown(c, f.Name):
		case f.More:
			d.More.add(f, p.Value, p.Error, in)
		default:
			d.Inputs = append(d.Inputs, in)
		}
	}
	kind := field("challan_kind", "Document with the goods", "", nil)
	add(kind, components.Radios(kind, kindOptions))
	number := field("challan_no", "Challan or invoice no.", hintNumber, upper("40"))
	add(number, components.TextInput(number))
	date := dateProps(c, s, components.FieldProps{Name: "challan_date", Label: "Challan date", Hint: hintDate})
	add(date.FieldProps, components.DateInput(date))
	vehicle := field("vehicle_no", "Vehicle no.", "", upper("20"))
	add(vehicle, components.TextInput(vehicle))
	transporter := field("transporter", "Transporter", "", templ.Attributes{"maxlength": "140"})
	add(transporter, components.TextInput(transporter))
	eway := field("eway_bill_no", "E-way bill no.", "", templ.Attributes{"maxlength": "20", "inputmode": "numeric"})
	add(eway, components.TextInput(eway))
	remarks := field("remarks", "Remarks", hintRemarks, nil)
	add(remarks, components.Textarea(remarks, remarksMax))
	rest := field("rest_not_coming", "", "", nil)
	var checked []string
	if ticked(rest.Value) {
		checked = []string{"1"}
	}
	d.Inputs = append(d.Inputs, components.Checkboxes(rest, restOptions, checked)) // drawn whatever Hide says (UX doc §5.6)
	return deliveryView(d), nil
}

// deliverySave is step 2's save: the fields the step shows, with the tick
// sent as false when it is off (UX doc §5.3).
func (m *module) deliverySave(c docscreen.Ctx) (docscreen.Save, error) {
	only, err := m.serviceOnly(c)
	if err != nil {
		return docscreen.Save{}, err
	}
	v := map[string]string{"rest_not_coming": "false"}
	if ticked(c.Req.PostFormValue("rest_not_coming")) {
		v["rest_not_coming"] = "true"
	}
	if shown(c, "remarks") {
		v["remarks"] = c.Req.PostFormValue("remarks")
	}
	if only {
		return docscreen.Save{Values: v}, nil
	}
	if shown(c, "challan_date") {
		date, fe := posted(c, "challan_date")
		if fe != nil {
			return docscreen.Save{Errors: []doctype.FieldError{*fe}}, nil
		}
		v["challan_date"] = date
	}
	for _, name := range []string{"challan_kind", "challan_no", "vehicle_no", "transporter", "eway_bill_no"} {
		if shown(c, name) {
			v[name] = c.Req.PostFormValue(name)
		}
	}
	return docscreen.Save{Values: v}, nil
}

// posted is the posted value of the GRN field name in DOC's plain form, or
// NUM's parse error on it.
func posted(c docscreen.Ctx, name string) (string, *doctype.FieldError) {
	f, _ := c.DT.Field(name)
	return docscreen.ToPlain(f, c.Req.PostFormValue(name))
}

// value is field name's value as an input shows it: posted on a redraw,
// else the draft's.
func value(c docscreen.Ctx, s docscreen.StepState, name string) string {
	if s.Posted {
		return c.Req.PostFormValue(name)
	}
	if c.View != nil {
		return c.View.Inputs[name]
	}
	return ""
}

// dateProps is a date field p with its value, error and declared marks,
// and docscreen's Today and Yesterday.
func dateProps(c docscreen.Ctx, s docscreen.StepState, p components.FieldProps) components.DateProps {
	p.ID, p.Value, p.Error = components.FieldID(p.Name), value(c, s, p.Name), errorOf(s.Errors, p.Name)
	d := components.DateProps{FieldProps: declared(c.DT, p.Name, p)}
	if c.Deps.Today != nil {
		d.Today, d.Yesterday = c.Deps.Today()
	}
	return d
}

// declared is p with Required and Tip from the declaration of field name,
// as docscreen's FieldInput sets them (CD-341 (2), CD-350).
func declared(dt *doctype.DocType, name string, p components.FieldProps) components.FieldProps {
	f, _ := dt.Field(name)
	p.Required, p.Tip = f.Required, f.Tip
	return p
}

// errorOf is the message of the header error on field name, if any.
func errorOf(errs []doctype.FieldError, name string) string {
	for _, e := range errs {
		if e.Field == name && e.Line == 0 {
			return e.Message
		}
	}
	return ""
}

// ReceiveLink is the PO page's Receive goods link for PO poID (CD-283):
// step 1 with the order preselected, ok false when p may not receive
// against it (create on the GRN, read on the PO through auth.CheckLinkTx,
// CD-312) or the check fails. internal/app wires it into purchaseweb.
func (d Deps) ReceiveLink(ctx context.Context, p auth.Principal, poID int64) (href string, ok bool) {
	if ok, err := d.Service.Receivable(ctx, p, poID); err != nil || !ok {
		return "", false
	}
	return "/doc/" + goodsreceipt.DocType + "/new?po=" + strconv.FormatInt(poID, 10), true
}
