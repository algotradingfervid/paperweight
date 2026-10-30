package csvio

import (
	"reflect"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list/listtest"
)

// nameShared is a master whose stored Email isn't Importable; its ImportOnly
// field has the same label and is declared after it, so a walk of the
// declared fields would reach the stored one first.
const nameShared = "csviotest.SharedLabel"

func init() {
	doctype.Register(doctype.DocType{
		Name: nameShared, Label: "Shared", PluralLabel: "Shared", Table: "numt_csvio_shared",
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Type: doctype.Text, Importable: true},
			{Name: "stored_email", Label: "Email", Type: doctype.Text},
			{Name: "email", Label: "Email", Type: doctype.Text, Importable: true, ImportOnly: true},
			{Name: "disabled", Label: "Disabled", Type: doctype.Check, Filterable: true},
		},
		Master: listtest.TableLookup("numt_csvio_shared"),
	})
}

func TestMapHeader_ImportFieldWinsSameLabel(t *testing.T) {
	dt, ok := doctype.Lookup(nameShared)
	if !ok {
		t.Fatalf("%s not registered", nameShared)
	}
	cols, warns, err := MapHeader(dt, []string{"Code", "Email"})
	if err != nil {
		t.Fatalf("MapHeader: %v", err)
	}
	if got, want := colNames(cols), []string{"0:code", "1:email"}; !reflect.DeepEqual(got, want) {
		t.Errorf("cols = %q, want %q", got, want)
	}
	if len(warns) != 0 {
		t.Errorf("warns = %q, want none", warns)
	}
}
