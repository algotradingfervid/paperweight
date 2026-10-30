package mstweb_test

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// No case ID: the generic rows editor (CD-201). Item's DocType has the Link
// and the factor; rowsDocType has a row field of each other kind.

const rowsDocType = "mstweb.Rows"

func init() {
	mstkit.RegisterMaster(doctype.DocType{
		Name: rowsDocType, Label: "Stub", PluralLabel: "Stubs", Table: "item",
		SearchFields: []string{"code"},
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, ListPriority: 4, MaxLen: 9},
			{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, ListPriority: 1},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check, ListPriority: 3},
			{Name: "note", Label: "Note", Column: "note", Type: doctype.Text, MaxLen: 12, OnLine: true},
			{Name: "side", Label: "Side", Column: "side", Type: doctype.Select, Options: []string{"Left", "Right"}, OnLine: true},
			{Name: "days", Label: "Days", Column: "days", Type: doctype.Int, OnLine: true},
			{Name: "rate", Label: "Rate", Column: "rate", Type: doctype.Rate, OnLine: true},
			{Name: "code_no", Label: "Code no", Column: "code_no", Type: doctype.Text, OnLine: true},
		},
	}, mstkit.LookupFunc(func(context.Context, *db.Tx, int64) (doctype.MasterInfo, bool, error) {
		return doctype.MasterInfo{}, false, nil
	}))
}

func onRowsDocType(s *screen) { s.DocType = rowsDocType }

// twoRows is a typed form with a scalar and two item rows.
func twoRows() url.Values {
	return url.Values{
		"version": {"2"}, "name": {"Typed"},
		"lines[1].uom_id": {"8"}, "lines[1].factor": {"10"},
		"lines[2].uom_id": {"9"}, "lines[2].factor": {"100"},
	}
}

func rowValue(t *testing.T, r webtest.Response, name string) string {
	t.Helper()
	v, ok := r.Value(name)
	if !ok {
		t.Fatalf("no control %s on the page", name)
	}
	return v
}

// Each cell comes through the scalar fields' input path, under its row's name
// and id: MaxLen, Select options, numbers and their server check all survive.
func TestScreens_RowCellsKeepTheirInputs(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{
		"lines[1].note": "first", "lines[1].side": "Right", "lines[1].days": "30", "lines[1].rate": "12.50", "lines[1].code_no": "7318",
		"lines[2].note": "second",
	}}, onRowsDocType, func(s *screen) {
		s.Codes = map[string]components.CodeKind{"code_no": components.HSN}
	})
	page := h.Get(t, h.Env.Kavya, record)
	webtest.WantStatus(t, page, http.StatusOK)

	in := page.Doc.Find(`input[name="lines[1].note"]`)
	if id, _ := in.Attr("id"); id != "f-lines-1-note" {
		t.Errorf("note id = %q; want f-lines-1-note", id)
	}
	if got, _ := in.Attr("maxlength"); got != "12" {
		t.Errorf("note maxlength = %q; want the field's 12", got)
	}
	if got := rowValue(t, page, "lines[2].note"); got != "second" {
		t.Errorf("row 2 note = %q; want second", got)
	}
	if got := page.Choices("lines[1].side"); !reflect.DeepEqual(got, []webtest.Choice{{Value: "Left", Label: "Left"}, {Value: "Right", Label: "Right"}}) {
		t.Errorf("side choices = %+v", got)
	}
	if got, ok := page.Doc.Find(`input[name="lines[1].side"][checked]`).Attr("value"); !ok || got != "Right" {
		t.Errorf("chosen side = %q (found %v); want Right", got, ok)
	}
	if got, _ := page.Doc.Find(`input[name="lines[1].days"]`).Attr("inputmode"); got != "numeric" {
		t.Errorf("days inputmode = %q; want numeric", got)
	}
	if got, _ := page.Doc.Find(`input[name="lines[1].rate"]`).Attr("hx-post"); got != "/ui/validate/rate" {
		t.Errorf("rate hx-post = %q; want /ui/validate/rate", got)
	}
	if got, _ := page.Doc.Find(`input[name="lines[1].code_no"]`).Attr("hx-post"); got != "/ui/validate/hsn" {
		t.Errorf("Codes row cell hx-post = %q; want /ui/validate/hsn", got)
	}
}

