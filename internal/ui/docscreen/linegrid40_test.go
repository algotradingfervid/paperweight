package docscreen

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"

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
// false failure, never a missed one. Modules now set CardsOnly (expense's
// Claim and Advance, CD-315), but this gate's empty Deps doesn't see them,
// so it treats those DocTypes as gridded; that is why the five-cell money
// check below counts only TestOnly DocTypes, which have no Module.
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
		n, shown, problems := lineGridProblems(t, d, dt)
		if !shown {
			continue
		}
		checked[dt.Name] = n
		// CD-313: a DocType edited on the line page shows the display grid
		// (Module.LineGrid), whose rows must fit the same layouts (M0-40b).
		for _, p := range problems {
			t.Error(p)
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
	// The sweep reaches a five-cell editable grid with a money input
	// (M0-40c): its money input sits in a narrow track, not the Amount one.
	// Only TestOnly DocTypes count (see the limits above).
	var fiveMoney []string
	for name, n := range checked {
		dt, _ := doctype.Lookup(name)
		if n != 5 || !dt.TestOnly {
			continue
		}
		r := httptest.NewRequest("GET", "/", nil)
		v := DocView{Lines: []LineView{{No: 1}}}
		for _, cell := range d.gridRowOf(r, dt, v, v.Lines[0], "1", nil, nil).Cells {
			var b strings.Builder
			if err := cell.Input.Render(context.Background(), &b); err != nil {
				t.Fatalf("%s: rendering cell %s: %v", name, cell.Label, err)
			}
			if strings.Contains(b.String(), `hx-post="/ui/validate/money"`) {
				fiveMoney = append(fiveMoney, name)
				break
			}
		}
	}
	if len(fiveMoney) == 0 {
		t.Errorf("no registered TestOnly DocType shows a five-cell editable grid with a money input; checked %v", checked)
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

// lineGridProblems is the gate for one DocType under d (M0-40, M0-40b;
// CD-313, CD-315): n is the cells of the grid its Items step shows and
// shown whether it shows one. With Module.LineGrid that is the display grid,
// whose cell count is LineGrid's on one empty line, and the editable grid
// must not show (lines are edited only on the line page); otherwise it is
// the editable grid. Either must have a row size the grid lays out.
func lineGridProblems(t *testing.T, d Deps, dt *doctype.DocType) (n int, shown bool, problems []string) {
	t.Helper()
	n, shown = gridCells(t, d, dt)
	if lg := d.modOf(dt).LineGrid; lg != nil {
		steps, err := FormSteps(dt)
		if err != nil || !slices.ContainsFunc(steps, func(s FormStep) bool { return s.Lines }) {
			return n, shown, nil // no Items step, no grid
		}
		if shown {
			problems = append(problems, fmt.Sprintf("%s: Module.LineGrid is set and the editable grid with %d cells shows too; lines are edited only on the line page (CD-313)", dt.Name, n))
		}
		r := httptest.NewRequest("GET", "/", nil)
		v := DocView{Lines: []LineView{{No: 1}}}
		n, shown = len(lg(d.ctxOf(r, dt, &v), v.Lines[0])), true
		if !slices.Contains(gridCellCounts, n) {
			problems = append(problems, fmt.Sprintf("%s: a display line grid (Module.LineGrid) with %d cells; the grid lays out %v", dt.Name, n, gridCellCounts))
		}
		return n, shown, problems
	}
	if shown && !slices.Contains(gridCellCounts, n) {
		problems = append(problems, fmt.Sprintf("%s: an editable line grid with %d cells; the grid lays out %v", dt.Name, n, gridCellCounts))
	}
	return n, shown, problems
}

// m40bLineGrid is a Module.LineGrid giving every line n text cells.
func m40bLineGrid(n int) func(Ctx, LineView) []components.GridCell {
	return func(_ Ctx, l LineView) []components.GridCell {
		cells := make([]components.GridCell, n)
		for i := range cells {
			cells[i] = components.GridCell{Label: "Cell " + strconv.Itoa(i+1), Text: strconv.Itoa(l.No)}
		}
		return cells
	}
}

// TestLineGrid_DisplayGate (M0-40b, CD-313, CD-315): the gate fails a
// DocType whose Module.LineGrid gives 4 cells, and passes one giving 3 or
// 5, on fixture.Order (5 editable cells) and with one editable cell hidden
// (4, cards). A 3- or 5-cell LineGrid passes only when the editable grid
// doesn't show beside it.
func TestLineGrid_DisplayGate(t *testing.T) {
	order, ok := doctype.Lookup(fixture.Order)
	if !ok {
		t.Fatal("fixture.Order is not registered")
	}
	for _, hide := range []string{"", "description"} {
		for _, n := range []int{3, 4, 5} {
			m := Module{LineGrid: m40bLineGrid(n)}
			if hide != "" {
				m.Hide = func(_ Ctx, f doctype.Field) bool { return f.Name == hide }
			}
			d := Deps{Modules: map[string]Module{fixture.Order: m}}
			got, shown, problems := lineGridProblems(t, d, order)
			what := fmt.Sprintf("LineGrid of %d cells, hiding %q", n, hide)
			if !shown || got != n {
				t.Errorf("%s: the gate sees a grid %v of %d cells, want the display grid of %d", what, shown, got, n)
			}
			want := []string{}
			if n == 4 {
				want = []string{fmt.Sprintf("%s: a display line grid (Module.LineGrid) with 4 cells; the grid lays out %v", fixture.Order, gridCellCounts)}
			}
			if !slices.Equal(problems, want) {
				t.Errorf("%s: gate problems %q, want %q", what, problems, want)
			}
			if _, editable := gridCells(t, d, order); editable {
				t.Errorf("%s: the editable grid shows beside Module.LineGrid (CD-313)", what)
			}
		}
	}
}

func isNotFound(err error) bool {
	var de *doc.Error
	return errors.As(err, &de) && de.Kind == doc.KindNotFound
}
