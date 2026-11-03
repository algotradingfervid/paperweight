package itemweb_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// No case ID: UI v2 D2 (§5.8, N10). An item picker's search offers
// "New item "{q}"" to a user with create on Item; POST /m/item/quick saves a
// Goods item from Code, Name, UoM, HSN and Tax code through the item service
// and answers with the picker, the new item chosen.

const itemQuick = "/m/item/quick"

func (w *world) quickForm(t *testing.T, kv ...string) url.Values {
	t.Helper()
	v := url.Values{
		"pick_id": {"f-item_id"}, "pick_label": {"Item"}, "pick_required": {"1"},
		"pick_search": {"/pick/Item"}, "pick_name": {"item_id"}, "pick_list": {"f-item_id-list"},
		"role": {""}, "quick_code": {"TMT-12"}, "quick_name": {"TMT bar 12 mm"},
		"quick_stock_uom_id": {strconv.FormatInt(w.uomID(t, "KGS"), 10)}, "quick_hsn_sac": {"7214"},
		"quick_default_tax_code_id": {strconv.FormatInt(w.taxID(t, "GST18"), 10)},
	}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return v
}

func TestQuickD2_ItemNewOption(t *testing.T) {
	w := newWorld(t)
	r := w.h.GetHTMX(t, w.h.Env.Kavya, "/pick/Item?q=TMT+bar")
	if got := webtest.Squash(r.Doc.Find(`[role="option"][data-quick] > span`).Text()); got != `New item "TMT bar"` {
		t.Errorf("New option = %q", got)
	}
	sheet := r.Doc.Find(`dialog#pick-item-sheet`)
	if got := webtest.Squash(sheet.Find("h2").Text()); got != "New item" {
		t.Errorf("sheet title = %q", got)
	}
	for _, name := range []string{"quick_code", "quick_name", "quick_stock_uom_id", "quick_hsn_sac", "quick_default_tax_code_id"} {
		if sheet.Find(`[name="`+name+`"]`).Length() == 0 {
			t.Errorf("the sheet has no %s", name)
		}
	}
	if got := webtest.Squash(sheet.Find(`#f-quick_code-hint`).Text()); got != "Made from the name. You can change it." {
		t.Errorf("code hint = %q", got)
	}
	if got := webtest.Squash(sheet.Find(`#f-quick_name-hint`).Text()); got != "The code is made from this. You can change it later in Items." {
		t.Errorf("name hint = %q", got)
	}
	if n := sheet.Find(`.field__hint[id$="-hint"]`).Length(); n != 2 {
		t.Errorf("%d field hints, want the code's and the name's", n)
	}
	if sheet.Find(`div[hidden]:has([name="quick_code"])`).Length() != 1 || sheet.Find(`[name="quick_code"]`).AttrOr("value", "x") != "" {
		t.Error("Code should be blank and hidden: the save makes it from the name")
	}
	if got := webtest.Squash(sheet.Find(`label[for="f-quick_hsn_sac"]`).Text()); got != "HSN/SAC * (required)" {
		t.Errorf("HSN label = %q, want the star: the quick form adds Goods", got)
	}
	if got := webtest.Squash(w.h.GetHTMX(t, w.h.Env.Priya, "/pick/Item?q=TMT").Doc.Find(`[data-quick]`).Text()); got != "" {
		t.Errorf("Priya's New option = %q, want none", got)
	}
	// An item picker has no role: a role offers nothing.
	if got := webtest.Squash(w.h.GetHTMX(t, w.h.Env.Kavya, "/pick/Item?q=TMT&role=vendor").Doc.Find(`[data-quick]`).Text()); got != "" {
		t.Errorf("New option with a role = %q, want none", got)
	}
}

