//go:build e2e

package gallery

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// TestFocusBrowser_HeaderHiddenWithClose (UI v2 §5.6, N12): the gallery
// wizard sets Model.Close, so in focus mode the app header is hidden below
// 64rem (390 and 1000 px), the wizard head is the sticky top bar, and the
// header shows again from 64rem (1024 px).
func TestFocusBrowser_HeaderHiddenWithClose(t *testing.T) {
	s := httptest.NewServer(Handler())
	t.Cleanup(s.Close)
	ctx := uibrowser.New(t)
	for _, c := range []struct {
		width  int64
		header string
		head   string
	}{
		{390, "none", "sticky"},
		{1000, "none", "sticky"},
		{1024, "flex", "static"},
	} {
		if err := uibrowser.At(ctx, c.width, s.URL+"/_gallery/wizard-1"); err != nil {
			t.Fatal(err)
		}
		sctx, cancel := context.WithTimeout(ctx, uibrowser.StepTimeout)
		got, err := chromedp.Run(sctx, chromedp.Evaluate[[]string](`[
  document.querySelectorAll(".wizard-close").length + "",
  getComputedStyle(document.querySelector(".app-header")).display,
  getComputedStyle(document.querySelector(".wizard-head")).position]`))
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		if got[0] != "1" {
			t.Fatalf("%d px: %s close links, want the gallery wizard's one", c.width, got[0])
		}
		if got[1] != c.header || got[2] != c.head {
			t.Errorf("%d px: header display %q, wizard head position %q; want %q, %q", c.width, got[1], got[2], c.header, c.head)
		}
	}
}
