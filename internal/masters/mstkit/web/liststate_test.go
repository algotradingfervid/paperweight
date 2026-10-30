package mstweb_test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// No case ID: a master with a status of its own (Project, Employee; M13), and
// a master with dates. statusDocType is item rows whose item_type stands in
// for the status: a Select at ListPriority 3, with disabled not in the list.
// datesDocType has a Date field and is never listed, so its table doesn't
// matter. Registered before msttest grants rights.
const (
	statusDocType = "mstweb.Status"
	datesDocType  = "mstweb.Dates"
)

func init() {
	none := mstkit.LookupFunc(func(context.Context, *db.Tx, int64) (doctype.MasterInfo, bool, error) {
		return doctype.MasterInfo{}, false, nil
	})
	mstkit.RegisterMaster(doctype.DocType{
		Name: statusDocType, Label: "Stub", PluralLabel: "Stubs", Table: "item",
		SearchFields: []string{"code", "name"},
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, ListPriority: 4},
			{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, ListPriority: 1},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check, Filterable: true},
			{Name: "item_type", Label: "Kind", Column: "item_type", Type: doctype.Select, ListPriority: 3, Options: []string{"Goods", "Service"}},
		},
	}, none)
	mstkit.RegisterMaster(doctype.DocType{
		Name: datesDocType, Label: "Stub", PluralLabel: "Stubs", Table: "item",
		SearchFields: []string{"code"},
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, ListPriority: 4},
			{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, ListPriority: 1},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check, ListPriority: 3},
			{Name: "starts", Label: "Starts", Column: "starts", Type: doctype.Date},
		},
	}, none)
}

func onStatusDocType(s *screen) { s.DocType = statusDocType }

func onDatesDocType(s *screen) { s.DocType = datesDocType }

// listHead is the list's column headings, in order.
func listHead(r webtest.Response) []string {
	return r.Doc.Find(".datalist-head span").Map(func(_ int, s *goquery.Selection) string { return webtest.Squash(s.Text()) })
}

// A list column at the state's priority whose field isn't disabled shows its
// own value, plainly and with its label, on the default list; Enabled and
// Disabled only come with the disabled records, in a column of their own.
func TestScreens_StatusFieldIsAPlainColumn(t *testing.T) {
	h := harness(t, stubService{}, onStatusDocType)
	kavya := h.Env.Kavya
	id := seedItem(t, h, "M12")

	list := h.Get(t, kavya, base)
	text, ok := list.RowLink(stubPath(id))
	if !ok || !strings.Contains(text, "Goods") || strings.Contains(text, "Enabled") {
		t.Errorf("row = %q (found %v); want the value Goods and no Enabled tag", text, ok)
	}
	if got, want := listHead(list), []string{"Name", "Kind"}; !slices.Equal(got, want) {
		t.Errorf("columns = %q; want %q", got, want)
	}
	if list.Doc.Find(`.datalist-head [data-priority="low"]`).Length() != 0 {
		t.Error("the status column is low priority; want it kept on phones")
	}

	all := base + "?include_disabled=true"
	text, _ = h.Get(t, kavya, all).RowLink(stubPath(id))
	if !strings.Contains(text, "Goods") || !strings.Contains(text, "Enabled") {
		t.Errorf("include_disabled row = %q; want Goods and Enabled", text)
	}
	if err := item.New(h.Env.Kit).SetDisabled(context.Background(), kavya, id, 1, true); err != nil {
		t.Fatal(err)
	}
	listed := h.Get(t, kavya, all)
	text, _ = listed.RowLink(stubPath(id))
	if !strings.Contains(text, "Goods") || !strings.Contains(text, "Disabled") {
		t.Errorf("include_disabled row = %q; want Goods and Disabled", text)
	}
	if got, want := listHead(listed), []string{"Name", "Kind", "State"}; !slices.Equal(got, want) {
		t.Errorf("columns = %q; want %q", got, want)
	}
}

// A plain status column carries its OptionLabels.
func TestScreens_StatusFieldLabel(t *testing.T) {
	h := harness(t, stubService{}, onStatusDocType, func(s *screen) {
		s.OptionLabels = map[string]map[string]string{"item_type": {"Goods": "Physical goods"}}
	})
	id := seedItem(t, h, "M12")
	text, _ := h.Get(t, h.Env.Kavya, base).RowLink(stubPath(id))
	if !strings.Contains(text, "Physical goods") {
		t.Errorf("row = %q; want the label Physical goods", text)
	}
}

