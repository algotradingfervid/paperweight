package expenseweb_test

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	expenseweb "github.com/vardhan/internaltools/internal/modules/expense/web"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// MORE (AD-11 ruling, CD-359 (7), UI v2 §5.5 item 3): the claim's Remarks,
// the claim line's Note and the advance line's Note, Project and Site are
// Field.More. Claim details and the claim line page are custom bodies that
// compose components.MoreDetails as docscreen's generic pages do (the
// advance line page): closed when empty, open on a value or an error, the
// summary naming the fields inside, a hidden field never inside or named.

const moreLong = "x" // repeated past a field's MaxLen for a 422

// moreEnv is s2New with each module's registered Hide also hiding hide.
func moreEnv(t *testing.T, hide ...string) *s2Env {
	t.Helper()
	e := expensetest.New(t)
	d := expenseweb.Deps{Service: e.Service, Engine: e.Engine, Auth: e.Auth, Files: e.Files}
	wrap := func(m docscreen.Module) docscreen.Module {
		real := m.Hide
		m.Hide = func(c docscreen.Ctx, f doctype.Field) bool {
			return slices.Contains(hide, f.Name) || real(c, f)
		}
		return m
	}
	ds := docscreen.Deps{
		DB: e.DB, Engine: e.Engine, Auth: e.Auth, Extras: s2Extras{e},
		Today: func() (string, string) { return s2Today, s2Yesterday },
		Modules: map[string]docscreen.Module{
			expense.ClaimDocType:   wrap(expenseweb.ClaimModule(d)),
			expense.AdvanceDocType: wrap(expenseweb.AdvanceModule(d)),
		},
	}
	r := chi.NewRouter()
	docscreen.Routes(ds)(r)
	expenseweb.Routes(d)(r)
	return &s2Env{Env: e, router: r}
}

// moreGroups are the More details groups in s.
func moreGroups(s *goquery.Selection) *goquery.Selection {
	return s.Find("details").FilterFunction(func(_ int, d *goquery.Selection) bool {
		return strings.HasPrefix(s2Text(d.ChildrenFiltered("summary")), "More details")
	})
}

// moreOne is s's one More details group.
func moreOne(t *testing.T, what string, s *goquery.Selection) *goquery.Selection {
	t.Helper()
	g := moreGroups(s)
	if g.Length() != 1 {
		t.Fatalf("%s: %d More details groups, want 1", what, g.Length())
	}
	return g
}

// moreWant checks s's group: summary, open, the inputs named inside and
// nowhere else in s, and the error shown inside when errOn is set.
func moreWant(t *testing.T, what string, s *goquery.Selection, summary string, open bool, inside []string, errOn string) {
	t.Helper()
	g := moreOne(t, what, s)
	if got := s2Text(g.ChildrenFiltered("summary")); got != summary {
		t.Errorf("%s: summary %q, want %q", what, got, summary)
	}
	if s2Open(g) != open {
		t.Errorf("%s: open = %v, want %v", what, s2Open(g), open)
	}
	for _, n := range inside {
		if in, all := g.Find(`[name="`+n+`"]`).Length(), s.Find(`[name="`+n+`"]`).Length(); in == 0 || in != all {
			t.Errorf("%s: %d of %d %s inputs inside More details, want all of at least one", what, in, all, n)
		}
	}
	if errOn != "" && s2Text(g.Find("#"+components.FieldID(errOn)+"-msg")) == "" {
		t.Errorf("%s: %s's error isn't shown inside More details", what, errOn)
	}
}

// moreFirstAfter reports whether the first <details> after the control
// named name in s is the More details group.
func moreFirstAfter(s *goquery.Selection, name string) bool {
	seen := false
	var first *goquery.Selection
	s.Find(`[name="` + name + `"], details`).EachWithBreak(func(_ int, x *goquery.Selection) bool {
		if x.AttrOr("name", "") == name {
			seen = true
			return true
		}
		if seen && goquery.NodeName(x) == "details" {
			first = x
			return false
		}
		return true
	})
	return first != nil && strings.HasPrefix(s2Text(first.ChildrenFiltered("summary")), "More details")
}

func moreVersion(t *testing.T, s *goquery.Selection) string {
	t.Helper()
	v := s.Find(`input[name="version"]`).First().AttrOr("value", "")
	if v == "" {
		t.Fatal("the form posts no version")
	}
	return v
}

// moreStepForm is the step page's form holding name.
func moreStepForm(t *testing.T, page *goquery.Document, name string) *goquery.Selection {
	t.Helper()
	f := page.Find(`[name="` + name + `"]`).First().Closest("form")
	if f.Length() != 1 {
		t.Fatalf("no form holds %s", name)
	}
	return f
}

