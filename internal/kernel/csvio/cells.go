package csvio

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// checkCell checks a non-empty cell of a field that isn't a Link and returns
// the user message, or "" when the cell is fine.
func checkCell(f *doctype.Field, cell string) string {
	var err error
	switch f.Type {
	case doctype.Money:
		_, err = types.ParseMoney(cell)
	case doctype.Qty:
		_, err = types.ParseQty(cell)
	case doctype.Rate:
		_, err = types.ParseRate(cell)
	case doctype.Percent:
		_, err = types.ParsePercent(cell)
	case doctype.Factor:
		_, err = types.ParseFactor(cell)
	case doctype.Date:
		_, err = types.ParseDate(cell)
	case doctype.Check:
		switch strings.ToLower(cell) {
		case "yes", "no", "true", "false", "1", "0":
		default:
			return `"` + cell + `" is not Yes or No.`
		}
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

// linkKey is one row of a link's target.
type linkKey struct {
	target string
	id     int64
}

// candidate is an active row a suggestion may name.
type candidate struct {
	id    int64
	code  string
	lower []rune
}

// linkMemo holds what one import run has learned about link targets, so a bad
// cell repeated down a file costs one link check per row and one scan per
// target, all under the write lock. A row's right to be linked can't change
// inside the run; a row the run creates has a new id, and added puts it in the
// candidates.
type linkMemo struct {
	auth       auth.Service
	principal  auth.Principal
	allowed    map[linkKey]bool
	candidates map[string][]candidate // by target, sorted by code
}

func newLinkMemo(a auth.Service, p auth.Principal) *linkMemo {
	return &linkMemo{auth: a, principal: p, allowed: map[linkKey]bool{}, candidates: map[string][]candidate{}}
}

// can is linkCheck's answer as a bool, asked once per row; a refusal is false.
func (m *linkMemo) can(ctx context.Context, tx *db.Tx, target *doctype.DocType, id int64) (bool, error) {
	if linkCheck == nil {
		return true, nil
	}
	k := linkKey{target.Name, id}
	if ok, seen := m.allowed[k]; seen {
		return ok, nil
	}
	err := linkCheck(ctx, tx, m.auth, m.principal, target, id)
	if errors.Is(err, auth.ErrNotFound) || errors.Is(err, auth.ErrForbidden) {
		m.allowed[k] = false
		return false, nil
	}
	if err != nil {
		return false, err
	}
	m.allowed[k] = true
	return true, nil
}

// added puts row id of dt, just created by this run, into dt's candidates when
// they have been read. A row the same RowFunc disabled stays out.
func (m *linkMemo) added(ctx context.Context, tx *db.Tx, dt *doctype.DocType, id int64) error {
	list, read := m.candidates[dt.Name]
	if !read {
		return nil
	}
	var code string
	err := tx.SQL().QueryRowContext(ctx,
		"SELECT "+dt.Table+".code FROM "+dt.Table+" WHERE "+dt.Table+".id = ? AND "+dt.Table+".disabled = 0", id).Scan(&code)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	c := candidate{id: id, code: code, lower: []rune(strings.ToLower(code))}
	at, _ := slices.BinarySearchFunc(list, code, func(x candidate, code string) int { return cmp.Compare(x.code, code) })
	m.candidates[dt.Name] = slices.Insert(list, at, c)
	return nil
}

// targetRows is target's active rows sorted by code, read once per run. It
// reads the table's raw disabled column, not the MasterLookup: every master
// table has one (CD-21).
func (m *linkMemo) targetRows(ctx context.Context, tx *db.Tx, target *doctype.DocType) ([]candidate, error) {
	if list, read := m.candidates[target.Name]; read {
		return list, nil
	}
	table := target.Table
	rows, err := tx.SQL().QueryContext(ctx, "SELECT "+table+".id, "+table+".code FROM "+table+" WHERE "+table+".disabled = 0")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	list := []candidate{}
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.code); err != nil {
			return nil, err
		}
		c.lower = []rune(strings.ToLower(c.code))
		list = append(list, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.SortFunc(list, func(x, y candidate) int { return cmp.Compare(x.code, y.code) })
	m.candidates[target.Name] = list
	return list, nil
}

// resolveLink finds the active record whose code is cell in f's target
// DocType. It returns the record id, or a user message when the code is
// missing or disabled. A record the link check refuses is missing: the check
// comes before the disabled answer, so hidden and unknown codes read the same
// (CD-232). Everything runs in tx.
func resolveLink(ctx context.Context, tx *db.Tx, m *linkMemo, f *doctype.Field, cell string) (int64, string, error) {
	target, ok := doctype.Lookup(f.Link)
	if !ok {
		return 0, "", fmt.Errorf("csvio: field %s links to unknown DocType %q", f.Name, f.Link)
	}
	var id int64
	err := tx.SQL().QueryRowContext(ctx,
		"SELECT "+target.Table+".id FROM "+target.Table+" WHERE "+target.Table+".code = ?", cell).Scan(&id)
	found := !errors.Is(err, sql.ErrNoRows)
	if err != nil && found {
		return 0, "", err
	}
	if found {
		if found, err = m.can(ctx, tx, target, id); err != nil {
			return 0, "", err
		}
	}
	if !found {
		s, err := suggestCode(ctx, tx, m, target, cell)
		if err != nil {
			return 0, "", err
		}
		msg := f.Label + " '" + cell + "' not found."
		if s != "" {
			msg += " Use an existing " + f.Label + " code such as " + s + "."
		}
		return 0, msg, nil
	}
	if target.Master != nil {
		mi, _, err := target.Master.Lookup(ctx, tx, id)
		if err != nil {
			return 0, "", err
		}
		if mi.Disabled {
			return 0, f.Label + " '" + cell + "' is disabled. Enable it or use another " + f.Label + ".", nil
		}
	}
	return id, "", nil
}

// suggestCode returns the active code the user may link to sharing the longest
// case-insensitive prefix with cell (ties: alphabetical), or "" when there is
// none. The rows go into buckets by shared length, each still in code order, so
// no cell sorts; link checks stop at the first readable row.
func suggestCode(ctx context.Context, tx *db.Tx, m *linkMemo, target *doctype.DocType, cell string) (string, error) {
	list, err := m.targetRows(ctx, tx, target)
	if err != nil {
		return "", err
	}
	want := []rune(strings.ToLower(cell))
	buckets := make([][]int, len(want)+1)
	for i, c := range list {
		n := 0
		for n < len(want) && n < len(c.lower) && want[n] == c.lower[n] {
			n++
		}
		buckets[n] = append(buckets[n], i)
	}
	for n := len(want); n >= 0; n-- {
		for _, i := range buckets[n] {
			ok, err := m.can(ctx, tx, target, list[i].id)
			if err != nil {
				return "", err
			}
			if ok {
				return list[i].code, nil
			}
		}
	}
	return "", nil
}
