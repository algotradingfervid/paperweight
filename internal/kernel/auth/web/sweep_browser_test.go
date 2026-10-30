//go:build e2e

package authweb_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/ui/screens"
	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// p8Step is one browser step's deadline.
const p8Step = 5 * time.Second

// TestRolesBrowser_SetupsReachTheirState runs each Setup's script in Chrome at
// every width, on one world, so a script that does nothing or works only once
// times out waiting for its state (CD-219).
func TestRolesBrowser_SetupsReachTheirState(t *testing.T) {
	saved := uibrowser.StepTimeout
	uibrowser.StepTimeout = p8Step
	t.Cleanup(func() { uibrowser.StepTimeout = saved })

	for name, tc := range p8Stateful {
		t.Run(name, func(t *testing.T) {
			w, h, admin := p6World(t)
			ctx := uibrowser.New(t)
			// The harness has no ui.Shell, so its forms carry an empty token.
			token := admin.Get("/_test/csrf").Body
			var waited string
			s := p8Screen(t, name, p8IDs(t, w), func(ctx context.Context, js, wait string) error {
				waited = wait
				fill := fmt.Sprintf(`document.querySelectorAll("input[name=csrf_token]").forEach(function (e) { e.value = %q; });`, token)
				return p8Do(ctx, chromedp.Evaluate[chromedp.Void](fill+js), chromedp.WaitVisible(chromedp.CSS(wait)))
			})

			// A page with no session lets the script set the harness's cookie.
			if err := uibrowser.At(ctx, screens.Widths[0], h.HTTP.URL+"/static/app.css"); err != nil {
				t.Fatal(err)
			}
			c := admin.Cookie()
			if err := p8Do(ctx, chromedp.Evaluate[chromedp.Void](fmt.Sprintf(`document.cookie = %q`, c.Name+"="+c.Value+"; path=/"))); err != nil {
				t.Fatalf("sign in: %v", err)
			}
			for _, width := range screens.Widths {
				if err := uibrowser.At(ctx, width, h.HTTP.URL+s.URL); err != nil {
					t.Fatal(err)
				}
				if err := s.Setup(ctx); err != nil {
					t.Fatalf("%d: setup: %v", width, err)
				}
				sctx, cancel := context.WithTimeout(ctx, p8Step)
				got, err := chromedp.Run(sctx, chromedp.Evaluate[string](`document.querySelector(`+strconv.Quote(waited)+`).textContent`))
				cancel()
				if err != nil {
					t.Fatalf("%d: read %s: %v", width, waited, err)
				}
				if !strings.Contains(strings.Join(strings.Fields(got), " "), tc.text) {
					t.Errorf("%d: %s = %q, want it to hold %q", width, waited, got, tc.text)
				}
			}
		})
	}
}

// p8Do runs actions under one step's deadline.
func p8Do(ctx context.Context, actions ...chromedp.Action[chromedp.Void]) error {
	ctx, cancel := context.WithTimeout(ctx, p8Step)
	defer cancel()
	return chromedp.Do(ctx, actions...)
}
