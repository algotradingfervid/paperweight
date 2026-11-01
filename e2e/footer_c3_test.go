//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/screens"
	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// footerJS focuses each visible field of the wizard step in front of its
// footer (#wizard .actionbar) and lists those whose box ends below the
// footer's top edge. It returns null on a page without a wizard footer.
const footerJS = `(() => {
  const bar = document.querySelector("#wizard .actionbar");
  if (!bar) return null;
  const out = [];
  const fields = [...document.querySelectorAll("#wizard :is(input:not([type=hidden]), select, textarea)")]
    .filter((el) => (el.compareDocumentPosition(bar) & Node.DOCUMENT_POSITION_FOLLOWING) && el.getClientRects().length > 0);
  for (const el of fields) {
    el.focus();
    if (document.activeElement !== el) continue;
    const r = el.getBoundingClientRect();
    if (r.width === 0 || r.height === 0) continue;
    const top = bar.getBoundingClientRect().top;
    if (r.bottom > top + 0.5) out.push((el.id || el.name || el.type) + ": bottom " + Math.round(r.bottom) + " > footer top " + Math.round(top));
  }
  return out;
})()`

// a12Wizards are the wizards A-12 must reach, by the URL part that names
// them; each needs at least one checked step.
var a12Wizards = []string{
	"/doc/" + purchase.DocType + "/",
	"/doc/" + goodsreceipt.DocType + "/",
	"/doc/" + expense.ClaimDocType + "/",
	"/doc/" + fixture.Order + "/",
	"/m/party/new",
	"/m/import",
	"/setup/company",
}

// TestE2E_FooterNeverCoversFields (A-12, UI v2 §5.6): on every wizard step of
// the screens sweep (docscreen modules, master wizards, the setup wizard and
// import) at 390 × 664, focusing each field leaves its bottom edge above the
// sticky footer's top edge.
func TestE2E_FooterNeverCoversFields(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]int{}
	page := func(ctx context.Context, s screens.Screen) []string {
		// A user's page has focus, so focus events fire (headless has none).
		if _, err := chromedp.Call(ctx, emulation.SetFocusEmulationEnabled, emulation.SetFocusEmulationEnabledParams{Enabled: true}); err != nil {
			return []string{"focus emulation: " + err.Error()}
		}
		_, err := chromedp.Run(ctx, chromedp.EmulateViewport(phone390, 664, chromedp.EmulateMobile, chromedp.EmulateTouch))
		if err != nil {
			return []string{"viewport: " + err.Error()}
		}
		got, err := chromedp.Run(ctx, chromedp.Evaluate[*[]string](footerJS))
		if err != nil {
			return []string{"footer check: " + err.Error()}
		}
		if got == nil {
			return nil
		}
		var loc string
		if v, err := chromedp.Run(ctx, chromedp.Evaluate[string](`location.pathname`)); err == nil {
			loc = v
		}
		mu.Lock()
		seen[loc]++
		mu.Unlock()
		return *got
	}
	sweep(t, uibrowser.Options{Widths: []int64{phone390}, Page: page})
	mu.Lock()
	defer mu.Unlock()
	for _, w := range a12Wizards {
		n := 0
		for loc, c := range seen {
			if strings.Contains(loc, w) {
				n += c
			}
		}
		if n == 0 {
			t.Errorf("A-12 checked no step of %s (steps seen: %v)", w, seen)
		}
	}
	t.Logf("A-12 checked %d wizard pages", len(seen))
}

// cueText is the save cue's visible text.
func cueText(s *Session) string {
	s.t.Helper()
	return Eval[string](s, `document.getElementById("save-cue").innerText.replace(/\s+/g, " ").trim()`)
}

// wantSaved fails unless the cue says "Saved {HH:MM}" with the app clock's
// time in IST (the harness's fixed clock reaches wizard.Respond).
func wantSaved(t *testing.T, s *Session, what string) {
	t.Helper()
	s.Until(what+": the cue says Saved", `/^Saved \d\d:\d\d$/.test(document.getElementById("saved-at").textContent)`)
	want := "Saved " + s.h.Clock.Now().In(types.IST).Format("15:04")
	if got := cueText(s); got != want {
		t.Errorf("%s: cue = %q, want %q", what, got, want)
	}
}

