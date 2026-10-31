//go:build e2e

package uibrowser

import (
	"context"
	_ "embed"

	"github.com/chromedp/chromedp"
)

//go:embed axe.min.js
var axeSource string

// Violation is one failed axe rule with the selectors of its nodes.
type Violation struct {
	ID    string   `json:"id"`
	Help  string   `json:"help"`
	Nodes []string `json:"nodes"`
}

type axeResult struct {
	Violations []Violation `json:"violations"`
}

const axeRunJS = `axe.run(document, {runOnly: {type: "tag", values: ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"]}})
  .then(r => ({violations: r.violations.map(v => ({id: v.id, help: v.help, nodes: v.nodes.map(n => n.target.join(" "))}))}))`

// axeTopJS sets data-axe on html (the app CSS then makes the sticky header
// and action bar static), scrolls to the top and waits two frames for the
// layout to settle.
const axeTopJS = `new Promise((done) => { document.documentElement.setAttribute("data-axe", ""); window.scrollTo({top: 0, left: 0, behavior: "instant"}); requestAnimationFrame(() => requestAnimationFrame(() => done())); })`

// axeDoneJS removes data-axe again.
const axeDoneJS = `document.documentElement.removeAttribute("data-axe")`

// Axe injects axe-core 4.14.0 into the current page and returns its WCAG 2.2
// AA violations. It runs at the top of the page with the sticky app header
// and action bar static (CD-344 (3)): content under sticky chrome, a link an
// earlier step left half under the header or a field under the bar at load,
// overlaps the chrome's buttons, a false target-size failure.
func Axe(ctx context.Context) ([]Violation, error) {
	if err := chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](axeTopJS, chromedp.EvalAwaitPromise)); err != nil {
		return nil, err
	}
	if err := chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](axeSource)); err != nil {
		return nil, err
	}
	res, err := chromedp.Run(ctx, chromedp.Evaluate[axeResult](axeRunJS, chromedp.EvalAwaitPromise))
	if err != nil {
		return nil, err
	}
	return res.Violations, chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](axeDoneJS))
}
