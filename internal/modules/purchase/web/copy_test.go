package purchaseweb

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// p12AUTH099 is AUTH-099's text for a copier whose sites are PUNE-WH and
// METRO-SITE (CD-149).
const p12AUTH099 = "You can only use Sites METRO-SITE, PUNE-WH. Choose one of your allowed sites."

// p12Flash is the Copy flash for the new draft and the source's display (UX
// "After Copy or Create PO", approved in CD-258).
func p12Flash(draft int64, display string) string {
	return "Draft " + strconv.FormatInt(draft, 10) + " is a copy of " + display + ". Check it before you submit."
}

// p12Web mounts the module routes over svc on a bare router (AUTH's guard and
// CSRF are internal/app's) inside a session, as AUTH's middleware runs them
// (web.WithSessions, CD-150), plus a probe route that answers the session's
// flash as JSON.
type p12Web struct {
	env *purchasetest.Env
	h   http.Handler
}

func newP12Web(t *testing.T, env *purchasetest.Env, svc *purchase.Service) *p12Web {
	t.Helper()
	sm := scs.New()
	r := chi.NewRouter()
	pd := Deps{
		Service: svc, Approvals: env.Approvals, Comments: env.Comments, Sessions: sm,
		CSRF: func(*http.Request) string { return "p12-token" },
	}
	pd.ListHidden = docscreen.Deps{DB: env.DB, Modules: Modules(pd)}.ListHidden
	Routes(pd)(r)
	r.Get("/p12-flash", func(w http.ResponseWriter, req *http.Request) {
		_ = json.NewEncoder(w).Encode(web.TakeFlash(req.Context()))
	})
	h := sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.ServeHTTP(w, req.WithContext(web.WithSessions(req.Context(), sm)))
	}))
	return &p12Web{env: env, h: h}
}

// p12Answer is one POST's response and the flash the next page would show.
type p12Answer struct {
	status   int
	location string
	body     string // the body's text
	flash    []web.Notice
}

// post copies path's PO as p, then reads the flash with the response's
// session cookie, so a flash put after the response started is lost here as
// it is in production.
func (w *p12Web) post(t *testing.T, p auth.Principal, path string) p12Answer {
	t.Helper()
	req := httptest.NewRequestWithContext(auth.WithPrincipal(context.Background(), p), http.MethodPost, path, strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	w.h.ServeHTTP(rec, req)
	d, err := goquery.NewDocumentFromReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("POST %s: parse body: %v", path, err)
	}
	a := p12Answer{status: rec.Code, location: rec.Header().Get("Location"), body: strings.Join(strings.Fields(d.Text()), " ")}

	probe := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/p12-flash", nil)
	for _, c := range rec.Result().Cookies() {
		probe.AddCookie(c)
	}
	pr := httptest.NewRecorder()
	w.h.ServeHTTP(pr, probe)
	if err := json.Unmarshal(pr.Body.Bytes(), &a.flash); err != nil {
		t.Fatalf("read the flash after POST %s: %v (%q)", path, err, pr.Body.String())
	}
	return a
}

// orders is the number of purchase orders and the highest id.
func (w *p12Web) orders(t *testing.T) (n int, maxID int64) {
	t.Helper()
	if err := w.env.DB.SQL().QueryRowContext(t.Context(), `SELECT count(*), COALESCE(max(id), 0) FROM purchase_order`).Scan(&n, &maxID); err != nil {
		t.Fatalf("count purchase_order: %v", err)
	}
	return n, maxID
}

