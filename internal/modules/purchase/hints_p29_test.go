package purchase_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// p29NotFilled are the fields no user fills in that are neither ReadOnly,
// System nor RollUp: set by module routes (request_line_id, copied_from_*)
// or by Validate (request_date, hidden as an input, CD-258 Q8 (a)).
var p29NotFilled = map[string]bool{
	"request_line_id": true, "copied_from_no": true, "copied_from_id": true, "request_date": true,
}

// P29 (UI v2): every field a user fills in on purchase.Order and
// purchase.Request has its one-sentence hint, pinned exactly; a new
// editable field without a hint, or a hint on a field nobody fills in,
// fails.
func TestPO_FieldHints(t *testing.T) {
	want := map[string]map[string]string{
		purchase.DocType: {
			"vendor_id":         "Pick the party you are buying from; only vendors are listed.",
			"vendor_address_id": "Pick the address to print on the order; the vendor's primary billing address is chosen for you.",
			"site_id":           "Pick the site the goods are to be delivered to.",
			"project_id":        "Left blank, the site's project is used.",
			"order_date":        "Today or an earlier date; it sets the financial year in the PO number.",
			"required_by":       "The date you need the goods at the site, on or after the order date.",
			"payment_terms_id":  "Left blank, the vendor's usual terms are used, for example 30 days from the bill date.",
			"vendor_quote_ref":  "The vendor's quotation number, printed on the order, for example QT-2026-118.",
			"terms":             "Left blank, the company's standard terms are used; type a dash to print none.",
			"reverse_charge":    "Tick only if your CA has said reverse charge applies, so GST is shown as payable by us and left out of the total.",
			"remarks":           "For people in the company only, and never printed.",
			"earlier_po_no":     "Only for orders first raised on paper; printed on the order so the vendor recognises it.",
			"earlier_po_date":   "The date of the paper order, given together with Earlier PO no.",
			"item_id":           "Pick the item to buy.",
			"description":       "Left blank, the item's name is used; it is printed on the order.",
			"uom_id":            "Left blank, the item's stock unit is used; another unit needs a conversion on the item.",
			"qty":               "How many to order in the chosen UoM, more than 0.",
			"price":             "Left blank, the last price paid to this vendor on this project is used, or else the item's standard rate.",
			"discount_pct":      "The vendor's discount on the rate, from 0 to 100, for example 5 for 5%.",
			"tax_code_id":       "Left blank, the item's tax code is used.",
			"hsn_sac":           "Left blank, the item's HSN or SAC code is used.",
			"already_received":  "What arrived before this order was entered in the app.",
		},
		purchase.RequestDocType: {
			"site_id":     "Pick the site the items are needed at.",
			"project_id":  "Left blank, the site's project is used.",
			"required_by": "The date you need the items, on or after the request date.",
			"purpose":     `For example, "Slab casting on level 3".`,
			"item_id":     "Pick the item you need.",
			"description": "Left blank, the item's name is used.",
			"uom_id":      "Left blank, the item's stock unit is used.",
			"qty":         "How many you need in the chosen unit, more than 0.",
			"note":        "For example, a brand or size.",
		},
	}
	for name, hints := range want {
		dt := lookup(t, name)
		for _, f := range dt.Fields {
			editable := !f.ReadOnly && !f.System && !f.RollUp && !p29NotFilled[f.Name]
			w, pinned := hints[f.Name]
			switch {
			case editable && f.Hint == "":
				t.Errorf("%s.%s is filled in by users but has no Hint", name, f.Name)
			case !editable && f.Hint != "":
				t.Errorf("%s.%s is never filled in by users but has Hint %q", name, f.Name, f.Hint)
			case editable && !pinned:
				t.Errorf("%s.%s Hint %q is not pinned in this test", name, f.Name, f.Hint)
			case pinned && f.Hint != w:
				t.Errorf("%s.%s Hint = %q, want %q", name, f.Name, f.Hint, w)
			}
		}
		for f := range hints {
			field(t, dt, f)
		}
	}
}
