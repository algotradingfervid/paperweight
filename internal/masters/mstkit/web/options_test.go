package mstweb_test

import (
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
)

// No case ID: OptionLabels, a Select's words for its stored values (CD-175).
// Item's Goods has a label here and Service none.

func labelled(s *screen) {
	s.OptionLabels = map[string]map[string]string{"item_type": {"Goods": "Physical goods"}}
}

func itemTypes(t *testing.T) []webtest.Choice {
	t.Helper()
	h := harness(t, stubService{})
	return h.Get(t, h.Env.Kavya, base+"/new").Choices("item_type")
}

// The form shows the labels and posts the stored values; a value without a
// label shows as stored. The ticked value and the field's error are as on any
// radio group, and the options keep the Field's order.
func TestScreens_OptionLabelsOnTheForm(t *testing.T) {
	var got stubRecord
	h := harness(t, stubService{in: &got, rec: stubRecord{"item_type": "Goods"}, err: &mstkit.ValidationError{Fields: []doctype.FieldError{
		{Field: "item_type", Message: "Item type is wrong."},
	}}}, labelled)

	page := h.Get(t, h.Env.Kavya, record)
	webtest.WantStatus(t, page, http.StatusOK)
	want := itemTypes(t)
	for i := range want {
		if want[i].Value == "Goods" {
			want[i].Label = "Physical goods"
		}
	}
	if choices := page.Choices("item_type"); !reflect.DeepEqual(choices, want) {
		t.Errorf("choices = %+v; want %+v", choices, want)
	}
	if v, ok := page.Doc.Find(`input[name="item_type"][checked]`).Attr("value"); !ok || v != "Goods" {
		t.Errorf("ticked = %q (found %v); want the stored Goods", v, ok)
	}

	resp := h.Post(t, h.Env.Kavya, record, url.Values{"version": {"3"}, "item_type": {"Goods"}})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, "item_type", "Item type is wrong.")
	if got["item_type"] != "Goods" {
		t.Errorf("service got item_type %q; want the stored Goods", got["item_type"])
	}
}

// The list and the read-only page show the label, a value without one as
// stored; the export keeps the stored value.
func TestScreens_OptionLabelsInListAndReadOnly(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{"item_type": "Goods"}}, labelled)
	id := seedItem(t, h, "M12")

	text, ok := h.Get(t, h.Env.Kavya, base).RowLink(stubPath(id))
	if !ok || !strings.Contains(text, "Physical goods") {
		t.Errorf("row = %q (found %v); want the label Physical goods", text, ok)
	}
	file := h.Get(t, h.Env.Kavya, base+"/export.csv")
	webtest.WantStatus(t, file, http.StatusOK)
	if strings.Contains(file.Body, "Physical goods") || !strings.Contains(file.Body, "Goods") {
		t.Errorf("export = %q; want the stored Goods, no label", file.Body)
	}

	read := h.Get(t, h.Env.Priya, record)
	webtest.WantStatus(t, read, http.StatusOK)
	webtest.WantText(t, read, "Physical goods")

	h = harness(t, stubService{rec: stubRecord{"item_type": "Service"}}, labelled)
	webtest.WantText(t, h.Get(t, h.Env.Priya, record), "Item typeService")
}

// Without OptionLabels a Select shows its stored values.
func TestScreens_OptionLabelsOff(t *testing.T) {
	for _, c := range itemTypes(t) {
		if c.Label != c.Value {
			t.Errorf("choice %+v; want the stored value as its label", c)
		}
	}
}

// A row's Select takes its labels by field name, on the form and read-only.
func TestScreens_OptionLabelsOnRows(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{"lines[1].side": "Right"}}, onRowsDocType, func(s *screen) {
		s.OptionLabels = map[string]map[string]string{"side": {"Left": "Port", "Right": "Starboard"}}
	})
	page := h.Get(t, h.Env.Kavya, record)
	webtest.WantStatus(t, page, http.StatusOK)
	want := []webtest.Choice{{Value: "Left", Label: "Port"}, {Value: "Right", Label: "Starboard"}}
	if got := page.Choices("lines[1].side"); !reflect.DeepEqual(got, want) {
		t.Errorf("side choices = %+v; want %+v", got, want)
	}
	webtest.WantText(t, h.Get(t, h.Env.Priya, record), "Starboard")
}
