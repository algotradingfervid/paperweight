package expenseweb_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	expenseweb "github.com/vardhan/internaltools/internal/modules/expense/web"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// R12 (UI review, EXP mediums): receipts match the hint, key facts, the
// sticky footer on the advance line and Mark as paid, and the Details copy.

const (
	r12Admin = "admin@vardhan.test"

	r12MsgType      = "Receipt must be a HEIC, JPG, PDF, PNG or WEBP file."
	r12Required     = "Fields marked * are required."
	r12HintPurpose  = "Your approver reads this first."
	r12HintClaimDay = "Expenses on this claim can't be dated after it."
	r12HintAdvDay   = "The day you're asking for the money."
	r12ClaimCard    = "About this claim"
	r12ClaimDesc    = "The purpose and date print on the claim."
	r12AdvCard      = "About this advance"
	r12AdvDesc      = "The purpose and date print on the advance."
	r12AdvIntro     = "What the advance is for and the day you're asking for it."
	r12BarSave      = `.actionbar button[form="line-form"]`
	r12Day          = s2Today // expensetest.Date as shown
)

func r12ID(name string) string { return "#" + components.FieldID(name) }

// r12Facts are the document header's labels and values, in order.
func r12Facts(page *goquery.Document) (labels, values []string) {
	page.Find(".doc-head__facts dt").Each(func(_ int, s *goquery.Selection) { labels = append(labels, s2Text(s)) })
	page.Find(".doc-head__facts dd").Each(func(_ int, s *goquery.Selection) { values = append(values, s2Text(s)) })
	return labels, values
}

func r12WantFacts(t *testing.T, what string, page *goquery.Document, labels, values []string) {
	t.Helper()
	l, v := r12Facts(page)
	if !slices.Equal(l, labels) || !slices.Equal(v, values) {
		t.Errorf("%s: key facts %q = %q, want %q = %q", what, l, v, labels, values)
	}
}

func r12DocPath(dt string, id int64) string { return "/doc/" + dt + "/" + strconv.FormatInt(id, 10) }

// §5.5: only what the hint promises is a receipt. A CSV, XLSX or DOCX is
// refused with the module's own text, naming what is allowed, in any case
// of extension, and nothing is attached, so no file tile or "n photos" count
// follows. Every allowed type still goes in. The shared default keeps its
// list (FILE-006, FILE-007).
func TestR12_ReceiptTypes(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	deepak := e.P(t, s2Deepak)
	c := e.DraftClaim(t, deepak, nil)
	if _, err := e.EditClaim(t, deepak, c.ID, expensetest.Values{"purpose": "Site visit, Pune"}, nil); err != nil {
		t.Fatalf("edit claim: %v", err)
	}
	id := strconv.FormatInt(c.ID, 10)
	v := strconv.Itoa(e.version(t, c.ID))
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"sheet.csv", []byte("a,b\n1,2\n")},
		{"SHEET.CSV", []byte("a,b\n1,2\n")},
		{"rates.xlsx", []byte("PK\x03\x04 not really a workbook")},
		{"quote.docx", []byte("PK\x03\x04 not really a document")},
		{"README", []byte("plain text")},
	} {
		e.refused(t, tc.name, http.StatusUnprocessableEntity, r12MsgType, func() *httptest.ResponseRecorder {
			return e.upload(t, s2Deepak, id, v, s2File(tc.name, tc.body))
		})
	}
	if got := e.FilesOf(t, expense.ClaimDocType, c.ID); len(got) != 0 {
		t.Fatalf("after the refusals the claim has %d attachments, want 0", len(got))
	}

	// One real photo: the check page counts it, and only it.
	if rec := e.upload(t, s2Deepak, id, v, s2File("bill.jpg", filestest.JPEG(4096))); rec.Code != http.StatusOK {
		t.Fatalf("a JPEG: %d %s", rec.Code, rec.Body)
	}
	if rec := e.upload(t, s2Deepak, id, v, s2File("sheet.csv", []byte("a,b\n1,2\n"))); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a CSV after a JPEG: %d %s", rec.Code, rec.Body)
	}
	check := s2Text(e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/edit/check").Selection)
	if !strings.Contains(check, "1 photo isn't on any expense. Remove it below, or use it on an expense.") {
		t.Errorf("check page: no one-photo warning in %q", check)
	}
	if strings.Contains(check, "2 photos") || strings.Contains(check, "sheet.csv") {
		t.Errorf("check page counts the CSV: %q", check)
	}

	for _, name := range []string{"a.pdf", "a.jpg", "a.jpeg", "a.png", "a.webp", "a.heic", "a.heif"} {
		if !slices.Contains(expense.ClaimPolicy.Exts, name[1:]) {
			t.Errorf("claim policy refuses %s, the hint's %q", name, expense.ClaimPolicy.Exts)
		}
	}
	if len(expense.ClaimPolicy.Exts) != 7 {
		t.Errorf("claim policy types = %q, want the 7 photo and PDF types", expense.ClaimPolicy.Exts)
	}
	if !slices.Contains(files.DefaultPolicy.Exts, ".csv") || !slices.Contains(files.DefaultPolicy.Exts, ".xlsx") {
		t.Errorf("the shared default changed: %q", files.DefaultPolicy.Exts)
	}
}

