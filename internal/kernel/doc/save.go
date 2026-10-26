package doc

import (
	"context"
	"fmt"
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// create is the create path (save with id 0); see the plan's Task 6.
func (e *engine) create(ctx context.Context, tx *db.Tx, p auth.Principal, dt *DocType, in ActionInput) (Result, error) {
	var supplied *string
	if v := in.Values["number"]; v != "" {
		supplied = &v
	}
	if err := e.Numbers.Guard(nil, supplied); err != nil {
		return Result{}, numberError(err)
	}

	n, parseErrs := normalize(dt, in, e.ParseValue, true)
	now := e.Clock.Now().UTC()
	d := Doc{DocType: dt.Name, State: StateDraft, Version: 1, CreatedBy: p.ID, UpdatedBy: p.ID,
		CreatedAt: now, UpdatedAt: now, Header: n.Header}
	for i, l := range n.Lines {
		d.Lines = append(d.Lines, Line{LineNo: i + 1, Values: l.Values})
	}
	// A restricted user's draft with its dimension empty would be invisible
	// to them (a 404); it gets the Required errors instead, after a check of
	// the right alone, and is never written (CD-178).
	requiredErrs, err := e.restrictedRequired(ctx, tx, p, dt, &n)
	if err != nil {
		return Result{}, err
	}
	ref := recordRef(dt, d)
	if len(requiredErrs) > 0 {
		ref = nil
	}
	if err := e.Auth.Can(ctx, p, dt.Name, "create", ref); err != nil {
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
	errs := byPosition(dt, slices.Concat(parseErrs, readOnlyErrs, fieldErrs, requiredErrs))
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
	fe, err := e.checkSources(ctx, tx, dt, d)
	if err != nil {
		return Result{}, err
	}
	if fe != nil {
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

// save saves the existing draft d (the plan's Task 7 save algorithm). The
// pipeline has checked the write right, the state and in.Version.
func (e *engine) save(ctx context.Context, tx *db.Tx, p auth.Principal, dt *DocType, d Doc, in ActionInput) (Result, error) {
	var stored, supplied *string
	if d.Number != "" {
		stored = &d.Number
	}
	if v := in.Values["number"]; v != "" {
		supplied = &v
	}
	if err := e.Numbers.Guard(stored, supplied); err != nil {
		return Result{}, numberError(err)
	}

	n, parseErrs := normalize(dt, in, e.ParseValue, false)
	keepStored(dt, d, &n)
	if err := e.checkRestrictions(ctx, tx, p, dt, &n); err != nil {
		return Result{}, err
	}
	readOnlyErrs := checkReadOnly(dt, in, &d)
	fieldErrs, warnings := checkFields(ctx, tx, dt, &n, false, masters)
	if n.err != nil {
		return Result{}, n.err
	}
	requiredErrs, err := e.restrictedRequired(ctx, tx, p, dt, &n)
	if err != nil {
		return Result{}, err
	}
	if pos := foreignLine(d, &n); pos > 0 {
		return Result{}, Refused(fmt.Sprintf(msgForeignLine, pos))
	}
	errs := byPosition(dt, slices.Concat(parseErrs, readOnlyErrs, fieldErrs, requiredErrs))
	if len(parseErrs) > 0 || len(readOnlyErrs) > 0 || missingLink(dt, &n) {
		return Result{}, invalid(errs) // nothing written: a foreign key would fail
	}
	if err := storedLinks(ctx, tx, dt, d, &n); err != nil {
		return Result{}, err
	}
	res := Result{ID: d.ID, State: d.State, Number: d.Number, Version: d.Version, Warnings: warnings}
	if len(saveChanges(dt, d, n, nil)) == 0 && !reordered(d, &n) {
		return res, nil // DOC-029: nothing changed, nothing written
	}

	now := e.Clock.Now().UTC()
	fields := storedFields(dt, false)
	args := make([]any, len(fields))
	for i, f := range fields {
		args[i] = toSQL(f, n.Header[f.Name])
	}
	ok, err := updateHeaderGuarded(ctx, tx, dt, d.ID, d.Version, columns(fields), args, now, p.ID)
	if err != nil {
		return Result{}, err
	}
	if !ok {
		return Result{}, stale(naming.Label(d.Number, d.ID))
	}
	after := Doc{DocType: dt.Name, ID: d.ID, Number: d.Number, State: d.State, Version: d.Version + 1,
		CreatedBy: d.CreatedBy, CreatedAt: d.CreatedAt, UpdatedBy: p.ID, UpdatedAt: now, Header: n.Header}
	newIDs, err := syncLines(ctx, tx, dt, d, &n)
	if err != nil {
		return Result{}, err
	}
	for i, l := range n.Lines {
		id := l.ID
		if id == 0 {
			id = newIDs[i]
		}
		after.Lines = append(after.Lines, Line{ID: id, LineNo: i + 1, Values: l.Values})
	}
	fe, err := e.checkSources(ctx, tx, dt, after)
	if err != nil {
		return Result{}, err
	}
	if fe != nil {
		errs = byPosition(dt, append(errs, *fe))
	}
	if errs = append(errs, dt.Hooks.Validate(ctx, tx, d.ID)...); len(errs) > 0 {
		return Result{}, invalid(errs)
	}

	err = e.Audit.Append(ctx, tx, audit.Entry{
		DocType: dt.Name, DocID: d.ID, Action: string(ActionSave),
		Changes: saveChanges(dt, d, n, newIDs), By: p.ID, At: now,
	})
	if err != nil {
		return Result{}, err
	}
	res.Version = after.Version
	return res, nil
}

// numberError is the number guard's refusal on field "number" (CD-28).
func numberError(err error) *Error {
	return &Error{Kind: KindInvalid, Message: err.Error(), Fields: []FieldError{{Field: "number", Message: err.Error()}}, Err: err}
}

// keepStored fills n with d's stored values where the save leaves them as
// they are: header keys the input omits, keys a kept line omits, and every
// ReadOnly field (CD-79; checkReadOnly refuses a different client value).
// Fields normalize couldn't read are left out; their error is returned.
// A kept line that carries only its id has no user keys, so normalize treats
// it as blank and save removes it.
func keepStored(dt *DocType, d Doc, n *normalized) {
	fill := func(vals, stored Values, unread map[string]bool, onLine bool) {
		for _, f := range storedFields(dt, onLine) {
			if _, given := vals[f.Name]; (!given || f.ReadOnly) && !unread[f.Name] {
				vals[f.Name] = stored[f.Name]
			}
		}
	}
	fill(n.Header, d.Header, n.unread, false)
	for _, l := range n.Lines {
		if i := slices.IndexFunc(d.Lines, func(s Line) bool { return s.ID == l.ID }); l.ID != 0 && i >= 0 {
			fill(l.Values, d.Lines[i].Values, l.unread, true)
		}
	}
}

// foreignLine returns the 1-based input position of the first line whose id
// is not a line of d, or is sent twice; 0 when every id is d's.
func foreignLine(d Doc, n *normalized) int {
	seen := map[int64]bool{}
	for _, l := range n.Lines {
		if l.ID == 0 {
			continue
		}
		if seen[l.ID] || !slices.ContainsFunc(d.Lines, func(s Line) bool { return s.ID == l.ID }) {
			return l.Pos + 1
		}
		seen[l.ID] = true
	}
	return 0
}

// storedLinks adds the master records of d's stored Link values to n.links,
// so the audit shows the old values as {id, code} too (DOC-040).
func storedLinks(ctx context.Context, tx *db.Tx, dt *DocType, d Doc, n *normalized) error {
	add := func(vals Values, onLine bool) error {
		for _, f := range storedFields(dt, onLine) {
			id, isID := vals[f.Name].(int64)
			lookup := masterOf(f.Link)
			if f.Type != Link || !isID || lookup == nil {
				continue
			}
			key := linkKey{Target: f.Link, ID: id}
			if _, ok := n.links[key]; ok {
				continue
			}
			mi, found, err := lookup.Lookup(ctx, tx, id)
			if err != nil {
				return err
			}
			if found {
				n.links[key] = mi
			}
		}
		return nil
	}
	if err := add(d.Header, false); err != nil {
		return err
	}
	for _, l := range d.Lines {
		if err := add(l.Values, true); err != nil {
			return err
		}
	}
	return nil
}

// reordered reports whether a kept line of n moves to another line number.
func reordered(d Doc, n *normalized) bool {
	for i, l := range n.Lines {
		if l.ID != 0 && slices.ContainsFunc(d.Lines, func(s Line) bool { return s.ID == l.ID && s.LineNo != i+1 }) {
			return true
		}
	}
	return false
}

// syncLines makes d's stored lines match n: it deletes dropped lines, updates
// kept ones and inserts new ones, numbering them in input order. It returns
// the new lines' ids by index in n.Lines.
func syncLines(ctx context.Context, tx *db.Tx, dt *DocType, d Doc, n *normalized) (map[int]int64, error) {
	for _, s := range d.Lines {
		if slices.ContainsFunc(n.Lines, func(l nline) bool { return l.ID == s.ID }) {
			continue
		}
		q := "DELETE FROM " + quote(dt.LinesTable) + " WHERE id = ? AND parent_id = ?"
		if _, err := tx.SQL().ExecContext(ctx, q, s.ID, d.ID); err != nil {
			return nil, fmt.Errorf("doc: delete %s %d: %w", dt.LinesTable, s.ID, err)
		}
	}
	fields := storedFields(dt, true)
	set := ""
	for _, c := range columns(fields) {
		set += ", " + c + " = ?"
	}
	update := "UPDATE " + quote(dt.LinesTable) + " SET line_no = ?" + set + " WHERE id = ? AND parent_id = ?"
	newIDs := map[int]int64{}
	for i, l := range n.Lines {
		if l.ID == 0 {
			id, err := insertLine(ctx, tx, dt, d.ID, i+1, l.Values)
			if err != nil {
				return nil, err
			}
			newIDs[i] = id
			continue
		}
		args := []any{i + 1}
		for _, f := range fields {
			args = append(args, toSQL(f, l.Values[f.Name]))
		}
		if _, err := tx.SQL().ExecContext(ctx, update, append(args, l.ID, d.ID)...); err != nil {
			return nil, fmt.Errorf("doc: update %s %d: %w", dt.LinesTable, l.ID, err)
		}
	}
	return newIDs, nil
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
