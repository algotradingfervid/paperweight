//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// rr1bVendor is the realistic long vendor name of the UI re-review: one
// long word (Venkateswara) a narrow track breaks inside.
const rr1bVendor = "Shree Venkateswara Steel Traders Private Limited"

// rr1bEmployee is a realistic long employee name with one long word
// (Venkatasubramanian) a narrow track breaks inside (RR1b fix 3).
const rr1bEmployee = "Venkatasubramanian Ramakrishnan Iyer"

// nameHeaders are the report column labels that hold a name: such a column
// is Wide, so its words stay whole (RR1b fix 3).
var nameHeaders = []string{"Employee", "Vendor", "Item"}

// namesJS reports every visible column headed by a name label (%s, a JSON
// array) whose cells aren't Wide, or whose words break inside themselves,
// and how many such cells it measured.
const namesJS = `((names) => {
  const vis = (e) => e.checkVisibility({visibilityProperty: true});
  const out = [];
  let n = 0;
  const head = [...document.querySelectorAll(".datalist-head > span")].filter(vis);
  document.querySelectorAll("ul.datalist-rows > li a").forEach((link, row) => {
    const cells = [...link.querySelectorAll("dl")].filter((d) => vis(d) || (d.classList.contains("datalist-blank") && d.dataset.priority !== "low"));
    head.forEach((h, i) => {
      const label = h.textContent.trim();
      if (!names.includes(label) || !cells[i] || !vis(cells[i])) return;
      n++;
      if (!cells[i].classList.contains("datalist-wide")) out.push("row " + row + ": " + label + " isn't Wide");
      const walk = document.createTreeWalker(cells[i].querySelector("dd") ?? cells[i], NodeFilter.SHOW_TEXT);
      for (let t = walk.nextNode(); t; t = walk.nextNode()) {
        for (const m of t.data.matchAll(/\S+/g)) {
          const rg = document.createRange();
          rg.setStart(t, m.index);
          rg.setEnd(t, m.index + m[0].length);
          if (/^[A-Z0-9/-]+$/.test(m[0])) continue;
          if (new Set([...rg.getClientRects()].filter((q) => q.width > 0).map((q) => Math.round(q.top))).size > 1) out.push("row " + row + ": " + label + " word " + m[0] + " breaks inside");
        }
      }
    });
  });
  return {problems: out, wide: n};
})(%s)`

// reportHub is the one report route that draws no list: the GRN reports'
// index of links. reportPaths checks it holds no list.
const reportHub = "/reports/grn"

