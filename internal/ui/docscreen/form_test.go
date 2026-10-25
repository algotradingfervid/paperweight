package docscreen_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/alexedwards/scs/v2"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// Task 14's extra test DocTypes (CD-65 prefix).
const (
	// claimType has one editable Money field and no lines: details, notes,
	// check.
	claimType = "docscreentest.Claim"
	// shipType groups its fields by Step: details, delivery, check.
	shipType = "docscreentest.Shipment"
)

var registerTask14Once sync.Once

func registerTask14() {
	docscreentest.RegisterSample()
	registerTask14Once.Do(func() {
		doctype.Register(doctype.DocType{
			Name: claimType, Label: "Expense Claim", PluralLabel: "Expense Claims", Table: "docscreentest_claim", Submittable: true,
			Fields: []doctype.Field{
				{Name: "claim_date", Label: "Claim date", Type: doctype.Date, Required: true},
				{Name: "total", Label: "Total", Type: doctype.Money},
			},
		})
		doctype.Register(doctype.DocType{
			Name: shipType, Label: "Shipment", PluralLabel: "Shipments", Table: "docscreentest_shipment", Submittable: true,
			Fields: []doctype.Field{
				{Name: "ref", Label: "Reference", Type: doctype.Text, Required: true},
				{Name: "deliver_on", Label: "Deliver on", Type: doctype.Date, Required: true, Step: "delivery"},
				{Name: "deliver_to", Label: "Deliver to", Type: doctype.Text, Step: "delivery"},
			},
		})
	})
}

// newFormEnv is newDocEnv plus the Task 14 DocTypes, with the same rights
// on each.
func newFormEnv(t *testing.T, rights ...auth.Right) *docEnv {
	t.Helper()
	registerTask14()
	e := newDocEnv(t, rights...)
	for _, name := range []string{claimType, shipType} {
		for _, r := range rights {
			e.az.Grant("Buyer", name, r, auth.ScopeAll)
		}
	}
	return e
}

// remount rebuilds the router from e.d, as newDocEnv does, after a test
// changed a dependency.
func (e *docEnv) remount() {
	r := web.NewRouter(web.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Sessions: scs.New()})
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(ui.WithShell(r.Context(), ui.Shell{CSRF: csrfToken})))
		})
	})
	docscreen.Routes(e.d)(r)
	e.h = r
}

// stateEngine applies a successful action's State to the fake, as DOC
// would, so the page an action leads to sees the new state.
type stateEngine struct{ *docfake.Fake }

func (s stateEngine) Do(ctx context.Context, p auth.Principal, name string, id int64, a doc.Action, in doc.ActionInput) (doc.Result, error) {
	res, err := s.Fake.Do(ctx, p, name, id, a, in)
	if err == nil && res.State != "" {
		target := id
		if res.ID != 0 {
			target = res.ID
		}
		s.SetState(name, target, res.State)
	}
	return res, err
}

// draftInfo is document id of DocType name as a draft created by Priya.
func draftInfo(name, label string, id int64) doctype.Info {
	return doctype.Info{
		DocType: name, DocLabel: label, ID: id, State: doctype.StateDraft,
		Display: "Draft " + itoa(id), CreatedBy: priya.ID, EmployeeID: 5,
	}
}

// pathOf is the path of a URL header value.
func pathOf(t *testing.T, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("bad URL %q: %v", raw, err)
	}
	return u.Path
}

// rootIDs are the ids of the response's top-level elements: what an
// outerHTML swap puts in place of its target.
func rootIDs(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	return uitest.Doc(t, rec).Find("body").Children().Map(func(_ int, s *goquery.Selection) string {
		id, _ := s.Attr("id")
		return id
	})
}

// stepCount is the stepper's "Step n of N" text.
func stepCount(sel *goquery.Selection) string {
	return norm(sel.Find(".stepper__count").Text())
}

// stepForm is the wizard's step form: the POST form in #wizard whose
// action is the step's URL.
func stepForm(doc *goquery.Document, action string) *goquery.Selection {
	return doc.Find(`#wizard form[method="post"]`).FilterFunction(func(_ int, s *goquery.Selection) bool {
		a, _ := s.Attr("action")
		return a == action
	})
}

