//go:build e2e

package docscreen_test

import (
	"strconv"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/platform/brand"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// M0-53 (CD-326 (1), (2), CD-333) in the browser: with CardsOnly the Items
// step and the check page keep their cards in the 40rem form column; on
// every card, for every type, amount and receipt combination at 360, 768
// and 1440, no word of the type or the date line is split across lines, the
// type is at least 7rem wide, and the amount is whole, right-aligned, in
// tabular figures, on the type's row or (only when it doesn't fit there) on
// its own line under the type. An image receipt's thumbnail sits left of
// the type and the date (3rem when the list is narrower than 24rem, else
// 4rem), and the actions span the card.

var (
	m53Types   = []string{"Food", "Lodging", "Site miscellaneous"}
	m53Amounts = []string{"₹4,500.00", "₹1,50,000.00", "₹10,00,00,00,000.00"}
)

// m53Combos is the number of cards: every type × amount, with and without a
// thumbnail (lines 1 to 9 have one).
const m53Combos = 18

// m53LineCard is an expense-style card for line l: its type and amount by
// combination, the date with a project code, and a receipt photo on lines
// 1 to 9.
func m53LineCard(_ docscreen.Ctx, l docscreen.LineView) components.LineCard {
	c := l.Card
	i := l.No - 1
	c.Item, c.Code, c.QtyRate, c.Amount = m53Types[i%3], "", "22-10-2026 · PRJ-METRO", m53Amounts[(i/3)%3]
	if i < 9 {
		c.Receipt = &components.Attachment{ID: int64(70 + l.No), Kind: "image", Name: "IMG_2041.jpg", Size: "2.3 MB", URL: brand.AppLogoURL}
	}
	return c
}

// m53CardsJS lists what is wrong with the cards on the page (arg: the
// number of cards).
const m53CardsJS = `((want) => {
  const out = [];
  const px = (rem) => rem * parseFloat(getComputedStyle(document.documentElement).fontSize);
  const col = document.querySelector("main .stack"), list = document.querySelector(".linelist");
  if (!col || !list) return ["no content column or line list"];
  if (col.getBoundingClientRect().width > px(40) + 0.5) out.push("the content column is " + col.getBoundingClientRect().width + " px, want at most 40rem");
  if (list.getBoundingClientRect().width > px(40) + 0.5) out.push("the line list is " + list.getBoundingClientRect().width + " px, want at most 40rem");
  // split names each word of el's text that spans more than one line.
  const split = (el, what) => {
    const walk = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
    for (let t = walk.nextNode(); t; t = walk.nextNode()) {
      for (const m of t.data.matchAll(/\S+/g)) {
        const r = document.createRange();
        r.setStart(t, m.index);
        r.setEnd(t, m.index + m[0].length);
        const tops = new Set([...r.getClientRects()].filter((x) => x.width > 0).map((x) => Math.round(x.top)));
        if (tops.size > 1) out.push(what + ": the word " + JSON.stringify(m[0]) + " is split over " + tops.size + " lines");
      }
    }
  };
  const cards = [...document.querySelectorAll("li.lines-card")];
  if (cards.length !== want) return [cards.length + " cards, want " + want];
  const narrow = list.getBoundingClientRect().width < px(24);
  cards.forEach((c, i) => {
    const what = "card " + (i + 1);
    const no = c.querySelector(".lines-no"), name = c.querySelector(".lines-name"), type = name.firstElementChild, amt = c.querySelector(".lines-amount"), meta = c.querySelector(".lines-meta");
    const cb = c.getBoundingClientRect(), cs = getComputedStyle(c);
    const left = cb.left + parseFloat(cs.paddingLeft) + parseFloat(cs.borderLeftWidth), right = cb.right - parseFloat(cs.paddingRight) - parseFloat(cs.borderRightWidth);
    const a = amt.getBoundingClientRect(), n = name.getBoundingClientRect(), d = no.getBoundingClientRect();
    split(type, what + " type " + JSON.stringify(type.textContent));
    split(meta, what + " date line");
    split(amt, what + " amount " + amt.textContent);
    if (getComputedStyle(name).overflowWrap !== "break-word") out.push(what + ": the type's overflow-wrap is " + getComputedStyle(name).overflowWrap + ", want break-word");
    if (n.width < px(7) - 0.5) out.push(what + ": the type is " + Math.round(n.width) + " px wide, want at least 7rem");
    const lh = parseFloat(getComputedStyle(amt).lineHeight) || 24;
    if (a.height > lh * 1.5) out.push(what + ": the amount takes " + Math.round(a.height) + " px, more than one line");
    if (amt.scrollWidth > amt.clientWidth + 0.5) out.push(what + ": the amount overflows its box");
    if (a.left < left - 0.5 || a.right > right + 0.5) out.push(what + ": the amount (" + Math.round(a.left) + "-" + Math.round(a.right) + ") is outside the card's content (" + Math.round(left) + "-" + Math.round(right) + ")");
    if (Math.abs(right - a.right) > 1.5) out.push(what + ": the amount's right " + Math.round(a.right) + " is not the card's content edge " + Math.round(right));
    if (!getComputedStyle(amt).fontVariantNumeric.includes("tabular-nums")) out.push(what + ": the amount's figures aren't tabular");
    if (Math.abs(a.top - n.top) > 4) {
      if (a.top < n.bottom - 1 || a.left < n.left - 0.5) out.push(what + ": the amount is neither on the type's row nor under the type");
      const gap = parseFloat(getComputedStyle(amt.parentElement).columnGap) || 0;
      if (n.left + px(7) + gap + a.width <= right - 1) out.push(what + ": the amount left the type's row though it fits there");
    } else if (a.left < n.right - 0.5) out.push(what + ": the amount overlaps the type");
    if (d.right > n.left + 0.5) out.push(what + ": the number is not left of the type");
    const thumb = c.querySelector(".lines-receipt .attachments__thumb");
    if ((i < 9) !== !!thumb) { out.push(what + ": thumbnail " + !!thumb + ", want " + (i < 9)); return; }
    if (!thumb) return;
    const t = thumb.getBoundingClientRect(), m = meta.getBoundingClientRect();
    if (t.right > n.left + 0.5 || t.right > m.left + 0.5) out.push(what + ": the thumbnail (right " + Math.round(t.right) + ") is not left of the type (" + Math.round(n.left) + ") and date (" + Math.round(m.left) + ")");
    if (t.top > m.top || t.bottom < n.top) out.push(what + ": the thumbnail is not beside the type and date");
    const tw = narrow ? 3 : 4;
    if (Math.abs(t.width - px(tw)) > 1) out.push(what + ": the thumbnail is " + t.width + " px wide, want " + tw + "rem");
    const acts = c.querySelector(".lines-actions");
    if (acts) {
      const b = acts.getBoundingClientRect();
      if (Math.abs(b.left - left) > 1 || Math.abs(b.right - right) > 1) out.push(what + ": the actions (" + Math.round(b.left) + "-" + Math.round(b.right) + ") don't span the card (" + Math.round(left) + "-" + Math.round(right) + ")");
    }
  });
  return out;
})`

// CD-326 (1), (2), CD-333: the cards' column and every card's type, date
// line, amount, thumbnail and actions, on the Items step and the check page,
// at 360, 768 and 1440.
func TestModuleBrowser_CardsOnlyLayout53(t *testing.T) {
	ctx := uibrowser.New(t)
	csp := uibrowser.CSPProblems(ctx)
	e, id := m20Env(t, m53Combos, docscreen.Module{CardsOnly: true, LineCard: m53LineCard})
	srv := m26Server(t, e)
	for _, step := range []string{"items", "check"} {
		for _, width := range []int64{360, 768, 1440} {
			what := step + " at " + strconv.FormatInt(width, 10)
			if err := uibrowser.At(ctx, width, srv+m01bURL(id)+"/edit/"+step); err != nil {
				t.Fatal(err)
			}
			problems, err := m26Step(ctx, what, chromedp.Evaluate[[]string](m53CardsJS+"("+strconv.Itoa(m53Combos)+")"))
			if err != nil {
				t.Fatalf("%s: %v", what, err)
			}
			for _, p := range problems {
				t.Errorf("%s: %s", what, p)
			}
			over, err := uibrowser.Overflow(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range over {
				t.Errorf("%s: overflow: %s", what, p)
			}
		}
	}
	for _, p := range csp() {
		t.Errorf("CSP: %s", p)
	}
}
