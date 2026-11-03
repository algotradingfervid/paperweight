package docscreen_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/kernel/list/listtest"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// M0-52 (CD-325, CD-326 (3)): a Select value shows through its OptionLabels
// entry, falling back to the raw value; the state field is never shown raw.

const labelledType = "docscreentest.Labelled"

var registerLabelledOnce sync.Once

// registerLabelled registers a document whose kind Select labels "urgent"
// but not "normal", and whose state field is a low-priority list column. Its
// StateLabels renames pending, so the global label can't pass for it.
func registerLabelled() {
	docscreentest.RegisterSample()
	registerLabelledOnce.Do(func() {
		doctype.Register(doctype.DocType{
			Name: labelledType, Label: "Labelled Order", PluralLabel: "Labelled Orders",
			Table: "docscreentest_labelled", LinesTable: "docscreentest_labelled_line", Submittable: true,
			StateLabels: map[doctype.State]string{doctype.StatePending: "Sent for sign-off"},
			Fields: []doctype.Field{
				{Name: "supplier_id", Label: "Supplier", Type: doctype.Link, Link: docscreentest.Supplier, Required: true},
				{Name: "kind", Label: "Kind", Type: doctype.Select, Filterable: true, ListPriority: 4,
					Options: []string{"normal", "urgent"}, OptionLabels: map[string]string{"urgent": "Very urgent"}},
				{Name: "amount", Label: "Amount", Type: doctype.Money, ReadOnly: true, ListPriority: 2},
				{Name: "state", Label: "State", Type: doctype.Select, ReadOnly: true, Filterable: true, ListPriority: 5,
					Options: []string{string(doctype.StateDraft), string(doctype.StatePending), string(doctype.StateApproved)}},
				{Name: "item_id", Label: "Item", Type: doctype.Link, Link: docscreentest.Item, Required: true, OnLine: true},
				{Name: "qty", Label: "Quantity", Type: doctype.Qty, Required: true, OnLine: true},
				{Name: "line_kind", Label: "Line kind", Type: doctype.Select, OnLine: true,
					Options: []string{"normal", "urgent"}, OptionLabels: map[string]string{"urgent": "Rush line"}},
			},
		})
	})
}

func labelledEnv(t *testing.T) *docEnv {
	t.Helper()
	registerLabelled()
	e := newDocEnv(t, allRights...)
	for _, r := range allRights {
		e.az.Grant("Buyer", labelledType, r, auth.ScopeAll)
	}
	return e
}

func seedLabelled(e *docEnv, s doctype.State, kind string) {
	docscreentest.Seed(e.docs, doctype.Info{
		DocType: labelledType, DocLabel: "Labelled Order", ID: 42, State: s,
		Display: "Draft 42", CreatedBy: priya.ID,
	}, doc.Record{
		Version: 1,
		Values:  map[string]string{"supplier_id": "7", "kind": kind, "amount": "100.00", "state": string(s)},
		Lines:   []map[string]string{{"id": "1", "item_id": "3", "qty": "2.000", "line_kind": "urgent"}},
	})
}

// summary is every "Label: Value" row of the summary lists in sel.
func summary(sel *goquery.Selection) []string {
	var out []string
	sel.Find("dl.summary .summary-row").Each(func(_ int, r *goquery.Selection) {
		out = append(out, norm(r.Find("dt").Text())+": "+norm(r.Find("dd").Text()))
	})
	return out
}

func noStateRow(t *testing.T, where string, rows []string) {
	t.Helper()
	for _, r := range rows {
		if strings.HasPrefix(r, "State:") {
			t.Errorf("%s shows %q; the badge shows the state", where, r)
		}
	}
}

