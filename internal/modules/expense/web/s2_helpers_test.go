package expenseweb_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	expenseweb "github.com/vardhan/internaltools/internal/modules/expense/web"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// S2 · Expense line page and receipt route (plan MOD-EXP S2; UX
// docs/ux/2026-10-31-expense.md §5.5; spec §3.3; CD-133, CD-251, CD-270,
// CD-275). These helpers serve the docscreen routes with the expense Modules
// and the module routes on one router, over expensetest's real engine,
// auth.Service and files.Store.

const (
	s2Deepak = "deepak@vardhan.test" // Employee, EMP-007; owns the claims
	s2Priya  = "priya@vardhan.test"  // Deepak's manager: team read, own write
	s2Kavya  = "kavya@vardhan.test"  // can't read Deepak's claims

	s2Today     = "24-10-2026" // expensetest's clock
	s2Yesterday = "23-10-2026"

	// AUTH's own-scope write refusal for Priya on Deepak's claim (CD-142).
	s2MsgOwnWrite = "You can only change Expense Claims you created."
	// FILE's texts (CD-142, CD-209).
	s2MsgTwoParts  = "Upload one file at a time."
	s2MsgTooLarge  = "File is larger than 10 MB. Choose a smaller file or compress it."
	s2MsgGIF       = "File type .gif is not allowed. Use PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or CSV."
	s2MsgLimit100  = "A document can have at most 100 attachments. Remove one first."
	s2MsgReuseLine = "This file is already on this claim, as the receipt on line 2. It is used for this expense too."
	s2MsgReuseNone = "This file is already on this claim. It is used for this expense."

	// UX §5.5's copy.
	s2UploadHint   = "Photo or PDF of the bill, up to 10 MB."
	s2AmountHint   = "Including any GST."
	s2NoteHint     = `Printed on the claim. For example "Pune to Mumbai, Volvo bus".`
	s2ReasonHint   = `For example "Auto rickshaw, no bill given" or "Paper file".`
	s2TickHint     = "Tick only when the supplier billed the company, not you. The accounts team decides whether GST can be claimed."
	s2TickOption   = "The bill shows our GSTIN"
	s2BillDate     = "Use the bill date."
	s2BilledToHint = "The branch whose GSTIN is printed on the bill."
	s2GSTHint      = "Part of the amount above, not added to it."
	s2SameSummary  = "Same receipt as another expense"
	s2SameOwn      = "No, a receipt of its own"
	s2NoReceipt    = "No receipt"
	s2GSTSummary   = "Bill in company GSTIN"
	s2ProjSummary  = "Project or site"
)

// s2MsgStale is DOC-069's text for draft id.
func s2MsgStale(id int64) string {
	return "Someone else changed Draft " + strconv.FormatInt(id, 10) + " after you opened it. Reload it and make your changes again."
}

type s2Env struct {
	*expensetest.Env
	router chi.Router
}

// s2New serves docscreen (with ClaimModule and AdvanceModule, as app.New
// wires them) and the expense module routes on one router.
func s2New(t *testing.T) *s2Env {
	t.Helper()
	e := expensetest.New(t)
	d := expenseweb.Deps{Service: e.Service, Engine: e.Engine, Auth: e.Auth, Files: e.Files}
	ds := docscreen.Deps{
		DB: e.DB, Engine: e.Engine, Auth: e.Auth, Extras: s2Extras{e},
		Today: func() (string, string) { return s2Today, s2Yesterday },
		Modules: map[string]docscreen.Module{
			expense.ClaimDocType:   expenseweb.ClaimModule(d),
			expense.AdvanceDocType: expenseweb.AdvanceModule(d),
		},
	}
	r := chi.NewRouter()
	docscreen.Routes(ds)(r)
	expenseweb.Routes(d)(r)
	return &s2Env{Env: e, router: r}
}

// s2Extras is docscreen.Extras for draft line pages: no approval, origin or
// history, and the record's attachments from the real store as p reads them.
type s2Extras struct{ e *expensetest.Env }

func (s2Extras) Approval(context.Context, auth.Principal, string, int64) (docscreen.ApprovalView, error) {
	return docscreen.ApprovalView{}, nil
}

func (s2Extras) Origin(context.Context, auth.Principal, string, int64) (docscreen.Origin, error) {
	return docscreen.Origin{}, nil
}

func (s2Extras) Audit(context.Context, auth.Principal, string, int64) ([]docscreen.HistoryRow, error) {
	return nil, nil
}

