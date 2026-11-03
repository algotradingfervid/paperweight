//go:build e2e

package gallery

import "testing"

// RR1b fix 4 (sibling of the sort select): at 320 px every components.Select
// on the fields page shows its choice through a button's selectedcontent,
// and with the longest option chosen (a 100-character label) that text
// clips inside the select's end padding, never running under the arrow.
func TestGallerySelect_LongChoiceClearOfArrow(t *testing.T) {
	_, eval, _ := openFieldsB4(t, 320)
	got := eval("selects", `(() => {
  const out = [];
  const sels = [...document.querySelectorAll("select.field__input")].filter((s) => s.checkVisibility());
  if (sels.length === 0) return "no select shown";
  for (const s of sels) {
    let best = 0;
    [...s.options].forEach((o, i) => { if (o.text.length > s.options[best].text.length) best = i; });
    s.selectedIndex = best;
    const cs = getComputedStyle(s), r = s.getBoundingClientRect();
    const limit = r.right - parseFloat(cs.borderRightWidth) - parseFloat(cs.paddingRight);
    const sc = s.querySelector(":scope > button > selectedcontent");
    let end;
    if (sc) {
      end = sc.getBoundingClientRect().right;
      if (sc.textContent.trim() !== s.options[best].text.trim()) out.push(s.id + ": shows " + JSON.stringify(sc.textContent.trim()));
    } else {
      // The select draws the label itself, from its start padding.
      out.push(s.id + ": no button holding selectedcontent");
      const c = document.createElement("canvas").getContext("2d");
      c.font = cs.font;
      end = r.left + parseFloat(cs.borderLeftWidth) + parseFloat(cs.paddingLeft) + c.measureText(s.options[best].text).width;
    }
    if (end > limit + 0.5) out.push(s.id + ": text ends at " + Math.round(end) + ", under the arrow from " + Math.round(limit));
  }
  return out.join("; ");
})()`)
	if got != "" {
		t.Error(got)
	}
}
