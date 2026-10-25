package doc

import (
	"context"
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// create is the create path (save with id 0); see the plan's Task 6.
func (e *engine) create(ctx context.Context, tx *db.Tx, p auth.Principal, dt *DocType, in ActionInput) (Result, error) {
	var supplied *string
	if v := in.Values["number"]; v != "" {
		supplied = &v
	}
	if err := e.Numbers.Guard(nil, supplied); err != nil {
		return Result{}, &Error{Kind: KindInvalid, Message: err.Error(), Fields: []FieldError{{Field: "number", Message: err.Error()}}, Err: err}
	}

	n, parseErrs := normalize(dt, in, e.ParseValue, true)
	now := e.Clock.Now().UTC()
	d := Doc{DocType: dt.Name, State: StateDraft, Version: 1, CreatedBy: p.ID, UpdatedBy: p.ID,
		CreatedAt: now, UpdatedAt: now, Header: n.Header}
	for i, l := range n.Lines {
		d.Lines = append(d.Lines, Line{LineNo: i + 1, Values: l.Values})
	}
	if err := e.Auth.Can(ctx, p, dt.Name, "create", recordRef(dt, d)); err != nil {
		return Result{}, err
	}
	if err := e.checkRestrictions(ctx, tx, p, dt, &n); err != nil {
		return Result{}, err
	}
	readOnlyErrs := checkReadOnly(dt, in, nil)
	fieldErrs, warnings := checkFields(ctx, tx, dt, &n, false, masters)
	if n.err != nil {
		return Result{}, n.err
	}
	errs := byPosition(dt, slices.Concat(parseErrs, readOnlyErrs, fieldErrs))
	if len(parseErrs) > 0 || len(readOnlyErrs) > 0 || missingLink(dt, &n) {
		return Result{}, invalid(errs) // nothing written: a foreign key would fail
	}

	id, err := insertHeader(ctx, tx, dt, n.Header, now, p.ID)
	if err != nil {
		return Result{}, err
	}
	d.ID = id
	for i := range d.Lines {
		if d.Lines[i].ID, err = insertLine(ctx, tx, dt, id, d.Lines[i].LineNo, d.Lines[i].Values); err != nil {
			return Result{}, err
		}
	}
	if fe := e.checkSources(ctx, tx, dt, d); fe != nil {
		errs = byPosition(dt, append(errs, *fe))
	}
	if errs = append(errs, dt.Hooks.Validate(ctx, tx, id)...); len(errs) > 0 {
		return Result{}, invalid(errs)
	}

	err = e.Audit.Append(ctx, tx, audit.Entry{
		DocType: dt.Name, DocID: id, Action: "create", To: string(StateDraft),
		Changes: createChanges(dt, d, n.links), By: p.ID, At: now,
	})
	if err != nil {
		return Result{}, err
	}
	return Result{ID: id, State: StateDraft, Version: 1, Warnings: warnings}, nil
}

// save saves an existing draft (Task 7).
func (e *engine) save(context.Context, *db.Tx, auth.Principal, *DocType, Doc, ActionInput) (Result, error) {
	return Result{}, errNotImplemented
}

// missingLink reports whether a master Link value of n was not found: after
// checkFields, every master record found is in n.links.
func missingLink(dt *DocType, n *normalized) bool {
	missing := func(vals Values, onLine bool) bool {
		for _, f := range dt.Fields {
			id, isID := vals[f.Name].(int64)
			if f.OnLine != onLine || f.Type != Link || !isID || masterOf(f.Link) == nil {
				continue
			}
			if _, found := n.links[linkKey{Target: f.Link, ID: id}]; !found {
				return true
			}
		}
		return false
	}
	if missing(n.Header, false) {
		return true
	}
	return slices.ContainsFunc(n.Lines, func(l nline) bool { return missing(l.Values, true) })
}

// byPosition sorts errs by line, then by field order on that line (stable):
// a line's "id" comes first, the header's "lines" errors after its fields,
// and names that aren't fields last.
func byPosition(dt *DocType, errs []FieldError) []FieldError {
	pos := func(fe FieldError) int {
		switch fe.Field {
		case "id":
			return -1
		case "lines":
			return len(dt.Fields)
		}
		if i := slices.IndexFunc(dt.Fields, func(f Field) bool { return f.Name == fe.Field }); i >= 0 {
			return i
		}
		return len(dt.Fields) + 1
	}
	slices.SortStableFunc(errs, func(a, b FieldError) int {
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return pos(a) - pos(b)
	})
	return errs
}
