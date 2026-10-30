package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/app"
	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/cli"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui"
)

const modulePath = "github.com/vardhan/internaltools"

// Review Focus 2: until the first admin exists everything but /setup, the
// static files and /healthz goes to /setup.
func TestApp_FreshInstallRedirectsToSetup(t *testing.T) {
	w := newWorld(t)
	b := w.browser()

	for _, path := range []string{"/", "/signin", "/approvals", orderPath, "/setup/company", "/no-such-page"} {
		if r := b.get(path); r.status != http.StatusSeeOther || r.location() != "/setup" {
			t.Errorf("GET %s on an empty data dir = %d to %q, want 303 to /setup", path, r.status, r.location())
		}
	}
	if r := b.post("/signin", url.Values{"email": {adminEmail}}); r.status != http.StatusSeeOther || r.location() != "/setup" {
		t.Errorf("POST /signin on an empty data dir = %d to %q, want 303 to /setup", r.status, r.location())
	}
	for _, path := range []string{"/setup", "/healthz", ui.CSSURL} {
		if r := b.get(path); r.status != http.StatusOK {
			t.Errorf("GET %s on an empty data dir = %d, want 200", path, r.status)
		}
	}

	// The real first-run form creates the admin; after it CD-17 applies.
	b.csrf = b.pageCSRF("/setup")
	r := b.post("/setup", url.Values{"email": {adminEmail}, "name": {"Anil Kapoor"}, "password": {testPassword}})
	if r.status != http.StatusSeeOther || r.location() != "/signin" {
		t.Fatalf("POST /setup = %d to %q (%q), want 303 to /signin", r.status, r.location(), r.pageText())
	}
	if r := b.get("/"); r.status != http.StatusSeeOther || r.location() != "/signin?next=%2F" {
		t.Errorf("GET / once a user exists = %d to %q, want 303 to /signin?next=%%2F", r.status, r.location())
	}
	if r := b.get("/setup"); r.status != http.StatusNotFound {
		t.Errorf("GET /setup once a user exists = %d, want 404", r.status)
	}
}

