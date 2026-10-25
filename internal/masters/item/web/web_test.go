package itemweb_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/item"
	itemweb "github.com/vardhan/internaltools/internal/masters/item/web"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Screens under test (MST Task 22): /m/item list, /new, /{id}, POST /{id}
// with version, /{id}/disable, /{id}/enable, /{id}/delete and the picker
// /m/item/pick?q=. Field names are the DocType's Field names; conversion rows
// post as lines[N].uom_id and lines[N].factor (doctype.FieldError.Path).
const (
	staleBolt    = "Someone else changed Item BOLT-M12 after you opened it. Reload it and make your changes again."
	msgHSNLength = "HSN code must be 4, 6 or 8 digits."            // MST-071
	msgRow2Zero  = "Row 2: Conversion factor must be more than 0." // MST-081
)

type world struct {
	h   *webtest.Harness
	svc *item.Service
	kit mstkit.Env
}

func newWorld(t *testing.T) *world {
	t.Helper()
	h := webtest.New(t, func(k mstkit.Env) func(chi.Router) { return itemweb.Routes(item.New(k), k) })
	return &world{h: h, svc: item.New(h.Env.Kit), kit: h.Env.Kit}
}

func (w *world) read(t *testing.T, fn func(ctx context.Context, tx *db.Tx) error) {
	t.Helper()
	ctx := context.Background()
	if err := w.h.Env.DB.Read(ctx, func(tx *db.Tx) error { return fn(ctx, tx) }); err != nil {
		t.Fatal(err)
	}
}

func (w *world) uomID(t *testing.T, code string) int64 {
	t.Helper()
	var u uom.UoM
	var ok bool
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		var err error
		u, ok, err = uom.New(w.kit).ByCode(ctx, tx, code)
		return err
	})
	if !ok {
		t.Fatalf("seeded UoM %s missing", code)
	}
	return u.ID
}

func (w *world) taxID(t *testing.T, code string) int64 {
	t.Helper()
	var tc taxcode.TaxCode
	var ok bool
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		var err error
		tc, ok, err = taxcode.New(w.kit).ByCode(ctx, tx, code)
		return err
	})
	if !ok {
		t.Fatalf("seeded tax code %s missing", code)
	}
	return tc.ID
}

func (w *world) byCode(t *testing.T, code string) (item.Item, bool) {
	t.Helper()
	var it item.Item
	var ok bool
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		var err error
		it, ok, err = w.svc.ByCode(ctx, tx, code)
		return err
	})
	return it, ok
}

func (w *world) mustGet(t *testing.T, code string) item.Item {
	t.Helper()
	it, ok := w.byCode(t, code)
	if !ok {
		t.Fatalf("item %s not found", code)
	}
	return it
}

func (w *world) conversions(t *testing.T, id int64) []item.Conversion {
	t.Helper()
	var out []item.Conversion
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		var err error
		out, err = w.svc.Conversions(ctx, tx, id)
		return err
	})
	return out
}

// seed creates the fixture items MS-ANGLE-50 (kg) and CABLE-4C (m, Drum 500)
// through the service, with default tax code GST18 (00-fixtures, M9).
func (w *world) seed(t *testing.T) (angle, cable item.Item) {
	t.Helper()
	ctx := context.Background()
	kgs, mtr, drum, gst18 := w.uomID(t, "KGS"), w.uomID(t, "MTR"), w.uomID(t, "DRUM"), w.taxID(t, "GST18")
	if _, err := w.svc.Create(ctx, w.h.Env.Kavya, item.Input{
		Code: "MS-ANGLE-50", Name: "MS Angle 50x50x5", Type: item.TypeGoods, HSNSAC: "7216",
		StockUoMID: kgs, DefaultTaxCodeID: gst18, IsStock: true,
	}); err != nil {
		t.Fatalf("create MS-ANGLE-50: %v", err)
	}
	if _, err := w.svc.Create(ctx, w.h.Env.Kavya, item.Input{
		Code: "CABLE-4C", Name: "Armoured Cable 4 core 16 sq mm", Type: item.TypeGoods, HSNSAC: "8544",
		StockUoMID: mtr, DefaultTaxCodeID: gst18, IsStock: true,
		Conversions: []item.Conversion{{UoMID: mtr, Factor: uom.FactorOne}, {UoMID: drum, Factor: 500 * uom.FactorOne}},
	}); err != nil {
		t.Fatalf("create CABLE-4C: %v", err)
	}
	return w.mustGet(t, "MS-ANGLE-50"), w.mustGet(t, "CABLE-4C")
}

func path(id int64, suffix string) string {
	return "/m/item/" + strconv.FormatInt(id, 10) + suffix
}

func ver(v int) string { return strconv.Itoa(v) }

