package csvio

import (
	"bytes"
	"encoding/csv"
	"strings"
)

// ErrorRowsCSV is the "Download rows with errors" file: a BOM, s's header
// plus an Error column, then only the rows of s that have errors in r, each
// with its messages joined. The messages leave out "Row N:", which numbers the
// original upload, and keep the column name when there is one. Cells, the
// header too, are formula-escaped (D-NUM-26).
func ErrorRowsCSV(s *Sheet, r Report) []byte {
	msgs := map[int][]string{}
	for _, e := range r.Errors {
		msgs[e.Row] = append(msgs[e.Row], errorText(e))
	}
	var b bytes.Buffer
	b.Write(utf8BOM)
	w := csv.NewWriter(&b)
	w.UseCRLF = true
	// Writes to a bytes.Buffer never fail.
	head := make([]string, 0, len(s.Header)+1)
	for _, c := range s.Header {
		head = append(head, EscapeFormula(c))
	}
	_ = w.Write(append(head, "Error"))
	for _, sr := range s.Rows {
		m, ok := msgs[sr.Num]
		if !ok {
			continue
		}
		rec := make([]string, 0, len(sr.Cells)+1)
		for _, c := range sr.Cells {
			rec = append(rec, EscapeFormula(c))
		}
		_ = w.Write(append(rec, strings.Join(m, "; ")))
	}
	w.Flush()
	return b.Bytes()
}

// errorText is the preview's text for e without its "Row N" prefix.
func errorText(e RowError) string {
	if e.Column == "" {
		return e.Message
	}
	return "column '" + e.Column + "': " + e.Message
}
