package itemweb_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/item"
	itemweb "github.com/vardhan/internaltools/internal/masters/item/web"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// No case ID: the screen behaviour Task 22 adds beyond its named tests.

// The service's messages, as literals (CD-142).
const (
	msgStockNotFound    = "Stock UoM not found. Pick one from the list."
	msgPurchaseNotFound = "Purchase UoM not found. Pick one from the list."
	msgTaxNotFound      = "Default tax code not found. Pick one from the list."
	msgRow2UoMNotFound  = "Row 2: UoM not found. Pick one from the list."
	msgRow3UoMBlank     = "Row 3: UoM is required."
	msgRow3Zero         = "Row 3: Conversion factor must be more than 0."
	msgSixDecimals      = "Conversion factor can have at most 6 decimal places." // MST-085
	msgStockReset       = "Stock UoM changed. Re-enter the other conversions."   // MST-089
)

// wantErrAt fails unless the message region of the field at path shows msg
// exactly. It goes by FieldID because this form has several pickers, whose
// search controls all share the name "q".
func wantErrAt(t *testing.T, r webtest.Response, path, msg string) {
	t.Helper()
	sel := `[id="` + components.FieldID(path) + `-msg"]`
	if got := webtest.Squash(r.Doc.Find(sel).Text()); got != msg {
		t.Errorf("message at %s = %q; want %q", path, got, msg)
	}
}

// posted is what a browser posts from the form with action: every named
// input, the checked ones among radios and checkboxes.
func posted(r webtest.Response, action string) url.Values {
	out := url.Values{}
	r.Doc.Find(`form[action="` + action + `"] input[name]`).Each(func(_ int, s *goquery.Selection) {
		kind, _ := s.Attr("type")
		if _, checked := s.Attr("checked"); (kind == "radio" || kind == "checkbox") && !checked {
			return
		}
		name, _ := s.Attr("name")
		value, _ := s.Attr("value")
		out.Add(name, value)
	})
	return out
}

