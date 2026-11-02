//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/uibrowser"
	"github.com/vardhan/internaltools/internal/ui/wizard"
)

// stepperLabelsJS is the current page's stepper labels; run on a wizard's
// first step, where no label carries "Completed: ".
const stepperLabelsJS = `[...document.querySelectorAll("nav.stepper .stepper__label")].map((l) => l.textContent.trim())`

// stepperFitJS swaps the page's stepper for %s (its HTML as a JSON string)
// and lists what is wrong with it. Every layout: a label on more than
// one line (the current label only in the compact row), a label cut short, a
// row wider than the stepper, overlapping dots, a connector closer to one
// neighbour than the other or touching either. The compact row: the next step
// starting inside the current label. With tap: a link under 44 x 44, or one
// whose corner or mid-edge elementFromPoint finds something else on (a
// connector crosses a link only at its mid-edge). A Range has one client rect
// per line box, which noOverflow can't see.
const stepperFitJS = `(() => {
  document.querySelector("nav.stepper").outerHTML = %s;
  const tap = %t;
  const nav = document.querySelector("nav.stepper");
  const label = nav.querySelector("#step-title");
  const grid = getComputedStyle(nav.querySelector("ol")).display === "grid";
  const out = [];
  for (const l of grid ? nav.querySelectorAll(".stepper__label") : [label]) {
    const range = document.createRange();
    range.selectNodeContents(l.lastChild); // not the clipped "Completed: "
    const lines = new Set([...range.getClientRects()].map((r) => Math.round(r.top))).size;
    if (lines !== 1) out.push(lines + " lines in " + l.textContent.trim());
  }
  if (label.scrollWidth > Math.ceil(label.getBoundingClientRect().width)) out.push("label cut short");
  const end = nav.getBoundingClientRect().right;
  const steps = [...nav.querySelectorAll(".stepper__step")];
  for (const li of steps) {
    if (li.getBoundingClientRect().right > end + 0.5) out.push("row wider than the stepper");
  }
  const edges = [...nav.querySelectorAll(grid ? ".stepper__dot" : ".stepper__dot, #step-title")].map((d) => d.getBoundingClientRect());
  for (let i = 1; i < edges.length; i++) {
    if (edges[i].left < edges[i - 1].right) out.push("a dot or the label overlaps the one before it");
  }
  const labelEnd = label.getBoundingClientRect().right;
  steps.forEach((li, i) => {
    if (i === 0) return;
    const left = li.getBoundingClientRect().left;
    const before = steps[i - 1];
    const afterLabel = !grid && before.classList.contains("is-current");
    if (afterLabel && left < labelEnd - 0.5) out.push("next step starts " + (labelEnd - left).toFixed(1) + " px inside the label");
    const line = getComputedStyle(li, "::before");
    const width = parseFloat(line.width);
    if (width < 1) return;
    const lineLeft = left + parseFloat(grid ? line.left : line.marginLeft);
    const from = afterLabel ? labelEnd : before.querySelector(".stepper__dot").getBoundingClientRect().right;
    const gapBefore = lineLeft - from;
    const gapAfter = li.querySelector(".stepper__dot").getBoundingClientRect().left - lineLeft - width;
    if (Math.abs(gapBefore - gapAfter) > 2 || gapBefore < 2 || gapAfter < 2) {
      out.push("connector " + (i + 1) + " gaps " + gapBefore.toFixed(1) + " and " + gapAfter.toFixed(1));
    }
  });
  if (tap) {
    for (const a of nav.querySelectorAll("a.stepper__item")) {
      const r = a.getBoundingClientRect();
      if (r.width < 44 || r.height < 44) out.push("link " + r.width.toFixed(1) + " x " + r.height.toFixed(1));
      const [cx, cy] = [(r.left + r.right) / 2, (r.top + r.bottom) / 2];
      for (const [x, y] of [[r.left + 1, r.top + 1], [r.right - 1, r.top + 1], [r.left + 1, r.bottom - 1], [r.right - 1, r.bottom - 1],
        [r.left + 1, cy], [r.right - 1, cy], [cx, r.top + 1], [cx, r.bottom - 1]]) {
        if (document.elementFromPoint(x, y)?.closest("a") !== a) out.push("link edge taken at " + Math.round(x - r.left) + "," + Math.round(y - r.top));
      }
    }
  }
  return out.join(", ");
})()`

// stepperWorst is a wide label of wizard.MaxLabel characters.
const stepperWorst = "Measurements"

// stepperMost is the most steps Model.Validate allows.
const stepperMost = 5

// stepperTapBudgetJS is how many px the stepper is short of holding four clear
// 44 px links, a dot, its gap, the label on one line and %d px of connectors.
const stepperTapBudgetJS = `(() => {
  const span = document.createElement("span");
  span.textContent = %q;
  document.querySelector("#step-title").append(span);
  const label = span.getBoundingClientRect().width;
  span.remove();
  const dot = document.querySelector(".stepper__dot").getBoundingClientRect().width;
  return 4 * 44 + dot + 8 + label + %d - document.querySelector("nav.stepper").getBoundingClientRect().width;
})()`

// stepperTapLimit is the five-step limit, by width: with stepperWorst current,
// the connector px that put four clear 44 px links out of reach. Past it links
// overlap, so stepperTapSkipped skips the tap check for those cases only; the
// test fails if a width stops being short.
//
// 320 px: one connector's 8 px of gaps. Four clear links can't fit at any step.
// 360 px: a middle step only. The other connectors at their 16 px basis, less
// the blank target edges they sit over, add 20 px, so the row needs 330.6 px of
// 328 px; nothing is left for the current step's zero-basis connector, and the
// link before it loses 2 px. The end steps fit. So does everything from 390 px.
//
// No real wizard has five steps; a real one is always checked.
var stepperTapLimit = map[int64]int{phone320: 8, 360: 20}

