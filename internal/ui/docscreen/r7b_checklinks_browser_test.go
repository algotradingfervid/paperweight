//go:build e2e

package docscreen_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// r7bAtLineJS lists what is wrong once a Check link has been followed to
// line %d: the card with that id is the one numbered %d, holds focus, and
// its top is in the viewport and not under the sticky head or footer.
const r7bAtLineJS = `((id, no) => {
  const out = [];
  const t = document.getElementById(id);
  if (!t || t.querySelector(".lines-no").textContent.trim() !== String(no)) return ["#" + id + " is not card " + no];
  if (document.activeElement !== t) out.push("focus is on " + (document.activeElement.id || document.activeElement.tagName) + ", want #" + id);
  const r = t.getBoundingClientRect();
  if (r.top < 0 || r.top >= innerHeight) out.push("card " + no + "'s top " + r.top + " is outside the viewport (" + innerHeight + ")");
  const at = document.elementFromPoint(r.left + r.width / 2, Math.min(r.top + 12, innerHeight - 1));
  if (!at || !t.contains(at)) out.push("card " + no + "'s top is covered by " + (at ? at.tagName + "." + at.className : "nothing"));
  return out;
})`

// R7b in the browser: on the Check page of a 25-line draft at 320, 360,
// 390, 768 and 1280, Enter on the problem link for line 25 opens every line
// (?all=1) with focus on card 25, in view and clear of the sticky bars; the
// link for line 3 stays on the collapsed page and focuses card 3.
func TestR7bBrowser_CheckLinkFocusesTheLine(t *testing.T) {
	ctx := uibrowser.New(t)
	csp := uibrowser.CSPProblems(ctx)
	check := func(docscreen.Ctx) (docscreen.CheckExtra, error) {
		return docscreen.CheckExtra{Problems: []doctype.FieldError{
			{Field: "qty", Line: 3, Message: "Line 3: Check the quantity."},
			{Field: "qty", Line: 25, Message: "Line 25: Check the quantity."},
		}}, nil
	}
	e, id := m20Env(t, 25, docscreen.Module{Check: check})
	srv := m26Server(t, e)
	path := m01bURL(id) + "/edit/check"
	for _, width := range []int64{320, 360, 390, 768, 1280} {
		for _, c := range []struct {
			no             int
			search, cardID string
		}{{25, "?all=1", "line-25"}, {3, "", "line-3"}} {
			what := "line " + strconv.Itoa(c.no) + " at " + strconv.FormatInt(width, 10)
			if err := uibrowser.At(ctx, width, srv+path); err != nil {
				t.Fatal(err)
			}
			link := `[...document.querySelectorAll("section.card li a")].find((a) => a.textContent.trim() === "Line ` + strconv.Itoa(c.no) + `: Check the quantity.")`
			err := m26Steps(ctx, what+": Enter on the link",
				chromedp.Evaluate[chromedp.Void](link+`.focus()`),
				chromedp.KeyEvent(kb.Enter),
			)
			if err != nil {
				t.Fatalf("%s: %v", what, err)
			}
			if err := r7bArrived(ctx, what, path, c.search, c.cardID); err != nil {
				t.Fatal(err)
			}
			bad, err := m26Step(ctx, what, chromedp.Evaluate[[]string](r7bAtLineJS+`("`+c.cardID+`", `+strconv.Itoa(c.no)+`)`))
			if err != nil {
				t.Fatalf("%s: %v", what, err)
			}
			for _, p := range bad {
				t.Errorf("%s: %s", what, p)
			}
			r14Overflow(ctx, t, what)
		}
	}
	for _, p := range csp() {
		t.Errorf("CSP: %s", p)
	}
}

// r7bArrived waits, under one step deadline, for path with search and the
// card's fragment; a navigation drops the poll's page context, so it is
// polled again on the new page.
func r7bArrived(ctx context.Context, what, path, search, cardID string) error {
	deadline := time.Now().Add(uibrowser.StepTimeout)
	cond := `document.readyState === "complete" && location.pathname === "` + path + `" && location.search === "` + search + `" && location.hash === "#` + cardID + `" && document.activeElement === document.getElementById("` + cardID + `")`
	for {
		_, err := m26Step(ctx, what+": wait for #"+cardID, chromedp.Poll[chromedp.Void](cond, chromedp.WithPollingTimeout(2*time.Second)))
		if err == nil || time.Now().After(deadline) {
			return err
		}
	}
}