func TestSelectLabels_DocPageDetails(t *testing.T) {
	for _, c := range []struct{ kind, want string }{{"urgent", "Kind: Very urgent"}, {"normal", "Kind: normal"}} {
		e := labelledEnv(t)
		seedLabelled(e, doctype.StatePending, c.kind)
		res := e.get(t, "/doc/"+labelledType+"/42")
		if res.Code != http.StatusOK {
			t.Fatalf("status = %d, body %.300s", res.Code, res.Body)
		}
		page := uitest.Doc(t, res)
		details := page.Find("section.card").FilterFunction(func(_ int, s *goquery.Selection) bool {
			return norm(s.Find(".card-title").First().Text()) == "Details"
		})
		rows := summary(details)
		if !slices.Contains(rows, c.want) {
			t.Errorf("Details = %q, want %q", rows, c.want)
		}
		noStateRow(t, "Details", rows)
	}
}

func TestSelectLabels_CheckPageAndTotals(t *testing.T) {
	e := labelledEnv(t)
	seedLabelled(e, doctype.StateDraft, "urgent")

	res := e.get(t, "/doc/"+labelledType+"/42/edit/check")
	if res.Code != http.StatusOK {
		t.Fatalf("check: status = %d, body %.300s", res.Code, res.Body)
	}
	page := uitest.Doc(t, res)
	rows := summary(checkSection(page.Selection, "Details"))
	if !slices.Contains(rows, "Kind: Very urgent") || !slices.Contains(rows, "Amount: ₹100.00") {
		t.Errorf("check Details = %q, want Kind: Very urgent and the amount", rows)
	}
	noStateRow(t, "the check page's Details", rows)

	res = e.get(t, "/doc/"+labelledType+"/42/edit/details")
	if res.Code != http.StatusOK {
		t.Fatalf("details step: status = %d, body %.300s", res.Code, res.Body)
	}
	page = uitest.Doc(t, res)
	label := page.Find(`input[type="radio"][name="kind"][value="urgent"]`).Closest("label")
	if got := norm(label.Text()); got != "Very urgent" {
		t.Errorf("urgent option label = %q, want Very urgent", got)
	}
	if got := norm(page.Find(`input[type="radio"][name="kind"][value="normal"]`).Closest("label").Text()); got != "normal" {
		t.Errorf("normal option label = %q, want the raw value", got)
	}
	rows = summary(page.Find("#wizard"))
	if !slices.Contains(rows, "Amount: ₹100.00") {
		t.Errorf("details step fixed values = %q, want the amount", rows)
	}
	noStateRow(t, "the details step", rows)

	res = e.get(t, "/doc/"+labelledType+"/42/edit/items")
	if res.Code != http.StatusOK {
		t.Fatalf("items step: status = %d, body %.300s", res.Code, res.Body)
	}
	page = uitest.Doc(t, res)
	rows = summary(page.Find("#doc-totals"))
	if !slices.Contains(rows, "Amount: ₹100.00") {
		t.Errorf("step totals = %q, want the amount", rows)
	}
	noStateRow(t, "the step totals", rows)
}

func TestSelectLabels_LineDisplay(t *testing.T) {
	registerLabelled()
	e := labelledEnv(t)
	seedLabelled(e, doctype.StatePending, "urgent")
	v, err := e.d.View(context.Background(), priya, labelledType, 42)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Lines[0].Display["line_kind"]; got != "Rush line" {
		t.Errorf("line display = %q, want Rush line", got)
	}
	if _, ok := v.Display["state"]; ok {
		t.Errorf("Display has state %q; the badge shows it", v.Display["state"])
	}
	if slices.Contains(v.HeaderNames, "state") {
		t.Errorf("HeaderNames = %q, want no state", v.HeaderNames)
	}
}