// stepperTapSkipped says whether step i of labels, at width, is past that limit.
func stepperTapSkipped(width int64, labels []string, i int) bool {
	if len(labels) != stepperMost || labels[i] != stepperWorst {
		return false
	}
	return width == phone320 || (width == 360 && i > 0 && i < len(labels)-1)
}

// stepperHTML is the real Stepper for labels, with step current on screen and
// steps up to furthest reached.
func stepperHTML(t *testing.T, labels []string, current, furthest int) string {
	t.Helper()
	m := wizard.Model{Title: "Wizard", BaseURL: "/x", Current: current, Furthest: furthest}
	for i, l := range labels {
		m.Steps = append(m.Steps, wizard.Step{Slug: fmt.Sprintf("s%d", i), Label: l})
	}
	var b strings.Builder
	if err := wizard.Stepper(m).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(b.String())
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// stepperCase is one wizard's labels. Grid also runs it from 30rem of stepper
// width, where labels sit in columns.
type stepperCase struct {
	name   string
	labels []string
	grid   bool
}

// TestE2E_StepperLabelsOneLine (UI v2 §5.6, A-14): every real label of every
// wizard, as the current step, stays on one line inside the stepper at 320,
// 360 and 390 px and on both sides of 30rem, clear of the next step, with
// even connectors, 44 px links and nothing overflowing. A label at
// wizard.MaxLabel does too at every place in a real-length wizard. In the
// longest wizard Model.Validate allows it does, past stepperTapLimit on one
// line but with overlapping links.
func TestE2E_StepperLabelsOneLine(t *testing.T) {
	h := Start(t)
	deps := h.App.DocDeps()
	var cases []stepperCase
	add := func(name string, labels []string) {
		cases = append(cases, stepperCase{name: name, labels: labels, grid: true})
	}
	for _, dt := range doctype.All() {
		if dt.Master != nil || dt.TestOnly {
			continue
		}
		steps, err := docscreen.FormSteps(dt)
		if err != nil {
			t.Fatalf("%s: %v", dt.Name, err)
		}
		var labels []string
		for _, s := range steps {
			if l, ok := deps.Modules[dt.Name].StepLabels[s.Slug]; ok {
				s.Label = l
			}
			labels = append(labels, s.Label)
		}
		add(dt.Name, labels)
	}
	a := h.SignIn(admin, phone320)
	for _, path := range []string{"/m/party/new", "/m/employee/new", "/m/import/choose"} {
		a.Open(path)
		add(path, Eval[[]string](a, stepperLabelsJS))
	}
	empty := StartEmpty(t)
	empty.createAdmin()
	fresh := empty.Browser(freshAdmin, phone320)
	fresh.Open("/signin")
	fresh.Fill("email", freshAdmin)
	fresh.Fill("password", password)
	fresh.Press("Sign in")
	fresh.Until("the setup wizard", `location.pathname === "/setup/company" && document.querySelector("nav.stepper") !== null`)
	add("/setup/company", Eval[[]string](fresh, stepperLabelsJS))
	if len(cases) < 9 {
		t.Fatalf("%d wizards, want at least the 5 documents, 3 masters and setup", len(cases))
	}

	if n := utf8.RuneCountInString(stepperWorst); n != wizard.MaxLabel {
		t.Fatalf("worst case has %d characters, want wizard.MaxLabel, %d", n, wizard.MaxLabel)
	}
	for _, n := range []int{4, stepperMost} {
		for i := range n {
			w := slices.Repeat([]string{"Check"}, n)
			w[i] = stepperWorst
			cases = append(cases, stepperCase{name: fmt.Sprintf("the longest label as step %d of %d", i+1, n), labels: w, grid: n < stepperMost})
		}
	}

	for _, c := range []struct {
		width int64
		grid  bool
	}{{phone320, false}, {360, false}, {390, false}, {511, false}, {512, true}, {520, true}} {
		s := h.SignIn(admin, c.width)
		s.Open("/m/party/new")
		if got := Eval[bool](s, `getComputedStyle(document.querySelector("nav.stepper ol")).display === "grid"`); got != c.grid {
			t.Errorf("%d px: grid layout = %t, want %t", c.width, got, c.grid)
		}
		if lines, ok := stepperTapLimit[c.width]; ok {
			short := Eval[float64](s, fmt.Sprintf(stepperTapBudgetJS, stepperWorst, lines))
			t.Logf("%d px: %.1f px short of five clear steps with %q", c.width, short, stepperWorst)
			if short <= 0 {
				t.Errorf("five clear steps fit %q at %d px: take the width out of stepperTapLimit and stepperTapSkipped", stepperWorst, c.width)
			}
		}
		for _, w := range cases {
			if c.grid && !w.grid {
				continue
			}
			for i := range w.labels {
				for _, furthest := range []int{i, len(w.labels) - 1} {
					if got := Eval[string](s, fmt.Sprintf(stepperFitJS, stepperHTML(t, w.labels, i, furthest), !stepperTapSkipped(c.width, w.labels, i))); got != "" {
						t.Errorf("%d px, %s, %q current, furthest %d: %s", c.width, w.name, w.labels[i], furthest, got)
					}
					over, err := uibrowser.Overflow(s.ctx)
					if err != nil {
						t.Fatal(err)
					}
					for _, o := range over {
						t.Errorf("%d px, %s, %q current, furthest %d: %s", c.width, w.name, w.labels[i], furthest, o)
					}
				}
			}
		}
	}
}