func TestFormSteps_FromFieldStep(t *testing.T) {
	registerTask14()
	type step struct {
		Slug, Label string
		Fields      []string
		Lines       bool
	}
	got := func(t *testing.T, name string) []step {
		t.Helper()
		dt, ok := doctype.Lookup(name)
		if !ok {
			t.Fatalf("%s not registered", name)
		}
		steps, err := docscreen.FormSteps(dt)
		if err != nil {
			t.Fatalf("FormSteps(%s): %v", name, err)
		}
		out := make([]step, len(steps))
		for i, s := range steps {
			// A ReadOnly field may be listed on its step to be shown there;
			// it is never an input, which the page checks below.
			fields := slices.DeleteFunc(slices.Clone(s.Fields), func(f string) bool { return f == "amount" })
			out[i] = step{s.Slug, s.Label, fields, s.Lines}
		}
		return out
	}
	equal := func(a, b []step) bool {
		return slices.EqualFunc(a, b, func(x, y step) bool {
			return x.Slug == y.Slug && x.Label == y.Label && x.Lines == y.Lines && slices.Equal(x.Fields, y.Fields)
		})
	}

	t.Run("sample", func(t *testing.T) {
		want := []step{
			{"details", "Details", []string{"supplier_id", "order_date", "priority", "remarks"}, false},
			{"items", "Items", nil, true},
			{"check", "Check and submit", nil, false},
		}
		if s := got(t, docscreentest.Sample); !equal(s, want) {
			t.Errorf("steps = %+v,\nwant %+v", s, want)
		}
	})
	t.Run("grouped by Step", func(t *testing.T) {
		want := []step{
			{"details", "Details", []string{"ref"}, false},
			{"delivery", "Delivery", []string{"deliver_on", "deliver_to"}, false},
			{"check", "Check and submit", nil, false},
		}
		if s := got(t, shipType); !equal(s, want) {
			t.Errorf("steps = %+v,\nwant %+v", s, want)
		}
	})
	t.Run("fewer than 3 adds notes", func(t *testing.T) {
		want := []step{
			{"details", "Details", []string{"claim_date", "total"}, false},
			{"notes", "Other details", nil, false},
			{"check", "Check and submit", nil, false},
		}
		if s := got(t, claimType); !equal(s, want) {
			t.Errorf("steps = %+v,\nwant %+v", s, want)
		}
	})
	t.Run("six steps is an error", func(t *testing.T) {
		const name = "docscreentest.TooMany"
		dt := &doctype.DocType{Name: name, Label: "Too Many", Fields: []doctype.Field{
			{Name: "f0", Label: "F0", Type: doctype.Text},
			{Name: "f1", Label: "F1", Type: doctype.Text, Step: "one"},
			{Name: "f2", Label: "F2", Type: doctype.Text, Step: "two"},
			{Name: "f3", Label: "F3", Type: doctype.Text, Step: "three"},
			{Name: "f4", Label: "F4", Type: doctype.Text, Step: "four"},
		}}
		steps, err := docscreen.FormSteps(dt)
		if err == nil {
			t.Fatalf("FormSteps = %+v, want an error for 6 steps", steps)
		}
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name the DocType %s", err, name)
		}
	})

	// amount is ReadOnly (CD-79): text on details and check, never posted.
	e := newFormEnv(t, allRights...)
	e.seed(doctype.StateDraft)
	for _, step := range []string{"details", "check"} {
		rec := e.get(t, doc42+"/edit/"+step)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d, body %.300s", step, rec.Code, rec.Body)
		}
		wiz := uitest.Doc(t, rec).Find("#wizard")
		if !strings.Contains(norm(wiz.Find("dl").Text()), "₹94,164.00") {
			t.Errorf("%s: amount ₹94,164.00 is not shown as a read-only value", step)
		}
		if n := wiz.Find(`[name="amount"]`).Length(); n != 0 {
			t.Errorf("%s: %d controls named amount; a ReadOnly field is never an input", step, n)
		}
	}
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{ID: 42, State: doctype.StateDraft, Version: 4}, nil)
	e.post(t, doc42+"/edit/details", url.Values{"supplier_id": {"7"}, "order_date": {"24-10-2026"}, "amount": {"1.00"}})
	calls := e.docs.Calls()
	if len(calls) != 1 {
		t.Fatalf("engine calls = %+v, want one save", calls)
	}
	if v, ok := calls[0].Input.Values["amount"]; ok {
		t.Errorf("save posted amount = %q; a ReadOnly field is never posted", v)
	}

	// A step with no fields says so (design-system §8.3).
	docscreentest.Seed(e.docs, draftInfo(claimType, "Expense Claim", 50), doc.Record{Version: 3, Values: map[string]string{"claim_date": "2026-10-24"}})
	rec := e.get(t, "/doc/"+claimType+"/50/edit/notes")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET notes: status = %d, body %.300s", rec.Code, rec.Body)
	}
	if text := norm(uitest.Doc(t, rec).Find("#wizard").Text()); !strings.Contains(text, "Nothing needed here") {
		t.Errorf("empty notes step lacks \"Nothing needed here\"")
	}
}

