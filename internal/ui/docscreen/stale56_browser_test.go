//go:build e2e

package docscreen_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/platform/brand"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// m56Server serves h with the bundle, as the app does, and returns its URL.
func m56Server(t *testing.T, h func() http.Handler) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("/static/ui/", http.StripPrefix("/static/ui", ui.Static()))
	mux.Handle("/static/brand/", brand.Static())
	mux.Handle("/static/", web.Static())
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h().ServeHTTP(w, r) }))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// HOOK-056g (CD-348, PUX-008, CD-346): Priya opens her draft's Details
// step; it is handed over to another buyer; her Save and continue is
// refused with AUTH's 403, which htmx doesn't swap, and the page shows
// AUTH's text as the one alert in #alerts. The step stays on screen and no
// other banner appears.
func TestStaleBrowser56_HandOverNotice(t *testing.T) {
	ctx := uibrowser.New(t)
	csp := uibrowser.CSPProblems(ctx)
	e := m15NewOrderEnv(t)
	e.who = m15Load(t, e.a, e.fx.Priya.ID)
	srv := m56Server(t, func() http.Handler { return e.h })
	if err := uibrowser.At(ctx, 1280, srv+m15DocURL(fixture.Order, e.id)+"/edit/details"); err != nil {
		t.Fatal(err)
	}
	m56HandOver(t, e)
	if err := m26Steps(ctx, "save the step and wait for the notice",
		chromedp.Evaluate[chromedp.Void](`document.getElementById("wizard-form").requestSubmit()`),
		chromedp.WaitReady(chromedp.CSS(`#alerts .banner-body`)),
	); err != nil {
		t.Fatal(err)
	}
	text, err := m26Step(ctx, "read the alerts", chromedp.Text(chromedp.CSS("#alerts")))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(norm(text), m15OrderOwnOnly) {
		t.Errorf("#alerts = %q, want AUTH's %q", norm(text), m15OrderOwnOnly)
	}
	n, err := m26Step(ctx, "count the banners", chromedp.Evaluate[int](`document.querySelectorAll("#alerts > *").length`))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("#alerts holds %d banners, want only the refusal's notice", n)
	}
	heading, err := m26Step(ctx, "read the heading", chromedp.Text(chromedp.CSS("h1#step-title")))
	if err != nil {
		t.Fatal(err)
	}
	if got := norm(heading); got != "Details" {
		t.Errorf("after the refusal the heading is %q, want the Details step kept", got)
	}
	for _, p := range csp() {
		t.Errorf("CSP: %s", p)
	}
}
