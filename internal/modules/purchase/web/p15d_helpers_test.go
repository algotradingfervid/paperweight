package purchaseweb

import (
	"bytes"
	"context"
	"encoding/json"
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

// Helpers for P15d's tests (Request screens; UX §5.14 to §5.18). All names
// carry p15d.

// p15dWeb is a purchase world on the real AUTH with docscreen's routes, using
// purchase's Modules, and the purchase module routes on one router inside
// the session middleware (AUTH's guard and CSRF are internal/app's).
type p15dWeb struct {
	env *purchasetest.Env
	h   http.Handler
}

func newP15dWeb(t *testing.T) *p15dWeb {
	t.Helper()
	env := purchasetest.New(t)
	sm := scs.New()
	d := Deps{
		Service: env.Service, Approvals: env.Approvals, Comments: env.Comments, Sessions: sm,
		CSRF: func(*http.Request) string { return "p15d-token" },
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
	r.Get("/p15d-flash", func(w http.ResponseWriter, req *http.Request) {
		_ = json.NewEncoder(w).Encode(web.TakeFlash(req.Context()))
	})
	h := sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.ServeHTTP(w, req.WithContext(web.WithSessions(req.Context(), sm)))
	}))
	return &p15dWeb{env: env, h: h}
}

// p15dPage is one answer: status, Location and the parsed body.
type p15dPage struct {
	status   int
	location string
	doc      *goquery.Document
	cookies  []*http.Cookie
}

// text is the page's text, spaces collapsed.
func (p p15dPage) text() string { return p15dCollapse(p.doc.Text()) }

// find is the collapsed text of every element matching sel that has any,
// in order.
func (p p15dPage) find(sel string) []string {
	var out []string
	p.doc.Find(sel).Each(func(_ int, s *goquery.Selection) {
		if t := p15dCollapse(s.Text()); t != "" {
			out = append(out, t)
		}
	})
	return out
}

// notes are the line cards' note lines (each card's meta lines but the
// quantity one).
func (p p15dPage) notes() []string {
	return p.find(".lines-card > p.lines-meta:not(:has(.num))")
}

// banners are the messages of main's banners of kind (info, danger), without
// the kind's word or the link.
func (p p15dPage) banners(kind string) []string {
	var out []string
	for _, s := range p.find("main .banner--" + kind + " .banner-body > p:first-child") {
		_, msg, _ := strings.Cut(s, ": ")
		out = append(out, msg)
	}
	return out
}

func p15dCollapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// do sends method path as p, with form as a plain form post when not nil.
func (w *p15dWeb) do(t *testing.T, p auth.Principal, method, path string, form url.Values) p15dPage {
	t.Helper()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequestWithContext(auth.WithPrincipal(context.Background(), p), method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	w.h.ServeHTTP(rec, req)
	d, err := goquery.NewDocumentFromReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("%s %s: parse body: %v", method, path, err)
	}
	return p15dPage{status: rec.Code, location: rec.Header().Get("Location"), doc: d, cookies: rec.Result().Cookies()}
}

// flash is the flash the next page after a would show, read with a's
// session cookie, so a flash put after the response started is lost here as
// it is in production.
func (w *p15dWeb) flash(t *testing.T, a p15dPage) []web.Notice {
	t.Helper()
	probe := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/p15d-flash", nil)
	for _, c := range a.cookies {
		probe.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	w.h.ServeHTTP(rec, probe)
	var out []web.Notice
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("read the flash: %v (%q)", err, rec.Body.String())
	}
	return out
}

func (w *p15dWeb) get(t *testing.T, p auth.Principal, path string) p15dPage {
	t.Helper()
	return w.do(t, p, http.MethodGet, path, nil)
}

// id is the id, as a form value, of the master row with code in table.
func (w *p15dWeb) id(t *testing.T, table, code string) string {
	t.Helper()
	var n int64
	if err := w.env.DB.SQL().QueryRowContext(t.Context(), `SELECT id FROM `+table+` WHERE code = ?`, code).Scan(&n); err != nil { //nolint:gosec // G202: fixed table names
		t.Fatalf("id of %s %s: %v", table, code, err)
	}
	return strconv.FormatInt(n, 10)
}

