package doc

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// WP1-DOC Task 3: value parsing, normalization and field validation.

func i64p(v int64) *int64 { return &v }

// testDT is the Task 3 test DocType. It is not registered.
func testDT() *DocType {
	return &DocType{
		Name:        "t.Order",
		Label:       "Test order",
		PluralLabel: "Test orders",
		Table:       "t_order",
		LinesTable:  "t_order_line",
		Submittable: true,
		Fields: []Field{
			{Name: "supplier_id", Column: "supplier_id", Label: "Supplier", Type: Link, Link: "t.Party", Required: true},
			{Name: "order_date", Column: "order_date", Label: "Order date", Type: Date, Required: true},
			{Name: "supplier_ref", Column: "supplier_ref", Label: "Supplier ref", Type: Text, MaxLen: 140},
			{Name: "credit_days", Column: "credit_days", Label: "Credit days", Type: Int, Min: i64p(0), Max: i64p(365)},
			{Name: "is_urgent", Column: "is_urgent", Label: "Urgent", Type: Check, Required: true},
			{Name: "item_id", Column: "item_id", Label: "Item", Type: Link, Link: "t.Item", Required: true, OnLine: true},
			{Name: "qty", Column: "qty", Label: "Quantity", Type: Qty, Required: true, Positive: true, OnLine: true},
			{Name: "rate", Column: "rate", Label: "Rate", Type: Rate, NonNegative: true, OnLine: true},
			{Name: "description", Column: "description", Label: "Description", Type: Text, OnLine: true},
		},
	}
}

// mapMaster is a map-backed MasterLookup.
type mapMaster map[int64]MasterInfo

func (m mapMaster) Lookup(_ context.Context, _ *db.Tx, id int64) (MasterInfo, bool, error) {
	mi, ok := m[id]
	return mi, ok, nil
}

func testMasters() masterResolver {
	ms := map[string]MasterLookup{
		"t.Party": mapMaster{
			1: {Code: "SHREE", Name: "Shree Steel Traders"},
			3: {Code: "RAJKOT", Name: "Rajkot Castings", Disabled: true},
		},
		"t.Item": mapMaster{
			10: {Code: "MS-ANGLE-50", Name: "MS Angle 50x50x5"},
			11: {Code: "CEMENT-OPC", Name: "OPC Cement 53 Grade", Disabled: true},
		},
	}
	return func(target string) (MasterLookup, bool) {
		m, ok := ms[target]
		return m, ok
	}
}

func validHeader() map[string]string {
	return map[string]string{
		"supplier_id":  "1",
		"order_date":   "2026-10-24",
		"supplier_ref": "",
		"credit_days":  "30",
		"is_urgent":    "false",
	}
}

func validLine() map[string]string {
	return map[string]string{"item_id": "10", "qty": "1200.000", "rate": "62.5000", "description": ""}
}

// validate runs normalize (which must succeed) and then checkFields.
func validate(t *testing.T, dt *DocType, in ActionInput, requireLines bool) (normalized, []FieldError, []string) {
	t.Helper()
	n, nerrs := normalize(dt, in, PlainValue, true)
	if len(nerrs) != 0 {
		t.Fatalf("normalize errors = %v, want none", nerrs)
	}
	errs, warnings := checkFields(context.Background(), nil, dt, &n, requireLines, testMasters())
	return n, errs, warnings
}

func eqErrs(t *testing.T, got, want []FieldError) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("field errors:\n got %#v\nwant %#v", got, want)
	}
}

func hasErr(errs []FieldError, want FieldError) bool {
	for _, e := range errs {
		if e == want {
			return true
		}
	}
	return false
}

// DOC-019
func TestDoc_Validate_MandatoryCollected(t *testing.T) {
	h := validHeader()
	delete(h, "supplier_id")
	delete(h, "order_date")
	_, errs, _ := validate(t, testDT(), ActionInput{Values: h, Lines: []map[string]string{validLine()}}, false)
	eqErrs(t, errs, []FieldError{
		{Field: "supplier_id", Line: 0, Message: "Supplier is required."},
		{Field: "order_date", Line: 0, Message: "Order date is required."},
	})
}

