//go:build e2e

package docscreen_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/vardhan/internaltools/internal/platform/brand"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// m26Filled is the note the Body's Receive all pending writes on every line,
// so a test sees whether that button was pressed.
const m26Filled = "m26 filled"

// m26Server serves e's router with the bundle, as the app does, and returns
// its URL. The router is read on every request, so a later m02Mount counts.
func m26Server(t *testing.T, e *hookedEnv) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("/static/ui/", http.StripPrefix("/static/ui", ui.Static()))
	mux.Handle("/static/brand/", brand.Static())
	mux.Handle("/static/", web.Static())
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { e.h.ServeHTTP(w, r) }))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// m26Save is the Items Save: Receive all pending (m26_fill=all) sets every
// line to 999 with note m26Filled; otherwise each line gets its typed
// quantity, by id.
func m26Save(c docscreen.Ctx) (docscreen.Save, error) {
	fill := c.Req.PostFormValue("m26_fill") == "all"
	var lines []map[string]string
	for i, l := range c.View.Lines {
		if fill {
			lines = append(lines, map[string]string{"id": l.ID, "qty": "999", "note": m26Filled})
			continue
		}
		lines = append(lines, map[string]string{"id": l.ID, "qty": c.Req.PostFormValue("m26_qty_" + strconv.Itoa(i+1))})
	}
	return docscreen.Save{Lines: lines}, nil
}

// m26Step runs a under one step deadline, naming what on a timeout.
func m26Step[T any](ctx context.Context, what string, a chromedp.Action[T]) (T, error) {
	sctx, cancel := context.WithTimeoutCause(ctx, uibrowser.StepTimeout, fmt.Errorf("%s: step deadline %v exceeded: %w", what, uibrowser.StepTimeout, context.DeadlineExceeded))
	defer cancel()
	v, err := chromedp.Run(sctx, a)
	if err != nil && sctx.Err() != nil {
		err = context.Cause(sctx)
	}
	return v, err
}

// m26Steps runs acts in order under one step deadline.
func m26Steps(ctx context.Context, what string, acts ...chromedp.Action[chromedp.Void]) error {
	sctx, cancel := context.WithTimeoutCause(ctx, uibrowser.StepTimeout, fmt.Errorf("%s: step deadline %v exceeded: %w", what, uibrowser.StepTimeout, context.DeadlineExceeded))
	defer cancel()
	err := chromedp.Do(sctx, acts...)
	if err != nil && sctx.Err() != nil {
		err = context.Cause(sctx)
	}
	return err
}

// m26First is the step form's first submit, the button Enter presses, as the
// browser lays it out.
type m26First struct {
	Found, Copy        bool
	Display            string
	Width, Height      float64
	Name, Value, Label string
}

const m26MeasureFirst = `(() => {
  const f = document.getElementById("wizard-form");
  const c = f && [...f.elements].find(e => e.type === "submit");
  if (!c) return {Found: false};
  const r = c.getBoundingClientRect();
  return {
    Found: true,
    Copy: c.getAttribute("aria-hidden") === "true" && c.tabIndex === -1 && c.textContent.trim() === "",
    Display: getComputedStyle(c).display,
    Width: r.width, Height: r.height,
    Name: c.name, Value: c.value, Label: c.textContent.trim(),
  };
})()`

// m26Num is a stored quantity without trailing decimal zeros ("7.000" → "7").
func m26Num(q string) string {
	if strings.Contains(q, ".") {
		q = strings.TrimRight(strings.TrimRight(q, "0"), ".")
	}
	return q
}

