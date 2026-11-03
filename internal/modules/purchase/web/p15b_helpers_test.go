package purchaseweb

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// Helpers for P15b's tests (Order document page and close page; UX §5.9,
// §5.11). All names carry p15b.

// p15bGRN is a test-only goods receipt on the real goods_receipt tables (the
// shared migrations create them in every test database), so the PO page's
// Goods receipts card reads doc.LinkedBlockers and Hooks.Date as it will with
// MOD-GRN, which purchase may not import (PO spec §14.5). Its site and
// project restrict who reads it on the real auth.Service; Stores may create
// it, which the test ReceiveLink asks (CD-283).
const p15bGRN = "p15btest.Receipt"

func init() {
	doctype.Register(doctype.DocType{
		Name: p15bGRN, Label: "Goods Receipt", PluralLabel: "Goods Receipts",
		Table: "goods_receipt", LinesTable: "goods_receipt_line", Series: "TG/{FY}/{#####}",
		TestOnly: true, Submittable: true, OwnerField: "created_by",
		ProjectFields: []string{"project_id"},
		SiteFields:    []string{"site_id"},
		DefaultRights: map[string]map[string]string{
			"Stores":           {"read": "all", "create": "all", "write": "all", "submit": "all"},
			"Purchase User":    {"read": "all"},
			"Purchase Manager": {"read": "all"},
		},
		Fields: []doctype.Field{
			{Name: "purchase_order_id", Label: "Purchase order", Type: doctype.Link, Link: purchase.DocType, Required: true},
			{Name: "site_id", Label: "Site", Type: doctype.Int},
			{Name: "project_id", Label: "Project", Type: doctype.Int},
			{Name: "receipt_date", Label: "Receipt date", Type: doctype.Date, Required: true},
			{Name: "received_qty", Label: "Received", Type: doctype.Qty, OnLine: true},
		},
		Links: []doctype.LinkSpec{{Field: "purchase_order_id", Target: purchase.DocType}},
		Hooks: p15bGRNHooks{},
	})
}

// p15bGRNHooks checks nothing; Date is the receipt date, as MOD-GRN's is.
type p15bGRNHooks struct{}

func (p15bGRNHooks) Validate(context.Context, *db.Tx, int64) []doctype.FieldError { return nil }
func (p15bGRNHooks) BeforeSubmit(context.Context, *db.Tx, int64) error            { return nil }
func (p15bGRNHooks) ApplyEffects(context.Context, *db.Tx, int64) error            { return nil }
func (p15bGRNHooks) ReverseEffects(context.Context, *db.Tx, int64) error          { return nil }
func (p15bGRNHooks) Amount(context.Context, *db.Tx, int64) (types.Money, error)   { return 0, nil }
func (p15bGRNHooks) Date(ctx context.Context, tx *db.Tx, id int64) (types.Date, error) {
	var iso string
	if err := tx.SQL().QueryRowContext(ctx, `SELECT receipt_date FROM goods_receipt WHERE id = ?`, id).Scan(&iso); err != nil {
		return types.Date{}, err
	}
	return types.DateFromISO(iso)
}

// p15bReceiveLink is PUX-038's test hook: ok only for users holding create
// on p15bGRN, with the CD-283 link.
func p15bReceiveLink(a auth.Service) func(context.Context, auth.Principal, int64) (string, bool) {
	return func(ctx context.Context, p auth.Principal, poID int64) (string, bool) {
		if a.Can(ctx, p, p15bGRN, auth.RightCreate, nil) != nil {
			return "", false
		}
		return p15bReceiveHref(poID), true
	}
}

func p15bReceiveHref(poID int64) string {
	return "/doc/goodsreceipt.Receipt/new?po=" + strconv.FormatInt(poID, 10)
}

// newP15bWeb is P15d's web world (the real AUTH, docscreen's routes with
// purchase's Modules and the module routes) with receive as Deps.ReceiveLink
// (nil: no hook), and p15bGRN's series.
func newP15bWeb(t *testing.T, receive bool) *p15dWeb {
	t.Helper()
	env := purchasetest.New(t)
	dt, _ := doctype.Lookup(p15bGRN)
	if err := naming.New(env.DB, env.Clock, env.Auth, audit.Default).EnsureSeries(t.Context(), dt.Name, dt.Label, dt.Series); err != nil {
		t.Fatalf("EnsureSeries(%s): %v", p15bGRN, err)
	}
	sm := scs.New()
	d := Deps{
		Service: env.Service, Approvals: env.Approvals, Comments: env.Comments, Sessions: sm,
		CSRF: func(*http.Request) string { return "p15b-token" },
	}
	if receive {
		d.ReceiveLink = p15bReceiveLink(env.Auth)
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
	d.ListHidden = docscreen.Deps{DB: env.DB, Modules: Modules(d)}.ListHidden
	Routes(d)(r)
	r.Get("/p15d-flash", func(w http.ResponseWriter, req *http.Request) {
		_ = json.NewEncoder(w).Encode(web.TakeFlash(req.Context()))
	})
	h := sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.ServeHTTP(w, req.WithContext(web.WithSessions(req.Context(), sm)))
	}))
	return &p15dWeb{env: env, h: h}
}

