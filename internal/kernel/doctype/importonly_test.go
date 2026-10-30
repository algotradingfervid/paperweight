package doctype

import (
	"slices"
	"strings"
	"testing"
)

// MST-IMP0 (CD-199 (5), CD-210): an ImportOnly field exists only in a CSV
// import file. It has no column, and Register keeps it out of dt.Fields.

// importedDocType is a valid master with one ImportOnly field, email, between
// two stored fields; each case changes one copy of it.
func importedDocType() DocType {
	return DocType{
		Name: "t.Imported", Label: "Imported", PluralLabel: "Imported", Table: "t_imported",
		Fields: []Field{
			{Name: "code", Label: "Code", Type: Text, Importable: true},
			{Name: "email", Label: "Email", Type: Text, Importable: true, ImportOnly: true},
			{Name: "name", Label: "Name", Type: Text, Importable: true},
			{Name: "remarks", Label: "Remarks", Type: Text},
			{Name: "mobile", Label: "Mobile", Type: Text, Importable: true, ImportOnly: true},
			{Name: "uom_id", Label: "UoM", Type: Int, OnLine: true, Importable: true},
		},
		LinesTable: "t_imported_line",
		Master:     nopMaster{},
	}
}

// emailField returns the ImportOnly field of a copy of importedDocType.
func emailField(dt *DocType) *Field { return &dt.Fields[1] }

func TestRegister_ImportOnlyRefused(t *testing.T) {
	cases := []struct {
		name string
		edit func(dt *DocType)
	}{
		{"not Importable", func(dt *DocType) { emailField(dt).Importable = false }},
		{"OnLine", func(dt *DocType) { emailField(dt).OnLine = true }},
		{"Filterable", func(dt *DocType) { emailField(dt).Filterable = true }},
		{"Sortable", func(dt *DocType) { emailField(dt).Sortable = true }},
		{"Searchable", func(dt *DocType) { emailField(dt).Searchable = true }},
		{"ListPriority", func(dt *DocType) { emailField(dt).ListPriority = 2 }},
		{"ReadOnly", func(dt *DocType) { emailField(dt).ReadOnly = true }},
		{"RollUp", func(dt *DocType) { emailField(dt).RollUp = true }},
		{"System", func(dt *DocType) { emailField(dt).System = true }},
		{"Column differs from Name", func(dt *DocType) { emailField(dt).Column = "email_address" }},
		{"named by SearchFields", func(dt *DocType) { dt.SearchFields = []string{"code", "email"} }},
		{"named by DefaultSort", func(dt *DocType) { dt.DefaultSort = []SortKey{{Field: "email"}} }},
	}
	for _, c := range cases {
		dt := importedDocType()
		c.edit(&dt)
		got := mustPanic(t, c.name, func() { newRegistry().Register(dt) })
		if got != "" && !strings.Contains(got, "email") {
			t.Errorf("%s: panic = %q, want it to name the field", c.name, got)
		}
	}
}

func TestRegister_ImportOnlyAllowed(t *testing.T) {
	stored := Field{Name: "contact_email", Label: "Contact email", Type: Text, Column: "email"}
	cases := []struct {
		name string
		edit func(dt *DocType)
	}{
		{"as declared", func(*DocType) {}},
		{"explicit Column equal to Name", func(dt *DocType) { emailField(dt).Column = "email" }},
		{"Required", func(dt *DocType) { emailField(dt).Required = true }},
		{"stored field on the same column, declared after", func(dt *DocType) { dt.Fields = append(dt.Fields, stored) }},
		{"stored field on the same column, declared before", func(dt *DocType) { dt.Fields = append([]Field{stored}, dt.Fields...) }},
	}
	for _, c := range cases {
		dt := importedDocType()
		c.edit(&dt)
		r := newRegistry()
		mustNotPanic(t, c.name, func() { r.Register(dt) })
		got, ok := r.Lookup(dt.Name)
		if !ok {
			continue
		}
		// Register fills Column as for any field.
		i := slices.IndexFunc(got.ImportFields(), func(f Field) bool { return f.Name == "email" })
		if i < 0 {
			t.Errorf("%s: ImportFields() = %q, want email in it", c.name, fieldNames(got.ImportFields()))
		} else if col := got.ImportFields()[i].Column; col != "email" {
			t.Errorf("%s: email has Column %q, want %q", c.name, col, "email")
		}
	}
}

