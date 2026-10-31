package expenseweb_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	filesweb "github.com/vardhan/internaltools/internal/kernel/files/web"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	expenseweb "github.com/vardhan/internaltools/internal/modules/expense/web"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// The receipt route's body refusals beyond the committed S2 tests: a body
// over the limit, no "file" part and a body that isn't multipart, each as
// FILE's htmx notice with nothing written.
func TestExpReceipt_BodyRefusals(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil)
	id, v := strconv.FormatInt(c.ID, 10), strconv.Itoa(e.version(t, c.ID))
	big := int(files.MaxFileBytes+filesweb.MultipartOverhead) + 1
	e.refused(t, "a body over the limit", http.StatusRequestEntityTooLarge, files.MsgTooLarge, func() *httptest.ResponseRecorder {
		return e.upload(t, s2Deepak, id, v, s2File("big.jpg", filestest.JPEG(big)))
	})
	e.refused(t, "no file part", http.StatusUnprocessableEntity, "Choose a file to upload.", func() *httptest.ResponseRecorder {
		return e.upload(t, s2Deepak, id, v, s2Part{field: "other", name: "bill.jpg", body: filestest.JPEG(4096)})
	})
	e.refused(t, "not multipart", http.StatusUnprocessableEntity, "Choose a file to upload.", func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/modules/expense/claims/"+id+"/receipt?version="+v, strings.NewReader("file=x"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		return e.serve(t, s2Deepak, req)
	})
}

// A HEIC receipt shows as FILE's download chip (FILE-033), not a thumbnail.
func TestExpReceipt_HEIC(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil)
	rec := e.upload(t, s2Deepak, strconv.FormatInt(c.ID, 10), strconv.Itoa(e.version(t, c.ID)), s2File("IMG_1.heic", filestest.HEIC("heic")))
	if rec.Code != http.StatusOK {
		t.Fatalf("upload IMG_1.heic: status %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	if got := s2Text(s2Parse(t, rec).Find("#receipt-files")); !strings.Contains(got, "HEIC image, opens as download") {
		t.Errorf("#receipt-files reads %q, want the HEIC download chip", got)
	}
}

// An amount NUM can't read is Line.Save's parse error: 422 at Amount with the
// typed text kept, nothing written.
func TestExpLine_ParseError(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil)
	newLine := s2ClaimPath(c.ID) + "/lines/new"
	post := s2Base(t, e.page(t, s2Deepak, newLine), "")
	post.Set("amount", "abc")
	var rec *httptest.ResponseRecorder
	e.WantRefused(t, "-", func() error {
		rec = e.postForm(t, s2Deepak, newLine, post)
		return s2Refusal("-")
	})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("amount abc: status %d, want 422 (body %q)", rec.Code, rec.Body.String())
	}
	form := s2LineForm(t, s2Parse(t, rec))
	if got := form.Find(`input[name="amount"]`).AttrOr("value", ""); got != "abc" {
		t.Errorf("Amount is %q after the 422, want the typed abc", got)
	}
	if s2Text(form.Find("#f-amount-msg")) == "" {
		t.Errorf("Amount shows no error after the 422")
	}
}

// A refused Save line keeps the picked same receipt: the disclosure open with
// the posted line chosen (UX §5.5).
func TestExpLine_RedrawKeepsSameAs(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil)
	e.withReceipts(t, c.ID, map[int]string{1: "taxi.jpg"})
	newLine := s2ClaimPath(c.ID) + "/lines/new"
	page := e.page(t, s2Deepak, newLine)
	post := s2Base(t, page, "")
	post.Set("same_as", s2ValueOf(t, s2Options(s2LineForm(t, page), "same_as"), "Same receipt as line 1"))
	post.Set("expense_date", "25-10-2026")
	rec := e.postForm(t, s2Deepak, newLine, post)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("future date: status %d, want 422 (body %q)", rec.Code, rec.Body.String())
	}
	same := s2Details(s2LineForm(t, s2Parse(t, rec)), s2SameSummary)
	if !s2Open(same) {
		t.Errorf("after the 422 %q is closed, want open with line 1 picked", s2SameSummary)
	}
	for _, o := range s2Options(same, "same_as") {
		if o.Checked != (o.Label == "Same receipt as line 1") {
			t.Errorf("after the 422 %q checked = %v, want only line 1", o.Label, o.Checked)
		}
	}
}