// DOC-020
func TestDoc_Validate_MandatoryBlankRules(t *testing.T) {
	dt := testDT()
	for i := range dt.Fields {
		if dt.Fields[i].Name == "supplier_ref" {
			dt.Fields[i].Required = true
		}
	}
	h := validHeader()
	h["supplier_ref"] = "   "
	h["is_urgent"] = "false"
	l := validLine()
	l["qty"] = "0"
	_, errs, _ := validate(t, dt, ActionInput{Values: h, Lines: []map[string]string{l}}, false)

	if !hasErr(errs, FieldError{Field: "supplier_ref", Line: 0, Message: "Supplier ref is required."}) {
		t.Errorf("errors %#v: want supplier_ref \"Supplier ref is required.\"", errs)
	}
	for _, e := range errs {
		if (e.Field == "qty" || e.Field == "is_urgent") && strings.Contains(e.Message, "is required") {
			t.Errorf("unexpected required error for a zero/false value: %#v", e)
		}
	}
	// qty 0 is filled; the separate positive rule (DOC-033) still applies.
	if !hasErr(errs, FieldError{Field: "qty", Line: 1, Message: "Line 1: Quantity must be more than 0."}) {
		t.Errorf("errors %#v: want the qty positive error", errs)
	}
}

// DOC-021
func TestDoc_Validate_LineMandatoryRowNo(t *testing.T) {
	l2 := validLine()
	l2["qty"] = ""
	in := ActionInput{Values: validHeader(), Lines: []map[string]string{validLine(), l2, validLine()}}
	_, errs, _ := validate(t, testDT(), in, false)
	want := FieldError{Field: "qty", Line: 2, Message: "Line 2: Quantity is required."}
	eqErrs(t, errs, []FieldError{want})
	if len(errs) == 1 {
		if got := errs[0].Path(); got != "lines[2].qty" {
			t.Errorf("Path() = %q, want %q", got, "lines[2].qty")
		}
	}
}

// DOC-022
func TestDoc_Validate_AtLeastOneLine(t *testing.T) {
	blank := func() map[string]string {
		return map[string]string{"item_id": "", "qty": "", "rate": "", "description": ""}
	}
	cases := map[string][]map[string]string{
		"no lines":    nil,
		"blank lines": {blank(), blank()},
	}
	for name, lines := range cases {
		t.Run(name, func(t *testing.T) {
			in := ActionInput{Values: validHeader(), Lines: lines}
			n, errs, _ := validate(t, testDT(), in, false)
			if len(n.Lines) != 0 {
				t.Errorf("normalized lines = %d, want 0", len(n.Lines))
			}
			eqErrs(t, errs, nil)

			_, errs, _ = validate(t, testDT(), in, true)
			eqErrs(t, errs, []FieldError{{Field: "lines", Line: 0, Message: "Add at least one line."}})
		})
	}
}

// DOC-023
func TestDoc_Validate_NonNegativeMinMax(t *testing.T) {
	t.Run("rate -10", func(t *testing.T) {
		l := validLine()
		l["rate"] = "-10.0000"
		_, errs, _ := validate(t, testDT(), ActionInput{Values: validHeader(), Lines: []map[string]string{l}}, false)
		eqErrs(t, errs, []FieldError{{Field: "rate", Line: 1, Message: "Line 1: Rate can't be negative."}})
	})
	for _, tc := range []struct {
		days string
		want []FieldError
	}{
		{"400", []FieldError{{Field: "credit_days", Line: 0, Message: "Credit days must be between 0 and 365."}}},
		{"30", nil},
		{"", nil},
	} {
		t.Run("credit_days "+tc.days, func(t *testing.T) {
			h := validHeader()
			h["credit_days"] = tc.days
			_, errs, _ := validate(t, testDT(), ActionInput{Values: h, Lines: []map[string]string{validLine()}}, false)
			eqErrs(t, errs, tc.want)
		})
	}
}

// DOC-032
func TestDoc_Link_MissingMaster(t *testing.T) {
	h := validHeader()
	h["supplier_id"] = "9999"
	l2 := validLine()
	l2["item_id"] = "9999"
	_, errs, _ := validate(t, testDT(), ActionInput{Values: h, Lines: []map[string]string{validLine(), l2}}, false)
	eqErrs(t, errs, []FieldError{
		{Field: "supplier_id", Line: 0, Message: "Supplier not found. Pick one from the list."},
		{Field: "item_id", Line: 2, Message: "Line 2: Item not found. Pick one from the list."},
	})
}

// DOC-033
func TestDoc_Line_QtyPositive(t *testing.T) {
	l1, l2 := validLine(), validLine()
	l1["qty"] = "0"
	l2["qty"] = "-1"
	_, errs, _ := validate(t, testDT(), ActionInput{Values: validHeader(), Lines: []map[string]string{l1, l2}}, false)
	eqErrs(t, errs, []FieldError{
		{Field: "qty", Line: 1, Message: "Line 1: Quantity must be more than 0."},
		{Field: "qty", Line: 2, Message: "Line 2: Quantity must be more than 0."},
	})
}

