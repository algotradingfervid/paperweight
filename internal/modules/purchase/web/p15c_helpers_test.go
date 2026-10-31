package purchaseweb

import (
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// Helpers for P15c's tests (Add from requests; UX §5.6 the link, §5.7; PO
// spec §20.4 item 2). All names carry p15c.

// UX §5.6 and §5.7 texts.
const (
	p15cHeading   = "Add from requests"
	p15cPrimary   = "Add selected"
	p15cBack      = "Back to items"
	p15cQtyLabel  = "Quantity to order"
	p15cNoTick    = "Tick at least one line to add."
	p15cUnreadOne = "On another order"
)

// p15cLink is the Items step link text for n open request lines.
func p15cLink(n int) string { return "Add from requests (" + strconv.Itoa(n) + " open)" }

// p15cStale is DOC-069's stale text for display.
func p15cStale(display string) string {
	return "Someone else changed " + display + " after you opened it. Reload it and make your changes again."
}

// p15cWeb is a purchase world on the real AUTH with docscreen's routes using
// purchase's Modules, and the purchase module routes, inside the session
// middleware (as p15dWeb).
type p15cWeb struct {
	env *purchasetest.Env
	h   http.Handler
}

func newP15cWeb(t *testing.T) *p15cWeb {
	t.Helper()
	env := purchasetest.New(t)
	return &p15cWeb{env: env, h: p15cHandler(env, env.Service)}
}

// p15cHandler serves env's screens, with svc as the screens' Service.
func p15cHandler(env *purchasetest.Env, svc *purchase.Service) http.Handler {
	sm := scs.New()
	d := Deps{
		Service: svc, Approvals: env.Approvals, Comments: env.Comments, Sessions: sm,
		CSRF: func(*http.Request) string { return "p15c-token" },
	}
	r := chi.NewRouter()
	docscreen.Routes(docscreen.Deps{
		DB: env.DB, Engine: env.Engine, Auth: env.Auth, Approval: env.Approvals, Extras: docscreentest.NewExtras(),
		Today: func() (string, string) {
			today := types.Today(env.Clock)
			return types.FormatDate(today), types.FormatDate(types.AddDays(today, -1))
		},
		Modules: Modules(d),
	})(r)
	Routes(d)(r)
	return sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.ServeHTTP(w, req.WithContext(web.WithSessions(req.Context(), sm)))
	}))
}

// p15cPage is one answer.
type p15cPage struct {
	status   int
	location string
	doc      *goquery.Document
}

func (p p15cPage) text() string { return p15cCollapse(p.doc.Text()) }

// find is the collapsed text of every element matching sel that has any.
func (p p15cPage) find(sel string) []string {
	var out []string
	p.doc.Find(sel).Each(func(_ int, s *goquery.Selection) {
		if t := p15cCollapse(s.Text()); t != "" {
			out = append(out, t)
		}
	})
	return out
}

// errors are the error summary's messages.
func (p p15cPage) errors() []string { return p.find(".error-summary li") }

// link is the href of every link or button-link whose text is text.
func (p p15cPage) link(text string) []string {
	var out []string
	p.doc.Find("a").Each(func(_ int, s *goquery.Selection) {
		if p15cCollapse(s.Text()) == text {
			h, _ := s.Attr("href")
			out = append(out, h)
		}
	})
	return out
}

func p15cCollapse(s string) string { return strings.Join(strings.Fields(s), " ") }

func (w *p15cWeb) do(t *testing.T, p auth.Principal, method, path string, form url.Values) p15cPage {
	t.Helper()
	return p15cDo(t, w.h, p, method, path, form)
}

func p15cDo(t *testing.T, h http.Handler, p auth.Principal, method, path string, form url.Values) p15cPage {
	t.Helper()
	body := strings.NewReader("")
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequestWithContext(auth.WithPrincipal(context.Background(), p), method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	d, err := goquery.NewDocumentFromReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("%s %s: parse body: %v", method, path, err)
	}
	return p15cPage{status: rec.Code, location: rec.Header().Get("Location"), doc: d}
}

func (w *p15cWeb) get(t *testing.T, p auth.Principal, path string) p15cPage {
	t.Helper()
	return w.do(t, p, http.MethodGet, path, nil)
}