func TestFieldInput_ByType(t *testing.T) {
	docscreentest.RegisterSample()
	d := docscreen.Deps{Today: func() (string, string) { return "24-10-2026", "23-10-2026" }}
	dt, _ := doctype.Lookup(docscreentest.Sample)
	field := func(name string) doctype.Field {
		f, ok := dt.Field(name)
		if !ok {
			t.Fatalf("no field %s", name)
		}
		return f
	}
	render := func(f doctype.Field, value, errMsg string) *goquery.Document {
		return uitest.Render(t, docscreen.FieldInput(f, "f-"+f.Name, value, errMsg, d))
	}

	t.Run("money", func(t *testing.T) {
		doc := render(doctype.Field{Name: "total", Label: "Total", Type: doctype.Money}, "94,164.00", "")
		in := doc.Find(`input#f-total[inputmode="decimal"]`)
		if in.Length() != 1 {
			t.Fatal("Money is not one input#f-total with inputmode=decimal")
		}
		if v, _ := in.Attr("value"); v != "94,164.00" {
			t.Errorf("value = %q", v)
		}
		if hx, _ := in.Attr("hx-post"); hx != "/ui/validate/money" {
			t.Errorf("hx-post = %q, want /ui/validate/money (CD-48)", hx)
		}
	})
	t.Run("link", func(t *testing.T) {
		doc := render(field("supplier_id"), "", "")
		if doc.Find(`[role="combobox"][hx-get="/pick/`+docscreentest.Supplier+`"]`).Length() != 1 {
			t.Errorf("Link is not a combobox searching /pick/%s", docscreentest.Supplier)
		}
		if doc.Find(`input[type="hidden"][name="supplier_id"]`).Length() != 1 {
			t.Error("Link has no hidden supplier_id input")
		}
		if strings.Contains(doc.Text(), "(optional)") {
			t.Error("a Required field is marked (optional)")
		}
	})
	t.Run("select with 2 options", func(t *testing.T) {
		doc := render(field("priority"), "Urgent", "")
		radios := doc.Find(`input[type="radio"][name="priority"]`)
		vals := radios.Map(func(_ int, s *goquery.Selection) string { v, _ := s.Attr("value"); return v })
		if !slices.Equal(vals, []string{"Normal", "Urgent"}) {
			t.Errorf("radio values = %q, want [Normal Urgent]", vals)
		}
		if doc.Find("select").Length() != 0 {
			t.Error("2 options render a <select>; want radios")
		}
		text := norm(doc.Text())
		for _, l := range []string{"Normal", "Urgent"} {
			if !strings.Contains(text, l) {
				t.Errorf("no label %q", l)
			}
		}
		if _, ok := doc.Find(`input[type="radio"][value="Urgent"]`).Attr("checked"); !ok {
			t.Error("the value Urgent is not checked")
		}
		if !strings.Contains(text, "(optional)") {
			t.Error("Required false: no \"(optional)\"")
		}
	})
	t.Run("select with 7 options", func(t *testing.T) {
		opts := []string{"A", "B", "C", "D", "E", "F", "G"}
		doc := render(doctype.Field{Name: "grade", Label: "Grade", Type: doctype.Select, Options: opts}, "", "")
		if n := doc.Find(`select[name="grade"] option`).FilterFunction(func(_ int, s *goquery.Selection) bool {
			v, _ := s.Attr("value")
			return slices.Contains(opts, v)
		}).Length(); n != 7 {
			t.Errorf("select has %d of the 7 options", n)
		}
		if doc.Find(`input[type="radio"]`).Length() != 0 {
			t.Error("7 options render radios; want a <select>")
		}
	})
	t.Run("long text", func(t *testing.T) {
		doc := render(field("remarks"), "Deliver to gate 2", "Too long.")
		ta := doc.Find(`textarea#f-remarks[name="remarks"][maxlength="1000"]`)
		if ta.Length() != 1 {
			t.Fatal("remarks is not a textarea with maxlength=1000")
		}
		if ta.Text() != "Deliver to gate 2" {
			t.Errorf("textarea text = %q", ta.Text())
		}
		text := norm(doc.Text())
		for _, want := range []string{"Shown on the printed order.", "(optional)", "Too long.", "Remarks"} {
			if !strings.Contains(text, want) {
				t.Errorf("remarks lacks %q", want)
			}
		}
	})
	t.Run("short text", func(t *testing.T) {
		doc := render(doctype.Field{Name: "ref", Label: "Reference", Type: doctype.Text, MaxLen: 100}, "", "")
		if doc.Find(`input[type="text"][name="ref"][maxlength="100"]`).Length() != 1 || doc.Find("textarea").Length() != 0 {
			t.Error("Text with MaxLen 100 is not a text input with maxlength=100")
		}
	})
	t.Run("int", func(t *testing.T) {
		doc := render(doctype.Field{Name: "count", Label: "Count", Type: doctype.Int}, "", "")
		if doc.Find(`input[name="count"][inputmode="numeric"]`).Length() != 1 {
			t.Error("Int is not an input with inputmode=numeric")
		}
	})
	t.Run("check", func(t *testing.T) {
		doc := render(doctype.Field{Name: "urgent", Label: "Urgent", Type: doctype.Check}, "", "")
		if n := doc.Find(`input[type="checkbox"]`).Length(); n != 1 {
			t.Errorf("Check renders %d checkboxes, want 1", n)
		}
	})
	t.Run("date", func(t *testing.T) {
		doc := render(field("order_date"), "24-10-2026", "")
		if doc.Find(`input#f-order_date[name="order_date"]`).Length() != 1 {
			t.Fatal("no order_date input")
		}
		if doc.Find(`[data-set-date="24-10-2026"]`).Length() != 1 || doc.Find(`[data-set-date="23-10-2026"]`).Length() != 1 {
			t.Error("Date has no Today/Yesterday from d.Today")
		}
	})
	t.Run("timestamp", func(t *testing.T) {
		doc := render(doctype.Field{Name: "sent_at", Label: "Sent at", Type: doctype.Timestamp}, "24-10-2026 14:45", "")
		if n := doc.Find(`input:not([type="hidden"]):not([readonly]), textarea:not([readonly]), select`).Length(); n != 0 {
			t.Errorf("Timestamp renders %d editable controls, want read-only text", n)
		}
		if !strings.Contains(doc.Text(), "24-10-2026 14:45") {
			t.Error("Timestamp value not shown")
		}
	})
}

