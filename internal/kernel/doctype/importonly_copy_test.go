package doctype

import (
	"slices"
	"strings"
	"testing"
)

// A copy of a registered DocType carries its ImportOnly fields: registering
// the copy loses none of them, and a field the copy adds is seen.
func TestRegister_CopyKeepsImportOnly(t *testing.T) {
	r := newRegistry()
	r.Register(importedDocType())
	first, _ := r.Lookup("t.Imported")

	cp := *first
	cp.Name = "t.ImportedCopy"
	r = newRegistry()
	r.Register(cp)
	dt, ok := r.Lookup("t.ImportedCopy")
	if !ok {
		t.Fatal("copy not registered")
	}
	if got, want := fieldNames(dt.ImportFields()), []string{"code", "email", "name", "mobile", "uom_id"}; !slices.Equal(got, want) {
		t.Errorf("registered copy: ImportFields() = %q, want %q", got, want)
	}
	if got, want := fieldNames(dt.Fields), []string{"code", "name", "remarks", "uom_id"}; !slices.Equal(got, want) {
		t.Errorf("registered copy: Fields = %q, want %q", got, want)
	}

	cp = *first
	cp.Fields = []Field{{Name: "code", Importable: true}}
	if got, want := fieldNames(cp.ImportFields()), []string{"code", "email", "mobile"}; !slices.Equal(got, want) {
		t.Errorf("copy with fewer fields: ImportFields() = %q, want %q", got, want)
	}
}

// An ImportOnly field has no column, so a convention column's name is free.
func TestRegister_ImportOnlyConventionName(t *testing.T) {
	r := newRegistry()
	dt := importedDocType()
	emailField(&dt).Name = "number"
	r.Register(dt)
	got, _ := r.Lookup(dt.Name)
	i := slices.IndexFunc(got.ImportFields(), func(f Field) bool { return f.Name == "number" })
	if i < 0 {
		t.Fatalf("ImportFields() = %q, want number in it", fieldNames(got.ImportFields()))
	}
	if got.ImportFields()[i].System {
		t.Error("ImportOnly field named number became System")
	}
}

// csvio's checkRow reads row["code"] as the record's code and queries the
// table's code column, which an ImportOnly field doesn't have.
func TestRegister_ImportOnlyNamedCodeRefused(t *testing.T) {
	dt := importedDocType()
	dt.Fields[0].Name = "ref"
	emailField(&dt).Name = "code"
	got := mustPanic(t, "ImportOnly code", func() { newRegistry().Register(dt) })
	if !strings.Contains(got, "can't be named code") {
		t.Errorf("panic = %q, want it to say the field can't be named code", got)
	}
}

// Check resolves the Link of an ImportOnly field, though Register took the
// field out of Fields.
func TestCheckRegistry_ImportOnlyLink(t *testing.T) {
	dt := importedDocType()
	email := emailField(&dt)
	email.Type, email.Link = Link, "t.Missing"
	r := newRegistry()
	r.Register(dt)
	err := r.Check()
	if err == nil {
		t.Fatal("ImportOnly Link to an unknown DocType: Check() = nil, want an error")
	}
	for _, want := range []string{`"email"`, `"t.Missing"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Check() = %q, want it to contain %q", err, want)
		}
	}

	email.Link = "t.Imported"
	r = newRegistry()
	r.Register(dt)
	if err := r.Check(); err != nil {
		t.Errorf("ImportOnly Link to a known DocType: Check() = %v, want nil", err)
	}
}
