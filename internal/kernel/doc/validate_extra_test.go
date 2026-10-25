package doc

import (
	"context"
	"reflect"
	"testing"
)

// A Required field whose value can't be read gets exactly one error, the
// unreadable one, in field-then-line order (review of WP1-DOC Task 3).
func TestDoc_Validate_UnreadableRequiredOneError(t *testing.T) {
	dt := testDT()
	dt.Fields = append(dt.Fields, Field{Name: "mode", Column: "mode", Label: "Mode", Type: Select, Options: []string{"Cash", "Bank"}, Required: true})
	h := validHeader()
	h["supplier_id"] = "abc"
	h["order_date"] = "24/10/2026"
	h["mode"] = "Cheque"
	l := validLine()
	l["qty"] = "12.5.3"

	n, errs := normalize(dt, ActionInput{Values: h, Lines: []map[string]string{validLine(), l}}, PlainValue, true)
	ferrs, _ := checkFields(context.Background(), nil, dt, &n, false, testMasters())
	got := append(errs, ferrs...)

	want := []FieldError{
		{Field: "supplier_id", Line: 0, Message: "Supplier isn't a valid value. Check it and try again."},
		{Field: "order_date", Line: 0, Message: "Order date isn't a valid value. Check it and try again."},
		{Field: "mode", Line: 0, Message: "Mode isn't a valid value. Check it and try again."},
		{Field: "qty", Line: 2, Message: "Line 2: Quantity isn't a valid value. Check it and try again."},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("errors:\n got %#v\nwant %#v", got, want)
	}
}

func TestFormatPlain_WrongTypePanics(t *testing.T) {
	defer func() {
		if r := recover(); r != "doc: FormatPlain got string for Money field \"total\"" {
			t.Errorf("recover() = %#v", r)
		}
	}()
	FormatPlain(Field{Name: "total", Type: Money}, "10.00")
}
