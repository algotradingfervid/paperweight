package print_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/platform/db"
)

func emptyView(context.Context, *db.Tx, int64) (templ.Component, error) {
	return templ.NopComponent, nil
}

func mustPanic(t *testing.T, what string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s did not panic", what)
		}
	}()
	f()
}

func TestRegistry_Register(t *testing.T) {
	reg := print.NewRegistry()
	reg.Register(testPO, emptyView)
	if _, ok := reg.Lookup(testPO); !ok {
		t.Fatalf("Lookup(%s) after Register = not found", testPO)
	}
	if _, ok := reg.Lookup("test.Other"); ok {
		t.Errorf("Lookup(test.Other) found a view")
	}
	mustPanic(t, "duplicate Register", func() { reg.Register(testPO, emptyView) })
	mustPanic(t, "nil view Register", func() { reg.Register("test.Nil", nil) })
	mustPanic(t, "empty name Register", func() { reg.Register("", emptyView) })
}

func TestRegistry_Missing(t *testing.T) {
	dts := []*doctype.DocType{
		{Name: testPO, Printable: true},
		{Name: testGRN, Printable: true},
		{Name: testExpense, Printable: true},
		{Name: "test.NoTemplate", Printable: true},
		{Name: "test.Master"},
	}
	names := print.PrintableNames(dts)
	if want := []string{testPO, testGRN, testExpense, "test.NoTemplate"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("PrintableNames = %v, want %v", names, want)
	}
	reg := print.NewRegistry()
	reg.Register(testPO, emptyView)
	reg.Register(testGRN, func(context.Context, *db.Tx, int64) (templ.Component, error) {
		return print.GRNDocument(print.GRNDoc{}), nil
	})
	reg.Register(testExpense, func(context.Context, *db.Tx, int64) (templ.Component, error) {
		return print.ExpenseDocument(print.ExpenseDoc{}), nil
	})
	err := reg.Missing(names)
	if err == nil || err.Error() != "No print template for DocType test.NoTemplate." {
		t.Fatalf("Missing = %v, want No print template for DocType test.NoTemplate.", err)
	}
	reg.Register("test.NoTemplate", emptyView)
	if err := reg.Missing(names); err != nil {
		t.Errorf("Missing after registering all = %v, want nil", err)
	}
}