// §5.13 item 3: the claim's and the advance's header facts, in order, with
// exact values. The employee follows Module.Hide: a non-admin reading a
// draft doesn't get it in the facts; past draft everyone does. The Advance
// shows to a reader of the advance and to no one else (CD-392 (9), CD-321).
func TestR12_KeyFacts(t *testing.T) {
	t.Parallel()
	d := expenseweb.Deps{}
	if got, want := expenseweb.ClaimModule(d).KeyFacts, []string{"employee_id", "claim_date", "purpose", "advance_id"}; !slices.Equal(got, want) {
		t.Errorf("claim KeyFacts = %q, want %q", got, want)
	}
	if got, want := expenseweb.AdvanceModule(d).KeyFacts, []string{"employee_id", "advance_date", "purpose"}; !slices.Equal(got, want) {
		t.Errorf("advance KeyFacts = %q, want %q", got, want)
	}

	e := s2New(t)
	deepak := e.P(t, s2Deepak)
	draft := e.DraftClaim(t, deepak, expensetest.Values{"purpose": "Site visit, Pune"})
	r12WantFacts(t, "a reader on Deepak's draft", e.page(t, s2Priya, r12DocPath(expense.ClaimDocType, draft.ID)),
		[]string{"Claim date", "Purpose"}, []string{r12Day, "Site visit, Pune"})

	paid := e.PaidAdvance(t, deepak, 1000000)
	claim := e.ApprovedClaim(t, deepak, expensetest.Values{"purpose": "Site visit, Pune"})
	r12WantFacts(t, "an approved claim with no advance", e.page(t, s2Deepak, r12DocPath(expense.ClaimDocType, claim.ID)),
		[]string{"Employee", "Claim date", "Purpose"}, []string{"EMP-007 Deepak Nair", r12Day, "Site visit, Pune"})
	set := e.ApprovedClaim(t, deepak, expensetest.Values{
		"purpose": "Set against", "advance_id": strconv.FormatInt(paid.ID, 10), "less_advance": "100.00",
	})
	setPath := r12DocPath(expense.ClaimDocType, set.ID)
	for _, who := range []string{s2Deepak, s2Priya} { // the owner and his manager read the advance
		r12WantFacts(t, who+" on a claim set against an advance", e.page(t, who, setPath),
			[]string{"Employee", "Claim date", "Purpose", "Advance"}, []string{"EMP-007 Deepak Nair", r12Day, "Set against", "EA/26-27/00001"})
	}

	adv := e.DraftAdvance(t, deepak, expensetest.Values{"purpose": "Nagpur trip"})
	r12WantFacts(t, "a reader on the draft advance", e.page(t, s2Priya, r12DocPath(expense.AdvanceDocType, adv.ID)),
		[]string{"Advance date", "Purpose"}, []string{r12Day, "Nagpur trip"})
	r12WantFacts(t, "the paid advance", e.page(t, s2Deepak, r12DocPath(expense.AdvanceDocType, paid.ID)),
		[]string{"Employee", "Advance date", "Purpose"}, []string{"EMP-007 Deepak Nair", r12Day, "Pune Metro site visit"})

	// CD-321: a viewer who can read the claim but not the advance gets no
	// Advance fact, and its number shows nowhere on the page.
	e.S4RevokeRead(t, s2Priya, expense.AdvanceDocType)
	page := e.page(t, s2Priya, setPath)
	r12WantFacts(t, "a viewer who can't read the advance", page,
		[]string{"Employee", "Claim date", "Purpose"}, []string{"EMP-007 Deepak Nair", r12Day, "Set against"})
	if strings.Contains(s2Text(page.Selection), "EA/26-27/00001") {
		t.Error("the page names the advance to a viewer who can't read it")
	}
}