// A Link cell is the picker, showing the stored master as "{Code} {Name}"
// and posting its id under the row's name.
func TestScreens_RowCellKeepsThePicker(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{}})
	nos := nosID(t, h)
	h = harness(t, stubService{rec: stubRecord{"lines[1].uom_id": strconv.FormatInt(nos, 10), "lines[1].factor": "1"}})
	page := h.Get(t, h.Env.Kavya, record)
	webtest.WantStatus(t, page, http.StatusOK)
	if got := rowValue(t, page, "lines[1].uom_id"); got != strconv.FormatInt(nos, 10) {
		t.Errorf("uom_id = %q; want the stored %d", got, nos)
	}
	cell := page.Doc.Find(`input[name="lines[1].uom_id"]`).Closest(".rows-cell")
	if got := webtest.Squash(cell.Text()); !strings.Contains(got, "NOS Numbers") {
		t.Errorf("uom cell = %q; want NOS Numbers", got)
	}
}

func nosID(t *testing.T, h *webtest.Harness) int64 {
	t.Helper()
	ctx := context.Background()
	var nos uom.UoM
	if err := h.Env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		nos, _, err = uom.New(h.Env.Kit).ByCode(ctx, tx, "NOS")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return nos.ID
}

// The editor carries the master's words, and WideLines gives a column its
// track, in the head and in every row.
func TestScreens_RowsEditorCopyAndWide(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{"lines[1].factor": "1", "lines[2].factor": "2"}}, func(s *screen) {
		s.WideLines = map[string]bool{"factor": true}
	})
	page := h.Get(t, h.Env.Kavya, record)
	if got := webtest.Squash(page.Doc.Find("fieldset#f-lines legend").Text()); got != "Conversions" {
		t.Errorf("legend = %q; want Conversions", got)
	}
	add := page.Doc.Find(`fieldset button[name="_rows"][value="add"]`)
	if got := webtest.Squash(add.Text()); got != "Add conversion" {
		t.Errorf("add button = %q; want Add conversion", got)
	}
	for _, v := range []string{"remove:1", "remove:2"} {
		if page.Doc.Find(`fieldset button[name="_rows"][value="`+v+`"]`).Length() != 1 {
			t.Errorf("no button for %s", v)
		}
	}
	for _, name := range []string{"lines[1].factor", "lines[2].factor"} {
		if page.Doc.Find(`.rows-cell--wide input[name="`+name+`"]`).Length() != 1 {
			t.Errorf("%s is not in a wide cell", name)
		}
	}
	if page.Doc.Find(`.rows-cell--wide input[name="lines[1].uom_id"]`).Length() != 0 {
		t.Error("uom_id is in a wide cell; only WideLines' fields are")
	}
}

// A row field's error sits next to that row's input and the summary link
// reaches it; the same field on another row stays clean.
func TestScreens_RowErrorsSitOnTheirRow(t *testing.T) {
	h := harness(t, stubService{err: &mstkit.ValidationError{Fields: []doctype.FieldError{
		{Field: "factor", Line: 2, Message: "Row 2: Factor is wrong."},
	}}})
	resp := h.Post(t, h.Env.Kavya, base+"/new", twoRows())
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, "lines[2].factor", "Row 2: Factor is wrong.")
	if msg, invalid, _ := resp.FieldMessage("lines[1].factor"); msg != "" || invalid {
		t.Errorf("row 1 factor shows %q (invalid %v); want nothing", msg, invalid)
	}
	if resp.Doc.Find("input#f-lines-2-factor").Length() != 1 {
		t.Error("the summary's link target #f-lines-2-factor is no input")
	}
}

