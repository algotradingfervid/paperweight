package mstweb_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// No case ID: what the generic screens do for a master that uses them. Each
// test goes through HTTP, so deleting the call site fails it.

// nullableDocType lists a nullable Link: item rows, under a test DocType,
// because no real master lists a column that can be NULL. noSortDocType has
// no name field, so RegisterMaster leaves its DefaultSort empty. Registered
// before msttest grants rights.
const (
	nullableDocType = "mstweb.Nullable"
	noSortDocType   = "mstweb.NoSort"
)

func init() {
	mstkit.RegisterMaster(doctype.DocType{
		Name: nullableDocType, Label: "Stub", PluralLabel: "Stubs", Table: "item",
		SearchFields: []string{"code", "name"},
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, ListPriority: 4},
			{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, ListPriority: 1},
			{Name: "default_tax_code_id", Label: "Default tax code", Column: "default_tax_code_id", Type: doctype.Link, Link: taxcode.DocType, ListPriority: 5},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check, ListPriority: 3},
		},
	}, mstkit.LookupFunc(func(context.Context, *db.Tx, int64) (doctype.MasterInfo, bool, error) {
		return doctype.MasterInfo{}, false, nil
	}))
}

func init() {
	mstkit.RegisterMaster(doctype.DocType{
		Name: noSortDocType, Label: "Stub", PluralLabel: "Stubs", Table: "item",
		SearchFields: []string{"code"},
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, ListPriority: 1},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check, ListPriority: 3},
		},
	}, mstkit.LookupFunc(func(context.Context, *db.Tx, int64) (doctype.MasterInfo, bool, error) {
		return doctype.MasterInfo{}, false, nil
	}))
}

func deny(r auth.Right) string {
	return (&auth.DeniedError{Right: r, Label: "Item"}).Error()
}

func badRate(t *testing.T) string {
	t.Helper()
	_, err := types.ParseRate("abc")
	if err == nil {
		t.Fatal(`ParseRate("abc") = nil`)
	}
	return err.Error()
}

// rateInput is a master's ToInput for a rate field: NUM's message on the
// field, and the field left at its zero.
func rateInput(v map[string]string) (stubRecord, []doctype.FieldError) {
	if _, err := types.ParseRate(v["standard_rate"]); err != nil {
		return stubRecord{}, []doctype.FieldError{{Field: "standard_rate", Message: err.Error()}}
	}
	return v, nil
}

// A value that doesn't parse is NUM's message on its field and the typed text
// kept, with the other fields' errors beside it, and the service is never
// called: its input would hold a zero it could save (CD-37).
func TestScreens_ParseErrorKeepsTheServiceOut(t *testing.T) {
	calls := 0
	h := harness(t, stubService{rec: stubRecord{}, calls: &calls}, func(s *screen) {
		s.ToInput = rateInput
		s.Validate = func(stubRecord) (stubRecord, error) {
			return nil, &mstkit.ValidationError{Fields: []doctype.FieldError{
				{Field: "name", Message: "Name is required."},
				{Field: "standard_rate", Message: "Rate went missing."},
			}}
		}
	})
	for _, target := range []string{base + "/new", record} {
		resp := h.Post(t, h.Env.Kavya, target, url.Values{"version": {"3"}, "name": {""}, "standard_rate": {"abc"}})
		webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
		webtest.WantFieldError(t, resp, "standard_rate", badRate(t))
		webtest.WantFieldError(t, resp, "name", "Name is required.")
		webtest.WantNoText(t, resp, "Rate went missing.")
		if v, _ := resp.Value("standard_rate"); v != "abc" {
			t.Errorf("POST %s: rate value = %q; want the typed abc", target, v)
		}
	}
	if calls != 0 {
		t.Errorf("service called %d times on parse errors; want none", calls)
	}
}

// Without Validate the parse errors show alone.
func TestScreens_ParseErrorWithoutValidate(t *testing.T) {
	h := harness(t, stubService{}, func(s *screen) { s.ToInput = rateInput })
	resp := h.Post(t, h.Env.Kavya, base+"/new", url.Values{"standard_rate": {"abc"}})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, "standard_rate", badRate(t))
}

