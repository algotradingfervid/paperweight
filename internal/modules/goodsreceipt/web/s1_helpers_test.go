package grnweb_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	grnweb "github.com/vardhan/internaltools/internal/modules/goodsreceipt/web"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// MOD-GRN S1 screen helpers: the generic document screens with grnweb's
// Module over grntest's world (real engine, real auth.Service, real
// goodsreceipt.Service; research §2 pattern 2), driven over HTTP as
// fixture users. The clock is authtest.T0, 24-10-2026 10:30 IST.

const (
	s1New   = "/doc/" + goodsreceipt.DocType + "/new"
	s1Today = "24-10-2026"

	s1MsgNotAvailable = "You can't use this Purchase Order. It isn't available to you."
	s1MsgChoosePO     = "Choose a purchase order."
)

// s1World is grntest's world behind docscreen's routes and grnweb's module
// routes, inside one scs session manager as AUTH's middleware runs them.
// Requests carry no session cookie, so each starts a fresh session, unless
// keep is set: then the session cookie of each answer goes with the next
// request (S3b's no-JS redraw).
type s1World struct {
	*grntest.Env
	t      *testing.T
	hidden map[string]bool                             // fields the Module's Hide also hides, as another Hide would (CD-319)
	hideIf func(c docscreen.Ctx, f doctype.Field) bool // another Hide asked with the Ctx, when set
	sm     *scs.SessionManager
	keep   bool // send the last answer's session cookie
	htmx   bool // send HX-Request
	cookie *http.Cookie
}

func newS1World(t *testing.T) *s1World {
	t.Helper()
	sm := scs.New()
	sm.IdleTimeout = 12 * time.Hour // production's setting (lessons §11)
	return &s1World{Env: grntest.New(t), t: t, sm: sm}
}

// s1Page is one answer, its body parsed (CD-148).
type s1Page struct {
	status   int
	location string
	body     string
	doc      *goquery.Document
}

// text is the page's text with runs of white space collapsed.
func (p s1Page) text() string { return strings.Join(strings.Fields(p.doc.Text()), " ") }

// do serves method path as p through docscreen.Routes with grnweb's Module.
func (w *s1World) do(p auth.Principal, method, path string, form url.Values) s1Page {
	w.t.Helper()
	d := docscreen.Deps{
		DB: w.DB, Engine: w.Engine, Auth: w.Auth, Approval: w.Approvals,
		Lister: docscreentest.NewLister(), Extras: docscreentest.NewExtras(),
		Principal: func(*http.Request) (auth.Principal, bool) { return p, true },
		Today:     func() (string, string) { return s1Today, "23-10-2026" },
		Modules:   grnweb.Modules(grnweb.Deps{Service: w.Service, Sessions: w.sm}),
	}
	if len(w.hidden) > 0 || w.hideIf != nil {
		m := d.Modules[goodsreceipt.DocType]
		own := m.Hide
		m.Hide = func(c docscreen.Ctx, f doctype.Field) bool {
			return w.hidden[f.Name] || (w.hideIf != nil && w.hideIf(c, f)) || own(c, f)
		}
		d.Modules[goodsreceipt.DocType] = m
	}
	r := chi.NewRouter()
	docscreen.Routes(d)(r)
	grnweb.Routes(grnweb.Deps{Service: w.Service, Sessions: w.sm})(r)
	var req *http.Request
	if form != nil {
		req = httptest.NewRequestWithContext(auth.WithPrincipal(w.t.Context(), p), method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequestWithContext(auth.WithPrincipal(w.t.Context(), p), method, path, nil)
	}
	if w.htmx {
		req.Header.Set("HX-Request", "true")
	}
	if w.keep && w.cookie != nil {
		req.AddCookie(w.cookie)
	}
	rec := httptest.NewRecorder()
	w.sm.LoadAndSave(r).ServeHTTP(rec, req)
	for _, line := range rec.Header().Values("Set-Cookie") {
		if c, err := http.ParseSetCookie(line); err == nil && c.Name == w.sm.Cookie.Name {
			w.cookie = c
		}
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rec.Body.String()))
	if err != nil {
		w.t.Fatalf("%s %s: parse: %v", method, path, err)
	}
	return s1Page{status: rec.Code, location: rec.Header().Get("Location"), body: rec.Body.String(), doc: doc}
}

func (w *s1World) get(p auth.Principal, path string) s1Page {
	w.t.Helper()
	return w.do(p, http.MethodGet, path, nil)
}

func (w *s1World) post(p auth.Principal, path string, form url.Values) s1Page {
	w.t.Helper()
	return w.do(p, http.MethodPost, path, form)
}