// After a 409 the receipt area keeps the posted version, so an upload from
// the stale page is refused too, as Save line is.
func TestExpLine_StaleKeepsUploadVersion(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	deepak := e.P(t, s2Deepak)
	c := e.DraftClaim(t, deepak, nil)
	newLine := s2ClaimPath(c.ID) + "/lines/new"
	post := s2Base(t, e.page(t, s2Deepak, newLine), "")
	if _, err := e.EditClaim(t, deepak, c.ID, expensetest.Values{"purpose": "Site visit, Pune"}, nil); err != nil {
		t.Fatalf("edit claim: %v", err)
	}
	rec := e.postForm(t, s2Deepak, newLine, post)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale Save line: status %d, want 409 (body %q)", rec.Code, rec.Body.String())
	}
	want := "/modules/expense/claims/" + strconv.FormatInt(c.ID, 10) + "/receipt?version=" + url.QueryEscape(post.Get("version"))
	forms := s2Parse(t, rec).Find("#receipt form")
	if forms.Length() == 0 {
		t.Fatalf("the 409 page has no upload forms")
	}
	for i := range forms.Length() {
		if got := forms.Eq(i).AttrOr("hx-post", ""); got != want {
			t.Errorf("upload form %d posts to %q after the 409, want the posted version's %q", i, got, want)
		}
	}
}

// s2Checked is the value of the checked option named name in s ("" none).
func s2Checked(s *goquery.Selection, name string) string {
	for _, o := range s2Options(s, name) {
		if o.Checked {
			return o.Value
		}
	}
	return ""
}

// s2Post is the line form of page as the browser posts it: s2Base with the
// checked same_as option.
func s2Post(t *testing.T, page *goquery.Document, hash string) url.Values {
	t.Helper()
	post := s2Base(t, page, hash)
	if v := s2Checked(s2LineForm(t, page), "same_as"); v != "" {
		post.Set("same_as", v)
	}
	return post
}

