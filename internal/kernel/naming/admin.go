package naming

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/naming/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// requireAdmin returns a forbidden *Error unless p is an Administrator. It
// runs before db.Write, so the role lookup never waits behind our own lock.
func (n *Namer) requireAdmin(ctx context.Context, p auth.Principal) error {
	ok, err := n.roles.HasRole(ctx, p.ID, "Administrator")
	if err != nil {
		return err
	}
	if !ok {
		return &Error{Kind: auth.ErrForbidden, Msg: msgForbidden}
	}
	return nil
}

// List returns every series ordered by label, with each counter's last
// number keyed by rendered prefix.
func (n *Namer) List(ctx context.Context) ([]SeriesInfo, error) {
	var out []SeriesInfo
	err := n.db.Read(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		series, err := q.ListSeries(ctx)
		if err != nil {
			return err
		}
		counters, err := q.ListCounters(ctx)
		if err != nil {
			return err
		}
		byType := make(map[string]map[string]int64, len(series))
		for _, s := range series {
			c := map[string]int64{}
			byType[s.DocType] = c
			out = append(out, SeriesInfo{DocType: s.DocType, Label: s.Label, Template: s.Template, Counters: c})
		}
		for _, c := range counters {
			if m, ok := byType[c.DocType]; ok {
				m[c.Prefix] = c.Last
			}
		}
		return nil
	})
	return out, err
}

// SetTemplate changes doctype's template. Numbers already issued keep their
// text; a new prefix starts its own counter.
func (n *Namer) SetTemplate(ctx context.Context, p auth.Principal, doctype, tpl string) error {
	if err := n.requireAdmin(ctx, p); err != nil {
		return err
	}
	tpl = strings.TrimSpace(tpl)
	t, err := parseTemplate(tpl)
	if err != nil {
		return err
	}
	return n.db.Write(ctx, func(tx *db.Tx) error {
		s, err := loadSeries(ctx, tx.SQL(), doctype)
		if err != nil {
			return err
		}
		if err := checkPrefixFree(ctx, tx.SQL(), doctype, t); err != nil {
			return err
		}
		if err := dbq.New(tx.SQL()).UpdateSeriesTemplate(ctx, dbq.UpdateSeriesTemplateParams{
			Template:  tpl,
			UpdatedAt: nowText(n.clk),
			UpdatedBy: sql.NullInt64{Int64: int64(p.ID), Valid: true},
			ID:        s.ID,
		}); err != nil {
			return err
		}
		if s.Template == tpl {
			return nil
		}
		return n.audit.Append(ctx, tx, audit.Entry{
			DocType: "naming.Series",
			DocID:   s.ID,
			Action:  "update",
			Changes: []audit.Change{{Field: "template", Before: s.Template, After: tpl, Op: "set"}},
			By:      p.ID,
			At:      n.clk.Now().UTC(),
		})
	})
}

// ParseLastNumber reads the settings form's "last number used" field for a
// series with template tpl: a base-10 whole number from 0 to the template's
// largest counter, else ErrCounterRange.
func ParseLastNumber(tpl, s string) (int64, error) {
	t, err := parseTemplate(tpl)
	if err != nil {
		return 0, err
	}
	last, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, counterRangeError(t)
	}
	if err := checkCounterRange(t, last); err != nil {
		return 0, err
	}
	return last, nil
}

// checkCounterRange rejects a last number outside 0..t.maxCounter().
func checkCounterRange(t template, last int64) error {
	if last < 0 || last > t.maxCounter() {
		return counterRangeError(t)
	}
	return nil
}

func counterRangeError(t template) error {
	return &Error{Kind: ErrCounterRange, Msg: fmt.Sprintf(msgCounterRange, t.maxCounter()), Field: "last"}
}

// SetCounter sets the last number used for the rendered prefix of doctype's
// series. It can't go below the highest base number already issued there.
func (n *Namer) SetCounter(ctx context.Context, p auth.Principal, doctype, prefix string, last int64) error {
	if err := n.requireAdmin(ctx, p); err != nil {
		return err
	}
	return n.db.Write(ctx, func(tx *db.Tx) error {
		s, err := loadSeries(ctx, tx.SQL(), doctype)
		if err != nil {
			return err
		}
		t, err := parseTemplate(s.Template)
		if err != nil {
			return err
		}
		if !matchPrefix(t, prefix) {
			return &Error{Kind: ErrPrefixMismatch, Msg: fmt.Sprintf(msgPrefixMismatch, prefix, t.src)}
		}
		if err := checkCounterRange(t, last); err != nil {
			return err
		}
		highest, err := highestIssued(ctx, tx.SQL(), doctype, prefix)
		if err != nil {
			return err
		}
		if last < highest {
			return &Error{Kind: ErrCounterBelowIssued, Msg: fmt.Sprintf(msgBelowIssued, prefix, t.digits, highest, highest), Field: "last"}
		}
		before, err := readLast(ctx, tx.SQL(), doctype, prefix)
		if err != nil {
			return err
		}
		id, err := setLast(ctx, tx, doctype, prefix, last, sql.NullInt64{Int64: int64(p.ID), Valid: true}, nowText(n.clk))
		if err != nil {
			return err
		}
		return n.audit.Append(ctx, tx, audit.Entry{
			DocType: "naming.Counter",
			DocID:   id,
			Action:  "update",
			Changes: []audit.Change{{
				Field:  "last",
				Before: strconv.FormatInt(before, 10),
				After:  strconv.FormatInt(last, 10),
				Op:     "set",
			}},
			By: p.ID,
			At: n.clk.Now().UTC(),
		})
	})
}

// PreviewTemplate returns the next three numbers tpl would give a document
// dated d, joined with ", " (CD-57, CD-83), stopping at the series maximum.
// It looks the counter up by rendered prefix alone and takes the MAX across
// DocTypes: checkPrefixFree only checks live templates, and a retired
// template's counter rows remain. It never writes.
func (n *Namer) PreviewTemplate(ctx context.Context, tpl string, d types.Date) (string, error) {
	if d == (types.Date{}) {
		return "", &Error{Kind: ErrInvalidDate, Msg: msgNoDate}
	}
	t, err := parseTemplate(strings.TrimSpace(tpl))
	if err != nil {
		return "", err
	}
	var last int64
	err = n.db.Read(ctx, func(tx *db.Tx) error {
		var err error
		last, err = dbq.New(tx.SQL()).MaxCounterLastByPrefix(ctx, t.prefix(d))
		return err
	})
	if err != nil {
		return "", err
	}
	if last >= t.maxCounter() {
		return "", &Error{Kind: ErrExhausted, Msg: fmt.Sprintf(msgExhausted, t.prefix(d), t.maxCounter())}
	}
	nums := make([]string, 0, 3)
	for c := last + 1; c <= last+3 && c <= t.maxCounter(); c++ {
		nums = append(nums, t.render(d, c))
	}
	return strings.Join(nums, ", "), nil
}
