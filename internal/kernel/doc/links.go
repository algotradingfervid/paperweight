package doc

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Links between documents (Task 11).

// Blocker is a live document linked to another one, so that one can't be
// cancelled.
type Blocker struct {
	DocType string
	ID      int64
	Number  string
	State   State
}

// blockingStates are the states in which a linked document blocks a cancel.
const blockingStates = "('pending_approval', 'approved', 'closed')"

// blockerWords are the state words in the blocked-cancel message.
var blockerWords = map[State]string{
	StateApproved: "approved",
	StatePending:  "waiting for approval",
	StateClosed:   "closed",
}

// maxListedBlockers is how many blockers the message names.
const maxListedBlockers = 10

// LinkedBlockers returns the documents in pending_approval, approved or closed
// whose LinkSpecs point at document id of doctype, one per document, ordered
// by number (unnumbered last), then id.
func LinkedBlockers(ctx context.Context, tx *db.Tx, doctype string, id int64) ([]Blocker, error) {
	t, ok := Lookup(doctype)
	if !ok {
		return nil, nil
	}
	type key struct {
		DocType string
		ID      int64
	}
	seen := map[key]bool{}
	var out []Blocker
	for _, s := range All() {
		for _, ls := range s.Links {
			if ls.Target != doctype {
				continue
			}
			f, ok := s.Field(ls.Field)
			if !ok {
				continue
			}
			var q string
			if ls.OnLine {
				q = "SELECT DISTINCT sh.id, sh.number, sh.state FROM " + quote(s.LinesTable) + " sl JOIN " + quote(s.Table) +
					" sh ON sh.id = sl.parent_id JOIN " + quote(t.LinesTable) + " tl ON tl.id = sl." + quote(f.Column) +
					" WHERE tl.parent_id = ? AND sh.state IN " + blockingStates
			} else {
				q = "SELECT sh.id, sh.number, sh.state FROM " + quote(s.Table) + " sh WHERE sh." + quote(f.Column) +
					" = ? AND sh.state IN " + blockingStates
			}
			rows, err := scanHeads(ctx, tx, q, id)
			if err != nil {
				return nil, fmt.Errorf("doc: blockers of %s %d in %s: %w", doctype, id, s.Name, err)
			}
			for _, r := range rows {
				if k := (key{s.Name, r.ID}); !seen[k] {
					seen[k] = true
					out = append(out, Blocker{DocType: s.Name, ID: r.ID, Number: r.Number, State: r.State})
				}
			}
		}
	}
	slices.SortStableFunc(out, func(a, b Blocker) int {
		return cmp.Or(
			cmp.Compare(boolInt(a.Number == ""), boolInt(b.Number == "")),
			cmp.Compare(a.Number, b.Number),
			cmp.Compare(a.ID, b.ID),
		)
	})
	return out, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// head is a document's id, number and state.
type head struct {
	ID     int64
	Number string
	State  State
}

// scanHeads runs q, which selects id, number and state, with args.
func scanHeads(ctx context.Context, tx *db.Tx, q string, args ...any) ([]head, error) {
	rows, err := tx.SQL().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []head
	for rows.Next() {
		var (
			h      head
			number sql.NullString
		)
		if err := rows.Scan(&h.ID, &number, &h.State); err != nil {
			return nil, err
		}
		h.Number = number.String
		out = append(out, h)
	}
	return out, rows.Err()
}

// checkSources checks the source documents of d's LinkSpecs; a nil
// *FieldError is OK. It stops at the first failure; a read error is returned.
func (e *engine) checkSources(ctx context.Context, tx *db.Tx, dt *DocType, d Doc) (*FieldError, error) {
	errs, err := sourceErrors(ctx, tx, dt, d, true)
	if err != nil || len(errs) == 0 {
		return nil, err
	}
	return &errs[0], nil
}

// sourceErrors checks the source documents of d's LinkSpecs and returns every
// failure, or only the first when first is set. A scoped save and Problems
// need them all: a stored error mustn't hide one in scope (CD-205).
func sourceErrors(ctx context.Context, tx *db.Tx, dt *DocType, d Doc, first bool) ([]FieldError, error) {
	var errs []FieldError
	check := func(t *DocType, field string, line int, v any, label string) (stop bool, err error) {
		fe, err := checkSource(ctx, tx, t, field, line, v, label)
		if fe != nil {
			errs = append(errs, *fe)
		}
		return err != nil || (first && fe != nil), err
	}
	for _, ls := range dt.Links {
		t, ok := Lookup(ls.Target)
		if !ok {
			continue
		}
		if !ls.OnLine {
			f, _ := dt.Field(ls.Field)
			if stop, err := check(t, ls.Field, 0, d.Header[ls.Field], f.Label); stop {
				return errs, err
			}
			continue
		}
		for _, l := range d.Lines {
			if stop, err := check(t, ls.Field, l.LineNo, l.Values[ls.Field], t.Label); stop {
				return errs, err
			}
		}
	}
	return errs, nil
}

// checkSource checks the source of field at line (0 = header): v is a header
// id of t, or with line > 0 a line id of t. A missing source is reported with
// missingLabel.
func checkSource(ctx context.Context, tx *db.Tx, t *DocType, field string, line int, v any, missingLabel string) (*FieldError, error) {
	id, ok := intOf(v)
	if !ok {
		return nil, nil // an empty value is the required check's job
	}
	q := "SELECT id, number, state FROM " + quote(t.Table) + " WHERE id = ?"
	missing := msgLinkNotFound
	if line > 0 {
		q = "SELECT sh.id, sh.number, sh.state FROM " + quote(t.LinesTable) + " tl JOIN " + quote(t.Table) +
			" sh ON sh.id = tl.parent_id WHERE tl.id = ?"
		missing = msgSourceLineNotFound
	}
	rows, err := scanHeads(ctx, tx, q, id)
	if err != nil {
		return nil, fmt.Errorf("doc: read source %s %d: %w", t.Name, id, err)
	}
	fe := func(msg string) *FieldError {
		return &FieldError{Field: field, Line: line, Message: linePrefix(line) + msg}
	}
	if len(rows) == 0 {
		return fe(fmt.Sprintf(missing, missingLabel)), nil
	}
	s, label := naming.Label(rows[0].Number, rows[0].ID), strings.ToLower(t.Label)
	switch rows[0].State {
	case StateApproved:
		return nil, nil
	case StatePending:
		return fe(fmt.Sprintf(msgSourcePending, s, label)), nil
	case StateCancelled:
		return fe(fmt.Sprintf(msgSourceCancelled, s, label)), nil
	case StateClosed:
		return fe(fmt.Sprintf(msgSourceClosed, s, label)), nil
	}
	return fe(fmt.Sprintf(msgSourceNotApproved, s, label)), nil // draft, rejected
}

// checkBlockers refuses cancelling d (display x) while linked documents are
// live (D-DOC-05). A read error is returned unchanged.
func (e *engine) checkBlockers(ctx context.Context, tx *db.Tx, dt *DocType, d Doc, x string) error {
	bs, err := LinkedBlockers(ctx, tx, dt.Name, d.ID)
	if err != nil {
		return err
	}
	if len(bs) == 0 {
		return nil
	}
	if len(bs) == 1 {
		b := naming.Label(bs[0].Number, bs[0].ID)
		return &Error{Kind: KindBlocked, Message: fmt.Sprintf(msgBlockedOne, x, b, blockerWords[bs[0].State], b)}
	}
	listed := make([]string, 0, maxListedBlockers)
	for _, b := range bs[:min(len(bs), maxListedBlockers)] {
		listed = append(listed, fmt.Sprintf(msgBlockerEntry, naming.Label(b.Number, b.ID), blockerWords[b.State]))
	}
	end := "."
	if len(bs) > maxListedBlockers {
		end = fmt.Sprintf(msgBlockedMore, len(bs)-maxListedBlockers)
	}
	return &Error{Kind: KindBlocked, Message: fmt.Sprintf(msgBlockedMany, x, len(bs), strings.Join(listed, ", ")) + end}
}

// References lists the documents that link to the master row id of table
// (CD-22): one per (document, field), ordered by DocType, ID, then Field, at
// most limit. Drafts and cancelled documents count.
func (e *engine) References(ctx context.Context, tx *db.Tx, table string, id int64, limit int) ([]Reference, error) {
	if limit < 1 {
		return nil, errors.New("doc: References needs a limit of at least 1")
	}
	masterNames := map[string]bool{}
	for _, m := range All() {
		if m.IsMaster() && m.Table == table {
			masterNames[m.Name] = true
		}
	}
	if len(masterNames) == 0 {
		return nil, nil
	}
	var out []Reference
	for _, s := range All() {
		if !s.IsDocument() {
			continue
		}
		for _, f := range s.Fields {
			if f.Type != Link || !masterNames[f.Link] {
				continue
			}
			name := f.Name
			q := "SELECT id, number, state FROM " + quote(s.Table) + " WHERE " + quote(f.Column) + " = ? ORDER BY id LIMIT ?"
			if f.OnLine {
				name = "lines." + f.Name
				q = "SELECT DISTINCT sh.id, sh.number, sh.state FROM " + quote(s.LinesTable) + " sl JOIN " + quote(s.Table) +
					" sh ON sh.id = sl.parent_id WHERE sl." + quote(f.Column) + " = ? ORDER BY sh.id LIMIT ?"
			}
			rows, err := scanHeads(ctx, tx, q, id, limit)
			if err != nil {
				return nil, fmt.Errorf("doc: references to %s %d in %s: %w", table, id, s.Name, err)
			}
			for _, r := range rows {
				out = append(out, Reference{DocType: s.Name, Field: name, ID: r.ID, Display: naming.Label(r.Number, r.ID), State: r.State})
			}
		}
	}
	slices.SortFunc(out, func(a, b Reference) int {
		return cmp.Or(cmp.Compare(a.DocType, b.DocType), cmp.Compare(a.ID, b.ID), cmp.Compare(a.Field, b.Field))
	})
	return out[:min(len(out), limit)], nil
}

// IsReferenced reports the first document that links to the master row id of
// table.
func (e *engine) IsReferenced(ctx context.Context, tx *db.Tx, table string, id int64) (string, bool, error) {
	refs, err := e.References(ctx, tx, table, id, 1)
	if err != nil || len(refs) == 0 {
		return "", false, err
	}
	return refs[0].Display, true, nil
}