// p15bStates are the six PO states the page's guards are tested in.
var p15bStates = []doctype.State{
	doctype.StateDraft, doctype.StatePending, doctype.StateRejected,
	doctype.StateApproved, doctype.StateClosed, doctype.StateCancelled,
}

// order is a PO by Priya in state s, built through Engine.Do: the default
// line, or 1,000 kg (₹62,500, so Purchase Manager's rule applies) when s
// needs approval to wait. Rejected is Ravi's reject, closed Priya's close and
// cancelled Ravi's cancel, each of an approved PO.
func (w *p15dWeb) order(t *testing.T, s doctype.State, opts ...purchasetest.OrderOpt) doc.Doc {
	t.Helper()
	env := w.env
	switch s {
	case doctype.StateDraft:
		return purchasetest.NewOrder(t, env, append(opts, purchasetest.StopAtDraft())...)
	case doctype.StatePending, doctype.StateRejected:
		po := purchasetest.NewOrder(t, env, append([]purchasetest.OrderOpt{purchasetest.OrderLines(map[string]string{"qty": "1000"})}, append(opts, purchasetest.StopAtPending())...)...)
		if s == doctype.StateRejected {
			w.act(t, env.Ravi, purchase.DocType, po.ID, doctype.ActionReject, "Rates too high")
		}
		return po
	}
	po := purchasetest.NewOrder(t, env, opts...)
	switch s {
	case doctype.StateClosed:
		w.act(t, env.Priya, purchase.DocType, po.ID, doctype.ActionClose, "Vendor stopped supplying")
	case doctype.StateCancelled:
		w.act(t, env.Ravi, purchase.DocType, po.ID, doctype.ActionCancel, "Ordered by mistake")
	}
	return po
}

// line is a PO line of qty of item in unit uom, with hsn and the rest of
// extra (already_received, description).
func (w *p15dWeb) line(t *testing.T, item, uom, hsn, qty string, extra map[string]string) map[string]string {
	t.Helper()
	m := map[string]string{
		"item_id": w.id(t, "item", item), "uom_id": w.id(t, "uom", uom), "qty": qty, "price": "350",
		"tax_code_id": w.id(t, "tax_code", "GST18"), "hsn_sac": hsn,
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

// cement is one line of qty Bag of CEMENT-OPC.
func (w *p15dWeb) cement(t *testing.T, qty string, extra map[string]string) map[string]string {
	t.Helper()
	return w.line(t, "CEMENT-OPC", "BAG", "2523", qty, extra)
}

// grn is a p15bGRN against PO poID on site code siteCode (PRJ-METRO),
// received on date (dd-mm-yyyy), saved and submitted (approved) by the
// Administrator through Engine.Do.
func (w *p15dWeb) grn(t *testing.T, poID int64, siteCode, date string) doc.Doc {
	t.Helper()
	ctx, env := t.Context(), w.env
	on, err := types.ParseDate(date)
	if err != nil {
		t.Fatal(err)
	}
	res, err := env.Engine.Do(ctx, env.Admin, p15bGRN, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"purchase_order_id": strconv.FormatInt(poID, 10), "site_id": w.id(t, "site", siteCode),
			"project_id": w.id(t, "project", "PRJ-METRO"), "receipt_date": on.ISO(),
		},
		Lines: []map[string]string{{"received_qty": "5"}},
	})
	if err != nil {
		t.Fatalf("save GRN: %v", err)
	}
	if res, err = env.Engine.Do(ctx, env.Admin, p15bGRN, res.ID, doctype.ActionSubmit, doc.ActionInput{Version: res.Version}); err != nil || res.State != doctype.StateApproved {
		t.Fatalf("submit GRN %d = %s, %v; want approved", res.ID, res.State, err)
	}
	var out doc.Doc
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		out, err = doc.LoadDoc(ctx, tx, p15bGRN, res.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// user is a new user whose only role holds read and order on
// purchase.Order, read on purchase.Request and grn on p15bGRN (lessons §5:
// a rights test needs a user holding only the right the rule names).
func (w *p15dWeb) user(t *testing.T, name string, order []auth.Right, grn ...auth.Right) auth.Principal {
	t.Helper()
	ctx, env := t.Context(), w.env
	role := "P15b " + name
	if err := env.Auth.CreateRole(ctx, env.Admin, role); err != nil {
		t.Fatalf("CreateRole(%s): %v", role, err)
	}
	set := func(dt string, rights []auth.Right) {
		m := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}
		for _, r := range rights {
			m[r] = auth.ScopeAll
		}
		if err := env.Auth.SetRights(ctx, env.Admin, role, dt, m); err != nil {
			t.Fatalf("SetRights(%s, %s): %v", role, dt, err)
		}
	}
	set(purchase.DocType, order)
	set(purchase.RequestDocType, nil)
	if len(grn) > 0 {
		set(p15bGRN, grn)
	}
	id, _, err := env.Auth.CreateUser(ctx, env.Admin, auth.NewUser{Email: "p15b-" + name + "@vardhan.test", Name: role, Password: "Correct-Horse-42", Roles: []string{role}})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", role, err)
	}
	p, _, err := env.Auth.LoadPrincipal(ctx, id)
	if err != nil {
		t.Fatalf("LoadPrincipal(%s): %v", role, err)
	}
	return p
}