// id is the id, as a form value, of the master row with code in table.
func (w *p15cWeb) id(t *testing.T, table, code string) string {
	t.Helper()
	var n int64
	if err := w.env.DB.SQL().QueryRowContext(t.Context(), `SELECT id FROM `+table+` WHERE code = ?`, code).Scan(&n); err != nil { //nolint:gosec // G202: fixed table names
		t.Fatalf("id of %s %s: %v", table, code, err)
	}
	return strconv.FormatInt(n, 10)
}

// row is a request line of qty of item in unit uom.
func (w *p15cWeb) row(t *testing.T, item, uom, qty string) map[string]string {
	t.Helper()
	return map[string]string{"item_id": w.id(t, "item", item), "uom_id": w.id(t, "uom", uom), "qty": qty}
}

// request is Suresh's submitted request on site / project with rows, needed
// by 07-11-2026.
func (w *p15cWeb) request(t *testing.T, site, project string, rows ...map[string]string) doc.Doc {
	t.Helper()
	return purchasetest.NewRequest(t, w.env,
		purchasetest.RequestHeader(map[string]string{
			"site_id": w.id(t, "site", site), "project_id": w.id(t, "project", project), "required_by": "07-11-2026",
		}),
		purchasetest.RequestLines(rows...))
}

// prA is PR-A (METRO-SITE / PRJ-METRO): 40 BAG of CEMENT-OPC and 25 KGS of
// WIRE-18G, plus rows.
func (w *p15cWeb) prA(t *testing.T, rows ...map[string]string) doc.Doc {
	t.Helper()
	return w.request(t, "METRO-SITE", "PRJ-METRO",
		append([]map[string]string{w.row(t, "CEMENT-OPC", "BAG", "40"), w.row(t, "WIRE-18G", "KGS", "25")}, rows...)...)
}

// hold is a PO by Priya (stopping at stop) with one line of qty on request
// line lineID, in that line's item and unit.
func (w *p15cWeb) hold(t *testing.T, stop purchasetest.OrderOpt, lineID int64, item, uom, hsn, qty string) doc.Doc {
	t.Helper()
	return purchasetest.NewOrder(t, w.env, stop, purchasetest.OrderLines(map[string]string{
		"item_id": w.id(t, "item", item), "uom_id": w.id(t, "uom", uom), "hsn_sac": hsn, "qty": qty,
		"request_line_id": strconv.FormatInt(lineID, 10),
	}))
}

// draft is Priya's METRO-SITE / PRJ-METRO draft from SHREE with
// purchasetest's one default line (100 kg MS-ANGLE-50).
func (w *p15cWeb) draft(t *testing.T) doc.Doc {
	t.Helper()
	return purchasetest.NewOrder(t, w.env, purchasetest.StopAtDraft())
}

// saveAs saves a new PO draft as p with header values and lines through
// Engine.Do and returns its id.
func (w *p15cWeb) saveAs(t *testing.T, p auth.Principal, header map[string]string, lines ...map[string]string) int64 {
	t.Helper()
	res, err := w.env.Engine.Do(t.Context(), p, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{Values: header, Lines: lines})
	if err != nil {
		t.Fatalf("save a PO as %s: %v", p.Name, err)
	}
	return res.ID
}

// act runs action a on document id of dt as p at its stored version.
func (w *p15cWeb) act(t *testing.T, p auth.Principal, dt string, id int64, a doctype.Action, reason string) {
	t.Helper()
	ctx := t.Context()
	var v int
	if err := w.env.DB.Read(ctx, func(tx *db.Tx) error {
		d, err := doc.LoadDoc(ctx, tx, dt, id)
		v = d.Version
		return err
	}); err != nil {
		t.Fatalf("load %s %d: %v", dt, id, err)
	}
	if _, err := w.env.Engine.Do(ctx, p, dt, id, a, doc.ActionInput{Version: v, Reason: reason}); err != nil {
		t.Fatalf("%s %s %d as %s: %v", a, dt, id, p.Name, err)
	}
}