// reportPaths are every report page, from the app's own router: GET routes
// under /reports/ other than filter pages, CSV exports and the hub.
func (h *Harness) reportPaths() []string {
	var out []string
	err := chi.Walk(h.routes(), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == http.MethodGet && strings.HasPrefix(route, "/reports/") && route != reportHub &&
			!strings.HasSuffix(route, "/filter") && !strings.HasSuffix(route, ".csv") {
			out = append(out, route)
		}
		return nil
	})
	if err != nil {
		h.t.Fatal(err)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// badgesJS reports every list state badge whose word breaks inside itself,
// or whose text runs out of the badge, and how many badges it measured.
const badgesJS = `(() => {
  const out = [];
  let n = 0;
  for (const b of document.querySelectorAll(".datalist .badge")) {
    if (!b.checkVisibility()) continue;
    n++;
    const walk = document.createTreeWalker(b, NodeFilter.SHOW_TEXT);
    for (let t = walk.nextNode(); t; t = walk.nextNode()) {
      for (const m of t.data.matchAll(/\S+/g)) {
        const rg = document.createRange();
        rg.setStart(t, m.index);
        rg.setEnd(t, m.index + m[0].length);
        if (new Set([...rg.getClientRects()].filter((q) => q.width > 0).map((q) => Math.round(q.top))).size > 1) out.push("badge word " + m[0] + " breaks inside");
      }
      const rg = document.createRange();
      rg.selectNodeContents(t);
      const r = rg.getBoundingClientRect(), o = b.getBoundingClientRect();
      if (r.width > 0 && (r.top < o.top - 1 || r.bottom > o.bottom + 1 || r.right > o.right + 1)) out.push("badge " + b.innerText.trim() + " text runs out of the badge");
    }
  }
  return {problems: out, wide: n};
})()`

// RR1b finding 1 (CD-396): on every module list and the party list, at a
// phone (320, 360 and 390 px), a tablet and 1024, 1280 and 1440 px, no state
// badge breaks inside a word or spills its text out of the pill.
func TestE2E_ListBadgesNeverBreakInsideAWord(t *testing.T) {
	h := Start(t)
	h.prepareSweep()
	s := h.SignIn(admin, desktop)
	badges := 0
	for _, p := range h.listPaths() {
		for _, sz := range [][2]int64{{320, 568}, {360, 640}, {390, 844}, {768, 1024}, {1024, 800}, {1280, 900}, {1440, 900}} {
			s.openAt(sz[0], sz[1], p)
			r := Eval[columnsResult](s, badgesJS)
			for _, m := range r.Problems {
				t.Errorf("%s at %d: %s", p, sz[0], m)
			}
			badges += r.Wide
		}
	}
	if badges == 0 {
		t.Error("no state badge measured: the check saw nothing")
	}
}

// RR1b finding 2 (CD-396, R5's list rules on report tables): with a long
// realistic vendor and employee name and crore figures, every report at
// 1280 and 1440 px draws aligned rows whose headers take at most 2 lines and
// sit on their cells, whose names (Wide) never break inside a word, and
// whose badges never break; nothing scrolls sideways and no target is
// small. Every name column (Employee, Vendor, Item, by header) is Wide and
// its words stay whole (RR1b fix 3). The GRN reports hub draws no list.
func TestE2E_ReportColumns1280And1440(t *testing.T) {
	h := Start(t)
	st := h.prepareSweep()
	ctx := context.Background()
	err := h.App.DB().Write(ctx, func(tx *db.Tx) error {
		if _, err := tx.SQL().ExecContext(ctx, `UPDATE party SET name = ? WHERE id = ?`, rr1bVendor, st.long.Party); err != nil {
			return err
		}
		// The documents keep the vendor's name as saved (snapshots).
		for _, table := range []string{"purchase_order", "goods_receipt"} {
			if _, err := tx.SQL().ExecContext(ctx, `UPDATE `+table+` SET snap_vendor_name = ? WHERE snap_vendor_name = ?`, rr1bVendor, uitest.LongName100); err != nil { //nolint:gosec // G202: the two table names above
				return err
			}
		}
		// Expense claims and advances keep the employee's name as saved.
		if _, err := tx.SQL().ExecContext(ctx, `UPDATE employee SET name = ?1 WHERE id IN (SELECT employee_id FROM expense_claim UNION SELECT employee_id FROM employee_advance)`, rr1bEmployee); err != nil {
			return err
		}
		for _, table := range []string{"expense_claim", "employee_advance"} {
			if _, err := tx.SQL().ExecContext(ctx, `UPDATE `+table+` SET snap_employee_name = ?`, rr1bEmployee); err != nil { //nolint:gosec // G202: the two table names above
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s := h.SignIn(admin, desktop)
	paths := h.reportPaths()
	if len(paths) < 6 {
		t.Fatalf("report pages = %q, want the GRN, expense and purchase reports", paths)
	}
	vendor, names := false, map[string]int{}
	quoted, err := json.Marshal(nameHeaders)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range paths {
		for _, w := range []int64{1280, 1440} {
			s.openAt(w, 900, p)
			at := p + " at " + strconv.FormatInt(w, 10)
			r := Eval[columnsResult](s, columnsJS)
			for _, m := range r.Problems {
				t.Errorf("%s: %s", at, m)
			}
			for _, m := range Eval[columnsResult](s, badgesJS).Problems {
				t.Errorf("%s: %s", at, m)
			}
			nr := Eval[columnsResult](s, fmt.Sprintf(namesJS, quoted))
			for _, m := range nr.Problems {
				t.Errorf("%s: %s", at, m)
			}
			names[strings.SplitN(p, "/", 4)[2]] += nr.Wide
			layoutProblems(t, s, at)
			if s.Shows(rr1bVendor) {
				vendor = true
				if r.Wide == 0 {
					t.Errorf("%s shows the vendor in no Wide cell", at)
				}
			}
		}
	}
	if !vendor {
		t.Errorf("no report showed %q: the name check measured nothing", rr1bVendor)
	}
	for _, area := range []string{"expense", "grn", "purchase"} {
		if names[area] == 0 {
			t.Errorf("no %s report showed a name column: the header check measured nothing (%v)", area, names)
		}
	}
	s.Open(reportHub)
	if Eval[bool](s, `document.querySelector(".datalist") !== null`) {
		t.Errorf("%s draws a list: add it to reportPaths", reportHub)
	}
}
