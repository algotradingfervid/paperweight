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
// them, Validate resets them); rest_not_coming on a saved draft or document
// while it is off (UX doc §5.6): step 2's own Body and Save draw and post it
// whatever Hide says, so a new tick is never dropped; and the document
// fields of a receipt with only service lines (UX doc §5.3). A failed read
// shows the field.
func (m *module) hide(c docscreen.Ctx, f doctype.Field) bool {
	switch {
	case f.Name == "site_id" || f.Name == "project_id" || f.Name == "po_line_id":
		return true
	case f.Name == "rest_not_coming":
		return c.View != nil && !ticked(c.View.Inputs[f.Name])
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
	h := c.Deps.Modules[c.DT.Name].Hide
	return h == nil || !h(c, f)
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
	All       bool             // fully received orders are listed
	Toggle    *components.Link // widens or narrows the list
	EmptyLink *components.Link
}

// detailsBody draws step 1: the order fixed once the draft has lines, else
// the orders to choose from; then the receipt date.
func (m *module) detailsBody(c docscreen.Ctx, s docscreen.StepState) (templ.Component, error) {
	date := dateProps(c, s, components.FieldProps{Name: "receipt_date", Label: labelReceiptDate, Hint: hintReceiptDate})
	if date.Value == "" && !s.Posted && c.View == nil {
		date.Value = date.Today
	}
	if !shown(c, "receipt_date") {
		date = components.DateProps{}
	}
	if c.View != nil && len(c.View.Lines) > 0 {
		return m.fixedPO(c, date)
	}
	p, err := m.pick(c, s)
	if err != nil {
		return nil, err
	}
	return choosePOView(p, date), nil
}

// fixedPO is step 1 of a draft with lines (spec §3.1): its order from the
// GRN's own snapshot, and Delete draft for a user with delete on it.
func (m *module) fixedPO(c docscreen.Ctx, date components.DateProps) (templ.Component, error) {
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
	return fixedPOView(po, href, date), nil
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
		Field: components.FieldProps{ID: components.FieldID("purchase_order_id"), Name: "purchase_order_id", Label: labelPO,
			Hint: hint, Error: errorOf(s.Errors, "purchase_order_id"), Value: chosen},
		Select: len(list) > maxRadios,
		All:    all,
	}
	if p.Select {
		p.Options = append(p.Options, components.Option{})
	}
	for _, o := range list {
		id := strconv.FormatInt(o.ID, 10)
		if p.Select {
			p.Options = append(p.Options, components.Option{Value: id, Label: o.Display + " · " + o.VendorName + " · " + o.SiteName + " · " + pendingText(o)})
			continue
		}
		p.Options = append(p.Options, components.Option{Value: id, Label: o.Display + " · " + o.VendorName, Hint: o.SiteName + " · " + pendingText(o)})
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

// detailsSave is step 1's save. A draft with lines saves only the receipt
// date, plus a posted order, which Validate refuses as a change (spec §4).
// Otherwise the chosen order's values and pending lines come from
// DraftFromPO (plan G6 Do 3), with the typed receipt date; a blank order is
// its 422, another site's order its 403 (CD-240).
func (m *module) detailsSave(c docscreen.Ctx) (docscreen.Save, error) {
	dated := shown(c, "receipt_date")
	date, fe := posted(c, "receipt_date")
	if fe != nil && dated {
		return docscreen.Save{Errors: []doctype.FieldError{*fe}}, nil
	}
	if c.View != nil && len(c.View.Lines) > 0 {
		v := map[string]string{}
		if dated {
			v["receipt_date"] = date
		}
		if po, ok := c.Req.PostForm["purchase_order_id"]; ok {
			v["purchase_order_id"] = po[0]
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

// deliveryForm is step 2's inputs; Show holds the fields Hide shows (the
// tick is always drawn, UX doc §5.6).
type deliveryForm struct {
	ServiceOnly                        bool
	Show                               map[string]bool
	Kind, Number, Vehicle, Transporter components.FieldProps
	EWay, Remarks, Rest                components.FieldProps
	Date                               components.DateProps
	RestChecked                        []string
}

// deliveryBody draws step 2 from the post on a redraw, else from the draft
// (UX doc §5.3).
func (m *module) deliveryBody(c docscreen.Ctx, s docscreen.StepState) (templ.Component, error) {
	only, err := m.serviceOnly(c)
	if err != nil {
		return nil, err
	}
	field := func(name, label, hint string, optional bool, attrs templ.Attributes) components.FieldProps {
		return components.FieldProps{ID: components.FieldID(name), Name: name, Label: label, Hint: hint, Optional: optional,
			Error: errorOf(s.Errors, name), Value: value(c, s, name), Attrs: attrs}
	}
	upper := func(max string) templ.Attributes {
		return templ.Attributes{"maxlength": max, "autocapitalize": "characters", "spellcheck": "false"}
	}
	d := deliveryForm{
		ServiceOnly: only,
		Kind:        field("challan_kind", "Document with the goods", "", false, nil),
		Number:      field("challan_no", "Challan or invoice no.", hintNumber, false, upper("40")),
		Date:        dateProps(c, s, components.FieldProps{Name: "challan_date", Label: "Challan date", Hint: hintDate, Optional: true}),
		Vehicle:     field("vehicle_no", "Vehicle no.", "", true, upper("20")),
		Transporter: field("transporter", "Transporter", "", true, templ.Attributes{"maxlength": "140"}),
		EWay:        field("eway_bill_no", "E-way bill no.", "", true, templ.Attributes{"maxlength": "20", "inputmode": "numeric"}),
		Remarks:     field("remarks", "Remarks", hintRemarks, true, nil),
		Rest:        field("rest_not_coming", "", "", false, nil),
	}
	if ticked(d.Rest.Value) {
		d.RestChecked = []string{"1"}
	}
	d.Show = map[string]bool{}
	for _, name := range append(slices.Clone(documentFields), "remarks") {
		d.Show[name] = shown(c, name)
	}
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

// dateProps is a date field p with its value and error, and docscreen's
// Today and Yesterday.
func dateProps(c docscreen.Ctx, s docscreen.StepState, p components.FieldProps) components.DateProps {
	p.ID, p.Value, p.Error = components.FieldID(p.Name), value(c, s, p.Name), errorOf(s.Errors, p.Name)
	d := components.DateProps{FieldProps: p}
	if c.Deps.Today != nil {
		d.Today, d.Yesterday = c.Deps.Today()
	}
	return d
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