// Add redraws with what was typed and a blank row, on both forms, and the
// service hears nothing.
func TestScreens_AddRow(t *testing.T) {
	for _, target := range []string{base + "/new", record} {
		calls := 0
		h := harness(t, stubService{rec: stubRecord{"lines[1].factor": "1"}, calls: &calls})
		form := twoRows()
		form.Set("_rows", "add")
		resp := h.Post(t, h.Env.Kavya, target, form)
		webtest.WantStatus(t, resp, http.StatusOK)
		if got := rowValue(t, resp, "name"); got != "Typed" {
			t.Errorf("POST %s: name = %q; want the typed Typed", target, got)
		}
		for name, want := range map[string]string{
			"lines[1].uom_id": "8", "lines[1].factor": "10",
			"lines[2].uom_id": "9", "lines[2].factor": "100",
			"lines[3].uom_id": "", "lines[3].factor": "",
		} {
			if got := rowValue(t, resp, name); got != want {
				t.Errorf("POST %s: %s = %q; want %q", target, name, got, want)
			}
		}
		if calls != 0 {
			t.Errorf("POST %s: service called %d times; want none", target, calls)
		}
	}
}

// Add starts the first row of a form that has none.
func TestScreens_AddFirstRow(t *testing.T) {
	h := harness(t, stubService{})
	resp := h.Post(t, h.Env.Kavya, base+"/new", url.Values{"_rows": {"add"}})
	webtest.WantStatus(t, resp, http.StatusOK)
	if !resp.HasControl("lines[1].factor") {
		t.Error("no row 1 after Add")
	}
	if resp.HasControl("lines[2].factor") {
		t.Error("Add made a second row")
	}
}

// StartRows opens the new page with that many blank rows, each with every row
// field, and touches no other page; a master that doesn't set it opens with
// none.
func TestScreens_StartRows(t *testing.T) {
	cases := []struct {
		rows int
		want []string
		none []string
	}{
		{0, nil, []string{"lines[1].uom_id", "lines[1].factor"}},
		{1, []string{"lines[1].uom_id", "lines[1].factor"}, []string{"lines[2].factor"}},
		{2, []string{"lines[1].factor", "lines[2].uom_id", "lines[2].factor"}, []string{"lines[3].factor"}},
	}
	for _, c := range cases {
		h := harness(t, stubService{}, func(s *screen) { s.StartRows = c.rows })
		page := h.Get(t, h.Env.Kavya, base+"/new")
		webtest.WantStatus(t, page, http.StatusOK)
		for _, name := range c.want {
			if v := rowValue(t, page, name); v != "" {
				t.Errorf("StartRows %d: %s = %q; want blank", c.rows, name, v)
			}
		}
		for _, name := range c.none {
			if page.HasControl(name) {
				t.Errorf("StartRows %d: new page has %s", c.rows, name)
			}
		}
	}

	h := harness(t, stubService{}, func(s *screen) { s.StartRows = 2 })
	page := h.Get(t, h.Env.Kavya, record)
	webtest.WantStatus(t, page, http.StatusOK)
	if page.HasControl("lines[1].factor") {
		t.Error("a stored record with no rows gained a row")
	}
	resp := h.Post(t, h.Env.Kavya, base+"/new", url.Values{"_rows": {"add"}})
	webtest.WantStatus(t, resp, http.StatusOK)
	if resp.HasControl("lines[2].factor") {
		t.Error("a posted form gained the start rows")
	}

	// A refused Create redraws what was posted: no rows stays no rows.
	h = harness(t, stubService{err: &mstkit.ValidationError{Fields: []doctype.FieldError{{Field: "name", Message: "Name is wrong."}}}}, func(s *screen) { s.StartRows = 2 })
	resp = h.Post(t, h.Env.Kavya, base+"/new", url.Values{"name": {"Typed"}})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	if resp.HasControl("lines[1].factor") {
		t.Error("a refused create gained the start rows")
	}
}

// Remove drops its row and renumbers the rest 1..n, and the service hears
// nothing.
func TestScreens_RemoveRow(t *testing.T) {
	for _, target := range []string{base + "/new", record} {
		calls := 0
		h := harness(t, stubService{rec: stubRecord{}, calls: &calls})
		form := twoRows()
		form.Set("lines[3].uom_id", "10")
		form.Set("lines[3].factor", "1000")
		form.Set("_rows", "remove:2")
		resp := h.Post(t, h.Env.Kavya, target, form)
		webtest.WantStatus(t, resp, http.StatusOK)
		if got := rowValue(t, resp, "name"); got != "Typed" {
			t.Errorf("POST %s: name = %q; want the typed Typed", target, got)
		}
		for name, want := range map[string]string{
			"lines[1].uom_id": "8", "lines[1].factor": "10",
			"lines[2].uom_id": "10", "lines[2].factor": "1000",
		} {
			if got := rowValue(t, resp, name); got != want {
				t.Errorf("POST %s: %s = %q; want %q", target, name, got, want)
			}
		}
		if resp.HasControl("lines[3].factor") {
			t.Errorf("POST %s: row 3 is still there", target)
		}
		if calls != 0 {
			t.Errorf("POST %s: service called %d times; want none", target, calls)
		}
	}
}

