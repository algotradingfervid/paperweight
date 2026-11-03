//go:build e2e

package uibrowser

import "testing"

// RR1c 1: in the gallery's lists drawn as aligned rows, at 900, 1100, 1280
// and 1440 px, a row is no taller than its tallest shown cell and the header
// no taller than its tallest shown label: a low-priority value hidden in a
// track of no width below 64rem (the sample's Name in Hindi) takes no height.
func TestGalleryBrowser_DataListRowsFitContent(t *testing.T) {
	g := startGallery(t)
	ctx := New(t)
	type result struct {
		Rows, Hidden int
		Bad          []string
	}
	rows, hidden := 0, 0
	for _, p := range []string{"datalist", "auth-users"} {
		for _, w := range []int64{900, 1100, 1280, 1440} {
			open(t, ctx, w, g.URL+"/_gallery/"+p)
			r := eval[result](t, ctx, `(() => {
  const vis = (e) => e.checkVisibility({visibilityProperty: true});
  const pad = (e, a, b) => { const s = getComputedStyle(e); return parseFloat(s[a]) + parseFloat(s[b]); };
  const r = {Rows: 0, Hidden: 0, Bad: []};
  document.querySelectorAll(".datalist").forEach((l) => {
    const head = l.querySelector(":scope > .datalist-head");
    if (!head || getComputedStyle(head).display === "none") return;
    const labels = [...head.children].filter(vis);
    const tallest = Math.max(0, ...labels.map((s) => s.getBoundingClientRect().height));
    if (head.getBoundingClientRect().height > tallest + pad(head, "paddingTop", "paddingBottom") + 2) r.Bad.push("header is " + Math.round(head.getBoundingClientRect().height) + " px, its labels " + Math.round(tallest));
    l.querySelectorAll("ul.datalist-rows > li > a.datalist-link").forEach((a, i) => {
      r.Rows++;
      const cells = [...a.querySelectorAll("dl, p.datalist-cell")];
      r.Hidden += cells.filter((c) => c.dataset.priority === "low" && !vis(c)).length;
      const box = a.getBoundingClientRect();
      const bottom = Math.max(box.top, ...cells.filter(vis).map((c) => c.getBoundingClientRect().bottom));
      const want = Math.max(bottom - box.top + parseFloat(getComputedStyle(a).paddingBottom), parseFloat(getComputedStyle(a).minHeight) || 0);
      if (box.height > want + 2) r.Bad.push("row " + i + " is " + Math.round(box.height) + " px, its cells need " + Math.round(want));
    });
  });
  return r;
})()`)
			rows += r.Rows
			hidden += r.Hidden
			for _, b := range r.Bad {
				t.Errorf("%s@%d: %s", p, w, b)
			}
		}
	}
	if rows == 0 || hidden == 0 {
		t.Errorf("measured %d aligned rows and %d hidden low-priority values, want some of each", rows, hidden)
	}
}