// Claim details: Remarks in a group of its own at the end of the step,
// after the advance disclosure (UX §5.3's order); closed when blank, open
// when stored or typed and on its 422.
func TestMoreExp_ClaimRemarks(t *testing.T) {
	t.Parallel()
	e := moreEnv(t)
	deepak := e.P(t, s2Deepak)
	e.PaidAdvance(t, deepak, 100000) // draws the advance disclosure
	const summary = "More details · remarks"
	blank := e.DraftClaim(t, deepak, nil)
	path := s2ClaimPath(blank.ID) + "/edit/details"
	form := moreStepForm(t, e.page(t, s2Deepak, path), "purpose")
	moreWant(t, "blank", form, summary, false, []string{"remarks"}, "")
	if !moreFirstAfter(form, "less_advance") {
		t.Error("More details isn't the first group after the advance disclosure's Less advance")
	}
	if n := moreGroups(form).Find(`[name="purpose"], [name="claim_date"], [name="advance_id"]`).Length(); n != 0 {
		t.Errorf("%d other inputs inside More details", n)
	}

	stored := e.DraftClaim(t, deepak, expensetest.Values{"remarks": "Taxi from the station."})
	moreWant(t, "stored", moreStepForm(t, e.page(t, s2Deepak, s2ClaimPath(stored.ID)+"/edit/details"), "purpose"), summary, true, []string{"remarks"}, "")

	post := func(remarks string) *goquery.Document {
		t.Helper()
		v := moreVersion(t, moreStepForm(t, e.page(t, s2Deepak, path), "purpose"))
		rec := e.postForm(t, s2Deepak, path, url.Values{
			"version": {v}, "purpose": {"Pune Metro site visit"}, "claim_date": {"25-10-2026"}, "remarks": {remarks},
		})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status %d, want 422 (body %.300q)", rec.Code, rec.Body.String())
		}
		return s2Parse(t, rec)
	}
	// A future date: the redraw keeps Remarks closed when blank, open when typed.
	moreWant(t, "422 elsewhere", moreStepForm(t, post(""), "purpose"), summary, false, []string{"remarks"}, "")
	moreWant(t, "422 typed", moreStepForm(t, post("Taxi."), "purpose"), summary, true, []string{"remarks"}, "")
	page := post(strings.Repeat(moreLong, 501))
	moreWant(t, "422 on remarks", moreStepForm(t, page, "purpose"), summary, true, []string{"remarks"}, "remarks")
}

