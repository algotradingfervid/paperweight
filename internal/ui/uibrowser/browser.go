//go:build e2e

// Package uibrowser holds the browser checks for the gallery and the
// full-route sweep: overflow, target size, axe and CSP problems.
package uibrowser

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/log"
	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/ui/screens"
)

// BrowserTimeout bounds the whole life of a browser from New, so a stuck
// page fails the test instead of hanging until go test's timeout.
const BrowserTimeout = 4 * time.Minute

// StepTimeout bounds each step (open, setup, overflow, targets, log) and
// AxeTimeout an axe run. Vars so a test may lower them.
var (
	StepTimeout = 20 * time.Second
	AxeTimeout  = 30 * time.Second
)

var errBrowserDeadline = fmt.Errorf("browser deadline %v exceeded: %w", BrowserTimeout, context.DeadlineExceeded)

// New starts a headless browser for t and closes it when t ends or after
// BrowserTimeout.
func New(t *testing.T) context.Context {
	t.Helper()
	tctx, cancelT := context.WithTimeoutCause(context.Background(), BrowserTimeout, errBrowserDeadline)
	actx, cancelA := chromedp.NewExecAllocator(tctx, chromedp.DefaultExecAllocatorOptions[:]...)
	ctx, cancel := chromedp.NewContext(actx)
	t.Cleanup(func() {
		if errors.Is(context.Cause(tctx), errBrowserDeadline) {
			t.Errorf("uibrowser: %v", errBrowserDeadline)
		}
		cancel()
		cancelA()
		cancelT()
	})
	if err := chromedp.Do(ctx); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			t.Fatal("Chrome not found: install Chrome or Chromium to run e2e tests")
		}
		t.Fatal(err)
	}
	return ctx
}

// withDeadline runs f under a deadline of d derived from ctx; the browser itself
// lives on. A deadline error names what and the deadline that was hit.
func withDeadline[T any](ctx context.Context, d time.Duration, what string, f func(context.Context) (T, error)) (T, error) {
	sctx, cancel := context.WithTimeoutCause(ctx, d, fmt.Errorf("step deadline %v exceeded: %w", d, context.DeadlineExceeded))
	defer cancel()
	v, err := f(sctx)
	if err != nil && sctx.Err() != nil {
		return v, fmt.Errorf("%s: %w", what, context.Cause(sctx))
	}
	return v, err
}

// At sets the viewport to width w (as a phone below screens.MobileBelow) and
// opens url.
func At(ctx context.Context, w int64, url string) error {
	var opts []chromedp.EmulateViewportOption
	if w < screens.MobileBelow {
		opts = append(opts, chromedp.EmulateMobile, chromedp.EmulateTouch)
	}
	_, err := withDeadline(ctx, StepTimeout, "open "+url, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, chromedp.Do(ctx, chromedp.EmulateViewport(w, 800, opts...), chromedp.Navigate(url), chromedp.WaitReady("body"))
	})
	return err
}

// Overflow returns the horizontal overflow problems on the current page.
func Overflow(ctx context.Context) ([]string, error) {
	return withDeadline(ctx, StepTimeout, "overflow", func(ctx context.Context) ([]string, error) {
		return chromedp.Run(ctx, chromedp.Evaluate[[]string](screens.OverflowJS))
	})
}

// SmallTargets returns the interactive elements smaller than 44 × 44 px.
func SmallTargets(ctx context.Context) ([]string, error) {
	return withDeadline(ctx, StepTimeout, "target size", func(ctx context.Context) ([]string, error) {
		return chromedp.Run(ctx, chromedp.Evaluate[[]string](screens.TargetSizeJS))
	})
}

// CSPProblems subscribes to the browser log. The returned func lists the
// security and Content Security Policy entries seen since its previous call.
func CSPProblems(ctx context.Context) func() []string {
	var (
		mu  sync.Mutex
		out []string
	)
	entries := chromedp.Events(ctx, log.EntryAdded)
	if _, err := withDeadline(ctx, StepTimeout, "enable log", func(ctx context.Context) (cdp.Empty, error) {
		return chromedp.Call(ctx, log.Enable, cdp.Empty{})
	}); err != nil {
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
	url := base + s.URL
	for _, w := range screens.Widths {
		if err := At(ctx, w, url); err != nil {
			t.Errorf("%s@%d: %v", s.Name, w, err)
			continue
		}
		if s.Setup != nil {
			if _, err := withDeadline(ctx, StepTimeout, "setup "+url, func(ctx context.Context) (struct{}, error) {
				return struct{}{}, s.Setup(ctx)
			}); err != nil {
				t.Errorf("%s@%d: setup: %v", s.Name, w, err)
				continue
			}
		}
		report(t, ctx, s.Name, w, url, "overflow", Overflow)
		if w == 360 {
			report(t, ctx, s.Name, w, url, "target smaller than 44px", SmallTargets)
		}
		if w == 360 || w == 1440 {
			vs, err := withDeadline(ctx, AxeTimeout, "axe "+url, Axe)
			if err != nil {
				t.Errorf("%s@%d: %v", s.Name, w, err)
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

func report(t *testing.T, ctx context.Context, name string, w int64, url, what string, f func(context.Context) ([]string, error)) {
	t.Helper()
	probs, err := f(ctx)
	if err != nil {
		t.Errorf("%s@%d: %s: %s: %v", name, w, url, what, err)
	}
	for _, p := range probs {
		t.Errorf("%s@%d: %s: %s", name, w, what, p)
	}
}