func TestQuickD2_ItemSaved(t *testing.T) {
	w := newWorld(t)
	r := w.h.Browser(w.h.Env.Kavya).PostHTMX(t, itemQuick, w.quickForm(t))
	webtest.WantStatus(t, r, http.StatusOK)
	it := w.mustGet(t, "TMT-12")
	kgs, gst18 := w.uomID(t, "KGS"), w.taxID(t, "GST18")
	if it.Name != "TMT bar 12 mm" || it.Type != item.TypeGoods || it.HSNSAC != "7214" || it.StockUoMID != kgs ||
		it.PurchaseUoMID != kgs || it.DefaultTaxCodeID != gst18 || it.IsStock || it.StandardRate != 0 {
		t.Errorf("saved item = %+v", it)
	}
	if got := webtest.Squash(r.Doc.Find(`.picker [data-picker-summary]`).Text()); got != "TMT-12 TMT bar 12 mm" {
		t.Errorf("chosen summary = %q", got)
	}
	if got := r.Doc.Find(`.picker input[type="hidden"][name="item_id"]`).AttrOr("value", ""); got != strconv.FormatInt(it.ID, 10) {
		t.Errorf("item_id = %q, want %d", got, it.ID)
	}
}

// A bad UoM or tax code is refused next to its field before anything is
// written; so are a missing name and a taken code. No create right is 403.
func TestQuickD2_ItemRefusals(t *testing.T) {
	w := newWorld(t)
	w.seed(t)
	count := func() int {
		var n int
		w.read(t, func(ctx context.Context, tx *db.Tx) error {
			return tx.SQL().QueryRowContext(ctx, `SELECT count(*) FROM item`).Scan(&n)
		})
		return n
	}
	before := count()
	webtest.WantStatus(t, w.h.Browser(w.h.Env.Priya).PostHTMX(t, itemQuick, w.quickForm(t)), http.StatusForbidden)
	webtest.WantStatus(t, w.h.Browser(w.h.Env.Kavya).PostHTMX(t, itemQuick, w.quickForm(t, "role", "vendor")), http.StatusBadRequest)
	for _, c := range []struct{ field, value, msg string }{
		{"quick_stock_uom_id", "999999", item.MsgStockNotFound},
		{"quick_stock_uom_id", "kg", item.MsgStockNotFound},
		{"quick_default_tax_code_id", "999999", taxcode.MsgDefaultNotFound},
		{"quick_name", "", "Name is required."},
		{"quick_code", "ms-angle-50", "Code MS-ANGLE-50 is already used by Item MS Angle 50x50x5. Choose another code."},
		{"quick_hsn_sac", "72142", msgHSNLength},
	} {
		r := w.h.Browser(w.h.Env.Kavya).PostHTMX(t, itemQuick, w.quickForm(t, c.field, c.value))
		webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
		webtest.WantFieldErrorID(t, r, "f-"+c.field, c.msg) // components.FieldID: pickers have no named control to describe
		if got := webtest.Squash(r.Doc.Find(".banner").Text()); got != "" {
			t.Errorf("%s: banner %q beside the field error, want none (Purchase UoM follows the UoM)", c.field, got)
		}
		if got := r.Header.Get("HX-Retarget"); got != "#f-item_id-list-sheet-form" {
			t.Errorf("%s: HX-Retarget = %q", c.field, got)
		}
	}
	if n := count(); n != before {
		t.Errorf("items = %d after refusals, want %d", n, before)
	}
}

// R8: an item's code is made from the name, past a collision, and a refused
// HSN shows with the code still hidden.
func TestQuickR8_ItemCode(t *testing.T) {
	w := newWorld(t)
	webtest.WantStatus(t, w.h.Browser(w.h.Env.Kavya).PostHTMX(t, itemQuick, w.quickForm(t, "quick_code", "")), http.StatusOK)
	r := w.h.Browser(w.h.Env.Kavya).PostHTMX(t, itemQuick, w.quickForm(t, "quick_code", ""))
	webtest.WantStatus(t, r, http.StatusOK)
	if got := webtest.Squash(r.Doc.Find("[data-picker-summary]").Text()); got != "TMTBAR12MM2 TMT bar 12 mm" {
		t.Errorf("chosen = %q, want the second code", got)
	}
	r = w.h.Browser(w.h.Env.Kavya).PostHTMX(t, itemQuick, w.quickForm(t, "quick_code", "", "quick_name", "Angle", "quick_hsn_sac", ""))
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	if r.Doc.Find(`div[hidden]:has([name="quick_code"])`).Length() != 1 {
		t.Error("another field's refusal shows Code")
	}
}