func TestWizardForm_PostsPlainValues(t *testing.T) {
	e := newFormEnv(t, allRights...)
	e.seed(doctype.StateDraft)
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{ID: 42, State: doctype.StateDraft, Version: 4}, nil)

	rec := e.post(t, doc42+"/edit/details", url.Values{"supplier_id": {"7"}, "order_date": {"27-03-2026"}}, "HX-Request", "true")
	if rec.Code != http.StatusOK {
		t.Fatalf("details: status = %d, body %.300s", rec.Code, rec.Body)
	}
	if got := pathOf(t, rec.Header().Get("HX-Push-Url")); got != doc42+"/edit/items" {
		t.Errorf("details: HX-Push-Url = %q, want %s/edit/items", got, doc42)
	}
	calls := e.docs.Calls()
	if len(calls) != 1 || calls[0].Action != doctype.ActionSave || calls[0].ID != 42 || calls[0].Input.Version != 3 {
		t.Fatalf("details: engine calls = %+v, want one save of 42 at version 3", calls)
	}
	if v := calls[0].Input.Values; v["order_date"] != "2026-03-27" || v["supplier_id"] != "7" {
		t.Errorf("details: Values = %q, want order_date 2026-03-27 and supplier_id 7", v)
	}

	// return=check goes straight back to the check page.
	rec = e.post(t, doc42+"/edit/details", url.Values{"supplier_id": {"7"}, "order_date": {"27-03-2026"}, "return": {"check"}}, "HX-Request", "true")
	if got := pathOf(t, rec.Header().Get("HX-Push-Url")); rec.Code != http.StatusOK || got != doc42+"/edit/check" {
		t.Errorf("return=check: status %d, HX-Push-Url %q, want 200 and %s/edit/check", rec.Code, got, doc42)
	}

	// A line's typed rate is saved in Rate's plain form.
	rec = e.post(t, doc42+"/lines/1", url.Values{"item_id": {"3"}, "qty": {"1,200"}, "rate": {"62.5"}})
	if got := location(t, rec); got != doc42+"/edit/items" {
		t.Errorf("line save 303 to %q, want %s/edit/items", got, doc42)
	}
	calls = e.docs.Calls()
	if len(calls) != 3 || len(calls[2].Input.Lines) != 1 {
		t.Fatalf("line save: engine calls = %+v, want a third call with one line", calls)
	}
	if l := calls[2].Input.Lines[0]; l["rate"] != "62.50" || l["id"] != "1" {
		t.Errorf("line save: line = %q, want rate 62.50 (Rate.Plain) and id 1", l)
	}

	// A money value NUM can't parse is a 422 with NUM's text; DOC is not called.
	_, perr := types.ParseMoney("1.234,56")
	if perr == nil {
		t.Fatal("ParseMoney(1.234,56) parsed")
	}
	docscreentest.Seed(e.docs, draftInfo(claimType, "Expense Claim", 50), doc.Record{Version: 3, Values: map[string]string{"claim_date": "2026-10-24"}})
	before := len(e.docs.Calls())
	rec = e.post(t, "/doc/"+claimType+"/50/edit/details", url.Values{"claim_date": {"24-10-2026"}, "total": {"1.234,56"}}, "HX-Request", "true")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("money parse error: status = %d, want 422; body %.300s", rec.Code, rec.Body)
	}
	page := uitest.Doc(t, rec)
	if !strings.Contains(norm(page.Text()), perr.Error()) {
		t.Errorf("422 body lacks NUM's text %q", perr.Error())
	}
	if link := page.Find(`.error-summary a[href="#f-total"]`); !strings.Contains(norm(link.Text()), perr.Error()) {
		t.Errorf("error summary has no link to #f-total with NUM's text")
	}
	if n := len(e.docs.Calls()); n != before {
		t.Errorf("engine called on a parse error: %+v", e.docs.Calls()[before:])
	}
}

