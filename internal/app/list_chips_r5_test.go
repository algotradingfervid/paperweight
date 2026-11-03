package app_test

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// R5 (UI v2 §5.12, A-23; lessons: derived lists apply the viewer's scope): on
// the real app every module list, taken from App.DocDeps, has a Filters
// control and quick chips from its DocType's state field. Signed in as the
// visibility sweep's restricted viewer (project A and own expense records),
// each chip (draft, pending, approved: the states work moves through) counts
// exactly the readable seeded records in that state,
// never a hidden one, and following the chip lists that many.
func TestApp_ListQuickChipsEveryModuleScoped(t *testing.T) {
	s := seedSweep(t)
	v := s.w.signedIn(viewerEmail)
	mods := s.w.app.DocDeps().Modules
	if len(mods) == 0 {
		t.Fatal("App.DocDeps has no modules")
	}
	hiddenCounted := 0
	quick := []string{string(doctype.StateDraft), string(doctype.StatePending), string(doctype.StateApproved)}
	for _, name := range slices.Sorted(maps.Keys(mods)) {
		dt, ok := doctype.Lookup(name)
		if !ok {
			t.Fatalf("module %s has no DocType", name)
		}
		readable, hidden := map[string]int{}, map[string]int{}
		for _, r := range s.records {
			if r.dt != name {
				continue
			}
			state := s.w.text(`SELECT state FROM `+dt.Table+` WHERE id = ?`, r.id)
			if r.hidden {
				hidden[state]++
			} else {
				readable[state]++
			}
		}
		state, ok := dt.Field("state")
		if !ok {
			t.Errorf("%s declares no state field, so its list has no quick chips", name)
			continue
		}
		want := []string{"All"}
		for _, o := range state.Options {
			if !slices.Contains(quick, o) {
				continue
			}
			if n := readable[o]; n > 0 {
				word := cmp.Or(dt.StateLabels[doctype.State(o)], components.StateLabel(doctype.State(o)))
				want = append(want, word+" "+types.FormatCount(int64(n)))
			}
			if hidden[o] > 0 {
				hiddenCounted++
			}
		}

		r := v.get("/doc/" + name)
		if r.status != http.StatusOK {
			t.Errorf("GET /doc/%s = %d %.400s", name, r.status, r.doc.Find("main").Text())
			continue
		}
		var got, hrefs []string
		r.doc.Find(`ul[aria-label="Quick filters"] a`).Each(func(_ int, a *goquery.Selection) {
			got = append(got, strings.Join(strings.Fields(a.Text()), " "))
			hrefs = append(hrefs, a.AttrOr("href", ""))
		})
		if !slices.Equal(got, want) {
			t.Errorf("%s quick chips = %q, want %q (readable %v, hidden %v)", name, got, want, readable, hidden)
			continue
		}
		if n := r.doc.Find("form.filterbar details.filterbar-panel summary").Length(); n != 1 {
			t.Errorf("%s: %d Filters controls, want 1", name, n)
		}
		for i, href := range hrefs[1:] {
			label := got[i+1]
			n := label[strings.LastIndex(label, " ")+1:]
			count := strings.TrimSpace(v.get(href).doc.Find(".filterbar-count").Text())
			if !strings.HasPrefix(count, n+" ") {
				t.Errorf("%s chip %q lists %q", name, label, count)
			}
		}
	}
	if hiddenCounted == 0 {
		t.Error("no hidden record sits in a state a chip counts: the scope check proves nothing")
	}
}

// R5: the restricted viewer's chips keep a search: the counts are of the
// rows the search finds, so a chip never promises rows the search drops.
func TestApp_ListQuickChipsKeepSearch(t *testing.T) {
	s := seedSweep(t)
	v := s.w.signedIn(viewerEmail)
	for _, name := range slices.Sorted(maps.Keys(s.w.app.DocDeps().Modules)) {
		r := v.get("/doc/" + name + "?q=zzqqnomatch")
		if r.status != http.StatusOK {
			t.Errorf("GET %s = %d", name, r.status)
			continue
		}
		var got []string
		r.doc.Find(`ul[aria-label="Quick filters"] a`).Each(func(_ int, a *goquery.Selection) {
			got = append(got, strings.Join(strings.Fields(a.Text()), " "))
		})
		if !slices.Equal(got, []string{"All"}) {
			t.Errorf("%s chips with a search that finds nothing = %q, want only All", name, got)
		}
	}
}