// A master whose disabled field is its list state keeps the one Status column,
// showing Enabled or Disabled.
func TestScreens_DisabledFieldStaysTheState(t *testing.T) {
	h := harness(t, stubService{})
	id := seedItem(t, h, "M12")
	all := h.Get(t, h.Env.Kavya, base+"?include_disabled=true")
	if got := listHead(all); slices.Contains(got, "State") || !slices.Contains(got, "Status") {
		t.Errorf("columns = %q; want Status and no State", got)
	}
	if text, _ := all.RowLink(stubPath(id)); !strings.Contains(text, "Enabled") {
		t.Errorf("row = %q; want Enabled", text)
	}
}

// Defaults preselect on the new page only: a refused form shows what was
// posted and a stored record what is stored, blank included.
func TestScreens_DefaultsOnTheNewPageOnly(t *testing.T) {
	defaults := func(s *screen) { s.Defaults = map[string]string{"item_type": "Service"} }
	ticked := func(r webtest.Response) string {
		v, _ := r.Doc.Find(`input[name="item_type"][checked]`).Attr("value")
		return v
	}

	h := harness(t, stubService{err: &refusal{text: "No.", status: http.StatusUnprocessableEntity}}, defaults)
	if got := ticked(h.Get(t, h.Env.Kavya, base+"/new")); got != "Service" {
		t.Errorf("new page item_type = %q; want the default Service", got)
	}
	resp := h.Post(t, h.Env.Kavya, base+"/new", url.Values{"code": {"NEW"}})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	if got := ticked(resp); got != "" {
		t.Errorf("refused form item_type = %q; want nothing ticked, as posted", got)
	}

	h = harness(t, stubService{rec: stubRecord{}}, defaults)
	if got := ticked(h.Get(t, h.Env.Kavya, record)); got != "" {
		t.Errorf("stored record item_type = %q; want nothing ticked, as stored", got)
	}

	h = harness(t, stubService{})
	if got := ticked(h.Get(t, h.Env.Kavya, base+"/new")); got != "" {
		t.Errorf("new page without Defaults item_type = %q; want nothing ticked", got)
	}
}

// Values gives a date as stored (the read-only view needs that); the form
// shows it dd-mm-yyyy, and a blank one blank.
func TestScreens_StoredDateOnTheForm(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{"starts": "2026-07-01"}}, onDatesDocType)
	page := h.Get(t, h.Env.Kavya, record)
	webtest.WantStatus(t, page, http.StatusOK)
	if v, _ := page.Value("starts"); v != "01-07-2026" {
		t.Errorf("form starts = %q; want 01-07-2026", v)
	}
	read := h.Get(t, h.Env.Priya, record)
	webtest.WantStatus(t, read, http.StatusOK)
	webtest.WantText(t, read, "01-07-2026")

	h = harness(t, stubService{rec: stubRecord{"starts": ""}}, onDatesDocType)
	if v, ok := h.Get(t, h.Env.Kavya, record).Value("starts"); !ok || v != "" {
		t.Errorf("form starts = %q (found %v); want empty", v, ok)
	}
}

// A refused save keeps the date as typed, whatever its form; a refusal of an
// action with no form shows the stored record.
func TestScreens_RefusedSaveKeepsTheTypedDate(t *testing.T) {
	stored := stubRecord{"starts": "2026-07-01"}
	form := url.Values{"version": {"3"}, "code": {"STUB"}, "name": {"Stub"}, "starts": {"2026-08-15"}}
	for _, status := range []int{http.StatusUnprocessableEntity, http.StatusConflict} {
		h := harness(t, stubService{rec: stored, err: &refusal{text: "No.", status: status}}, onDatesDocType)
		resp := h.Post(t, h.Env.Kavya, record, form)
		webtest.WantStatus(t, resp, status)
		if v, _ := resp.Value("starts"); v != "2026-08-15" {
			t.Errorf("%d page starts = %q; want the typed 2026-08-15", status, v)
		}
	}

	h := harness(t, stubService{rec: stored, err: &refusal{text: "No.", status: http.StatusConflict}}, onDatesDocType)
	resp := h.Post(t, h.Env.Kavya, record+"/disable", url.Values{"version": {"3"}})
	webtest.WantStatus(t, resp, http.StatusConflict)
	if v, _ := resp.Value("starts"); v != "01-07-2026" {
		t.Errorf("refused disable page starts = %q; want the stored 01-07-2026", v)
	}
}

// A stored date that can't be read is an internal error, as it is on the
// read-only view, never a form with an empty date to save over it.
func TestScreens_UnreadableStoredDate(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{"starts": "junk"}}, onDatesDocType)
	webtest.WantStatus(t, h.Get(t, h.Env.Kavya, record), http.StatusInternalServerError)
}
