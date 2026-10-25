//go:build e2e

package docscreen

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/chromedp"
)

// setupTimeout bounds a screen's Setup, so a page that never shows what it
// waits for fails the sweep instead of hanging it.
const setupTimeout = 15 * time.Second

// browserSubmit runs js on the page of the chromedp context ctx and waits
// until wait is visible, for at most setupTimeout.
func browserSubmit(ctx context.Context, js, wait string) error {
	ctx, cancel := context.WithTimeout(ctx, setupTimeout)
	defer cancel()
	if err := chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](js), chromedp.WaitVisible(chromedp.CSS(wait))); err != nil {
		return fmt.Errorf("docscreen: setup waiting for %s: %w", wait, err)
	}
	return nil
}
