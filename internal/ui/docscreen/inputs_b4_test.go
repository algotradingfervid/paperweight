package docscreen_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// A Check field renders as a switch row on a real checkbox posting DOC's
// "true", or a master form's "1" with MasterChecks (UI v2 §5.5 item 7, N6;
// CD-322); its label and hint are the field's.
func TestFieldInput_CheckIsSwitch(t *testing.T) {
	f := doctype.Field{Name: "is_stock", Label: "Stock item", Hint: "Counted in stock.", Type: doctype.Check}
	for _, d := range []struct {
		deps docscreen.Deps
		yes  string
	}{{docscreen.Deps{}, "true"}, {docscreen.Deps{MasterChecks: true}, "1"}} {
		for _, c := range []struct {
			value string
			on    bool
		}{{"1", true}, {"true", true}, {"0", false}, {"false", false}, {"", false}} {
			doc := uitest.Render(t, docscreen.FieldInput(f, "f-is_stock", c.value, "", d.deps))
			in := doc.Find(`label.switch-row input#f-is_stock[type="checkbox"][role="switch"][name="is_stock"]`)
			if in.Length() != 1 {
				t.Fatalf("value %q: no switch for the Check field", c.value)
			}
			if v, _ := in.Attr("value"); v != d.yes {
				t.Errorf("MasterChecks %v, value %q: posts %q, want %s", d.deps.MasterChecks, c.value, v, d.yes)
			}
			if _, on := in.Attr("checked"); on != c.on {
				t.Errorf("value %q: on = %v, want %v", c.value, on, c.on)
			}
			if doc.Find(`input[type="hidden"][name="is_stock"]`).Length() != 0 {
				t.Errorf("value %q: a plain switch posts a hidden value too", c.value)
			}
			if got := strings.TrimSpace(doc.Find(".switch-row__label").Text()); got != "Stock item" {
				t.Errorf("label = %q", got)
			}
		}
	}
}

// Field.Invert shows a negatively stored field positively: the switch carries
// Invert's words and is on when the stored value is false (§5.5 item 7). On
// posts DOC's "false" first, off the hidden "true"; with MasterChecks "0"
// and "1" (CD-353 (4)).
func TestFieldInput_CheckInvertBothWays(t *testing.T) {
	f := doctype.Field{Name: "disabled", Label: "Disabled", Type: doctype.Check, Invert: "Active"}
	for _, d := range []struct {
		deps    docscreen.Deps
		yes, no string
	}{{docscreen.Deps{}, "true", "false"}, {docscreen.Deps{MasterChecks: true}, "1", "0"}} {
		for _, c := range []struct {
			value string
			on    bool
		}{{"0", true}, {"false", true}, {"", true}, {"1", false}, {"true", false}} {
			doc := uitest.Render(t, docscreen.FieldInput(f, "f-disabled", c.value, "", d.deps))
			in := doc.Find(`input#f-disabled[role="switch"]`)
			if _, on := in.Attr("checked"); on != c.on {
				t.Errorf("stored %q: on = %v, want %v", c.value, on, c.on)
			}
			if v, _ := in.Attr("value"); v != d.no {
				t.Errorf("MasterChecks %v, stored %q: an inverted switch posts %q when on, want %s", d.deps.MasterChecks, c.value, v, d.no)
			}
			if v, _ := doc.Find(`input[type="hidden"][name="disabled"]`).Attr("value"); v != d.yes {
				t.Errorf("MasterChecks %v, stored %q: the value posted when off = %q, want %s", d.deps.MasterChecks, c.value, v, d.yes)
			}
			if got := strings.TrimSpace(doc.Find(".switch-row__label").Text()); got != "Active" {
				t.Errorf("label = %q, want Active", got)
			}
		}
	}
}

func selectField(labels ...string) doctype.Field {
	f := doctype.Field{Name: "kind", Label: "Kind", Type: doctype.Select}
	for i := range labels {
		f.Options = append(f.Options, "o"+strconv.Itoa(i))
	}
	f.OptionLabels = map[string]string{}
	for i, l := range labels {
		f.OptionLabels[f.Options[i]] = l
	}
	return f
}

// The option-count rule (§5.5 item 8, §10 B4): five or more options are a
// select; fewer are segmented when every label has 12 characters or fewer,
// else tiles.
func TestFieldInput_SelectOptionCountRule(t *testing.T) {
	for _, c := range []struct {
		name   string
		labels []string
		want   string
	}{
		{"4 short", []string{"One", "Two", "Three", "Twelve chars"}, "segmented"},
		{"4 with a 13-character label", []string{"One", "Two", "Three", "Thirteen char"}, "tiles"},
		{"13-character label first", []string{"Thirteen char", "One", "Two"}, "tiles"},
		{"12 characters of ₹, more bytes", []string{"₹₹₹₹₹₹₹₹₹₹₹₹", "One"}, "segmented"},
		{"12 Devanagari characters", []string{"कखगघङचछजझञटठ", "One"}, "segmented"},
		{"5 short", []string{"A", "B", "C", "D", "E"}, "select"},
		{"2 short", []string{"Yes", "No"}, "segmented"},
	} {
		doc := uitest.Render(t, docscreen.FieldInput(selectField(c.labels...), "f-kind", "o1", "", docscreen.Deps{}))
		got := ""
		switch {
		case doc.Find("select#f-kind").Length() == 1:
			got = "select"
		case doc.Find(".segmented.segmented--tiles input#f-kind[type=radio]").Length() == 1:
			got = "tiles"
		case doc.Find(".segmented input#f-kind[type=radio]").Length() == 1:
			got = "segmented"
		}
		if got != c.want {
			t.Errorf("%s: renders as %q, want %q", c.name, got, c.want)
		}
		if n := doc.Find(`[name="kind"]`).Length(); got != "select" && n != len(c.labels) {
			t.Errorf("%s: %d radios, want %d", c.name, n, len(c.labels))
		}
		chosen := "checked"
		if got == "select" {
			chosen = "selected"
		}
		if _, ok := doc.Find(`[value="o1"]`).Attr(chosen); !ok {
			t.Errorf("%s: value o1 not chosen", c.name)
		}
	}
}

// A-21: GST category (five categories) renders as a <select> (R13).
func TestFieldInput_GSTCategoryIsSelect(t *testing.T) {
	dt, ok := doctype.Lookup(party.DocType)
	if !ok {
		t.Fatal("party is not registered")
	}
	for _, f := range dt.Fields {
		if f.Name != "gst_category" {
			continue
		}
		doc := uitest.Render(t, docscreen.FieldInput(f, "f-gst_category", "", "", docscreen.Deps{}))
		if doc.Find(`select#f-gst_category[name="gst_category"]`).Length() != 1 {
			t.Error("GST category is not a select (A-21)")
		}
		if doc.Find(`input[type=radio]`).Length() != 0 {
			t.Error("GST category still renders radios")
		}
		return
	}
	t.Fatal("party has no gst_category field")
}
