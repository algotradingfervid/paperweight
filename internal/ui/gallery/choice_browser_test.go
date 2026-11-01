//go:build e2e

package gallery

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// openFieldsB4 opens /_gallery/fields at width w and returns step helpers.
func openFieldsB4(t *testing.T, w int64) (context.Context, func(string, string) string, func(string, ...chromedp.Action[chromedp.Void])) {
	t.Helper()
	s := httptest.NewServer(Handler())
	t.Cleanup(s.Close)
	ctx := uibrowser.New(t)
	if err := uibrowser.At(ctx, w, s.URL+"/_gallery/fields"); err != nil {
		t.Fatal(err)
	}
	eval := func(what, js string) string {
		t.Helper()
		sctx, cancel := context.WithTimeout(ctx, uibrowser.StepTimeout)
		defer cancel()
		got, err := chromedp.Run(sctx, chromedp.Evaluate[string](js))
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		return got
	}
	do := func(what string, a ...chromedp.Action[chromedp.Void]) {
		t.Helper()
		sctx, cancel := context.WithTimeout(ctx, uibrowser.StepTimeout)
		defer cancel()
		if err := chromedp.Do(sctx, a...); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	return ctx, eval, do
}

// axChecked is the accessibility tree's role and checked state for the
// element with id: what a screen reader hears.
func axChecked(t *testing.T, ctx context.Context, id string) string {
	t.Helper()
	sctx, cancel := context.WithTimeout(ctx, uibrowser.StepTimeout)
	defer cancel()
	nodes, err := chromedp.Run(sctx, chromedp.Nodes(chromedp.CSS("#"+id)))
	if err != nil || len(nodes) == 0 {
		t.Fatalf("find #%s: %v", id, err)
	}
	no := false
	res, err := chromedp.Call(sctx, accessibility.GetPartialAXTree, accessibility.GetPartialAXTreeParams{BackendNodeID: nodes[0].BackendNodeID, FetchRelatives: &no})
	if err != nil || len(res.Nodes) == 0 {
		t.Fatalf("AX node for #%s: %v", id, err)
	}
	n := res.Nodes[0]
	role, checked := "", ""
	if n.Role != nil {
		role = strings.Trim(string(n.Role.Value), `"`)
	}
	for _, p := range n.Properties {
		if p.Name == accessibility.PropertyNameChecked && p.Value != nil {
			checked = strings.Trim(string(p.Value.Value), `"`)
		}
	}
	return role + " " + checked
}

// The switch rows post as a Check does, both ways, and an inverted switch
// posts the stored meaning first, both ways; the row is the ≥ 56 px target,
// the switch 48 × 28, and the accessibility tree hears a switch whose checked
// state follows each tap (UI v2 §5.5 item 7, N6).
func TestSwitchBrowser_PostsAndAnnounces(t *testing.T) {
	ctx, eval, do := openFieldsB4(t, 390)
	// Wrap both rows in a form, so FormData says what a submit would send.
	eval("wrap", `(() => { const f = document.createElement("form"); f.id = "uiv2b4-form";
		const a = document.getElementById("f-stock").closest(".field"), b = document.getElementById("f-active").closest(".field");
		a.before(f); f.append(a, b); return ""; })()`)
	posts := func(name string) string {
		return eval("FormData", `new FormData(document.getElementById("uiv2b4-form")).getAll(`+"\""+name+"\""+`).join(",")`)
	}
	first := func(name string) string {
		return eval("first value", `String(new URLSearchParams(new FormData(document.getElementById("uiv2b4-form"))).get(`+"\""+name+"\""+`))`)
	}
	tapLabel := func(id string) {
		do("tap "+id, chromedp.Click(chromedp.CSS("label:has(#"+id+") .switch-row__label")))
	}

	if got := posts("f_stock"); got != "1" {
		t.Errorf("stock on posts %q, want 1", got)
	}
	if got := axChecked(t, ctx, "f-stock"); got != "switch true" {
		t.Errorf("AX = %q, want switch true", got)
	}
	tapLabel("f-stock")
	if got := posts("f_stock"); got != "" {
		t.Errorf("stock off posts %q, want nothing", got)
	}
	if got := axChecked(t, ctx, "f-stock"); got != "switch false" {
		t.Errorf("AX after tap = %q, want switch false", got)
	}

	// Inverted: stored "0" (not disabled) shows on.
	if got := eval("inverted on", `String(document.getElementById("f-active").checked)`); got != "true" {
		t.Errorf("inverted switch over stored 0 is checked = %s, want true", got)
	}
	if got := first("f_disabled"); got != "0" {
		t.Errorf("inverted on: the form reads %q, want 0", got)
	}
	tapLabel("f-active")
	if got := first("f_disabled"); got != "1" {
		t.Errorf("inverted off: the form reads %q, want 1", got)
	}
	tapLabel("f-active")
	if got := first("f_disabled"); got != "0" {
		t.Errorf("inverted on again: the form reads %q, want 0", got)
	}

	if got := eval("sizes", `(() => { const s = document.getElementById("f-stock").getBoundingClientRect(), r = document.getElementById("f-stock").closest("label").getBoundingClientRect();
		return Math.round(s.width) + "x" + Math.round(s.height) + " " + (r.height >= 56) + " " + (r.width >= 300); })()`); got != "48x28 true true" {
		t.Errorf("switch and row = %s, want 48x28 true true", got)
	}
}

// Segmented and tiles are native radio groups: arrow keys move the choice
// (UI v2 §5.5 item 8).
func TestSegmentedBrowser_ArrowKeys(t *testing.T) {
	_, eval, do := openFieldsB4(t, 390)
	checked := func(name string) string {
		return eval("checked", `document.querySelector('input[name="`+name+`"]:checked').value`)
	}
	do("focus priority", chromedp.Evaluate[chromedp.Void](`document.getElementById("f-priority").focus()`), chromedp.KeyEvent(kb.ArrowRight))
	if got := checked("f_priority"); got != "Urgent" {
		t.Errorf("after ArrowRight priority = %q, want Urgent", got)
	}
	do("left", chromedp.KeyEvent(kb.ArrowLeft))
	if got := checked("f_priority"); got != "Normal" {
		t.Errorf("after ArrowLeft priority = %q, want Normal", got)
	}
	do("focus type", chromedp.Evaluate[chromedp.Void](`document.querySelector('input[name="f_type"]:checked').focus()`), chromedp.KeyEvent(kb.ArrowDown))
	if got := checked("f_type"); got != "food" {
		t.Errorf("after ArrowDown type = %q, want food", got)
	}
	if got := eval("thumb", `getComputedStyle(document.querySelector('input[name="f_type"]:checked').closest("label").querySelector(".segmented__check")).display`); got == "none" {
		t.Error("the chosen tile hides its check icon")
	}
	if got := eval("others", `getComputedStyle(document.querySelector('input[name="f_type"]:not(:checked)').closest("label").querySelector(".segmented__check")).display`); got != "none" {
		t.Errorf("an unchosen tile shows its check icon (display %s)", got)
	}
	if got := eval("tiles columns", `String(getComputedStyle(document.querySelector("#f-type").closest(".segmented")).gridTemplateColumns.split(" ").length)`); got != "2" {
		t.Errorf("tiles have %s columns, want 2", got)
	}
}

// No overflow and no axe violation on the fields page at 320 px, with the
// switch rows, segmented control and tiles in their worst cases.
func TestChoiceBrowser_320OverflowAndAxe(t *testing.T) {
	ctx, _, _ := openFieldsB4(t, 320)
	probs, err := uibrowser.Overflow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range probs {
		t.Errorf("overflow at 320: %s", p)
	}
	vs, err := uibrowser.Axe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		t.Errorf("axe %s: %s: %s", v.ID, v.Help, strings.Join(v.Nodes, ", "))
	}
	small, err := uibrowser.SmallTargets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range small {
		t.Errorf("target smaller than 44px at 320: %s", s)
	}
}