// HOOK-046: on an Items step whose Body has its own submit (Receive all
// pending), Enter in a Body input presses the primary: the step saves the
// typed quantity and moves to Check, or with a nav=stay primary
// redraws Items; Receive all pending's effect is absent. The form's first
// submit is the Enter copy, laid out 0×0 but not display:none, and the page
// has no small target, no overflow and no CSP problem, at a phone and a
// desktop width.
func TestModuleBrowser_EnterPressesPrimary(t *testing.T) {
	ctx := uibrowser.New(t)
	csp := uibrowser.CSPProblems(ctx)
	for _, c := range []struct {
		name string
		stay bool
	}{{"save and continue", false}, {"nav=stay", true}} {
		for _, width := range []int64{360, 1280} {
			t.Run(c.name+"/"+strconv.FormatInt(width, 10), func(t *testing.T) {
				e, _, id := m02Env(t)
				step := docscreen.Step{Body: m26Body(m26BodyOpts{button: true}), Save: m26Save}
				if c.stay {
					step.Bar = func(docscreen.Ctx, docscreen.StepState) (docscreen.StepBar, error) {
						return docscreen.StepBar{Primary: "Receive and stay", Stay: true}, nil
					}
				}
				m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"items": step}})
				srv := m26Server(t, e)
				items := m02URL(id) + "/edit/items"
				before := m02Rec(t, e, id)

				if err := uibrowser.At(ctx, width, srv+items); err != nil {
					t.Fatal(err)
				}
				first, err := m26Step(ctx, "measure the first submit", chromedp.Evaluate[m26First](m26MeasureFirst))
				if err != nil {
					t.Fatal(err)
				}
				switch {
				case !first.Found:
					t.Errorf("#wizard-form has no submit")
				case !first.Copy:
					t.Errorf("the form's first submit is %q (name %q), want the empty, aria-hidden, tabindex -1 Enter copy", first.Label, first.Name)
				}
				if first.Display == "none" || first.Width != 0 || first.Height != 0 {
					t.Errorf("the form's first submit is display %q, %.1f×%.1f px; want laid out 0×0, not display:none", first.Display, first.Width, first.Height)
				}
				wantName, wantValue := "", ""
				if c.stay {
					wantName, wantValue = "nav", "stay"
				}
				if first.Name != wantName || first.Value != wantValue {
					t.Errorf("the form's first submit posts %q=%q, want the primary's %q=%q", first.Name, first.Value, wantName, wantValue)
				}
				small, err := uibrowser.SmallTargets(ctx)
				if err != nil {
					t.Fatal(err)
				}
				for _, p := range small {
					t.Errorf("small target: %s", p)
				}
				over, err := uibrowser.Overflow(ctx)
				if err != nil {
					t.Fatal(err)
				}
				for _, p := range over {
					t.Errorf("overflow: %s", p)
				}

				// Enter in line 1's input; then wait for either answer: Check
				// and submit, or Items redrawn at the next version.
				const inputSel = `#m26-body input[name="m26_qty_1"]`
				input := chromedp.CSS(inputSel)
				next := strconv.Itoa(before.Version + 1)
				if err := m26Steps(ctx, "type 7 and press Enter",
					chromedp.Evaluate[chromedp.Void](`document.querySelector('`+inputSel+`').value = ""`),
					chromedp.SendKeys(input, "7"+kb.Enter),
					chromedp.WaitReady(chromedp.CSS(`#wizard-form[action$="/submit"], #wizard-form[action$="/edit/items"] #doc-version[value="`+next+`"]`)),
				); err != nil {
					t.Fatal(err)
				}
				heading, err := m26Step(ctx, "read the heading", chromedp.Text(chromedp.CSS("h2#step-title")))
				if err != nil {
					t.Fatal(err)
				}
				want := "Check"
				if c.stay {
					want = "Items"
				}
				if got := norm(heading); got != want {
					t.Errorf("after Enter the heading is %q, want %q", got, want)
				}

				after := m02Rec(t, e, id)
				if after.Version != before.Version+1 {
					t.Errorf("stored version %d, want %d: Enter saves once", after.Version, before.Version+1)
				}
				for i, l := range after.Lines {
					if l["note"] == m26Filled {
						t.Errorf("line %d has note %q: Enter pressed %s, want the primary", i+1, m26Filled, m26FillLabel)
					}
				}
				if len(after.Lines) != 2 || m26Num(after.Lines[0]["qty"]) != "7" || after.Lines[1]["qty"] != before.Lines[1]["qty"] {
					t.Errorf("stored lines %v, want line 1 the typed 7 and line 2 unchanged %q", after.Lines, before.Lines[1]["qty"])
				}
				for _, p := range csp() {
					t.Errorf("CSP: %s", p)
				}
			})
		}
	}
}
