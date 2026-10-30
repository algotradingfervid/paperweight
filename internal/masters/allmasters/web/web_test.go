package mastersweb_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/alexedwards/scs/v2"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	mastersweb "github.com/vardhan/internaltools/internal/masters/allmasters/web"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
)

// No case IDs of its own: the wizard's words are NUM's (NUM-140 to NUM-158).
// Fixture DocTypes stand in for the masters, because Tasks 20 and 21 register
// the real importers (a second RegisterImporter panics). The real masters are
// Importable (CD-210), so they are on the choose step too.

const (
	widget = "mastersweb.Widget" // importable, with a RowFunc
	gadget = "mastersweb.Gadget" // importable, with a RowFunc
	orphan = "mastersweb.Orphan" // importable, no RowFunc
	plain  = "mastersweb.Plain"  // not importable
	note   = "mastersweb.Note"   // importable, with a RowFunc, no code column

	header   = "Code,Name\r\n"
	clean    = header + "W1,One\r\nW2,Two\r\n"
	oneBad   = header + "W1,One\r\nW2,\r\n"
	forbidMe = header + "W1,FORBID\r\n"

	denied = "You don't have permission to import Widget UoM. Ask your administrator for a role that allows it."
)

func fixture(name, label, plural, table string, importable bool) {
	k := mstkit.Kind{DocType: name, Label: label, Table: table, HasCode: true}
	mstkit.RegisterMaster(doctype.DocType{
		Name: name, Label: label, PluralLabel: plural, Table: table, Importable: importable,
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, Required: true, Importable: true},
			{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, Required: true, Importable: true},
			{Name: "made_on", Label: "Made On", Column: "made_on", Type: doctype.Date, Importable: true},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check},
		},
	}, mstkit.StandardLookup(k))
}

// cancelKey carries the request's cancel function to the RowFunc.
type cancelKey struct{}

// noteRow is a RowFunc for a master with no code: nothing in the row can tell
// csvio that it was imported before.
func noteRow(ctx context.Context, tx *db.Tx, _ auth.Principal, row map[string]string) (int64, []doctype.FieldError, error) {
	res, err := tx.SQL().ExecContext(ctx, `INSERT INTO mastersweb_note (name) VALUES (?)`, row["name"])
	if err != nil {
		return 0, nil, err
	}
	id, err := res.LastInsertId()
	return id, nil, err
}

func insertRow(table string) csvio.RowFunc {
	return func(ctx context.Context, tx *db.Tx, _ auth.Principal, row map[string]string) (int64, []doctype.FieldError, error) {
		if cancel, ok := ctx.Value(cancelKey{}).(context.CancelFunc); ok {
			cancel() // the browser went away in the middle of the import
			return 0, nil, ctx.Err()
		}
		if row["name"] == "FORBID" {
			return 0, nil, auth.Forbidden("You can only work with your assigned widgets.")
		}
		res, err := tx.SQL().ExecContext(ctx, "INSERT INTO "+table+" (code, name) VALUES (?, ?)", row["code"], row["name"]) //nolint:gosec // G202: fixed test tables
		if err != nil {
			return 0, nil, err
		}
		id, err := res.LastInsertId()
		return id, nil, err
	}
}

func init() {
	fixture(widget, "Widget UoM", "Widget UoMs", "mastersweb_widget", true)
	fixture(gadget, "Gadget", "Gadgets", "mastersweb_gadget", true)
	fixture(orphan, "Orphan", "Orphans", "mastersweb_orphan", true)
	fixture(plain, "Plain", "Plains", "mastersweb_plain", false)
	mstkit.RegisterMaster(doctype.DocType{
		Name: note, Label: "Note", PluralLabel: "Notes", Table: "mastersweb_note", Importable: true,
		Fields: []doctype.Field{
			{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, Required: true, Importable: true},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check},
		},
	}, mstkit.StandardLookup(mstkit.Kind{DocType: note, Label: "Note", Table: "mastersweb_note"}))
	csvio.RegisterImporter(note, noteRow)
	csvio.RegisterImporter(widget, insertRow("mastersweb_widget"))
	csvio.RegisterImporter(gadget, insertRow("mastersweb_gadget"))
}

// world is the wizard on a router of its own: webtest.Harness keeps its
// session manager private and carries no cookie between requests.
type world struct {
	env     *msttest.Env
	sm      *scs.SessionManager
	handler http.Handler
}

