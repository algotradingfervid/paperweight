package doc

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const maxLines = 500

// linkKey identifies a master record: the target DocType and its id.
type linkKey struct {
	Target string
	ID     int64
}

// masterResolver returns the MasterLookup of a master DocType; ok is false
// for a document DocType.
type masterResolver func(target string) (doctype.MasterLookup, bool)

// checkReadOnly refuses client values for ReadOnly fields (CD-79): any
// non-empty value on create (current nil), and on save a value that differs
// from the stored one.
func checkReadOnly(dt *DocType, in ActionInput, current *Doc) []FieldError {
	var errs []FieldError
	check := func(m map[string]string, stored Values, line int, onLine bool) {
		for _, f := range dt.Fields {
			if !f.ReadOnly || !userField(f, onLine) {
				continue
			}
			s := m[f.Name]
			if s == "" || (stored != nil && s == FormatPlain(f, stored[f.Name])) {
				continue
			}
			errs = append(errs, fieldErr(f, line, msgReadOnly))
		}
	}
	var header Values
	if current != nil {
		header = current.Header
	}
	check(in.Values, header, 0, false)
	line := 0
	for _, m := range in.Lines {
		if blankLine(dt, m) {
			continue
		}
		line++
		var stored Values
		if current != nil && m["id"] != "" {
			for _, l := range current.Lines {
				if strconv.FormatInt(l.ID, 10) == m["id"] {
					stored = l.Values
				}
			}
		}
		check(m, stored, line, true)
	}
	return errs
}

// checkFields collects every field error of n (D-DOC-20), in field order and
// then line order, at most one per field: required, NonNegative, Positive,
// Min/Max, MaxLen, Select options, then the master lookup of a Link (CD-21).
// Fields normalize couldn't read already have their error and are skipped.
// Master warnings are returned once per distinct text (CD-68). A lookup error
// stops further lookups and is left in n.err.
func checkFields(ctx context.Context, tx *db.Tx, dt *DocType, n *normalized, requireLines bool, masters masterResolver) (errs []FieldError, warnings []string) {
	check := func(vals Values, unread map[string]bool, line int, onLine bool) {
		for _, f := range dt.Fields {
			if !userField(f, onLine) || unread[f.Name] {
				continue
			}
			if e, w, bad := checkField(ctx, tx, f, vals[f.Name], line, n, masters); bad {
				errs = append(errs, e)
			} else if w != "" && !slices.Contains(warnings, w) {
				warnings = append(warnings, w)
			}
		}
	}
	check(n.Header, n.unread, 0, false)
	switch {
	case len(n.Lines) == 0 && requireLines:
		errs = append(errs, FieldError{Field: "lines", Message: msgNoLines})
	case len(n.Lines) > maxLines:
		errs = append(errs, FieldError{Field: "lines", Message: msgTooManyLines})
	}
	for i, l := range n.Lines {
		check(l.Values, l.unread, i+1, true)
	}
	return errs, warnings
}

// checkField returns the first error of value v of f, or a master warning.
func checkField(ctx context.Context, tx *db.Tx, f Field, v any, line int, n *normalized, masters masterResolver) (FieldError, string, bool) {
	if s, isText := v.(string); v == nil || (isText && strings.TrimSpace(s) == "") {
		if f.Required && f.Type != Check {
			return fieldErr(f, line, msgRequired), "", true
		}
		if v == nil {
			return FieldError{}, "", false
		}
	}
	if num, ok := intOf(v); ok && f.Type != Link {
		sc, _ := scale(f.Type)
		bound := func(b int64) string { return formatDecimal(b, sc) }
		switch {
		case f.NonNegative && num < 0:
			return fieldErr(f, line, msgNonNegative), "", true
		case f.Positive && num <= 0:
			return fieldErr(f, line, msgPositive), "", true
		case f.Min != nil && f.Max != nil && (num < *f.Min || num > *f.Max):
			return fieldErr(f, line, msgBetween, bound(*f.Min), bound(*f.Max)), "", true
		case f.Min != nil && f.Max == nil && num < *f.Min:
			return fieldErr(f, line, msgAtLeast, bound(*f.Min)), "", true
		case f.Max != nil && f.Min == nil && num > *f.Max:
			return fieldErr(f, line, msgAtMost, bound(*f.Max)), "", true
		}
	}
	if s, isText := v.(string); isText {
		if f.MaxLen > 0 && utf8.RuneCountInString(s) > f.MaxLen {
			return fieldErr(f, line, msgMaxLen, f.MaxLen), "", true
		}
		// PlainValue already refuses a value outside Options; this fires only
		// when Deps.ParseValue replaces PlainValue.
		if f.Type == Select && !slices.Contains(f.Options, s) {
			return fieldErr(f, line, msgUnreadable), "", true
		}
	}
	id, isID := v.(int64)
	if f.Type != Link || !isID || n.err != nil {
		return FieldError{}, "", false
	}
	lookup, ok := masters(f.Link)
	if !ok {
		return FieldError{}, "", false
	}
	key := linkKey{Target: f.Link, ID: id}
	mi, found := n.links[key]
	if !found {
		var err error
		mi, found, err = lookup.Lookup(ctx, tx, id)
		if err != nil {
			n.err = err
			return FieldError{}, "", false
		}
		if !found {
			return fieldErr(f, line, msgLinkNotFound), "", true
		}
		n.links[key] = mi
	}
	switch {
	case mi.Disabled:
		display := mi.Name
		if mi.Code != "" {
			display = mi.Code + " " + mi.Name
		}
		return fieldErr(f, line, msgLinkDisabled, display, strings.ToLower(f.Label)), "", true
	case mi.Blocked != "":
		return FieldError{Field: f.Name, Line: line, Message: linePrefix(line) + mi.Blocked}, "", true
	case mi.Warning != "":
		return FieldError{}, linePrefix(line) + mi.Warning, false
	}
	return FieldError{}, "", false
}