// A 403 beats a 422: the right the service would check comes first, with
// AUTH's message, on create and on save.
func TestScreens_ParseErrorForbidden(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{}}, func(s *screen) { s.ToInput = rateInput })
	form := url.Values{"version": {"3"}, "standard_rate": {"abc"}}
	for _, p := range []auth.Principal{h.Env.Priya, h.Env.Ravi} {
		resp := h.Post(t, p, base+"/new", form)
		webtest.WantStatus(t, resp, http.StatusForbidden)
		webtest.WantText(t, resp, deny(auth.RightCreate))
		resp = h.Post(t, p, record, form)
		webtest.WantStatus(t, resp, http.StatusForbidden)
		webtest.WantText(t, resp, deny(auth.RightWrite))
	}
}

// The save's right is checked on the record: a restricted user's refusal
// comes from the RecordRef, which a kind-level check would miss.
func TestScreens_ParseErrorChecksTheRecord(t *testing.T) {
	refs := 0
	h := harness(t, stubService{rec: stubRecord{}}, func(s *screen) {
		s.ToInput = rateInput
		s.Ref = func(id int64) *auth.RecordRef {
			if id == 7 {
				refs++
			}
			return &auth.RecordRef{DocType: item.DocType, ID: id}
		}
	})
	resp := h.Post(t, h.Env.Priya, record, url.Values{"version": {"3"}, "standard_rate": {"abc"}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	if refs != 1 {
		t.Errorf("Ref(7) called %d times on a refused save; want 1", refs)
	}
}

// Line fields are not in the form, and not in the read-only view.
func TestScreens_OnLineFieldsAreNotScalars(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{"name": "Stub"}})
	blank := h.Get(t, h.Env.Kavya, base+"/new")
	webtest.WantStatus(t, blank, http.StatusOK)
	if !blank.HasControl("name") {
		t.Error("form has no name control")
	}
	for _, name := range []string{"uom_id", "factor"} {
		if blank.HasControl(name) {
			t.Errorf("form has a scalar control for line field %q", name)
		}
	}
	webtest.WantNoText(t, h.Get(t, h.Env.Priya, record), "Conversion factor")
}

// Posted rows reach ToInput in the same flat map, only for OnLine fields and
// numbered 1..n, and a refused save keeps them, in row order. Stored rows
// render too.
func TestScreens_RowsStayFlat(t *testing.T) {
	var got map[string]string
	h := harness(t, stubService{
		rec: stubRecord{"lines[1].factor": "1", "lines[2].factor": "100"},
		err: &mstkit.ValidationError{Fields: []doctype.FieldError{{Field: "name", Message: "Name is wrong."}}},
	}, func(s *screen) {
		s.ToInput = func(v map[string]string) (stubRecord, []doctype.FieldError) {
			got = v
			return v, nil
		}
	})
	resp := h.Post(t, h.Env.Kavya, base+"/new", url.Values{
		"name":             {"Typed"},
		"lines[10].factor": {"5"},
		"lines[2].uom_id":  {"8"},
		"lines[2].factor":  {"100"},
		"lines[1].bogus":   {"x"},
		"lines[1].name":    {"x"},
		"lines[x].factor":  {"x"},
		"lines[0].factor":  {"x"},
		"lines[+1].factor": {"x"},
		"lines[01].factor": {"x"},
		"lines[3]factor":   {"x"},
		"line[1].factor":   {"x"},
	})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	rows := map[string]string{}
	for key, value := range got {
		if strings.HasPrefix(key, "line") {
			rows[key] = value
		}
	}
	want := map[string]string{"lines[1].uom_id": "8", "lines[1].factor": "100", "lines[2].uom_id": "", "lines[2].factor": "5"}
	if !reflect.DeepEqual(rows, want) {
		t.Errorf("ToInput rows = %v; want %v", rows, want)
	}
	if names := rowInputs(resp); !reflect.DeepEqual(names, []string{"lines[1].uom_id=8", "lines[1].factor=100", "lines[2].uom_id=", "lines[2].factor=5"}) {
		t.Errorf("rows on the 422 page = %q", names)
	}

	page := h.Get(t, h.Env.Kavya, record)
	if names := rowInputs(page); !reflect.DeepEqual(names, []string{"lines[1].uom_id=", "lines[1].factor=1", "lines[2].uom_id=", "lines[2].factor=100"}) {
		t.Errorf("stored rows = %q", names)
	}
}

