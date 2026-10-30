//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth/cli"
	"github.com/vardhan/internaltools/internal/ui/screens"
	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

const (
	// sweepBrowser bounds one sweep's browser. A full pass over 7 minutes would
	// not fit in go test's 10-minute limit on this package anyway.
	sweepBrowser = 7 * time.Minute

	freshAdmin = "first.admin@vardhan.test"
	freshName  = "First Admin"

	// backRounds repeats Back after sign-out: what Chrome does with its
	// back/forward cache is its own decision, and it can vary between runs.
	backRounds = 4
)

var (
	sweepPhoneDesktop = []int64{phone360, desktop}
	sweepTarget       = []int64{phone360}
	registerSweep     sync.Once
)

// sweepAs is Options.SignIn for one browser: a real sign-in, skipped when the
// browser already is that user.
func (h *Harness) sweepAs() func(ctx context.Context, email string) error {
	current := ""
	return func(ctx context.Context, email string) error {
		if email == current {
			return nil
		}
		current = email
		if _, err := chromedp.Call(ctx, network.ClearBrowserCookies, cdp.Empty{}); err != nil {
			return err
		}
		if email == "" {
			return nil
		}
		return chromedp.Do(ctx,
			chromedp.Navigate(h.URL+"/signin"),
			chromedp.Evaluate[chromedp.Void](`(() => {
  const f = document.querySelector("input[name=password]").form;
  f.elements.email.value = `+strconv.Quote(email)+`;
  f.elements.password.value = `+strconv.Quote(password)+`;
  f.requestSubmit();
})()`),
			chromedp.WaitReady(chromedp.CSS(`form[action="/signout"], input[name=legal_name]`)))
	}
}

// sweep checks every page state with opt: h's screens, then a new install's,
// fresh.
func sweep(t *testing.T, opt uibrowser.Options, more ...func(h *Harness, s sweepState) []screens.Screen) (h, fresh *Harness, ctx context.Context) {
	t.Helper()
	h = Start(t)
	state := h.prepareSweep()
	fresh = StartEmpty(t)
	t.Parallel()
	ctx = uibrowser.NewFor(t, sweepBrowser)
	if opt.CSP != nil {
		opt.CSP = uibrowser.CSPProblems(ctx)
	}
	check := func(h *Harness, list []screens.Screen) {
		t.Helper()
		opt.SignIn = h.sweepAs()
		for _, s := range list {
			start := time.Now()
			uibrowser.Check(t, ctx, s, h.URL, opt)
			if d := time.Since(start); d > 2*time.Second {
				t.Logf("SLOW %v %s", d, s.Name)
			}
		}
	}
	list := h.appScreens(state)
	for _, f := range more {
		list = append(list, f(h, state)...)
	}
	check(h, list)
	setup, company := freshScreens()
	check(fresh, setup)
	fresh.createAdmin()
	check(fresh, company)
	return h, fresh, ctx
}

// createAdmin is `app admin create` on an empty install.
func (h *Harness) createAdmin() {
	h.t.Helper()
	args := []string{"--email", freshAdmin, "--name", freshName}
	if err := cli.AdminCreate(context.Background(), h.App.Auth(), args, strings.NewReader(password+"\n"+password+"\n"), &strings.Builder{}); err != nil {
		h.t.Fatal(err)
	}
}

func TestE2E_NoHorizontalOverflow(t *testing.T) {
	sweep(t, uibrowser.Options{Widths: screens.Widths, Overflow: true})
}

func TestE2E_AxeNoViolations(t *testing.T) {
	sweep(t, uibrowser.Options{Widths: sweepPhoneDesktop, Axe: true})
}

func TestE2E_TargetSizeAtLeast44(t *testing.T) {
	sweep(t, uibrowser.Options{Widths: sweepTarget, Targets: true})
}