// CD-17, CD-63, CD-91. The plan quotes the session-expired text for every
// signed-out case; AUTH's guest() answers "Sign in to continue." when no
// cookie was presented and the session-expired text when one was, so both are
// pinned (the code wins, .work/wp3-t2-decisions.md).
func TestApp_AllRoutesRequireSignIn(t *testing.T) {
	w := newWorld(t)
	w.firstAdmin()
	b := w.browser()

	fill := func(param string) string {
		if strings.EqualFold(param, "doctype") {
			return fixture.Order
		}
		return "1"
	}

	checked := 0
	err := chi.Walk(w.routes(), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if isPublic(route) {
			return nil
		}
		checked++
		path := fillRoute(route, fill)
		loc := "/signin?next=" + url.QueryEscape(path)

		for _, c := range []struct {
			name, msg string
			cookie    string
		}{
			{"no session", msgSignInRequired, ""},
			{"stale session", msgSessionExpired, "session=stale-session-token"},
		} {
			hdr := map[string]string{}
			if c.cookie != "" {
				hdr["Cookie"] = c.cookie
			}
			if method != http.MethodGet {
				hdr["Origin"] = w.srv.URL
				r := b.do(method, path, nil, "", hdr)
				if r.status != http.StatusUnauthorized {
					t.Errorf("%s %s (%s) = %d, want 401", method, route, c.name, r.status)
				}
				if method != http.MethodHead && r.alert() != c.msg {
					t.Errorf("%s %s (%s) text = %q, want %q", method, route, c.name, r.alert(), c.msg)
				}
				continue
			}
			r := b.do(method, path, nil, "", hdr)
			if r.status != http.StatusSeeOther || r.location() != loc {
				t.Errorf("GET %s (%s) = %d to %q, want 303 to %q", route, c.name, r.status, r.location(), loc)
			}
			hdr["HX-Request"] = "true"
			r = b.do(method, path, nil, "", hdr)
			if r.status != http.StatusUnauthorized || r.header.Get("HX-Redirect") != loc {
				t.Errorf("htmx GET %s (%s) = %d, HX-Redirect %q, want 401 and %q", route, c.name, r.status, r.header.Get("HX-Redirect"), loc)
			}
			if got := noticeText(t, r.header.Get("HX-Trigger")); got != c.msg {
				t.Errorf("htmx GET %s (%s) notice = %q, want %q", route, c.name, got, c.msg)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	if checked < 60 {
		t.Errorf("walked %d guarded routes, want at least 60: the router isn't fully mounted", checked)
	}
}

// noticeText reads web.Error's HX-Trigger notice.
func noticeText(t *testing.T, trigger string) string {
	t.Helper()
	var v struct {
		Notice struct{ Text string } `json:"notice"`
	}
	if err := json.Unmarshal([]byte(trigger), &v); err != nil {
		t.Errorf("HX-Trigger %q: %v", trigger, err)
	}
	return v.Notice.Text
}

// DOC-085's route clause, CD-91.
func TestApp_NoPutPatchDeleteRoutes(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	admin := w.signedIn(adminEmail)
	id := admin.newOrder(m, "PRJ-METRO", "1000.00")
	fill := func(param string) string {
		switch strings.ToLower(param) {
		case "doctype":
			return fixture.Order
		case "id":
			return strconv.FormatInt(id, 10)
		case "step":
			return "details"
		case "action":
			return "cancel"
		}
		return "1"
	}

	var gets []string
	err := chi.Walk(w.routes(), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		switch method {
		case http.MethodPut, http.MethodPatch, http.MethodDelete:
			t.Errorf("route %s %s is registered, want none (DOC-085)", method, route)
		case http.MethodGet:
			gets = append(gets, route)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	if len(gets) < 30 {
		t.Fatalf("walked %d GET routes, want at least 30: the router isn't fully mounted", len(gets))
	}

	before := w.count(`SELECT count(*) FROM audit_log`)
	hdr := map[string]string{"X-CSRF-Token": admin.csrf}
	for _, route := range gets {
		path := fillRoute(route, fill)
		for _, method := range []string{http.MethodPut, http.MethodPatch, http.MethodDelete} {
			r := admin.do(method, path, nil, "", hdr)
			if r.status != http.StatusMethodNotAllowed && r.status != http.StatusNotFound {
				t.Errorf("%s %s = %d, want 405 or 404", method, path, r.status)
			}
		}
	}
	if after := w.count(`SELECT count(*) FROM audit_log`); after != before {
		t.Errorf("audit_log rows went from %d to %d, want no change", before, after)
	}
}

// CD-87, CD-200, CD-231: AUTH's route inventory is the Guarded handlers
// chi.Walk finds.
func TestApp_RouteGuards(t *testing.T) {
	w := newWorld(t)
	kinds := map[string]authweb.Kind{}
	validate, docRoutes := 0, 0
	err := chi.Walk(w.routes(), func(method, route string, h http.Handler, _ ...func(http.Handler) http.Handler) error {
		g, ok := h.(*authweb.Guarded)
		if !ok {
			return nil
		}
		kinds[method+" "+route] = g.Req.Kind
		switch {
		case strings.HasPrefix(route, "/ui/validate/"):
			validate++
			if g.Req.Kind != authweb.KindSelf {
				t.Errorf("%s %s has kind %d, want KindSelf (CD-87)", method, route, g.Req.Kind)
			}
		case strings.HasPrefix(route, "/doc/"):
			docRoutes++
			if g.Req.Kind != authweb.KindCheckedInHandler {
				t.Errorf("%s %s has kind %d, want KindCheckedInHandler (CD-46)", method, route, g.Req.Kind)
			}
		case g.Req.Kind == authweb.KindCheckedInHandler:
			t.Errorf("%s %s uses KindCheckedInHandler outside /doc/ (CD-87)", method, route)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	if validate == 0 {
		t.Error("no /ui/validate/ route is mounted")
	}
	if docRoutes < 18 {
		t.Errorf("found %d /doc/ routes, want at least 18", docRoutes)
	}
	for route, want := range map[string]authweb.Kind{
		"GET /":               authweb.KindSelf,
		"GET /search":         authweb.KindSelf,
		"GET /approvals":      authweb.KindSelf,
		"GET /approvals/mine": authweb.KindSelf,
		"GET /setup/company":  authweb.KindAdmin,
		"POST /setup/company": authweb.KindAdmin,
	} {
		if got, ok := kinds[route]; !ok || got != want {
			t.Errorf("%s: kind %d (registered %v), want %d", route, got, ok, want)
		}
	}
}

// AUTH hand-off: CheckRoutes over the full router.
func TestApp_CheckRoutesClean(t *testing.T) {
	w := newWorld(t)
	routes := w.routes()
	if problems := authweb.CheckRoutes(routes); len(problems) != 0 {
		t.Errorf("CheckRoutes found %d problems:\n%s", len(problems), strings.Join(problems, "\n"))
	}
	n := 0
	err := chi.Walk(routes, func(string, string, http.Handler, ...func(http.Handler) http.Handler) error {
		n++
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	if n < 60 {
		t.Errorf("walked %d routes, want at least 60: a clean result over an empty router proves nothing", n)
	}
}

// deps lists a package's dependencies in the release build.
func deps(t *testing.T, pkg string, args ...string) []string {
	t.Helper()
	cmd := exec.Command("go", append(append([]string{"list", "-deps"}, args...), pkg)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v: %s", pkg, err, stderr.String())
	}
	return strings.Fields(string(out))
}

func TestApp_FixtureDocTypesNotInRelease(t *testing.T) {
	binary := deps(t, modulePath+"/cmd/app")
	if !slices.Contains(binary, modulePath+"/internal/app") {
		t.Fatal("cmd/app does not depend on internal/app, so this proves nothing about the release binary")
	}
	if slices.Contains(binary, modulePath+"/internal/fixture") {
		t.Error("the release binary links internal/fixture")
	}
	if slices.Contains(deps(t, modulePath+"/internal/app"), modulePath+"/internal/fixture") {
		t.Error("internal/app imports internal/fixture outside its tests")
	}
}

// U8: the gallery is an e2e-tag page only.
func TestApp_GalleryNotInRelease(t *testing.T) {
	binary := deps(t, modulePath+"/cmd/app")
	if !slices.Contains(binary, modulePath+"/internal/app") {
		t.Fatal("cmd/app does not depend on internal/app, so this proves nothing about the release binary")
	}
	if slices.Contains(binary, modulePath+"/internal/ui/gallery") {
		t.Error("the release binary links internal/ui/gallery")
	}
}

// CD-231: /approvals is the landing page, so it opens for a user with no
// approval rights and for one with no rights at all.
func TestApp_ApprovalsOpensForEveryone(t *testing.T) {
	w := newWorld(t)
	admin := w.seedUsers()
	if _, _, err := w.app.Auth().CreateUser(context.Background(), admin, auth.NewUser{
		Email: nobodyEmail, Name: "Nina Bose", Password: testPassword, Roles: []string{},
	}); err != nil {
		t.Fatalf("CreateUser with no roles: %v", err)
	}

	for _, email := range []string{deepakEmail, nobodyEmail} {
		t.Run(email, func(t *testing.T) {
			b := w.browser()
			r := b.signIn(email)
			if r.location() != "/" {
				t.Fatalf("POST /signin redirected to %q, want /", r.location())
			}
			r = b.get("/")
			if r.status != http.StatusSeeOther || r.location() != "/approvals" {
				t.Fatalf("GET / = %d to %q, want 303 to /approvals", r.status, r.location())
			}
			r = b.get(r.location())
			if r.status != http.StatusOK {
				t.Fatalf("GET /approvals = %d (%q), want 200", r.status, r.pageText())
			}
			if !strings.Contains(r.pageText(), msgInboxEmpty) {
				t.Errorf("GET /approvals text = %q, want %q", r.pageText(), msgInboxEmpty)
			}
		})
	}
}

// goodSetup is CD-165's input with 00-fixtures.md's company.
func goodSetup() url.Values {
	return url.Values{
		"legal_name": {"Vardhan Infra Projects Pvt Ltd"}, "state_code": {"27"}, "gstin": {"27AAFCV2449G1Z7"},
		"pan": {""}, "branch_code": {"PUNE"}, "branch_name": {"Pune Works"},
	}
}

// CD-228, CD-231: the public host's whole first install.
func TestApp_SecureFirstInstall(t *testing.T) {
	w := newWorld(t, func(c *app.Config) { c.Secure = true })
	b := w.browser()

	assertSetupClosed := func(when string) {
		t.Helper()
		if r := b.get("/setup"); r.status != http.StatusNotFound {
			t.Errorf("GET /setup %s = %d, want 404", when, r.status)
		}
		if r := b.do(http.MethodPost, "/setup", strings.NewReader("email=x"), "application/x-www-form-urlencoded", nil); r.status != http.StatusNotFound {
			t.Errorf("POST /setup %s = %d, want 404", when, r.status)
		}
	}
	assertSetupClosed("on an empty database")
	if r := b.get("/"); r.status != http.StatusSeeOther || r.location() != "/signin?next=%2F" {
		t.Errorf("GET / on an empty database = %d to %q, want 303 to /signin?next=%%2F, never /setup", r.status, r.location())
	}
	if r := b.get("/signin"); r.status != http.StatusOK {
		t.Errorf("GET /signin on an empty database = %d, want 200", r.status)
	}

	var out bytes.Buffer
	args := []string{"--email", adminEmail, "--name", "Anil Kapoor"}
	if err := cli.AdminCreate(context.Background(), w.app.Auth(), args, strings.NewReader(testPassword+"\n"+testPassword+"\n"), &out); err != nil {
		t.Fatalf("admin create: %v (%q)", err, out.String())
	}
	assertSetupClosed("after admin create")

	r := b.signIn(adminEmail)
	if r.location() != "/" {
		t.Fatalf("POST /signin redirected to %q, want /", r.location())
	}
	if r := b.get("/"); r.status != http.StatusSeeOther || r.location() != "/setup/company" {
		t.Fatalf("GET / with no company = %d to %q, want 303 to /setup/company", r.status, r.location())
	}
	if r := b.get("/setup/company"); r.status != http.StatusOK {
		t.Fatalf("GET /setup/company = %d, want 200", r.status)
	}
	r = b.post("/setup/company", goodSetup())
	if r.status != http.StatusSeeOther || r.location() != "/" {
		t.Fatalf("POST /setup/company = %d to %q (%q), want 303 to /", r.status, r.location(), r.pageText())
	}
	r = b.get("/")
	if r.status != http.StatusSeeOther || r.location() != "/approvals" {
		t.Fatalf("GET / after company setup = %d to %q, want 303 to /approvals", r.status, r.location())
	}
	if r := b.get(r.location()); r.status != http.StatusOK || !strings.Contains(r.pageText(), msgInboxEmpty) {
		t.Errorf("GET /approvals = %d, text %q, want 200 and %q", r.status, r.pageText(), msgInboxEmpty)
	}
}

// CD-165, CD-198: first-run setup calls company.SetupTx.
func TestApp_CompanySetup(t *testing.T) {
	w := newWorld(t)
	admin := w.seedUsers()
	b := w.signedIn(adminEmail)

	t.Run("form_asks_for_every_field", func(t *testing.T) {
		r := b.get("/setup/company")
		if r.status != http.StatusOK {
			t.Fatalf("GET /setup/company = %d, want 200", r.status)
		}
		for name := range goodSetup() {
			if r.doc.Find(`[name="`+name+`"]`).Length() != 1 {
				t.Errorf("the form has no single %q field", name)
			}
		}
	})

	t.Run("not_for_other_users", func(t *testing.T) {
		p := w.signedIn(priyaEmail)
		if r := p.get("/setup/company"); r.status != http.StatusForbidden {
			t.Errorf("GET /setup/company as Priya = %d, want 403", r.status)
		}
		if r := p.post("/setup/company", goodSetup()); r.status != http.StatusForbidden {
			t.Errorf("POST /setup/company as Priya = %d, want 403", r.status)
		}
		if n := w.count(`SELECT count(*) FROM company`); n != 0 {
			t.Errorf("company rows after Priya's POST = %d, want 0", n)
		}
	})

	refusals := []struct {
		name, field, value string
		in                 company.SetupInput
	}{
		{"bad_gstin", "gstin", "27AABCS1429B1Z5", company.SetupInput{LegalName: "Vardhan Infra Projects Pvt Ltd", StateCode: "27", GSTIN: "27AABCS1429B1Z5", BranchCode: "PUNE", BranchName: "Pune Works"}},
		{"blank_branch_code", "branch_code", "", company.SetupInput{LegalName: "Vardhan Infra Projects Pvt Ltd", StateCode: "27", GSTIN: "27AAFCV2449G1Z7", BranchName: "Pune Works"}},
	}
	for _, c := range refusals {
		t.Run(c.name, func(t *testing.T) {
			msgs := w.refusal(func(tx *db.Tx) error {
				return w.app.Masters().Company.SetupTx(context.Background(), tx, admin, c.in)
			})
			form := goodSetup()
			form.Set(c.field, c.value)
			r := b.post("/setup/company", form)
			if r.status != http.StatusUnprocessableEntity {
				t.Fatalf("POST = %d, want 422", r.status)
			}
			for _, m := range msgs {
				if !strings.Contains(r.pageText(), m) {
					t.Errorf("page text %q, want the service's %q", r.pageText(), m)
				}
			}
			if v, _ := r.doc.Find(`[name="legal_name"]`).Attr("value"); v != "Vardhan Infra Projects Pvt Ltd" {
				t.Errorf("legal_name after a refusal = %q, want the typed value kept", v)
			}
			if v, _ := r.doc.Find(`[name="` + c.field + `"]`).Attr("value"); v != c.value {
				t.Errorf("%s after a refusal = %q, want the typed %q", c.field, v, c.value)
			}
			if n := w.count(`SELECT count(*) FROM company`); n != 0 {
				t.Errorf("company rows after a refusal = %d, want 0", n)
			}
		})
	}

	t.Run("saves_and_closes", func(t *testing.T) {
		r := b.post("/setup/company", goodSetup())
		if r.status != http.StatusSeeOther || r.location() != "/" {
			t.Fatalf("POST = %d to %q (%q), want 303 to /", r.status, r.location(), r.pageText())
		}
		if got := w.text(`SELECT legal_name || '/' || pan FROM company`); got != "Vardhan Infra Projects Pvt Ltd/AAFCV2449G" {
			t.Errorf("company = %q, want the legal name and the PAN from the GSTIN", got)
		}
		if got := w.text(`SELECT code || '/' || name FROM branch`); got != "PUNE/Pune Works" {
			t.Errorf("branch = %q, want PUNE/Pune Works", got)
		}
		if r := b.get("/setup/company"); r.status != http.StatusNotFound {
			t.Errorf("GET /setup/company once a company exists = %d, want 404", r.status)
		}
		if r := b.post("/setup/company", goodSetup()); r.status != http.StatusNotFound {
			t.Errorf("POST /setup/company once a company exists = %d, want 404", r.status)
		}
	})
}

// Signed-in `/`: SafeNext and the header brand link land here (CD-198).
func TestApp_SignedInHome(t *testing.T) {
	w := newWorld(t)
	_ = w.seedUsers()

	admin := w.signedIn(adminEmail)
	if r := admin.get("/"); r.status != http.StatusSeeOther || r.location() != "/setup/company" {
		t.Errorf("GET / as Administrator with no company = %d to %q, want 303 to /setup/company", r.status, r.location())
	}
	priya := w.signedIn(priyaEmail)
	if r := priya.get("/"); r.status != http.StatusSeeOther || r.location() != "/approvals" {
		t.Errorf("GET / as Priya with no company = %d to %q, want 303 to /approvals", r.status, r.location())
	}
	if r := admin.post("/setup/company", goodSetup()); r.status != http.StatusSeeOther {
		t.Fatalf("POST /setup/company = %d (%q), want 303", r.status, r.pageText())
	}
	if r := admin.get("/"); r.status != http.StatusSeeOther || r.location() != "/approvals" {
		t.Errorf("GET / as Administrator with a company = %d to %q, want 303 to /approvals", r.status, r.location())
	}
	page := priya.get("/approvals")
	if page.doc.Find(`a.brand[href="/"]`).Length() != 1 {
		t.Error("the header has no brand link to /")
	}
}