// p12ID is the id, as a form value, of the master row with code in table.
func p12ID(t *testing.T, env *purchasetest.Env, table, code string) string {
	t.Helper()
	var n int64
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT id FROM `+table+` WHERE code = ?`, code).Scan(&n); err != nil { //nolint:gosec // G202: fixed table names
		t.Fatalf("id of %s %s: %v", table, code, err)
	}
	return strconv.FormatInt(n, 10)
}

// p12ArjunTech is an approved TECH-SITE / PRJ-TECHPARK PO by Arjun through
// Engine.Do, which Priya (PRJ-METRO only) can't read.
func p12ArjunTech(t *testing.T, env *purchasetest.Env) int64 {
	t.Helper()
	ctx := t.Context()
	res, err := env.Engine.Do(ctx, env.Arjun, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"vendor_id": p12ID(t, env, "party", "SHREE"), "site_id": p12ID(t, env, "site", "TECH-SITE"),
			"project_id": p12ID(t, env, "project", "PRJ-TECHPARK"), "order_date": types.Today(env.Clock).ISO(),
		},
		Lines: []map[string]string{{
			"item_id": p12ID(t, env, "item", "MS-ANGLE-50"), "uom_id": p12ID(t, env, "uom", "KGS"),
			"qty": "10", "price": "62.5", "tax_code_id": p12ID(t, env, "tax_code", "GST18"),
		}},
	})
	if err != nil {
		t.Fatalf("save PRJ-TECHPARK PO as Arjun: %v", err)
	}
	res, err = env.Engine.Do(ctx, env.Arjun, purchase.DocType, res.ID, doctype.ActionSubmit, doc.ActionInput{Version: res.Version})
	if err != nil || res.State != doctype.StateApproved {
		t.Fatalf("submit PRJ-TECHPARK PO as Arjun = %s, %v; want approved", res.State, err)
	}
	return res.ID
}

func p12Path(id string) string { return "/modules/purchase/orders/" + id + "/copy" }

func p12PO(id int64) string { return "/doc/" + purchase.DocType + "/" + strconv.FormatInt(id, 10) }

// p12Danger reports whether a flash level is drawn as a Danger banner
// (layout's flashKind: anything but success, info and warning).
func p12Danger(level string) bool {
	return level != "success" && level != "info" && level != "warning"
}

// PO-064 handler (PO spec §24.1, plan P12 Do 4; UX "After Copy or Create PO"
// and "Copy refused"; CD-245, CD-258): POST …/copy creates the copy and
// redirects (303) to its Details step with the copy flash. A restricted
// copier's refusal goes back to the PO page with AUTH-099's text as a danger
// flash and creates nothing. AUTH's 404 and 403 use the error page, with no
// flash and nothing created.
func TestPurchaseWeb_CopyPost(t *testing.T) {
	env := purchasetest.New(t)
	w := newP12Web(t, env, env.Service)
	po := purchasetest.NewOrder(t, env)
	hidden := p12ArjunTech(t, env)

	t.Run("copy redirects to the draft's Details with the flash", func(t *testing.T) {
		n, _ := w.orders(t)
		a := w.post(t, env.Arjun, p12Path(strconv.FormatInt(po.ID, 10)))
		after, newID := w.orders(t)
		if after != n+1 {
			t.Fatalf("orders after Copy = %d, want %d (%d %q)", after, n+1, a.status, a.body)
		}
		if want := p12PO(newID) + "/edit/details"; a.status != http.StatusSeeOther || a.location != want {
			t.Errorf("Copy = %d Location %q (%q), want 303 to %s", a.status, a.location, a.body, want)
		}
		var owner int64
		if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT created_by FROM purchase_order WHERE id = ?`, newID).Scan(&owner); err != nil {
			t.Fatal(err)
		}
		if owner != int64(env.Arjun.ID) {
			t.Errorf("copy created_by %d, want Arjun %d", owner, env.Arjun.ID)
		}
		if want := p12Flash(newID, po.Number); len(a.flash) != 1 || a.flash[0].Text != want || p12Danger(a.flash[0].Level) {
			t.Errorf("flash = %+v, want one non-danger %q", a.flash, want)
		}
	})

	deniedCreate := (&auth.DeniedError{Right: auth.RightCreate, Label: "Purchase Order", Plural: "Purchase Orders"}).Error()
	for _, c := range []struct {
		name   string
		p      auth.Principal
		id     string
		status int
		text   string
	}{
		{"a missing PO", env.Arjun, "999999", http.StatusNotFound, auth.MsgNotFound},
		{"a PO Priya can't read", env.Priya, strconv.FormatInt(hidden, 10), http.StatusNotFound, auth.MsgNotFound},
		{"an id that isn't a number", env.Arjun, "abc", http.StatusNotFound, auth.MsgNotFound},
		{"Ravi has no create", env.Ravi, strconv.FormatInt(po.ID, 10), http.StatusForbidden, deniedCreate},
	} {
		t.Run(c.name, func(t *testing.T) {
			n, _ := w.orders(t)
			a := w.post(t, c.p, p12Path(c.id))
			if a.status != c.status || !strings.Contains(a.body, c.text) {
				t.Errorf("Copy = %d %q, want %d with %q", a.status, a.body, c.status, c.text)
			}
			if len(a.flash) != 0 {
				t.Errorf("flash = %+v, want none (the error page)", a.flash)
			}
			if after, _ := w.orders(t); after != n {
				t.Errorf("orders after the refusal = %d, want %d", after, n)
			}
		})
	}

	// Last: it narrows Arjun's sites.
	t.Run("a restricted copier goes back to the PO with AUTH-099's text", func(t *testing.T) {
		src := p12ArjunTech(t, env)
		hook := &purchasetest.CreateHook{Engine: env.Engine}
		hook.Before = func(ctx context.Context, p auth.Principal) {
			ids := []int64{}
			for _, code := range []string{"PUNE-WH", "METRO-SITE"} {
				id, _ := strconv.ParseInt(p12ID(t, env, "site", code), 10, 64)
				ids = append(ids, id)
			}
			if err := env.Auth.ChangeRestrictions(ctx, env.Admin, p.ID, auth.DimSite, ids, nil, nil); err != nil {
				t.Fatalf("restrict %s: %v", p.Name, err)
			}
		}
		rw := newP12Web(t, env, env.ServiceWith(hook))
		n, _ := rw.orders(t)
		a := rw.post(t, env.Arjun, p12Path(strconv.FormatInt(src, 10)))
		if a.status != http.StatusSeeOther || a.location != p12PO(src) {
			t.Errorf("restricted Copy = %d Location %q (%q), want 303 to %s", a.status, a.location, a.body, p12PO(src))
		}
		if len(a.flash) != 1 || a.flash[0].Text != p12AUTH099 || !p12Danger(a.flash[0].Level) {
			t.Errorf("flash = %+v, want one danger %q", a.flash, p12AUTH099)
		}
		if after, _ := rw.orders(t); after != n {
			t.Errorf("orders after the refusal = %d, want %d", after, n)
		}
	})
}