// R5 (EXP claims list, GRN M4): claims and advances show their date on the
// card and as a column, and claims, advances and goods receipts sort by it.
func TestApp_ListDateColumnAndSort(t *testing.T) {
	s := seedSweep(t)
	v := s.w.signedIn(viewerEmail)
	for _, c := range []struct{ dt, field, label string }{
		{"expense.Claim", "claim_date", "Claim date"},
		{"expense.Advance", "advance_date", "Advance date"},
		{"goodsreceipt.Receipt", "receipt_date", "Receipt date"},
	} {
		r := v.get("/doc/" + c.dt)
		if r.status != http.StatusOK {
			t.Errorf("GET %s = %d", c.dt, r.status)
			continue
		}
		if r.doc.Find(`#filter-sort option[value="`+c.field+`.desc"]`).Length() != 1 {
			t.Errorf("%s: no %s sort", c.dt, c.field)
		}
		var labels []string
		r.doc.Find("ul.datalist-rows > li:first-child dl:not([data-priority]) dt").Each(func(_ int, s *goquery.Selection) {
			labels = append(labels, strings.TrimSpace(s.Text()))
		})
		if !slices.Contains(labels, c.label) {
			t.Errorf("%s first card's values = %q, want %s on the card", c.dt, labels, c.label)
		}
	}
}

// CD-394 (3): a quick chip's count is the count query's, exact past one page:
// 26 draft purchase requests (the page holds 25) show Draft 26.
func TestApp_ListQuickChipCountPastOnePage(t *testing.T) {
	w := newWorld(t)
	admin, _, _ := w.canonical()
	ctx := context.Background()
	id := func(table, code string) string {
		t.Helper()
		return w.text(`SELECT CAST(id AS TEXT) FROM `+table+` WHERE code = ?`, code)
	}
	values := map[string]string{"request_date": "2026-10-24", "site_id": id("site", "METRO-SITE"), "project_id": id("project", "PRJ-METRO")}
	line := map[string]string{"item_id": id("item", "MS-ANGLE-50"), "uom_id": id("uom", "KGS"), "qty": "100"}
	for range 26 {
		if _, err := w.app.Engine().Do(ctx, admin, purchase.RequestDocType, 0, doctype.ActionSave, doc.ActionInput{Values: values, Lines: []map[string]string{line}}); err != nil {
			t.Fatal(err)
		}
	}
	r := w.signedIn(adminEmail).get("/doc/" + purchase.RequestDocType)
	var got []string
	r.doc.Find(`ul[aria-label="Quick filters"] a`).Each(func(_ int, a *goquery.Selection) {
		got = append(got, strings.Join(strings.Fields(a.Text()), " "))
	})
	if !slices.Contains(got, "Draft 26") {
		t.Errorf("quick chips = %q, want Draft 26", got)
	}
}

// R5: claims, advances and goods receipts sort by their date. Dates are set
// against id order (a later id an earlier date) except the last id, which
// gets the latest date, so in each direction alone a sort that falls back to
// ids either way fails; as the administrator over every row.
func TestApp_ListSortsByDate(t *testing.T) {
	s := seedSweep(t)
	v := s.w.signedIn(adminEmail)
	for _, c := range []struct{ dt, field string }{
		{expense.ClaimDocType, "claim_date"}, {expense.AdvanceDocType, "advance_date"}, {goodsreceipt.DocType, "receipt_date"},
	} {
		dt, _ := doctype.Lookup(c.dt)
		var ids []int64
		rows, err := s.w.app.DB().SQL().QueryContext(context.Background(), `SELECT id FROM `+dt.Table+` ORDER BY id`)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		if len(ids) < 3 || len(ids) > 27 {
			t.Fatalf("%s: %d records, want 3 to 27 to date in September", c.dt, len(ids))
		}
		last := ids[len(ids)-1]
		for i, id := range ids {
			date := fmt.Sprintf("2026-09-%02d", 28-i) // later id, earlier date
			if id == last {
				date = "2026-09-29" // breaks the pattern: the latest
			}
			if _, err := s.w.app.DB().SQL().ExecContext(context.Background(), `UPDATE `+dt.Table+` SET `+c.field+` = ? WHERE id = ?`, date, id); err != nil {
				t.Fatal(err)
			}
		}
		newest := append([]int64{last}, ids[:len(ids)-1]...) // the last id, then id ascending
		oldest := slices.Clone(newest)
		slices.Reverse(oldest)
		for dir, want := range map[string][]int64{"desc": newest, "asc": oldest} {
			r := v.get("/doc/" + c.dt + "?size=100&sort=" + c.field + "." + dir)
			var got []int64
			r.doc.Find("ul.datalist-rows > li > a").Each(func(_ int, a *goquery.Selection) {
				href := a.AttrOr("href", "")
				n, _ := strconv.ParseInt(href[strings.LastIndex(href, "/")+1:], 10, 64)
				if slices.Contains(ids, n) {
					got = append(got, n)
				}
			})
			if !slices.Equal(got, want) {
				t.Errorf("%s sort %s.%s = %v, want %v", c.dt, c.field, dir, got, want)
			}
		}
	}
}