// id is the id of a master row by code.
func (w *s1World) id(table, code string) string {
	w.t.Helper()
	var id int64
	if err := w.DB.SQL().QueryRowContext(w.t.Context(), `SELECT id FROM `+table+` WHERE code = ?`, code).Scan(&id); err != nil { //nolint:gosec // G202: fixed table names
		w.t.Fatalf("id of %s %s: %v", table, code, err)
	}
	return strconv.FormatInt(id, 10)
}

// po is an approved PO by Priya on METRO-SITE (purchasetest's defaults)
// from vendor code, with one line per qty of MS-ANGLE-50.
func (w *s1World) po(vendor string, qtys ...string) doc.Doc {
	w.t.Helper()
	var lines []map[string]string
	for _, q := range qtys {
		lines = append(lines, map[string]string{"qty": q})
	}
	return purchasetest.NewOrder(w.t, w.Env.Env, purchasetest.StopAtApproved(),
		purchasetest.OrderHeader(map[string]string{"vendor_id": w.id("party", vendor)}), purchasetest.OrderLines(lines...))
}

// servicePO is an approved PO whose only line is SVC-CRANE, 8 hours.
func (w *s1World) servicePO() doc.Doc {
	w.t.Helper()
	return purchasetest.NewOrder(w.t, w.Env.Env, purchasetest.StopAtApproved(), purchasetest.OrderLines(map[string]string{
		"item_id": w.id("item", "SVC-CRANE"), "uom_id": w.id("uom", "HRS"), "qty": "8", "price": "1500", "hsn_sac": "997313",
	}))
}

// techPO is an approved PO on TECH-SITE / PRJ-TECHPARK raised by admin,
// which Suresh (PUNE-WH, METRO-SITE) can't read.
func (w *s1World) techPO() doc.Doc {
	w.t.Helper()
	return grntest.POAs(w.t, w.Env, w.Admin, doctype.StateApproved, map[string]string{
		"site_id": w.id("site", "TECH-SITE"), "project_id": w.id("project", "PRJ-TECHPARK"),
	})
}

// receiveAll approves a GRN by Suresh receiving every line of po in full.
func (w *s1World) receiveAll(po doc.Doc) doc.Doc {
	w.t.Helper()
	var lines []grntest.Line
	for _, l := range po.Lines {
		lines = append(lines, grntest.Line{POLineID: l.ID, Received: l.Values["qty"].(types.Qty).Plain()})
	}
	return w.GRN(w.t, w.Suresh, po.ID, lines, doctype.StateApproved)
}

// draft creates a GRN draft as Suresh through step 1's POST from po and
// returns its id.
func (w *s1World) draft(po doc.Doc) int64 {
	w.t.Helper()
	return w.created(w.post(w.Suresh, s1New, url.Values{"purchase_order_id": {strconv.FormatInt(po.ID, 10)}, "receipt_date": {s1Today}}))
}

// created is the id of the draft step 1's POST r created.
func (w *s1World) created(r s1Page) int64 {
	w.t.Helper()
	rest, ok := strings.CutPrefix(r.location, "/doc/"+goodsreceipt.DocType+"/")
	if r.status != http.StatusSeeOther || !ok {
		w.t.Fatalf("POST %s = %d %q, want 303 to the new draft (%s)", s1New, r.status, r.location, r.text())
	}
	id, err := strconv.ParseInt(strings.Split(rest, "/")[0], 10, 64)
	if err != nil {
		w.t.Fatal(err)
	}
	return id
}

// stepPath is step slug of GRN id.
func stepPath(id int64, slug string) string {
	return "/doc/" + goodsreceipt.DocType + "/" + strconv.FormatInt(id, 10) + "/edit/" + slug
}

// radios are the purchase_order_id radios: value, label text, checked.
type s1Radio struct {
	value, text string
	checked     bool
}

func (p s1Page) radios(name string) []s1Radio {
	var out []s1Radio
	p.doc.Find(`input[type="radio"][name="` + name + `"]`).Each(func(_ int, s *goquery.Selection) {
		_, checked := s.Attr("checked")
		out = append(out, s1Radio{value: s.AttrOr("value", ""), text: strings.Join(strings.Fields(s.Parent().Text()), " "), checked: checked})
	})
	return out
}

// errorSummary is the page's error summary messages, joined by " | ".
func (p s1Page) errorSummary() string {
	var out []string
	p.doc.Find(".error-summary li").Each(func(_ int, s *goquery.Selection) { out = append(out, s.Text()) })
	return strings.Join(out, " | ")
}

// summaryRows are the page's summary list rows as "label: value".
func (p s1Page) summaryRows() []string {
	var out []string
	p.doc.Find(".summary-row").Each(func(_ int, s *goquery.Selection) {
		out = append(out, s.Find("dt").Text()+": "+s.Find("dd").Text())
	})
	return out
}
