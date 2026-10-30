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

// New starts a headless browser for t and closes it when t ends or after
// BrowserTimeout.
func New(t *testing.T) context.Context {
	t.Helper()
	return NewFor(t, BrowserTimeout)
}

// NewFor is New with a longer life, for a full sweep in one browser.
func NewFor(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	errBrowserDeadline := fmt.Errorf("browser deadline %v exceeded: %w", d, context.DeadlineExceeded)
	tctx, cancelT := context.WithTimeoutCause(context.Background(), d, errBrowserDeadline)
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

// setup runs s.Setup, if there is one, on the page open at url.
func setup(ctx context.Context, s screens.Screen, url string) error {
	if s.Setup == nil {
		return nil
	}
	_, err := withDeadline(ctx, StepTimeout, "setup "+url, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, s.Setup(ctx)
	})
	return err
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

// Options says what Check does.
type Options struct {
	Widths                 []int64 // the widths to open each screen at
	Overflow, Targets, Axe bool    // the checks to run at each width
	// CSP is the func CSPProblems returned for this browser; nil skips the check.
	CSP func() []string
	// SignIn gives the browser of ctx the session of email, or none for "". Nil
	// means nobody can sign in, so a screen with As set is a problem.
	SignIn func(ctx context.Context, email string) error
	// Page, if set, looks at the page after its Setup, at each width.
	Page func(ctx context.Context, s screens.Screen) []string
}

// Check opens s at each width under base, signed in as s.As, and reports what
// opt asks for.
func Check(t *testing.T, ctx context.Context, s screens.Screen, base string, opt Options) {
	t.Helper()
	for _, p := range Problems(ctx, s, base, opt) {
		t.Error(p)
	}
}

// Problems is what Check reports.
func Problems(ctx context.Context, s screens.Screen, base string, opt Options) []string {
	var out []string
	add := func(w int64, what string, probs []string, err error) {
		if err != nil {
			out = append(out, fmt.Sprintf("%s@%d: %s: %v", s.Name, w, what, err))
		}
		for _, p := range probs {
			out = append(out, fmt.Sprintf("%s@%d: %s: %s", s.Name, w, what, p))
		}
	}
	switch {
	case opt.SignIn != nil:
		if _, err := withDeadline(ctx, StepTimeout, "sign in", func(ctx context.Context) (struct{}, error) {
			return struct{}{}, opt.SignIn(ctx, s.As)
		}); err != nil {
			return []string{s.Name + ": sign in as " + s.As + ": " + err.Error()}
		}
	case s.As != "":
		return []string{s.Name + ": Options has no SignIn for " + s.As}
	}
	url := base + s.URL
	for _, w := range opt.Widths {
		if err := At(ctx, w, url); err != nil {
			add(w, "open", nil, err)
			continue
		}
		if err := setup(ctx, s, url); err != nil {
			add(w, "setup", nil, err)
			continue
		}
		if opt.Page != nil {
			add(w, "page", opt.Page(ctx, s), nil)
		}
		if opt.Overflow {
			probs, err := Overflow(ctx)
			add(w, "overflow", probs, err)
		}
		if opt.Targets {
			probs, err := SmallTargets(ctx)
			add(w, "target smaller than 44px", probs, err)
		}
		if opt.Axe {
			vs, err := withDeadline(ctx, AxeTimeout, "axe "+url, Axe)
			add(w, "axe", nil, err)
			for _, v := range vs {
				add(w, "axe "+v.ID, []string{v.Help + ": " + strings.Join(v.Nodes, ", ")}, nil)
			}
		}
		if opt.CSP != nil {
			add(w, "CSP", opt.CSP(), nil)
		}
	}
	return out
}
