//go:build e2e

package gallery

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// largeQtySize is the fields page's LargeQtyInput sample measured in the page.
type largeQtySize struct {
	Height, Figures, Suffix float64
	Value                   string
	Clipped                 bool
}

const measureLargeQty = `(() => {
  const i = document.getElementById("f-received"), s = i.parentElement.querySelector(".field__suffix");
  return {
    Height: i.getBoundingClientRect().height,
    Figures: parseFloat(getComputedStyle(i).fontSize),
    Suffix: parseFloat(getComputedStyle(s).fontSize),
    Value: i.value,
    Clipped: i.scrollWidth > i.clientWidth + 1 || i.getBoundingClientRect().right > document.documentElement.clientWidth + 0.5,
  };
})()`

// TestLargeQtyInputBrowser_Size (HOOK-048, CD-286 Q4): on a phone the large
// quantity box is at least 56 px tall with 20 px figures and a 16 px unit,
// shows 9,99,999.9999 whole, and the page neither overflows nor has a small
// target.
func TestLargeQtyInputBrowser_Size(t *testing.T) {
	s := httptest.NewServer(Handler())
	t.Cleanup(s.Close)
	ctx := uibrowser.New(t)
	for _, w := range []int64{320, 360} {
		if err := uibrowser.At(ctx, w, s.URL+"/_gallery/fields"); err != nil {
			t.Fatal(err)
		}
		sctx, cancel := context.WithTimeoutCause(ctx, uibrowser.StepTimeout, fmt.Errorf("measure LargeQtyInput: step deadline %v exceeded: %w", uibrowser.StepTimeout, context.DeadlineExceeded))
		got, err := chromedp.Run(sctx, chromedp.Evaluate[largeQtySize](measureLargeQty))
		if err != nil && sctx.Err() != nil {
			err = context.Cause(sctx)
		}
		cancel()
		if err != nil {
			t.Fatalf("%d: measure: %v", w, err)
		}
		if got.Value != "9,99,999.9999" {
			t.Fatalf("%d: sample value = %q", w, got.Value)
		}
		if got.Height < 56 {
			t.Errorf("%d: height %.1f px, want at least 56", w, got.Height)
		}
		if got.Figures != 20 {
			t.Errorf("%d: figures %.1f px, want 20", w, got.Figures)
		}
		if got.Suffix != 16 {
			t.Errorf("%d: unit suffix %.1f px, want 16", w, got.Suffix)
		}
		if got.Clipped {
			t.Errorf("%d: 9,99,999.9999 is clipped", w)
		}
		probs, err := uibrowser.Overflow(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range probs {
			t.Errorf("%d: overflow: %s", w, p)
		}
		small, err := uibrowser.SmallTargets(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range small {
			t.Errorf("%d: small target: %s", w, p)
		}
	}
}