// A hidden Remarks is never inside or named: with nothing else in it, no
// group is drawn, even with a stored value or a posted one.
func TestMoreExp_ClaimRemarksHidden(t *testing.T) {
	t.Parallel()
	e := moreEnv(t, "remarks")
	c := e.DraftClaim(t, e.P(t, s2Deepak), expensetest.Values{"remarks": "ZqHidden"})
	path := s2ClaimPath(c.ID) + "/edit/details"
	page := e.page(t, s2Deepak, path)
	if n := moreGroups(page.Selection).Length(); n != 0 {
		t.Errorf("GET: %d More details groups with Remarks hidden, want none", n)
	}
	if page.Find(`[name="remarks"]`).Length() != 0 || strings.Contains(page.Text(), "ZqHidden") {
		t.Error("GET: the hidden Remarks is drawn")
	}
	rec := e.postForm(t, s2Deepak, path, url.Values{
		"version": {moreVersion(t, page.Selection)}, "purpose": {"Pune Metro site visit"}, "claim_date": {"25-10-2026"}, "remarks": {"ZqPosted"},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", rec.Code)
	}
	page = s2Parse(t, rec)
	if moreGroups(page.Selection).Length() != 0 || page.Find(`[name="remarks"]`).Length() != 0 {
		t.Error("422: the hidden Remarks is drawn")
	}
}

// The claim line page: Note in a group of its own where UX §5.5 puts the
// Note, right after Date and before the closed choices; Project and Site
// stay in Project or site.
func TestMoreExp_ClaimLineNote(t *testing.T) {
	t.Parallel()
	e := moreEnv(t)
	deepak := e.P(t, s2Deepak)
	const summary = "More details · note"
	c := e.DraftClaim(t, deepak, nil, expensetest.Values{}, expensetest.Values{"description": "Lunch at site canteen"})
	newLine := s2ClaimPath(c.ID) + "/lines/new"
	page := e.page(t, s2Deepak, newLine)
	form := s2LineForm(t, page)
	moreWant(t, "new line", form, summary, false, []string{"description"}, "")
	if !moreFirstAfter(form, "expense_date") {
		t.Error("More details isn't the first group after Date")
	}
	if n := moreGroups(form).Find(`[name="project_id"], [name="site_id"], [name="amount"]`).Length(); n != 0 {
		t.Errorf("%d other inputs inside More details", n)
	}
	moreWant(t, "line 1, blank", s2LineForm(t, e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/1")), summary, false, []string{"description"}, "")
	moreWant(t, "line 2, stored", s2LineForm(t, e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/2")), summary, true, []string{"description"}, "")

	post := func(note string) *goquery.Selection {
		t.Helper()
		p := s2Base(t, e.page(t, s2Deepak, newLine), "")
		p.Set("expense_date", "25-10-2026") // a 422 elsewhere
		p.Set("description", note)
		rec := e.postForm(t, s2Deepak, newLine, p)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status %d, want 422 (body %.300q)", rec.Code, rec.Body.String())
		}
		return s2LineForm(t, s2Parse(t, rec))
	}
	moreWant(t, "422 elsewhere", post(""), summary, false, []string{"description"}, "")
	moreWant(t, "422 typed", post("Taxi."), summary, true, []string{"description"}, "")
	moreWant(t, "422 on note", post(strings.Repeat(moreLong, 201)), summary, true, []string{"description"}, "description")
}

// A hidden Note is never inside or named on the claim line page.
func TestMoreExp_ClaimLineNoteHidden(t *testing.T) {
	t.Parallel()
	e := moreEnv(t, "description")
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil, expensetest.Values{"description": "ZqHidden"})
	page := e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/1")
	if n := moreGroups(page.Selection).Length(); n != 0 {
		t.Errorf("%d More details groups with Note hidden, want none", n)
	}
	if page.Find(`[name="description"]`).Length() != 0 || strings.Contains(page.Text(), "ZqHidden") {
		t.Error("the hidden Note is drawn")
	}
}

// moreAdvLine is advance id's line no page form (the generic line page).
func moreAdvLine(t *testing.T, e *s2Env, id int64, no string) *goquery.Selection {
	t.Helper()
	page := e.page(t, s2Deepak, "/doc/"+expense.AdvanceDocType+"/"+strconv.FormatInt(id, 10)+"/lines/"+no)
	return moreStepForm(t, page, "line_amount")
}

// The advance line page (docscreen's generic one): Note, Project and Site
// in one group after Type and Amount; each opens it when stored and on its
// 422; nothing else does.
func TestMoreExp_AdvanceLine(t *testing.T) {
	t.Parallel()
	e := moreEnv(t)
	deepak := e.P(t, s2Deepak)
	const summary = "More details · note, project, site"
	inside := []string{"description", "project_id", "site_id"}
	project, site := strconv.FormatInt(e.F.Projects[0].ID, 10), strconv.FormatInt(e.F.Sites[0].ID, 10)
	adv := e.DraftAdvance(t, deepak, nil, nil,
		expensetest.Values{"description": "Bus fare"}, expensetest.Values{"project_id": project}, expensetest.Values{"site_id": site})
	moreWant(t, "line 1, blank", moreAdvLine(t, e, adv.ID, "1"), summary, false, inside, "")
	for no, name := range map[string]string{"2": "description", "3": "project_id", "4": "site_id"} {
		moreWant(t, "line "+no+", stored "+name, moreAdvLine(t, e, adv.ID, no), summary, true, inside, "")
	}
	form := moreAdvLine(t, e, adv.ID, "new")
	moreWant(t, "new line", form, summary, false, inside, "")
	if n := moreGroups(form).Find(`[name="expense_type_id"], [name="line_amount"]`).Length(); n != 0 {
		t.Errorf("%d other inputs inside More details", n)
	}

	newLine := "/doc/" + expense.AdvanceDocType + "/" + strconv.FormatInt(adv.ID, 10) + "/lines/new"
	for _, tc := range []struct{ name, value string }{
		{"description", strings.Repeat(moreLong, 201)},
		{"project_id", "999999"},
		{"site_id", "999999"},
	} {
		p := url.Values{
			"version": {moreVersion(t, moreAdvLine(t, e, adv.ID, "new"))}, "expense_type_id": {strconv.FormatInt(e.F.ExpenseTypeIDs["TRAVEL"], 10)},
			"line_amount": {"500"}, tc.name: {tc.value},
		}
		rec := e.postForm(t, s2Deepak, newLine, p)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: status %d, want 422 (body %.300q)", tc.name, rec.Code, rec.Body.String())
		}
		moreWant(t, "422 on "+tc.name, moreStepForm(t, s2Parse(t, rec), "line_amount"), summary, true, inside, tc.name)
	}
}

// A hidden advance line field is never inside, named or opening the group;
// with all three hidden there is no group.
func TestMoreExp_AdvanceLineHidden(t *testing.T) {
	t.Parallel()
	project := func(e *s2Env) string { return strconv.FormatInt(e.F.Projects[0].ID, 10) }
	e := moreEnv(t, "project_id")
	adv := e.DraftAdvance(t, e.P(t, s2Deepak), nil, expensetest.Values{"project_id": project(e)})
	form := moreAdvLine(t, e, adv.ID, "1")
	moreWant(t, "project hidden", form, "More details · note, site", false, []string{"description", "site_id"}, "")
	if form.Find(`[name="project_id"]`).Length() != 0 {
		t.Error("the hidden Project is drawn")
	}

	e = moreEnv(t, "description", "project_id", "site_id")
	adv = e.DraftAdvance(t, e.P(t, s2Deepak), nil, expensetest.Values{"description": "ZqHidden", "project_id": project(e)})
	form = moreAdvLine(t, e, adv.ID, "1")
	if n := moreGroups(form).Length(); n != 0 {
		t.Errorf("all hidden: %d More details groups, want none", n)
	}
}