func TestRegister_StripsImportOnlyFromFields(t *testing.T) {
	r := newRegistry()
	r.Register(importedDocType())
	dt, ok := r.Lookup("t.Imported")
	if !ok {
		t.Fatal("t.Imported not registered")
	}

	if got, want := fieldNames(dt.Fields), []string{"code", "name", "remarks", "uom_id"}; !slices.Equal(got, want) {
		t.Errorf("Fields = %q, want %q", got, want)
	}
	if got, want := fieldNames(dt.HeaderFields()), []string{"code", "name", "remarks"}; !slices.Equal(got, want) {
		t.Errorf("HeaderFields() = %q, want %q", got, want)
	}
	if got, want := fieldNames(dt.LineFields()), []string{"uom_id"}; !slices.Equal(got, want) {
		t.Errorf("LineFields() = %q, want %q", got, want)
	}
	for _, name := range []string{"email", "mobile"} {
		if f, ok := dt.Field(name); ok {
			t.Errorf("Field(%q) = %+v, want not found", name, f)
		}
	}

	// Declaration order, ImportOnly fields included, non-Importable ones not.
	imp := dt.ImportFields()
	if got, want := fieldNames(imp), []string{"code", "email", "name", "mobile", "uom_id"}; !slices.Equal(got, want) {
		t.Fatalf("ImportFields() = %q, want %q", got, want)
	}
	for _, f := range imp {
		if want := f.Name == "email" || f.Name == "mobile"; f.ImportOnly != want {
			t.Errorf("ImportFields() %s: ImportOnly = %v, want %v", f.Name, f.ImportOnly, want)
		}
	}

	// A DocType with no ImportOnly field keeps every field, and ImportFields
	// lists its Importable ones.
	r = newRegistry()
	plain := importedDocType()
	plain.Fields = slices.DeleteFunc(plain.Fields, func(f Field) bool { return f.ImportOnly })
	r.Register(plain)
	dt, _ = r.Lookup("t.Imported")
	if got, want := fieldNames(dt.Fields), []string{"code", "name", "remarks", "uom_id"}; !slices.Equal(got, want) {
		t.Errorf("no ImportOnly field: Fields = %q, want %q", got, want)
	}
	if got, want := fieldNames(dt.ImportFields()), []string{"code", "name", "uom_id"}; !slices.Equal(got, want) {
		t.Errorf("no ImportOnly field: ImportFields() = %q, want %q", got, want)
	}

	// A literal never registered: the Importable fields of dt.Fields.
	literal := DocType{Fields: []Field{
		{Name: "a", Importable: true},
		{Name: "b"},
		{Name: "c", Importable: true},
	}}
	if got, want := fieldNames(literal.ImportFields()), []string{"a", "c"}; !slices.Equal(got, want) {
		t.Errorf("literal: ImportFields() = %q, want %q", got, want)
	}
}

func TestCheckRegistry_ImportableNeedsField(t *testing.T) {
	// Importable, and no field can be imported: refused.
	r := newRegistry()
	r.Register(DocType{
		Name: "t.NoColumns", Label: "No columns", Table: "t_no_columns", Importable: true,
		Fields: []Field{{Name: "code", Label: "Code", Type: Text}},
		Master: nopMaster{},
	})
	err := r.Check()
	if err == nil {
		t.Fatal("Importable DocType with no Importable field: Check() = nil, want an error")
	}
	for _, want := range []string{"t.NoColumns", "no Importable field"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Check() = %q, want it to contain %q", err, want)
		}
	}

	// An ImportOnly field counts, though Register takes it out of dt.Fields.
	r = newRegistry()
	r.Register(DocType{
		Name: "t.OnlyImportOnly", Label: "Only ImportOnly", Table: "t_only_import_only", Importable: true,
		Fields: []Field{
			{Name: "code", Label: "Code", Type: Text},
			{Name: "email", Label: "Email", Type: Text, Importable: true, ImportOnly: true},
		},
		Master: nopMaster{},
	})
	if err := r.Check(); err != nil {
		t.Errorf("only an ImportOnly field Importable: Check() = %v, want nil", err)
	}

	// The rule binds Importable DocTypes only.
	r = newRegistry()
	r.Register(DocType{
		Name: "t.NotImportable", Label: "Not importable", Table: "t_not_importable",
		Fields: []Field{{Name: "code", Label: "Code", Type: Text}},
		Master: nopMaster{},
	})
	if err := r.Check(); err != nil {
		t.Errorf("DocType not Importable: Check() = %v, want nil", err)
	}
}

func TestCheckRegistry_RightsFieldImportOnly(t *testing.T) {
	cases := []struct {
		name string
		edit func(dt *DocType)
	}{
		{"ProjectFields", func(dt *DocType) { dt.ProjectFields = []string{"email"} }},
		{"SiteFields", func(dt *DocType) { dt.SiteFields = []string{"email"} }},
		{"OwnerField", func(dt *DocType) { dt.OwnerField = "email" }},
		{"EmployeeField", func(dt *DocType) { dt.EmployeeField = "email" }},
	}
	for _, c := range cases {
		dt := importedDocType()
		c.edit(&dt)
		r := newRegistry()
		r.Register(dt)
		err := r.Check()
		if err == nil {
			t.Errorf("%s: Check() = nil, want an error", c.name)
			continue
		}
		for _, want := range []string{`"email"`, "names no column"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: Check() = %q, want it to contain %q", c.name, err, want)
			}
		}
	}
}