// richItem creates an item with every optional field set: Standard Rate 12.50
// and a Drum row.
func (w *world) richItem(t *testing.T) item.Item {
	t.Helper()
	mtr, drum := w.uomID(t, "MTR"), w.uomID(t, "DRUM")
	_, err := w.svc.Create(context.Background(), w.h.Env.Kavya, item.Input{
		Code: "RICH", Name: "Rich item", Type: item.TypeGoods, HSNSAC: "8544", StockUoMID: mtr, PurchaseUoMID: drum,
		DefaultTaxCodeID: w.taxID(t, "GST18"), IsStock: true, StandardRate: 125000,
		Conversions: []item.Conversion{{UoMID: mtr, Factor: uom.FactorOne}, {UoMID: drum, Factor: 500 * uom.FactorOne}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return w.mustGet(t, "RICH")
}

// The form shows the stored rows, read inside Get: item.Get alone has none.
func TestScreens_ItemFormShowsStoredRows(t *testing.T) {
	w := newWorld(t)
	_, cable := w.seed(t)
	mtr := w.uomID(t, "MTR")
	drum := w.mustUoM(t, "DRUM")

	page := w.h.Get(t, w.h.Env.Kavya, path(cable.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	for name, want := range map[string]string{
		"lines[1].uom_id": strconv.FormatInt(mtr, 10), "lines[1].factor": "1",
		"lines[2].uom_id": strconv.FormatInt(drum.ID, 10), "lines[2].factor": "500",
	} {
		if v, ok := page.Value(name); !ok || v != want {
			t.Errorf("%s = %q (found %v); want %q", name, v, ok, want)
		}
	}
	if page.HasControl("lines[3].factor") {
		t.Error("the form has a row the item doesn't")
	}
	webtest.WantText(t, page, drum.Code+" "+drum.Name)
}

// Saving what the page rendered, rows untouched, keeps every field and every
// row, and writes nothing (CD-208): a form without its rows would wipe the
// table.
func TestScreens_ItemSaveUntouchedKeepsRows(t *testing.T) {
	w := newWorld(t)
	rich := w.richItem(t)
	before := w.conversions(t, rich.ID)

	page := w.h.Get(t, w.h.Env.Kavya, path(rich.ID, ""))
	resp := w.h.Post(t, w.h.Env.Kavya, path(rich.ID, ""), posted(page, path(rich.ID, "")))
	webtest.WantRedirect(t, resp, path(rich.ID, ""))

	if got := w.conversions(t, rich.ID); !slices.Equal(got, before) {
		t.Errorf("conversions after an untouched save = %v; want %v", got, before)
	}
	if got := w.mustGet(t, "RICH"); got != rich {
		t.Errorf("item after an untouched save = %+v; want %+v", got, rich)
	}
}

// The service's refusal of a negative rate reaches the field on both forms
// and nothing is saved (CD-220).
func TestScreens_ItemNegativeRate(t *testing.T) {
	const msg = "Standard Rate can't be negative."
	w := newWorld(t)
	form := w.boltForm(t, "Hex Bolt")
	form.Set("standard_rate", "-1")
	resp := w.h.Post(t, w.h.Env.Kavya, "/m/item/new", form)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	wantErrAt(t, resp, "standard_rate", msg)
	if _, ok := w.byCode(t, "BOLT-M12"); ok {
		t.Fatal("BOLT-M12 saved with a negative rate")
	}

	rich := w.richItem(t)
	edit := posted(w.h.Get(t, w.h.Env.Kavya, path(rich.ID, "")), path(rich.ID, ""))
	edit.Set("standard_rate", "-1")
	resp = w.h.Post(t, w.h.Env.Kavya, path(rich.ID, ""), edit)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	wantErrAt(t, resp, "standard_rate", msg)
	if got := w.mustGet(t, "RICH"); got != rich {
		t.Errorf("item after a refused save = %+v; want %+v", got, rich)
	}
}

// A user without write sees every value and the rows, linked masters as
// "{Code} {Name}" (CD-68).
func TestScreens_ItemReadOnlyShowsRows(t *testing.T) {
	w := newWorld(t)
	rich := w.richItem(t)
	mtr, drum := w.mustUoM(t, "MTR"), w.mustUoM(t, "DRUM")

	page := w.h.Get(t, w.h.Env.Priya, path(rich.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	webtest.WantText(t, page, "RICH Rich item")
	webtest.WantText(t, page, mtr.Code+" "+mtr.Name)
	webtest.WantText(t, page, drum.Code+" "+drum.Name)
	webtest.WantText(t, page, "GST18 GST 18%")
	webtest.WantText(t, page, "₹12.50")
	webtest.WantText(t, page, "500")
	if page.HasControl("name") {
		t.Error("Priya's record page has a form")
	}
}

func (w *world) mustUoM(t *testing.T, code string) uom.UoM {
	t.Helper()
	id := w.uomID(t, code)
	var u uom.UoM
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		var err error
		u, err = uom.New(w.kit).Get(ctx, tx, id)
		return err
	})
	return u
}

// A stored zero rate means none (CD-37): empty on the list, the form and the
// read-only view, never ₹0. A real rate shows in all three.
func TestScreens_ItemZeroRateIsNone(t *testing.T) {
	w := newWorld(t)
	angle, _ := w.seed(t)
	rich := w.richItem(t)
	kavya, priya := w.h.Env.Kavya, w.h.Env.Priya

	list := w.h.Get(t, kavya, "/m/item")
	if text, _ := list.RowLink(path(angle.ID, "")); strings.Contains(text, "₹") {
		t.Errorf("list row of a zero rate = %q; want no rate", text)
	}
	if text, _ := list.RowLink(path(rich.ID, "")); !strings.Contains(text, "₹12.50") {
		t.Errorf("list row of RICH = %q; want ₹12.50", text)
	}

	if v, ok := w.h.Get(t, kavya, path(angle.ID, "")).Value("standard_rate"); !ok || v != "" {
		t.Errorf("form rate of a zero rate = %q (found %v); want empty", v, ok)
	}
	if v, _ := w.h.Get(t, kavya, path(rich.ID, "")).Value("standard_rate"); v != "12.50" {
		t.Errorf("form rate of RICH = %q; want 12.50", v)
	}

	webtest.WantNoText(t, w.h.Get(t, priya, path(angle.ID, "")), "₹")
	webtest.WantText(t, w.h.Get(t, priya, path(rich.ID, "")), "₹12.50")
}

// A value that doesn't parse is refused on its field with the service's or
// NUM's message and nothing is saved (CD-37); strconv's text never shows.
func TestScreens_ItemParseErrors(t *testing.T) {
	_, rateErr := types.ParseRate("abc")
	_, factorErr := types.ParseFactor("abc")
	tests := []struct {
		name, field, value, at, want string
	}{
		{"stock uom", "stock_uom_id", "abc", "stock_uom_id", msgStockNotFound},
		{"stock uom out of range", "stock_uom_id", "99999999999999999999", "stock_uom_id", msgStockNotFound},
		{"purchase uom", "purchase_uom_id", "12x", "purchase_uom_id", msgPurchaseNotFound},
		{"tax code", "default_tax_code_id", "abc", "default_tax_code_id", msgTaxNotFound},
		{"rate", "standard_rate", "abc", "standard_rate", rateErr.Error()},
		{"row uom", "lines[2].uom_id", "abc", "lines[2].uom_id", msgRow2UoMNotFound},
		{"row factor", "lines[2].factor", "abc", "lines[2].factor", "Row 2: " + factorErr.Error()},
		{"row factor seven decimals", "lines[2].factor", "1.1234567", "lines[2].factor", "Row 2: " + msgSixDecimals},
		{"row factor ten decimals", "lines[2].factor", "1.1234567891", "lines[2].factor", "Row 2: " + msgSixDecimals},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			form := w.boltForm(t, "Hex Bolt")
			form.Set(tc.field, tc.value)
			resp := w.h.Post(t, w.h.Env.Kavya, "/m/item/new", form)
			webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
			wantErrAt(t, resp, tc.at, tc.want)
			webtest.WantNoText(t, resp, "strconv")
			webtest.WantNoText(t, resp, "invalid syntax")
			if _, ok := w.byCode(t, "BOLT-M12"); ok {
				t.Fatal("BOLT-M12 saved despite the parse error")
			}
		})
	}
}

// An id that parses but names no record gets the same text from the service.
func TestScreens_ItemUnknownLinks(t *testing.T) {
	w := newWorld(t)
	form := w.boltForm(t, "Hex Bolt")
	form.Set("stock_uom_id", "999999")
	form.Set("purchase_uom_id", "999998")
	form.Set("default_tax_code_id", "999997")
	resp := w.h.Post(t, w.h.Env.Kavya, "/m/item/new", form)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	wantErrAt(t, resp, "stock_uom_id", msgStockNotFound)
	wantErrAt(t, resp, "purchase_uom_id", msgPurchaseNotFound)
	wantErrAt(t, resp, "default_tax_code_id", msgTaxNotFound)
}

// A row naming a UoM that doesn't exist is refused by the service on that row.
func TestScreens_ItemUnknownRowUoM(t *testing.T) {
	w := newWorld(t)
	form := w.boltForm(t, "Hex Bolt")
	form.Set("lines[2].uom_id", "999999")
	form.Set("purchase_uom_id", "")
	resp := w.h.Post(t, w.h.Env.Kavya, "/m/item/new", form)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	wantErrAt(t, resp, "lines[2].uom_id", msgRow2UoMNotFound)
	if _, ok := w.byCode(t, "BOLT-M12"); ok {
		t.Fatal("BOLT-M12 saved with an unknown row UoM")
	}
}

// Changing the stock UoM resets the table to the new stock row (MST-089),
// whatever the form's rows held, a blank one included, and the next page
// shows the service's warning.
func TestScreens_ItemStockChangeResetsRows(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	_, cable := w.seed(t)
	kgs := w.uomID(t, "KGS")

	form := posted(w.h.Get(t, kavya, path(cable.ID, "")), path(cable.ID, ""))
	form.Set("stock_uom_id", strconv.FormatInt(kgs, 10))
	form.Set("lines[3].uom_id", "")
	form.Set("lines[3].factor", "")
	resp := w.h.Post(t, kavya, path(cable.ID, ""), form)
	webtest.WantRedirect(t, resp, path(cable.ID, ""))

	page := w.h.Follow(t, kavya, resp)
	webtest.WantStatus(t, page, http.StatusOK)
	if got := webtest.Squash(page.Doc.Find("#alerts").Text()); !strings.Contains(got, msgStockReset) {
		t.Errorf("#alerts = %q; want %q", got, msgStockReset)
	}
	want := []item.Conversion{{UoMID: kgs, Factor: uom.FactorOne}}
	if got := w.conversions(t, cable.ID); !slices.Equal(got, want) {
		t.Errorf("conversions after the stock change = %v; want %v", got, want)
	}
}

// A parse error doesn't hide the other fields' errors: Validate adds them.
func TestScreens_ItemParseErrorAddsValidate(t *testing.T) {
	w := newWorld(t)
	form := w.boltForm(t, "Hex Bolt")
	form.Set("standard_rate", "abc")
	form.Set("hsn_sac", "72142")
	resp := w.h.Post(t, w.h.Env.Kavya, "/m/item/new", form)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, "hsn_sac", msgHSNLength)
}

// Every posted row reaches the service, the blank one Add leaves too: its
// blank UoM and factor are errors on that row, never a row quietly dropped.
func TestScreens_ItemBlankRowIsRefused(t *testing.T) {
	w := newWorld(t)
	form := w.boltForm(t, "Hex Bolt")
	form.Set("lines[3].uom_id", "")
	form.Set("lines[3].factor", "")
	resp := w.h.Post(t, w.h.Env.Kavya, "/m/item/new", form)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	wantErrAt(t, resp, "lines[3].uom_id", msgRow3UoMBlank)
	wantErrAt(t, resp, "lines[3].factor", msgRow3Zero)
	if _, ok := w.byCode(t, "BOLT-M12"); ok {
		t.Fatal("BOLT-M12 saved with a blank row")
	}
}

// A blank factor on a row with its UoM is that row's error too.
func TestScreens_ItemBlankFactorIsRowError(t *testing.T) {
	w := newWorld(t)
	form := w.boltForm(t, "Hex Bolt")
	form.Set("lines[2].factor", "")
	resp := w.h.Post(t, w.h.Env.Kavya, "/m/item/new", form)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	wantErrAt(t, resp, "lines[2].factor", msgRow2Zero)
}

// Rows the page numbered with gaps keep their order and save as 1..n.
func TestScreens_ItemRowsWithGaps(t *testing.T) {
	w := newWorld(t)
	form := w.boltForm(t, "Hex Bolt")
	box := form.Get("lines[2].uom_id")
	form.Del("lines[2].uom_id")
	form.Del("lines[2].factor")
	form.Set("lines[7].uom_id", box)
	form.Set("lines[7].factor", "100")
	resp := w.h.Post(t, w.h.Env.Kavya, "/m/item/new", form)
	bolt, ok := w.byCode(t, "BOLT-M12")
	if !ok {
		t.Fatalf("POST saved nothing: %d %s", resp.Status, webtest.Squash(resp.Body))
	}
	if got := w.conversions(t, bolt.ID); len(got) != 2 || got[1].Factor != 100*uom.FactorOne {
		t.Errorf("conversions = %v; want the Box row at 100", got)
	}
}

// Blank links and rate are none: the purchase UoM falls back to the stock UoM.
func TestScreens_ItemBlankOptionalFields(t *testing.T) {
	w := newWorld(t)
	form := w.boltForm(t, "Hex Bolt")
	form.Set("purchase_uom_id", "")
	form.Set("default_tax_code_id", "")
	form.Set("standard_rate", "")
	resp := w.h.Post(t, w.h.Env.Kavya, "/m/item/new", form)
	bolt, ok := w.byCode(t, "BOLT-M12")
	if !ok {
		t.Fatalf("POST saved nothing: %d %s", resp.Status, webtest.Squash(resp.Body))
	}
	if bolt.PurchaseUoMID != bolt.StockUoMID || bolt.DefaultTaxCodeID != 0 || bolt.StandardRate != 0 {
		t.Errorf("BOLT-M12 = %+v; want purchase = stock, no tax code, no rate", bolt)
	}
}

// Every scalar on the form reaches the service.
func TestScreens_ItemSavesEveryField(t *testing.T) {
	w := newWorld(t)
	form := w.boltForm(t, "Hex Bolt")
	form.Set("is_stock", "1")
	form.Set("standard_rate", "1,250.50")
	form.Set("lines[2].factor", "9,99,999.999999")
	resp := w.h.Post(t, w.h.Env.Kavya, "/m/item/new", form)
	bolt, ok := w.byCode(t, "BOLT-M12")
	if !ok {
		t.Fatalf("POST saved nothing: %d %s", resp.Status, webtest.Squash(resp.Body))
	}
	wantRate, err := types.ParseRate("1250.5")
	if err != nil {
		t.Fatal(err)
	}
	if !bolt.IsStock || bolt.StandardRate != wantRate {
		t.Errorf("BOLT-M12 = %+v; want a stock item at %v", bolt, wantRate)
	}
	wantFactor, err := types.ParseFactor("999999.999999")
	if err != nil {
		t.Fatal(err)
	}
	if got := w.conversions(t, bolt.ID); got[1].Factor != wantFactor {
		t.Errorf("row 2 factor = %v; want %v", got[1].Factor, wantFactor)
	}

	page := w.h.Get(t, w.h.Env.Kavya, path(bolt.ID, ""))
	if _, ok := page.CheckboxValue("is_stock"); !ok || page.Doc.Find(`input[name="is_stock"][checked]`).Length() != 1 {
		t.Error("is_stock isn't ticked on the record page")
	}
}

// HSN/SAC is a CodeInput checked at /ui/validate/hsn; the factor a NumberInput
// of Item's own 6-decimal kind (CD-203) in the wide track, on stored rows and
// on one Add just made.
func TestScreens_ItemControls(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	_, cable := w.seed(t)

	blank := w.h.Get(t, kavya, "/m/item/new")
	if v, _ := blank.Doc.Find(`input[name="hsn_sac"]`).Attr("hx-post"); v != "/ui/validate/hsn" {
		t.Errorf("hsn_sac hx-post = %q; want /ui/validate/hsn", v)
	}

	page := w.h.Get(t, kavya, path(cable.ID, ""))
	factor := page.Doc.Find(`input[name="lines[2].factor"]`)
	if v, _ := factor.Attr("hx-post"); v != "/ui/validate/factor6" {
		t.Errorf("factor hx-post = %q; want /ui/validate/factor6", v)
	}
	if factor.Closest(".rows-cell--wide").Length() != 1 {
		t.Error("factor isn't in a wide cell")
	}
	if page.Doc.Find(`input[name="lines[2].uom_id"]`).Closest(".rows-cell--wide").Length() != 0 {
		t.Error("uom_id is in a wide cell; only the factor needs it")
	}
}

// Add and Remove redraw the form with the rows changed and save nothing
// (CD-201).
func TestScreens_ItemAddRemoveRow(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	_, cable := w.seed(t)
	before := w.conversions(t, cable.ID)

	form := posted(w.h.Get(t, kavya, path(cable.ID, "")), path(cable.ID, ""))
	form.Set("_rows", "add")
	page := w.h.Post(t, kavya, path(cable.ID, ""), form)
	webtest.WantStatus(t, page, http.StatusOK)
	if !page.HasControl("lines[3].uom_id") || !page.HasControl("lines[3].factor") {
		t.Error("Add drew no third row")
	}

	form.Set("_rows", "remove:2")
	page = w.h.Post(t, kavya, path(cable.ID, ""), form)
	webtest.WantStatus(t, page, http.StatusOK)
	if page.HasControl("lines[2].uom_id") {
		t.Error("Remove left the second row")
	}
	if got := w.conversions(t, cable.ID); !slices.Equal(got, before) {
		t.Errorf("conversions after Add and Remove = %v; want unchanged %v", got, before)
	}
}

// The screens speak in Item's words, every string of Copy.
func TestScreens_ItemCopy(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya

	empty := w.h.Get(t, kavya, "/m/item")
	webtest.WantStatus(t, empty, http.StatusOK)
	webtest.WantText(t, empty, "No items yet.")
	if got := webtest.Squash(empty.Doc.Find("h1").First().Text()); got != "Items" {
		t.Errorf("list title = %q; want Items", got)
	}
	webtest.WantText(t, empty, "Show disabled items")
	webtest.WantText(t, empty, "Search items")

	blank := w.h.Get(t, kavya, "/m/item/new")
	webtest.WantStatus(t, blank, http.StatusOK)
	webtest.WantText(t, blank, "New item")
	webtest.WantText(t, blank, "Unit conversions")
	webtest.WantText(t, blank, "Add conversion")

	resp := w.h.Post(t, kavya, "/m/item/new", w.boltForm(t, "Hex Bolt"))
	bolt := w.mustGet(t, "BOLT-M12")
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Item saved.")
	webtest.WantText(t, w.h.Get(t, kavya, "/m/item"), "1 item")

	resp = w.h.Post(t, kavya, path(bolt.ID, "/disable"), url.Values{"version": {ver(bolt.Version)}})
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Item disabled.")
	webtest.WantText(t, w.h.Get(t, kavya, "/m/item"), "All items are disabled.")

	off := w.mustGet(t, "BOLT-M12")
	resp = w.h.Post(t, kavya, path(bolt.ID, "/enable"), url.Values{"version": {ver(off.Version)}})
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Item enabled.")

	on := w.mustGet(t, "BOLT-M12")
	resp = w.h.Post(t, kavya, path(bolt.ID, "/delete"), url.Values{"version": {ver(on.Version)}})
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Item deleted.")
}

// factor6 is Item's validator kind: 6 decimals at most, MST-085's message past
// that, the value formatted back for the input.
func TestParsers_Factor6(t *testing.T) {
	h := components.ValidateHandler(itemweb.Parsers())
	tests := []struct {
		name, in, value, msg string
		invalid              bool
	}{
		{"six decimals", "999999.999999", "9,99,999.999999", "", false},
		{"grouped", "9,99,999.999999", "9,99,999.999999", "", false},
		{"small", "0.000001", "0.000001", "", false},
		{"whole", "100", "100", "", false},
		{"seven decimals", "1.1234567", "1.1234567", msgSixDecimals, true},
		{"ten decimals", "1.1234567891", "1.1234567891", msgSixDecimals, true},
		{"zero", "0", "0", "Conversion factor must be more than 0.", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{"_id": {"f-x"}, "_name": {"x"}, "x": {tc.in}}
			req := httptest.NewRequest(http.MethodPost, "/ui/validate/factor6", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			var ev struct {
				Field struct {
					Value   string `json:"value"`
					Invalid bool   `json:"invalid"`
				} `json:"ui:field"`
			}
			if err := json.Unmarshal([]byte(rec.Header().Get("HX-Trigger")), &ev); err != nil {
				t.Fatal(err)
			}
			if ev.Field.Value != tc.value || ev.Field.Invalid != tc.invalid {
				t.Errorf("field = %+v; want value %q invalid %v", ev.Field, tc.value, tc.invalid)
			}
			doc, err := goquery.NewDocumentFromReader(rec.Body)
			if err != nil {
				t.Fatal(err)
			}
			if got := webtest.Squash(doc.Find("#f-x-msg").Text()); got != tc.msg {
				t.Errorf("message = %q; want %q", got, tc.msg)
			}
		})
	}
}