func (x s2Extras) Attachments(ctx context.Context, p auth.Principal, rec auth.RecordRef) ([]components.Attachment, error) {
	metas, err := x.e.Files.List(ctx, p, rec)
	if err != nil {
		return nil, err
	}
	out := make([]components.Attachment, 0, len(metas))
	for _, m := range metas {
		kind := "file"
		switch {
		case m.MIME == "image/heic" || m.MIME == "image/heif":
			kind = "heic"
		case strings.HasPrefix(m.MIME, "image/"):
			kind = "image"
		}
		out = append(out, components.Attachment{
			ID: m.ID, Name: m.Name, Size: files.HumanSize(m.Size), URL: "/files/" + strconv.FormatInt(m.ID, 10), Kind: kind,
		})
	}
	return out, nil
}

func s2ClaimPath(id int64) string {
	return "/doc/" + expense.ClaimDocType + "/" + strconv.FormatInt(id, 10)
}

// serve runs req as email (no principal when email is "").
func (e *s2Env) serve(t *testing.T, email string, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	if email != "" {
		req = req.WithContext(auth.WithPrincipal(req.Context(), e.P(t, email)))
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

// page GETs path as email; it must be 200.
func (e *s2Env) page(t *testing.T, email, path string) *goquery.Document {
	t.Helper()
	rec := e.serve(t, email, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s as %s: status %d, want 200 (body %q)", path, email, rec.Code, rec.Body.String())
	}
	return s2Parse(t, rec)
}

// postForm posts form to path as email, as the browser posts the line form.
func (e *s2Env) postForm(t *testing.T, email, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return e.serve(t, email, req)
}

// s2Part is one multipart part.
type s2Part struct {
	field, name string
	body        []byte
}

// upload posts parts to the receipt route of claim id as email, as
// FileUpload's htmx form does.
func (e *s2Env) upload(t *testing.T, email string, id string, version string, parts ...s2Part) *httptest.ResponseRecorder {
	t.Helper()
	return e.serve(t, email, e.uploadRequest(t, id, version, parts...))
}

// uploadRequest is the htmx multipart POST of parts to claim id's receipt
// route, with no principal: the version in the action's query (the company
// logo page's pattern, companyweb logo.go), "" leaving it out.
func (e *s2Env) uploadRequest(t *testing.T, id string, version string, parts ...s2Part) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		w, err := mw.CreateFormFile(p.field, p.name)
		if err != nil {
			t.Fatalf("CreateFormFile(%q, %q): %v", p.field, p.name, err)
		}
		if _, err := w.Write(p.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	target := "/modules/expense/claims/" + id + "/receipt"
	if version != "" {
		target += "?version=" + url.QueryEscape(version)
	}
	req := httptest.NewRequest(http.MethodPost, target, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("HX-Request", "true")
	return req
}

// file is one "file" part.
func s2File(name string, body []byte) s2Part { return s2Part{field: "file", name: name, body: body} }

func s2Parse(t *testing.T, rec *httptest.ResponseRecorder) *goquery.Document {
	t.Helper()
	d, err := goquery.NewDocumentFromReader(strings.NewReader(rec.Body.String()))
	if err != nil {
		t.Fatalf("parse body: %v", err)
	}
	return d
}

// s2Text is s's text with spaces collapsed.
func s2Text(s *goquery.Selection) string { return strings.Join(strings.Fields(s.Text()), " ") }

// s2Refusal carries a text for expensetest's WantRefused.
type s2Refusal string

func (r s2Refusal) Error() string { return string(r) }

// refused runs do, which must answer status with msg as FILE's htmx refusal
// (CD-251), and write nothing: no attachment, audit row or version change
// (expensetest.WantRefused's fingerprint). msg "" checks only the status.
func (e *s2Env) refused(t *testing.T, what string, status int, msg string, do func() *httptest.ResponseRecorder) {
	t.Helper()
	text := msg
	if text == "" {
		text = "-"
	}
	var rec *httptest.ResponseRecorder
	e.WantRefused(t, text, func() error {
		rec = do()
		return s2Refusal(text)
	})
	if msg == "" {
		if rec.Code != status {
			t.Errorf("%s: status %d, want %d (body %q)", what, rec.Code, status, rec.Body.String())
		}
		return
	}
	s2WantNotice(t, what, rec, status, msg)
}

// s2WantNotice checks an htmx refusal as files/web gives it (CD-251): the
// status, an empty body, HX-Reswap none and an HX-Trigger that decodes to
// exactly the error notice, so the upload's own region shows msg and the
// typed fields stay.
func s2WantNotice(t *testing.T, what string, rec *httptest.ResponseRecorder, status int, msg string) {
	t.Helper()
	if rec.Code != status {
		t.Errorf("%s: status %d, want %d (body %q)", what, rec.Code, status, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("%s: body %q, want empty", what, rec.Body.String())
	}
	if got := rec.Header().Values("HX-Reswap"); len(got) != 1 || got[0] != "none" {
		t.Errorf("%s: HX-Reswap = %q, want exactly [\"none\"]", what, got)
	}
	trigger := rec.Header().Values("HX-Trigger")
	if len(trigger) != 1 {
		t.Errorf("%s: HX-Trigger = %q, want exactly one", what, trigger)
		return
	}
	var got any
	if err := json.Unmarshal([]byte(trigger[0]), &got); err != nil {
		t.Errorf("%s: HX-Trigger = %q is not JSON: %v", what, trigger[0], err)
		return
	}
	want := map[string]any{"notice": map[string]any{"level": "error", "text": msg}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s: HX-Trigger decodes to %v, want %v", what, got, want)
	}
}

// exec runs q in one write.
func (e *s2Env) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	err := e.DB.Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(context.Background(), q, args...)
		return err
	})
	if err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

// id reads one id.
func (e *s2Env) id(t *testing.T, q string, args ...any) int64 {
	t.Helper()
	var id int64
	err := e.DB.Read(context.Background(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(context.Background(), q, args...).Scan(&id)
	})
	if err != nil {
		t.Fatalf("read %q: %v", q, err)
	}
	return id
}

// version is claim id's stored version.
func (e *s2Env) version(t *testing.T, id int64) int {
	t.Helper()
	return int(e.id(t, `SELECT version FROM expense_claim WHERE id = ?`, id))
}

// line is stored line no (1-based) of claim id.
func (e *s2Env) line(t *testing.T, id int64, no int) map[string]string {
	t.Helper()
	rec := e.LoadDoc(t, expense.ClaimDocType, id)
	if no > len(rec.Lines) {
		t.Fatalf("claim %d has %d lines, want line %d", id, len(rec.Lines), no)
	}
	return rec.Lines[no-1]
}

// withReceipts attaches a file per hash-holding line as Deepak and saves
// claim id's lines with those receipts: receipts[n] names line n's file
// ("" keeps it without one). Lines naming the same file share its hash. It
// returns each file's Meta by name.
func (e *s2Env) withReceipts(t *testing.T, id int64, receipts map[int]string) map[string]files.Meta {
	t.Helper()
	deepak := e.P(t, s2Deepak)
	metas := map[string]files.Meta{}
	lines := map[int]expensetest.Values{}
	for no, name := range receipts {
		m, ok := metas[name]
		if !ok {
			m = e.MustAttach(t, deepak, expense.ClaimDocType, id, name, s2Bytes(name))
			metas[name] = m
		}
		lines[no] = expensetest.Values{"receipt_sha256": m.SHA256, "no_receipt_reason": ""}
	}
	if _, err := e.EditClaim(t, deepak, id, nil, lines); err != nil {
		t.Fatalf("save claim %d with receipts %v: %v", id, receipts, err)
	}
	return metas
}

// s2Bytes are JPEG bytes unique to name.
func s2Bytes(name string) []byte {
	b := []byte("\xFF\xD8\xFF\xE0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00" + name)
	return append(b, make([]byte, 2048)...)
}

// lineForm is the line page's #line-form.
func s2LineForm(t *testing.T, d *goquery.Document) *goquery.Selection {
	t.Helper()
	f := d.Find("form#line-form")
	if f.Length() != 1 {
		t.Fatalf("the line page has %d form#line-form, want 1", f.Length())
	}
	return f
}

// s2Field is the .field around the first control named name in s.
func s2Field(t *testing.T, s *goquery.Selection, name string) *goquery.Selection {
	t.Helper()
	c := s.Find(`[name="` + name + `"]`).First()
	if c.Length() == 0 {
		t.Fatalf("no control named %s", name)
	}
	return c.Closest(".field")
}

// s2Hint is the field hint of the control named name: the .field__hint that
// is the .field's own child (not an option's).
func s2Hint(t *testing.T, s *goquery.Selection, name string) string {
	t.Helper()
	return s2Text(s2Field(t, s, name).ChildrenFiltered(".field__hint"))
}

// s2Label is the field label (label or legend) of the control named name.
func s2Label(t *testing.T, s *goquery.Selection, name string) string {
	t.Helper()
	// The UX copy gives the label's name: a required field's asterisk and
	// hidden " (required)" (CD-341 (2)) are left out by class.
	l := s2Field(t, s, name).ChildrenFiltered(".field__label").Clone()
	l.Find(".field__req, .visually-hidden").Remove()
	return s2Text(l)
}

// s2Required tells whether the field named name is marked required: the
// label's asterisk and hidden " (required)", and aria-required on its control.
func s2Required(t *testing.T, s *goquery.Selection, name string) bool {
	t.Helper()
	l := s2Field(t, s, name).ChildrenFiltered(".field__label")
	return strings.TrimSpace(l.Find(".field__req").Text()) == "*" &&
		l.Find(".visually-hidden").Text() == " (required)" &&
		s.Find(`[name="`+name+`"][aria-required="true"]`).Length() > 0
}

// s2Option is one radio or checkbox option: label, hint, value, checked.
type s2Option struct {
	Label, Hint, Value string
	Checked            bool
}

// s2Options are the radio or checkbox options named name in s, in order.
func s2Options(s *goquery.Selection, name string) []s2Option {
	var out []s2Option
	s.Find(`input[name="` + name + `"]`).Each(func(_ int, in *goquery.Selection) {
		row := in.Closest("label")
		text := row.Find(".field__choice-text").Clone()
		hint := s2Text(text.Find(".field__hint"))
		text.Find(".field__hint").Remove()
		_, checked := in.Attr("checked")
		out = append(out, s2Option{Label: s2Text(text), Hint: hint, Value: in.AttrOr("value", ""), Checked: checked})
	})
	return out
}

// s2Labels are the options' labels.
func s2Labels(opts []s2Option) []string {
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		out = append(out, o.Label)
	}
	return out
}

// s2ValueOf is the value of the option labelled label.
func s2ValueOf(t *testing.T, opts []s2Option, label string) string {
	t.Helper()
	for _, o := range opts {
		if o.Label == label {
			return o.Value
		}
	}
	t.Fatalf("no option %q among %q", label, s2Labels(opts))
	return ""
}

// s2Details is the <details> in s whose summary reads summary.
func s2Details(s *goquery.Selection, summary string) *goquery.Selection {
	return s.Find("details").FilterFunction(func(_ int, d *goquery.Selection) bool {
		return s2Text(d.ChildrenFiltered("summary")) == summary
	})
}

// s2Summaries are the summaries of every <details> in s, in order.
func s2Summaries(s *goquery.Selection) []string {
	var out []string
	s.Find("details").Each(func(_ int, d *goquery.Selection) {
		out = append(out, s2Text(d.ChildrenFiltered("summary")))
	})
	return out
}

// s2Open reports whether details d is open.
func s2Open(d *goquery.Selection) bool {
	_, ok := d.Attr("open")
	return ok
}

// s2HiddenReceipt is the line form's one hidden receipt hash input, posted
// as receipt_hash: the module hides the field receipt_sha256, so no page has
// an input of that name (CD-319).
func s2HiddenReceipt(t *testing.T, form *goquery.Selection) *goquery.Selection {
	t.Helper()
	in := form.Find(`input[name="receipt_hash"]`)
	if in.Length() != 1 || in.AttrOr("type", "") != "hidden" {
		t.Fatalf("the line form has %d receipt_hash inputs (type %q), want one hidden", in.Length(), in.AttrOr("type", ""))
	}
	if in.AttrOr("id", "") == "" {
		t.Errorf("the hidden receipt_hash input has no id; the upload answer swaps it out of band by id (UX §5.5)")
	}
	return in
}

// s2Base is the line form's posted values for a new expense: the page's
// version, Food, ₹450 today, a note, and the receipt hash.
func s2Base(t *testing.T, d *goquery.Document, hash string) url.Values {
	t.Helper()
	form := s2LineForm(t, d)
	return url.Values{
		"version":           {form.Find(`input[name="version"]`).AttrOr("value", "")},
		"expense_type_id":   {s2ValueOf(t, s2Options(form, "expense_type_id"), "Food")},
		"amount":            {"450"},
		"expense_date":      {s2Today},
		"description":       {"Lunch at site canteen"},
		"receipt_hash":      {hash},
		"no_receipt_reason": {""},
	}
}
