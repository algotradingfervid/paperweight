//go:build e2e

package uibrowser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/ui/screens"
)

func TestDeadline_AtOnSilentServerFailsFast(t *testing.T) {
	hang := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-hang:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(hang)
	old := StepTimeout
	StepTimeout = time.Second
	t.Cleanup(func() { StepTimeout = old })
	ctx := New(t)

	start := time.Now()
	err := At(ctx, 360, srv.URL+"/never")
	took := time.Since(start)
	if err == nil {
		t.Fatal("At on a server that never answers = nil; want a deadline error")
	}
	if msg := err.Error(); !strings.Contains(msg, "open") || !strings.Contains(msg, srv.URL+"/never") || !strings.Contains(msg, "deadline") {
		t.Errorf("At error = %q; want the step, the URL and the deadline", msg)
	}
	if took > StepTimeout+3*time.Second {
		t.Errorf("At took %v; want under %v plus a margin", took, StepTimeout)
	}
}

// A Setup that waits for an element that never appears fails within the step
// deadline on the path Check and visit use.
func TestDeadline_SetupThatNeverFinishesFailsFast(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><body><p>no button here</p></body></html>`))
	}))
	defer srv.Close()
	old := StepTimeout
	StepTimeout = time.Second
	t.Cleanup(func() { StepTimeout = old })
	ctx := New(t)
	url := srv.URL + "/page"
	if err := At(ctx, 360, url); err != nil {
		t.Fatal(err)
	}
	s := screens.Screen{Name: "never", URL: "/page", Setup: func(ctx context.Context) error {
		return chromedp.Do(ctx, chromedp.Click(chromedp.CSS("#never")))
	}}

	start := time.Now()
	err := setup(ctx, s, url)
	took := time.Since(start)
	if err == nil {
		t.Fatal("setup waiting for an element that never appears = nil; want a deadline error")
	}
	if msg := err.Error(); !strings.Contains(msg, "setup") || !strings.Contains(msg, url) || !strings.Contains(msg, "deadline") {
		t.Errorf("setup error = %q; want the step, the URL and the deadline", msg)
	}
	if took > StepTimeout+3*time.Second {
		t.Errorf("setup took %v; want under %v plus a margin", took, StepTimeout)
	}
}
