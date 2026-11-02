//go:build e2e

package e2e

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// grnTileField is the one Select field FieldInput would draw as tiles. Delivery
// details doesn't use FieldInput for it: the page draws components.Radios,
// which TestE2E_TilesWordsStayWhole pins, so it has no tile to check. Expense
// type isn't a field of that kind: the line page calls components.Tiles itself.
const grnTileField = goodsreceipt.DocType + "." + "challan_kind"

// tilesCallers are the only non-test files that call components.Tiles. The
// gallery's calls use made-up options, so they aren't real data.
var tilesCallers = []string{"internal/modules/expense/web/s2_line.go", "internal/ui/docscreen/inputs.go"}

// tileFields are the editable Select fields of every registered DocType that
// FieldInput draws as tiles, by rendering each one through FieldInput.
func tileFields(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, d := range doctype.All() {
		for _, f := range d.Fields {
			if f.Type != doctype.Select || f.ReadOnly || f.System {
				continue
			}
			page := uitest.Render(t, docscreen.FieldInput(f, components.FieldID(f.Name), "", "", docscreen.Deps{}))
			if page.Find(".segmented--tiles").Length() > 0 {
				out = append(out, d.Name+"."+f.Name)
			}
		}
	}
	return out
}

// The word check's list of callers is complete: a new caller of Tiles, or a
// new Select field that comes out as tiles, fails here until the browser test
// below loops over its options too. docscreen/inputs.go's only such field is
// grnTileField.
func TestE2E_TilesWordsCoverEveryCaller(t *testing.T) {
	_ = startExpense(t) // registers the DocTypes
	if got, want := tileFields(t), []string{grnTileField}; !slices.Equal(got, want) {
		t.Errorf("tile fields = %q, want %q", got, want)
	}
	var callers []string
	err := filepath.WalkDir("../internal", func(path string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() && e.Name() == "gallery" {
			return filepath.SkipDir
		}
		if ext := filepath.Ext(path); e.IsDir() || (ext != ".go" && ext != ".templ") || strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, "_templ.go") {
			return nil
		}
		src, err := os.ReadFile(path) //nolint:gosec // G304, G122: this repository's own tree
		if bytes.Contains(src, []byte("components.Tiles(")) {
			callers = append(callers, strings.TrimPrefix(filepath.ToSlash(path), "../"))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(callers, tilesCallers) {
		t.Errorf("callers of Tiles = %q, want %q", callers, tilesCallers)
	}
}

// tileLayout is the tiles of radio group name as "a, b / c": its options in
// DOM order, a slash between rows.
func tileLayout(name string) string {
	return `(() => {
  const rows = new Map();
  for (const i of document.querySelectorAll("input[name=` + name + `]")) {
    const l = i.closest("label"), top = l.getBoundingClientRect().top;
    rows.set(top, [...(rows.get(top) ?? []), l.textContent.replace(/\s+/g, " ").trim()]);
  }
  return [...rows.values()].map((r) => r.join(", ")).join(" / ");
})()`
}

// tileSplits lists every word of radio group name's labels whose text runs
// over more than one line: a Range over the word gives one client rect per
// line it sits on, so distinct rect tops above one mean the word was broken.
// It reads the rendered lines, so it needs no guess at font or width.
func tileSplits(name string) string {
	return `[...document.querySelectorAll("input[name=` + name + `]")].flatMap((i) => {
  const l = i.closest("label");
  const text = [...l.querySelector(".segmented__text").childNodes].find((n) => n.nodeType === Node.TEXT_NODE && n.data.trim());
  return [...text.data.matchAll(/\S+/g)].flatMap((m) => {
    const r = document.createRange();
    r.setStart(text, m.index);
    r.setEnd(text, m.index + m[0].length);
    const lines = new Set([...r.getClientRects()].map((c) => Math.round(c.top))).size;
    return lines > 1 ? [JSON.stringify(text.data.trim()) + (i.checked ? " chosen" : " not chosen") + " at " + l.offsetWidth + " px: " + m[0] + " on " + lines + " lines"] : [];
  });
})`
}

// wantWholeWords checks radio group name's labels, drawn as wantRows, keep
// every word whole with nothing chosen and with each label chosen in turn,
// the check showing on the chosen one.
func wantWholeWords(t *testing.T, s *Session, name, wantRows string, labels []string) {
	t.Helper()
	if got := Eval[string](s, tileLayout(name)); got != wantRows {
		t.Errorf("%s draws %q, want %q", name, got, wantRows)
	}
	report := func(state string) {
		t.Helper()
		if got := Eval[[]string](s, tileSplits(name)); len(got) != 0 {
			t.Errorf("%s, %s: words split: %q", name, state, got)
		}
	}
	report("as the page loads")
	for _, label := range labels {
		pickRadio(s, name, label)
		check := `(` + choice(name, label) + `).querySelector(".segmented__check").getBoundingClientRect().width`
		if w := Eval[float64](s, check); w == 0 {
			t.Errorf("%s has no check showing when chosen", label)
		}
		report(label + " chosen")
	}
}

// TestE2E_TilesWordsStayWhole (CD-353, UI v2 §5.5 item 8): every real option
// drawn as a tile, on the real page at 320 and 360 px, keeps each word whole,
// unselected and selected: the seeded expense types on the line page. GRN's
// challan kinds are radios on Delivery details, which this also pins; if that
// page moves to Tiles this fails, and its options join the loop.
func TestE2E_TilesWordsStayWhole(t *testing.T) {
	h := startExpense(t)
	r := h.Do(priya, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{Values: map[string]string{
		"vendor_id": h.idOf("party", "SHREE"), "site_id": h.idOf("site", "METRO-SITE"), "project_id": h.idOf("project", "PRJ-METRO"), "order_date": plainDate,
	}, Lines: []map[string]string{{"item_id": h.idOf("item", "BOLT-M12"), "uom_id": h.idOf("uom", "NOS"), "qty": "500", "price": "12"}}})
	h.Do(priya, purchase.DocType, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version}) // no rule: approved on submit

	for _, width := range []int64{320, 360} {
		t.Run(strconv.FormatInt(width, 10), func(t *testing.T) {
			d := h.SignIn(deepak, width)
			d.Open("/doc/" + expense.ClaimDocType)
			d.Press("New expense claim")
			onStep(d, "Step 1 of 3")
			d.Fill("purpose", "Pune Metro site visit")
			d.Press(saveContinue)
			onStep(d, "Step 2 of 3")
			d.Press("Add an expense")
			d.WaitText("Take photo")
			wantWholeWords(t, d, "expense_type_id", "Food, Fuel / Lodging, Travel / Site miscellaneous",
				[]string{"Food", "Fuel", "Lodging", "Travel", "Site miscellaneous"})

			s := h.SignIn(suresh, width)
			s.Open("/doc/" + goodsreceipt.DocType)
			startGRN(s)
			kinds := `[...document.querySelectorAll("input[name=challan_kind]")]`
			if got, want := Eval[[]string](s, kinds+`.map((i) => i.closest("label").textContent.trim())`), []string{"Delivery challan", "Tax invoice"}; !slices.Equal(got, want) {
				t.Errorf("challan kinds = %q, want %q", got, want)
			}
			if Eval[bool](s, kinds+`.some((i) => i.closest(".segmented--tiles"))`) {
				t.Error("Delivery details draws challan_kind as tiles: add its options to the word check")
			}
		})
	}
}