func TestWizardForm_NewCreatesDraftAndAdvances(t *testing.T) {
	const warn = "Supplier SHREE is on hold."
	e := newFormEnv(t, append(allRights, "create")...)
	e.seed(doctype.StateDraft)
	e.docs.SetResult(docscreentest.Sample, 0, doctype.ActionSave, doc.Result{ID: 42, State: doctype.StateDraft, Version: 1, Warnings: []string{warn}}, nil)

	rec := e.get(t, samplePath+"/new")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET new: status = %d, body %.300s", rec.Code, rec.Body)
	}
	page := uitest.Doc(t, rec)
	if got := stepCount(page.Selection); got != "Step 1 of 3" {
		t.Errorf("GET new: stepper = %q, want Step 1 of 3", got)
	}
	if n := page.Find(".stepper a").Length(); n != 0 {
		t.Errorf("GET new: %d stepper links; Furthest 0 makes no step a link", n)
	}
	if title := norm(page.Find(".wizard-title").Text()); strings.Contains(title, "Draft") || strings.Contains(title, "·") {
		t.Errorf("GET new: title line %q carries a draft label; a new document has none", title)
	}
	if stepForm(page, samplePath+"/new").Length() != 1 {
		t.Errorf("GET new: the step form does not post to %s/new", samplePath)
	}

	form := url.Values{"csrf_token": {csrfToken}, "supplier_id": {"1"}, "order_date": {"24-10-2026"}}
	rec = e.do(t, http.MethodPost, samplePath+"/new", form, "HX-Request", "true")
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx POST new: status = %d, body %.300s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("HX-Push-Url"); pathOf(t, got) != "/doc/docscreentest.Sample/42/edit/items" {
		t.Errorf("HX-Push-Url = %q, want /doc/docscreentest.Sample/42/edit/items", got)
	}
	if ids := rootIDs(t, rec); !slices.Equal(ids, []string{"wizard"}) {
		t.Errorf("htmx response roots = %q, want only #wizard", ids)
	}
	next := uitest.Doc(t, rec)
	if got := norm(next.Find("h1#step-title").Text()); got != "Items" {
		t.Errorf("next step heading = %q, want Items", got)
	}
	if got := bannerTexts(next.Selection, "warning"); !slices.Equal(got, []string{warn}) {
		t.Errorf("warning banners = %q, want [%q] (Result.Warnings, CD-68)", got, warn)
	}
	calls := e.docs.Calls()
	if len(calls) != 1 || calls[0].Action != doctype.ActionSave || calls[0].ID != 0 {
		t.Fatalf("engine calls = %+v, want one save with id 0", calls)
	}
	if v := calls[0].Input.Values; v["supplier_id"] != "1" || v["order_date"] != "2026-10-24" {
		t.Errorf("Values = %q, want supplier_id 1 and order_date 2026-10-24", v)
	}

	rec = e.do(t, http.MethodPost, samplePath+"/new", form)
	if got := location(t, rec); got != "/doc/docscreentest.Sample/42/edit/items" {
		t.Errorf("plain POST new: 303 to %q", got)
	}

	// Without create the new form is AUTH's 403 and nothing is saved.
	e2 := newFormEnv(t, allRights...)
	rec = e2.do(t, http.MethodPost, samplePath+"/new", form)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST new without create: status = %d, want 403", rec.Code)
	}
	if calls := e2.docs.Calls(); len(calls) != 0 {
		t.Errorf("POST new without create: engine calls = %+v", calls)
	}
}

