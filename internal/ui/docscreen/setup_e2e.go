//go:build e2e

package docscreen

import (
	"context"

	"github.com/chromedp/chromedp"
)

// browserSubmit runs js on the page of the chromedp context ctx and waits
// until wait is visible.
func browserSubmit(ctx context.Context, js, wait string) error {
	return chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](js), chromedp.WaitVisible(chromedp.CSS(wait)))
}