// r12AdvVersion is advance id's stored version as posted.
func r12AdvVersion(t *testing.T, e *s2Env, id int64) string {
	t.Helper()
	return strconv.FormatInt(e.id(t, `SELECT version FROM employee_advance WHERE id = ?`, id), 10)
}

// §5.6, CD-387 (6): the advance's line page is docscreen's general line page
// with its sticky footer and wizard head: Save line sits in the bar and posts
// the form, which has no button of its own. The form saves, keeps what was typed on a refusal, and
// a change keeps the stored line.
func TestR12_AdvanceLineFooter(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	deepak := e.P(t, s2Deepak)
	adv := e.DraftAdvance(t, deepak, expensetest.Values{"purpose": "Nagpur trip"})
	path := r12DocPath(expense.AdvanceDocType, adv.ID) + "/lines"
	wantBar := func(what string, page *goquery.Document) {
		t.Helper()
		if got := s2Text(page.Find(r12BarSave)); got != "Save line" {
			t.Errorf("%s: bar's button = %q, want Save line", what, got)
		}
		if n := page.Find("form#line-form").Length(); n != 1 {
			t.Errorf("%s: %d #line-form, want 1", what, n)
		}
		if n := page.Find("form#line-form button[type=submit]").Length(); n != 0 {
			t.Errorf("%s: the form still has %d submit buttons", what, n)
		}
		// CD-387 (6): the general line page's wizard head ‹ goes back to Items.
		if href := page.Find("#main .wizard-head a.wizard-close").AttrOr("href", ""); href != r12DocPath(expense.AdvanceDocType, adv.ID)+"/edit/items" {
			t.Errorf("%s: ‹ goes to %q, want the Items step", what, href)
		}
	}
	page := e.page(t, s2Deepak, path+"/new")
	wantBar("new line", page)
	typeID := strconv.FormatInt(e.id(t, `SELECT id FROM expense_type WHERE code = ?`, "TRAVEL"), 10)

	// A parse error keeps every typed value and stays on the page.
	rec := e.postForm(t, s2Deepak, path+"/new", url.Values{
		"version": {r12AdvVersion(t, e, adv.ID)}, "expense_type_id": {typeID},
		"description": {"Bus fare"}, "line_amount": {"abc"},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad amount: %d %s", rec.Code, rec.Body)
	}
	refused := s2Parse(t, rec)
	wantBar("the 422", refused)
	if got := refused.Find(r12ID("line_amount")).AttrOr("value", ""); got != "abc" {
		t.Errorf("422 Amount = %q, want the typed abc", got)
	}
	if got := refused.Find(r12ID("description")).AttrOr("value", ""); got != "Bus fare" {
		t.Errorf("422 Note = %q, want the typed Bus fare", got)
	}
	if got := s2Text(refused.Selection); !strings.Contains(got, `"abc" is not a valid amount.`) {
		t.Errorf("422 has no Amount error: %q", got)
	}

	rec = e.postForm(t, s2Deepak, path+"/new", url.Values{
		"version": {r12AdvVersion(t, e, adv.ID)}, "expense_type_id": {typeID},
		"description": {"Bus fare"}, "line_amount": {"6,000.00"},
	})
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	got := e.Load(t, expense.AdvanceDocType, adv.ID)
	if len(got.Lines) != 2 || got.Lines[1]["description"] != "Bus fare" || got.Lines[1]["line_amount"] != "6000.00" || got.Lines[1]["expense_type_id"] != typeID {
		t.Fatalf("saved lines = %v", got.Lines)
	}

	stored := e.page(t, s2Deepak, path+"/2")
	wantBar("line 2", stored)
	if v := stored.Find(r12ID("description")).AttrOr("value", ""); v != "Bus fare" {
		t.Errorf("line 2 Note = %q, want the stored Bus fare", v)
	}
	if v := stored.Find(r12ID("line_amount")).AttrOr("value", ""); v == "" {
		t.Error("line 2 Amount is blank")
	}
	rec = e.postForm(t, s2Deepak, path+"/2", url.Values{
		"version": {r12AdvVersion(t, e, adv.ID)}, "expense_type_id": {typeID},
		"description": {"Bus and lodging"}, "line_amount": {"6,500.00"},
	})
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusOK {
		t.Fatalf("change: %d %s", rec.Code, rec.Body)
	}
	got = e.Load(t, expense.AdvanceDocType, adv.ID)
	if len(got.Lines) != 2 || got.Lines[0]["line_amount"] != "5000.00" || got.Lines[1]["description"] != "Bus and lodging" || got.Lines[1]["line_amount"] != "6500.00" {
		t.Fatalf("changed lines = %v", got.Lines)
	}
}

// r12Get GETs path as email; it must be 200.
func r12Get(t *testing.T, e *webEnv, email, path string) *goquery.Document {
	t.Helper()
	req := httptest.NewRequestWithContext(auth.WithPrincipal(context.Background(), e.P(t, email)), http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d", path, rec.Code)
	}
	return s2Parse(t, rec)
}

// §5.6: Mark as paid and Undo paid use the sticky footer: the button sits
// in the bar and posts the form, on the first draw and on a refusal, and
// Undo paid keeps its danger look.
func TestR12_PaidFooter(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	adv := e.approvedAdvance(t)
	base := "/modules/expense/advances/" + strconv.FormatInt(adv.ID, 10)
	want := func(what string, page *goquery.Document, label, class string) {
		t.Helper()
		b := page.Find(`.actionbar button[form="paid-form"]`)
		if got := s2Text(b); got != label {
			t.Errorf("%s: bar's button = %q, want %q", what, got, label)
		}
		if !b.HasClass(class) {
			t.Errorf("%s: bar's button class = %q, want %s", what, b.AttrOr("class", ""), class)
		}
		if n := page.Find("form#paid-form").Length(); n != 1 {
			t.Errorf("%s: %d #paid-form, want 1", what, n)
		}
		if n := page.Find("form#paid-form button[type=submit]").Length(); n != 0 {
			t.Errorf("%s: the form still has %d submit buttons", what, n)
		}
	}
	want("Mark as paid", r12Get(t, e, webMeena, base+"/paid"), "Mark as paid", "btn--primary")
	v, _ := e.stored(t, adv.ID)
	rec := e.post(t, webMeena, adv.ID, "paid", form(v, ""))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("blank date: %d", rec.Code)
	}
	want("the 422", s2Parse(t, rec), "Mark as paid", "btn--primary")
	rec = e.post(t, webMeena, adv.ID, "paid", form(v, "21-10-2026"))
	if rec.Code >= 400 {
		t.Fatalf("mark as paid: %d %s", rec.Code, rec.Body)
	}
	want("Undo paid", r12Get(t, e, webMeena, base+"/unpaid"), "Undo paid", "btn--danger")
}

