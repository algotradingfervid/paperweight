package doc

import (
	"context"
	"slices"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Info implements doctype.InfoSource (CD-05) in the caller's tx. An unknown
// DocType, a non-document or a missing row is KindNotFound.
func (e *engine) Info(ctx context.Context, tx *db.Tx, doctype string, id int64) (doctype.Info, error) {
	d, err := LoadDoc(ctx, tx, doctype, id)
	if err != nil {
		return Info{}, err
	}
	dt, _ := Lookup(doctype)
	return infoOf(ctx, tx, dt, d)
}

func infoOf(ctx context.Context, tx *db.Tx, dt *DocType, d Doc) (doctype.Info, error) {
	party, err := partyOf(ctx, tx, dt, d)
	if err != nil {
		return Info{}, err
	}
	ref := recordRef(dt, d)
	return doctype.Info{
		DocType: dt.Name, DocLabel: dt.Label, ID: d.ID, State: d.State, Number: d.Number,
		Display: naming.Label(d.Number, d.ID), Party: party,
		CreatedBy: ref.CreatedBy, EmployeeID: ref.EmployeeID, ProjectIDs: ref.ProjectIDs, SiteIDs: ref.SiteIDs,
	}, nil
}

// partyOf is the "{Code} {Name}" display (CD-55, CD-68) of the master named by
// the EmployeeField when it is a Link to a master, otherwise by the first
// Required header Link to a master; "" when there is none or it is empty.
func partyOf(ctx context.Context, tx *db.Tx, dt *DocType, d Doc) (string, error) {
	var field *Field
	for _, f := range dt.HeaderFields() {
		if f.Type != Link || masterOf(f.Link) == nil {
			continue
		}
		if f.Column == dt.EmployeeField {
			field = &f
			break
		}
		if f.Required && field == nil {
			field = &f
		}
	}
	if field == nil {
		return "", nil
	}
	id, ok := d.Header[field.Name].(int64)
	if !ok {
		return "", nil
	}
	mi, found, err := masterOf(field.Link).Lookup(ctx, tx, id)
	if err != nil || !found {
		return "", err
	}
	return masterDisplay(mi), nil
}

// masterOf returns the MasterLookup of the DocType target, or nil when it is
// not a registered master (CD-21).
func masterOf(target string) MasterLookup {
	if dt, ok := Lookup(target); ok {
		return dt.Master
	}
	return nil
}

// masters is the masterResolver over the registry.
func masters(target string) (MasterLookup, bool) {
	m := masterOf(target)
	return m, m != nil
}

// masterDisplay is "{Code} {Name}", or the name alone without a code (CD-82).
func masterDisplay(mi MasterInfo) string {
	if mi.Code == "" {
		return mi.Name
	}
	return mi.Code + " " + mi.Name
}

// recordRef is d as AUTH sees it, from the DocType's rights fields (CD-09).
func recordRef(dt *DocType, d Doc) *auth.RecordRef {
	ref := &auth.RecordRef{DocType: dt.Name, ID: d.ID}
	if v, ok := headerColumn(dt, d, dt.OwnerField); ok {
		ref.CreatedBy = types.UserID(v)
	}
	if v, ok := headerColumn(dt, d, dt.EmployeeField); ok {
		ref.EmployeeID = v
	}
	ref.ProjectIDs = columnIDs(dt, d, dt.ProjectFields)
	ref.SiteIDs = columnIDs(dt, d, dt.SiteFields)
	return ref
}

// headerColumn returns the integer in header column c of d.
func headerColumn(dt *DocType, d Doc, c string) (int64, bool) {
	switch c {
	case "":
		return 0, false
	case "id":
		return d.ID, true
	case "created_by":
		return int64(d.CreatedBy), true
	case "updated_by":
		return int64(d.UpdatedBy), true
	}
	return valueAt(dt.HeaderFields(), d.Header, c)
}

// valueAt returns the integer value of the field with Column c in vals.
func valueAt(fields []Field, vals Values, c string) (int64, bool) {
	for _, f := range fields {
		if f.Column == c {
			return intOf(vals[f.Name])
		}
	}
	return 0, false
}

// columnIDs returns the distinct non-null ids in the columns cols of d (header
// columns, and "lines.col" for every line), in first-seen order.
func columnIDs(dt *DocType, d Doc, cols []string) []int64 {
	var ids []int64
	add := func(v int64, ok bool) {
		if ok && !slices.Contains(ids, v) {
			ids = append(ids, v)
		}
	}
	for _, c := range cols {
		col, onLine := strings.CutPrefix(c, "lines.")
		if !onLine {
			add(headerColumn(dt, d, col))
			continue
		}
		for _, l := range d.Lines {
			if col == "id" {
				add(l.ID, true)
				continue
			}
			add(valueAt(dt.LineFields(), l.Values, col))
		}
	}
	return ids
}