// TestE2E_NoCSPViolations sweeps with report-uri on every response, and the
// pages the other sweeps leave out, because style-src 'self' applies to them
// too. An inline style at the end proves a violation does reach the server.
func TestE2E_NoCSPViolations(t *testing.T) {
	h, fresh, ctx := sweep(t, uibrowser.Options{Widths: screens.Widths, CSP: func() []string { return nil }}, func(h *Harness, s sweepState) []screens.Screen {
		return []screens.Screen{{Name: "print", Route: "/print/{doctype}/{id}", URL: "/print/" + fixture.Order + "/" + strconv.FormatInt(s.order.Draft, 10), As: priya}}
	})
	// The sweep ends on the new install, which has its own report endpoint.
	if err := uibrowser.At(ctx, desktop, h.URL+"/signin"); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](`document.body.setAttribute("style", "color: red")`)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(stepDeadline)
	reports := h.CSPReports()
	for len(reports) == 0 && time.Now().Before(deadline) {
		time.Sleep(waitEvery)
		reports = h.CSPReports()
	}
	if len(reports) != 1 {
		t.Errorf("%d CSP reports, want only the control's:\n%s", len(reports), strings.Join(reports, "\n"))
	}
	if reports := fresh.CSPReports(); len(reports) != 0 {
		t.Errorf("%d CSP reports from the new install, want none:\n%s", len(reports), strings.Join(reports, "\n"))
	}
}

// TestE2E_EveryRouteCovered is the sweep's own gate: every GET route of the real
// router has a screen or is on the notPages list, each screen really opens the
// page it names, for its user, and each route on the list really is no page.
func TestE2E_EveryRouteCovered(t *testing.T) {
	h := Start(t)
	state := h.prepareSweep()
	list := h.appScreens(state)
	setup, company := freshScreens()
	registerSweep.Do(func() { screens.Register(slices.Concat(list, setup, company)...) })

	pages := notPages(state)
	for _, p := range notPageProblems(pages) {
		t.Error(p)
	}
	if missing := uncovered(h.routes(), pages); len(missing) != 0 {
		t.Errorf("routes with no screen: %q", missing)
	}

	ctx := uibrowser.NewFor(t, sweepBrowser)
	opt := uibrowser.Options{Widths: []int64{desktop}, SignIn: h.sweepAs(), Page: h.opensAsNamed}
	for _, s := range list {
		uibrowser.Check(t, ctx, s, h.URL, opt)
	}
	if err := opt.SignIn(ctx, admin); err != nil {
		t.Fatal(err)
	}
	for _, p := range pages {
		if name := notPageAfter[p.route]; name != "" {
			i := slices.IndexFunc(list, func(s screens.Screen) bool { return s.Name == name })
			uibrowser.Check(t, ctx, list[i], h.URL, uibrowser.Options{Widths: opt.Widths, SignIn: opt.SignIn})
		}
		want := notPageStatus[p.route]
		if want == 0 {
			want = http.StatusOK
		}
		if why := h.answersNoPage(ctx, p.url, want); why != "" {
			t.Errorf("%s (%s): %s", p.route, p.why, why)
		}
	}
}

// A not-page entry with no sample URL is refused, and a not-page route hides
// no route under it: a new GET route there needs a screen of its own.
func TestE2E_NotPagesAreChecked(t *testing.T) {
	if got := notPageProblems([]notPage{{"/x.csv", "", "a CSV"}}); len(got) != 1 {
		t.Errorf("an entry with no URL: problems %q, want one", got)
	}
	if got := notPageProblems(notPages(sweepState{})); len(got) != 0 {
		t.Errorf("notPages: %q", got)
	}
	r := chi.NewRouter()
	nop := func(http.ResponseWriter, *http.Request) {}
	r.Get("/files/{id}", nop)
	r.Get("/files/{id}/thumb", nop)
	got := uncovered(r, []notPage{{"/files/{id}", "/files/1", "a file"}})
	if want := []string{"/files/{id}/thumb"}; !slices.Equal(got, want) {
		t.Errorf("uncovered = %q, want %q", got, want)
	}
}

