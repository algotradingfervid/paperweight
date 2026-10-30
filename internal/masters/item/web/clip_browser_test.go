//go:build e2e

package itemweb_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/platform/brand"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/screens"
	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// clipStep is one browser step's deadline.
const clipStep = 5 * time.Second

// worstFactor is Item's longest factor as it reads after the validator
// formats it (CD-203).
const worstFactor = "9,99,999.999999"

// TestItemBrowser_FactorNeverClips opens a real item page, with the bundle,
// and shows the longest factor unclipped at every width (CD-169, CD-203).
func TestItemBrowser_FactorNeverClips(t *testing.T) {
	w := newWorld(t)
	mtr, drum := w.uomID(t, "MTR"), w.uomID(t, "DRUM")
	f, err := types.ParseFactor(worstFactor)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := w.svc.Create(context.Background(), w.h.Env.Kavya, item.Input{
		Code: "LONG", Name: "Long factor", Type: item.TypeGoods, HSNSAC: "8544", StockUoMID: mtr,
		Conversions: []item.Conversion{{UoMID: mtr, Factor: 1_000_000_000}, {UoMID: drum, Factor: f}},
	})
	if err != nil {
		t.Fatal(err)
	}

	r := w.h.Router
	r.Handle("/static/*", web.Static())
	r.Handle("/static/ui/*", http.StripPrefix("/static/ui", ui.Static()))
	r.Handle("/static/brand/*", brand.Static())
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		r.ServeHTTP(rw, req.WithContext(auth.WithPrincipal(req.Context(), w.h.Env.Kavya)))
	}))
	t.Cleanup(srv.Close)

	ctx := uibrowser.New(t)
	url := srv.URL + "/m/item/" + strconv.FormatInt(saved.ID, 10)
	input := `document.querySelector('input[name="lines[2].factor"]')`
	// The extra widths are the band where the rows have just lined up, which
	// screens.Widths steps over.
	widths := slices.Clone(screens.Widths)
	for width := int64(1040); width <= 1200; width += 16 {
		widths = append(widths, width)
	}
	for _, width := range widths {
		if err := uibrowser.At(ctx, width, url); err != nil {
			t.Fatal(err)
		}
		sctx, cancel := context.WithTimeout(ctx, clipStep)
		shown, err := chromedp.Run(sctx, chromedp.Evaluate[bool](`(() => {
  const e = `+input+`;
  e.value = "`+worstFactor+`";
  return e.getClientRects().length > 0 && e.scrollWidth <= e.clientWidth + 1;
})()`))
		cancel()
		if err != nil {
			t.Fatalf("%d: evaluate: %v", width, err)
		}
		if !shown {
			t.Errorf("%d: %s is clipped in the factor input", width, worstFactor)
		}
		probs, err := uibrowser.Overflow(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range probs {
			t.Errorf("%d: %s", width, p)
		}
	}
}