// s2Meta is claim id's attachment named name.
func (e *s2Env) s2Meta(t *testing.T, id int64, name string) files.Meta {
	t.Helper()
	for _, m := range e.FilesOf(t, expense.ClaimDocType, id) {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("claim %d has no attachment %s", id, name)
	return files.Meta{}
}

// A line that owns a receipt another line shares opens with its own receipt
// chosen, so a new photo uploaded there is what Save line stores (review 1).
func TestExpLine_OwnerUploadWins(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil, nil, nil)
	bill := e.withReceipts(t, c.ID, map[int]string{1: "bill.jpg", 2: "bill.jpg"})["bill.jpg"]
	line1 := s2ClaimPath(c.ID) + "/lines/1"
	page := e.page(t, s2Deepak, line1)
	if got := s2Checked(s2LineForm(t, page), "same_as"); got != "" {
		t.Errorf("line 1, which owns bill.jpg, opens with same_as %q checked, want its own receipt", got)
	}
	if s2Open(s2Details(s2LineForm(t, page), s2SameSummary)) {
		t.Errorf("line 1 opens with %q open", s2SameSummary)
	}
	if rec := e.upload(t, s2Deepak, strconv.FormatInt(c.ID, 10), strconv.Itoa(e.version(t, c.ID)), s2File("new.jpg", s2Bytes("new.jpg"))); rec.Code != http.StatusOK {
		t.Fatalf("upload new.jpg: status %d (body %q)", rec.Code, rec.Body.String())
	}
	fresh := e.s2Meta(t, c.ID, "new.jpg")
	if rec := e.postForm(t, s2Deepak, line1, s2Post(t, page, fresh.SHA256)); rec.Code != http.StatusSeeOther {
		t.Fatalf("Save line 1: status %d, want 303 (body %q)", rec.Code, rec.Body.String())
	}
	if got := e.line(t, c.ID, 1)["receipt_sha256"]; got != fresh.SHA256 {
		t.Errorf("line 1 holds %q after the save, want new.jpg's %s", got, fresh.SHA256)
	}
	if got := e.line(t, c.ID, 2)["receipt_sha256"]; got != bill.SHA256 {
		t.Errorf("line 2 holds %q, want bill.jpg's %s unchanged", got, bill.SHA256)
	}
	// Line 2 shares line 1's receipt: its page picks line 1.
	if got := s2Checked(s2LineForm(t, e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/2")), "same_as"); got != "" {
		t.Errorf("line 2 now has its own receipt, same_as %q checked", got)
	}
}

// A same_as naming no other line's receipt (out of range, not a number, or
// a line with none) is ignored: the posted hash is saved.
func TestExpLine_ForgedSameAs(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil, nil, nil) // line 2 has no receipt
	e.withReceipts(t, c.ID, map[int]string{1: "taxi.jpg"})
	if rec := e.upload(t, s2Deepak, strconv.FormatInt(c.ID, 10), strconv.Itoa(e.version(t, c.ID)), s2File("own.jpg", s2Bytes("own.jpg"))); rec.Code != http.StatusOK {
		t.Fatalf("upload own.jpg: status %d", rec.Code)
	}
	own := e.s2Meta(t, c.ID, "own.jpg")
	newLine := s2ClaimPath(c.ID) + "/lines/new"
	for i, forged := range []string{"99", "0", "x", "2"} {
		post := s2Base(t, e.page(t, s2Deepak, newLine), own.SHA256)
		post.Set("same_as", forged)
		if rec := e.postForm(t, s2Deepak, newLine, post); rec.Code != http.StatusSeeOther {
			t.Fatalf("same_as %s: status %d, want 303 (body %q)", forged, rec.Code, rec.Body.String())
		}
		if got := e.line(t, c.ID, 3+i)["receipt_sha256"]; got != own.SHA256 {
			t.Errorf("same_as %s: line %d holds %q, want the posted %s", forged, 3+i, got, own.SHA256)
		}
	}
}

// #receipt-files shows only this line's receipt, not the claim's other files.
func TestExpLine_ReceiptFilesOnlyThisLine(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil, nil, nil)
	metas := e.withReceipts(t, c.ID, map[int]string{1: "taxi.jpg", 2: "bill.jpg"})
	rf := e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/2").Find("#receipt-files")
	if n := rf.Find("a").Length(); n != 1 {
		t.Errorf("line 2's #receipt-files has %d links, want only its own receipt", n)
	}
	if rf.Find(`a[href="/files/`+strconv.FormatInt(metas["bill.jpg"].ID, 10)+`"]`).Length() != 1 {
		t.Errorf("line 2's #receipt-files doesn't show bill.jpg")
	}
	if strings.Contains(s2Text(rf), "taxi.jpg") {
		t.Errorf("line 2's #receipt-files shows line 1's taxi.jpg")
	}
}