// §5.5 items 1, 2 and 5, §5.6: Claim details is a card under the required
// line; each hint says something its placeholder doesn't, and a filled date
// shows no "For example". An Administrator's code sits in the same card.
func TestR12_ClaimDetailsCopy(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	for _, tc := range []struct {
		email string
		code  bool
	}{{s2Deepak, false}, {r12Admin, true}} {
		page := e.page(t, tc.email, "/doc/"+expense.ClaimDocType+"/new")
		r12WantCard(t, tc.email, page, r12ClaimCard, r12ClaimDesc)
		if got := s2Text(page.Find(r12ID("purpose")).Closest(".field").Find(".field__hint")); got != r12HintPurpose {
			t.Errorf("%s: Purpose hint = %q", tc.email, got)
		}
		if got := s2Text(page.Find(r12ID("claim_date")).Closest(".field").Find(".field__hint").First()); got != r12HintClaimDay {
			t.Errorf("%s: Claim date hint = %q", tc.email, got)
		}
		if strings.Contains(s2Text(page.Selection), "For example") {
			t.Errorf("%s: a hint still says For example", tc.email)
		}
		if got := page.Find(".card " + r12ID("employee_id")).Length(); (got == 1) != tc.code {
			t.Errorf("%s: %d Employee code inputs in the card, want code = %v", tc.email, got, tc.code)
		}
		if got := page.Find(".card " + r12ID("remarks")).Length(); got != 1 {
			t.Errorf("%s: Remarks is outside the card", tc.email)
		}
	}
}

