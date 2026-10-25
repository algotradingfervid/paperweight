package doc

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// cd144DT declares the convention column "number" as a display field (CD-144).
var cd144DT = func() *DocType {
	doctype.Register(DocType{Name: "t.DocCD144", Label: "CD-144", PluralLabel: "CD-144s", Table: "t_doc_cd144",
		LinesTable: "t_doc_cd144_line", Hooks: cd144Hooks{},
		Fields: []Field{
			{Name: "number", Label: "Number", Type: Text},
			{Name: "note", Label: "Note", Type: Text},
		}})
	dt, _ := doctype.Lookup("t.DocCD144")
	return dt
}()

// cd144Hooks are no-op hooks, so the engine treats t.DocCD144 as a document.
type cd144Hooks struct{}

func (cd144Hooks) Validate(context.Context, *db.Tx, int64) []FieldError       { return nil }
func (cd144Hooks) BeforeSubmit(context.Context, *db.Tx, int64) error          { return nil }
func (cd144Hooks) ApplyEffects(context.Context, *db.Tx, int64) error          { return nil }
func (cd144Hooks) ReverseEffects(context.Context, *db.Tx, int64) error        { return nil }
func (cd144Hooks) Amount(context.Context, *db.Tx, int64) (types.Money, error) { return 0, nil }
func (cd144Hooks) Date(context.Context, *db.Tx, int64) (types.Date, error)    { return types.Date{}, nil }

// CD-144: a convention-column field is never a user field: normalize,
// checkReadOnly and storedFields skip it. A posted number still goes through
// Numbers.Guard (refused on create, CD-28); see convention_engine_test.go.
func TestDoc_ConventionField_NotUserField(t *testing.T) {
	in := ActionInput{Values: map[string]string{"number": "PO-HACK", "note": "hi"}}
	for _, isCreate := range []bool{true, false} {
		n, errs := normalize(cd144DT, in, PlainValue, isCreate)
		if len(errs) > 0 {
			t.Errorf("create=%v: normalize errors %v", isCreate, errs)
		}
		if _, ok := n.Header["number"]; ok {
			t.Errorf("create=%v: normalize kept number = %v", isCreate, n.Header["number"])
		}
	}
	if errs := checkReadOnly(cd144DT, in, &Doc{Header: Values{"number": "PO-1"}}); len(errs) > 0 {
		t.Errorf("checkReadOnly = %v, want none", errs)
	}
	for _, f := range storedFields(cd144DT, false) {
		if f.Name == "number" {
			t.Errorf("storedFields includes number: create and save would write it")
		}
	}
}