// The disclosure open rules after a refused Save line (a future date, so
// the posted values are redrawn): the GSTIN tick alone, a value in it or a
// GST error opens Bill in company GSTIN; a project opens Project or site.
func TestExpLine_DisclosureOpenRules(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil)
	newLine := s2ClaimPath(c.ID) + "/lines/new"
	project := strconv.FormatInt(e.F.Projects[0].ID, 10)
	for _, tc := range []struct {
		name, summary string
		set           map[string]string
		open          bool
	}{
		{"nothing", s2GSTSummary, nil, false},
		{"the tick alone", s2GSTSummary, map[string]string{"company_bill": "true"}, true},
		{"an invoice no.", s2GSTSummary, map[string]string{"invoice_no": "INV-1"}, true},
		{"a supplier GSTIN", s2GSTSummary, map[string]string{"supplier_gstin": "27AABCS1429B1ZU"}, true},
		{"a GST figure", s2GSTSummary, map[string]string{"gst_amount": "10"}, true},
		{"GST above the amount", s2GSTSummary, map[string]string{"gst_amount": "999", "expense_date": s2Today}, true},
		{"a tax code only", s2GSTSummary, map[string]string{"tax_code_id": strconv.FormatInt(e.F.TaxCodes[0].ID, 10)}, false},
		{"an HSN/SAC only", s2GSTSummary, map[string]string{"hsn_sac": "996331"}, false},
		{"nothing", s2ProjSummary, nil, false},
		{"a project", s2ProjSummary, map[string]string{"project_id": project}, true},
	} {
		post := s2Base(t, e.page(t, s2Deepak, newLine), "")
		post.Set("expense_date", "25-10-2026")
		for k, v := range tc.set {
			post.Set(k, v)
		}
		rec := e.postForm(t, s2Deepak, newLine, post)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("%s: status %d, want 422 (body %q)", tc.name, rec.Code, rec.Body.String())
		}
		if got := s2Open(s2Details(s2LineForm(t, s2Parse(t, rec)), tc.summary)); got != tc.open {
			t.Errorf("%s: %q open = %v, want %v", tc.name, tc.summary, got, tc.open)
		}
	}
}