// version is PO id's stored version.
func (w *p15cWeb) version(t *testing.T, id int64) int {
	t.Helper()
	var v int
	if err := w.env.DB.SQL().QueryRowContext(t.Context(), `SELECT version FROM purchase_order WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatalf("version of PO %d: %v", id, err)
	}
	return v
}

// p15cLine is one stored PO line.
type p15cLine struct {
	no                        int
	item, uom, qty            int64
	price, discount, rqLineID sql.NullInt64
}

// lines are PO id's stored lines in line order.
func (w *p15cWeb) lines(t *testing.T, id int64) []p15cLine {
	t.Helper()
	rows, err := w.env.DB.SQL().QueryContext(t.Context(), `SELECT line_no, item_id, uom_id, qty, price, discount_pct, request_line_id
		FROM purchase_order_line WHERE parent_id = ? ORDER BY line_no`, id)
	if err != nil {
		t.Fatalf("lines of PO %d: %v", id, err)
	}
	defer func() { _ = rows.Close() }()
	var out []p15cLine
	for rows.Next() {
		var l p15cLine
		if err := rows.Scan(&l.no, &l.item, &l.uom, &l.qty, &l.price, &l.discount, &l.rqLineID); err != nil {
			t.Fatal(err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// lineCount is the number of PO lines in the database.
func (w *p15cWeb) lineCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := w.env.DB.SQL().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM purchase_order_line`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// p15cItems is draft id's Items step URL; p15cFrom is its Add from requests
// view.
func p15cItems(id int64) string {
	return "/doc/" + purchase.DocType + "/" + strconv.FormatInt(id, 10) + "/edit/items"
}

func p15cFrom(id int64) string { return p15cItems(id) + "?from=requests" }

// p15cDisplay is DOC's display of a stored document.
func p15cDisplay(d doc.Doc) string {
	if d.Number != "" {
		return d.Number
	}
	return "Draft " + strconv.FormatInt(d.ID, 10)
}

func p15cDraftName(id int64) string { return "Draft " + strconv.FormatInt(id, 10) }

// p15cCard is the section.card whose title is title (empty selection when
// none).
func p15cCard(p p15cPage, title string) *goquery.Selection {
	return p.doc.Find("section.card").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return p15cCollapse(s.Find("h2.card-title").First().Text()) == title
	}).First()
}

// p15cRow is one tickable request row as the view renders it: its checkbox
// (Checkboxes with one option) and its quantity input, paired in order.
type p15cRow struct {
	box, qty *goquery.Selection
}

func (r p15cRow) label() string {
	return p15cCollapse(r.box.Closest("label").Find(".field__choice-text").First().Contents().First().Text())
}
func (r p15cRow) hint() string {
	return p15cCollapse(r.box.Closest("label").Find(".field__hint").First().Text())
}
func (r p15cRow) value() string { v, _ := r.qty.Attr("value"); return v }
func (r p15cRow) ticked() bool  { _, ok := r.box.Attr("checked"); return ok }

// rows are card's tickable rows: each checkbox paired with the quantity input
// (a NumberInput, class num) at the same position. A held row has neither.
func p15cRows(t *testing.T, card *goquery.Selection) []p15cRow {
	t.Helper()
	boxes, qtys := card.Find(`input[type="checkbox"]`), card.Find(`input.num`)
	if boxes.Length() != qtys.Length() {
		t.Fatalf("card has %d checkboxes and %d quantity inputs, want one of each per open row", boxes.Length(), qtys.Length())
	}
	out := make([]p15cRow, boxes.Length())
	for i := range out {
		out[i] = p15cRow{box: boxes.Eq(i), qty: qtys.Eq(i)}
	}
	return out
}

// p15cFind is the row whose checkbox label holds code.
func p15cFind(t *testing.T, rows []p15cRow, code string) p15cRow {
	t.Helper()
	for _, r := range rows {
		if strings.Contains(r.label(), code) {
			return r
		}
	}
	t.Fatalf("no tickable row for %s", code)
	return p15cRow{}
}

// p15cForm is the step form's post as the browser sends it after the
// primary labelled primary is pressed: every named input (checkboxes only
// when ticked, a tick in tick added, qty values overridden by set, keyed by
// the input's name) plus the primary's own name and value. It returns the
// form's action too.
func p15cForm(t *testing.T, p p15cPage, primary string, tick []p15cRow, set map[*goquery.Selection]string) (string, url.Values) {
	t.Helper()
	form := p.doc.Find("form#wizard-form")
	if form.Length() != 1 {
		t.Fatalf("page has %d step forms: %s", form.Length(), p.text())
	}
	action, _ := form.Attr("action")
	ticked := map[string]bool{}
	for _, r := range tick {
		n, _ := r.box.Attr("name")
		v, _ := r.box.Attr("value")
		ticked[n+"\x00"+v] = true
	}
	overrides := map[string]string{}
	for s, v := range set {
		n, _ := s.Attr("name")
		overrides[n] = v
	}
	out := url.Values{}
	form.Find("input, select, textarea").Each(func(_ int, s *goquery.Selection) {
		name, ok := s.Attr("name")
		if !ok || name == "" {
			return
		}
		typ, _ := s.Attr("type")
		value, _ := s.Attr("value")
		switch {
		case goquery.NodeName(s) == "select":
			value, _ = s.Find("option[selected]").Attr("value")
		case goquery.NodeName(s) == "textarea":
			value = s.Text()
		case typ == "checkbox" || typ == "radio":
			_, checked := s.Attr("checked")
			if !checked && !ticked[name+"\x00"+value] {
				return
			}
		case typ == "submit" || typ == "button":
			return
		}
		if v, ok := overrides[name]; ok {
			value = v
		}
		out.Add(name, value)
	})
	pressed := false
	p15cButtons(p).Each(func(_ int, s *goquery.Selection) {
		if pressed || p15cCollapse(s.Text()) != primary {
			return
		}
		pressed = true
		if n, ok := s.Attr("name"); ok && n != "" {
			v, _ := s.Attr("value")
			out.Set(n, v)
		}
	})
	if !pressed {
		t.Fatalf("step form has no %q button: %s", primary, p.text())
	}
	return action, out
}