// row is a request line of qty of item in unit uom, with note.
func (w *p15dWeb) row(t *testing.T, item, uom, qty, note string) map[string]string {
	t.Helper()
	return map[string]string{"item_id": w.id(t, "item", item), "uom_id": w.id(t, "uom", uom), "qty": qty, "note": note}
}

// prA is Suresh's submitted PR-A (METRO-SITE / PRJ-METRO): 40 Bag of
// CEMENT-OPC and 25 kg of WIRE-18G, needed by 07-11-2026.
func (w *p15dWeb) prA(t *testing.T, opts ...purchasetest.RequestOpt) doc.Doc {
	t.Helper()
	return purchasetest.NewRequest(t, w.env, append([]purchasetest.RequestOpt{
		purchasetest.RequestHeader(map[string]string{"required_by": "07-11-2026", "purpose": "Slab casting, level 3"}),
		purchasetest.RequestLines(w.row(t, "CEMENT-OPC", "BAG", "40", "UltraTech or ACC only"), w.row(t, "WIRE-18G", "KGS", "25", "")),
	}, opts...)...)
}

// hold is a PO by Priya (stopping at stop) with one line of qty on request
// line lineID, in that line's item and unit.
func (w *p15dWeb) hold(t *testing.T, stop purchasetest.OrderOpt, lineID int64, item, uom, hsn, qty string) doc.Doc {
	t.Helper()
	return purchasetest.NewOrder(t, w.env, stop, purchasetest.OrderLines(map[string]string{
		"item_id": w.id(t, "item", item), "uom_id": w.id(t, "uom", uom), "hsn_sac": hsn, "qty": qty,
		"request_line_id": strconv.FormatInt(lineID, 10),
	}))
}

// act runs action a on document id of dt as p at its stored version, with
// reason.
func (w *p15dWeb) act(t *testing.T, p auth.Principal, dt string, id int64, a doctype.Action, reason string) {
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

// probe is a new user whose only role holds read and rights on
// purchase.Order (AUTH needs read for any other right) and read (all) on
// purchase.Request (lessons §5: a rights test needs a user holding only the
// right the rule names).
func (w *p15dWeb) probe(t *testing.T, name string, rights ...auth.Right) auth.Principal {
	t.Helper()
	ctx, env := t.Context(), w.env
	role := "P15d " + name
	if err := env.Auth.CreateRole(ctx, env.Admin, role); err != nil {
		t.Fatalf("CreateRole(%s): %v", role, err)
	}
	order := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}
	for _, r := range rights {
		order[r] = auth.ScopeAll
	}
	if err := env.Auth.SetRights(ctx, env.Admin, role, purchase.DocType, order); err != nil {
		t.Fatalf("SetRights(%s, order): %v", role, err)
	}
	if err := env.Auth.SetRights(ctx, env.Admin, role, purchase.RequestDocType, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}); err != nil {
		t.Fatalf("SetRights(%s, request): %v", role, err)
	}
	id, _, err := env.Auth.CreateUser(ctx, env.Admin, auth.NewUser{Email: "p15d-" + name + "@vardhan.test", Name: role, Password: "Correct-Horse-42", Roles: []string{role}})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", role, err)
	}
	p, _, err := env.Auth.LoadPrincipal(ctx, id)
	if err != nil {
		t.Fatalf("LoadPrincipal(%s): %v", role, err)
	}
	return p
}

// p15dDoc is the document page path of dt id.
func p15dDoc(dt string, id int64) string { return "/doc/" + dt + "/" + strconv.FormatInt(id, 10) }

// p15dOrderPath is the Create PO page of request id.
func p15dOrderPath(id int64) string {
	return "/modules/purchase/requests/" + strconv.FormatInt(id, 10) + "/order"
}

// p15dDisplay is DOC's display of a stored document.
func p15dDisplay(d doc.Doc) string {
	if d.Number != "" {
		return d.Number
	}
	return "Draft " + strconv.FormatInt(d.ID, 10)
}
