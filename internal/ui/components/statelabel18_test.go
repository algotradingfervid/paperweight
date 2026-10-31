package components_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// M0-18 (CD-263 G7): a DataList row's state badge shows Row.StateLabel in
// place of the component's word, keeping the state's tint and icon; a blank
// StateLabel keeps the component's own word (overrides only).
func TestDataList_StateLabel(t *testing.T) {
	cols := []components.Column{
		{Key: "no", Label: "Number", Priority: components.PTitle},
		{Key: "state", Label: "State", Priority: components.PState},
	}
	rows := []struct {
		no    string
		state doctype.State
		label string
		want  string
		icon  string
	}{
		{"PR-1", doctype.StateApproved, "Submitted", "Submitted", "check-circle"},
		{"PR-2", doctype.StatePending, "With the buyer", "With the buyer", "clock"},
		{"PR-3", doctype.StateApproved, "", "Approved", "check-circle"},
		{"PR-4", doctype.StatePending, "", "Pending approval", "clock"},
		{"PR-5", "on_hold", "Held", "Held", ""},
	}
	p := components.DataListProps{Label: "Requests", Columns: cols}
	for _, r := range rows {
		p.Rows = append(p.Rows, components.Row{
			Href: "/doc/x/" + r.no, Values: map[string]string{"no": r.no}, State: r.state, StateLabel: r.label,
		})
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	items := uitest.Render(t, components.DataList(p)).Find("ul.datalist-rows > li")
	if items.Length() != len(rows) {
		t.Fatalf("rendered %d rows, want %d", items.Length(), len(rows))
	}
	for i, r := range rows {
		t.Run(r.no, func(t *testing.T) {
			b := items.Eq(i).Find("span.badge")
			if b.Length() != 1 {
				t.Fatalf("row %s: want one span.badge, got %d", r.no, b.Length())
			}
			if got := text(b); got != r.want {
				t.Errorf("row %s (state %s, StateLabel %q): badge text = %q, want %q", r.no, r.state, r.label, got, r.want)
			}
			wantClass := "badge--" + string(r.state)
			if r.icon == "" {
				wantClass = "badge--neutral"
			}
			if !b.HasClass(wantClass) {
				t.Errorf("row %s: badge class = %q, want %s kept with the label", r.no, b.AttrOr("class", ""), wantClass)
			}
			if r.icon != "" {
				if svg, _ := b.Find("svg").Html(); svg != iconHTML(t, r.icon) {
					t.Errorf("row %s: badge icon is not %s", r.no, r.icon)
				}
			}
		})
	}
}
