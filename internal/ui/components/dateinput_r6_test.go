package components_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// UI v2 §5.7: aria-pressed is true on the chip that matches the value, in any
// form the server reads, and false on the other; with no match, or a value
// the server can't read, both are false.
func TestDateInput_ChipsPressed(t *testing.T) {
	for _, c := range []struct{ value, today, yesterday string }{
		{"24-10-2026", "true", "false"},
		{"24/10/2026", "true", "false"},
		{"23-10-2026", "false", "true"},
		{"3-10-2026", "false", "false"},
		{"", "false", "false"},
		{"24-13-2026", "false", "false"},
	} {
		p := components.DateProps{FieldProps: components.FieldProps{ID: "f-d", Name: "d", Label: "Date", Value: c.value}, Today: "24-10-2026", Yesterday: "23-10-2026"}
		doc := uitest.Render(t, components.DateInput(p))
		for chip, want := range map[string]string{"24-10-2026": c.today, "23-10-2026": c.yesterday} {
			if got, ok := doc.Find(`button[data-set-date="` + chip + `"]`).Attr("aria-pressed"); !ok || got != want {
				t.Errorf("value %q: chip %s aria-pressed = %q (present %v), want %q", c.value, chip, got, ok, want)
			}
		}
	}
}

// A chip with no value would blank the field (shell review H1): DateInput
// leaves it out, and with neither leaves out the row. The box, its calendar
// button and the dd-mm-yyyy text stay.
func TestDateInput_NoChipWithoutAValue(t *testing.T) {
	for _, c := range []struct {
		name, today, yesterday string
		chips, rows            int
	}{
		{"both", "24-10-2026", "23-10-2026", 2, 1},
		{"no yesterday", "24-10-2026", "", 1, 1},
		{"no today", "", "23-10-2026", 1, 1},
		{"neither", "", "", 0, 0},
	} {
		p := components.DateProps{FieldProps: components.FieldProps{ID: "f-d", Name: "d", Label: "Date"}, Today: c.today, Yesterday: c.yesterday}
		doc := uitest.Render(t, components.DateInput(p))
		if got := doc.Find("[data-set-date]").Length(); got != c.chips {
			t.Errorf("%s: %d chips, want %d", c.name, got, c.chips)
		}
		if got := doc.Find(`[data-set-date=""]`).Length(); got != 0 {
			t.Errorf("%s: %d chips with no value", c.name, got)
		}
		if got := doc.Find(".field__actions").Length(); got != c.rows {
			t.Errorf("%s: %d chip rows, want %d", c.name, got, c.rows)
		}
		if doc.Find("input#f-d[data-date]").Length() != 1 || doc.Find("[data-date-pick]").Length() != 1 {
			t.Errorf("%s: the box or its calendar button is missing", c.name)
		}
	}
}

// CD-387 (8): Qty, Rate, Money and a date select on a tap; Percent and Factor
// don't.
func TestNumberInput_TapSelects(t *testing.T) {
	p := components.FieldProps{ID: "f-n", Name: "n", Label: "N", Value: "10"}
	for _, c := range []struct {
		name string
		doc  templ.Component
		want bool
	}{
		{"qty", components.QtyInput(p, false, "kg"), true},
		{"whole qty", components.QtyInput(p, true, "Nos"), true},
		{"large qty", components.LargeQtyInput(p, true, "Nos"), true},
		{"number qty", components.NumberInput(p, components.Qty, false), true},
		{"money", components.MoneyInput(p), true},
		{"rate", components.RateInput(p), true},
		{"date", components.DateInput(components.DateProps{FieldProps: p}), true},
		{"percent", components.PercentInput(p), false},
		{"factor", components.NumberInput(p, components.Factor, false), false},
	} {
		if got := uitest.Render(t, c.doc).Find("input#f-n[data-tap-select]").Length() == 1; got != c.want {
			t.Errorf("%s: selects on a tap = %v, want %v", c.name, got, c.want)
		}
	}
}