func rowInputs(r webtest.Response) []string {
	var out []string
	r.Doc.Find(`form input[name^="lines["]:not([type="search"]):not([role="combobox"])`).Each(func(_ int, s *goquery.Selection) {
		name, _ := s.Attr("name")
		value, _ := s.Attr("value")
		out = append(out, name+"="+value)
	})
	return out
}

// seedItem creates item code through the real service, with a rate.
func seedItem(t *testing.T, h *webtest.Harness, code string) int64 {
	t.Helper()
	return seedRated(t, h, code, "12.50")
}

func seedRated(t *testing.T, h *webtest.Harness, code, typedRate string) int64 {
	t.Helper()
	ctx := context.Background()
	var nos uom.UoM
	var ok bool
	if err := h.Env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		nos, ok, err = uom.New(h.Env.Kit).ByCode(ctx, tx, "NOS")
		return err
	}); err != nil || !ok {
		t.Fatalf("seeded NOS: %v (found %v)", err, ok)
	}
	rate, err := types.ParseRate(typedRate)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := item.New(h.Env.Kit).Create(ctx, h.Env.Kavya, item.Input{
		Code: code, Name: "Bolt " + code, Type: item.TypeGoods, HSNSAC: "7318", StockUoMID: nos.ID, StandardRate: rate,
	})
	if err != nil {
		t.Fatal(err)
	}
	return saved.ID
}

func stubPath(id int64) string { return base + "/" + strconv.FormatInt(id, 10) }

// Every declared list column is filled from the row's typed value: the title
// "{Code} {Name}" (CD-68), the rate through NUM's formatter, a Select, and a
// Link as "{Code} {Name}".
func TestScreens_ListFillsEveryColumn(t *testing.T) {
	h := harness(t, stubService{})
	id := seedItem(t, h, "M12")
	text, ok := h.Get(t, h.Env.Kavya, base).RowLink(stubPath(id))
	if !ok {
		t.Fatal("list has no row for the item")
	}
	for _, want := range []string{"M12 Bolt M12", "₹12.50", "Goods", "NOS Numbers"} {
		if !strings.Contains(text, want) {
			t.Errorf("row = %q; want it to contain %q", text, want)
		}
	}
}

// A ZeroIsNone column shows a stored zero as empty; a real rate stays, and
// without the option a zero is a value, as Tax Code's 0% is. The export keeps
// its 0.00.
func TestScreens_ZeroIsNone(t *testing.T) {
	h := harness(t, stubService{}, func(s *screen) { s.ZeroIsNone = map[string]bool{"standard_rate": true} })
	none, rated := seedRated(t, h, "NONE", "0"), seedRated(t, h, "RATED", "12.50")
	list := h.Get(t, h.Env.Kavya, base)
	if text, ok := list.RowLink(stubPath(none)); !ok || strings.Contains(text, "₹") {
		t.Errorf("zero rate row = %q (found %v); want no rate", text, ok)
	}
	if text, _ := list.RowLink(stubPath(rated)); !strings.Contains(text, "₹12.50") {
		t.Errorf("rated row = %q; want ₹12.50", text)
	}
	if file := h.Get(t, h.Env.Kavya, base+"/export.csv"); !strings.Contains(file.Body, "0.00") {
		t.Errorf("export = %q; want the zero rate as 0.00", file.Body)
	}

	h = harness(t, stubService{})
	id := seedRated(t, h, "NONE", "0")
	if text, _ := h.Get(t, h.Env.Kavya, base).RowLink(stubPath(id)); !strings.Contains(text, "₹0.00") {
		t.Errorf("row without ZeroIsNone = %q; want ₹0.00", text)
	}
}