// DOC-043
func TestDoc_Lines_BlankDropped(t *testing.T) {
	lines := []map[string]string{
		validLine(),
		{"item_id": "", "qty": "", "rate": "", "description": ""},
		{"item_id": "", "qty": "", "rate": "", "description": "loading charges"},
	}
	n, errs, _ := validate(t, testDT(), ActionInput{Values: validHeader(), Lines: lines}, false)
	if len(n.Lines) != 2 {
		t.Fatalf("normalized lines = %d, want 2", len(n.Lines))
	}
	if got := n.Lines[1].Values["description"]; got != "loading charges" {
		t.Errorf("line 2 description = %#v, want %q", got, "loading charges")
	}
	if !hasErr(errs, FieldError{Field: "item_id", Line: 2, Message: "Line 2: Item is required."}) {
		t.Errorf("errors %#v: want item_id line 2 \"Line 2: Item is required.\"", errs)
	}
	for _, e := range errs {
		if e.Line == 3 {
			t.Errorf("error on line 3 after the blank line was dropped: %#v", e)
		}
	}
}

// Review Focus 2
func TestDoc_Validate_UnreadableValue(t *testing.T) {
	h := validHeader()
	h["supplier_id"] = "abc"
	h["order_date"] = "24/10/2026"
	h["credit_days"] = "3O"
	l := validLine()
	l["qty"] = "12.5.3"
	_, errs := normalize(testDT(), ActionInput{Values: h, Lines: []map[string]string{l}}, PlainValue, true)

	want := []FieldError{
		{Field: "supplier_id", Line: 0, Message: "Supplier isn't a valid value. Check it and try again."},
		{Field: "order_date", Line: 0, Message: "Order date isn't a valid value. Check it and try again."},
		{Field: "credit_days", Line: 0, Message: "Credit days isn't a valid value. Check it and try again."},
		{Field: "qty", Line: 1, Message: "Line 1: Quantity isn't a valid value. Check it and try again."},
	}
	byPath := func(s []FieldError) {
		sort.Slice(s, func(i, j int) bool { return s[i].Path() < s[j].Path() })
	}
	got := append([]FieldError(nil), errs...)
	byPath(got)
	byPath(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("normalize errors:\n got %#v\nwant %#v", got, want)
	}
}

func TestDoc_Normalize_DropsSystemAndRollUp(t *testing.T) {
	dt := testDT()
	dt.Fields = append(dt.Fields,
		Field{Name: "approved_at", Column: "approved_at", Label: "Approved at", Type: Timestamp, System: true},
		Field{Name: "received_qty", Column: "received_qty", Label: "Received", Type: Qty, RollUp: true, OnLine: true},
	)
	h := validHeader()
	h["state"] = "approved"
	h["version"] = "7"
	h["created_by"] = "1"
	h["number"] = "PO/26-27/00001"
	h["approved_at"] = "2026-10-24T05:00:00.000Z"
	l := validLine()
	l["received_qty"] = "5.000"

	n, errs := normalize(dt, ActionInput{Values: h, Lines: []map[string]string{l}}, PlainValue, true)
	if len(errs) != 0 {
		t.Fatalf("normalize errors = %v, want none", errs)
	}
	for _, k := range []string{"state", "version", "created_by", "number", "approved_at"} {
		if v, ok := n.Header[k]; ok {
			t.Errorf("header key %q kept (%#v), want absent", k, v)
		}
	}
	if got := n.Header["supplier_id"]; got != int64(1) {
		t.Errorf("header supplier_id = %#v, want int64(1)", got)
	}
	if len(n.Lines) != 1 {
		t.Fatalf("normalized lines = %d, want 1", len(n.Lines))
	}
	if v, ok := n.Lines[0].Values["received_qty"]; ok {
		t.Errorf("line key received_qty kept (%#v), want absent", v)
	}
	if got := n.Lines[0].Values["qty"]; got != types.Qty(1200000) {
		t.Errorf("line qty = %#v, want types.Qty(1200000)", got)
	}
}

func TestDoc_CheckReadOnly(t *testing.T) {
	dt := testDT()
	dt.Fields = append(dt.Fields, Field{Name: "total", Column: "total", Label: "Total", Type: Money, ReadOnly: true})
	errTotal := FieldError{Field: "total", Line: 0, Message: "Total is calculated and can't be entered."}
	in := func(total string) ActionInput {
		h := validHeader()
		h["total"] = total
		return ActionInput{Values: h, Lines: []map[string]string{validLine()}}
	}

	eqErrs(t, checkReadOnly(dt, in("10.00"), nil), []FieldError{errTotal})
	eqErrs(t, checkReadOnly(dt, in(""), nil), nil)

	current := &Doc{Header: Values{"total": types.Money(1000)}}
	eqErrs(t, checkReadOnly(dt, in("10.00"), current), nil)
	eqErrs(t, checkReadOnly(dt, in("11.00"), current), []FieldError{errTotal})
}