// A row posted out of sequence is still a row: Remove goes by its number, and
// the rest close up.
func TestScreens_RemoveRowWithAGap(t *testing.T) {
	h := harness(t, stubService{})
	resp := h.Post(t, h.Env.Kavya, base+"/new", url.Values{
		"lines[2].factor": {"2"}, "lines[5].factor": {"5"}, "lines[9].factor": {"9"}, "_rows": {"remove:5"},
	})
	webtest.WantStatus(t, resp, http.StatusOK)
	if got := rowValue(t, resp, "lines[1].factor"); got != "2" {
		t.Errorf("row 1 = %q; want 2", got)
	}
	if got := rowValue(t, resp, "lines[2].factor"); got != "9" {
		t.Errorf("row 2 = %q; want 9", got)
	}
}

// A redraw keeps the version the page posted, in the form and More actions,
// though the row has moved on to 3 (CD-94).
func TestScreens_RowRedrawKeepsThePostedVersion(t *testing.T) {
	for _, action := range []string{"add", "remove:1"} {
		h := harness(t, stubService{rec: stubRecord{}})
		form := twoRows()
		form.Set("_rows", action)
		resp := h.Post(t, h.Env.Kavya, record, form)
		webtest.WantStatus(t, resp, http.StatusOK)
		for _, to := range []string{record, record + "/disable"} {
			if v, ok := resp.FormValue(to, "version"); !ok || v != "2" {
				t.Errorf("%s: version in the form posting to %s = %q (found %v); want the posted 2", action, to, v, ok)
			}
		}
	}
}

// A row action without its version is the same 400 as a save without one.
func TestScreens_RowActionNeedsTheVersion(t *testing.T) {
	calls := 0
	h := harness(t, stubService{rec: stubRecord{}, calls: &calls})
	webtest.WantStatus(t, h.Post(t, h.Env.Kavya, record, url.Values{"_rows": {"add"}}), http.StatusBadRequest)
	if calls != 0 {
		t.Errorf("service called %d times; want none", calls)
	}
}

// A row that isn't there, or an action that doesn't exist, is a bad form:
// never a save.
func TestScreens_BadRowActionIsRefused(t *testing.T) {
	for _, action := range []string{"remove:3", "remove:0", "remove:-1", "remove:", "remove:x", "remove", "bogus", "2"} {
		for _, target := range []string{base + "/new", record} {
			calls := 0
			h := harness(t, stubService{rec: stubRecord{}, calls: &calls})
			form := twoRows()
			form.Set("_rows", action)
			resp := h.Post(t, h.Env.Kavya, target, form)
			webtest.WantStatus(t, resp, http.StatusBadRequest)
			webtest.WantText(t, resp, auth.ErrBadForm.Error())
			if calls != 0 {
				t.Errorf("_rows=%s on %s: service called %d times; want none", action, target, calls)
			}
		}
	}
}

// A user who can't save can't press Add or Remove either: AUTH's 403 comes
// first, even for a row that doesn't exist.
func TestScreens_RowActionsCheckTheRight(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{}})
	for _, action := range []string{"add", "remove:1", "remove:9"} {
		form := twoRows()
		form.Set("_rows", action)
		resp := h.Post(t, h.Env.Priya, base+"/new", form)
		webtest.WantStatus(t, resp, http.StatusForbidden)
		webtest.WantText(t, resp, deny(auth.RightCreate))
		resp = h.Post(t, h.Env.Priya, record, form)
		webtest.WantStatus(t, resp, http.StatusForbidden)
		webtest.WantText(t, resp, deny(auth.RightWrite))
	}
}