func TestSelectLabels_ListCellsAndChips(t *testing.T) {
	registerLabelled()
	e := newEnv(t)
	e.az.Grant("Buyer", labelledType, "read", auth.ScopeAll)
	e.lister.Set(labelledType, types.Result[docscreen.ListRow]{Total: 3, Rows: []docscreen.ListRow{
		{Info: doctype.Info{DocType: labelledType, ID: 1, State: doctype.StatePending, Display: "Draft 1"},
			Values: map[string]string{"kind": "urgent", "state": "pending_approval", "amount": "1.00"}},
		{Info: doctype.Info{DocType: labelledType, ID: 2, State: doctype.StateApproved, Display: "LO/1"},
			Values: map[string]string{"kind": "normal", "state": "approved", "amount": "2.00"}},
		{Info: doctype.Info{DocType: labelledType, ID: 3, State: "archived", Display: "LO/2"},
			Values: map[string]string{"kind": "normal", "state": "archived", "amount": "3.00"}},
	}})
	rec := e.get(t, "/doc/"+labelledType+"?f=kind:in:urgent&f=kind:in:normal&f=state:=:pending_approval&f=state:!=:archived")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %.300s", rec.Code, rec.Body)
	}
	page := uitest.Doc(t, rec)
	cell := func(row int, label string) string {
		return norm(page.Find("ul.datalist-rows > li").Eq(row).Find("dl").FilterFunction(func(_ int, s *goquery.Selection) bool {
			return norm(s.Find("dt").Text()) == label
		}).First().Find("dd").Text())
	}
	for _, c := range []struct {
		row         int
		label, want string
	}{
		{0, "Kind", "Very urgent"},
		{1, "Kind", "normal"},
		{0, "State", "Sent for sign-off"},
		{1, "State", "Approved"},
		{2, "State", "archived"}, // an unknown state shows raw, never blank
	} {
		if got := cell(c.row, c.label); !strings.HasPrefix(got, c.want) {
			t.Errorf("row %d %s cell = %q, want %q", c.row+1, c.label, got, c.want)
		}
	}
	var chips []string
	page.Find(`ul[aria-label="Active filters"] a.chip`).Each(func(_ int, s *goquery.Selection) { chips = append(chips, norm(s.Text())) })
	// R5: plain words; the single state is the current quick chip.
	for _, want := range []string{"Kind: Very urgent, normal", "State: not archived"} {
		if !slices.ContainsFunc(chips, func(c string) bool { return strings.HasPrefix(c, want) }) {
			t.Errorf("chips = %q, want one starting %q", chips, want)
		}
	}
	if len(chips) != 2 {
		t.Errorf("removable chips = %q, want the two above", chips)
	}
	if got := norm(page.Find(`ul[aria-label="Quick filters"] a[aria-current="true"]`).Text()); got != "Sent for sign-off 3" {
		t.Errorf("current quick chip = %q, want the state's label and count", got)
	}
}

// CSV export stays raw: a labelled Select exports its stored value.
func TestSelectLabels_CSVStaysRaw(t *testing.T) {
	d := listtest.Setup(t)
	listtest.SeedFixtures(t, d)
	listtest.InsertPO(t, d, listtest.PORow{Number: "PO/26-27/00001", PartyName: "Shree Steel Traders", Status: "pending_approval"})
	dt := *listtest.PO()
	dt.Fields = slices.Clone(dt.Fields)
	i := slices.IndexFunc(dt.Fields, func(f doctype.Field) bool { return f.Name == "status" })
	dt.Fields[i].OptionLabels = map[string]string{"pending_approval": "Waiting for approval"}

	var buf bytes.Buffer
	if _, err := csvio.NewExporter(d, listtest.NewAuth()).Export(context.Background(), &buf, testkit.Principal(t, "priya@vardhan.test"), &dt, list.Query{}); err != nil {
		t.Fatal(err)
	}
	recs, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(buf.Bytes(), []byte(bom)))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	col := slices.Index(recs[0], dt.Fields[i].Label)
	if col < 0 || len(recs) != 2 || recs[1][col] != "pending_approval" {
		t.Errorf("export = %q, want the status column raw (pending_approval)", recs)
	}
}

const bom = "\xef\xbb\xbf" // the UTF-8 BOM
