package purchase

import (
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
)

// relabelled is a DocType of fields with field's Label changed to label.
func relabelled(fields []doctype.Field, field, label string) *doctype.DocType {
	fields = slices.Clone(fields)
	for i := range fields {
		if fields[i].Name == field {
			fields[i].Label = label
		}
	}
	return &doctype.DocType{Fields: fields}
}

// R13 fix1: an unresolved UoM or tax code is refused with the label its field
// declares, so renaming the field renames the refusal.
func TestR13_BlankLabelsFollowDeclaration(t *testing.T) {
	order := relabelled(relabelled(orderFields(), "uom_id", "Order unit").Fields, "tax_code_id", "GST code")
	got := orderBlanks(order, []dbq.OrderLinesForValidateRow{{LineNo: 2}, {LineNo: 3, UomID: 1, TaxCodeID: 1}})
	want := []doctype.FieldError{
		{Field: "uom_id", Line: 2, Message: "Line 2: Order unit is required."},
		{Field: "tax_code_id", Line: 2, Message: "Line 2: GST code is required."},
	}
	if !slices.Equal(got, want) {
		t.Errorf("order blanks = %+v, want %+v", got, want)
	}
	request := relabelled(requestFields(), "uom_id", "Request unit")
	got = requestBlanks(request, []dbq.RequestLinesForValidateRow{{LineNo: 4}, {LineNo: 5, UomID: 1}})
	if want := []doctype.FieldError{{Field: "uom_id", Line: 4, Message: "Line 4: Request unit is required."}}; !slices.Equal(got, want) {
		t.Errorf("request blanks = %+v, want %+v", got, want)
	}
}