// On a record the right is checked on the record, as a save's is.
func TestScreens_RowActionChecksTheRecord(t *testing.T) {
	refs := 0
	h := harness(t, stubService{rec: stubRecord{}}, func(s *screen) {
		s.Ref = func(id int64) *auth.RecordRef {
			if id == 7 {
				refs++
			}
			return &auth.RecordRef{DocType: item.DocType, ID: id}
		}
	})
	form := twoRows()
	form.Set("_rows", "add")
	webtest.WantStatus(t, h.Post(t, h.Env.Priya, record, form), http.StatusForbidden)
	if refs != 1 {
		t.Errorf("Ref(7) called %d times on a refused row action; want 1", refs)
	}
}

// A reader sees the rows as text, with links as "{Code} {Name}" and numbers
// formatted, and nothing to edit them with.
func TestScreens_ReadOnlyRows(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{}})
	nos := nosID(t, h)
	h = harness(t, stubService{rec: stubRecord{
		"name": "Stub", "lines[1].uom_id": strconv.FormatInt(nos, 10), "lines[1].factor": "12.5",
		"lines[2].uom_id": strconv.FormatInt(nos, 10), "lines[2].factor": "100",
	}})
	page := h.Get(t, h.Env.Priya, record)
	webtest.WantStatus(t, page, http.StatusOK)
	webtest.WantText(t, page, "Conversions")
	summaries := page.Doc.Find("section dl.summary")
	if summaries.Length() != 2 {
		t.Fatalf("rows on the read-only page = %d; want 2", summaries.Length())
	}
	pairs := func(s *goquery.Selection) map[string]string {
		out := map[string]string{}
		s.Find(".summary-row").Each(func(_ int, row *goquery.Selection) {
			out[webtest.Squash(row.Find("dt").Text())] = webtest.Squash(row.Find("dd").Text())
		})
		return out
	}
	factor, err := types.ParseFactor("12.5")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := pairs(summaries.Eq(0)), map[string]string{
		"Row": "1", "UoM": "NOS Numbers", "Conversion factor": types.FormatFactor(factor),
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("row 1 = %v; want %v", got, want)
	}
	if got := pairs(summaries.Eq(1))["Row"]; got != "2" {
		t.Errorf("second row's number = %q; want 2", got)
	}
	if page.Doc.Find(`[name^="lines["], [name="_rows"]`).Length() != 0 {
		t.Error("a reader got a control for the rows")
	}
}

// A form with rows gets the content width, so the editor's 48rem layout can
// happen (CD-201); the rest stay at the form width.
func TestScreens_RowsFormIsWide(t *testing.T) {
	const narrow = ".form-width"
	h := harness(t, stubService{rec: stubRecord{}})
	for _, target := range []string{base + "/new", record} {
		if page := h.Get(t, h.Env.Kavya, target); page.Doc.Find(narrow).Length() != 0 {
			t.Errorf("GET %s with rows has the form width", target)
		}
	}
	h = harness(t, stubService{rec: stubRecord{}}, func(s *screen) { s.DocType = noSortDocType })
	for _, target := range []string{base + "/new", record} {
		if page := h.Get(t, h.Env.Kavya, target); page.Doc.Find(narrow).Length() == 0 {
			t.Errorf("GET %s without rows lost the form width", target)
		}
	}
}

// totalValidate is a rows master's Validate: rows it can't see add up to
// nothing, and row 1 of what it got is wrong too.
func totalValidate(stubRecord) (stubRecord, error) {
	return nil, &mstkit.ValidationError{Fields: []doctype.FieldError{
		{Message: "Portions add up to 0%."},
		{Field: "name", Message: "Name is required."},
		{Field: "factor", Line: 1, Message: "Row 1: Stale."},
	}}
}

