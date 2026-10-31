package purchaseweb

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

// Create PO page texts (UX §5.18; spec §20.4).
const (
	fromRequestTitle  = "Create a purchase order"
	itemsToOrderTitle = "Items to order"
	vendorHint        = "The last price paid to this vendor is filled in."
	createLabel       = "Create purchase order"
	createBusy        = "Creating…"
	// msgNothingPending is "Everything on {PR} is already on an order."
	msgNothingPending = "Everything on %s is already on an order."
	// msgCreatedFromRequest is the success flash, "Draft {new} was created
	// from {PR}." (UX §5.18).
	msgCreatedFromRequest = "Draft %d was created from %s."
	vendorField           = "vendor_id"
)

// linkedOrderColumns are the Linked orders rows: the title (the order's
// display), vendor, receipt status and state (UX §5.16).
var linkedOrderColumns = []components.Column{
	{Key: "vendor", Label: "Vendor", Priority: components.PMeta},
	{Key: "receipt", Label: "Receipt", Priority: components.PMeta},
	{Key: "state", Label: "State", Priority: components.PState},
}

// fromRequestView is the Create PO page.
type fromRequestView struct {
	opts             layout.PageOpts
	summary, nothing string
	back             components.Link
	cards            []components.LineCard
	action           string
	version          int
	vendor           components.PickerProps
	errs             []components.FieldErr
	conflict         string
	refusals         []string
}

// fromRequestPage serves GET /modules/purchase/requests/{id}/order (UX
// §5.18). AUTH's 403 and 404 are the platform's error page.
func (h *handlers) fromRequestPage(w http.ResponseWriter, r *http.Request) {
	h.fromRequestView(w, r, http.StatusOK, nil)
}

// renderFromRequest redraws the Create PO page with status and errs; P13's
// POST calls it for a 422 or 409. The page reloads its own view from the
// path id and keeps the posted vendor.
var renderFromRequest = func(h *handlers, w http.ResponseWriter, r *http.Request, status int, errs []doctype.FieldError) {
	h.fromRequestView(w, r, status, errs)
}

// fromRequestView renders the page for the request in the path with status:
// errs on vendor_id go to the picker and the error summary; a 409's message
// is the conflict banner; any other message is a danger banner, unless it is
// the nothing-pending text the page already shows. A redraw's form keeps the
// posted version (CD-94).
func (h *handlers) fromRequestView(w http.ResponseWriter, r *http.Request, status int, errs []doctype.FieldError) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64) // not a number: 0, which no request has (404)
	vendorID, _ := strconv.ParseInt(r.PostFormValue(vendorField), 10, 64)
	p, _ := auth.PrincipalFrom(r.Context())
	st, err := h.d.Service.FromRequestState(r.Context(), p, id, vendorID)
	if err != nil && status == http.StatusConflict {
		// A stale post on a request the page can't show (a draft's state
		// refusal): the conflict text comes first (CD-259), with no form.
		web.Error(w, r, status, errs[0].Message)
		return
	}
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	req := st.Request
	back := components.Link{Label: req.Display, Href: "/doc/" + purchase.RequestDocType + "/" + strconv.FormatInt(id, 10)}
	parts := []string{"From " + req.Display, req.RequesterName, st.SiteCode}
	if req.NeededBy != (types.Date{}) {
		parts = append(parts, "Needed by "+types.FormatDate(req.NeededBy))
	}
	v := fromRequestView{
		opts:    layout.PageOpts{Title: fromRequestTitle, Width: layout.Form, Back: &back},
		summary: strings.Join(parts, " · "), back: back, action: fromRequestPath(id), version: st.Version,
	}
	if posted, err := strconv.Atoi(r.PostFormValue("version")); err == nil {
		v.version = posted // a redraw keeps the posted version (CD-94)
	}
	for _, row := range req.Rows {
		if row.Pending > 0 {
			v.cards = append(v.cards, components.LineCard{No: row.LineNo, Item: row.Description, Code: row.ItemCode, QtyRate: types.FormatQtyUoM(row.Pending, row.UoMCode)})
		}
	}
	if len(v.cards) == 0 {
		v.nothing = fmt.Sprintf(msgNothingPending, req.Display)
	}
	fp := components.FieldProps{ID: components.FieldID(vendorField), Name: vendorField, Label: "Vendor", Hint: vendorHint}
	for _, e := range errs {
		switch {
		case e.Field == vendorField:
			fp.Error = e.Message
			v.errs = append(v.errs, components.FieldErr{ID: fp.ID, Message: e.Message})
		case status == http.StatusConflict:
			v.conflict = e.Message
		case e.Message != v.nothing:
			v.refusals = append(v.refusals, e.Message)
		}
	}
	v.vendor = components.PickerProps{Field: fp, SearchURL: vendorSearchURL(), HiddenName: vendorField, ListboxID: fp.ID + "-list"}
	if vendorID != 0 {
		row := components.PickRow{ID: vendorID, Code: strconv.FormatInt(vendorID, 10)} // the raw id when unreadable (CD-233)
		if st.VendorCode != "" {
			row.Code, row.Name = st.VendorCode, st.VendorName
		}
		v.vendor.Chosen = &row
	}
	ui.Render(w, r, status, fromRequestPage(v), fromRequestMain(v))
}

// vendorSearchURL is the Order's vendor picker search, as docscreen's
// FieldInput builds it for vendor_id (vendors only).
func vendorSearchURL() string {
	dt, _ := doctype.Lookup(purchase.DocType)
	f, _ := dt.Field(vendorField)
	return "/pick/" + f.Link + "?role=" + f.LinkRole
}