func TestPlainValue_Scales(t *testing.T) {
	f := func(ft FieldType) Field { return Field{Name: "v", Label: "Value", Type: ft} }
	sel := Field{Name: "mode", Label: "Mode", Type: Select, Options: []string{"Cash", "Bank"}}

	ok := []struct {
		f    Field
		in   string
		want any
	}{
		{f(Qty), "1200.000", types.Qty(1200000)},
		{f(Qty), "1200", types.Qty(1200000)},
		{f(Rate), "62.5000", types.Rate(625000)},
		{f(Money), "94164.00", types.Money(9416400)},
		{f(Percent), "18", types.Percent(18000)},
		{f(Date), "2026-10-24", types.Date{Year: 2026, Month: time.October, Day: 24}},
		{f(Check), "true", true},
		{sel, "Cash", "Cash"},
		{f(Qty), "", nil},
	}
	for _, tc := range ok {
		got, err := PlainValue(tc.f, tc.in)
		if err != nil {
			t.Errorf("PlainValue(%v, %q) error %v", tc.f.Type, tc.in, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("PlainValue(%v, %q) = %#v, want %#v", tc.f.Type, tc.in, got, tc.want)
		}
	}

	got, err := PlainValue(f(Timestamp), "2026-10-24T05:00:00.000Z")
	ts, isTime := got.(time.Time)
	if err != nil || !isTime || !ts.Equal(time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)) || ts.Location() != time.UTC {
		t.Errorf("PlainValue(Timestamp) = %#v, %v; want 2026-10-24T05:00:00Z UTC", got, err)
	}

	for _, tc := range []struct {
		f  Field
		in string
	}{
		{f(Qty), "1200.0001"},
		{sel, "Cheque"},
	} {
		if v, err := PlainValue(tc.f, tc.in); err == nil {
			t.Errorf("PlainValue(%v, %q) = %#v, want an error", tc.f.Type, tc.in, v)
		}
	}
}

func TestFormatPlain_RoundTrip(t *testing.T) {
	f := func(ft FieldType) Field { return Field{Name: "v", Label: "Value", Type: ft} }

	for _, tc := range []struct {
		f    Field
		v    any
		want string
	}{
		{f(Qty), types.Qty(1200000), "1200.000"},
		{f(Rate), types.Rate(625000), "62.5000"},
		{f(Money), types.Money(9416400), "94164.00"},
		{f(Percent), types.Percent(18000), "18.000"},
		{f(Qty), nil, ""},
	} {
		if got := FormatPlain(tc.f, tc.v); got != tc.want {
			t.Errorf("FormatPlain(%v, %#v) = %q, want %q", tc.f.Type, tc.v, got, tc.want)
		}
	}

	sel := Field{Name: "mode", Label: "Mode", Type: Select, Options: []string{"Cash", "Bank"}}
	ts := time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		f Field
		v any
	}{
		{f(Text), "Shree Steel Traders"},
		{f(Int), int64(30)},
		{f(Check), true},
		{f(Check), false},
		{sel, "Bank"},
		{f(Link), int64(10)},
		{f(Money), types.Money(9416400)},
		{f(Qty), types.Qty(1200000)},
		{f(Rate), types.Rate(625000)},
		{f(Percent), types.Percent(18000)},
		{f(Factor), types.Factor(1500000000)},
		{f(Date), types.Date{Year: 2026, Month: time.October, Day: 24}},
		{f(Timestamp), ts},
		{f(Money), nil},
	} {
		s := FormatPlain(tc.f, tc.v)
		got, err := PlainValue(tc.f, s)
		if err != nil {
			t.Errorf("PlainValue(%v, FormatPlain(%#v) = %q) error %v", tc.f.Type, tc.v, s, err)
			continue
		}
		if want, isTime := tc.v.(time.Time); isTime {
			g, ok := got.(time.Time)
			if !ok || !g.Equal(want) || g.Location() != time.UTC {
				t.Errorf("Timestamp round trip via %q = %#v, want %v UTC", s, got, want)
			}
			continue
		}
		if !reflect.DeepEqual(got, tc.v) {
			t.Errorf("%v round trip via %q = %#v, want %#v", tc.f.Type, s, got, tc.v)
		}
	}
}
