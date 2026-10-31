package app_test

// TOOLING T2a: the redraw sweep. The most common screen review finding was "a
// rule applied on most redraw paths but missed on one" (newPost's 422/409
// skipped the hidden-field filter in M0-1b). This sweep drives every
// docscreen path that draws a document form, or a part of one, for every
// registered document DocType, through the real handlers on a real App
// (httptest, no browser), with a probe Module.Hide that hides fields on every
// DocType. A new module screen is covered with no test written.
//
// Why internal/app: only here are the real module DocTypes (purchase.Order,
// purchase.Request, expense.Claim, expense.Advance) registered with their
// migrations and services, next to the fixture DocTypes (fixture.Order,
// fixture.Receipt, fixture.Claim) and apptest.GRN. docscreentest.Hooked is
// not swept: registering it here would add a DocType without tables to every
// other app test's search and nav. Its one feature, a module with hooks, is
// what the probe Module gives every DocType.
//
// Every cell runs twice (rsDoc.both): first with the probe hiding nothing,
// where the page must draw at least one probe field (or the cell says it
// draws none, and that is checked), then with the probe hiding, where it must
// draw none. So a page that never drew the field can't pass for one that
// hides it.
//
// The guard (TestToolingRedraw_TableMatchesCode) holds docscreen to three
// lists here: every route Routes registers (rsRoutes, each named to its cells
// below), every function that sends a page (rsSites), and every caller of the
// functions that build one (rsBuilders). A new route or redraw path fails it
// until it has a cell.
//
// | Path                                   | Site                            | Cell                 |
// |----------------------------------------|---------------------------------|----------------------|
// | GET new                                | newGet                          | new GET              |
// | POST new success, htmx                 | newPost -> advance              | new POST ok          |
// | POST new 422, module Save errors       | newPost invalid                 | new 422 module       |
// | POST new 422, parse error              | newPost invalid                 | new 422 parse        |
// | POST new 409                           | newPost invalid                 | new 409 (skip+check) |
// | GET step                               | stepGet -> arrival              | step GET <slug>      |
// | POST step success, htmx                | stepPost -> advance             | step POST ok <slug>  |
// | POST step nav=stay                     | stepPost -> stay -> advance     | step stay <slug>     |
// | POST step nav=finish                   | stepPost Finish (redirect)      | step finish          |
// | POST step 422, module Save errors      | stepPost rerender               | step 422 module ...  |
// | POST step 422, parse error             | stepPost rerender               | step 422 parse ...   |
// | POST step 409                          | stepPost rerender               | step 409 <slug>      |
// | POST submit 422, writer                | failingStep                     | submit 422           |
// | POST submit 422, submitter not writer  | failingStep -> refused          | submit 422 reader    |
// | POST submit 409 (refusal in page)      | refused                         | submit 409           |
// | POST approve, htmx                     | succeeded (doc page)            | approve ok           |
// | GET reject page, POST reject 422       | reasonGet, reasonPost           | reject GET, reject 422 |
// | POST withdraw, htmx                    | succeeded -> checkPage          | withdraw             |
// | POST revise, htmx                      | succeeded -> checkPage          | revise               |
// | POST amend, htmx, writer               | succeeded -> checkPage          | amend                |
// | POST amend, htmx, amender not writer   | checkPage -> readOnlyPage       | amend reader         |
// | POST cancel success, htmx              | succeeded (doc page)            | cancel ok            |
// | POST delete success, htmx              | succeeded -> renderList         | not a form (list)    |
// | GET document page, reader of a draft   | page                            | page reader          |
// | GET step or line page, reader          | readOnly redirect               | page reader          |
// | GET document page, approved            | page                            | page approved        |
// | GET line new / line no                 | lineGet generic                 | line GET             |
// | GET line no, module Line               | lineGet -> lineBody             | line GET module      |
// | POST line 422, parse error             | linePost generic refusal        | line 422 parse       |
// | POST line 409                          | linePost generic refusal        | line 409             |
// | POST line 422, module Line.Save errors | linePost -> lineBody refusal    | line 422 module      |
// | POST grid row success, htmx fragment   | gridSaved                       | grid ok              |
// | POST grid row 422/409, htmx fragment   | gridRefused                     | grid 422, grid 409   |
// | GET remove line                        | removeGet -> removePage         | remove GET           |
// | POST remove line 409 / 422             | itemsRefused remove page        | remove 409, lines 422 (skip+check) |
// | POST move line 409 / 422               | itemsRefused items step         | move 409, lines 422 (skip+check) |
// | the items step's line cards            | stepViewOf -> itemsBody         | card                 |
// | GET reason/confirm page                | reasonGet                       | reason GET           |
// | POST reason 422/409                    | reasonPost                      | reason 422, 409      |
// | list, search, error page               | list, renderList, problem, search | not a form         |
// | refusal with no document view          | refused problem page            | not a form           |
//
// Each cell asserts, where its path draws them:
//   (a) no field the probe hides appears: no input, id, value, label, error
//       link or Link display naming it; and no input posts a ReadOnly field;
//   (b) a 422/409 redraw shows the same inputs as a fresh GET of that step or
//       line, and exactly the errors the rule gives (CD-256 (1)): the step's
//       own and unowned ones, an error on a hidden field as a plain message;
//   (c) the version the page keeps: the posted one wherever typed values are
//       kept (stepPost, linePost, the remove and reason pages; CD-94); the
//       grid refusal sends no #doc-version, so the page keeps its own; the
//       move refusal redraws the stored lines, so the stored version;
//   (d) derived values (the read-only summary rows) after a 422/409 are those
//       of a fresh GET: docscreen computes none, it shows the record as stored
//       (CD-302 makes the stored values the module's job).

import (
	"cmp"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// rsRoutes is every route docscreen.Routes registers, with the cells that
// sweep it ("not a form" for the pages that draw no document field).
var rsRoutes = map[string]string{
	"GET /search":                                "not a form",
	"GET /doc/{doctype}":                         "not a form",
	"GET /doc/{doctype}/new":                     "new GET",
	"POST /doc/{doctype}/new":                    "new POST ok, new 422 module, new 422 parse, new 409",
	"GET /doc/{doctype}/{id}":                    "page reader, page approved",
	"GET /doc/{doctype}/{id}/edit/{step}":        "step GET, page reader",
	"POST /doc/{doctype}/{id}/edit/{step}":       "step POST ok, step stay, step finish, step 422 module, step 422 parse, step 409",
	"GET /doc/{doctype}/{id}/lines/new":          "line GET",
	"POST /doc/{doctype}/{id}/lines/new":         "line 422 module",
	"GET /doc/{doctype}/{id}/lines/{no}":         "line GET, line GET module, page reader",
	"POST /doc/{doctype}/{id}/lines/{no}":        "line 422 parse, line 409, line 422 module, grid ok, grid 422, grid 409",
	"GET /doc/{doctype}/{id}/lines/{no}/remove":  "remove GET",
	"POST /doc/{doctype}/{id}/lines/{no}/remove": "remove 409, lines 422 <route>",
	"POST /doc/{doctype}/{id}/lines/{no}/move":   "move 409, lines 422 <route>",
	"GET /doc/{doctype}/{id}/{action}":           "reason GET, reject GET",
	"POST /doc/{doctype}/{id}/{action}":          "submit 422, submit 422 reader, submit 409, approve ok, reject 422, withdraw, revise, amend, amend reader, cancel ok, reason 422, reason 409",
}

// rsSites is every docscreen function that sends a page or fragment, with
// its count of ui.Render, wizard.Respond and renderList calls.
var rsSites = map[string]int{
	"newGet": 1, "newPost": 1, "stepGet": 1, "stepPost": 2, "advance": 2,
	"failingStep": 1, "checkPage": 1, "readOnlyPage": 1,
	"page": 1, "refused": 2, "succeeded": 2,
	"lineGet": 2, "linePost": 2, "gridSaved": 1, "gridRefused": 1,
	"removeGet": 1, "itemsRefused": 2,
	"reasonGet": 1, "reasonPost": 1,
	"list": 1, "renderList": 1, "problem": 1, "search": 1,
}

// rsBuilders is every caller of the functions that build a document page or a
// part of one, with its count of calls. A new caller is a new redraw path.
var rsBuilders = map[string]map[string]int{
	"stepViewOf":     {"newGet": 1, "newPost": 1, "stepPost": 1, "failingStep": 1, "checkPage": 1, "arrival": 1, "itemsRefused": 1},
	"arrival":        {"stepGet": 1, "advance": 1},
	"linePage":       {"lineGet": 1, "linePost": 1},
	"lineShell":      {"lineBody": 1, "linePage": 1},
	"lineBody":       {"lineGet": 1, "linePost": 1},
	"removePage":     {"removeGet": 1, "itemsRefused": 1},
	"pageViewOf":     {"page": 1, "refused": 1, "succeeded": 1, "readOnlyPage": 1},
	"reasonViewWith": {"reasonGet": 1, "reasonPost": 1},
	"gridResponse":   {"gridSaved": 1, "gridRefused": 1},
	"gridRowOf":      {"itemsBody": 1, "gridSaved": 1, "gridRefused": 1, "gridShown": 1}, // gridShown only counts the row's cells (CD-315)
	"refused":        {"failingStep": 1, "actionPost": 1},
	"itemsRefused":   {"reorder": 1},
	"checkPage":      {"succeeded": 2},
	"readOnlyPage":   {"checkPage": 1},
	"failingStep":    {"actionPost": 1},
	"advance":        {"newPost": 1, "stepPost": 1, "stay": 1},
	"stay":           {"newPost": 1, "stepPost": 1},
}

// The sweep's users besides the admin, who holds every right.
const (
	rsAdmin     = "admin"
	rsReader    = "rs-reader"    // read only
	rsSubmitter = "rs-submitter" // read and submit, no write
	rsAmender   = "rs-amender"   // read and amend, no write
	rsApprover  = "rs-approver"  // read and approve; every approvable DocType's rule names its role
)

// Messages the probe Module's Save hooks return.
const (
	rsMsgVisible = "Probe error on a shown field."
	rsMsgHidden  = "Probe error on a hidden field."
	rsMsgOther   = "Probe error on another step."
	rsMsgPlain   = "Probe error with no field."
	rsTyped      = "ZqTyped"
	rsShow       = "+show" // a handler mode's control twin: the probe hides nothing
	rsBadLink    = "987654321"
)

// cell runs one cell. The CD-319 BUG skips the sweep found went when M0-46
// fixed them (bd30a1cd): these cells are the independent check of the fix.
func (d *rsDoc) cell(t *testing.T, name string, fn func(t *testing.T)) {
	t.Helper()
	t.Run(name, fn)
}

// both runs a request in mode's control twin, where the probe hides nothing
// and the page must draw a probe field (none says it draws none, which is
// checked), then in mode, where it must draw none; it returns the second.
func (d *rsDoc) both(t *testing.T, where, mode string, none bool, run func(t *testing.T, mode string) rsResp) rsResp {
	t.Helper()
	ctl := run(t, mode+rsShow)
	drawn := d.marks(ctl)
	switch {
	case none && len(drawn) > 0:
		t.Errorf("%s: the page now draws the probe field %s (%s): drop this cell's none, so its hiding is checked", where, drawn[0].field, drawn[0].kind)
	case !none && len(drawn) == 0:
		t.Errorf("%s: with nothing hidden the page draws no probe field, so hiding checks nothing", where)
	}
	r := run(t, mode)
	d.noHidden(t, where, r)
	return r
}

type rsEnv struct {
	t     *testing.T
	w     *world
	m     fixture.Masters
	who   map[string]auth.Principal
	h     map[string]http.Handler // by mode; see newRSEnv
	doc   map[string]*rsDoc
	count atomic.Int64 // unique invoice numbers

	orderOnce sync.Once
	orderLine string // an approved fixture.Order's line, which every receipt receives

	poOnce              sync.Once
	po, poLine, poLine2 string // an approved purchase.Order and its two lines, which every goods receipt receives
}

// rsCardBug: docscreen hands Module.LineCard the LineView.Card lineView built
// with every field shown; CD-319 (3) filtered only the default card. The
// purchase stubs (orderLineCard, requestLineCard) return l.Card, so their
// cards print a hidden line Link's name. marks exempts text found only inside
// the cards of a module with its own LineCard (or the remove page's question,
// which names the card's item), and the "module line card" cell fails once
// no such text turns up.
const rsCardBug = "BUG: a module LineCard gets the unfiltered l.Card, so returning it prints hidden line fields (reported to head)"

// rsDoc is one DocType's probe: the fields it hides and their seeded values.
type rsDoc struct {
	e         *rsEnv
	dt        *doctype.DocType
	steps     []docscreen.FormStep
	hide      map[string]bool
	sentinel  map[string]string // a hidden Text field's stored value
	shown     map[string]string // a hidden Link's master name, as cards and pages show it
	valid     func(t *testing.T) (values, line map[string]string)
	mod       docscreen.Module // the app's own Module for dt
	itemsNone bool             // the probe draft's items step draws no probe field; see draft
	rows      atomic.Bool      // a 422/409 redraw compared non-empty read-only rows
	cardBug   atomic.Bool      // a module LineCard printed a hidden field; see rsCardBug
}

// newRSEnv is one App with the canonical seed, the sweep's users, an
// approval rule on every approvable DocType, and six handlers: "plain" (the
// probe's Hide over the app's Modules), "inject" (and step Save hooks that
// refuse with probe errors), "line" (and a Line whose Save refuses), each with
// its control twin mode+rsShow, where the probe hides nothing.
func newRSEnv(t *testing.T) *rsEnv {
	t.Helper()
	w := newWorld(t)
	// The admin and the masters only: the canonical users' password hashes
	// cost seconds per App under -race.
	admin := w.firstAdmin()
	testkit.SeedMasters(t, w.app.DB())
	m := fixture.SeedMasters(t, w.app.DB())
	// An expense's maker is an employee: the admin is EMP-007, the expense seeders' employee.
	if _, err := w.app.DB().SQL().ExecContext(t.Context(), "UPDATE employee SET user_id = ? WHERE code = 'EMP-007'", int64(admin.ID)); err != nil {
		t.Fatalf("link the admin to EMP-007: %v", err)
	}
	e := &rsEnv{t: t, w: w, m: m, who: map[string]auth.Principal{rsAdmin: admin}, h: map[string]http.Handler{}, doc: map[string]*rsDoc{}}
	e.users(admin)
	ctx := t.Context()
	for _, dt := range rsDocTypes() {
		if dt.Approvable {
			// APR asks AUTH about the role as the signed-in user (app.go roleDirectory).
			in := approval.RuleInput{DocType: dt.Name, ApproverType: approval.ApproverRole, Role: rsRole(rsApprover)}
			if _, err := w.app.Approvals().CreateRule(auth.WithPrincipal(ctx, admin), admin, in); err != nil {
				t.Fatalf("approval rule on %s: %v", dt.Name, err)
			}
		}
	}
	// fixture.Order's PO/, fixture.Claim's EX/ and fixture.Receipt's GR/ series
	// share purchase.Order's, expense.Claim's and goodsreceipt.Receipt's
	// prefixes, and a number is unique across DocTypes (CD-295).
	namer := naming.New(w.app.DB(), testkit.Clock(), w.app.Auth(), audit.Default)
	for name, prefix := range map[string]string{fixture.Order: "PO/26-27/", fixture.Claim: "EX/26-27/", fixture.Receipt: "GR/26-27/"} {
		if err := namer.SetCounter(ctx, admin, name, prefix, 50000); err != nil {
			t.Fatalf("move %s's counter: %v", name, err)
		}
	}
	for _, dt := range rsDocTypes() {
		d := &rsDoc{e: e, dt: dt, hide: map[string]bool{}, sentinel: map[string]string{}, shown: map[string]string{}}
		steps, err := docscreen.FormSteps(dt)
		if err != nil {
			t.Fatalf("FormSteps(%s): %v", dt.Name, err)
		}
		d.steps = steps
		d.valid = e.seeder(dt)
		d.pick()
		e.doc[dt.Name] = d
	}
	base := w.app.ToolingRedrawDeps().Modules
	for _, probe := range []bool{true, false} {
		plain, inject, line := map[string]docscreen.Module{}, map[string]docscreen.Module{}, map[string]docscreen.Module{}
		for name, d := range e.doc {
			m := base[name]
			d.mod = m
			own := m.Hide
			m.Hide = func(c docscreen.Ctx, f doctype.Field) bool {
				return (probe && d.hide[f.Name]) || (own != nil && own(c, f))
			}
			plain[name] = m
			inject[name] = d.injecting(m)
			line[name] = d.lining(m)
		}
		suffix := ""
		if !probe {
			suffix = rsShow
		}
		e.h["plain"+suffix], e.h["inject"+suffix], e.h["line"+suffix] = e.mount(plain), e.mount(inject), e.mount(line)
	}
	return e
}

// rsDocTypes are the registered document DocTypes, by name.
func rsDocTypes() []*doctype.DocType {
	var out []*doctype.DocType
	for _, dt := range doctype.All() {
		if dt.IsDocument() {
			out = append(out, dt)
		}
	}
	slices.SortFunc(out, func(a, b *doctype.DocType) int { return cmp.Compare(a.Name, b.Name) })
	return out
}

func rsRole(user string) string { return "RS " + user }

// users creates the reader, submitter, amender and approver, each in a role
// holding only its rights, on every document DocType where AUTH applies them.
func (e *rsEnv) users(admin auth.Principal) {
	e.t.Helper()
	ctx := e.t.Context()
	a := e.w.app.Auth()
	for user, rights := range map[string][]auth.Right{
		rsReader:    {auth.RightRead},
		rsSubmitter: {auth.RightRead, auth.RightSubmit},
		rsAmender:   {auth.RightRead, auth.RightAmend},
		rsApprover:  {auth.RightRead, auth.RightApprove},
	} {
		if err := a.CreateRole(ctx, admin, rsRole(user)); err != nil {
			e.t.Fatalf("CreateRole(%s): %v", user, err)
		}
		for _, dt := range rsDocTypes() {
			m := map[auth.Right]auth.Scope{}
			for _, r := range rights {
				if auth.Applies(dt, r) {
					m[r] = auth.ScopeAll
				}
			}
			if err := a.SetRights(ctx, admin, rsRole(user), dt.Name, m); err != nil {
				e.t.Fatalf("SetRights(%s, %s): %v", user, dt.Name, err)
			}
		}
		id, _, err := a.CreateUser(ctx, admin, auth.NewUser{Email: user + "@vardhan.test", Name: user, Password: testPassword, Roles: []string{rsRole(user)}})
		if err != nil {
			e.t.Fatalf("CreateUser(%s): %v", user, err)
		}
		e.who[user] = e.w.principal(id)
	}
}

func (e *rsEnv) mount(mods map[string]docscreen.Module) http.Handler {
	d := e.w.app.ToolingRedrawDeps()
	d.Modules = mods
	d.Principal = func(r *http.Request) (auth.Principal, bool) {
		p, ok := e.who[r.Header.Get("X-Rs-User")]
		return p, ok
	}
	r := web.NewRouter(web.Options{Logger: slog.New(slog.NewTextHandler(e.w.logs, nil)), Sessions: scs.New()})
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(ui.WithShell(r.Context(), ui.Shell{CSRF: "rs-csrf"})))
		})
	})
	docscreen.Routes(d)(r)
	return r
}

