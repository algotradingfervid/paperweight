package doc

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
)

// createChanges is the change set of a new document: a set change from null
// for every non-empty header value, and an add change per line (CD-20).
func createChanges(dt *DocType, d Doc, links map[linkKey]doctype.MasterInfo) []audit.Change {
	var changes []audit.Change
	for _, f := range storedFields(dt, false) {
		if v := d.Header[f.Name]; v != nil {
			changes = append(changes, audit.Change{Field: f.Name, Before: "null", After: rawJSON(f, v, links), Op: audit.OpSet})
		}
	}
	for _, l := range d.Lines {
		changes = append(changes, audit.Change{LineNo: l.LineNo, LineID: l.ID, After: lineJSON(dt, l.Values, links), Op: audit.OpAdd})
	}
	return changes
}

// lineJSON is a JSON object of the non-empty user values of a line, in field order.
func lineJSON(dt *DocType, vals Values, links map[linkKey]doctype.MasterInfo) string {
	var parts []string
	for _, f := range storedFields(dt, true) {
		if v := vals[f.Name]; v != nil {
			parts = append(parts, jsonString(f.Name)+":"+rawJSON(f, v, links))
		}
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// rawJSON is the audit form of a typed value: numbers in their stored integer
// scale, dates and text as strings, and a master link as {id, code} (DOC-087).
func rawJSON(f Field, v any, links map[linkKey]doctype.MasterInfo) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return jsonString(x)
	case bool:
		return strconv.FormatBool(x)
	case types.Date:
		return jsonString(x.ISO())
	case time.Time:
		return jsonString(x.UTC().Format(timestampLayout))
	case int64:
		if mi, ok := links[linkKey{Target: f.Link, ID: x}]; ok && f.Type == Link {
			b, _ := json.Marshal(struct {
				ID   int64  `json:"id"`
				Code string `json:"code"`
			}{x, mi.Code})
			return string(b)
		}
	}
	if n, ok := intOf(v); ok {
		return strconv.FormatInt(n, 10)
	}
	return jsonString(FormatPlain(f, v))
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// saveChanges is the change set of saving after over before (CD-20): a set
// change per changed header field, a set change per changed field of a kept
// line (at its new line number), an add change per new line (its id from
// newIDs by index in after.Lines) and a remove change per dropped line (at
// its old line number). A change of line_no alone is not recorded. Links of
// both versions are expected in after.links.
func saveChanges(dt *DocType, before Doc, after normalized, newIDs map[int]int64) []audit.Change {
	var changes []audit.Change
	diff := func(fields []Field, b, a Values, lineNo int, lineID int64) {
		for _, f := range fields {
			bv, av := toSQL(f, b[f.Name]), toSQL(f, a[f.Name])
			if rawJSON(f, bv, nil) != rawJSON(f, av, nil) {
				changes = append(changes, audit.Change{Field: f.Name, LineNo: lineNo, LineID: lineID,
					Before: rawJSON(f, bv, after.links), After: rawJSON(f, av, after.links), Op: audit.OpSet})
			}
		}
	}
	diff(storedFields(dt, false), before.Header, after.Header, 0, 0)
	stored := map[int64]Line{}
	for _, l := range before.Lines {
		stored[l.ID] = l
	}
	kept := map[int64]bool{}
	for i, l := range after.Lines {
		if l.ID == 0 {
			changes = append(changes, audit.Change{LineNo: i + 1, LineID: newIDs[i], After: lineJSON(dt, l.Values, after.links), Op: audit.OpAdd})
			continue
		}
		kept[l.ID] = true
		diff(storedFields(dt, true), stored[l.ID].Values, l.Values, i+1, l.ID)
	}
	for _, l := range before.Lines {
		if !kept[l.ID] {
			changes = append(changes, audit.Change{LineNo: l.LineNo, LineID: l.ID, Before: lineJSON(dt, l.Values, after.links), Op: audit.OpRemove})
		}
	}
	return changes
}