// page is document page of PO id as p, which must answer 200.
func (w *p15dWeb) page(t *testing.T, p auth.Principal, id int64) p15dPage {
	t.Helper()
	pg := w.get(t, p, p15dDoc(purchase.DocType, id))
	if pg.status != http.StatusOK {
		t.Fatalf("%s GET PO %d = %d: %s", p.Name, id, pg.status, pg.text())
	}
	return pg
}

// markSent records PO id as sent by p on on (dd-mm-yyyy) by via with note.
func (w *p15dWeb) markSent(t *testing.T, p auth.Principal, id int64, on, via, note string) {
	t.Helper()
	d, err := types.ParseDate(on)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.env.Service.MarkSent(t.Context(), p, id, 0, purchase.SentInput{SentOn: d, SentVia: via, Note: note}); err != nil {
		t.Fatalf("MarkSent(%d): %v", id, err)
	}
}

// setExpected records PO id as expected on on (dd-mm-yyyy) with note, as p
// at the clock's now.
func (w *p15dWeb) setExpected(t *testing.T, p auth.Principal, id int64, on, note string) {
	t.Helper()
	d, err := types.ParseDate(on)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.env.Service.SetExpected(t.Context(), p, id, 0, d, note); err != nil {
		t.Fatalf("SetExpected(%d): %v", id, err)
	}
}

// more are the labels under More actions.
func (p p15dPage) more() []string { return p.find(".more-actions li") }

// primary is the bar's primary button label and href, "" when none.
func (p p15dPage) primary() (label, href string) {
	s := p.doc.Find(".actionbar a").First()
	href, _ = s.Attr("href")
	return p15dCollapse(s.Text()), href
}

// cardLink is the href of the card titled title's Change link and its
// label, "" when there is none.
func (p p15dPage) cardLink(title string) (label, href string) {
	p.doc.Find("section.card").Each(func(_ int, s *goquery.Selection) {
		if label == "" && p15dCollapse(s.Find("h2.card-title").First().Text()) == title {
			a := s.Find(".card-head > a").First()
			href, _ = a.Attr("href")
			label = strings.TrimSuffix(p15dCollapse(a.Text()), " "+title)
		}
	})
	return label, href
}

// cards are the titles of the page's cards, in order.
func (p p15dPage) cards() []string { return p.find("section.card h2.card-title") }

// hasCard reports whether the page has a card titled title.
func (p p15dPage) hasCard(title string) bool { return slices.Contains(p.cards(), title) }

// pair is the value of the Details pair labelled label, ok false when absent.
func (p p15dPage) pair(label string) (string, bool) {
	var v string
	var ok bool
	p.doc.Find("dt").Each(func(_ int, s *goquery.Selection) {
		if !ok && p15dCollapse(s.Text()) == label {
			v, ok = p15dCollapse(s.Next().Text()), true
		}
	})
	return v, ok
}

// p15bAt is 31-10-2026 at hh:mm UTC.
func p15bAt(hh, mm int) time.Time { return time.Date(2026, 10, 31, hh, mm, 0, 0, time.UTC) }
