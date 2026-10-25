package csvio

import (
	"fmt"

	"github.com/vardhan/internaltools/internal/types"
)

// RowError is one problem on one spreadsheet row.
type RowError struct {
	Row     int
	Column  string
	Message string
}

func (e RowError) String() string {
	if e.Column == "" {
		return fmt.Sprintf("Row %d: %s", e.Row, e.Message)
	}
	return fmt.Sprintf("Row %d, column '%s': %s", e.Row, e.Column, e.Message)
}

// Report is the outcome of Preview or Import.
type Report struct {
	Rows, Ready, Imported int
	Errors                []RowError
	Warnings              []string
	PluralLabel           string
	preview               bool
}

// errorRows counts the rows with at least one error.
func (r Report) errorRows() int {
	n, last := 0, 0
	for _, e := range r.Errors {
		if e.Row != last {
			n++
			last = e.Row
		}
	}
	return n
}

// Summary is the one-line result shown above the report.
func (r Report) Summary() string {
	bad := r.errorRows()
	switch {
	case r.preview:
		return plural(r.Ready, "row is", "rows are") + " ready, " + plural(bad, "row has", "rows have") + " errors"
	case len(r.Errors) > 0:
		return "Nothing was imported. Fix " + plural(bad, "row", "rows") + " and upload again."
	}
	return "Imported " + types.FormatCount(int64(r.Imported)) + " " + r.PluralLabel + "."
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return types.FormatCount(int64(n)) + " " + many
}