// UI v2 §5.7: the echo has a check icon, on the drawn field and in
// /ui/validate's answer, and an EchoNote follows it in both, posted back as
// _note. A blank or refused date shows neither, and another kind no icon.
func TestDateInput_EchoCheckAndNote(t *testing.T) {
	const want = "Saturday, 24 October 2026 · the day the goods arrived"
	p := components.FieldProps{ID: "f-d", Name: "d", Label: "Date", Value: "24-10-2026", EchoNote: "the day the goods arrived"}
	doc := uitest.Render(t, components.DateInput(components.DateProps{FieldProps: p}))
	msg := doc.Find("p#f-d-msg")
	if got := strings.TrimSpace(msg.Text()); got != want || msg.Find("svg").Length() != 1 || !msg.HasClass("field__msg--echo") {
		t.Errorf("drawn echo = %q, %d icons, class %q", got, msg.Find("svg").Length(), msg.AttrOr("class", ""))
	}
	vals := doc.Find("input#f-d").AttrOr("hx-vals", "")
	if !strings.Contains(vals, `"_note":"the day the goods arrived"`) {
		t.Errorf("hx-vals = %s", vals)
	}
	p.EchoNote = ""
	if got := uitest.Render(t, components.DateInput(components.DateProps{FieldProps: p})).Find("input#f-d").AttrOr("hx-vals", ""); strings.Contains(got, "_note") {
		t.Errorf("no note: hx-vals = %s", got)
	}
	p.Value = ""
	p.EchoNote = "the day the goods arrived"
	if got := uitest.Render(t, components.DateInput(components.DateProps{FieldProps: p})).Find("p#f-d-msg"); got.Find("svg").Length() != 0 || strings.TrimSpace(got.Text()) != "" {
		t.Errorf("blank date: message %q, %d icons", got.Text(), got.Find("svg").Length())
	}

	post := func(kind, value, note string) *goquery.Selection {
		t.Helper()
		f := url.Values{"_id": {"f-d"}, "_name": {"d"}, "d": {value}}
		if note != "" {
			f.Set("_note", note)
		}
		r := httptest.NewRequest(http.MethodPost, "/ui/validate/"+kind, strings.NewReader(f.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		components.ValidateHandler(components.NUMParsers()).ServeHTTP(w, r)
		return uitest.Doc(t, w).Find("p#f-d-msg")
	}
	if m := post("date", "24-10-2026", "the day the goods arrived"); strings.TrimSpace(m.Text()) != want || m.Find("svg").Length() != 1 {
		t.Errorf("validated echo = %q, %d icons", m.Text(), m.Find("svg").Length())
	}
	if m := post("date", "24-10-2026", ""); strings.TrimSpace(m.Text()) != "Saturday, 24 October 2026" || m.Find("svg").Length() != 1 {
		t.Errorf("no note: echo = %q, %d icons", m.Text(), m.Find("svg").Length())
	}
	if m := post("date", "", "the day the goods arrived"); strings.TrimSpace(m.Text()) != "" || m.Find("svg").Length() != 0 {
		t.Errorf("blank: echo = %q, %d icons", m.Text(), m.Find("svg").Length())
	}
	if m := post("date", "31-02-2026", "the day the goods arrived"); strings.Contains(m.Text(), "goods") {
		t.Errorf("refused: %q", m.Text())
	}
	echo := map[string]components.ParseFunc{"code": func(string) (string, string, error) { return "X", "Maharashtra", nil }}
	r := httptest.NewRequest(http.MethodPost, "/ui/validate/code", strings.NewReader(url.Values{"_id": {"f-d"}, "_name": {"d"}, "d": {"x"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	components.ValidateHandler(echo).ServeHTTP(w, r)
	if m := uitest.Doc(t, w).Find("p#f-d-msg"); strings.TrimSpace(m.Text()) != "Maharashtra" || m.Find("svg").Length() != 0 {
		t.Errorf("another kind's echo = %q, %d icons", m.Text(), m.Find("svg").Length())
	}
}

// failingWriter is a response whose body can't be written.
type failingWriter struct {
	*httptest.ResponseRecorder
}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("no body") }

// A message region that can't be written answers 500.
func TestValidateHandler_RenderFailure(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/ui/validate/date", strings.NewReader(url.Values{"_id": {"f-d"}, "_name": {"d"}, "d": {"24-10-2026"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := failingWriter{httptest.NewRecorder()}
	components.ValidateHandler(components.NUMParsers()).ServeHTTP(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
}

// GRN recheck: a date past Max echoes with no check, drawn and from
// /ui/validate; on Max itself, and with no Max, it keeps the check.
func TestDateInput_NoCheckPastMax(t *testing.T) {
	const limit = "24-10-2026"
	post := func(value, maxDay string) *goquery.Selection {
		t.Helper()
		f := url.Values{"_id": {"f-d"}, "_name": {"d"}, "d": {value}}
		if maxDay != "" {
			f.Set("_max", maxDay)
		}
		r := httptest.NewRequest(http.MethodPost, "/ui/validate/date", strings.NewReader(f.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		components.ValidateHandler(components.NUMParsers()).ServeHTTP(w, r)
		return uitest.Doc(t, w).Find("p#f-d-msg")
	}
	for _, c := range []struct {
		value, limit, echo string
		icons              int
	}{
		{"25-10-2026", limit, "Sunday, 25 October 2026", 0},
		{"24-10-2026", limit, "Saturday, 24 October 2026", 1},
		{"23-10-2026", limit, "Friday, 23 October 2026", 1},
		{"25-10-2026", "", "Sunday, 25 October 2026", 1},
	} {
		p := components.DateProps{FieldProps: components.FieldProps{ID: "f-d", Name: "d", Label: "Date", Value: c.value}, Max: c.limit}
		doc := uitest.Render(t, components.DateInput(p))
		for what, m := range map[string]*goquery.Selection{"drawn": doc.Find("p#f-d-msg"), "validated": post(c.value, c.limit)} {
			if got := strings.TrimSpace(m.Text()); got != c.echo || m.Find("svg").Length() != c.icons {
				t.Errorf("%s %s, max %q: echo %q, %d icons; want %q, %d", what, c.value, c.limit, got, m.Find("svg").Length(), c.echo, c.icons)
			}
		}
		if got := doc.Find("input#f-d").AttrOr("hx-vals", ""); strings.Contains(got, `"_max":"`+limit+`"`) != (c.limit != "") || strings.Contains(got, "_max") != (c.limit != "") {
			t.Errorf("max %q: hx-vals = %s", c.limit, got)
		}
	}
}
