package purchaseweb

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/web"
)

// p15eWeb mounts the module routes on a bare router inside a session, as
// AUTH's middleware runs them (web.WithSessions, CD-150), plus a probe that
// answers the session's flash as JSON.
type p15eWeb struct {
	env *purchasetest.Env
	h   http.Handler
}

func newP15eWeb(t *testing.T, env *purchasetest.Env) *p15eWeb {
	t.Helper()
	sm := scs.New()
	r := chi.NewRouter()
	Routes(Deps{
		Service: env.Service, Approvals: env.Approvals, Comments: env.Comments, Sessions: sm,
		CSRF: func(*http.Request) string { return "p15e-token" },
	})(r)
	r.Get("/p15e-flash", func(w http.ResponseWriter, req *http.Request) {
		_ = json.NewEncoder(w).Encode(web.TakeFlash(req.Context()))
	})
	h := sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.ServeHTTP(w, req.WithContext(web.WithSessions(req.Context(), sm)))
	}))
	return &p15eWeb{env: env, h: h}
}

// p15ePage is one response: its status, Location, parsed body and the body's
// text with spaces collapsed.
type p15ePage struct {
	status   int
	location string
	doc      *goquery.Document
	text     string
	flash    []web.Notice // after a POST: the flash the next page would show
}

func (w *p15eWeb) serve(t *testing.T, req *http.Request) (p15ePage, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	w.h.ServeHTTP(rec, req)
	d, err := goquery.NewDocumentFromReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("%s %s: parse body: %v", req.Method, req.URL, err)
	}
	return p15ePage{status: rec.Code, location: rec.Header().Get("Location"), doc: d, text: squash(d.Text())}, rec
}

// get is GET path as p.
func (w *p15eWeb) get(t *testing.T, p auth.Principal, path string) p15ePage {
	t.Helper()
	req := httptest.NewRequestWithContext(auth.WithPrincipal(context.Background(), p), http.MethodGet, path, nil)
	page, _ := w.serve(t, req)
	return page
}

// post is POST path with form as p, then reads the flash with the response's
// session cookie, so a flash put after the response started is lost here as
// it is in production.
func (w *p15eWeb) post(t *testing.T, p auth.Principal, path string, form url.Values) p15ePage {
	t.Helper()
	req := httptest.NewRequestWithContext(auth.WithPrincipal(context.Background(), p), http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	page, rec := w.serve(t, req)
	probe := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/p15e-flash", nil)
	for _, c := range rec.Result().Cookies() {
		probe.AddCookie(c)
	}
	pr := httptest.NewRecorder()
	w.h.ServeHTTP(pr, probe)
	if err := json.Unmarshal(pr.Body.Bytes(), &page.flash); err != nil {
		t.Fatalf("read the flash after POST %s: %v (%q)", path, err, pr.Body.String())
	}
	return page
}

func squash(s string) string { return strings.Join(strings.Fields(s), " ") }

// texts is the collapsed text of each node sel matches in p.
func (p p15ePage) texts(sel string) []string {
	var out []string
	p.doc.Find(sel).Each(func(_ int, s *goquery.Selection) { out = append(out, squash(s.Text())) })
	return out
}

// attr is the attribute of the first node sel matches, ok false when none.
func (p p15ePage) attr(sel, name string) (string, bool) {
	return p.doc.Find(sel).First().Attr(name)
}

// p15eUser creates a user holding exactly rights (by DocType) through a new
// role, on the real auth.Service, and returns the principal.
func p15eUser(t *testing.T, env *purchasetest.Env, name string, rights map[string][]auth.Right) auth.Principal {
	t.Helper()
	ctx := context.Background()
	role := name + " role"
	if err := env.Auth.CreateRole(ctx, env.Admin, role); err != nil {
		t.Fatalf("CreateRole(%s): %v", role, err)
	}
	for dt, rs := range rights {
		m := map[auth.Right]auth.Scope{}
		for _, r := range rs {
			m[r] = auth.ScopeAll
		}
		if err := env.Auth.SetRights(ctx, env.Admin, role, dt, m); err != nil {
			t.Fatalf("SetRights(%s, %s): %v", role, dt, err)
		}
	}
	email := strings.ToLower(strings.ReplaceAll(name, " ", ".")) + "@vardhan.test"
	id, _, err := env.Auth.CreateUser(ctx, env.Admin, auth.NewUser{Email: email, Name: name, Password: "Correct-Horse-42", Roles: []string{role}})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", name, err)
	}
	p, _, err := env.Auth.LoadPrincipal(ctx, id)
	if err != nil {
		t.Fatalf("LoadPrincipal(%s): %v", name, err)
	}
	return p
}

// p15eRuleEnv is an env with an approval rule from ₹0 for Purchase Manager,
// so a submitted PO waits for approval.
func p15eRuleEnv(t *testing.T) *purchasetest.Env {
	t.Helper()
	env := purchasetest.New(t)
	if _, err := env.Approvals.CreateRule(t.Context(), env.Admin, approval.RuleInput{
		DocType: purchase.DocType, FromAmount: 0, ApproverType: approval.ApproverRole, Role: "Purchase Manager",
	}); err != nil {
		t.Fatalf("setup: rule from 0: %v", err)
	}
	return env
}