func TestWizardForm_GetIsFullPageWithoutHXRequest(t *testing.T) {
	e := newFormEnv(t, allRights...)
	e.seed(doctype.StateDraft)

	rec := e.get(t, doc42+"/edit/items")
	if rec.Code != http.StatusOK {
		t.Fatalf("plain GET: status = %d, body %.300s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "<html") {
		t.Error("plain GET is not a full page")
	}
	if !slices.Contains(rec.Header().Values("Vary"), "HX-Request") {
		t.Errorf("plain GET: Vary = %q", rec.Header().Values("Vary"))
	}
	page := uitest.Doc(t, rec)
	if got := stepCount(page.Selection); got != "Step 2 of 3" {
		t.Errorf("stepper = %q, want Step 2 of 3", got)
	}
	if got := norm(page.Find("h1#step-title").Text()); got != "Items" {
		t.Errorf("heading = %q, want Items", got)
	}

	rec = e.get(t, doc42+"/edit/items", "HX-Request", "true")
	if rec.Code != http.StatusOK {
		t.Fatalf("htmx GET: status = %d", rec.Code)
	}
	body := strings.TrimSpace(rec.Body.String())
	if strings.Contains(body, "<html") || !strings.HasPrefix(body, `<div id="wizard"`) {
		t.Errorf("htmx GET is not the #wizard fragment: %.200s", body)
	}
	if !slices.Contains(rec.Header().Values("Vary"), "HX-Request") {
		t.Errorf("htmx GET: Vary = %q", rec.Header().Values("Vary"))
	}
}

func TestWizardForm_NonDraftRedirectsToDocument(t *testing.T) {
	e := newFormEnv(t, allRights...)
	e.seed(doctype.StatePending)
	if got := location(t, e.get(t, doc42+"/edit/details")); got != doc42 {
		t.Errorf("non-draft edit step 303 to %q, want %s", got, doc42)
	}
}

func TestWizardForm_InvalidKeepsPostedValues(t *testing.T) {
	const msg = "Order date is required."
	e := newFormEnv(t, allRights...)
	e.seed(doctype.StateDraft)
	docscreentest.SetMaster(docscreentest.Supplier, 8, doctype.MasterInfo{Code: "GANESH", Name: "Ganesh Hardware"})
	invalid := statusErr{status: http.StatusUnprocessableEntity, msg: "Fix the highlighted fields and save again.", fields: []doctype.FieldError{{Field: "order_date", Message: msg}}}
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{}, invalid)

	rec := e.post(t, doc42+"/edit/details", url.Values{"supplier_id": {"8"}, "order_date": {""}, "remarks": {"Deliver to gate 2"}}, "HX-Request", "true")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body %.300s", rec.Code, rec.Body)
	}
	if h := rec.Header().Get("HX-Push-Url"); h != "" {
		t.Errorf("a 422 pushes %q", h)
	}
	if calls := e.docs.Calls(); len(calls) != 1 || calls[0].Action != doctype.ActionSave {
		t.Fatalf("engine calls = %+v, want one save", calls)
	}
	page := uitest.Doc(t, rec)
	link := page.Find(`.error-summary a[href="#f-order_date"]`)
	if link.Length() != 1 || norm(link.Text()) != msg {
		t.Errorf("error summary link to #f-order_date = %q, want %q", norm(link.Text()), msg)
	}
	if v, _ := page.Find(`#wizard input[name="supplier_id"]`).Attr("value"); v != "8" {
		t.Errorf("supplier_id = %q, want the posted 8 (not the stored 7)", v)
	}
	if got := page.Find(`#wizard textarea[name="remarks"]`).Text(); got != "Deliver to gate 2" {
		t.Errorf("remarks = %q, want the typed text", got)
	}
}

