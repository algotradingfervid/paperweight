package docscreen_test

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// todayLineType has a Date field on its lines, which no fixture DocType has.
const todayLineType = "test.TodayLine"

func init() {
	doctype.Register(doctype.DocType{
		Name: todayLineType, Label: "Today Line", PluralLabel: "Today Lines", Table: "todayline", LinesTable: "todayline_line",
		Fields: []doctype.Field{
			{Name: "start", Label: "Start", Type: doctype.Date},
			{Name: "note", Label: "Note", Type: doctype.Text},
			{Name: "line_date", Label: "Line date", Type: doctype.Date, OnLine: true},
		},
	})
}

// Module.TodayDates is checked when the routes are built: a name that isn't a
// header Date field would silently leave the field blank, and with no
// Deps.Today there is no today to give.
func TestCheckTodayDates(t *testing.T) {
	today := func() (string, string) { return "24-10-2026", "23-10-2026" }
	for _, c := range []struct {
		name  string
		dates []string
		today func() (string, string)
		want  string
	}{
		{"none", nil, nil, ""},
		{"a header date", []string{"start"}, today, ""},
		{"no today", []string{"start"}, nil, "Deps.Today"},
		{"unknown", []string{"nope"}, today, `"nope"`},
		{"not a date", []string{"note"}, today, `"note"`},
		{"a line date", []string{"line_date"}, today, `"line_date"`},
	} {
		mods := map[string]docscreen.Module{todayLineType: {TodayDates: c.dates}}
		err := docscreen.CheckTodayDates(mods, c.today)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: %v", c.name, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
			t.Errorf("%s: error = %v, want it to name %s", c.name, err, c.want)
		}
	}
	if err := docscreen.CheckTodayDates(map[string]docscreen.Module{"no.such": {TodayDates: []string{"x"}}}, today); err == nil {
		t.Error("an unregistered DocType passed")
	}
}

// Routes refuses to build with a bad TodayDates.
func TestRoutes_PanicsOnBadTodayDates(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Routes built with a TodayDates name that isn't a date")
		}
	}()
	docscreen.Routes(docscreen.Deps{Modules: map[string]docscreen.Module{todayLineType: {TodayDates: []string{"note"}}}})
}