// opensAsNamed is the page's HTTP status and route against s's. A Setup that
// posts ends on a status of its own, and it has waited for its state.
func (h *Harness) opensAsNamed(ctx context.Context, s screens.Screen) []string {
	got, err := chromedp.Run(ctx, chromedp.Evaluate[pageFacts](`({
  path: location.pathname,
  status: performance.getEntriesByType("navigation")[0].responseStatus,
  main: document.querySelector("main#main") !== null,
})`))
	if err != nil {
		return []string{err.Error()}
	}
	var out []string
	want := expectedStatus[s.Name]
	if want == 0 {
		want = 200
	}
	if s.Setup == nil && got.Status != want {
		out = append(out, fmt.Sprintf("status %d at %s, want %d", got.Status, got.Path, want))
	}
	if !got.Main && expectedStatus[s.Name] == 0 {
		out = append(out, "no main#main at "+got.Path)
	}
	path, _, _ := strings.Cut(s.URL, "?")
	find := func(p string) string { return h.routes().Find(chi.NewRouteContext(), "GET", p) }
	if find(path) != s.Route && find(got.Path) != s.Route {
		out = append(out, fmt.Sprintf("route %q or %q, screen says %q", find(path), find(got.Path), s.Route))
	}
	return out
}

type pageFacts struct {
	Path   string `json:"path"`
	Status int    `json:"status"`
	Main   bool   `json:"main"`
}

// expectedStatus are the screens that aren't a 200.
var expectedStatus = map[string]int{"not found": 404, "unknown path": 404, "unknown path signed out": 404, "refused": 403}

// answersNoPage fetches url in the signed-in browser and says what's wrong if
// the answer isn't status want or is a whole page.
func (h *Harness) answersNoPage(ctx context.Context, url string, want int) string {
	sctx, cancel := context.WithTimeout(ctx, stepDeadline)
	defer cancel()
	got, err := chromedp.Run(sctx, chromedp.Evaluate[pageFacts](`fetch(`+strconv.Quote(url)+`).then(async (r) => ({
  status: r.status, path: new URL(r.url).pathname, main: /<html/i.test(await r.text()),
}))`, chromedp.EvalAwaitPromise))
	switch {
	case err != nil:
		return err.Error()
	case got.Status != want:
		return fmt.Sprintf("status %d at %s, want %d", got.Status, got.Path, want)
	case got.Main:
		return "it answers with a whole page at " + got.Path
	}
	return ""
}

// TestE2E_BackAfterSignOutShowsNothing is CD-225: on a shared computer, Back
// after sign-out shows the sign-in page, never the signed-in one, whether
// Chrome reloads it or restores it from the back/forward cache.
func TestE2E_BackAfterSignOutShowsNothing(t *testing.T) {
	h := Start(t)
	path := DocPath(fixture.Order, refPO(h).ID)
	const supplier = "Shree Steel Traders"
	s := h.SignIn(priya, desktop)
	s.Open(path)
	if got := Eval[string](s, `fetch(location.href).then((r) => r.headers.get("Cache-Control"))`, chromedp.EvalAwaitPromise); got != "private, no-store" {
		t.Errorf("the signed-in page's Cache-Control is %q, want %q (CD-225)", got, "private, no-store")
	}
	signIn := func() {
		s.Open("/signin")
		s.Fill("email", priya)
		s.Fill("password", password)
		s.Press("Sign in")
		s.Until("the signed-in landing page", `location.pathname === "/approvals"`)
	}
	backShowsSignIn := func(when string) {
		s.Back()
		s.Until("the sign-in page "+when, `location.pathname === "/signin" && document.querySelector("input[name=password]") !== null`)
		if s.Shows(supplier) {
			t.Errorf("Back %s shows the document:\n%s", when, s.page())
		}
	}

	for range backRounds {
		s.Open(path)
		s.WaitText(supplier)
		s.Do("open the account menu", chromedp.Evaluate[chromedp.Void](`document.querySelector("details.user-menu").open = true`))
		s.Press("Sign out")
		s.Until("the sign-in page", `location.pathname === "/signin"`)
		backShowsSignIn("after sign-out")
		signIn()
	}

	// A session ended on the server changes no cookie, so Chrome keeps the page
	// in its cache: only the app can tell. (Administrator's "sign out everywhere",
	// the idle limit.)
	for range backRounds {
		s.Open(path)
		s.WaitText(supplier)
		s.Open("/account/password")
		if _, err := h.App.DB().SQL().ExecContext(context.Background(), `DELETE FROM sessions`); err != nil {
			t.Fatal(err)
		}
		backShowsSignIn("after the session ended")
		signIn()
	}
}