func TestWizard_ConflictKeepsTypedValues(t *testing.T) {
	const msg = "Someone else changed Draft 42 after you opened it. Reload it and make your changes again."
	e := newFormEnv(t, allRights...)
	e.seed(doctype.StateDraft)
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{}, statusErr{status: http.StatusConflict, msg: msg})

	rec := e.post(t, doc42+"/edit/details", url.Values{"supplier_id": {"7"}, "order_date": {"24-10-2026"}, "remarks": {"Deliver to gate 2"}}, "HX-Request", "true")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body %.300s", rec.Code, rec.Body)
	}
	page := uitest.Doc(t, rec)
	if got := bannerTexts(page.Selection, "danger"); !slices.Contains(got, msg+" Reload") && !slices.Contains(got, msg) {
		t.Errorf("danger banners = %q, want DOC's text %q", got, msg)
	}
	reload := buttonsNamed(page.Find(".banner--danger"), "Reload")
	if href, _ := reload.Attr("href"); reload.Length() != 1 || pathOf(t, href) != doc42+"/edit/details" {
		t.Errorf("Reload link href = %q, want the step URL %s/edit/details", href, doc42)
	}
	if got := page.Find(`#wizard textarea[name="remarks"]`).Text(); got != "Deliver to gate 2" {
		t.Errorf("remarks = %q, want the typed text kept", got)
	}
	if calls := e.docs.Calls(); len(calls) != 1 || calls[0].Input.Version != 3 {
		t.Errorf("engine calls = %+v, want one save at the posted version 3", calls)
	}
}

func TestWizardForm_BackSavesWithoutValidating(t *testing.T) {
	const ship = "/doc/" + shipType + "/60"
	for _, hx := range []bool{true, false} {
		e := newFormEnv(t, allRights...)
		docscreentest.Seed(e.docs, draftInfo(shipType, "Shipment", 60), doc.Record{Version: 3, Values: map[string]string{"ref": "SHP-1"}})
		// DOC accepts the draft as typed (a draft may be incomplete); a DOC
		// field error on Back is TestWizardForm_BackOn422StaysOnStep.
		e.docs.SetResult(shipType, 60, doctype.ActionSave, doc.Result{ID: 60, State: doctype.StateDraft, Version: 4}, nil)
		var hdr []string
		if hx {
			hdr = []string{"HX-Request", "true"}
		}
		rec := e.post(t, ship+"/edit/delivery", url.Values{"nav": {"back"}, "deliver_on": {""}, "deliver_to": {"Gate 2"}}, hdr...)

		calls := e.docs.Calls()
		if len(calls) != 1 || calls[0].Action != doctype.ActionSave || calls[0].Input.Version != 3 {
			t.Fatalf("htmx=%v: engine calls = %+v, want one save at version 3", hx, calls)
		}
		if got := calls[0].Input.Values["deliver_to"]; got != "Gate 2" {
			t.Errorf("htmx=%v: saved deliver_to = %q, want the typed Gate 2", hx, got)
		}
		if !hx {
			if got := location(t, rec); got != ship+"/edit/details" {
				t.Errorf("plain back: 303 to %q, want %s/edit/details", got, ship)
			}
			continue
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("htmx back: status = %d, want 200; body %.300s", rec.Code, rec.Body)
		}
		if got := pathOf(t, rec.Header().Get("HX-Push-Url")); got != ship+"/edit/details" {
			t.Errorf("htmx back: HX-Push-Url = %q, want %s/edit/details", got, ship)
		}
		page := uitest.Doc(t, rec)
		if got := norm(page.Find("h1#step-title").Text()); got != "Details" {
			t.Errorf("htmx back: heading = %q, want Details", got)
		}
		if page.Find(".error-summary").Length() != 0 {
			t.Error("htmx back shows an error summary; back saves without validating")
		}
		if v, _ := page.Find(`#wizard input[name="ref"]`).Attr("value"); v != "SHP-1" {
			t.Errorf("htmx back: ref = %q, want the draft's SHP-1", v)
		}
	}
}

func TestWizardForm_FinishLater(t *testing.T) {
	e := newFormEnv(t, allRights...)
	e.seed(doctype.StateDraft)
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{ID: 42, State: doctype.StateDraft, Version: 4}, nil)
	rec := e.post(t, doc42+"/edit/details", url.Values{"nav": {"finish"}, "supplier_id": {"7"}, "order_date": {"24-10-2026"}}, "HX-Request", "true")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %.300s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("HX-Redirect"); got != "/doc/docscreentest.Sample/42" {
		t.Errorf("HX-Redirect = %q, want /doc/docscreentest.Sample/42", got)
	}
	if calls := e.docs.Calls(); len(calls) != 1 || calls[0].Action != doctype.ActionSave || calls[0].Input.Version != 3 {
		t.Errorf("engine calls = %+v, want one save at version 3", calls)
	}
	rec = e.post(t, doc42+"/edit/details", url.Values{"nav": {"finish"}, "supplier_id": {"7"}, "order_date": {"24-10-2026"}})
	if got := location(t, rec); got != doc42 {
		t.Errorf("plain finish: 303 to %q, want %s", got, doc42)
	}
}

