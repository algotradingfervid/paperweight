package docscreen

import (
	"fmt"
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// stateKey is the column key of the state badge taken from Info.State.
const stateKey = "state"

// ListColumns maps every field with ListPriority > 0 to a list column,
// ordered by priority then field order (CD-42). With no priority-1 field the
// title is Info.Display (Row.Title); a Submittable DocType with no
// priority-3 field gets its state badge from Info.State. More than 8
// columns is a registration bug and panics.
func ListColumns(dt *doctype.DocType) []components.Column {
	var fields []doctype.Field
	for _, f := range dt.Fields {
		if f.ListPriority > 0 && !f.OnLine {
			fields = append(fields, f)
		}
	}
	if dt.Submittable && !slices.ContainsFunc(fields, func(f doctype.Field) bool { return f.ListPriority == int(components.PState) }) {
		fields = append(fields, doctype.Field{Name: stateKey, Label: "State", ListPriority: int(components.PState)})
	}
	slices.SortStableFunc(fields, func(a, b doctype.Field) int { return a.ListPriority - b.ListPriority })
	cols := make([]components.Column, 0, len(fields))
	for _, f := range fields {
		p := min(components.Priority(f.ListPriority), components.PLow)
		cols = append(cols, components.Column{Key: f.Name, Label: f.Label, Priority: p, Num: p == components.PFigure})
	}
	if len(cols) > 8 {
		panic(fmt.Sprintf("docscreen: %s has %d list columns; at most 8 fit", dt.Name, len(cols)))
	}
	return cols
}
