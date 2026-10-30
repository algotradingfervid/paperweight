package doc_test

import (
	"reflect"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
)

// History reads audit values back in plain form: the stored numbers at their
// scale, null as nothing, a master link as its code (DOC-087, DOC-089).
func TestAuditPlain(t *testing.T) {
	for _, c := range []struct {
		name string
		f    doc.Field
		raw  string
		want string
	}{
		{"null", doc.Field{Type: doc.Text}, `null`, ""},
		{"text", doc.Field{Type: doc.Text}, `"Deliver \"now\""`, `Deliver "now"`},
		{"text of digits", doc.Field{Type: doc.Text}, `"007"`, "007"},
		{"money", doc.Field{Type: doc.Money}, `13000000`, "130000.00"},
		{"qty", doc.Field{Type: doc.Qty}, `400000`, "400.000"},
		{"rate", doc.Field{Type: doc.Rate}, `625000`, "62.5000"},
		{"percent", doc.Field{Type: doc.Percent}, `18000`, "18.000"},
		{"factor", doc.Field{Type: doc.Factor}, `1500000000`, "1.500000000"},
		{"int", doc.Field{Type: doc.Int}, `30`, "30"},
		{"check", doc.Field{Type: doc.Check}, `true`, "true"},
		{"check stored as a number", doc.Field{Type: doc.Check}, `1`, "1"},
		{"date", doc.Field{Type: doc.Date}, `"2026-10-24"`, "2026-10-24"},
		{"master link", doc.Field{Type: doc.Link}, `{"id":7,"code":"SHREE"}`, "SHREE"},
		{"document link", doc.Field{Type: doc.Link}, `12`, "12"},
		{"not JSON", doc.Field{Type: doc.Text}, `r.jpg`, "r.jpg"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := doc.AuditPlain(c.f, c.raw); got != c.want {
				t.Errorf("AuditPlain(%s) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

// A line's add or remove change holds the whole line as one object.
func TestAuditLine(t *testing.T) {
	got := doc.AuditLine(`{"item_id":{"id":7,"code":"WIRE-18G"},"qty":50000,"uom":"kg"}`)
	want := map[string]string{"item_id": `{"id":7,"code":"WIRE-18G"}`, "qty": "50000", "uom": `"kg"`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("AuditLine = %v, want %v", got, want)
	}
	if got := doc.AuditLine(""); got != nil {
		t.Errorf("AuditLine of nothing = %v, want nil", got)
	}
}