// boltForm is a valid form for fixture item BOLT-M12 (Nos, Box of 100).
func (w *world) boltForm(t *testing.T, name string) url.Values {
	t.Helper()
	nos, box := w.uomID(t, "NOS"), w.uomID(t, "BOX")
	return url.Values{
		"code":                {"BOLT-M12"},
		"name":                {name},
		"item_type":           {"Goods"},
		"hsn_sac":             {"7318"},
		"stock_uom_id":        {strconv.FormatInt(nos, 10)},
		"purchase_uom_id":     {strconv.FormatInt(box, 10)},
		"default_tax_code_id": {strconv.FormatInt(w.taxID(t, "GST18"), 10)},
		"lines[1].uom_id":     {strconv.FormatInt(nos, 10)},
		"lines[1].factor":     {"1"},
		"lines[2].uom_id":     {strconv.FormatInt(box, 10)},
		"lines[2].factor":     {"100"},
	}
}

func TestScreens_ItemCreateEditDisable(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	angle, cable := w.seed(t)

	// The list shows the seeded rows as "{Code} {Name}" (M13, CD-68), each
	// a link to its record, and "New item" for a user with create (§8.1).
	list := w.h.Get(t, kavya, "/m/item")
	webtest.WantStatus(t, list, http.StatusOK)
	for _, it := range []item.Item{angle, cable} {
		text, ok := list.RowLink(path(it.ID, ""))
		if !ok || !strings.Contains(text, it.Code+" "+it.Name) {
			t.Errorf("list row %s = %q (found %v); want a link containing %q", path(it.ID, ""), text, ok, it.Code+" "+it.Name)
		}
	}
	if _, ok := list.RowLink("/m/item/new"); !ok {
		t.Error(`list has no link to /m/item/new for Kavya (create right)`)
	}

	// The new form: Select options are the stored values with their labels
	// (CD-42, CD-175), MaxLen becomes maxlength (CD-42).
	blank := w.h.Get(t, kavya, "/m/item/new")
	webtest.WantStatus(t, blank, http.StatusOK)
	wantChoices := []webtest.Choice{{Value: "Goods", Label: "Goods"}, {Value: "Service", Label: "Service"}}
	if got := blank.Choices("item_type"); !slices.Equal(got, wantChoices) {
		t.Errorf("item_type choices = %v; want %v", got, wantChoices)
	}
	for _, name := range []string{"code", "name", "item_type", "hsn_sac", "stock_uom_id", "purchase_uom_id", "default_tax_code_id"} {
		if !blank.HasControl(name) {
			t.Errorf("new item form has no control named %q", name)
		}
	}
	for name, want := range map[string]string{"code": "20", "hsn_sac": "8"} {
		if got, _ := blank.Doc.Find(`input[name="` + name + `"]`).Attr("maxlength"); got != want {
			t.Errorf("input %s maxlength = %q; want %q", name, got, want)
		}
	}

	// An invalid HSN: 422, MST-071's message next to the HSN field, the typed
	// values kept, nothing saved.
	bad := w.boltForm(t, "Hex Bolt M12x60")
	bad.Set("hsn_sac", "72142")
	resp := w.h.Post(t, kavya, "/m/item/new", bad)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, "hsn_sac", msgHSNLength)
	if v, _ := resp.Value("code"); v != "BOLT-M12" {
		t.Errorf("422 page code value = %q; want the typed BOLT-M12", v)
	}
	if _, ok := w.byCode(t, "BOLT-M12"); ok {
		t.Fatal("BOLT-M12 saved despite the 422")
	}

	// A conversion row with factor 0: 422 with MST-081's row message next to
	// that row's factor (CD-170 "Row N: " prefix).
	bad = w.boltForm(t, "Hex Bolt M12x60")
	bad.Set("lines[2].factor", "0")
	resp = w.h.Post(t, kavya, "/m/item/new", bad)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, "lines[2].factor", msgRow2Zero)
	if _, ok := w.byCode(t, "BOLT-M12"); ok {
		t.Fatal("BOLT-M12 saved despite the 422")
	}

	// A valid create: 303 to the record; header and conversion rows stored.
	resp = w.h.Post(t, kavya, "/m/item/new", w.boltForm(t, "Hex Bolt M12x60"))
	bolt, ok := w.byCode(t, "BOLT-M12")
	if !ok {
		t.Fatalf("valid POST /m/item/new saved nothing: %d %s", resp.Status, webtest.Squash(resp.Body))
	}
	webtest.WantRedirect(t, resp, path(bolt.ID, ""))
	nos, box := w.uomID(t, "NOS"), w.uomID(t, "BOX")
	if bolt.Name != "Hex Bolt M12x60" || bolt.Type != item.TypeGoods || bolt.HSNSAC != "7318" ||
		bolt.StockUoMID != nos || bolt.PurchaseUoMID != box || bolt.DefaultTaxCodeID != w.taxID(t, "GST18") {
		t.Errorf("stored BOLT-M12 = %+v", bolt)
	}
	wantConv := []item.Conversion{{UoMID: nos, Factor: uom.FactorOne}, {UoMID: box, Factor: 100 * uom.FactorOne}}
	if got := w.conversions(t, bolt.ID); !slices.Equal(got, wantConv) {
		t.Errorf("BOLT-M12 conversions = %v; want %v", got, wantConv)
	}

	// The record page posts the version it shows (CD-94), shows linked
	// masters as "{Code} {Name}" (CD-68) and the conversion rows in the grid.
	page := w.h.Get(t, kavya, path(bolt.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	if v, _ := page.Value("version"); v != ver(bolt.Version) {
		t.Errorf("record page version = %q; want %d", v, bolt.Version)
	}
	if v, _ := page.Value("name"); v != "Hex Bolt M12x60" {
		t.Errorf("record page name = %q; want Hex Bolt M12x60", v)
	}
	webtest.WantText(t, page, "GST18 GST 18%")
	for _, name := range []string{"lines[1].uom_id", "lines[1].factor", "lines[2].uom_id", "lines[2].factor"} {
		if !page.HasControl(name) {
			t.Errorf("record page has no grid control %q", name)
		}
	}

	// Edit with the page's version: 303 back to the record, saved, version up.
	edit := w.boltForm(t, "Hex Bolt M12 x 60 mm")
	edit.Set("version", ver(bolt.Version))
	resp = w.h.Post(t, kavya, path(bolt.ID, ""), edit)
	webtest.WantRedirect(t, resp, path(bolt.ID, ""))
	after := w.mustGet(t, "BOLT-M12")
	if after.Name != "Hex Bolt M12 x 60 mm" || after.Version != bolt.Version+1 {
		t.Fatalf("after edit: name %q version %d; want Hex Bolt M12 x 60 mm, %d", after.Name, after.Version, bolt.Version+1)
	}

	// A stale version: 409 with DOC's stale message, typed values kept,
	// nothing overwritten.
	stale := w.boltForm(t, "Stale edit")
	stale.Set("version", ver(bolt.Version))
	resp = w.h.Post(t, kavya, path(bolt.ID, ""), stale)
	webtest.WantStatus(t, resp, http.StatusConflict)
	webtest.WantText(t, resp, staleBolt)
	if v, _ := resp.Value("name"); v != "Stale edit" {
		t.Errorf("409 page name value = %q; want the typed Stale edit", v)
	}
	if got := w.mustGet(t, "BOLT-M12"); got.Name != "Hex Bolt M12 x 60 mm" {
		t.Errorf("stale save overwrote the name: %q", got.Name)
	}

	// Disable: 303 to the record; hidden from the default list, shown as
	// "Disabled" with include_disabled=true (MST-009) and on its page.
	resp = w.h.Post(t, kavya, path(bolt.ID, "/disable"), url.Values{"version": {ver(after.Version)}})
	webtest.WantRedirect(t, resp, path(bolt.ID, ""))
	disabled := w.mustGet(t, "BOLT-M12")
	if !disabled.Disabled {
		t.Fatal("BOLT-M12 not disabled after POST /disable")
	}
	if _, ok := w.h.Get(t, kavya, "/m/item").RowLink(path(bolt.ID, "")); ok {
		t.Error("disabled BOLT-M12 still in the default list")
	}
	text, ok := w.h.Get(t, kavya, "/m/item?include_disabled=true").RowLink(path(bolt.ID, ""))
	if !ok || !strings.Contains(text, "Disabled") {
		t.Errorf("include_disabled list row = %q (found %v); want it tagged Disabled", text, ok)
	}
	webtest.WantText(t, w.h.Get(t, kavya, path(bolt.ID, "")), "Disabled")

	// Enable brings it back.
	resp = w.h.Post(t, kavya, path(bolt.ID, "/enable"), url.Values{"version": {ver(disabled.Version)}})
	webtest.WantRedirect(t, resp, path(bolt.ID, ""))
	if w.mustGet(t, "BOLT-M12").Disabled {
		t.Fatal("BOLT-M12 still disabled after POST /enable")
	}
	text, ok = w.h.Get(t, kavya, "/m/item").RowLink(path(bolt.ID, ""))
	if !ok || strings.Contains(text, "Disabled") {
		t.Errorf("after enable the list row = %q (found %v); want it listed, not Disabled", text, ok)
	}
}

func TestScreens_ItemReadOnlyForbidden(t *testing.T) {
	w := newWorld(t)
	priya := w.h.Env.Priya
	angle, _ := w.seed(t)
	deny := func(r auth.Right) string { return (&auth.DeniedError{Right: r, Label: "Item"}).Error() }

	// Priya (read only) sees the list, without "New item".
	list := w.h.Get(t, priya, "/m/item")
	webtest.WantStatus(t, list, http.StatusOK)
	if _, ok := list.RowLink(path(angle.ID, "")); !ok {
		t.Error("Priya's list lacks MS-ANGLE-50")
	}
	if _, ok := list.RowLink("/m/item/new"); ok {
		t.Error("Priya's list links to /m/item/new without the create right")
	}

	// Every write is 403 with AUTH's message and changes nothing.
	resp := w.h.Post(t, priya, "/m/item/new", w.boltForm(t, "Hex Bolt M12x60"))
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightCreate))
	if _, ok := w.byCode(t, "BOLT-M12"); ok {
		t.Error("Priya's create saved BOLT-M12")
	}

	edit := url.Values{
		"version": {ver(angle.Version)}, "code": {"MS-ANGLE-50"}, "name": {"Renamed by Priya"}, "item_type": {"Goods"},
		"hsn_sac": {"7216"}, "stock_uom_id": {strconv.FormatInt(angle.StockUoMID, 10)},
		"lines[1].uom_id": {strconv.FormatInt(angle.StockUoMID, 10)}, "lines[1].factor": {"1"},
	}
	resp = w.h.Post(t, priya, path(angle.ID, ""), edit)
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightWrite))

	resp = w.h.Post(t, priya, path(angle.ID, "/disable"), url.Values{"version": {ver(angle.Version)}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightWrite))

	resp = w.h.Post(t, priya, path(angle.ID, "/delete"), url.Values{"version": {ver(angle.Version)}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightDelete))

	got := w.mustGet(t, "MS-ANGLE-50")
	if got.Name != angle.Name || got.Disabled || got.Version != angle.Version {
		t.Errorf("after Priya's refused writes MS-ANGLE-50 = %+v; want unchanged %+v", got, angle)
	}
}

