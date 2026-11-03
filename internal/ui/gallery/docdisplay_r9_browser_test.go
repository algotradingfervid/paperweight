//go:build e2e

package gallery

import (
	"net/http/httptest"
	"testing"

	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// TestDocDisplayBrowser_EveryWordAndAmount (UI review R9, PO H6, GRN M8, EXP
// totals): at each width from 320 to 1280, no real state word splits across
// lines, no amount of any length wraps or leaves its box, a connector crosses
// no word and joins its dots, and kv rows keep label and value on one row,
// with money and text starting at one place. uibrowser.DocDisplay measures
// the rendered lines of each word, not overflow.
func TestDocDisplayBrowser_EveryWordAndAmount(t *testing.T) {
	s := httptest.NewServer(Handler())
	t.Cleanup(s.Close)
	ctx := uibrowser.New(t)
	for _, w := range []int64{320, 360, 390, 480, 768, 1280} {
		if err := uibrowser.At(ctx, w, s.URL+"/_gallery/docdisplay"); err != nil {
			t.Fatal(err)
		}
		bad, err := uibrowser.DocDisplay(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range bad {
			t.Errorf("%d px: %s", w, p)
		}
	}
}