// DOC's disabled-link texts for the default PO's project and site (kernel/doc
// msgLinkDisabled).
const (
	p12MsgProjectOff = "Project PRJ-METRO Pune Metro Depot is disabled. Pick an active project, or ask an admin to enable it."
	p12MsgSiteOff    = "Deliver to METRO-SITE METRO-SITE is disabled. Pick an active deliver to, or ask an admin to enable it."
)

// p12Disable disables the site or project with code through its service.
func p12Disable(t *testing.T, env *purchasetest.Env, table, code string) {
	t.Helper()
	kit := mstkit.Env{DB: env.DB, Auth: env.Auth, Engine: env.Engine, Auditor: audit.Default, Clock: env.Clock}
	var svc interface {
		SetDisabledTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, disabled bool) error
	}
	switch table {
	case "site":
		svc = site.New(kit)
	case "project":
		svc = project.New(kit)
	default:
		t.Fatalf("p12Disable: no service for %s", table)
	}
	ctx := t.Context()
	id, _ := strconv.ParseInt(p12ID(t, env, table, code), 10, 64)
	if err := env.DB.Write(ctx, func(tx *db.Tx) error {
		var version int
		if err := tx.SQL().QueryRowContext(ctx, `SELECT version FROM `+table+` WHERE id = ?`, id).Scan(&version); err != nil { //nolint:gosec // G202: fixed table names
			return err
		}
		return svc.SetDisabledTx(ctx, tx, env.Admin, id, version, true)
	}); err != nil {
		t.Fatalf("disable %s %s: %v", table, code, err)
	}
}

// PO-064 handler, plan P12 fix round 1 (AD-13 ruling (a) and its test-4
// ruling): a disabled site or project isn't pre-filtered, so create refuses it
// with DOC's disabled text. That refusal is UX §5's "Copy refused": a 303 back
// to the PO page with one danger flash holding DOC's field message(s), joined
// with a space, and nothing created.
func TestPurchaseWeb_CopyPostDisabledSiteProject(t *testing.T) {
	for _, c := range []struct {
		name    string
		disable [][2]string // table, code
		text    string
	}{
		{"disabled project", [][2]string{{"project", "PRJ-METRO"}}, p12MsgProjectOff},
		{"disabled site", [][2]string{{"site", "METRO-SITE"}}, p12MsgSiteOff},
		// Both: one flash, DOC's field errors in its order (site first),
		// joined by a space.
		{"disabled site and project", [][2]string{{"site", "METRO-SITE"}, {"project", "PRJ-METRO"}}, p12MsgSiteOff + " " + p12MsgProjectOff},
	} {
		t.Run(c.name, func(t *testing.T) {
			env := purchasetest.New(t)
			w := newP12Web(t, env, env.Service)
			po := purchasetest.NewOrder(t, env) // METRO-SITE / PRJ-METRO
			for _, d := range c.disable {
				p12Disable(t, env, d[0], d[1])
			}
			n, _ := w.orders(t)

			a := w.post(t, env.Arjun, p12Path(strconv.FormatInt(po.ID, 10)))

			if a.status != http.StatusSeeOther || a.location != p12PO(po.ID) {
				t.Errorf("Copy = %d Location %q (%q), want 303 to %s", a.status, a.location, a.body, p12PO(po.ID))
			}
			if len(a.flash) != 1 || a.flash[0].Text != c.text || !p12Danger(a.flash[0].Level) {
				t.Errorf("flash = %+v, want one danger %q", a.flash, c.text)
			}
			if after, _ := w.orders(t); after != n {
				t.Errorf("orders after the refusal = %d, want %d", after, n)
			}
		})
	}
}
