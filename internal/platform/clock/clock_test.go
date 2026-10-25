package clock_test

import (
	"go/parser"
	"go/token"
	"strconv"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/platform/clock"
)

func TestClock_FixedAdvance(t *testing.T) {
	c := clock.NewFixed(time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC))
	c.Advance(15 * time.Minute)
	if got := c.Now(); !got.Equal(time.Date(2026, 10, 24, 5, 15, 0, 0, time.UTC)) {
		t.Fatalf("Now() = %v", got)
	}
}

func TestClock_ISTWithoutZoneinfo(t *testing.T) {
	t.Setenv("ZONEINFO", "/nonexistent")
	got := time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC).In(clock.IST).Format("15:04")
	if got != "10:30" {
		t.Fatalf("IST = %s", got)
	}
}

// TestClock_EmbedsTZData guards the time/tzdata import. TestClock_ISTWithoutZoneinfo
// cannot: IST loads at package init, and Go still finds system zoneinfo on most machines.
func TestClock_EmbedsTZData(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "clock.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse clock.go: %v", err)
	}
	for _, imp := range f.Imports {
		if path, _ := strconv.Unquote(imp.Path.Value); path == "time/tzdata" {
			return
		}
	}
	t.Fatal(`clock.go does not import "time/tzdata"; add import _ "time/tzdata" so IST loads on machines without system zoneinfo`)
}