// TestE2E_SaveCue (UI v2 §5.6, N12): a new document's cue says "Not saved
// yet"; after a step save, and after Back, it says "Saved {HH:MM}" in IST,
// read from the live region. A saved event changes only the cue: the field
// being typed in keeps its value and focus. In focus mode the app header is
// hidden below 64rem and shown from 64rem, and the wizard head's h1 names
// the document.
func TestE2E_SaveCue(t *testing.T) {
	h := Start(t)
	p := h.SignIn(priya, phone390)
	p.Open("/doc/" + fixture.Order + "/new")
	if got := cueText(p); got != "Not saved yet" {
		t.Errorf("new document cue = %q, want Not saved yet", got)
	}
	// No close × yet on docscreen wizards (C3b): the header stays, so the
	// page is no dead end.
	if got := Eval[string](p, `getComputedStyle(document.querySelector(".app-header")).display`); got == "none" {
		t.Error("app header hidden at 390 on a focus page without a close ×, want shown")
	}
	orderDetails(p)
	p.Press(saveContinue)
	p.WaitText("Step 2 of 3")
	wantSaved(t, p, "after Save and continue")
	id := p.DocID()
	if got := Eval[string](p, `document.querySelector("#wizard h1").innerText`); got != draft(id) {
		t.Errorf("h1 after the first save = %q, want %q", got, draft(id))
	}
	if got := Eval[string](p, `document.getElementById("saved-at").getAttribute("aria-live")`); got != "polite" {
		t.Errorf("#saved-at aria-live = %q, want polite", got)
	}

	// Back saves too; mark the cue so a fresh "Saved" can be told apart.
	Eval[bool](p, `(document.getElementById("saved-at").textContent = "", true)`)
	p.Until("the items step settled", `document.activeElement && document.activeElement.id === "step-title"`)
	h.DelayPosts(fmt.Sprintf("/doc/%s/%d/edit/items", fixture.Order, id), 1500*time.Millisecond)
	p.Press("Back")
	p.Until("the cue says Saving…", `document.getElementById("save-cue").innerText.trim() === "Saving…"`)
	p.WaitText("Step 1 of 3")
	wantSaved(t, p, "after Back")
	h.DelayPosts("/no-such-path", 0)

	// A saved event while typing writes the cue only.
	p.Until("the settle", `document.activeElement && document.activeElement.id === "step-title"`)
	p.FillID("f-remarks", "Bay 3")
	Eval[bool](p, `(document.body.dispatchEvent(new CustomEvent("saved", {bubbles: true, detail: {value: "Saved 09:15"}})), true)`)
	if got := cueText(p); got != "Saved 09:15" {
		t.Errorf("cue after a saved event = %q, want Saved 09:15", got)
	}
	if got := Eval[string](p, `document.activeElement.id + "=" + document.activeElement.value`); got != "f-remarks=Bay 3" {
		t.Errorf("typed field after a saved event = %q, want f-remarks=Bay 3 still focused", got)
	}

	// With a close × in the wizard head the header hides below 64rem and
	// shows from 64rem. Docscreen gets its × in C3b; one is added here.
	p.Do("add a close ×", chromedp.Evaluate[chromedp.Void](`(() => { const a = document.createElement("a"); a.className = "wizard-close"; a.href = "/"; a.textContent = "Close"; document.querySelector(".wizard-head").prepend(a); })()`))
	for _, c := range []struct {
		width int64
		want  bool
	}{{1000, false}, {1024, true}} {
		p.Do("resize", chromedp.EmulateViewport(c.width, 800))
		if got := Eval[string](p, `getComputedStyle(document.querySelector(".app-header")).display`); (got != "none") != c.want {
			t.Errorf("app header display at %d with a close × = %q, want shown %v", c.width, got, c.want)
		}
	}

	d := h.SignIn(priya, desktop)
	d.Open(fmt.Sprintf("/doc/%s/%d/edit/details", fixture.Order, id))
	if got := Eval[string](d, `getComputedStyle(document.querySelector(".app-header")).display`); got == "none" {
		t.Error("app header hidden at 64rem+ in focus mode, want shown")
	}
	if got := cueText(d); got != "" {
		t.Errorf("a saved draft's cue on opening = %q, want empty", got)
	}
}