// A row that didn't parse isn't in the input, so what Validate says about
// the rows is false. It still speaks for the scalar fields.
func TestScreens_RowParseErrorSilencesRowChecks(t *testing.T) {
	h := harness(t, stubService{}, func(s *screen) {
		s.ToInput = func(map[string]string) (stubRecord, []doctype.FieldError) {
			return stubRecord{}, []doctype.FieldError{{Field: "factor", Line: 2, Message: "Row 2: Not a number."}}
		}
		s.Validate = totalValidate
	})
	resp := h.Post(t, h.Env.Kavya, base+"/new", twoRows())
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, "lines[2].factor", "Row 2: Not a number.")
	webtest.WantFieldError(t, resp, "name", "Name is required.")
	webtest.WantNoText(t, resp, "Portions add up")
	webtest.WantNoText(t, resp, "Row 1: Stale.")
}

// With only a scalar's parse error the rows are all there, so Validate's
// word on them stands.
func TestScreens_ScalarParseErrorKeepsRowChecks(t *testing.T) {
	h := harness(t, stubService{}, func(s *screen) {
		s.ToInput = rateInput
		s.Validate = totalValidate
	})
	resp := h.Post(t, h.Env.Kavya, base+"/new", url.Values{"standard_rate": {"abc"}, "lines[1].factor": {"x"}})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantBanner(t, resp, "Portions add up to 0%.")
	webtest.WantFieldError(t, resp, "lines[1].factor", "Row 1: Stale.")
}

// Posted out of sequence, rows close up before the save: the service's Line
// is the row on the page, and the summary's link reaches its input. The
// number posted is gone, on create and on update.
func TestScreens_ServiceLineErrorAfterAGap(t *testing.T) {
	for _, target := range []string{base + "/new", record} {
		h := harness(t, stubService{rec: stubRecord{}, err: &mstkit.ValidationError{Fields: []doctype.FieldError{
			{Field: "factor", Line: 2, Message: "Row 2: Factor is wrong."},
		}}})
		resp := h.Post(t, h.Env.Kavya, target, url.Values{
			"version": {"3"}, "lines[2].factor": {"2"}, "lines[10].factor": {"10"},
		})
		webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
		webtest.WantFieldError(t, resp, "lines[2].factor", "Row 2: Factor is wrong.")
		if got := rowValue(t, resp, "lines[2].factor"); got != "10" {
			t.Errorf("POST %s: row 2 = %q; want the posted row 10's 10", target, got)
		}
		if got := rowValue(t, resp, "lines[1].factor"); got != "2" {
			t.Errorf("POST %s: row 1 = %q; want 2", target, got)
		}
		if resp.HasControl("lines[10].factor") {
			t.Errorf("POST %s: row 10 is still on the page", target)
		}
		if text, ok := resp.RowLink("#f-lines-2-factor"); !ok || text != "Row 2: Factor is wrong." {
			t.Errorf("POST %s: summary link = %q (found %v)", target, text, ok)
		}
		if resp.Doc.Find("input#f-lines-2-factor").Length() != 1 {
			t.Errorf("POST %s: the summary's link target is no input", target)
		}
	}
}

// A Numbers field is a NumberInput of that kind, scalar or in a row, with its
// MaxLen, value and error.
func TestScreens_NumbersUseNumberInput(t *testing.T) {
	const kind = components.NumberKind("factor6")
	h := harness(t, stubService{
		rec: stubRecord{"code": "1.5", "lines[1].note": "2.5"},
		err: &mstkit.ValidationError{Fields: []doctype.FieldError{
			{Field: "code", Message: "Code is wrong."},
			{Field: "note", Line: 1, Message: "Row 1: Note is wrong."},
		}},
	}, onRowsDocType, func(s *screen) {
		s.Numbers = map[string]components.NumberKind{"code": kind, "note": kind}
	})
	page := h.Get(t, h.Env.Kavya, record)
	webtest.WantStatus(t, page, http.StatusOK)
	for name, want := range map[string]struct{ value, maxLen string }{
		"code":          {"1.5", "9"},
		"lines[1].note": {"2.5", "12"},
	} {
		in := page.Doc.Find(`input[name="` + name + `"]`)
		if got, _ := in.Attr("hx-post"); got != "/ui/validate/factor6" {
			t.Errorf("%s hx-post = %q; want /ui/validate/factor6", name, got)
		}
		if got, _ := in.Attr("inputmode"); got != "decimal" {
			t.Errorf("%s inputmode = %q; want decimal", name, got)
		}
		if got, _ := in.Attr("value"); got != want.value {
			t.Errorf("%s value = %q; want %q", name, got, want.value)
		}
		if got, _ := in.Attr("maxlength"); got != want.maxLen {
			t.Errorf("%s maxlength = %q; want %q", name, got, want.maxLen)
		}
	}
	if page.Doc.Find(`input[name="lines[1].days"][hx-post]`).Length() != 0 {
		t.Error("days, not a Numbers field, became a NumberInput")
	}
	resp := h.Post(t, h.Env.Kavya, base+"/new", url.Values{"code": {"x"}, "lines[1].note": {"y"}})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, "code", "Code is wrong.")
	webtest.WantFieldError(t, resp, "lines[1].note", "Row 1: Note is wrong.")
}

