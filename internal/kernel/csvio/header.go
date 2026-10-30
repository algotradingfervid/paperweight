package csvio

import (
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// ColumnMap ties a file column to the field it fills.
type ColumnMap struct {
	Index int
	Field *doctype.Field
}

// normaliseHeader strips a BOM, lower-cases, and turns "_" and runs of
// spaces into one space.
func normaliseHeader(s string) string {
	s = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "\uFEFF"))
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "_", " ")), " ")
}

// MapHeader maps header cells to dt's fields by label, name or
// "<DocType label> <field label>". It returns the importable columns, the
// warnings for ignored columns, and a *FileError for a missing required or
// repeated column.
func MapHeader(dt *doctype.DocType, header []string) ([]ColumnMap, []string, error) {
	var cols []ColumnMap
	var warns []string
	seen := map[*doctype.Field]bool{}
	imports := dt.ImportFields() // one call: seen compares pointers into it
	for i, cell := range header {
		n := normaliseHeader(cell)
		if n == "" {
			continue
		}
		f := matchField(dt, imports, n)
		if f == nil {
			warns = append(warns, "Column '"+strings.TrimSpace(strings.TrimPrefix(cell, "\uFEFF"))+"' isn't a field of "+dt.Label+" and will be ignored.")
			continue
		}
		if seen[f] {
			return nil, nil, &FileError{Msg: "Column '" + f.Label + "' appears twice. Keep one."}
		}
		seen[f] = true
		if !f.Importable {
			warns = append(warns, "Column '"+f.Label+"' can't be imported and will be ignored.")
			continue
		}
		cols = append(cols, ColumnMap{Index: i, Field: f})
	}
	for i := range imports {
		f := &imports[i]
		if f.Required && !seen[f] {
			return nil, nil, &FileError{Msg: "Column '" + f.Label + "' is missing. Add it to the first row."}
		}
	}
	return cols, warns, nil
}

// matchField finds n among the import fields, then among dt's other fields
// (those get the "can't be imported" warning).
func matchField(dt *doctype.DocType, imports []doctype.Field, n string) *doctype.Field {
	if f := findField(dt, imports, n); f != nil {
		return f
	}
	return findField(dt, dt.Fields, n)
}

func findField(dt *doctype.DocType, fields []doctype.Field, n string) *doctype.Field {
	for i := range fields {
		f := &fields[i]
		if n == normaliseHeader(f.Label) || n == normaliseHeader(f.Name) || n == normaliseHeader(dt.Label+" "+f.Label) {
			return f
		}
	}
	return nil
}