func TestScreens_ItemPicker(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	angle, cable := w.seed(t)

	// Matching enabled rows as "{Code} {Name}" (CD-68), each posting its id.
	rows := w.h.GetHTMX(t, kavya, "/m/item/pick?q=Angle")
	webtest.WantStatus(t, rows, http.StatusOK)
	got := rows.PickRows()
	if len(got) != 1 || got[0].Value != strconv.FormatInt(angle.ID, 10) || !strings.Contains(got[0].Text, "MS-ANGLE-50 MS Angle 50x50x5") {
		t.Errorf("pick?q=Angle = %v; want one row %d \"MS-ANGLE-50 MS Angle 50x50x5\"", got, angle.ID)
	}
	got = w.h.GetHTMX(t, kavya, "/m/item/pick?q=Cable").PickRows()
	if len(got) != 1 || got[0].Value != strconv.FormatInt(cable.ID, 10) || !strings.Contains(got[0].Text, "CABLE-4C Armoured Cable 4 core 16 sq mm") {
		t.Errorf("pick?q=Cable = %v; want one row %d CABLE-4C", got, cable.ID)
	}

	// A disabled item is hidden from the picker (MST-009).
	if err := w.svc.SetDisabled(context.Background(), kavya, cable.ID, cable.Version, true); err != nil {
		t.Fatal(err)
	}
	if got := w.h.GetHTMX(t, kavya, "/m/item/pick?q=Cable").PickRows(); len(got) != 0 {
		t.Errorf("pick?q=Cable after disabling CABLE-4C = %v; want no rows", got)
	}
}

func TestScreens_ItemDelete(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	angle, _ := w.seed(t)

	// An unused item is deleted: 303 to the list, then its page is 404.
	resp := w.h.Post(t, kavya, path(angle.ID, "/delete"), url.Values{"version": {ver(angle.Version)}})
	webtest.WantRedirect(t, resp, "/m/item")
	if _, ok := w.byCode(t, "MS-ANGLE-50"); ok {
		t.Fatal("MS-ANGLE-50 still exists after POST /delete")
	}
	var err error
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		_, err = w.svc.Get(ctx, tx, angle.ID)
		return nil
	})
	if !errors.Is(err, doctype.ErrNotFound) {
		t.Fatalf("Get after delete = %v; want doctype.ErrNotFound", err)
	}
	page := w.h.Get(t, kavya, path(angle.ID, ""))
	webtest.WantStatus(t, page, http.StatusNotFound)
	webtest.WantText(t, page, doctype.ErrNotFound.Error()) // CD-76
}