// A saved unticked line whose tax code and HSN/SAC Validate filled from the
// type reopens with Bill in company GSTIN closed (AD ruling, review 1).
func TestExpLine_DefaultsKeepGSTClosed(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	e.exec(t, `UPDATE expense_type SET default_hsn_sac = '996331', default_tax_code_id = ? WHERE code = 'TRAVEL'`, e.F.TaxCodes[0].ID)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil)
	if got := e.line(t, c.ID, 1); got["hsn_sac"] == "" || got["tax_code_id"] == "" {
		t.Fatalf("Validate didn't fill the type's defaults: hsn_sac %q, tax_code_id %q", got["hsn_sac"], got["tax_code_id"])
	}
	if s2Open(s2Details(s2LineForm(t, e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/1")), s2GSTSummary)) {
		t.Errorf("line 1 reopens with %q open; the type's defaults don't open it", s2GSTSummary)
	}
}

// Billed to offers only enabled branches with a GSTIN.
func TestExpLine_BilledToSkipsBranches(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	ins := `INSERT INTO branch (code, name, state_code, gstin, disabled, created_at, updated_at) VALUES (?, ?, '27', ?, ?, '2026-10-01T00:00:00.000Z', '2026-10-01T00:00:00.000Z')`
	e.exec(t, ins, "AAA", "No GSTIN Office", "", 0)
	e.exec(t, ins, "AAB", "Closed Office", "27AAFCV2449G2Z6", 1)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil)
	got := s2Labels(s2Options(s2LineForm(t, e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/new")), "billed_to_id"))
	if want := []string{"BLR · Bengaluru Site Office", "PUNE · Pune Works"}; !slices.Equal(got, want) {
		t.Errorf("Billed to offers %q, want %q", got, want)
	}
}

// No "(optional)" on the fields a ticked line needs at submit (AD ruling).
func TestExpLine_NoOptionalMarks(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil)
	form := s2LineForm(t, e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/new"))
	for _, n := range []string{"company_bill", "billed_to_id", "supplier_gstin", "invoice_no"} {
		if got := s2Text(s2Field(t, form, n).ChildrenFiltered(".field__label")); strings.Contains(got, "(optional)") {
			t.Errorf("%s's label is %q, want no (optional)", n, got)
		}
	}
	// The same-receipt group repeats no legend under its summary.
	c2 := e.DraftClaim(t, e.P(t, s2Deepak), nil)
	e.withReceipts(t, c2.ID, map[int]string{1: "taxi.jpg"})
	same := s2Details(s2LineForm(t, e.page(t, s2Deepak, s2ClaimPath(c2.ID)+"/lines/new")), s2SameSummary)
	if got := s2Text(same.Find("legend")); got != "" {
		t.Errorf("the same-receipt group's legend reads %q, want none under the summary", got)
	}
}

// The same-bytes note says file, names the lowest line, and is left out on
// a line that already holds that receipt (AD ruling, review 1).
func TestExpReceipt_SameBytesOnOwnLine(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	deepak := e.P(t, s2Deepak)
	c := e.DraftClaim(t, deepak, nil, nil, nil, nil)
	bill := e.withReceipts(t, c.ID, map[int]string{2: "bill.jpg", 3: "bill.jpg"})["bill.jpg"]
	id, v := strconv.FormatInt(c.ID, 10), strconv.Itoa(e.version(t, c.ID))
	if got := e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/3").Find("#receipt form").First().AttrOr("hx-post", ""); got != "/modules/expense/claims/"+id+"/receipt?version="+v+"&line=3" {
		t.Errorf("line 3's upload posts to %q, want the line in the query", got)
	}
	for _, tc := range []struct{ line, note string }{
		{"1", "This file is already on this claim, as the receipt on line 2. It is used for this expense too."},
		{"3", ""},
	} {
		req := e.uploadRequest(t, id, v, s2File("again.jpg", s2Bytes("bill.jpg")))
		req.URL.RawQuery += "&line=" + tc.line
		rec := e.serve(t, s2Deepak, req)
		d := s2Fragment(t, "line "+tc.line, rec, bill, "")
		if got := s2Text(d.Find(`#receipt-files [role="status"]`)); got != tc.note {
			t.Errorf("line %s: the note reads %q, want %q", tc.line, got, tc.note)
		}
	}
}

// A version ahead of the claim's is stale too, and an upload that fails
// after the checks (the database closed) is the plain internal error.
func TestExpReceipt_AheadAndFailure(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	deepak := e.P(t, s2Deepak)
	c := e.DraftClaim(t, deepak, nil)
	id := strconv.FormatInt(c.ID, 10)
	ahead := strconv.Itoa(e.version(t, c.ID) + 1)
	e.refused(t, "a version ahead", http.StatusConflict, s2MsgStale(c.ID), func() *httptest.ResponseRecorder {
		return e.upload(t, s2Deepak, id, ahead, s2File("bill.jpg", filestest.JPEG(4096)))
	})
	v := strconv.Itoa(e.version(t, c.ID))
	if err := e.DB.Close(); err != nil {
		t.Fatal(err)
	}
	req := e.uploadRequest(t, id, v, s2File("bill.jpg", filestest.JPEG(4096)))
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req.WithContext(auth.WithPrincipal(req.Context(), deepak)))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("upload with the database closed: status %d, want 500 (body %q)", rec.Code, rec.Body.String())
	}
}

// s2NewHide is s2New with the claim module's Hide replaced by hide.
func s2NewHide(t *testing.T, hide func(docscreen.Ctx, doctype.Field) bool) *s2Env {
	t.Helper()
	e := expensetest.New(t)
	d := expenseweb.Deps{Service: e.Service, Engine: e.Engine, Auth: e.Auth, Files: e.Files}
	m := expenseweb.ClaimModule(d)
	m.Hide = hide
	ds := docscreen.Deps{
		DB: e.DB, Engine: e.Engine, Auth: e.Auth, Extras: s2Extras{e},
		Today:   func() (string, string) { return s2Today, s2Yesterday },
		Modules: map[string]docscreen.Module{expense.ClaimDocType: m},
	}
	r := chi.NewRouter()
	docscreen.Routes(ds)(r)
	expenseweb.Routes(d)(r)
	return &s2Env{Env: e, router: r}
}

