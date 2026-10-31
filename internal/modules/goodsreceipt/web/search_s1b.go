package grnweb

import (
	"net/http"
	"strconv"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// S1b: the PO search behind step 1's combobox at 50 or more orders (UX doc
// §5.2, CD-323 (2)).

const (
	poSearchURL  = "/modules/goodsreceipt/po-search"
	hintPOSearch = "Search by PO number or vendor."
	minPOSearch  = 50 // orders from which step 1 is the combobox
)

// poListboxID is the combobox's listbox, which the search fills.
var poListboxID = components.FieldID("purchase_order_id") + "-list"

// poSearch answers GET poSearchURL?q=: the orders POChoices gives for q
// (read-scoped on the PO), widened to fully received ones on all=1, as
// PickerResults rows for step 1's listbox.
func (h *handlers) poSearch(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFrom(r.Context())
	q := r.URL.Query().Get("q")
	list, err := h.d.Service.POChoices(r.Context(), p, r.URL.Query().Get("all") == "1", q, 1)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	rows := make([]components.PickRow, 0, len(list))
	for _, o := range list {
		rows = append(rows, pickRow(o))
	}
	frag := components.PickerResults(poListboxID, rows, q)
	ui.Render(w, r, http.StatusOK, frag, frag)
}

// pickRow is order o as a search row: Code the PO number, Name the vendor,
// Line its site and pending lines.
func pickRow(o goodsreceipt.POChoice) components.PickRow {
	return components.PickRow{ID: o.ID, Code: o.Display, Name: o.VendorName, Line: o.SiteName + " · " + pendingText(o)}
}

// poCombobox is step 1's combobox for list: field f with the search hint,
// searching poSearchURL (with all=1 when widened), and the chosen order, if
// list has it, as its chosen row.
func poCombobox(f components.FieldProps, list []goodsreceipt.POChoice, all bool) *components.PickerProps {
	f.Hint = hintPOSearch
	p := &components.PickerProps{Field: f, SearchURL: poSearchURL, HiddenName: "purchase_order_id", ListboxID: poListboxID}
	if all {
		p.SearchURL += "?all=1"
	}
	for _, o := range list {
		if strconv.FormatInt(o.ID, 10) == f.Value {
			row := pickRow(o)
			p.Chosen = &row
		}
	}
	return p
}
