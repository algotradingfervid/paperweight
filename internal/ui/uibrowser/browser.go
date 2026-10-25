//go:build e2e

// Package uibrowser holds the browser checks for the gallery and the
// full-route sweep: overflow, target size, axe and CSP problems.
package uibrowser

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/log"
	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/ui/screens"
)

// New starts a headless browser for t and closes it when t ends.
func New(t *testing.T) context.Context {
	t.Helper()
	actx, cancelA := chromedp.NewExecAllocator(context.Background(), chromedp.DefaultExecAllocatorOptions[:]...)
	ctx, cancel := chromedp.NewContext(actx)
	t.Cleanup(func() { cancel(); cancelA() })
	if err := chromedp.Do(ctx); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			t.Fatal("Chrome not found: install Chrome or Chromium to run e2e tests")
		}
		t.Fatal(err)
	}
	return ctx
}

// At sets the viewport to width w (as a phone below screens.MobileBelow) and
// opens url.
func At(ctx context.Context, w int64, url string) error {
	var opts []chromedp.EmulateViewportOption
	if w < screens.MobileBelow {
		opts = append(opts, chromedp.EmulateMobile, chromedp.EmulateTouch)
	}
	return chromedp.Do(ctx, chromedp.EmulateViewport(w, 800, opts...), chromedp.Navigate(url), chromedp.WaitReady("body"))
}

// Overflow returns the horizontal overflow problems on the current page.
func Overflow(ctx context.Context) ([]string, error) {
	return chromedp.Run(ctx, chromedp.Evaluate[[]string](screens.OverflowJS))
}

// SmallTargets returns the interactive elements smaller than 44 × 44 px.
func SmallTargets(ctx context.Context) ([]string, error) {
	return chromedp.Run(ctx, chromedp.Evaluate[[]string](screens.TargetSizeJS))
}

// CSPProblems subscribes to the browser log. The returned func lists the
// security and Content Security Policy entries seen since its previous call.
func CSPProblems(ctx context.Context) func() []string {
	var (
		mu  sync.Mutex
		out []string
	)
	entries := chromedp.Events(ctx, log.EntryAdded)
	if _, err := chromedp.Call(ctx, log.Enable, cdp.Empty{}); err != nil {
		out = append(out, "enable log: "+err.Error())
	}
	go func() {
		for ev, err := range entries {
			if err != nil {
				return
			}
			if e := ev.Entry; e != nil && (e.Source == log.EntrySourceSecurity || strings.Contains(e.Text, "Content Security Policy")) {
				mu.Lock()
				out = append(out, e.Text)
				mu.Unlock()
			}
		}
	}()
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		got := out
		out = nil
		return got
	}
}

// Check opens s at every width in screens.Widths under base and reports
// overflow, small targets (at 360), axe violations (at 360 and 1440) and CSP
// problems.
func Check(t *testing.T, ctx context.Context, s screens.Screen, base string) {
	t.Helper()
	if s.As != "" {
		t.Errorf("%s: sign-in not supported yet", s.Name)
		return
	}
	csp := CSPProblems(ctx)
	for _, w := range screens.Widths {
		if err := At(ctx, w, base+s.URL); err != nil {
			t.Errorf("%s@%d: open: %v", s.Name, w, err)
			continue
		}
		if s.Setup != nil {
			if err := s.Setup(ctx); err != nil {
				t.Errorf("%s@%d: setup: %v", s.Name, w, err)
				continue
			}
		}
		report(t, ctx, s.Name, w, "overflow", Overflow)
		if w == 360 {
			report(t, ctx, s.Name, w, "target smaller than 44px", SmallTargets)
		}
		if w == 360 || w == 1440 {
			vs, err := Axe(ctx)
			if err != nil {
				t.Errorf("%s@%d: axe: %v", s.Name, w, err)
			}
			for _, v := range vs {
				t.Errorf("%s@%d: axe %s: %s: %s", s.Name, w, v.ID, v.Help, strings.Join(v.Nodes, ", "))
			}
		}
		for _, p := range csp() {
			t.Errorf("%s@%d: CSP: %s", s.Name, w, p)
		}
	}
	for _, p := range csp() {
		t.Errorf("%s: CSP: %s", s.Name, p)
	}
}

func report(t *testing.T, ctx context.Context, name string, w int64, what string, f func(context.Context) ([]string, error)) {
	t.Helper()
	probs, err := f(ctx)
	if err != nil {
		t.Errorf("%s@%d: %s: %v", name, w, what, err)
	}
	for _, p := range probs {
		t.Errorf("%s@%d: %s: %s", name, w, what, p)
	}
}