// idOf is a real master's id by code.
func (e *rsEnv) idOf(t *testing.T, table, code string) string {
	t.Helper()
	var id int64
	if err := e.w.app.DB().SQL().QueryRowContext(t.Context(), "SELECT id FROM "+table+" WHERE code = ?", code).Scan(&id); err != nil {
		t.Fatalf("%s %s: %v", table, code, err)
	}
	return strconv.FormatInt(id, 10)
}

// seeder is the header and line values of a valid document of dt, in plain
// form: the module DocTypes' as the visibility sweep seeds them
// (tooling_visibility_helpers_test.go), the fixture ones' from 00-fixtures,
// and for other TestOnly DocTypes (CD-291) every Required field and every
// date. A release DocType without a seeder fails the sweep.
func (e *rsEnv) seeder(dt *doctype.DocType) func(t *testing.T) (values, line map[string]string) {
	const day = "2026-10-24"
	switch dt.Name {
	case purchase.RequestDocType:
		return func(t *testing.T) (map[string]string, map[string]string) {
			return map[string]string{"site_id": e.idOf(t, "site", "METRO-SITE"), "project_id": e.idOf(t, "project", "PRJ-METRO"), "request_date": day},
				map[string]string{"item_id": e.idOf(t, "item", "MS-ANGLE-50"), "uom_id": e.idOf(t, "uom", "KGS"), "qty": "1"}
		}
	case purchase.DocType:
		return func(t *testing.T) (map[string]string, map[string]string) {
			return map[string]string{
				"vendor_id": e.idOf(t, "party", "SHREE"), "site_id": e.idOf(t, "site", "METRO-SITE"), "project_id": e.idOf(t, "project", "PRJ-METRO"), "order_date": day,
			}, map[string]string{
				"item_id": e.idOf(t, "item", "MS-ANGLE-50"), "uom_id": e.idOf(t, "uom", "KGS"), "qty": "1", "price": "10.00",
				"tax_code_id": e.idOf(t, "tax_code", "GST18"), "hsn_sac": "7216",
			}
		}
	case expense.AdvanceDocType:
		return func(t *testing.T) (map[string]string, map[string]string) {
			return map[string]string{"employee_id": e.idOf(t, "employee", "EMP-007"), "advance_date": day, "purpose": "x"},
				map[string]string{"expense_type_id": e.idOf(t, "expense_type", "TRAVEL"), "line_amount": "100.00"}
		}
	case expense.ClaimDocType:
		return func(t *testing.T) (map[string]string, map[string]string) {
			return map[string]string{"employee_id": e.idOf(t, "employee", "EMP-007"), "claim_date": day, "purpose": "x"},
				map[string]string{
					"expense_date": day, "expense_type_id": e.idOf(t, "expense_type", "TRAVEL"), "amount": "100.00", "no_receipt_reason": "x",
					"supplier_gstin": "27AABCS1429B1ZU", "invoice_no": "INV-RS-" + strconv.FormatInt(e.count.Add(1), 10),
				}
		}
	case goodsreceipt.DocType:
		return func(t *testing.T) (map[string]string, map[string]string) {
			e.poOnce.Do(func() {
				// Two lines with room for every receipt the sweep approves: a
				// receipt names each PO line once.
				values, line := e.doc[purchase.DocType].valid(t)
				line["qty"] = "1000"
				admin := e.who[rsAdmin]
				res, err := e.w.app.Engine().Do(t.Context(), admin, purchase.DocType, 0, doc.ActionSave, doc.ActionInput{Values: values, Lines: []map[string]string{line, maps.Clone(line)}})
				if err == nil {
					res, err = e.w.app.Engine().Do(t.Context(), admin, purchase.DocType, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
				}
				if err == nil {
					res, err = e.w.app.Engine().Do(t.Context(), e.who[rsApprover], purchase.DocType, res.ID, doc.ActionApprove, doc.ActionInput{Version: res.Version})
				}
				if err != nil || res.State != doctype.StateApproved {
					t.Fatalf("an approved purchase.Order for the receipts: %v (%s)", err, res.State)
				}
				lines := e.load(t, purchase.DocType, res.ID).Lines
				e.po, e.poLine, e.poLine2 = strconv.FormatInt(res.ID, 10), lines[0]["id"], lines[1]["id"]
			})
			if e.poLine == "" {
				t.Fatal("no approved purchase.Order line for the receipts")
			}
			return map[string]string{"purchase_order_id": e.po, "receipt_date": day, "challan_kind": "Delivery challan", "challan_date": day},
				map[string]string{"po_line_id": e.poLine, "received_qty": "1"}
		}
	case fixture.Order:
		return func(*testing.T) (map[string]string, map[string]string) {
			return map[string]string{
				"supplier_id": idText(e.m.Party["SHREE"]), "project_id": idText(e.m.Project["PRJ-METRO"]), "order_date": day, "amount": "10.00",
			}, map[string]string{"item_id": idText(e.m.Item["MS-ANGLE-50"]), "qty": "100", "rate": "10.0000", "uom": "kg"}
		}
	case fixture.Receipt:
		return func(t *testing.T) (map[string]string, map[string]string) {
			e.orderOnce.Do(func() {
				id, err := e.doc[fixture.Order].approved(t)
				if err != nil {
					t.Fatalf("an approved fixture.Order for the receipts: %v", err)
				}
				e.orderLine = e.load(t, fixture.Order, id).Lines[0]["id"]
			})
			if e.orderLine == "" {
				t.Fatal("no approved fixture.Order line for the receipts")
			}
			return map[string]string{"receipt_date": day}, map[string]string{"order_line_id": e.orderLine, "qty": "1"}
		}
	}
	if !dt.TestOnly {
		// A release DocType needs real values, as the visibility sweep's
		// sweepSeeders require: generic ones would skip its approved cells forever.
		return func(t *testing.T) (map[string]string, map[string]string) {
			t.Fatalf("document DocType %s has no seeder in rsEnv.seeder: add one, so the redraw sweep covers its approved states", dt.Name)
			return nil, nil
		}
	}
	return func(t *testing.T) (map[string]string, map[string]string) {
		values, line := map[string]string{}, map[string]string{}
		for _, f := range dt.Fields {
			// Lines also get every Link and quantity, so a line is never blank.
			fill := f.Required || f.Type == doctype.Date || (f.OnLine && (f.Type == doctype.Link || f.Type == doctype.Qty))
			if f.ReadOnly || f.RollUp || !fill {
				continue
			}
			if f.OnLine {
				line[f.Name] = e.validValue(t, f)
			} else {
				values[f.Name] = e.validValue(t, f)
			}
		}
		return values, line
	}
}

// pick chooses what the probe hides: on each header step with two or more
// editable fields, a Text field (seeded with a sentinel; an optional one
// when there is one) and a Required
// field, keeping one editable field shown; on the lines, a Text field (with a
// sentinel) and the first Link (the card's item), or else the first field,
// keeping one shown; and one ReadOnly header field with a label no other
// field has.
func (d *rsDoc) pick() {
	for _, s := range d.steps {
		if s.Lines || s.Slug == "check" {
			continue
		}
		var editable []doctype.Field
		for _, name := range s.Fields {
			if f, ok := d.dt.Field(name); ok && !f.ReadOnly {
				editable = append(editable, f)
			}
		}
		left := len(editable)
		i := slices.IndexFunc(editable, func(f doctype.Field) bool { return f.Type == doctype.Text && !f.Required })
		if i < 0 {
			i = slices.IndexFunc(editable, func(f doctype.Field) bool { return f.Type == doctype.Text })
		}
		if i >= 0 && left > 1 {
			d.hide[editable[i].Name] = true
			d.sentinel[editable[i].Name] = "ZqH" + strconv.Itoa(len(d.sentinel))
			left--
		}
		// Not a link to a document (a goods receipt's purchase order): its
		// source, which a create needs, so a draft without it can't be made.
		if i := slices.IndexFunc(editable, func(f doctype.Field) bool { return f.Required && !d.hide[f.Name] && !rsDocLink(f) }); i >= 0 && left > 1 {
			d.hide[editable[i].Name] = true
		}
	}
	var lineFields []doctype.Field
	for _, f := range d.dt.Fields {
		if f.OnLine && !f.ReadOnly && !f.RollUp {
			lineFields = append(lineFields, f)
		}
	}
	left := len(lineFields)
	if i := slices.IndexFunc(lineFields, func(f doctype.Field) bool { return f.Type == doctype.Text }); i >= 0 && left > 1 {
		d.hide[lineFields[i].Name] = true
		d.sentinel[lineFields[i].Name] = "ZqL"
		left--
	}
	if i := slices.IndexFunc(lineFields, func(f doctype.Field) bool { return f.Type == doctype.Link }); i >= 0 && left > 1 {
		d.hide[lineFields[i].Name] = true
		left--
	}
	if left == len(lineFields) && left > 1 {
		d.hide[lineFields[0].Name] = true // no Text or Link on the lines
	}
	for _, f := range d.dt.Fields {
		if f.ReadOnly && !f.OnLine && f.Name != "number" && f.Name != "state" && d.uniqueLabel(f) {
			d.hide[f.Name] = true
			break
		}
	}
}

// rsDocLink reports whether f links to a document DocType.
func rsDocLink(f doctype.Field) bool {
	target, ok := doctype.Lookup(f.Link)
	return f.Type == doctype.Link && ok && target.IsDocument()
}

// uniqueLabel reports whether no other field or step of dt has f's label,
// so a page showing it names f (see rsReasonLabels for reason pages).
func (d *rsDoc) uniqueLabel(f doctype.Field) bool {
	n := 0
	for _, g := range d.dt.Fields {
		if strings.EqualFold(g.Label, f.Label) {
			n++
		}
	}
	for _, s := range d.steps {
		if strings.EqualFold(s.Label, f.Label) {
			n++
		}
	}
	return n == 1
}

// rsReasonLabels are the labels docscreen's reason pages give their own
// reason field (reasons.go): on a reason page, a field with one of them isn't
// named by it.
var rsReasonLabels = []string{"Reason for rejecting", "Reason for cancelling", "Reason for closing"}

// names reads the master names a page shows for the hidden Links of a valid
// document: on a line card, the item's; in a header, the party's.
func (d *rsDoc) names(t *testing.T) {
	t.Helper()
	values, line := d.valid(t)
	err := d.e.w.app.DB().Read(t.Context(), func(tx *db.Tx) error {
		for name := range d.hide {
			f, _ := d.dt.Field(name)
			v := values[name]
			if f.OnLine {
				v = line[name]
			}
			target, ok := doctype.Lookup(f.Link)
			if f.Type != doctype.Link || v == "" || !ok || target.Master == nil {
				continue
			}
			id, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return err
			}
			mi, found, err := target.Master.Lookup(t.Context(), tx, id)
			if err != nil || !found {
				return fmt.Errorf("%s %s: found %v, %w", f.Link, v, found, err)
			}
			if mi.Name != "" {
				d.shown[name] = mi.Name
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the hidden Links' names: %v", err)
	}
}

// injecting is m with Save hooks on every wizard step but check that refuse
// with the probe errors (hooks spec §4.1): the 422 paths for errors on hidden
// fields and on other steps, which no generic post gives. The app wires the
// real modules' Modules since 981bbcb4: inject mode replaces their step Save
// hooks, and with them what their Validate would write (CD-302), and line
// mode replaces their Line's Save; plain mode (and its control twin) is what
// exercises the real modules' hooks as the app runs them.
func (d *rsDoc) injecting(m docscreen.Module) docscreen.Module {
	m.Steps = maps.Clone(m.Steps)
	if m.Steps == nil {
		m.Steps = map[string]docscreen.Step{}
	}
	for i, s := range d.steps[:len(d.steps)-1] {
		st := m.Steps[s.Slug]
		st.Save = func(docscreen.Ctx) (docscreen.Save, error) {
			return docscreen.Save{Values: map[string]string{}, Errors: d.probeErrors(i)}, nil
		}
		m.Steps[s.Slug] = st
	}
	return m
}

// lining is m with a Line whose Save refuses with the probe's line errors
// (hooks spec §4.2), around the module's own Line.Body and Top when it has a
// Line. It is apart from injecting because a Line turns the grid off (CD-256
// (6)), and the items step's errors link to the grid.
func (d *rsDoc) lining(m docscreen.Module) docscreen.Module {
	if d.dt.LinesTable == "" {
		return m
	}
	line := docscreen.Line{Body: func(docscreen.Ctx, int, docscreen.StepState) (templ.Component, error) { return templ.NopComponent, nil }}
	if m.Line != nil {
		line = *m.Line
	}
	line.Save = func(docscreen.Ctx, int) (docscreen.Save, error) {
		var errs []doctype.FieldError
		if f, ok := d.shownLine(); ok {
			errs = append(errs, doctype.FieldError{Field: f.Name, Message: rsMsgVisible})
		}
		if f, ok := d.hiddenLine(); ok {
			errs = append(errs, doctype.FieldError{Field: f.Name, Message: rsMsgHidden})
		}
		return docscreen.Save{Values: map[string]string{}, Errors: append(errs, doctype.FieldError{Message: rsMsgPlain})}, nil
	}
	m.Line = &line
	return m
}

// probeErrors are the errors the probe's Save returns on step i: one on a
// shown field of the step, one on a hidden one, one on another step's
// field, and one with no field. On the lines step the first two are line 1's.
func (d *rsDoc) probeErrors(i int) []doctype.FieldError {
	s := d.steps[i]
	var errs []doctype.FieldError
	if s.Lines {
		if f, ok := d.shownLine(); ok {
			errs = append(errs, doctype.FieldError{Field: f.Name, Line: 1, Message: rsMsgVisible})
		}
		if f, ok := d.hiddenLine(); ok {
			errs = append(errs, doctype.FieldError{Field: f.Name, Line: 1, Message: rsMsgHidden})
		}
	} else {
		if f, ok := d.shownField(i); ok {
			errs = append(errs, doctype.FieldError{Field: f.Name, Message: rsMsgVisible})
		}
		if f, ok := d.hiddenField(i); ok {
			errs = append(errs, doctype.FieldError{Field: f.Name, Message: rsMsgHidden})
		}
	}
	if f, ok := d.otherField(i); ok {
		errs = append(errs, f)
	}
	return append(errs, doctype.FieldError{Message: rsMsgPlain})
}

// wantProbe is the summary step i shows for probeErrors, with the probe
// hiding (hide) or not: CD-256 (1) and stepErrors' rule, written from the
// spec, not the code.
func (d *rsDoc) wantProbe(i, lines int, hide bool) []string {
	hidden := "#step-title|" + rsMsgHidden
	var out []string
	if d.steps[i].Lines {
		if _, ok := d.shownLine(); ok {
			out = append(out, d.linesTarget(lines, hide)+"|"+rsMsgVisible)
		}
		if _, ok := d.hiddenLine(); ok {
			if !hide {
				hidden = d.linesTarget(lines, hide) + "|" + rsMsgHidden
			}
			out = append(out, hidden)
		}
	} else {
		if f, ok := d.shownField(i); ok {
			out = append(out, "#"+components.FieldID(f.Name)+"|"+rsMsgVisible)
		}
		if f, ok := d.hiddenField(i); ok {
			if !hide {
				hidden = "#" + components.FieldID(f.Name) + "|" + rsMsgHidden
			}
			out = append(out, hidden)
		}
	}
	return append(out, "#step-title|"+rsMsgPlain)
}

// grid reports whether the items step has docscreen's editable grid with
// the probe hiding (hide) or not: not when the module turns it off
// (CardsOnly, Line, NoLinePages; CD-256 (6)), and only for rows of 3 or 5
// editable cells, the fields not hidden (CD-315, docscreen.gridShown). The
// module's own Hide isn't counted: a wrong guess fails the grid cells' 303
// check or their 200, never passes silently.
func (d *rsDoc) grid(hide bool) bool {
	if d.mod.CardsOnly || d.mod.Line != nil || d.mod.NoLinePages {
		return false
	}
	n := 0
	for _, f := range d.dt.Fields {
		if f.OnLine && !f.ReadOnly && (!hide || !d.hide[f.Name]) {
			n++
		}
	}
	return n == 3 || n == 5
}

// linesTarget is where a line error links on the generic items step, with
// the probe hiding or not: the line editor when the grid shows lines, else
// the step heading.
func (d *rsDoc) linesTarget(lines int, hide bool) string {
	if d.grid(hide) && lines > 0 {
		return "#line-editor"
	}
	return "#step-title"
}

func (d *rsDoc) stepFields(i int, hidden bool) []doctype.Field {
	var out []doctype.Field
	for _, name := range d.steps[i].Fields {
		if f, ok := d.dt.Field(name); ok && !f.ReadOnly && d.hide[name] == hidden {
			out = append(out, f)
		}
	}
	return out
}

func first(fs []doctype.Field) (doctype.Field, bool) {
	if len(fs) == 0 {
		return doctype.Field{}, false
	}
	return fs[0], true
}

func (d *rsDoc) shownField(i int) (doctype.Field, bool)  { return first(d.stepFields(i, false)) }
func (d *rsDoc) hiddenField(i int) (doctype.Field, bool) { return first(d.stepFields(i, true)) }

// typedField is step i's first shown Text field: the one a redraw must keep typed.
func (d *rsDoc) typedField(i int) (doctype.Field, bool) {
	return first(slices.DeleteFunc(d.stepFields(i, false), func(f doctype.Field) bool { return f.Type != doctype.Text }))
}

// parseField is step i's first shown field a parser reads (number or date).
func (d *rsDoc) parseField(i int) (doctype.Field, bool) {
	return first(slices.DeleteFunc(d.stepFields(i, false), func(f doctype.Field) bool { return !rsParsed(f) }))
}

func rsParsed(f doctype.Field) bool {
	switch f.Type {
	case doctype.Int, doctype.Money, doctype.Qty, doctype.Rate, doctype.Percent, doctype.Factor, doctype.Date:
		return true
	}
	return false
}

// otherField is an error step i must drop: on another header step's field,
// or line 1's on the lines step; ok is false when no other step owns one.
func (d *rsDoc) otherField(i int) (doctype.FieldError, bool) {
	for j, s := range d.steps[:len(d.steps)-1] {
		if j == i {
			continue
		}
		if s.Lines {
			if f, ok := d.shownLine(); ok {
				return doctype.FieldError{Field: f.Name, Line: 1, Message: rsMsgOther}, true
			}
			continue
		}
		if f, ok := d.shownField(j); ok {
			return doctype.FieldError{Field: f.Name, Message: rsMsgOther}, true
		}
	}
	return doctype.FieldError{}, false
}

func (d *rsDoc) lineFields(hidden bool) []doctype.Field {
	var out []doctype.Field
	for _, f := range d.dt.Fields {
		if f.OnLine && !f.ReadOnly && !f.RollUp && d.hide[f.Name] == hidden {
			out = append(out, f)
		}
	}
	return out
}

func (d *rsDoc) shownLine() (doctype.Field, bool)  { return first(d.lineFields(false)) }
func (d *rsDoc) hiddenLine() (doctype.Field, bool) { return first(d.lineFields(true)) }

func (d *rsDoc) parseLine() (doctype.Field, bool) {
	return first(slices.DeleteFunc(d.lineFields(false), func(f doctype.Field) bool { return !rsParsed(f) }))
}

// hiddenLink is the hidden line Link, the line card's item.
func (d *rsDoc) hiddenLink() (doctype.Field, bool) {
	return first(slices.DeleteFunc(d.lineFields(true), func(f doctype.Field) bool { return f.Type != doctype.Link }))
}

// validValue is a value DOC takes for f, in plain form: a Link names its
// target's first row.
func (e *rsEnv) validValue(t *testing.T, f doctype.Field) string {
	switch f.Type {
	case doctype.Link:
		tdt, ok := doctype.Lookup(f.Link)
		if !ok {
			t.Fatalf("field %s links to unregistered %s", f.Name, f.Link)
		}
		var id int64
		if err := e.w.app.DB().SQL().QueryRowContext(t.Context(), "SELECT id FROM "+tdt.Table+" ORDER BY id LIMIT 1").Scan(&id); err != nil {
			return ""
		}
		return strconv.FormatInt(id, 10)
	case doctype.Date:
		return types.Today(testkit.Clock()).ISO()
	case doctype.Text:
		return "x"
	case doctype.Select:
		if len(f.Options) > 0 {
			return f.Options[0]
		}
		return ""
	case doctype.Check:
		return ""
	}
	return "1"
}

// withSentinels is values with the hidden Text fields' sentinels, header
// (onLine false) or line.
func (d *rsDoc) withSentinels(values map[string]string, onLine bool) map[string]string {
	out := map[string]string{}
	maps.Copy(out, values)
	for name, v := range d.sentinel {
		if f, _ := d.dt.Field(name); f.OnLine == onLine {
			out[name] = v
		}
	}
	return out
}

// sources is the seeder's value of each Required header link to a document
// (a goods receipt's purchase order): a create needs its source.
func (d *rsDoc) sources(t *testing.T) map[string]string {
	t.Helper()
	values, _ := d.valid(t)
	out := map[string]string{}
	for _, f := range d.dt.Fields {
		if !f.OnLine && f.Required && rsDocLink(f) {
			out[f.Name] = values[f.Name]
		}
	}
	return out
}

// draft saves a new draft as the admin: the header holds only the sentinels
// and its sources, so its other Required fields are empty, and two valid
// lines with theirs.
func (d *rsDoc) draft(t *testing.T) int64 {
	t.Helper()
	_, line := d.valid(t)
	var lines []map[string]string
	if d.dt.LinesTable != "" {
		line = d.withSentinels(line, true)
		lines = []map[string]string{line, maps.Clone(line)}
		if d.dt.Name == goodsreceipt.DocType {
			lines[1]["po_line_id"] = d.e.poLine2 // a receipt names each PO line once
		}
	}
	res, err := d.e.w.app.Engine().Do(t.Context(), d.e.who[rsAdmin], d.dt.Name, 0, doc.ActionSave, doc.ActionInput{Values: d.withSentinels(d.sources(t), false), Lines: lines})
	if err != nil {
		pr, _ := docscreen.Classify(err)
		t.Fatalf("saving the %s probe draft: %v %v", d.dt.Name, err, pr.Fields)
	}
	return res.ID
}

// submitted saves a valid document with the sentinels and submits it as the
// admin: approved, or pending on an approvable DocType (the rule's approver
// is rsApprover). An error is DOC's refusal of the generic values.
func (d *rsDoc) submitted(t *testing.T) (doc.Result, error) {
	t.Helper()
	values, line := d.valid(t)
	return d.submittedWith(t, values, line)
}

// submittedWith is submitted with the given valid values.
func (d *rsDoc) submittedWith(t *testing.T, values, line map[string]string) (doc.Result, error) {
	t.Helper()
	e := d.e
	res, err := e.w.app.Engine().Do(t.Context(), e.who[rsAdmin], d.dt.Name, 0, doc.ActionSave, doc.ActionInput{
		Values: d.withSentinels(values, false), Lines: []map[string]string{d.withSentinels(line, true)},
	})
	if err != nil {
		return doc.Result{}, err
	}
	sub, err := e.w.app.Engine().Do(t.Context(), e.who[rsAdmin], d.dt.Name, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
	if err != nil {
		return doc.Result{}, err
	}
	want := doctype.StateApproved
	if d.dt.Approvable {
		want = doctype.StatePending
	}
	if sub.State != want {
		return doc.Result{}, fmt.Errorf("submitted %s is %s, want %s", d.dt.Name, sub.State, want)
	}
	sub.ID = res.ID
	return sub, nil
}

// act runs action a on id as user, with the stored version.
func (d *rsDoc) act(t *testing.T, user string, id int64, a doctype.Action, reason string) doc.Result {
	t.Helper()
	res, err := d.e.w.app.Engine().Do(t.Context(), d.e.who[user], d.dt.Name, id, a, doc.ActionInput{Version: d.version(t, id), Reason: reason})
	if err != nil {
		t.Fatalf("%s %s %d as %s: %v", a, d.dt.Name, id, user, err)
	}
	return res
}

// approved is a new approved document; see submitted.
func (d *rsDoc) approved(t *testing.T) (int64, error) {
	t.Helper()
	values, line := d.valid(t)
	return d.approvedWith(t, values, line)
}

// approvedWith is approved with the given valid values.
func (d *rsDoc) approvedWith(t *testing.T, values, line map[string]string) (int64, error) {
	t.Helper()
	res, err := d.submittedWith(t, values, line)
	if err != nil {
		return 0, err
	}
	id := res.ID
	if res.State == doctype.StatePending {
		res = d.act(t, rsApprover, res.ID, doctype.ActionApprove, "")
	}
	if res.State != doctype.StateApproved {
		return 0, fmt.Errorf("%s is %s after approving, want approved", d.dt.Name, res.State)
	}
	return id, nil
}

// mustApproved is approved, failing the cell on an error.
func (d *rsDoc) mustApproved(t *testing.T) int64 {
	t.Helper()
	id, err := d.approved(t)
	return must(t, id, err)
}

// must is id, failing the cell on an error.
func must(t *testing.T, id int64, err error) int64 {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// pending is a new pending document of an approvable DocType.
func (d *rsDoc) pending(t *testing.T) int64 {
	t.Helper()
	res, err := d.submitted(t)
	return must(t, res.ID, err)
}

// cancelled is a new cancelled document.
func (d *rsDoc) cancelled(t *testing.T) int64 {
	t.Helper()
	id := d.mustApproved(t)
	d.act(t, rsAdmin, id, doctype.ActionCancel, "probe")
	return id
}

func (e *rsEnv) loadErr(t *testing.T, name string, id int64) (doc.Record, error) {
	var rec doc.Record
	err := e.w.app.DB().Read(t.Context(), func(tx *db.Tx) error {
		var err error
		rec, err = e.w.app.Engine().Load(t.Context(), tx, name, id)
		return err
	})
	return rec, err
}

func (e *rsEnv) load(t *testing.T, name string, id int64) doc.Record {
	t.Helper()
	rec, err := e.loadErr(t, name, id)
	if err != nil {
		t.Fatalf("load %s %d: %v", name, id, err)
	}
	return rec
}

func (d *rsDoc) version(t *testing.T, id int64) int { return d.e.load(t, d.dt.Name, id).Version }

// stale is a version other than id's stored one.
func (d *rsDoc) stale(t *testing.T, id int64) string {
	v := d.version(t, id)
	if v > 1 {
		return strconv.Itoa(v - 1)
	}
	return strconv.Itoa(v + 1)
}

// rsResp is one answer with its body parsed.
type rsResp struct {
	code int
	hdr  http.Header
	body string
	page *goquery.Document
}

func (e *rsEnv) do(t *testing.T, mode, user, method, path string, form url.Values, htmx bool) rsResp {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequestWithContext(t.Context(), method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set("X-Rs-User", user)
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	e.h[mode].ServeHTTP(rec, req)
	page, err := goquery.NewDocumentFromReader(strings.NewReader(rec.Body.String()))
	if err != nil {
		t.Fatalf("%s %s: parsing the body: %v", method, path, err)
	}
	return rsResp{code: rec.Code, hdr: rec.Header(), body: rec.Body.String(), page: page}
}

// want fails the cell unless r has status code.
func (r rsResp) want(t *testing.T, what string, code int) rsResp {
	t.Helper()
	if r.code != code {
		t.Fatalf("%s = %d, want %d; body %.600s", what, r.code, code, r.body)
	}
	return r
}

// plainOf is the plain mode of mode, keeping its control suffix.
func plainOf(mode string) string {
	if strings.HasSuffix(mode, rsShow) {
		return "plain" + rsShow
	}
	return "plain"
}

func isShow(mode string) bool { return strings.HasSuffix(mode, rsShow) }

// formValues is what a browser posts from the form sel of page.
func formValues(page *goquery.Document, sel string) url.Values {
	out := url.Values{}
	page.Find(sel).Find("input[name], select[name], textarea[name]").Each(func(_ int, s *goquery.Selection) {
		name, _ := s.Attr("name")
		typ, _ := s.Attr("type")
		switch {
		case goquery.NodeName(s) == "select":
			out.Add(name, s.Find("option[selected]").AttrOr("value", ""))
		case goquery.NodeName(s) == "textarea":
			out.Add(name, s.Text())
		case typ == "submit" || typ == "button":
		case typ == "checkbox" || typ == "radio":
			if _, on := s.Attr("checked"); on {
				out.Add(name, s.AttrOr("value", "on"))
			}
		default:
			out.Add(name, s.AttrOr("value", ""))
		}
	})
	return out
}

// names is the sorted set of names posted by sel's inputs.
func names(page *goquery.Document, sel string) []string {
	var out []string
	page.Find(sel).Find("input[name], select[name], textarea[name]").Each(func(_ int, s *goquery.Selection) {
		if n := s.AttrOr("name", ""); !slices.Contains(out, n) {
			out = append(out, n)
		}
	})
	slices.Sort(out)
	return out
}

// summary is the error summary as "href|message" lines, in order.
func summary(page *goquery.Document) []string {
	var out []string
	page.Find(".error-summary li a").Each(func(_ int, s *goquery.Selection) {
		out = append(out, s.AttrOr("href", "")+"|"+strings.Join(strings.Fields(s.Text()), " "))
	})
	return out
}

// derived is the read-only summary rows (dl.summary) inside sel.
func derived(page *goquery.Document, sel string) []string {
	var out []string
	page.Find(sel).Find("dl.summary .summary-row").Each(func(_ int, s *goquery.Selection) {
		out = append(out, strings.Join(strings.Fields(s.Text()), " "))
	})
	return out
}

// inputValue is the value the first input named name shows.
func inputValue(page *goquery.Document, name string) string {
	s := page.Find(`[name="` + name + `"]`).First()
	if goquery.NodeName(s) == "textarea" {
		return s.Text()
	}
	return s.AttrOr("value", "")
}

func eq(a, b []string) bool { return slices.Equal(a, b) }

// rsMark is one place a page draws a probe field.
type rsMark struct {
	field, kind string
	card        bool // the text is only inside a module LineCard's cards (rsCardBug)
}

// marks is every place r draws a probe field: an input posting it, an element
// with its id, a link or label pointing there, its sentinel value, its Link's
// master name, or its label (when no other field or step has it). A ReadOnly
// field posted by a form is a mark too (CD-79).
func (d *rsDoc) marks(r rsResp) []rsMark {
	var out []rsMark
	outside := sync.OnceValue(func() string { // the page without its line cards
		page := goquery.CloneDocument(r.page)
		page.Find("li.lines-card").Remove()
		page.Find("main p").FilterFunction(func(_ int, s *goquery.Selection) bool { // removePage's question names the card's item
			return strings.HasPrefix(s.Text(), "Remove line ")
		}).Remove()
		html, _ := page.Html()
		return html
	})
	for _, name := range slices.Sorted(maps.Keys(d.hide)) {
		f, _ := d.dt.Field(name)
		add := func(kind string) { out = append(out, rsMark{field: name, kind: kind}) }
		if n := r.page.Find(`[name="` + name + `"]`).Length(); n > 0 {
			add("an input posts it")
		}
		ids := []string{components.FieldID(name)}
		for n := 1; n <= 3; n++ {
			ids = append(ids, docscreen.LineFieldID(n, name), "grid-"+strconv.Itoa(n)+"-"+name)
		}
		for _, id := range ids {
			if r.page.Find(`[id="`+id+`"]`).Length() > 0 {
				add("an element has the id " + id)
			}
			links := r.page.Find(`[href="#` + id + `"], [for="` + id + `"], [aria-describedby~="` + id + `"]`)
			switch {
			case links.Length() == 0:
			default:
				add("a link or label points at " + id)
			}
		}
		for kind, text := range map[string]string{"its value": d.sentinel[name], "its Link's name": d.shown[name]} {
			if text == "" || !strings.Contains(r.body, text) {
				continue
			}
			i := strings.Index(r.body, text)
			add(kind + " " + strconv.Quote(text) + " in " + strconv.Quote(r.body[max(0, i-200):i+len(text)]))
			if d.mod.LineCard != nil && !strings.Contains(outside(), text) {
				out[len(out)-1].card = true
			}
		}
		reasonPage := r.page.Find(`[name="reason"]`).Length() > 0
		if d.uniqueLabel(f) && !(reasonPage && slices.ContainsFunc(rsReasonLabels, func(l string) bool { return strings.EqualFold(l, f.Label) })) {
			r.page.Find("label, dt, th, legend").Each(func(_ int, s *goquery.Selection) {
				if strings.EqualFold(strings.Join(strings.Fields(s.Text()), " "), f.Label) {
					add("its label " + strconv.Quote(f.Label))
				}
			})
		}
	}
	for _, f := range d.dt.Fields {
		if f.ReadOnly && r.page.Find(`form [name="`+f.Name+`"]`).Length() > 0 {
			out = append(out, rsMark{field: f.Name, kind: "a form posts the ReadOnly field (CD-79)"})
		}
	}
	return out
}

// noHidden is check (a): r draws no probe field.
func (d *rsDoc) noHidden(t *testing.T, where string, r rsResp) {
	t.Helper()
	for _, m := range d.marks(r) {
		if m.card {
			d.cardBug.Store(true) // rsCardBug
			continue
		}
		t.Errorf("%s: the hidden field %s is drawn: %s", where, m.field, m.kind)
	}
}

// TestToolingRedraw_Sweep runs every cell of the table for every registered
// document DocType.
func TestToolingRedraw_Sweep(t *testing.T) {
	if n := len(rsDocTypes()); n < 8 {
		t.Fatalf("%d document DocTypes registered, want at least the 8 registered at a04f8f3d", n)
	}
	for _, dt := range rsDocTypes() {
		t.Run(dt.Name, func(t *testing.T) {
			// An App per DocType: parallel writes to one database answered 503
			// on a loaded machine.
			t.Parallel()
			d := newRSEnv(t).doc[dt.Name]
			if len(d.hide) == 0 {
				t.Fatalf("the probe hides nothing on %s: its rule picks no field", dt.Name)
			}
			d.names(t)
			d.sweepNew(t)
			id := d.draft(t)
			d.itemsNone = d.itemsDrawNone(t, id)
			d.sweepSteps(t, id)
			d.sweepLines(t, id)
			d.sweepActions(t, id)
			d.sweepApproval(t)
			d.sweepApproved(t)
			d.cell(t, "module line card", func(t *testing.T) {
				switch {
				case d.mod.LineCard == nil:
					t.Skip("the default card, checked by every cell (CD-319 (3))")
				case !d.cardBug.Load():
					t.Fatalf("%s's own LineCard printed no hidden field: rsCardBug no longer reproduces; remove it and its exemption in marks", dt.Name)
				}
				t.Skip(rsCardBug)
			})
			d.cell(t, "derived rows", func(t *testing.T) {
				if d.rows.Load() {
					return
				}
				for _, s := range d.steps[:len(d.steps)-1] {
					for _, name := range s.Fields {
						if f, _ := d.dt.Field(name); f.ReadOnly {
							t.Fatalf("step %s shows the read-only %s, but no 422/409 redraw compared read-only rows", s.Slug, name)
						}
					}
				}
				t.Skip("no wizard step but check shows a read-only field (checked)")
			})
		})
	}
}

// itemsDrawNone reports whether draft id's items step, with nothing hidden,
// draws no probe field: the grid's hidden line inputs, the cards' item (a
// hidden Link) or uom, or the totals' hidden ReadOnly header field all count.
func (d *rsDoc) itemsDrawNone(t *testing.T, id int64) bool {
	if d.dt.LinesTable == "" || d.grid(false) || d.sentinel["uom"] != "" {
		return false
	}
	if _, ok := d.hiddenLink(); ok {
		return false
	}
	values := d.e.load(t, d.dt.Name, id).Values
	for name := range d.hide {
		if f, _ := d.dt.Field(name); f.ReadOnly && !f.OnLine && values[name] != "" && d.uniqueLabel(f) {
			return false
		}
	}
	return true
}

func (d *rsDoc) base() string        { return "/doc/" + d.dt.Name }
func (d *rsDoc) url(id int64) string { return d.base() + "/" + strconv.FormatInt(id, 10) }

// none reports whether step i, as the probe draft shows it, draws no probe
// field: a header step with no hidden field of its own; the items step when
// itemsNone. Check shows the hidden fields' labels.
func (d *rsDoc) none(i int) bool {
	if d.steps[i].Lines {
		return d.itemsNone
	}
	if i == len(d.steps)-1 {
		return false
	}
	return !slices.ContainsFunc(d.steps[i].Fields, func(name string) bool { return d.hide[name] })
}

func (d *rsDoc) sweepNew(t *testing.T) {
	e := d.e
	get := func(t *testing.T, mode string) rsResp {
		return e.do(t, plainOf(mode), rsAdmin, http.MethodGet, d.base()+"/new", nil, false).want(t, "GET new", http.StatusOK)
	}
	var fresh rsResp
	d.cell(t, "new GET", func(t *testing.T) { fresh = d.both(t, "GET new", "plain", d.none(0), get) })
	if fresh.page == nil {
		return
	}
	shown := names(fresh.page, "#wizard-form")
	post := func(t *testing.T, mode string, edit func(url.Values), htmx bool) rsResp {
		form := formValues(get(t, mode).page, "#wizard-form")
		for name, v := range d.sources(t) {
			form.Set(name, v) // a link's input value is its id
		}
		if edit != nil {
			edit(form)
		}
		return e.do(t, mode, rsAdmin, http.MethodPost, d.base()+"/new", form, htmx)
	}
	d.cell(t, "new POST ok", func(t *testing.T) {
		run := func(t *testing.T, mode string) rsResp {
			r := post(t, mode, nil, true).want(t, "POST new, htmx", http.StatusOK)
			if r.hdr.Get("HX-Push-Url") == "" {
				t.Fatalf("POST new: no HX-Push-Url, so it didn't advance; body %.400s", r.body)
			}
			return r
		}
		// The new draft's step 2 draws a probe field when a fresh GET of it
		// does: an empty items step may show only the totals.
		next := run(t, "plain"+rsShow).hdr.Get("HX-Push-Url")
		none := len(d.marks(e.do(t, "plain"+rsShow, rsAdmin, http.MethodGet, next, nil, false).want(t, "GET "+next, http.StatusOK))) == 0
		d.both(t, "POST new ok", "plain", none, run)
	})
	d.cell(t, "new 422 module", func(t *testing.T) {
		f, typed := d.typedField(0)
		r := d.both(t, "POST new 422", "inject", d.none(0), func(t *testing.T, mode string) rsResp {
			r := post(t, mode, func(v url.Values) {
				if typed {
					v.Set(f.Name, rsTyped)
				}
			}, false).want(t, "POST new with probe errors", http.StatusUnprocessableEntity)
			if isShow(mode) {
				d.sameSummary(t, "POST new 422, nothing hidden", r, d.wantProbe(0, 0, false))
			}
			return r
		})
		d.sameNames(t, "POST new 422", r, shown)
		d.sameSummary(t, "POST new 422", r, d.wantProbe(0, 0, true))
		if typed && inputValue(r.page, f.Name) != rsTyped {
			t.Errorf("POST new 422: %s = %q, want the typed %q kept", f.Name, inputValue(r.page, f.Name), rsTyped)
		}
	})
	d.cell(t, "new 422 parse", func(t *testing.T) {
		f, ok := d.parseField(0)
		if !ok {
			d.noParsed(t, 0)
			t.Skip("step 1 shows no number or date field to mistype (checked)")
		}
		r := d.both(t, "POST new parse 422", "plain", d.none(0), func(t *testing.T, mode string) rsResp {
			return post(t, mode, func(v url.Values) { v.Set(f.Name, "zz") }, false).want(t, "POST new with "+f.Name+"=zz", http.StatusUnprocessableEntity)
		})
		d.sameNames(t, "POST new parse 422", r, shown)
		if got := summary(r.page); len(got) != 1 || !strings.HasPrefix(got[0], "#"+components.FieldID(f.Name)+"|") {
			t.Errorf("POST new parse 422: summary %q, want one error linking %s", got, components.FieldID(f.Name))
		}
		if inputValue(r.page, f.Name) != "zz" {
			t.Errorf("POST new parse 422: %s = %q, want the typed zz kept", f.Name, inputValue(r.page, f.Name))
		}
	})
	d.cell(t, "new 409", func(t *testing.T) {
		// newPost has a 409 branch for DOC's conflict, but a create carries no
		// version: a stale one is ignored and the draft is made.
		r := post(t, "plain", func(v url.Values) { v.Set("version", "999") }, false)
		if r.code != http.StatusSeeOther {
			t.Fatalf("POST new with version 999 = %d, want 303: the 409 cell is now reachable, sweep it; body %.400s", r.code, r.body)
		}
		t.Skip("a create can't conflict: POST new ignores the version (checked)")
	})
}

// noParsed checks the skip reason "step i shows no field to mistype".
func (d *rsDoc) noParsed(t *testing.T, i int) {
	t.Helper()
	if f, ok := d.parseField(i); ok {
		t.Fatalf("step %s shows the parsed field %s: the skip is stale", d.steps[i].Slug, f.Name)
	}
}

func (d *rsDoc) sameNames(t *testing.T, where string, r rsResp, want []string) {
	t.Helper()
	if got := names(r.page, "#wizard-form"); !eq(got, want) {
		t.Errorf("%s: the form posts %q, want %q as a fresh GET", where, got, want)
	}
}

// sameSummary is check (b)'s errors: exactly want.
func (d *rsDoc) sameSummary(t *testing.T, where string, r rsResp, want []string) {
	t.Helper()
	if got := summary(r.page); !eq(got, want) {
		t.Errorf("%s: error summary\n  %q\nwant\n  %q", where, got, want)
	}
}

func (d *rsDoc) stepURL(id int64, i int) string {
	return d.url(id) + "/edit/" + d.steps[i].Slug
}

func (d *rsDoc) sweepSteps(t *testing.T, id int64) {
	e := d.e
	lines := len(e.load(t, d.dt.Name, id).Lines)
	for i, s := range d.steps {
		get := func(t *testing.T, mode string) rsResp {
			return e.do(t, plainOf(mode), rsAdmin, http.MethodGet, d.stepURL(id, i), nil, false).want(t, "GET "+s.Slug, http.StatusOK)
		}
		var fresh rsResp
		d.cell(t, "step GET "+s.Slug, func(t *testing.T) { fresh = d.both(t, "GET "+s.Slug, "plain", d.none(i), get) })
		if fresh.page == nil || s.Slug == "check" {
			continue
		}
		shown := names(fresh.page, "#wizard-form")
		before := derived(fresh.page, "#wizard")
		var posted string
		forms := map[string]url.Values{} // by GET mode; the version is set on each post
		post := func(t *testing.T, mode string, edit func(url.Values), htmx bool) rsResp {
			t.Helper()
			if forms[plainOf(mode)] == nil {
				forms[plainOf(mode)] = formValues(get(t, mode).page, "#wizard-form")
			}
			form := maps.Clone(forms[plainOf(mode)])
			form.Set("version", strconv.Itoa(d.version(t, id)))
			if edit != nil {
				edit(form)
			}
			posted = form.Get("version")
			return e.do(t, mode, rsAdmin, http.MethodPost, d.stepURL(id, i), form, htmx)
		}
		d.cell(t, "step 422 module "+s.Slug, func(t *testing.T) {
			f, typed := d.typedField(i)
			where := "POST " + s.Slug + " with probe errors"
			r := d.both(t, where, "inject", d.none(i), func(t *testing.T, mode string) rsResp {
				r := post(t, mode, func(v url.Values) {
					if typed {
						v.Set(f.Name, rsTyped)
					}
				}, false).want(t, where, http.StatusUnprocessableEntity)
				if isShow(mode) {
					d.sameSummary(t, where+", nothing hidden", r, d.wantProbe(i, lines, false))
				}
				return r
			})
			d.sameNames(t, where, r, shown)
			d.sameSummary(t, where, r, d.wantProbe(i, lines, true))
			d.keeps(t, where, r, posted, before)
			if typed && inputValue(r.page, f.Name) != rsTyped {
				t.Errorf("%s: %s = %q, want the typed %q kept", where, f.Name, inputValue(r.page, f.Name), rsTyped)
			}
		})
		d.cell(t, "step 422 parse "+s.Slug, func(t *testing.T) {
			f, ok := d.parseField(i)
			if !ok {
				d.noParsed(t, i)
				t.Skip("the step shows no number or date field to mistype (checked)")
			}
			where := "POST " + s.Slug + " with " + f.Name + "=zz"
			r := d.both(t, where, "plain", d.none(i), func(t *testing.T, mode string) rsResp {
				return post(t, mode, func(v url.Values) { v.Set(f.Name, "zz") }, false).want(t, where, http.StatusUnprocessableEntity)
			})
			d.sameNames(t, where, r, shown)
			if got := summary(r.page); len(got) != 1 || !strings.HasPrefix(got[0], "#"+components.FieldID(f.Name)+"|") {
				t.Errorf("%s: summary %q, want one error linking %s", where, got, components.FieldID(f.Name))
			}
			d.keeps(t, where, r, posted, before)
		})
		d.cell(t, "step 409 "+s.Slug, func(t *testing.T) {
			f, typed := d.typedField(i)
			where := "POST " + s.Slug + " with a stale version"
			var stale string
			r := d.both(t, where, "plain", d.none(i), func(t *testing.T, mode string) rsResp {
				stale = d.stale(t, id)
				return post(t, mode, func(v url.Values) {
					v.Set("version", stale)
					if typed {
						v.Set(f.Name, rsTyped)
					}
				}, false).want(t, where, http.StatusConflict)
			})
			d.sameNames(t, where, r, shown)
			d.sameSummary(t, where, r, nil)
			if r.page.Find("#wizard-conflict").Length() != 1 {
				t.Errorf("%s: no #wizard-conflict banner", where)
			}
			d.keeps(t, where, r, stale, before)
			if typed && inputValue(r.page, f.Name) != rsTyped {
				t.Errorf("%s: %s = %q, want the typed %q kept", where, f.Name, inputValue(r.page, f.Name), rsTyped)
			}
		})
		d.cell(t, "step stay "+s.Slug, func(t *testing.T) {
			d.both(t, "POST "+s.Slug+" nav=stay", "plain", d.none(i), func(t *testing.T, mode string) rsResp {
				r := post(t, mode, func(v url.Values) { v.Set("nav", "stay") }, true).want(t, "POST "+s.Slug+" nav=stay", http.StatusOK)
				if got := r.hdr.Get("HX-Push-Url"); got != d.stepURL(id, i) {
					t.Errorf("POST %s nav=stay pushed %q, want %q", s.Slug, got, d.stepURL(id, i))
				}
				return r
			})
		})
		d.cell(t, "step POST ok "+s.Slug, func(t *testing.T) {
			d.both(t, "POST "+s.Slug+" ok", "plain", d.none(i+1), func(t *testing.T, mode string) rsResp {
				r := post(t, mode, nil, true).want(t, "POST "+s.Slug, http.StatusOK)
				if got := r.hdr.Get("HX-Push-Url"); got != d.stepURL(id, i+1) {
					t.Errorf("POST %s pushed %q, want the next step %q", s.Slug, got, d.stepURL(id, i+1))
				}
				return r
			})
		})
	}
	d.cell(t, "step finish", func(t *testing.T) {
		form := formValues(e.do(t, "plain", rsAdmin, http.MethodGet, d.stepURL(id, 0), nil, false).page, "#wizard-form")
		form.Set("nav", "finish")
		r := e.do(t, "plain", rsAdmin, http.MethodPost, d.stepURL(id, 0), form, false).want(t, "POST nav=finish", http.StatusSeeOther)
		if r.page.Find("form").Length() != 0 {
			t.Errorf("POST nav=finish drew a form: it only redirects")
		}
	})
}

// keeps is checks (c) and (d) on a step redraw: the posted version, and the
// read-only rows as a fresh GET shows them.
func (d *rsDoc) keeps(t *testing.T, where string, r rsResp, posted string, before []string) {
	t.Helper()
	if got := r.page.Find("#doc-version").AttrOr("value", ""); got != posted {
		t.Errorf("%s: #doc-version = %q, want the posted %q (CD-94)", where, got, posted)
	}
	if got := derived(r.page, "#wizard"); !eq(got, before) {
		t.Errorf("%s: read-only rows %q, want %q as a fresh GET (CD-302)", where, got, before)
	}
	if len(before) > 0 {
		d.rows.Store(true)
	}
}

func (d *rsDoc) lineURL(id int64, no string) string { return d.url(id) + "/lines/" + no }

func (d *rsDoc) sweepLines(t *testing.T, id int64) {
	e := d.e
	if d.dt.LinesTable == "" {
		d.cell(t, "lines", func(t *testing.T) {
			if slices.ContainsFunc(d.steps, func(s docscreen.FormStep) bool { return s.Lines }) {
				t.Fatalf("%s has no LinesTable but a lines step", d.dt.Name)
			}
			t.Skip("no lines (checked)")
		})
		return
	}
	getLine := func(no string) func(t *testing.T, mode string) rsResp {
		return func(t *testing.T, mode string) rsResp {
			return e.do(t, plainOf(mode), rsAdmin, http.MethodGet, d.lineURL(id, no), nil, false).want(t, "GET line "+no, http.StatusOK)
		}
	}
	var fresh rsResp
	d.cell(t, "line GET", func(t *testing.T) {
		d.both(t, "GET line new", "plain", false, getLine("new"))
		fresh = d.both(t, "GET line 1", "plain", false, getLine("1"))
	})
	if fresh.page == nil {
		return
	}
	shown := names(fresh.page, "main form")
	forms := map[string]url.Values{} // by GET mode; the version is set on each post
	form := func(t *testing.T, mode string) url.Values {
		t.Helper()
		if forms[plainOf(mode)] == nil {
			forms[plainOf(mode)] = formValues(getLine("1")(t, mode).page, "main form")
		}
		v := maps.Clone(forms[plainOf(mode)])
		v.Set("version", strconv.Itoa(d.version(t, id)))
		return v
	}
	lineKeeps := func(t *testing.T, where string, r rsResp, posted string) {
		t.Helper()
		if got := names(r.page, "main form"); !eq(got, shown) {
			t.Errorf("%s: the form posts %q, want %q as a fresh GET", where, got, shown)
		}
		if got := inputValue(r.page, "version"); got != posted {
			t.Errorf("%s: version = %q, want the posted %q (CD-94)", where, got, posted)
		}
	}
	d.cell(t, "line GET module", func(t *testing.T) {
		// The probe's Line.Body draws nothing, and docscreen's shell no field.
		d.both(t, "GET line 1 with a module Line", "line", true, func(t *testing.T, mode string) rsResp {
			return e.do(t, mode, rsAdmin, http.MethodGet, d.lineURL(id, "1"), nil, false).want(t, "GET line 1 with a module Line", http.StatusOK)
		})
	})
	d.cell(t, "line 422 parse", func(t *testing.T) {
		f, ok := d.parseLine()
		if !ok {
			t.Fatalf("%s shows no number or date line field to mistype", d.dt.Name)
		}
		where := "POST line 1 with " + f.Name + "=zz"
		var posted string
		r := d.both(t, where, "plain", false, func(t *testing.T, mode string) rsResp {
			v := form(t, mode)
			v.Set(f.Name, "zz")
			posted = v.Get("version")
			return e.do(t, mode, rsAdmin, http.MethodPost, d.lineURL(id, "1"), v, false).want(t, where, http.StatusUnprocessableEntity)
		})
		lineKeeps(t, where, r, posted)
		if got := summary(r.page); len(got) != 1 || !strings.HasPrefix(got[0], "#"+components.FieldID(f.Name)+"|") {
			t.Errorf("%s: summary %q, want one error linking %s", where, got, components.FieldID(f.Name))
		}
		if inputValue(r.page, f.Name) != "zz" {
			t.Errorf("%s: %s = %q, want the typed zz kept", where, f.Name, inputValue(r.page, f.Name))
		}
	})
	d.cell(t, "line 409", func(t *testing.T) {
		var stale string
		r := d.both(t, "POST line 1 with a stale version", "plain", false, func(t *testing.T, mode string) rsResp {
			v := form(t, mode)
			stale = d.stale(t, id)
			v.Set("version", stale)
			return e.do(t, mode, rsAdmin, http.MethodPost, d.lineURL(id, "1"), v, false).want(t, "POST line 1 with a stale version", http.StatusConflict)
		})
		lineKeeps(t, "POST line 1 with a stale version", r, stale)
		d.sameSummary(t, "POST line 1 409 (the conflict is a banner)", r, nil)
	})
	d.cell(t, "line 422 module", func(t *testing.T) {
		for _, no := range []string{"1", "new"} {
			where := "POST line " + no + " with Line.Save errors"
			var posted string
			// With nothing hidden the summary links the hidden field's error.
			r := d.both(t, where, "line", false, func(t *testing.T, mode string) rsResp {
				v := form(t, mode)
				posted = v.Get("version")
				return e.do(t, mode, rsAdmin, http.MethodPost, d.lineURL(id, no), v, false).want(t, where, http.StatusUnprocessableEntity)
			})
			var want []string
			if f, ok := d.shownLine(); ok {
				want = append(want, "#"+components.FieldID(f.Name)+"|"+rsMsgVisible)
			}
			if _, ok := d.hiddenLine(); ok {
				want = append(want, "#main|"+rsMsgHidden)
			}
			want = append(want, "#main|"+rsMsgPlain)
			if got := summary(r.page); !eq(got, want) {
				t.Errorf("%s: error summary\n  %q\nwant\n  %q", where, got, want)
			}
			if got := inputValue(r.page, "version"); got != posted {
				t.Errorf("%s: version = %q, want the posted %q (CD-94)", where, got, posted)
			}
		}
	})
	d.cell(t, "grid 422", func(t *testing.T) {
		d.noGrid(t, id, form)
		f, ok := d.parseLine()
		if !ok {
			t.Fatalf("%s shows no number or date line field to mistype", d.dt.Name)
		}
		r := d.both(t, "grid 422", "plain", false, func(t *testing.T, mode string) rsResp {
			v := form(t, mode)
			v.Set(f.Name, "zz")
			return e.do(t, mode, rsAdmin, http.MethodPost, d.lineURL(id, "1"), v, true).want(t, "grid POST line 1 with "+f.Name+"=zz", http.StatusUnprocessableEntity)
		})
		if r.page.Find("#doc-version").Length() != 0 {
			t.Errorf("grid 422 sent a #doc-version: the page must keep its own (CD-94)")
		}
		if got := r.page.Find(`[id="grid-1-`+f.Name+`"]`).AttrOr("value", ""); got != "zz" {
			t.Errorf("grid 422: grid-1-%s = %q, want the typed zz kept", f.Name, got)
		}
	})
	d.cell(t, "grid 409", func(t *testing.T) {
		d.noGrid(t, id, form)
		r := d.both(t, "grid 409", "plain", false, func(t *testing.T, mode string) rsResp {
			v := form(t, mode)
			v.Set("version", d.stale(t, id))
			return e.do(t, mode, rsAdmin, http.MethodPost, d.lineURL(id, "1"), v, true).want(t, "grid POST line 1 with a stale version", http.StatusConflict)
		})
		if r.page.Find("#doc-version").Length() != 0 {
			t.Errorf("grid 409 sent a #doc-version: the page must keep its own (CD-94)")
		}
		if r.page.Find("#grid-alerts").Length() != 1 {
			t.Errorf("grid 409: no #grid-alerts with the conflict")
		}
	})
	d.cell(t, "grid ok", func(t *testing.T) {
		d.noGrid(t, id, form)
		r := d.both(t, "grid ok", "plain", false, func(t *testing.T, mode string) rsResp {
			return e.do(t, mode, rsAdmin, http.MethodPost, d.lineURL(id, "1"), form(t, mode), true).want(t, "grid POST line 1", http.StatusOK)
		})
		if got, want := r.page.Find("#doc-version").AttrOr("value", ""), strconv.Itoa(d.version(t, id)); got != want {
			t.Errorf("grid ok: #doc-version = %q, want the saved %q", got, want)
		}
	})
	_, link := d.hiddenLink()
	d.cell(t, "card", func(t *testing.T) {
		d.both(t, "GET items", "plain", d.itemsNone, func(t *testing.T, mode string) rsResp {
			r := e.do(t, mode, rsAdmin, http.MethodGet, d.url(id)+"/edit/items", nil, false).want(t, "GET items", http.StatusOK)
			if r.page.Find(".lines-card").Length() == 0 {
				t.Fatalf("GET items: no line cards")
			}
			return r
		})
	})
	// The remove page names the line by its card's item: with no hidden
	// Link, it draws no probe field.
	d.cell(t, "remove GET", func(t *testing.T) {
		d.both(t, "GET remove line 1", "plain", !link, func(t *testing.T, mode string) rsResp {
			return e.do(t, mode, rsAdmin, http.MethodGet, d.lineURL(id, "1")+"/remove", nil, false).want(t, "GET remove line 1", http.StatusOK)
		})
	})
	d.cell(t, "remove 409", func(t *testing.T) {
		var stale string
		r := d.both(t, "POST remove 409", "plain", !link, func(t *testing.T, mode string) rsResp {
			stale = d.stale(t, id)
			return e.do(t, mode, rsAdmin, http.MethodPost, d.lineURL(id, "1")+"/remove", url.Values{"version": {stale}}, false).want(t, "POST remove line 1 with a stale version", http.StatusConflict)
		})
		if got := inputValue(r.page, "version"); got != stale {
			t.Errorf("POST remove 409: version = %q, want the posted %q (CD-94)", got, stale)
		}
	})
	d.cell(t, "move 409", func(t *testing.T) {
		r := d.both(t, "POST move 409", "plain", d.itemsNone, func(t *testing.T, mode string) rsResp {
			return e.do(t, mode, rsAdmin, http.MethodPost, d.lineURL(id, "1")+"/move?dir=down", url.Values{"version": {d.stale(t, id)}}, false).want(t, "POST move line 1 with a stale version", http.StatusConflict)
		})
		d.sameSummary(t, "POST move 409", r, nil)
		if r.page.Find("#wizard-conflict").Length() != 1 {
			t.Errorf("POST move 409: no #wizard-conflict banner")
		}
		// The items step redraws the stored lines, nothing typed, so it
		// carries the stored version.
		if got, want := r.page.Find("#doc-version").AttrOr("value", ""), strconv.Itoa(d.version(t, id)); got != want {
			t.Errorf("POST move 409: #doc-version = %q, want the stored %q", got, want)
		}
	})
	d.linesRefused(t)
}

// rsRefusal is one way DOC refuses a save of the lines with field errors
// although a move or remove changes no line (ActionInput.Only, CD-207 (2)):
// setup breaks a new draft of d and names the user who saves it; reason,
// when not "", is the registry fact that leaves d without the route.
type rsRefusal struct {
	name   string
	reason func(d *rsDoc) string
	setup  func(t *testing.T, d *rsDoc) (id int64, user string)
}

// rsRefusals are the routes Only's doc lists as refusing out of scope, for
// stored lines: CD-178's restricted Required error, a missing link, a parse
// error, a read-only error, and, as a check that it stays out of scope, a
// field error on an unchanged line (MaxLen). An error with no field name
// comes only from a hook or an internal error, not from stored lines.
var rsRefusals = []rsRefusal{
	{"restricted Required", func(d *rsDoc) string {
		if _, _, _, ok := d.restrictable(); !ok {
			return "no Required header Link in ProjectFields or SiteFields with a line Link in the same dimension, which keeps a draft without it readable by a restricted user"
		}
		return ""
	}, func(t *testing.T, d *rsDoc) (int64, string) { return d.restrictedDraft(t) }},
	{"missing link", func(d *rsDoc) string {
		if _, ok := d.unkeyed(); !ok {
			return "every line Link has a foreign key, so none can name a missing master"
		}
		return ""
	}, func(t *testing.T, d *rsDoc) (int64, string) {
		f, _ := d.unkeyed()
		return d.brokenDraft(t, f, rsBadLink), rsAdmin
	}},
	{"parse", func(d *rsDoc) string {
		if _, ok := d.numericLine(); !ok {
			return "no number field without a foreign key on the lines"
		}
		return ""
	}, func(t *testing.T, d *rsDoc) (int64, string) {
		f, _ := d.numericLine()
		return d.brokenDraft(t, f, "zz"), rsAdmin
	}},
	{"read-only", func(d *rsDoc) string {
		if _, ok := d.readOnlyLine(); !ok {
			return "no ReadOnly line field"
		}
		return ""
	}, func(t *testing.T, d *rsDoc) (int64, string) {
		f, _ := d.readOnlyLine()
		return d.brokenDraft(t, f, "7"), rsAdmin
	}},
	{"out of scope (MaxLen)", func(d *rsDoc) string {
		if _, ok := d.maxLenLine(); !ok {
			return "no line Text with a MaxLen"
		}
		return ""
	}, func(t *testing.T, d *rsDoc) (int64, string) {
		f, _ := d.maxLenLine()
		return d.brokenDraft(t, f, strings.Repeat("x", f.MaxLen+1)), rsAdmin
	}},
}

// restrictable is the dimension, Required header field and line Link of the
// restricted Required route.
func (d *rsDoc) restrictable() (auth.Dimension, doctype.Field, doctype.Field, bool) {
	for dim, cols := range map[auth.Dimension][]string{auth.DimProject: d.dt.ProjectFields, auth.DimSite: d.dt.SiteFields} {
		var header, line *doctype.Field
		for _, f := range d.dt.Fields {
			switch {
			case !f.OnLine && f.Required && slices.Contains(cols, cmp.Or(f.Column, f.Name)):
				header = &f
			case f.OnLine && f.Type == doctype.Link && slices.Contains(cols, "lines."+cmp.Or(f.Column, f.Name)):
				line = &f
			}
		}
		if header != nil && line != nil {
			return dim, *header, *line, true
		}
	}
	return "", doctype.Field{}, doctype.Field{}, false
}

// restrictedDraft is a draft with the Required header dimension field empty
// and every line's Link on that dimension at V, saved by the admin, and a
// user with read and write restricted to V, who sees it through its lines
// (CD-178).
func (d *rsDoc) restrictedDraft(t *testing.T) (int64, string) {
	t.Helper()
	e, ctx := d.e, t.Context()
	dim, _, link, _ := d.restrictable()
	v := e.validValue(t, link)
	const user = "rs-restricted"
	if _, ok := e.who[user]; !ok {
		a, admin := e.w.app.Auth(), e.who[rsAdmin]
		if err := a.CreateRole(ctx, admin, rsRole(user)); err != nil {
			t.Fatal(err)
		}
		if err := a.SetRights(ctx, admin, rsRole(user), d.dt.Name, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightWrite: auth.ScopeAll}); err != nil {
			t.Fatal(err)
		}
		id, _, err := a.CreateUser(ctx, admin, auth.NewUser{Email: user + "@vardhan.test", Name: user, Password: testPassword, Roles: []string{rsRole(user)}})
		if err != nil {
			t.Fatal(err)
		}
		n, _ := strconv.ParseInt(v, 10, 64)
		if err := a.ChangeRestrictions(ctx, admin, id, dim, []int64{n}, nil, nil); err != nil {
			t.Fatal(err)
		}
		e.who[user] = e.w.principal(id)
	}
	_, line := d.valid(t)
	line = d.withSentinels(line, true)
	line[link.Name] = v
	res, err := e.w.app.Engine().Do(ctx, e.who[rsAdmin], d.dt.Name, 0, doc.ActionSave, doc.ActionInput{Values: d.withSentinels(nil, false), Lines: []map[string]string{line, maps.Clone(line)}})
	if err != nil {
		t.Fatalf("saving the restricted draft: %v", err)
	}
	return res.ID, user
}

// unkeyed is a line Link with no foreign key, the hidden one first.
func (d *rsDoc) unkeyed() (doctype.Field, bool) {
	for _, f := range append(d.lineFields(true), d.lineFields(false)...) {
		if f.Type == doctype.Link && !d.e.foreignKey(d.dt.LinesTable, cmp.Or(f.Column, f.Name)) {
			return f, true
		}
	}
	return doctype.Field{}, false
}

// numericLine is a line field a parser reads but not a date, with no
// foreign key, the hidden one first.
func (d *rsDoc) numericLine() (doctype.Field, bool) {
	return first(slices.DeleteFunc(append(d.lineFields(true), d.lineFields(false)...), func(f doctype.Field) bool {
		return !rsParsed(f) || f.Type == doctype.Date || d.e.foreignKey(d.dt.LinesTable, cmp.Or(f.Column, f.Name))
	}))
}

// maxLenLine is a line Text with a MaxLen, the hidden one first.
func (d *rsDoc) maxLenLine() (doctype.Field, bool) {
	return first(slices.DeleteFunc(append(d.lineFields(true), d.lineFields(false)...), func(f doctype.Field) bool { return f.Type != doctype.Text || f.MaxLen == 0 }))
}

// readOnlyLine is a ReadOnly line field.
func (d *rsDoc) readOnlyLine() (doctype.Field, bool) {
	for _, f := range d.dt.Fields {
		if f.OnLine && f.ReadOnly && !f.RollUp {
			return f, true
		}
	}
	return doctype.Field{}, false
}

// brokenDraft is a new probe draft with every line's f stored as bad.
func (d *rsDoc) brokenDraft(t *testing.T, f doctype.Field, bad string) int64 {
	t.Helper()
	id := d.draft(t)
	if _, err := d.e.w.app.DB().SQL().ExecContext(t.Context(), "UPDATE "+d.dt.LinesTable+" SET "+cmp.Or(f.Column, f.Name)+" = ? WHERE parent_id = ?", bad, id); err != nil {
		t.Fatalf("store %s = %q: %v", f.Name, bad, err)
	}
	return id
}

// foreignKey reports whether table's column has a foreign key.
func (e *rsEnv) foreignKey(table, column string) bool {
	var n int
	if err := e.w.app.DB().SQL().QueryRowContext(e.t.Context(), `SELECT count(*) FROM pragma_foreign_key_list(?) WHERE "from" = ?`, table, column).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n > 0
}

// linesRefused drives itemsRefused with DOC's field errors, one cell per
// rsRefusals route. Each cell either sweeps a move (the items step through
// refusalErrors) and a remove (the remove page) refused that way, or skips
// with what stops it, re-checked every run: the registry fact, DOC saving the
// move, or the handler answering before DOC.
func (d *rsDoc) linesRefused(t *testing.T) {
	e := d.e
	items := slices.IndexFunc(d.steps, func(s docscreen.FormStep) bool { return s.Lines })
	for _, route := range rsRefusals {
		d.cell(t, "lines 422 "+route.name, func(t *testing.T) {
			if why := route.reason(d); why != "" {
				t.Skipf("%s (checked)", why)
			}
			move := func(t *testing.T, mode string, errs *[]doctype.FieldError, lines *int) rsResp {
				id, user := route.setup(t, d)
				rec, err := e.loadErr(t, d.dt.Name, id)
				if err != nil {
					t.Skipf("DOC can't load the stored value, so a move, which sends the stored lines, never carries it (checked: %v)", err)
				}
				// What reorder sends for a move of line 1 down: every stored line.
				swapped := slices.Clone(rec.Lines)
				swapped[0], swapped[1] = swapped[1], swapped[0]
				_, err = e.w.app.Engine().Do(t.Context(), e.who[user], d.dt.Name, id, doc.ActionSave, doc.ActionInput{
					Version: rec.Version, Lines: swapped, Only: []string{"lines"},
				})
				if err == nil {
					t.Skip("DOC saves the move: the error is out of scope (checked)")
				}
				pr, ok := docscreen.Classify(err)
				if !ok || pr.Status != http.StatusUnprocessableEntity || len(pr.Fields) == 0 {
					t.Skipf("DOC refuses the move without field errors (checked: %v)", err)
				}
				*errs, *lines = pr.Fields, len(rec.Lines)
				r := e.do(t, mode, user, http.MethodPost, d.lineURL(id, "1")+"/move?dir=down", url.Values{"version": {strconv.Itoa(rec.Version)}}, false)
				if r.code != http.StatusUnprocessableEntity {
					// The remove is tried too, so the skip covers both handlers.
					rid, ruser := route.setup(t, d)
					rm := e.do(t, mode, ruser, http.MethodPost, d.lineURL(rid, "2")+"/remove", url.Values{"version": {strconv.Itoa(d.version(t, rid))}}, false)
					if rm.code == http.StatusUnprocessableEntity {
						t.Fatalf("the move answers %d before DOC's refusal but the remove reaches it: sweep the remove on this route", r.code)
					}
					t.Skipf("the move answers %d and the remove %d before DOC's refusal (checked: DOC refuses the move with %v)", r.code, rm.code, pr.Fields)
				}
				return r
			}
			var errs []doctype.FieldError
			var lines int
			r := d.both(t, "POST move, DOC refuses ("+route.name+")", "plain", false, func(t *testing.T, mode string) rsResp {
				return move(t, mode, &errs, &lines)
			})
			if got, want := summary(r.page), d.wantErrors(errs, items, lines, true); !eq(got, want) {
				t.Errorf("POST move, DOC refuses (%s): error summary\n  %q\nwant\n  %q", route.name, got, want)
			}
			var posted string
			// The remove page names the line by its card's item.
			_, link := d.hiddenLink()
			r = d.both(t, "POST remove, DOC refuses ("+route.name+")", "plain", !link, func(t *testing.T, mode string) rsResp {
				id, user := route.setup(t, d)
				posted = strconv.Itoa(d.version(t, id))
				return e.do(t, mode, user, http.MethodPost, d.lineURL(id, "2")+"/remove", url.Values{"version": {posted}}, false).
					want(t, "POST remove, DOC refuses ("+route.name+")", http.StatusUnprocessableEntity)
			})
			if got := inputValue(r.page, "version"); got != posted {
				t.Errorf("POST remove 422: version = %q, want the posted %q (CD-94)", got, posted)
			}
			msg := errs[0].Message
			if !strings.Contains(r.body, msg) {
				t.Errorf("POST remove 422: the page doesn't say %q", msg)
			}
		})
	}
}

// noGrid skips a grid cell unless the editable grid shows both with the
// probe hiding and without: the module turns it off (CD-256 (6)), or the row
// has other than 3 or 5 editable cells (CD-315). The check is that an htmx
// post of line 1 takes the line page's path (303), not a grid row's, in each
// mode without the grid. Each mode posts its own form: the hidden run's form
// posted to the show twin would clear the probe's fields.
func (d *rsDoc) noGrid(t *testing.T, id int64, form func(t *testing.T, mode string) url.Values) {
	t.Helper()
	off := ""
	for _, mode := range []string{"plain", "plain" + rsShow} {
		if d.grid(!isShow(mode)) {
			continue
		}
		r := d.e.do(t, mode, rsAdmin, http.MethodPost, d.lineURL(id, "1"), form(t, mode), true)
		if r.code != http.StatusSeeOther {
			t.Fatalf("an htmx post of line 1 in %s = %d, want the line page's 303 with the grid off", mode, r.code)
		}
		off += " " + mode
	}
	if off != "" {
		t.Skipf("no editable grid in%s: the module turns it off or the row has other than 3 or 5 cells (CD-256 (6), CD-315) (checked)", off)
	}
}

func (d *rsDoc) sweepActions(t *testing.T, id int64) {
	e := d.e
	d.cell(t, "page reader", func(t *testing.T) {
		d.both(t, "GET the draft as a reader", "plain", false, func(t *testing.T, mode string) rsResp {
			return e.do(t, mode, rsReader, http.MethodGet, d.url(id), nil, false).want(t, "GET the draft as a reader", http.StatusOK)
		})
		for _, path := range []string{d.stepURL(id, 0), d.lineURL(id, "1")} {
			if d.dt.LinesTable == "" && strings.Contains(path, "/lines/") {
				continue
			}
			g := e.do(t, "plain", rsReader, http.MethodGet, path, nil, false)
			if g.code != http.StatusSeeOther || g.hdr.Get("Location") != d.url(id) {
				t.Errorf("GET %s as a reader = %d to %q, want 303 to the document page", path, g.code, g.hdr.Get("Location"))
			}
		}
	})
	// The delete page shows the draft's number, party and total: the probe
	// draft's party is empty.
	d.cell(t, "reason GET", func(t *testing.T) {
		d.both(t, "GET delete", "plain", true, func(t *testing.T, mode string) rsResp {
			return e.do(t, mode, rsAdmin, http.MethodGet, d.url(id)+"/delete", nil, false).want(t, "GET delete", http.StatusOK)
		})
	})
	if !d.dt.Submittable {
		// No submit: each cell checks DOC refuses it (CD-21), so a DocType
		// that becomes Submittable runs them.
		for _, c := range []struct {
			name, user, version string
		}{
			{"submit 409", rsAdmin, d.stale(t, id)},
			{"submit 422", rsAdmin, strconv.Itoa(d.version(t, id))},
			{"submit 422 reader", rsSubmitter, strconv.Itoa(d.version(t, id))},
		} {
			d.cell(t, c.name, func(t *testing.T) {
				e.do(t, "plain", c.user, http.MethodPost, d.url(id)+"/submit", url.Values{"version": {c.version}}, false).want(t, "POST submit on a DocType that isn't Submittable", http.StatusForbidden)
				t.Skip("(checked) not Submittable: DOC refuses the submit")
			})
		}
		return
	}
	d.cell(t, "submit 409", func(t *testing.T) {
		d.both(t, "POST submit 409", "plain", false, func(t *testing.T, mode string) rsResp {
			return e.do(t, mode, rsAdmin, http.MethodPost, d.url(id)+"/submit", url.Values{"version": {d.stale(t, id)}}, false).want(t, "POST submit with a stale version", http.StatusConflict)
		})
	})
	errs := d.submitErrors(t)
	d.cell(t, "submit 422", func(t *testing.T) {
		r := d.both(t, "POST submit 422", "plain", false, func(t *testing.T, mode string) rsResp {
			return e.do(t, mode, rsAdmin, http.MethodPost, d.url(id)+"/submit", url.Values{"version": {strconv.Itoa(d.version(t, id))}}, false).want(t, "POST submit", http.StatusUnprocessableEntity)
		})
		i := d.failing(errs)
		if got, want := summary(r.page), d.wantErrors(errs, i, len(e.load(t, d.dt.Name, id).Lines), false); !eq(got, want) {
			t.Errorf("POST submit 422 on step %s: error summary\n  %q\nwant\n  %q", d.steps[i].Slug, got, want)
		}
	})
	d.cell(t, "submit 422 reader", func(t *testing.T) {
		r := d.both(t, "POST submit 422 as a submitter", "plain", false, func(t *testing.T, mode string) rsResp {
			return e.do(t, mode, rsSubmitter, http.MethodPost, d.url(id)+"/submit", url.Values{"version": {strconv.Itoa(d.version(t, id))}}, false).
				want(t, "POST submit as a submitter without write", http.StatusUnprocessableEntity)
		})
		if got := len(summary(r.page)); got != len(errs) {
			t.Errorf("POST submit 422 as a submitter: %d errors, want DOC's %d", got, len(errs))
		}
	})
}

// sweepApproval is the approval actions' cells: approve, reject, withdraw
// and revise, on an approvable DocType's pending and rejected documents.
func (d *rsDoc) sweepApproval(t *testing.T) {
	e := d.e
	if !d.dt.Approvable {
		d.cell(t, "approval", func(t *testing.T) {
			res, err := d.submitted(t)
			if !d.dt.Submittable {
				d.unsubmittable(t, err, "no pending or rejected document, so no approve, reject, withdraw or revise")
			}
			if err == nil && res.State != doctype.StateApproved {
				t.Fatalf("a submitted %s is %s: it has an approval step now, sweep it", d.dt.Name, res.State)
			}
			t.Skip("not approvable: no pending or rejected document, so no approve, reject, withdraw or revise (checked)")
		})
		return
	}
	gate, err := d.submitted(t)
	if err != nil {
		d.gap(t, "approval", err)
		return
	}
	action := func(user, path string, form url.Values, code int) func(t *testing.T, mode string) rsResp {
		return func(t *testing.T, mode string) rsResp {
			id := d.pending(t)
			if path == "/revise" {
				d.act(t, rsApprover, id, doctype.ActionReject, "probe")
			}
			f := url.Values{"version": {strconv.Itoa(d.version(t, id))}}
			maps.Copy(f, form)
			return e.do(t, mode, user, http.MethodPost, d.url(id)+path, f, true).want(t, "POST "+path, code)
		}
	}
	// The reject page, like every reason page, shows the document's number,
	// party and total: a probe field only when the party is hidden.
	d.cell(t, "approve ok", func(t *testing.T) {
		d.both(t, "POST approve, htmx", "plain", false, action(rsApprover, "/approve", nil, http.StatusOK))
	})
	d.cell(t, "reject GET", func(t *testing.T) {
		d.both(t, "GET reject", "plain", d.partyNone(), func(t *testing.T, mode string) rsResp {
			return e.do(t, mode, rsApprover, http.MethodGet, d.url(gate.ID)+"/reject", nil, false).want(t, "GET reject", http.StatusOK)
		})
	})
	d.cell(t, "reject 422", func(t *testing.T) {
		d.both(t, "POST reject with no reason", "plain", d.partyNone(), action(rsApprover, "/reject", url.Values{"reason": {" "}}, http.StatusUnprocessableEntity))
	})
	for _, a := range []string{"withdraw", "revise"} {
		d.cell(t, a, func(t *testing.T) {
			r := d.both(t, "POST "+a+", htmx", "plain", false, action(rsAdmin, "/"+a, nil, http.StatusOK))
			if !strings.HasSuffix(r.hdr.Get("HX-Push-Url"), "/edit/check") {
				t.Errorf("POST %s pushed %q, want the draft's check page", a, r.hdr.Get("HX-Push-Url"))
			}
		})
	}
}

// party is the field DOC shows as the document's party (doc.partyOf,
// CD-55): the header Link to a master holding the employee, else the first
// Required one.
func (d *rsDoc) party() string {
	out := ""
	for _, f := range d.dt.Fields {
		target, ok := doctype.Lookup(f.Link)
		if f.OnLine || f.Type != doctype.Link || !ok || target.Master == nil {
			continue
		}
		if f.Column == d.dt.EmployeeField && d.dt.EmployeeField != "" {
			return f.Name
		}
		if f.Required && out == "" {
			out = f.Name
		}
	}
	return out
}

// partyNone reports whether the reason page, which shows the document's
// number, party and total, draws no probe field: the probe doesn't hide the
// party, or it has no name to show.
func (d *rsDoc) partyNone() bool {
	p := d.party()
	return p == "" || !d.hide[p] || d.shown[p] == ""
}

// gap is a cell for what DOC refuses to approve from generic values: it
// skips with DOC's refusal, and runs once that refusal is gone.
func (d *rsDoc) gap(t *testing.T, name string, err error) {
	d.cell(t, name, func(t *testing.T) {
		pr, _ := docscreen.Classify(err)
		t.Skipf("no valid %s from the seeder (checked: DOC refuses it: %v %v)", d.dt.Name, err, pr.Fields)
	})
}

// sweepApproved is the cells on approved and cancelled documents.
func (d *rsDoc) sweepApproved(t *testing.T) {
	e := d.e
	ap, err := d.approved(t)
	if !d.dt.Submittable {
		d.cell(t, "approved", func(t *testing.T) {
			d.unsubmittable(t, err, "no approved or cancelled document, so no cancel or amend")
		})
		return
	}
	if err != nil {
		d.gap(t, "approved", err)
		return
	}
	d.cell(t, "page approved", func(t *testing.T) {
		d.both(t, "GET the approved document", "plain", false, func(t *testing.T, mode string) rsResp {
			return e.do(t, mode, rsAdmin, http.MethodGet, d.url(ap), nil, false).want(t, "GET the approved document", http.StatusOK)
		})
	})
	// The reason pages draw a probe field only when the party is hidden.
	// A refused cancel leaves ap approved; a cancel that succeeds takes a new one.
	cancel := func(form url.Values, code int, stale bool, htmx bool) func(t *testing.T, mode string) rsResp {
		return func(t *testing.T, mode string) rsResp {
			id := ap
			if code == http.StatusOK {
				id = d.mustApproved(t)
			}
			f := url.Values{"version": {strconv.Itoa(d.version(t, id))}}
			if stale {
				f.Set("version", d.stale(t, id))
			}
			maps.Copy(f, form)
			r := e.do(t, mode, rsAdmin, http.MethodPost, d.url(id)+"/cancel", f, htmx).want(t, "POST cancel", code)
			if code != http.StatusOK {
				if got := inputValue(r.page, "version"); got != f.Get("version") {
					t.Errorf("POST cancel %d: version = %q, want the posted %q (CD-94)", code, got, f.Get("version"))
				}
			}
			return r
		}
	}
	d.cell(t, "reason 422", func(t *testing.T) {
		d.both(t, "POST cancel with no reason", "plain", d.partyNone(), cancel(url.Values{"reason": {" "}}, http.StatusUnprocessableEntity, false, false))
	})
	d.cell(t, "reason 409", func(t *testing.T) {
		d.both(t, "POST cancel with a stale version", "plain", d.partyNone(), cancel(url.Values{"reason": {"probe"}}, http.StatusConflict, true, false))
	})
	d.cell(t, "cancel ok", func(t *testing.T) {
		d.both(t, "POST cancel, htmx", "plain", false, cancel(url.Values{"reason": {"probe"}}, http.StatusOK, false, true))
	})
	d.cell(t, "amend", func(t *testing.T) {
		r := d.both(t, "POST amend, htmx", "plain", false, func(t *testing.T, mode string) rsResp {
			c := d.cancelled(t)
			return e.do(t, mode, rsAdmin, http.MethodPost, d.url(c)+"/amend", url.Values{"version": {strconv.Itoa(d.version(t, c))}}, true).want(t, "POST amend, htmx", http.StatusOK)
		})
		if !strings.HasSuffix(r.hdr.Get("HX-Push-Url"), "/edit/check") {
			t.Errorf("POST amend pushed %q, want the new draft's check page", r.hdr.Get("HX-Push-Url"))
		}
	})
	d.cell(t, "amend reader", func(t *testing.T) {
		r := d.both(t, "POST amend as an amender", "plain", false, func(t *testing.T, mode string) rsResp {
			c := d.cancelled(t)
			return e.do(t, mode, rsAmender, http.MethodPost, d.url(c)+"/amend", url.Values{"version": {strconv.Itoa(d.version(t, c))}}, true).
				want(t, "POST amend as an amender without write, htmx", http.StatusOK)
		})
		if strings.HasSuffix(r.hdr.Get("HX-Push-Url"), "/edit/check") {
			t.Errorf("POST amend as an amender pushed %q, want the read-only document page", r.hdr.Get("HX-Push-Url"))
		}
	})
}

// unsubmittable checks that err, from submitting a document of a DocType
// that isn't Submittable, is DOC's 403 refusal (auth.Applies, CD-21), then
// skips the cell: what it skips needs a submitted document.
func (d *rsDoc) unsubmittable(t *testing.T, err error, what string) {
	t.Helper()
	if pr, ok := docscreen.Classify(err); !ok || pr.Status != http.StatusForbidden {
		t.Fatalf("submitting a %s, which isn't Submittable: %v, want DOC's 403", d.dt.Name, err)
	}
	t.Skipf("(checked) not Submittable: %s", what)
}

// submitErrors are DOC's errors for submitting a copy of the probe draft;
// the refused submit writes nothing.
func (d *rsDoc) submitErrors(t *testing.T) []doctype.FieldError {
	t.Helper()
	cp := d.draft(t)
	_, err := d.e.w.app.Engine().Do(t.Context(), d.e.who[rsAdmin], d.dt.Name, cp, doc.ActionSubmit, doc.ActionInput{Version: d.version(t, cp)})
	pr, ok := docscreen.Classify(err)
	if !ok || pr.Status != http.StatusUnprocessableEntity || len(pr.Fields) == 0 {
		t.Fatalf("submitting the probe draft: %v, want DOC's field errors", err)
	}
	return pr.Fields
}

// owner is the step owning error e (design-system §8.3), or -1.
func (d *rsDoc) owner(e doctype.FieldError) int {
	for j, s := range d.steps {
		lineErr := e.Line > 0 || e.Field == "lines"
		if (lineErr && s.Lines) || (!lineErr && slices.Contains(s.Fields, e.Field)) {
			return j
		}
	}
	return -1
}

// failing is the first step owning one of errs, else check.
func (d *rsDoc) failing(errs []doctype.FieldError) int {
	i := len(d.steps) - 1
	for _, e := range errs {
		if j := d.owner(e); j >= 0 && j < i {
			i = j
		}
	}
	return i
}

// wantErrors is the summary step i shows for DOC's errs: its own errors and
// unowned ones, an error on a hidden field as a plain message (CD-256 (1));
// for a refusal with none of those left, every error as a plain message.
func (d *rsDoc) wantErrors(errs []doctype.FieldError, i, lines int, refusal bool) []string {
	var out []string
	for _, e := range errs {
		if j := d.owner(e); e.Field != "" && j >= 0 && j != i {
			continue
		}
		switch {
		case e.Field == "" || d.hide[e.Field]:
			out = append(out, "#step-title|"+e.Message)
		case e.Line > 0 || e.Field == "lines":
			out = append(out, d.linesTarget(lines, true)+"|"+e.Message)
		default:
			out = append(out, "#"+components.FieldID(e.Field)+"|"+e.Message)
		}
	}
	if refusal && len(out) == 0 {
		// A refusal always says why: each error's message, with no field.
		for _, e := range errs {
			out = append(out, "#step-title|"+e.Message)
		}
	}
	return out
}

// TestToolingRedraw_TableMatchesCode is the staleness guard: docscreen's
// routes must be rsRoutes, its functions that send a page rsSites, and the
// callers of its page builders rsBuilders. A new or removed redraw path
// fails here until the sweep and its table are updated.
func TestToolingRedraw_TableMatchesCode(t *testing.T) {
	files, err := filepath.Glob("../ui/docscreen/*.go")
	if err != nil {
		t.Fatal(err)
	}
	sites, routes, builders := map[string]int{}, map[string]string{}, map[string]map[string]int{}
	fset := token.NewFileSet()
	scanned := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_templ.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		s, r, b := rsScan(f)
		for fn, n := range s {
			sites[fn] += n
		}
		maps.Copy(routes, r)
		for callee, callers := range b {
			if builders[callee] == nil {
				builders[callee] = map[string]int{}
			}
			for fn, n := range callers {
				builders[callee][fn] += n
			}
		}
	}
	if scanned < 10 || len(routes) < 10 {
		t.Fatalf("scanned %d docscreen files and %d routes, want at least 10 of each", scanned, len(routes))
	}
	// The routes as compiled too, so go test -overlay reaches them.
	r := chi.NewRouter()
	docscreen.Routes(docscreen.Deps{})(r)
	walked := map[string]string{}
	if err := chi.Walk(r, func(method, route string, h http.Handler, _ ...func(http.Handler) http.Handler) error {
		name := runtime.FuncForPC(reflect.ValueOf(h).Pointer()).Name()
		name = strings.TrimSuffix(name[strings.LastIndex(name, ".")+1:], "-fm")
		walked[method+" "+route] = name
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(walked, routes) {
		t.Errorf("docscreen.Routes as compiled %v, as read %v", walked, routes)
	}
	maps.Copy(routes, walked)
	for route, handler := range routes {
		if _, ok := rsRoutes[route]; !ok {
			t.Errorf("docscreen routes %s to %s, which no sweep cell covers: add it to rsRoutes and the sweep", route, handler)
		}
	}
	for route := range rsRoutes {
		if _, ok := routes[route]; !ok {
			t.Errorf("rsRoutes lists %s, which docscreen no longer routes", route)
		}
	}
	for fn, n := range sites {
		if rsSites[fn] != n {
			t.Errorf("docscreen.%s sends %d responses, the sweep's table has %d: add its path to the table and a cell to the sweep", fn, n, rsSites[fn])
		}
	}
	for fn, n := range rsSites {
		if sites[fn] == 0 {
			t.Errorf("the sweep's table lists docscreen.%s (%d), which sends none now: remove or rename it", fn, n)
		}
	}
	for callee, want := range rsBuilders {
		if got := builders[callee]; !maps.Equal(got, want) {
			t.Errorf("callers of docscreen.%s = %v, the sweep's table has %v: a new caller is a new redraw path; add it and a cell", callee, got, want)
		}
	}
}

// rsScan reads one file: by top-level function, its ui.Render,
// wizard.Respond and renderList calls; Routes' routes ("METHOD pattern" to
// handler); and by callee in rsBuilders, its callers' call counts. A call in
// a closure counts for its function.
func rsScan(f *ast.File) (sites map[string]int, routes map[string]string, builders map[string]map[string]int) {
	sites, routes, builders = map[string]int{}, map[string]string{}, map[string]map[string]int{}
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		fn := fd.Name.Name
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var name string
			var x *ast.Ident
			switch fun := c.Fun.(type) {
			case *ast.SelectorExpr:
				name = fun.Sel.Name
				x, _ = fun.X.(*ast.Ident)
			case *ast.Ident:
				name = fun.Name
			}
			switch {
			case x != nil && x.Name == "ui" && name == "Render",
				x != nil && x.Name == "wizard" && name == "Respond",
				name == "renderList":
				sites[fn]++
			}
			// Builders are Deps methods, called as d.name, but gridResponse, a
			// templ function; a bare call of another name is a local closure.
			if _, ok := rsBuilders[name]; ok && ((x != nil && x.Name == "d") || (x == nil && name == "gridResponse")) {
				if builders[name] == nil {
					builders[name] = map[string]int{}
				}
				builders[name][fn]++
			}
			if fn == "Routes" && x != nil && x.Name == "r" && (name == "Get" || name == "Post") && len(c.Args) == 2 {
				lit, _ := c.Args[0].(*ast.BasicLit)
				h, _ := c.Args[1].(*ast.SelectorExpr)
				if lit != nil && h != nil {
					pattern, _ := strconv.Unquote(lit.Value)
					routes[strings.ToUpper(name)+" "+pattern] = h.Sel.Name
				}
			}
			return true
		})
	}
	return sites, routes, builders
}

// The guard's scanner on source written here: response calls in closures
// count for their function, other calls don't; builders count by caller;
// Routes' routes are read with their handlers.
func TestToolingRedraw_GuardCounts(t *testing.T) {
	const src = `package p

func a() { ui.Render(w, r, 200, x, y); f := func() { wizard.Respond(w, r, o, "", nil) }; f(); d.refused(w, r, dt, id, pr) }
func b() { d.renderList(w, r, dt, "", nil); ui.Other(); render(); gridResponse(g); x.refused(); refused(pr) }
func c() { web.Render() }
func Routes(d Deps) func(chi.Router) { return func(r chi.Router) { r.Get("/x", d.xGet); r.Post("/x/{id}", d.xPost); r.Use(m) } }
`
	f, err := parser.ParseFile(token.NewFileSet(), "p.go", src, 0)
	if err != nil {
		t.Fatal(err)
	}
	sites, routes, builders := rsScan(f)
	if want := map[string]int{"a": 2, "b": 1}; !maps.Equal(sites, want) {
		t.Errorf("sites %v, want %v", sites, want)
	}
	if want := map[string]string{"GET /x": "xGet", "POST /x/{id}": "xPost"}; !maps.Equal(routes, want) {
		t.Errorf("routes %v, want %v", routes, want)
	}
	if got := builders["refused"]; !maps.Equal(got, map[string]int{"a": 1}) {
		t.Errorf("refused's callers %v, want a once (x.refused and a bare refused are not docscreen's)", got)
	}
	if got := builders["gridResponse"]; !maps.Equal(got, map[string]int{"b": 1}) {
		t.Errorf("gridResponse's callers %v, want b once", got)
	}
}
