//go:build e2e

package e2e

import (
	"strconv"
	"testing"
)

// sortTextJS reports when the sort select's shown text reaches into its end
// padding, where the arrow is drawn. The text is the selectedcontent
// element when the select has one (it clips with an ellipsis), else the
// chosen option's label measured in the select's font from its start
// padding, as the browser draws it.
const sortTextJS = `(() => {
  const s = document.getElementById("filter-sort");
  if (!s || !s.checkVisibility()) return "no sort select shown";
  const cs = getComputedStyle(s), r = s.getBoundingClientRect();
  const limit = r.right - parseFloat(cs.borderRightWidth) - parseFloat(cs.paddingRight);
  const sc = s.querySelector("selectedcontent");
  let end;
  if (sc) {
    end = sc.getBoundingClientRect().right;
  } else {
    const c = document.createElement("canvas").getContext("2d");
    c.font = cs.font;
    end = r.left + parseFloat(cs.borderLeftWidth) + parseFloat(cs.paddingLeft) + c.measureText(s.options[s.selectedIndex].text).width;
  }
  return end > limit + 0.5 ? "sort text ends at " + Math.round(end) + ", under the arrow from " + Math.round(limit) : "";
})()`

// RR1b finding 6: at 320, 360 and 390 px, on every module list and the
// party list, the sort select's text never runs under its arrow, with the
// longest sort label chosen as well as the default.
func TestE2E_SortTextClearOfArrow(t *testing.T) {
	h := Start(t)
	h.prepareSweep()
	s := h.SignIn(admin, phone390)
	for _, w := range []int64{320, 360, 390} {
		for _, p := range h.listPaths() {
			s.openAt(w, 700, p)
			at := p + " at " + strconv.FormatInt(w, 10)
			if m := Eval[string](s, sortTextJS); m != "" {
				t.Errorf("%s: %s", at, m)
			}
			// The longest label, chosen without sending the form.
			Eval[bool](s, `(() => {
  const s = document.getElementById("filter-sort");
  let best = 0;
  [...s.options].forEach((o, i) => { if (o.text.length > s.options[best].text.length) best = i; });
  s.selectedIndex = best;
  return true;
})()`)
			if m := Eval[string](s, sortTextJS); m != "" {
				t.Errorf("%s, longest label: %s", at, m)
			}
		}
	}
}