// §5.6, §5.5: advance Details has its intro, the required line, a card with
// the two fields and hints of their own; a refusal keeps what was typed, and
// a save goes through docscreen's own post.
func TestR12_AdvanceDetailsCopy(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	page := e.page(t, s2Deepak, "/doc/"+expense.AdvanceDocType+"/new")
	if got := s2Text(page.Find(".step-intro")); got != r12AdvIntro {
		t.Errorf("intro = %q, want %q", got, r12AdvIntro)
	}
	r12WantCard(t, "new advance", page, r12AdvCard, r12AdvDesc)
	if got := s2Text(page.Find(r12ID("purpose")).Closest(".field").Find(".field__hint")); got != r12HintPurpose {
		t.Errorf("Purpose hint = %q", got)
	}
	if got := s2Text(page.Find(r12ID("advance_date")).Closest(".field").Find(".field__hint").First()); got != r12HintAdvDay {
		t.Errorf("Advance date hint = %q", got)
	}
	if n := page.Find(".card " + r12ID("employee_id")).Length(); n != 0 {
		t.Errorf("a non-admin sees %d employee inputs", n)
	}
	if n := e.page(t, r12Admin, "/doc/"+expense.AdvanceDocType+"/new").Find(".card " + r12ID("employee_id")).Length(); n != 1 {
		t.Errorf("an Administrator sees %d employee inputs in the card, want 1", n)
	}

	deepak := e.P(t, s2Deepak)
	adv := e.DraftAdvance(t, deepak, expensetest.Values{"purpose": "Nagpur trip", "advance_date": "2026-10-20"})
	edit := r12DocPath(expense.AdvanceDocType, adv.ID) + "/edit/details"
	stored := e.page(t, s2Deepak, edit)
	if got := stored.Find(r12ID("purpose")).AttrOr("value", ""); got != "Nagpur trip" {
		t.Errorf("stored Purpose = %q", got)
	}
	if got := stored.Find(r12ID("advance_date")).AttrOr("value", ""); got != "20-10-2026" {
		t.Errorf("stored Advance date = %q", got)
	}
	rec := e.postForm(t, s2Deepak, edit, url.Values{
		"version": {r12AdvVersion(t, e, adv.ID)}, "purpose": {"Pune trip"}, "advance_date": {"31-02-2026"},
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad date: %d %s", rec.Code, rec.Body)
	}
	refused := s2Parse(t, rec)
	if got := refused.Find(r12ID("purpose")).AttrOr("value", ""); got != "Pune trip" {
		t.Errorf("422 Purpose = %q, want the typed Pune trip", got)
	}
	if got := refused.Find(r12ID("advance_date")).AttrOr("value", ""); got != "31-02-2026" {
		t.Errorf("422 Advance date = %q, want the typed value", got)
	}
	// Each error sits at its own field, and only there.
	dateErr := s2Text(refused.Find(r12ID("advance_date")).Closest(".field"))
	if !strings.Contains(dateErr, "31-02-2026 is not a valid date") {
		t.Errorf("422: Advance date's field has no date error: %q", dateErr)
	}
	if got := s2Text(refused.Find(r12ID("purpose")).Closest(".field")); strings.Contains(got, "not a valid date") {
		t.Errorf("422: Purpose's field shows the date's error: %q", got)
	}
	rec = e.postForm(t, s2Deepak, edit, url.Values{
		"version": {r12AdvVersion(t, e, adv.ID)}, "purpose": {"Pune trip"}, "advance_date": {"21-10-2026"},
	})
	if rec.Code >= 400 {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	if got := e.Load(t, expense.AdvanceDocType, adv.ID).Values["purpose"]; got != "Pune trip" {
		t.Errorf("saved Purpose = %q", got)
	}
}

// r12WantCard checks the step starts with the required line, then a card
// with heading and description holding Purpose.
func r12WantCard(t *testing.T, what string, page *goquery.Document, title, desc string) {
	t.Helper()
	note := page.Find("p.field__hint").FilterFunction(func(_ int, s *goquery.Selection) bool { return s2Text(s) == r12Required })
	if note.Length() != 1 {
		t.Fatalf("%s: %d required lines, want 1", what, note.Length())
	}
	if note.Closest(".card").Length() != 0 {
		t.Errorf("%s: the required line is inside a card", what)
	}
	card := page.Find(r12ID("purpose")).Closest(".card")
	if card.Length() != 1 {
		t.Fatalf("%s: Purpose isn't in a card", what)
	}
	if got := s2Text(card.Find(".card-head").Find("h2, h3").First()); got != title {
		t.Errorf("%s: card heading = %q, want %q", what, got, title)
	}
	if !strings.Contains(s2Text(card), desc) {
		t.Errorf("%s: card has no description %q", what, desc)
	}
}

// An Administrator with no linked employee isn't told to ask an
// administrator; anyone else still is (spec §5, EXP-003, CD-392 (6)).
func TestR12_NoEmployeeAdmin(t *testing.T) {
	t.Parallel()
	const (
		forAdmin = "Your user isn't linked to an employee. Enter the employee this is for, or link your user to an employee."
		forUser  = "Your user isn't linked to an employee. Ask your administrator."
	)
	e := s2New(t)
	ctx := context.Background()
	admin := e.P(t, r12Admin)
	for _, tc := range []struct {
		email string
		roles []string
		want  string
	}{
		{"noemp-admin@vardhan.test", []string{auth.RoleAdministrator}, forAdmin},
		{"noemp-user@vardhan.test", nil, forUser},
	} {
		uid, _, err := e.Auth.CreateUser(ctx, admin, auth.NewUser{Email: tc.email, Name: "No Employee", Password: "Correct-Horse-Battery-42", Roles: tc.roles})
		if err != nil {
			t.Fatalf("create %s: %v", tc.email, err)
		}
		p, _, err := e.Auth.LoadPrincipal(ctx, uid)
		if err != nil {
			t.Fatalf("load %s: %v", tc.email, err)
		}
		e.WantRefused(t, tc.want, func() error {
			_, err := e.Create(p, expense.ClaimDocType, e.ClaimInput(t, p, expensetest.Values{"employee_id": ""}))
			return err
		})
		e.WantRefused(t, tc.want, func() error {
			_, err := e.Create(p, expense.AdvanceDocType, e.AdvanceInput(t, p, expensetest.Values{"employee_id": ""}))
			return err
		})
	}
}