func newWorld(t *testing.T) *world {
	t.Helper()
	env := msttest.New(t)
	ctx := context.Background()
	if err := env.DB.Write(ctx, func(tx *db.Tx) error {
		for _, table := range []string{"mastersweb_widget", "mastersweb_gadget", "mastersweb_orphan", "mastersweb_plain"} {
			if _, err := tx.SQL().ExecContext(ctx, `CREATE TABLE `+table+` (id INTEGER PRIMARY KEY, code TEXT NOT NULL UNIQUE, name TEXT NOT NULL, disabled INTEGER NOT NULL DEFAULT 0)`); err != nil { //nolint:gosec // G202: fixed test tables
				return err
			}
		}
		_, err := tx.SQL().ExecContext(ctx, `CREATE TABLE mastersweb_note (id INTEGER PRIMARY KEY, name TEXT NOT NULL, disabled INTEGER NOT NULL DEFAULT 0)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	sm := scs.New()
	r := web.NewRouter(web.Options{Sessions: sm})
	mastersweb.Routes(env.Kit, sm)(r)
	return &world{env: env, sm: sm, handler: r}
}

// browser is one browser: its session cookie survives between requests, and
// each request carries the principal it is sent as.
type browser struct {
	w       *world
	cookies map[string]*http.Cookie
}

func (w *world) browser() *browser { return &browser{w: w, cookies: map[string]*http.Cookie{}} }

func (b *browser) do(t *testing.T, p auth.Principal, req *http.Request) webtest.Response {
	t.Helper()
	for _, c := range b.cookies {
		req.AddCookie(c)
	}
	req = req.WithContext(auth.WithPrincipal(req.Context(), p))
	rec := httptest.NewRecorder()
	b.w.handler.ServeHTTP(rec, req)
	for _, c := range (&http.Response{Header: rec.Header()}).Cookies() {
		b.cookies[c.Name] = c
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rec.Body.String()))
	if err != nil {
		t.Fatal(err)
	}
	return webtest.Response{Status: rec.Code, Location: rec.Header().Get("Location"), Header: rec.Header(), Body: rec.Body.String(), Doc: doc}
}

func (b *browser) get(t *testing.T, p auth.Principal, target string) webtest.Response {
	t.Helper()
	return b.do(t, p, httptest.NewRequest(http.MethodGet, target, nil))
}

func formRequest(target string, form url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func (b *browser) post(t *testing.T, p auth.Principal, target string, form url.Values) webtest.Response {
	t.Helper()
	return b.do(t, p, formRequest(target, form))
}

func (b *browser) choose(t *testing.T, p auth.Principal, dt string) {
	t.Helper()
	webtest.WantRedirect(t, b.post(t, p, "/m/import/choose", url.Values{"doctype": {dt}}), "/m/import/upload")
}

// sendPart posts content as the "file" part, or a part of another name.
func (b *browser) sendPart(t *testing.T, p auth.Principal, part, content string) webtest.Response {
	t.Helper()
	return b.do(t, p, partRequest(t, part, content))
}

// sendPartHX is sendPart as FileUpload's form sends it.
func (b *browser) sendPartHX(t *testing.T, p auth.Principal, part, content string) webtest.Response {
	t.Helper()
	req := partRequest(t, part, content)
	req.Header.Set("HX-Request", "true")
	return b.do(t, p, req)
}

func partRequest(t *testing.T, part, content string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile(part, "masters.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/m/import/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func (b *browser) upload(t *testing.T, p auth.Principal, content string) {
	t.Helper()
	webtest.WantRedirect(t, b.sendPart(t, p, "file", content), "/m/import/check")
}

func (w *world) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	ctx := context.Background()
	if err := w.env.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, q, args...).Scan(&n)
	}); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func (w *world) widgets(t *testing.T) int {
	t.Helper()
	return w.count(t, `SELECT count(*) FROM mastersweb_widget`)
}

func (w *world) uploads(t *testing.T) int {
	t.Helper()
	return w.count(t, `SELECT count(*) FROM csv_upload`)
}

// token is the session's csv_upload row: there must be exactly one.
func (w *world) token(t *testing.T) string {
	t.Helper()
	var s string
	ctx := context.Background()
	if err := w.env.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT token FROM csv_upload`).Scan(&s)
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

func cardTitles(r webtest.Response) []string {
	var out []string
	r.Doc.Find("#wizard .card-title").Each(func(_ int, s *goquery.Selection) {
		out = append(out, webtest.Squash(s.Text()))
	})
	return out
}

func stepLinks(r webtest.Response) bool {
	return r.Doc.Find(`.stepper a[href="/m/import/import"]`).Length() > 0
}

func TestChoose_ListsOnlyWhatTheUserMayImport(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	values := func(p auth.Principal) []string {
		r := b.get(t, p, "/m/import/choose")
		webtest.WantStatus(t, r, http.StatusOK)
		var out []string
		for _, c := range r.Choices("doctype") {
			out = append(out, c.Value)
		}
		return out
	}

	want := []string{"Address", "Contact", "Employee", "Expense Type", gadget, "Item", note, orphan, "Party", "Project", "Site", "Tax Code", "UoM", widget}
	if got := values(w.env.Admin); !slices.Equal(got, want) {
		t.Errorf("Admin sees %q; want %q (sorted by label, never the not-importable one)", got, want)
	}
	ctx := context.Background()
	if err := w.env.Kit.Auth.(*auth.Auth).SetRights(ctx, w.env.Admin, "Masters Manager", gadget, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}); err != nil {
		t.Fatal(err)
	}
	want = slices.DeleteFunc(want, func(v string) bool { return v == gadget })
	if got := values(w.env.Kavya); !slices.Equal(got, want) {
		t.Errorf("Kavya sees %q; want %q", got, want)
	}
	if got := values(w.env.Priya); len(got) != 0 {
		t.Errorf("Priya sees %q; want none", got)
	}
	r := b.get(t, w.env.Priya, "/m/import/choose")
	webtest.WantText(t, r, "There are no masters you can import.")
	if r.Doc.Find(`#wizard button[type="submit"]`).Length() != 0 {
		t.Error("the empty step offers a submit button")
	}
	if b.get(t, w.env.Admin, "/m/import/choose").Doc.Find(`select[name="doctype"]`).Length() != 1 {
		t.Error("the masters are not a select")
	}
}

func TestChoose_Refusals(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	for _, tc := range []struct{ name, doctype, msg string }{
		{"nothing chosen", "", "Master is required."},
		{"unknown", "mastersweb.Nope", "Master not found. Pick one from the list."},
		{"not importable", plain, "Master not found. Pick one from the list."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := b.post(t, w.env.Admin, "/m/import/choose", url.Values{"doctype": {tc.doctype}})
			webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
			webtest.WantFieldError(t, r, "doctype", tc.msg)
		})
	}
	r := b.post(t, w.env.Ravi, "/m/import/choose", url.Values{"doctype": {widget}})
	webtest.WantStatus(t, r, http.StatusForbidden)
	webtest.WantText(t, r, denied)
}

func TestTemplate_Download(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	dt, _ := doctype.Lookup(widget)

	b.choose(t, w.env.Kavya, widget)
	page := b.get(t, w.env.Kavya, "/m/import/upload")
	href := "/m/import/template?doctype=" + url.QueryEscape(widget)
	if page.Doc.Find(`a[href="`+href+`"]`).Length() != 1 {
		t.Fatalf("the Upload step has no link to %s\n%s", href, page.Text())
	}
	if n := page.Doc.Find(`a[href^="/m/import/template"]`).Length(); n != 1 {
		t.Errorf("%d template links on the Upload step; want the chosen master's only", n)
	}
	r := b.get(t, w.env.Kavya, href)
	webtest.WantStatus(t, r, http.StatusOK)
	if r.Body != string(csvio.Template(dt)) {
		t.Errorf("body = %q; want csvio.Template", r.Body)
	}
	if got := r.Header.Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := r.Header.Get("Content-Disposition"); got != `attachment; filename="mastersweb_widget-template.csv"` {
		t.Errorf("Content-Disposition = %q", got)
	}

	r = b.get(t, w.env.Ravi, href)
	webtest.WantStatus(t, r, http.StatusForbidden)
	webtest.WantText(t, r, denied)

	webtest.WantStatus(t, b.get(t, w.env.Admin, "/m/import/template?doctype="+url.QueryEscape(plain)), http.StatusNotFound)
	webtest.WantStatus(t, b.get(t, w.env.Admin, "/m/import/template?doctype=nope"), http.StatusNotFound)
}

func TestUpload_ShowsRequiredColumnsAndTakesACSV(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	b.choose(t, w.env.Kavya, widget)
	r := b.get(t, w.env.Kavya, "/m/import/upload")
	webtest.WantStatus(t, r, http.StatusOK)
	webtest.WantText(t, r, "CSV, up to 5 MB. Required columns: Code, Name.")
	input := r.Doc.Find(`#wizard input[type="file"][name="file"]`)
	if input.Length() != 1 {
		t.Fatalf("want one file input (no camera), got %d\n%s", input.Length(), r.Text())
	}
	if got, _ := input.Attr("accept"); got != ".csv,text/csv" {
		t.Errorf("accept = %q", got)
	}
	form := input.Closest("form")
	if got, _ := form.Attr("hx-post"); got != "/m/import/upload" {
		t.Errorf("hx-post = %q", got)
	}
	if got, _ := form.Attr("hx-target"); got != "#wizard" {
		t.Errorf("hx-target = %q", got)
	}
	if got, _ := form.Attr("enctype"); got != "multipart/form-data" {
		t.Errorf("enctype = %q", got)
	}
}

// Choosing a file posts it at once: the answer replaces #wizard with Check
// results, or with Upload and the refusal, whose summary links to the input.
func TestUpload_UnderHTMXSwapsTheWizard(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	b.choose(t, p, widget)

	r := b.sendPartHX(t, p, "file", strings.Repeat("x", csvio.MaxFileBytes+1))
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	if got := r.Doc.Find("#wizard h1").Text(); got != "Upload" {
		t.Errorf("heading = %q", got)
	}
	webtest.WantFieldError(t, r, "file", "This file is larger than 5 MB. Split it into smaller files.")
	href, _ := r.Doc.Find(`a[href^="#"]`).First().Attr("href")
	if href == "" || r.Doc.Find(href+`[name="file"]`).Length() != 1 {
		t.Errorf("the error summary links to %q, which is not the file input", href)
	}

	r = b.sendPartHX(t, p, "file", clean)
	webtest.WantStatus(t, r, http.StatusOK)
	if got := r.Header.Get("HX-Push-Url"); got != "/m/import/check" {
		t.Errorf("HX-Push-Url = %q", got)
	}
	if got := r.Doc.Find("#wizard h1").Text(); got != "Check results" {
		t.Errorf("heading = %q", got)
	}
}

func TestCheck_ShowsErrorsAsCardsAndBlocksStepFour(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	b.choose(t, p, widget)
	b.upload(t, p, "Code,Name,Colour\r\nW1,One,red\r\nW2,,blue\r\n")

	r := b.get(t, p, "/m/import/check")
	webtest.WantStatus(t, r, http.StatusOK)
	webtest.WantBanner(t, r, "1 row is ready, 1 row has errors")
	if got, want := cardTitles(r), []string{"Row 3 · Name is required."}; !slices.Equal(got, want) {
		t.Errorf("cards = %q; want %q", got, want)
	}
	webtest.WantText(t, r, "Column 'Colour' isn't a field of Widget UoM and will be ignored.")
	if stepLinks(r) {
		t.Error("the stepper links to Import while errors exist")
	}
	if r.Doc.Find(`button[type="submit"].btn--primary`).Length() != 0 {
		t.Error("the check step offers its primary button while errors exist")
	}
	if n := w.widgets(t); n != 0 {
		t.Errorf("%d rows written before Import", n)
	}

	// Every way past it is refused.
	webtest.WantRedirect(t, b.get(t, p, "/m/import/import"), "/m/import/check")
	webtest.WantRedirect(t, b.post(t, p, "/m/import/check", nil), "/m/import/import")
	direct := b.post(t, p, "/m/import/import", nil)
	webtest.WantStatus(t, direct, http.StatusUnprocessableEntity)
	webtest.WantBanner(t, direct, "Nothing was imported. Fix 1 row and upload again.")
	if got, want := cardTitles(direct), []string{"Row 3 · Name is required."}; !slices.Equal(got, want) {
		t.Errorf("cards = %q; want %q", got, want)
	}
	if n := w.widgets(t); n != 0 {
		t.Errorf("direct POST wrote %d rows", n)
	}
	if n := w.uploads(t); n != 1 {
		t.Errorf("%d csv_upload rows after a refused import; want the file kept", n)
	}
}

func TestCheck_ColumnNamedErrorsTitleTheColumn(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	b.choose(t, p, widget)
	b.upload(t, p, "Code,Name,Made On\r\nW1,One,31-02-2025\r\n")
	r := b.get(t, p, "/m/import/check")
	if got, want := cardTitles(r), []string{"Row 2 · Made On · 31-02-2025 is not a valid date. Use dd-mm-yyyy, for example 31-03-2025."}; !slices.Equal(got, want) {
		t.Errorf("cards = %q; want %q", got, want)
	}
}

func TestPreview_WritesNothing(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	audit := w.count(t, `SELECT count(*) FROM audit_log`)
	b.choose(t, p, widget)
	b.upload(t, p, clean)
	if n := w.widgets(t); n != 0 {
		t.Fatalf("Upload wrote %d rows", n)
	}

	r := b.get(t, p, "/m/import/check")
	webtest.WantBanner(t, r, "2 rows are ready, 0 rows have errors")
	if !stepLinks(r) {
		t.Error("a clean file's stepper has no link to Import")
	}
	if n := w.widgets(t); n != 0 {
		t.Errorf("Preview wrote %d rows", n)
	}
	if n := w.count(t, `SELECT count(*) FROM audit_log`); n != audit {
		t.Errorf("audit_log %d → %d", audit, n)
	}
}

func TestImport_CleanFile(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	b.choose(t, p, widget)
	b.upload(t, p, clean)
	webtest.WantRedirect(t, b.post(t, p, "/m/import/check", nil), "/m/import/import")

	page := b.get(t, p, "/m/import/import")
	webtest.WantStatus(t, page, http.StatusOK)
	if got := webtest.Squash(page.Doc.Find(`button.btn--primary`).Text()); got != "Import 2 widget UoMs" {
		t.Errorf("primary = %q", got)
	}
	if n := w.widgets(t); n != 0 {
		t.Fatalf("%d rows written before the Import POST", n)
	}

	r := b.post(t, p, "/m/import/import", nil)
	webtest.WantRedirect(t, r, "/m/import/choose")
	if n := w.widgets(t); n != 2 {
		t.Errorf("%d rows imported; want 2", n)
	}
	if n := w.count(t, `SELECT count(*) FROM audit_log WHERE reason = 'created by import'`); n != 2 {
		t.Errorf("%d import audit entries; want 2", n)
	}
	if n := w.uploads(t); n != 0 {
		t.Errorf("%d csv_upload rows after Import; want 0", n)
	}
	webtest.WantBanner(t, b.get(t, p, "/m/import/choose"), "Imported 2 Widget UoMs.")

	// The session forgot the master and the file.
	webtest.WantRedirect(t, b.get(t, p, "/m/import/upload"), "/m/import/choose")
	webtest.WantRedirect(t, b.get(t, p, "/m/import/check"), "/m/import/choose")
	webtest.WantRedirect(t, b.post(t, p, "/m/import/import", nil), "/m/import/choose")
}

func TestImport_OneRowSaysSingular(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	b.choose(t, p, widget)
	b.upload(t, p, header+"W1,One\r\n")
	r := b.get(t, p, "/m/import/import")
	if got := webtest.Squash(r.Doc.Find(`button.btn--primary`).Text()); got != "Import 1 widget UoM" {
		t.Errorf("primary = %q", got)
	}
}

// The preview is a promise about the past: Import validates again, so rows
// that became bad after Check results are never imported.
func TestImport_StalePreviewImportsNothing(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	b.choose(t, p, widget)
	b.upload(t, p, clean)
	webtest.WantBanner(t, b.get(t, p, "/m/import/check"), "2 rows are ready, 0 rows have errors")

	ctx := context.Background()
	if err := w.env.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `INSERT INTO mastersweb_widget (code, name) VALUES ('W2', 'Taken')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	r := b.post(t, p, "/m/import/import", nil)
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantBanner(t, r, "Nothing was imported. Fix 1 row and upload again.")
	if got, want := cardTitles(r), []string{"Row 3 · Code W2 already exists."}; !slices.Equal(got, want) {
		t.Errorf("cards = %q; want %q", got, want)
	}
	if n := w.widgets(t); n != 1 {
		t.Errorf("%d rows; want only the one inserted by hand", n)
	}
}

func TestUpload_NewFileReplacesTheOld(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	b.choose(t, p, widget)
	b.upload(t, p, oneBad)
	first := w.token(t)
	b.upload(t, p, clean)
	if w.token(t) == first {
		t.Error("the second upload kept the first token")
	}
	webtest.WantBanner(t, b.get(t, p, "/m/import/check"), "2 rows are ready, 0 rows have errors")
}

// A second browser of the same user replaces the first one's row too.
func TestUpload_ReplacesTheUsersRowFromAnotherSession(t *testing.T) {
	w := newWorld(t)
	p := w.env.Kavya
	one, two := w.browser(), w.browser()
	one.choose(t, p, widget)
	one.upload(t, p, clean)
	two.choose(t, p, widget)
	two.upload(t, p, oneBad)
	if n := w.uploads(t); n != 1 {
		t.Fatalf("%d csv_upload rows; want 1", n)
	}
	webtest.WantRedirect(t, one.get(t, p, "/m/import/check"), "/m/import/upload")
}

func TestUpload_OnlyTheTokenGoesInTheSession(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	b.choose(t, p, widget)
	b.upload(t, p, clean+strings.Repeat("Wx,padding padding padding padding\r\n", 3000))
	var raw string
	for _, c := range b.cookies {
		raw = c.Value
	}
	data, found, err := w.sm.Store.Find(raw)
	if err != nil || !found {
		t.Fatalf("session: found %v, %v", found, err)
	}
	if len(data) > 1024 {
		t.Errorf("session holds %d bytes; want only the token", len(data))
	}
	if !bytes.Contains(data, []byte(w.token(t))) {
		t.Error("the session doesn't hold the token")
	}
}

// Same session cookie, another signed-in user: the path reaches the row with
// user A's token and the store refuses it for user B.
func TestUpload_AnotherUsersTokenIsRefused(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	b.choose(t, w.env.Kavya, widget)
	b.upload(t, w.env.Kavya, clean)

	webtest.WantRedirect(t, b.get(t, w.env.Admin, "/m/import/check"), "/m/import/upload")
	webtest.WantRedirect(t, b.get(t, w.env.Admin, "/m/import/import"), "/m/import/upload")
	webtest.WantRedirect(t, b.post(t, w.env.Admin, "/m/import/import", nil), "/m/import/upload")
	if n := w.widgets(t); n != 0 {
		t.Errorf("user B imported %d rows from user A's file", n)
	}
	if n := w.uploads(t); n != 1 {
		t.Errorf("%d csv_upload rows; user A's row must stay", n)
	}
}

func TestUpload_RowsOlderThanADayGoOnTheNextUpload(t *testing.T) {
	w := newWorld(t)
	old, recent, next := w.browser(), w.browser(), w.browser()
	old.choose(t, w.env.Kavya, widget)
	old.upload(t, w.env.Kavya, clean)
	w.env.Clock.Advance(2 * time.Hour)
	recent.choose(t, w.env.Admin, widget)
	recent.upload(t, w.env.Admin, clean)

	// The old row is now just past 24 hours, the recent one 22 hours old.
	w.env.Clock.Advance(22*time.Hour + time.Second)
	if n := w.uploads(t); n != 2 {
		t.Fatalf("%d rows before the next upload; want 2 (only an upload sweeps)", n)
	}
	ctx := context.Background()
	if err := w.env.Kit.Auth.(*auth.Auth).SetRights(ctx, w.env.Admin, "Purchase Manager", widget, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightImport: auth.ScopeAll}); err != nil {
		t.Fatal(err)
	}
	next.choose(t, w.env.Ravi, widget)
	next.upload(t, w.env.Ravi, clean)
	if n := w.count(t, `SELECT count(*) FROM csv_upload WHERE user_id = ?`, int64(w.env.Kavya.ID)); n != 0 {
		t.Errorf("the 24-hour-old row survived")
	}
	if n := w.count(t, `SELECT count(*) FROM csv_upload WHERE user_id = ?`, int64(w.env.Admin.ID)); n != 1 {
		t.Errorf("the 22-hour-old row was swept")
	}
}

func TestUpload_TooLargeNoFileAndBadFiles(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	const tooBig = "This file is larger than 5 MB. Split it into smaller files."
	b.choose(t, p, widget)

	for name, size := range map[string]int{"one byte over": csvio.MaxFileBytes + 1, "body over the limit": csvio.MaxFileBytes + 2<<20} {
		t.Run(name, func(t *testing.T) {
			r := b.sendPart(t, p, "file", strings.Repeat("x", size))
			webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
			webtest.WantFieldError(t, r, "file", tooBig)
		})
	}
	// The handler bounds the whole body, but only when nothing parsed it first:
	// behind AUTH, a post with no X-CSRF-Token header is parsed by the CSRF check.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("note", strings.Repeat("x", csvio.MaxFileBytes+2<<20)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/m/import/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	r := b.do(t, p, req)
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, r, "file", tooBig)

	r = b.sendPart(t, p, "other", clean)
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, r, "file", "Choose a file to upload.")
	r = b.post(t, p, "/m/import/upload", nil)
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, r, "file", "Choose a file to upload.")
	if n := w.uploads(t); n != 0 {
		t.Errorf("%d csv_upload rows after refused uploads", n)
	}

	// A file csvio refuses as a whole shows NUM's message, and no way on.
	b.upload(t, p, "Code\r\nW1\r\n")
	r = b.get(t, p, "/m/import/check")
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantBanner(t, r, "Column 'Name' is missing. Add it to the first row.")
	webtest.WantRedirect(t, b.get(t, p, "/m/import/import"), "/m/import/check")
	if stepLinks(r) {
		t.Error("the stepper links to Import for a refused file")
	}
	direct := b.post(t, p, "/m/import/import", nil)
	webtest.WantRedirect(t, direct, "/m/import/check")
	w.wantUnclaimed(t)
}

func TestRefusals_NoImportRight(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	b.choose(t, w.env.Kavya, widget)
	b.upload(t, w.env.Kavya, clean)

	for _, r := range []webtest.Response{
		b.get(t, w.env.Ravi, "/m/import/upload"),
		b.sendPart(t, w.env.Ravi, "file", clean),
		b.get(t, w.env.Ravi, "/m/import/check"),
		b.post(t, w.env.Ravi, "/m/import/check", nil),
		b.get(t, w.env.Ravi, "/m/import/import"),
		b.post(t, w.env.Ravi, "/m/import/import", nil),
	} {
		webtest.WantStatus(t, r, http.StatusForbidden)
		webtest.WantText(t, r, denied)
	}
	if n := w.widgets(t); n != 0 {
		t.Errorf("%d rows imported", n)
	}
}

// CD-172 and CD-174 are the RowFunc's: a refusal it returns stays a 403 with
// its own message, in the preview and in the import.
func TestRefusals_RestrictedRowFunc(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	b.choose(t, p, widget)
	b.upload(t, p, forbidMe)
	for _, r := range []webtest.Response{b.get(t, p, "/m/import/check"), b.post(t, p, "/m/import/import", nil)} {
		webtest.WantStatus(t, r, http.StatusForbidden)
		webtest.WantText(t, r, "You can only work with your assigned widgets.")
	}
	w.wantUnclaimed(t)
}

func TestNoImporter_PlainMessage(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	b.choose(t, p, orphan)
	b.upload(t, p, clean)
	r := b.get(t, p, "/m/import/check")
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantBanner(t, r, "Import isn't ready for Orphans yet.")
	webtest.WantRedirect(t, b.post(t, p, "/m/import/import", nil), "/m/import/check")
	w.wantUnclaimed(t)
}

func TestSteps_NeedTheEarlierSteps(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	webtest.WantRedirect(t, b.get(t, p, "/m/import/upload"), "/m/import/choose")
	webtest.WantRedirect(t, b.sendPart(t, p, "file", clean), "/m/import/choose")
	webtest.WantRedirect(t, b.get(t, p, "/m/import/check"), "/m/import/choose")
	b.choose(t, p, widget)
	webtest.WantRedirect(t, b.get(t, p, "/m/import/check"), "/m/import/upload")
	webtest.WantRedirect(t, b.get(t, p, "/m/import/import"), "/m/import/upload")
	webtest.WantRedirect(t, b.post(t, p, "/m/import/import", nil), "/m/import/upload")
}

// A file uploaded for one master is never checked against another.
func TestChoose_AnotherMasterDropsTheFile(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	b.choose(t, p, widget)
	b.upload(t, p, clean)
	b.choose(t, p, widget)
	webtest.WantStatus(t, b.get(t, p, "/m/import/check"), http.StatusOK)
	b.choose(t, p, gadget)
	webtest.WantRedirect(t, b.get(t, p, "/m/import/check"), "/m/import/upload")
}

func TestBack_GoesToThePreviousStep(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	back := url.Values{"nav": {"back"}}
	b.choose(t, p, widget)
	b.upload(t, p, clean)
	webtest.WantRedirect(t, b.post(t, p, "/m/import/import", back), "/m/import/check")
	webtest.WantRedirect(t, b.post(t, p, "/m/import/check", back), "/m/import/upload")
	webtest.WantRedirect(t, b.post(t, p, "/m/import/upload", back), "/m/import/choose")
	if n := w.widgets(t); n != 0 {
		t.Errorf("Back imported %d rows", n)
	}
}

func TestHTMX_StepsSwapTheWizard(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	req := formRequest("/m/import/choose", url.Values{"doctype": {widget}})
	req.Header.Set("HX-Request", "true")
	r := b.do(t, p, req)
	webtest.WantStatus(t, r, http.StatusOK)
	if got := r.Header.Get("HX-Push-Url"); got != "/m/import/upload" {
		t.Errorf("HX-Push-Url = %q", got)
	}
	if r.Doc.Find("#wizard h1").Text() != "Upload" || r.Doc.Find("html body header").Length() != 0 {
		t.Errorf("want the #wizard fragment of Upload\n%s", webtest.Squash(r.Body))
	}

	// A stale step under htmx leaves by HX-Redirect.
	req = formRequest("/m/import/import", nil)
	req.Header.Set("HX-Request", "true")
	r = b.do(t, p, req)
	if got := r.Header.Get("HX-Redirect"); got != "/m/import/upload" {
		t.Errorf("HX-Redirect = %q", got)
	}
}

func TestHTMX_EveryStepMoveSwapsItsStep(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	b.choose(t, p, widget)
	b.upload(t, p, clean)
	hx := func(target, nav string) webtest.Response {
		req := formRequest(target, url.Values{"nav": {nav}})
		req.Header.Set("HX-Request", "true")
		return b.do(t, p, req)
	}
	for _, tc := range []struct {
		target, nav, push, heading string
	}{
		{"/m/import/check", "", "/m/import/import", "Import"},
		{"/m/import/import", "back", "/m/import/check", "Check results"},
		{"/m/import/check", "back", "/m/import/upload", "Upload"},
		{"/m/import/upload", "back", "/m/import/choose", "Choose master"},
	} {
		r := hx(tc.target, tc.nav)
		webtest.WantStatus(t, r, http.StatusOK)
		if got := r.Header.Get("HX-Push-Url"); got != tc.push {
			t.Errorf("POST %s nav=%q: HX-Push-Url = %q; want %q", tc.target, tc.nav, got, tc.push)
		}
		if got := r.Doc.Find("#wizard h1").Text(); got != tc.heading {
			t.Errorf("POST %s nav=%q: heading = %q; want %q", tc.target, tc.nav, got, tc.heading)
		}
	}
}

// Under htmx AUTH's refusal goes to the platform's error response, with
// the producing package's status.
func TestHTMX_RefusalKeepsItsStatus(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	b.choose(t, w.env.Kavya, widget)
	req := httptest.NewRequest(http.MethodGet, "/m/import/upload", nil)
	req.Header.Set("HX-Request", "true")
	webtest.WantStatus(t, b.do(t, w.env.Ravi, req), http.StatusForbidden)
}

func (w *world) exec(t *testing.T, q string) {
	t.Helper()
	ctx := context.Background()
	if err := w.env.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, q)
		return err
	}); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func (w *world) wantUnclaimed(t *testing.T) {
	t.Helper()
	if n := w.count(t, `SELECT count(*) FROM csv_upload WHERE claimed_at IS NOT NULL`); n != 0 {
		t.Errorf("%d rows still claimed after an import that imported nothing", n)
	}
}

func TestClaim_ARowBeingImportedIsRefused(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	b.choose(t, p, note)
	b.upload(t, p, "Name\r\nOne\r\nTwo\r\n")
	w.exec(t, `UPDATE csv_upload SET claimed_at = 'now'`)

	r := b.post(t, p, "/m/import/import", nil)
	webtest.WantStatus(t, r, http.StatusConflict)
	webtest.WantBanner(t, r, "This file is already being imported. Upload it again to import it again.")
	if n := w.count(t, `SELECT count(*) FROM mastersweb_note`); n != 0 {
		t.Errorf("%d rows imported from a claimed file", n)
	}

	// A new upload is the way out of a claim nobody released.
	b.upload(t, p, "Name\r\nOne\r\n")
	webtest.WantRedirect(t, b.post(t, p, "/m/import/import", nil), "/m/import/choose")
	if n := w.count(t, `SELECT count(*) FROM mastersweb_note`); n != 1 {
		t.Errorf("%d rows; want 1", n)
	}
}

func TestClaim_ARefusedImportLeavesTheFileImportable(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	b.choose(t, p, widget)
	b.upload(t, p, clean)
	w.exec(t, `INSERT INTO mastersweb_widget (code, name) VALUES ('W2', 'Taken')`)
	webtest.WantStatus(t, b.post(t, p, "/m/import/import", nil), http.StatusUnprocessableEntity)
	w.wantUnclaimed(t)

	w.exec(t, `DELETE FROM mastersweb_widget`)
	webtest.WantRedirect(t, b.post(t, p, "/m/import/import", nil), "/m/import/choose")
	if n := w.widgets(t); n != 2 {
		t.Errorf("%d rows; want 2", n)
	}
}

// A request cancelled mid-import can't keep the file: giving up the claim
// doesn't use the request's context.
func TestClaim_ACancelledRequestReleasesTheFile(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	b.choose(t, p, widget)
	b.upload(t, p, clean)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := formRequest("/m/import/import", nil).WithContext(context.WithValue(ctx, cancelKey{}, context.CancelFunc(cancel)))
	b.do(t, p, req)

	w.wantUnclaimed(t)
	if n := w.widgets(t); n != 0 {
		t.Errorf("%d rows imported", n)
	}
	webtest.WantRedirect(t, b.post(t, p, "/m/import/import", nil), "/m/import/choose")
	if n := w.widgets(t); n != 2 {
		t.Errorf("%d rows after the retry; want 2", n)
	}
}

func TestClaim_TwoPostsAtOnceImportOnce(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	b.choose(t, p, note)
	b.upload(t, p, "Name\r\nOne\r\nTwo\r\n")

	var cookies []*http.Cookie
	for _, c := range b.cookies {
		cookies = append(cookies, c)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			req := formRequest("/m/import/import", url.Values{})
			for _, c := range cookies {
				req.AddCookie(c)
			}
			<-start
			w.handler.ServeHTTP(httptest.NewRecorder(), req.WithContext(auth.WithPrincipal(req.Context(), p)))
		})
	}
	close(start)
	wg.Wait()
	if n := w.count(t, `SELECT count(*) FROM mastersweb_note`); n != 2 {
		t.Errorf("%d rows from a 2-row file posted 8 times at once", n)
	}
}

// The rows are committed, so a file that can't be deleted is no failure the
// user sees, and it can't import again.
func TestImport_FailedDeleteAfterCommit(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	b.choose(t, p, note)
	b.upload(t, p, "Name\r\nOne\r\n")
	w.exec(t, `CREATE TRIGGER keep_upload BEFORE DELETE ON csv_upload BEGIN SELECT RAISE(ABORT, 'kept'); END`)

	webtest.WantRedirect(t, b.post(t, p, "/m/import/import", nil), "/m/import/choose")
	if n := w.count(t, `SELECT count(*) FROM csv_upload WHERE claimed_at IS NOT NULL`); n != 1 {
		t.Errorf("%d claimed rows; want the row to stay claimed", n)
	}
	b.choose(t, p, note)
	r := b.post(t, p, "/m/import/import", nil)
	webtest.WantRedirect(t, r, "/m/import/upload")
	if n := w.count(t, `SELECT count(*) FROM mastersweb_note`); n != 1 {
		t.Errorf("%d rows; want 1", n)
	}
}

// Releasing is best effort: the page still says what was wrong.
func TestImport_FailedReleaseKeepsTheAnswer(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	b.choose(t, p, widget)
	b.upload(t, p, oneBad)
	w.exec(t, `CREATE TRIGGER keep_claim BEFORE UPDATE ON csv_upload WHEN NEW.claimed_at IS NULL BEGIN SELECT RAISE(ABORT, 'kept'); END`)
	webtest.WantStatus(t, b.post(t, p, "/m/import/import", nil), http.StatusUnprocessableEntity)
}

func TestErrorRows_Download(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	b.choose(t, p, widget)
	b.upload(t, p, oneBad)

	page := b.get(t, p, "/m/import/check")
	if page.Doc.Find(`a[href="/m/import/errors.csv"]`).Length() != 1 || !strings.Contains(page.Text(), "Download rows with errors") {
		t.Fatalf("no download link\n%s", page.Text())
	}
	r := b.get(t, p, "/m/import/errors.csv")
	webtest.WantStatus(t, r, http.StatusOK)
	if got := r.Header.Get("Content-Disposition"); got != `attachment; filename="mastersweb_widget-errors.csv"` {
		t.Errorf("Content-Disposition = %q", got)
	}
	if got := r.Header.Get("Content-Type"); got != "text/csv; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	sheet, err := csvio.ReadSheet(strings.NewReader(oneBad))
	if err != nil {
		t.Fatal(err)
	}
	rep := csvio.Report{Errors: []csvio.RowError{{Row: 3, Message: "Name is required."}}}
	if want := string(csvio.ErrorRowsCSV(sheet, rep)); r.Body != want {
		t.Errorf("body = %q; want %q", r.Body, want)
	}

	// AUTH's refusal, and another user's token, on the same session.
	denial := b.get(t, w.env.Ravi, "/m/import/errors.csv")
	webtest.WantStatus(t, denial, http.StatusForbidden)
	webtest.WantText(t, denial, denied)
	webtest.WantRedirect(t, b.get(t, w.env.Admin, "/m/import/errors.csv"), "/m/import/upload")
}

func TestErrorRows_NoLinkWithoutRowErrors(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	b.choose(t, p, widget)

	for name, file := range map[string]string{"clean": clean, "file-level problem": "Code\r\nW1\r\n"} {
		t.Run(name, func(t *testing.T) {
			b.upload(t, p, file)
			if b.get(t, p, "/m/import/check").Doc.Find(`a[href="/m/import/errors.csv"]`).Length() != 0 {
				t.Error("the page links to the rows with errors")
			}
			webtest.WantRedirect(t, b.get(t, p, "/m/import/errors.csv"), "/m/import/check")
		})
	}
}

func TestImport_FinishesWithHXRedirectUnderHTMX(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	p := w.env.Kavya
	b.choose(t, p, widget)
	b.upload(t, p, clean)
	req := formRequest("/m/import/import", nil)
	req.Header.Set("HX-Request", "true")
	r := b.do(t, p, req)
	if got := r.Header.Get("HX-Redirect"); got != "/m/import/choose" {
		t.Errorf("HX-Redirect = %q", got)
	}
	if n := w.widgets(t); n != 2 {
		t.Errorf("%d rows; want 2", n)
	}
}