// A DocType with no DefaultSort lists, with nothing preselected.
func TestScreens_ListWithoutDefaultSort(t *testing.T) {
	h := harness(t, stubService{}, func(s *screen) { s.DocType = noSortDocType })
	resp := h.Get(t, h.Env.Kavya, base)
	webtest.WantStatus(t, resp, http.StatusOK)
	if v, _ := resp.Value("sort"); v != "" {
		t.Errorf("selected sort = %q; want none", v)
	}
}

// A blank Noun or One would print broken sentences on a live page, and a
// DocType with rows has no editor without Rows and AddRow, so a Screen without
// them refuses to mount.
func TestScreens_CopyMustBeComplete(t *testing.T) {
	for _, tc := range []struct {
		name  string
		blank func(*mstweb.Copy)
	}{
		{"Noun", func(c *mstweb.Copy) { c.Noun = "" }},
		{"One", func(c *mstweb.Copy) { c.One = "" }},
		{"Rows", func(c *mstweb.Copy) { c.Rows = "" }},
		{"AddRow", func(c *mstweb.Copy) { c.AddRow = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("Routes with a blank Copy.%s did not panic", tc.name)
				}
			}()
			c := mstweb.Copy{Noun: "stubs", One: "stub", Rows: "Conversions", AddRow: "Add conversion"}
			tc.blank(&c)
			screen{DocType: item.DocType, Copy: c}.Routes()
		})
	}
}

// A NULL column is an empty cell, not a failed page or "<nil>".
func TestScreens_ListNullCell(t *testing.T) {
	h := harness(t, stubService{}, func(s *screen) {
		s.DocType = nullableDocType
		s.ZeroIsNone = map[string]bool{"default_tax_code_id": true}
	})
	id := seedItem(t, h, "M12")
	resp := h.Get(t, h.Env.Kavya, base)
	webtest.WantStatus(t, resp, http.StatusOK)
	if text, ok := resp.RowLink(stubPath(id)); !ok || strings.Contains(text, "nil") {
		t.Errorf("row = %q (found %v); want an empty tax code", text, ok)
	}
}

// Codes fields render through CodeInput, with what FieldInput would have
// given them; other fields stay as they were.
func TestScreens_CodesUseCodeInput(t *testing.T) {
	h := harness(t, stubService{
		rec: stubRecord{"hsn_sac": "7318"},
		err: &mstkit.ValidationError{Fields: []doctype.FieldError{{Field: "hsn_sac", Message: "HSN is wrong."}}},
	}, func(s *screen) {
		s.Codes = map[string]components.CodeKind{"hsn_sac": components.HSN, "code": components.PAN}
	})
	page := h.Get(t, h.Env.Kavya, record)
	webtest.WantStatus(t, page, http.StatusOK)
	in := page.Doc.Find(`input[name="hsn_sac"]`)
	if url, _ := in.Attr("hx-post"); url != "/ui/validate/hsn" {
		t.Errorf("hsn_sac hx-post = %q; want /ui/validate/hsn", url)
	}
	if v, _ := in.Attr("value"); v != "7318" {
		t.Errorf("hsn_sac value = %q; want the stored 7318", v)
	}
	if id, _ := in.Attr("id"); id != "f-hsn_sac" {
		t.Errorf("hsn_sac id = %q; want f-hsn_sac", id)
	}
	if label := webtest.Squash(page.Doc.Find(`label[for="f-hsn_sac"]`).Text()); label != "HSN/SAC (optional)" {
		t.Errorf("hsn_sac label = %q; want HSN/SAC (optional)", label)
	}
	if label := webtest.Squash(page.Doc.Find(`label[for="f-code"]`).Text()); label != "Code" {
		t.Errorf("required code label = %q; want Code", label)
	}
	if page.Doc.Find(`input[name="name"][hx-post]`).Length() != 0 {
		t.Error("name, not a Codes field, became a CodeInput")
	}
	if got, _ := in.Attr("maxlength"); got != "8" {
		t.Errorf("hsn_sac maxlength = %q; want the field's 8", got)
	}
	webtest.WantFieldError(t, h.Post(t, h.Env.Kavya, base+"/new", url.Values{"hsn_sac": {"1"}}), "hsn_sac", "HSN is wrong.")
}

