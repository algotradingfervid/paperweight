//go:build e2e

package gallery

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// TestQtySuffixBrowser_Size checks that a plain QtyInput's unit suffix is
// drawn at the inputs' 16 px, not the field's inherited small text (CD-290).
func TestQtySuffixBrowser_Size(t *testing.T) {
	s := httptest.NewServer(Handler())
	t.Cleanup(s.Close)
	ctx := uibrowser.New(t)
	if err := uibrowser.At(ctx, 1024, s.URL+"/_gallery/fields"); err != nil {
		t.Fatal(err)
	}
	sctx, cancel := context.WithTimeout(ctx, uibrowser.StepTimeout)
	defer cancel()
	const js = `(() => {
		const e = document.querySelector("#f-qty ~ .field__suffix");
		return e ? getComputedStyle(e).fontSize : "no suffix";
	})()`
	got, err := chromedp.Run(sctx, chromedp.Evaluate[string](js))
	if err != nil {
		t.Fatalf("read suffix size: %v", err)
	}
	if got != "16px" {
		t.Errorf("QtyInput suffix font-size = %s, want 16px", got)
	}
}