// A field the module hides is neither drawn nor posted by the line page, so
// Save line never blanks it (CD-319, review 1).
func TestExpLine_HiddenFieldKept(t *testing.T) {
	t.Parallel()
	e := s2NewHide(t, func(_ docscreen.Ctx, f doctype.Field) bool { return f.Name == "project_id" })
	project := strconv.FormatInt(e.F.Projects[0].ID, 10)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil, expensetest.Values{"project_id": project})
	line1 := s2ClaimPath(c.ID) + "/lines/1"
	page := e.page(t, s2Deepak, line1)
	if n := s2LineForm(t, page).Find(`[name="project_id"]`).Length(); n != 0 {
		t.Errorf("the hidden project_id is drawn (%d controls)", n)
	}
	post := s2Post(t, page, "")
	post.Set("no_receipt_reason", "Paper file")
	post.Set("project_id", "")
	if rec := e.postForm(t, s2Deepak, line1, post); rec.Code != http.StatusSeeOther {
		t.Fatalf("Save line 1: status %d, want 303 (body %q)", rec.Code, rec.Body.String())
	}
	if got := e.line(t, c.ID, 1)["project_id"]; got != project {
		t.Errorf("line 1's project_id = %q after the save, want the hidden %s kept", got, project)
	}
}

// receipt_sha256 is the line page's own field: drawn and posted even when
// the module hides it from the generic pages (S1's claimHide), while every
// other hidden field, here Billed to, is neither drawn nor posted (AD ruling,
// S2 review).
func TestExpLine_HiddenReceiptStillSaves(t *testing.T) {
	t.Parallel()
	e := s2NewHide(t, func(_ docscreen.Ctx, f doctype.Field) bool {
		return f.Name == "receipt_sha256" || f.Name == "billed_to_id"
	})
	pune := strconv.FormatInt(e.id(t, `SELECT id FROM branch WHERE code = 'PUNE'`), 10)
	blr := strconv.FormatInt(e.id(t, `SELECT id FROM branch WHERE code = 'BLR'`), 10)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil, expensetest.Values{
		"company_bill": "true", "billed_to_id": pune, "supplier_gstin": "27AABCS1429B1ZU", "invoice_no": "INV-881",
	})
	line1 := s2ClaimPath(c.ID) + "/lines/1"
	page := e.page(t, s2Deepak, line1)
	form := s2LineForm(t, page)
	s2HiddenReceipt(t, form)
	if n := form.Find(`[name="billed_to_id"]`).Length(); n != 0 {
		t.Errorf("the hidden billed_to_id is drawn (%d controls)", n)
	}
	if rec := e.upload(t, s2Deepak, strconv.FormatInt(c.ID, 10), strconv.Itoa(e.version(t, c.ID)), s2File("bill.jpg", s2Bytes("bill.jpg"))); rec.Code != http.StatusOK {
		t.Fatalf("upload bill.jpg: status %d (body %q)", rec.Code, rec.Body.String())
	}
	bill := e.s2Meta(t, c.ID, "bill.jpg")
	post := s2Post(t, page, bill.SHA256)
	post.Set("company_bill", "true")
	post.Set("supplier_gstin", "27AABCS1429B1ZU")
	post.Set("invoice_no", "INV-881")
	post.Set("billed_to_id", blr) // a forged post of the hidden field
	if rec := e.postForm(t, s2Deepak, line1, post); rec.Code != http.StatusSeeOther {
		t.Fatalf("Save line 1: status %d, want 303 (body %q)", rec.Code, rec.Body.String())
	}
	got := e.line(t, c.ID, 1)
	if got["receipt_sha256"] != bill.SHA256 {
		t.Errorf("line 1's receipt_sha256 = %q, want bill.jpg's %s", got["receipt_sha256"], bill.SHA256)
	}
	if got["billed_to_id"] != pune {
		t.Errorf("line 1's billed_to_id = %q, want the hidden PUNE %s kept", got["billed_to_id"], pune)
	}
}
