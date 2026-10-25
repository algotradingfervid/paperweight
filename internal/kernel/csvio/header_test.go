package csvio

import (
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list/listtest"
)

// colNames returns "index:field" pairs for comparison.
func colNames(cols []ColumnMap) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = strconv.Itoa(c.Index) + ":" + c.Field.Name
	}
	return out
}

func TestImport_HeaderMapping(t *testing.T) { // NUM-141
	cols, warns, err := MapHeader(listtest.Item(), []string{"\uFEFFcode", "  ITEM NAME ", "type", "uom", "hsn_sac"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("warns = %q, want none", warns)
	}
	want := []string{"0:code", "1:name", "2:item_type", "3:uom", "4:hsn_sac"}
	if got := colNames(cols); !reflect.DeepEqual(got, want) {
		t.Fatalf("cols = %q, want %q", got, want)
	}
	// Labels with runs of spaces and underscores also match.
	cols, warns, err = MapHeader(listtest.Item(), []string{"CODE", "Name", "Type", "UoM", "HSN/SAC", "Standard__Rate", "standard   rate"})
	if err == nil || err.Error() != "Column 'Standard Rate' appears twice. Keep one." {
		t.Fatalf("err = %v, warns = %q, cols = %q", err, warns, colNames(cols))
	}
}

func TestImport_RequiredAndDuplicateColumns(t *testing.T) { // NUM-142
	_, _, err := MapHeader(listtest.Party(), []string{"Code", "GSTIN"})
	assertFileError(t, err, "Column 'Name' is missing. Add it to the first row.")
	_, _, err = MapHeader(listtest.Party(), []string{"Code", "Name", "GSTIN", "GSTIN"})
	assertFileError(t, err, "Column 'GSTIN' appears twice. Keep one.")
}

func TestImport_UnknownColumnWarning(t *testing.T) { // NUM-143
	cols, warns, err := MapHeader(listtest.Item(), []string{"Code", "Name", "Type", "UoM", "Colour"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := []string{"Column 'Colour' isn't a field of Item and will be ignored."}
	if !reflect.DeepEqual(warns, want) {
		t.Fatalf("warns = %q, want %q", warns, want)
	}
	if got := colNames(cols); !reflect.DeepEqual(got, []string{"0:code", "1:name", "2:item_type", "3:uom"}) {
		t.Fatalf("cols = %q", got)
	}
}

func TestMapHeader_NotImportableWarning(t *testing.T) {
	dt := &doctype.DocType{Name: "x", Label: "Thing", Fields: []doctype.Field{
		{Name: "code", Label: "Code", Required: true, Importable: true},
		{Name: "total", Label: "Total", Required: true},
	}}
	cols, warns, err := MapHeader(dt, []string{"Code", "Total", ""})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := []string{"Column 'Total' can't be imported and will be ignored."}
	if !reflect.DeepEqual(warns, want) {
		t.Fatalf("warns = %q, want %q", warns, want)
	}
	if got := colNames(cols); !reflect.DeepEqual(got, []string{"0:code"}) {
		t.Fatalf("cols = %q", got)
	}
}

func assertFileError(t *testing.T, err error, msg string) {
	t.Helper()
	var fe *FileError
	if !errors.As(err, &fe) {
		t.Fatalf("err = %#v, want *FileError %q", err, msg)
	}
	if fe.Error() != msg {
		t.Fatalf("err = %q, want %q", fe.Error(), msg)
	}
	var he doctype.HTTPError = fe
	if he.Status() != 422 || he.FieldErrors() != nil {
		t.Fatalf("Status = %d, FieldErrors = %v", he.Status(), he.FieldErrors())
	}
}