func TestSubmit_FailureGoesToFirstFailingStep(t *testing.T) {
	cases := []struct {
		name   string
		fields []doctype.FieldError
		slug   string
		count  string
		head   string
		msg    string
	}{
		{"line error", []doctype.FieldError{{Field: "qty", Line: 2, Message: "Quantity must be more than zero."}}, "items", "Step 2 of 3", "Items", "Quantity must be more than zero."},
		{"header error", []doctype.FieldError{{Field: "order_date", Message: "Order date is required."}}, "details", "Step 1 of 3", "Details", "Order date is required."},
		{"earliest step wins", []doctype.FieldError{
			{Field: "qty", Line: 2, Message: "Quantity must be more than zero."},
			{Field: "order_date", Message: "Order date is required."},
		}, "details", "Step 1 of 3", "Details", "Order date is required."},
		// DOC's no-lines error is on the lines as a whole (Field "lines",
		// Line 0); it belongs to the Items step.
		{"no lines", []doctype.FieldError{{Field: "lines", Line: 0, Message: "Add at least one line."}}, "items", "Step 2 of 3", "Items", "Add at least one line."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newFormEnv(t, allRights...)
			rec := sampleRecord()
			rec.Lines = append(rec.Lines, map[string]string{"id": "2", "item_id": "3", "qty": "5.000", "rate": "62.5000"})
			docscreentest.Seed(e.docs, sampleInfo(42, doctype.StateDraft), rec)
			invalid := statusErr{status: http.StatusUnprocessableEntity, msg: "Fix the highlighted fields and save again.", fields: c.fields}
			e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSubmit, doc.Result{}, invalid)

			// The check page's submit form says where its htmx response goes.
			check := uitest.Doc(t, e.get(t, doc42+"/edit/check"))
			submit := buttonsNamed(check.Selection, "Submit")
			if submit.Length() != 1 {
				t.Fatalf("check page has %d Submit buttons, want 1", submit.Length())
			}
			target, _ := formOf(check, submit).Attr("hx-target")

			res := e.post(t, doc42+"/submit", nil, "HX-Request", "true")
			if res.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body %.300s", res.Code, res.Body)
			}
			if got := pathOf(t, res.Header().Get("HX-Push-Url")); got != doc42+"/edit/"+c.slug {
				t.Errorf("HX-Push-Url = %q, want %s/edit/%s", got, doc42, c.slug)
			}
			if target != "" {
				if ids := rootIDs(t, res); !slices.Equal(ids, []string{strings.TrimPrefix(target, "#")}) {
					t.Errorf("response roots %q don't replace the submit form's hx-target %q", ids, target)
				}
			}
			page := uitest.Doc(t, res)
			if got := stepCount(page.Selection); got != c.count {
				t.Errorf("stepper = %q, want %q", got, c.count)
			}
			if got := norm(page.Find("h1#step-title").Text()); got != c.head {
				t.Errorf("heading = %q, want %q", got, c.head)
			}
			items := page.Find(".error-summary li").Map(func(_ int, s *goquery.Selection) string { return norm(s.Text()) })
			if !slices.Contains(items, c.msg) {
				t.Errorf("error summary = %q, want it to hold %q", items, c.msg)
			}
			if calls := e.docs.Calls(); len(calls) != 1 || calls[0].Action != doctype.ActionSubmit || calls[0].Input.Version != 3 {
				t.Errorf("engine calls = %+v, want one submit at version 3", calls)
			}

			// Without htmx the same step is the full 422 page.
			full := e.post(t, doc42+"/submit", nil)
			if full.Code != http.StatusUnprocessableEntity || !strings.Contains(full.Body.String(), "<html") {
				t.Fatalf("plain submit: status %d, full page %v; want a 422 full page", full.Code, strings.Contains(full.Body.String(), "<html"))
			}
			if got := norm(uitest.Doc(t, full).Find("h1#step-title").Text()); got != c.head {
				t.Errorf("plain submit heading = %q, want %q", got, c.head)
			}
		})
	}
}
