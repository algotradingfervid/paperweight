//go:build e2e

package gallery

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// §5.11: at every width the line card's actions keep 44 px targets, with
// nothing outside the card or over another control; the move buttons are
// square and stay together at the end of the row; Remove's text is the
// danger-700 colour, on a card and on the grid; and the row is one line of
// buttons from 360 px, so a card is no taller for them.
func TestLinesBrowser_ActionsAtEveryWidth(t *testing.T) {
	ctx := uibrowser.New(t)
	s := startAttach(t)
	for _, width := range []int64{320, 360, 390, 768, 1280} {
		if err := uibrowser.At(ctx, width, s.URL+"/_gallery/lines"); err != nil {
			t.Fatal(err)
		}
		e7bOverflow(t, ctx, "lines")
		got := attachEval[[]string](t, ctx, `(() => {
  const out = [];
  const probe = document.body.appendChild(document.createElement("span"));
  probe.style.color = "var(--color-danger-700)";
  const danger = getComputedStyle(probe).color;
  probe.remove();
  const seen = [...document.querySelectorAll(".lines-actions, .linegrid-actions")].filter((a) => a.getClientRects().length > 0);
  if (seen.length < 100) out.push(seen.length + " action rows shown");
  for (const a of seen.slice(0, 40)) {
    const box = a.getBoundingClientRect();
    const bs = [...a.querySelectorAll(".btn")];
    const rs = bs.map((b) => b.getBoundingClientRect());
    bs.forEach((b, i) => {
      const r = rs[i], name = b.getAttribute("aria-label");
      if (r.width < 44 || r.height < 44) out.push(name + " is " + r.width + " x " + r.height);
      if (r.left < box.left - 0.5 || r.right > box.right + 0.5) out.push(name + " leaves its row");
      rs.slice(i + 1).forEach((o, j) => {
        if (r.left < o.right && o.left < r.right && r.top < o.bottom && o.top < r.bottom) out.push(name + " covers " + bs[i + 1 + j].getAttribute("aria-label"));
      });
      if (name.startsWith("Move") && Math.abs(r.width - r.height) > 0.5) out.push(name + " isn't square: " + r.width + " x " + r.height);
      if (name.startsWith("Remove") && getComputedStyle(b).color !== danger) out.push(name + " is " + getComputedStyle(b).color + ", not the danger text " + danger);
    });
    const moves = bs.filter((b) => b.hasAttribute("data-move")).map((b) => b.getBoundingClientRect());
    if (moves.length === 2 && moves[0].top !== moves[1].top) out.push("the move buttons split");
    if (a.matches(".lines-actions") && moves.length > 0 && Math.abs(moves[moves.length - 1].right - box.right) > 1) out.push("the move buttons aren't at the row's end");
    if (innerWidth >= 360 && a.matches(".lines-actions") && new Set(rs.map((r) => r.top)).size > 1) out.push("actions wrap at " + innerWidth);
  }
  return out;
})()`)
		if len(got) != 0 {
			t.Errorf("at %d px: %s", width, strings.Join(got, "; "))
		}
	}
}