// A normal save hands every posted row to the service, on create and on
// update.
func TestScreens_RowsReachTheService(t *testing.T) {
	want := []string{"lines[1].factor=10", "lines[1].uom_id=8", "lines[2].factor=100", "lines[2].uom_id=9"}
	for _, target := range []string{base + "/new", record} {
		var got stubRecord
		h := harness(t, stubService{rec: stubRecord{}, saved: mstkit.Saved{ID: 7}, in: &got})
		webtest.WantRedirect(t, h.Post(t, h.Env.Kavya, target, twoRows()), record)
		for _, kv := range want {
			key, value, _ := strings.Cut(kv, "=")
			if got[key] != value {
				t.Errorf("POST %s: service got %s = %q; want %q", target, key, got[key], value)
			}
		}
	}
}

// refuseCreate is a CanCreate for a user restricted on the master's
// dimension (CD-172).
func refuseCreate(context.Context, *db.Tx, auth.Principal) error {
	return auth.Forbidden("You can only work with your assigned stubs.")
}

// CD-172: a user the screen won't let create has no New, and is refused
// before the form, before a parse error's 422, and on a row action.
func TestScreens_CanCreateRefuses(t *testing.T) {
	const msg = "You can only work with your assigned stubs."
	calls := 0
	h := harness(t, stubService{calls: &calls}, func(s *screen) {
		s.CanCreate = refuseCreate
		s.ToInput = rateInput
	})
	list := h.Get(t, h.Env.Kavya, base)
	webtest.WantStatus(t, list, http.StatusOK)
	if _, ok := list.RowLink(base + "/new"); ok {
		t.Error("the list offers New to a user who can't create")
	}
	form := h.Get(t, h.Env.Kavya, base+"/new")
	webtest.WantStatus(t, form, http.StatusForbidden)
	webtest.WantText(t, form, msg)

	resp := h.Post(t, h.Env.Kavya, base+"/new", url.Values{"standard_rate": {"abc"}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, msg)

	resp = h.Post(t, h.Env.Kavya, base+"/new", url.Values{"_rows": {"add"}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, msg)
	if calls != 0 {
		t.Errorf("service called %d times; want none", calls)
	}
}

// A CanCreate that lets the user through changes nothing, and it reads
// through the request's user and a real transaction.
func TestScreens_CanCreateAllows(t *testing.T) {
	var who auth.Principal
	sawTx := false
	h := harness(t, stubService{}, func(s *screen) {
		s.CanCreate = func(_ context.Context, tx *db.Tx, p auth.Principal) error {
			who, sawTx = p, tx != nil
			return nil
		}
	})
	if _, ok := h.Get(t, h.Env.Kavya, base).RowLink(base + "/new"); !ok {
		t.Error("the list lost New")
	}
	webtest.WantStatus(t, h.Get(t, h.Env.Kavya, base+"/new"), http.StatusOK)
	if !reflect.DeepEqual(who, h.Env.Kavya) || !sawTx {
		t.Errorf("CanCreate got principal %+v (tx %v); want Kavya and a tx", who, sawTx)
	}
}

// The kind's create right still comes first: AUTH's words, not the screen's.
func TestScreens_CanCreateAfterTheRight(t *testing.T) {
	h := harness(t, stubService{}, func(s *screen) { s.CanCreate = refuseCreate })
	resp := h.Get(t, h.Env.Priya, base+"/new")
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightCreate))
}