// An id out of int64's range is no record.
func TestScreens_HugeIDIsNotFound(t *testing.T) {
	h := harness(t, stubService{getErr: doctype.ErrNotFound})
	webtest.WantStatus(t, h.Get(t, h.Env.Kavya, base+"/99999999999999999999"), http.StatusNotFound)
}

// The posted version stays on the page after a refusal, in the form and in
// More actions, though the row has moved on to 3 (CD-94).
func TestScreens_RefusalKeepsThePostedVersion(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		toIn   func(map[string]string) (stubRecord, []doctype.FieldError)
		status int
	}{
		{name: "stale", err: &refusal{text: "Stale.", status: http.StatusConflict}, status: http.StatusConflict},
		{name: "service 422", err: &mstkit.ValidationError{Fields: []doctype.FieldError{{Field: "name", Message: "Bad."}}}, status: http.StatusUnprocessableEntity},
		{name: "parse 422", toIn: rateInput, status: http.StatusUnprocessableEntity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := harness(t, stubService{rec: stubRecord{}, err: tc.err}, func(s *screen) {
				if tc.toIn != nil {
					s.ToInput = tc.toIn
				}
			})
			resp := h.Post(t, h.Env.Kavya, record, url.Values{"version": {"2"}, "standard_rate": {"abc"}})
			webtest.WantStatus(t, resp, tc.status)
			for _, action := range []string{record, record + "/disable"} {
				if v, ok := resp.FormValue(action, "version"); !ok || v != "2" {
					t.Errorf("version in the form posting to %s = %q (found %v); want the posted 2", action, v, ok)
				}
			}
		})
	}
}

func TestScreens_NoNew(t *testing.T) {
	h := harness(t, stubService{}, func(s *screen) { s.NoNew = true })
	webtest.WantStatus(t, h.Get(t, h.Env.Kavya, base+"/new"), http.StatusNotFound)
	webtest.WantStatus(t, h.Post(t, h.Env.Kavya, base+"/new", url.Values{}), http.StatusNotFound)
	list := h.Get(t, h.Env.Kavya, base)
	webtest.WantStatus(t, list, http.StatusOK)
	if _, ok := list.RowLink(base + "/new"); ok {
		t.Error("list links to the unmounted /new")
	}

	h = harness(t, stubService{})
	if _, ok := h.Get(t, h.Env.Kavya, base).RowLink(base + "/new"); !ok {
		t.Error("without NoNew the list has no New link")
	}
}

func TestScreens_NoDisable(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{}}, func(s *screen) { s.NoDisable = true })
	for _, suffix := range []string{"/disable", "/enable"} {
		webtest.WantStatus(t, h.Post(t, h.Env.Kavya, record+suffix, url.Values{"version": {"3"}}), http.StatusNotFound)
	}
	page := h.Get(t, h.Env.Kavya, record)
	webtest.WantStatus(t, page, http.StatusOK)
	webtest.WantNoText(t, page, "Disable")
	webtest.WantText(t, page, "Delete")
	webtest.WantNoText(t, h.Get(t, h.Env.Kavya, base), "Show disabled")
}

func TestScreens_NoDelete(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{}}, func(s *screen) { s.NoDelete = true })
	webtest.WantStatus(t, h.Post(t, h.Env.Kavya, record+"/delete", url.Values{"version": {"3"}}), http.StatusNotFound)
	page := h.Get(t, h.Env.Kavya, record)
	webtest.WantStatus(t, page, http.StatusOK)
	webtest.WantNoText(t, page, "Delete")
	webtest.WantText(t, page, "Disable")
	if confirm := h.Get(t, h.Env.Kavya, record+"?delete"); confirm.Doc.Find(`form[action="`+record+`/delete"]`).Length() != 0 {
		t.Error("?delete shows the confirm step of an unmounted route")
	}
}

