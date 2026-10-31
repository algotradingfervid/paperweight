//go:build e2e

package uibrowser

import "testing"

// TestCanvas_OnHTML: the canvas grey is on html, so it fills the window
// below a page shorter than the viewport (UI v2 §5.1, A-5). The gallery
// sign-in page fills the window, so the short gallery index stands in.
func TestCanvas_OnHTML(t *testing.T) {
	g := startGallery(t)
	ctx := New(t)
	open(t, ctx, 1280, g.URL+"/_gallery")
	if short := eval[bool](t, ctx, `document.body.getBoundingClientRect().height < window.innerHeight`); !short {
		t.Fatal("the gallery index fills the window; the check needs a short page")
	}
	if got := eval[string](t, ctx, `getComputedStyle(document.documentElement).backgroundColor`); got != "rgb(245, 246, 248)" {
		t.Errorf("html background-color = %s, want the canvas #F5F6F8", got)
	}
}