// p15cButtons are the buttons posting the step form: in it, or after it
// with form="wizard-form" when the generic Items body follows the form.
func p15cButtons(p p15cPage) *goquery.Selection {
	return p.doc.Find(`form#wizard-form button, button[form="wizard-form"]`)
}

// p15cPrimaryNav is the name=value of the step form's button labelled label
// ("" when it has none, "missing" when there is no such button).
func p15cPrimaryNav(p p15cPage, label string) string {
	out := "missing"
	p15cButtons(p).Each(func(_ int, s *goquery.Selection) {
		if out != "missing" || p15cCollapse(s.Text()) != label {
			return
		}
		n, _ := s.Attr("name")
		v, _ := s.Attr("value")
		out = n + "=" + v
	})
	return out
}

// p15cDocVersion is the redraw's #doc-version value.
func p15cDocVersion(p p15cPage) string {
	v, _ := p.doc.Find("#doc-version").Attr("value")
	return v
}

// p15cQty, p15cRate and p15cPct are stored values of form text.
func p15cQty(t *testing.T, s string) int64 {
	t.Helper()
	q, err := types.ParseQty(s)
	if err != nil {
		t.Fatal(err)
	}
	return int64(q)
}

func p15cRate(t *testing.T, s string) int64 {
	t.Helper()
	r, err := types.ParseRate(s)
	if err != nil {
		t.Fatal(err)
	}
	return int64(r)
}

func p15cPct(t *testing.T, s string) int64 {
	t.Helper()
	r, err := types.ParsePercent(s)
	if err != nil {
		t.Fatal(err)
	}
	return int64(r)
}

func p15cInt(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// probe is a new user restricted to site METRO-SITE on the real AUTH, with
// the Employee role (requests: own; master reads for its links) and a role
// holding, on purchase.Order, only read (own), create (all) and write,
// submit and delete (own), so another buyer's order is one it can't read and
// another requester's request one it can't read either.
func (w *p15cWeb) probe(t *testing.T, name string) auth.Principal {
	t.Helper()
	ctx, env := t.Context(), w.env
	role := "P15c " + name
	if err := env.Auth.CreateRole(ctx, env.Admin, role); err != nil {
		t.Fatalf("CreateRole(%s): %v", role, err)
	}
	if err := env.Auth.SetRights(ctx, env.Admin, role, purchase.DocType, map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeOwn, auth.RightCreate: auth.ScopeAll, auth.RightWrite: auth.ScopeOwn,
		auth.RightSubmit: auth.ScopeOwn, auth.RightDelete: auth.ScopeOwn,
	}); err != nil {
		t.Fatalf("SetRights(%s): %v", role, err)
	}
	id, _, err := env.Auth.CreateUser(ctx, env.Admin, auth.NewUser{Email: "p15c-" + name + "@vardhan.test", Name: role, Password: "Correct-Horse-42", Roles: []string{"Employee", role}})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", role, err)
	}
	site := p15cInt(t, w.id(t, "site", "METRO-SITE"))
	if err := env.Auth.ChangeRestrictions(ctx, env.Admin, id, auth.DimSite, []int64{site}, nil, nil); err != nil {
		t.Fatalf("restrict %s: %v", role, err)
	}
	p, _, err := env.Auth.LoadPrincipal(ctx, id)
	if err != nil {
		t.Fatalf("LoadPrincipal(%s): %v", role, err)
	}
	return p
}