// Extra renders on the record page, for readers too, and not on the new page
// (there is no record) or the delete confirm step.
func TestScreens_Extra(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{"name": "Stored"}}, func(s *screen) {
		s.Extra = func(r stubRecord) templ.Component {
			return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
				_, err := io.WriteString(w, `<p id="extra">`+r["name"]+`</p>`)
				return err
			})
		}
	})
	for _, p := range []auth.Principal{h.Env.Kavya, h.Env.Priya} {
		if got := h.Get(t, p, record).Doc.Find("#extra").Text(); got != "Stored" {
			t.Errorf("Extra on the record page = %q; want Stored", got)
		}
	}
	for _, target := range []string{base + "/new", record + "?delete"} {
		if h.Get(t, h.Env.Kavya, target).Doc.Find("#extra").Length() != 0 {
			t.Errorf("GET %s shows Extra", target)
		}
	}
}

// Rows and AddRow are only for a DocType with rows.
func TestScreens_RowCopyIsOptionalWithoutRows(t *testing.T) {
	screen{DocType: noSortDocType, Copy: mstweb.Copy{Noun: "stubs", One: "stub"}}.Routes()
}

// With nothing at all, every view says "yet"; include_disabled changes nothing
// and there is no search to try again.
func TestScreens_EmptyWithoutSearch(t *testing.T) {
	h := harness(t, stubService{})
	for _, target := range []string{base, base + "?include_disabled=true"} {
		resp := h.Get(t, h.Env.Kavya, target)
		webtest.WantStatus(t, resp, http.StatusOK)
		webtest.WantText(t, resp, "No stubs yet.")
		webtest.WantNoText(t, resp, "Try a different search")
	}
}

// When every record is disabled, "yet" is untrue: the list says so and still
// offers them.
func TestScreens_EmptyWhenAllDisabled(t *testing.T) {
	h := harness(t, stubService{})
	id := seedItem(t, h, "M12")
	if err := item.New(h.Env.Kit).SetDisabled(context.Background(), h.Env.Kavya, id, 1, true); err != nil {
		t.Fatal(err)
	}
	resp := h.Get(t, h.Env.Kavya, base)
	webtest.WantStatus(t, resp, http.StatusOK)
	webtest.WantText(t, resp, "All stubs are disabled.")
	webtest.WantNoText(t, resp, "No stubs yet.")
	webtest.WantNoText(t, resp, "Try a different search")
	if _, ok := resp.RowLink(base + "?include_disabled=true"); !ok {
		t.Error("no Show disabled link on a list of disabled records")
	}
}

// A page past the end has records: only Pagination speaks, and the way back
// is there.
func TestScreens_EmptyPastTheEnd(t *testing.T) {
	h := harness(t, stubService{})
	seedItem(t, h, "M12")
	resp := h.Get(t, h.Env.Kavya, base+"?page=9")
	webtest.WantStatus(t, resp, http.StatusOK)
	webtest.WantText(t, resp, "No more results.")
	webtest.WantNoText(t, resp, "No stubs yet.")
	webtest.WantNoText(t, resp, "No results.")
	webtest.WantNoText(t, resp, "are disabled.")
}

// The lower-case plural is what sentences use; Plural stays the title.
func TestScreens_CopyInSentences(t *testing.T) {
	h := harness(t, stubService{})
	seedItem(t, h, "M12")
	resp := h.Get(t, h.Env.Kavya, base)
	webtest.WantText(t, resp, "Show disabled stubs")
	if label, _ := resp.Doc.Find("nav.pagination").Attr("aria-label"); label != "Pages of stubs" {
		t.Errorf("pagination label = %q; want Pages of stubs", label)
	}
	if h1 := webtest.Squash(resp.Doc.Find("h1").Text()); h1 != "Stubs" {
		t.Errorf("h1 = %q; want Stubs", h1)
	}
}

// The count uses the singular for one and groups as NUM does; an empty list
// shows none.
func TestScreens_ResultCount(t *testing.T) {
	h := harness(t, stubService{})
	count := func() string { return webtest.Squash(h.Get(t, h.Env.Kavya, base).Doc.Find(".filterbar-count").Text()) }
	if got := count(); got != "" {
		t.Errorf("empty list count = %q; want none", got)
	}
	seedItem(t, h, "A1")
	if got := count(); got != "1 stub" {
		t.Errorf("count = %q; want 1 stub", got)
	}
	seedItem(t, h, "A2")
	if got := count(); got != "2 stubs" {
		t.Errorf("count = %q; want 2 stubs", got)
	}
}
