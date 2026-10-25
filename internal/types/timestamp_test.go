package types_test

import (
	"go/parser"
	"go/token"
	"math/rand/v2"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

func TestTime_StorageAndDisplay(t *testing.T) { // NUM-075
	if types.IST == nil {
		t.Fatal("types.IST is nil; it must be Asia/Kolkata")
	}
	ts := types.TimestampOf(time.Date(2025, 4, 1, 10, 0, 0, 0, types.IST))
	if got := ts.String(); got != "2025-04-01T04:30:00.000Z" {
		t.Errorf("TimestampOf(01-04-2025 10:00 IST).String() = %q, want %q", got, "2025-04-01T04:30:00.000Z")
	}
	if got := types.FormatIST(ts.Time()); got != "01-04-2025 10:00" {
		t.Errorf("FormatIST(ts.Time()) = %q, want %q", got, "01-04-2025 10:00")
	}
	if got := types.FormatIST(testkit.Clock().Now()); got != "24-10-2026 10:30" {
		t.Errorf("FormatIST(fixture clock) = %q, want %q", got, "24-10-2026 10:30")
	}

	// 100 random instants in 1900–2099: the stored strings are fixed width and
	// sort in the same order as the instants.
	r := rand.New(rand.NewPCG(1, 2))
	lo := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	hi := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	instants := make([]time.Time, 100)
	stored := make([]string, 100)
	for i := range instants {
		instants[i] = time.UnixMilli(lo + r.Int64N(hi-lo)).UTC()
		stored[i] = types.TimestampOf(instants[i]).String()
		if len(stored[i]) != len("2025-04-01T04:30:00.000Z") {
			t.Fatalf("TimestampOf(%s).String() = %q, want fixed width %d", instants[i].Format(time.RFC3339Nano), stored[i], len("2025-04-01T04:30:00.000Z"))
		}
	}
	slices.SortFunc(instants, func(a, b time.Time) int { return a.Compare(b) })
	sort.Strings(stored)
	for i := range instants {
		if want := types.TimestampOf(instants[i]).String(); stored[i] != want {
			t.Fatalf("sorted stored[%d] = %q, want %q (string order differs from time order)", i, stored[i], want)
		}
	}
}

func TestTime_EmbeddedTZData(t *testing.T) { // NUM-076
	if os.Getenv("NUM_TZ_CHILD") == "1" {
		if types.IST == nil {
			t.Fatal("types.IST is nil; it must be Asia/Kolkata")
		}
		_, off := time.Date(2025, 4, 1, 0, 0, 0, 0, types.IST).Zone()
		if off != 19800 {
			t.Fatalf("IST offset = %d seconds, want 19800 (+05:30)", off)
		}
		return
	}

	// (a) ist.go embeds the zone database.
	f, err := parser.ParseFile(token.NewFileSet(), "ist.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Errorf("parse ist.go: %v", err)
	} else {
		found := false
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == "time/tzdata" {
				found = true
			}
		}
		if !found {
			t.Error(`ist.go does not import "time/tzdata"`)
		}
	}

	// (b) IST still loads with ZONEINFO pointing at an empty directory.
	cmd := exec.Command(os.Args[0], "-test.run=^TestTime_EmbeddedTZData$", "-test.v")
	cmd.Env = append(os.Environ(), "ZONEINFO="+t.TempDir(), "NUM_TZ_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("child with empty ZONEINFO failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "--- PASS: TestTime_EmbeddedTZData") {
		t.Fatalf("child did not run TestTime_EmbeddedTZData:\n%s", out)
	}
}
