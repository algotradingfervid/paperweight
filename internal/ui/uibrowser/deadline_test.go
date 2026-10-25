//go:build e2e

package uibrowser

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
