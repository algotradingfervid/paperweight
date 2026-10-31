package docscreen

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"

	// Every module's DocTypes are registered, as in the app.
	_ "github.com/vardhan/internaltools/internal/modules/expense"
	_ "github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	_ "github.com/vardhan/internaltools/internal/modules/purchase"
)

// gridCellCounts are the row sizes the editable line grid lays out (M0-35).
var gridCellCounts = []int{3, 5}

// gridCells is how many cells dt's editable grid rows get (gridRowOf on one
// empty line) and whether its Items step shows the grid: FormSteps has the
// step and gridShown, the real decision, says so.
func gridCells(t *testing.T, d Deps, dt *doctype.DocType) (n int, shown bool) {
	t.Helper()
	steps, err := FormSteps(dt)
	if err != nil {
		t.Errorf("%s: %v", dt.Name, err)
		return 0, false
	}
	r := httptest.NewRequest("GET", "/", nil)
	v := DocView{Lines: []LineView{{No: 1}}}
	n = len(d.gridRowOf(r, dt, v, v.Lines[0], "1", nil, nil).Cells)
	return n, slices.ContainsFunc(steps, func(s FormStep) bool { return s.Lines }) && d.gridShown(r, dt, v)
}

// TestLineGrid_EveryDocTypeFits (M0-40, CD-313): every registered document
// DocType whose Items step shows the editable line grid has a row size the
// grid lays out; any other size shows cards (the fallback in gridShown).
//
// Two limits. A module whose Steps["items"].Body replaces the line editor
// also hides the grid, which this gate doesn't see: that can only give a
// false failure, never a missed one. The app's Deps sets no Modules yet, so
// neither does this one; when internal/app wires real Modules, add them to
// d here.
func TestLineGrid_EveryDocTypeFits(t *testing.T) {
	d := Deps{}
	checked := map[string]int{}
	for _, dt := range doctype.All() {
		if !dt.IsDocument() {
			// A master never reaches the items step: the engine refuses
			// it before reading (it would read through the nil tx otherwise).
			if _, err := doc.LoadDoc(context.Background(), nil, dt.Name, 1); !isNotFound(err) {
				t.Errorf("%s: the engine loads a master (%v); check its line grid here", dt.Name, err)
			}
			continue
		}
		n, shown := gridCells(t, d, dt)
		if !shown {
			continue
		}
		checked[dt.Name] = n
		// CD-313: a DocType edited on the line page shows a display-only
		// grid, which passes here once docscreen has it (M0-40 (b)).
		if !slices.Contains(gridCellCounts, n) {
			t.Errorf("%s: an editable line grid with %d cells; the grid lays out %v", dt.Name, n, gridCellCounts)
		}
	}
	// Every module registers its DocTypes here: a module directory whose
	// blank import is missing above has no "<dir>." DocType.
	dirs, err := os.ReadDir("../../modules")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range dirs {
		if !e.IsDir() || e.Name() == "modkit" {
			continue
		}
		if !slices.ContainsFunc(doctype.All(), func(dt *doctype.DocType) bool { return strings.HasPrefix(dt.Name, e.Name()+".") }) {
			t.Errorf("internal/modules/%s registers no DocType in this test: add its blank import", e.Name())
		}
	}
	// The sweep reaches the grid: fixture.Order is its five-cell case and
	// fixture.Claim its three-cell one.
	for name, want := range map[string]int{fixture.Order: 5, fixture.Claim: 3} {
		if n, ok := checked[name]; !ok || n != want {
			t.Errorf("%s: grid shown %v with %d cells, want %d", name, ok, n, want)
		}
	}
}

// TestLineGrid_FallsBackToCards (M0-40): gridShown shows the grid for rows
// of 3 and 5 cells only; 2, 4, 6 and 11 show cards. fixture.Order has five
// editable line fields; Hide takes away the first 5-n of them, and the
// sixth and later are copies of qty.
func TestLineGrid_FallsBackToCards(t *testing.T) {
	order, ok := doctype.Lookup(fixture.Order)
	if !ok {
		t.Fatal("fixture.Order is not registered")
	}
	for _, n := range []int{2, 3, 4, 5, 6, 11} {
		dt := *order
		dt.Fields = slices.Clone(order.Fields)
		qty, _ := order.Field("qty")
		for i := 5; i < n; i++ {
			extra := qty
			extra.Name = "qty_" + strconv.Itoa(i)
			dt.Fields = append(dt.Fields, extra)
		}
		hide := map[string]bool{}
		for _, name := range []string{"description", "uom", "rate"}[:max(0, 5-n)] {
			hide[name] = true
		}
		d := Deps{Modules: map[string]Module{dt.Name: {Hide: func(_ Ctx, f doctype.Field) bool { return hide[f.Name] }}}}
		got, shown := gridCells(t, d, &dt)
		if got != n {
			t.Fatalf("%d: the row has %d cells", n, got)
		}
		if want := n == 3 || n == 5; shown != want {
			t.Errorf("%d cells: grid shown %v, want %v", n, shown, want)
		}
	}
}

// TestLineGrid_SkipsNoGrid (M0-40): the gate skips a DocType exactly when
// docscreen renders it no grid: no lines table, or a module's CardsOnly,
// Line or NoLinePages.
func TestLineGrid_SkipsNoGrid(t *testing.T) {
	order, ok := doctype.Lookup(fixture.Order)
	if !ok {
		t.Fatal("fixture.Order is not registered")
	}
	noLines := *order
	noLines.LinesTable = ""
	if _, shown := gridCells(t, Deps{}, &noLines); shown {
		t.Error("a DocType with no lines table renders a line grid")
	}
	for name, m := range map[string]Module{
		"CardsOnly":   {CardsOnly: true},
		"Line":        {Line: &Line{}},
		"NoLinePages": {NoLinePages: true},
	} {
		d := Deps{Modules: map[string]Module{fixture.Order: m}}
		if _, shown := gridCells(t, d, order); shown {
			t.Errorf("%s: fixture.Order renders a line grid", name)
		}
	}
	if _, shown := gridCells(t, Deps{Modules: map[string]Module{fixture.Order: {}}}, order); !shown {
		t.Error("fixture.Order with an empty Module renders no line grid")
	}
}

func isNotFound(err error) bool {
	var de *doc.Error
	return errors.As(err, &de) && de.Kind == doc.KindNotFound
}
