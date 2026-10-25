package doc

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// normalized is an ActionInput parsed against its DocType.
type normalized struct {
	Header Values
	Lines  []nline
	links  map[linkKey]doctype.MasterInfo // filled by checkFields
	err    error                          // the first MasterLookup error from checkFields; the engine returns it
	unread map[string]bool                // header fields normalize couldn't read; checkFields skips them
}

// nline is one kept input line. Pos is its index in ActionInput.Lines; its
// line number is its index in normalized.Lines plus one.
type nline struct {
	ID     int64
	Values Values
	Pos    int
	unread map[string]bool // fields normalize couldn't read; checkFields skips them
}

// userField reports whether a client may send f at the given level: System
// and RollUp keys are ignored (DOC-003).
func userField(f Field, onLine bool) bool {
	return f.OnLine == onLine && !f.System && !f.RollUp
}

// blankLine reports whether every user value provided in m is empty.
func blankLine(dt *DocType, m map[string]string) bool {
	for _, f := range dt.Fields {
		if s, ok := m[f.Name]; ok && userField(f, true) && strings.TrimSpace(s) != "" {
			return false
		}
	}
	return true
}

// linePrefix is "" on the header (line 0) and "Line L: " on line L.
func linePrefix(line int) string {
	if line == 0 {
		return ""
	}
	return fmt.Sprintf(msgLinePrefix, line)
}

func fieldErr(f Field, line int, format string, args ...any) FieldError {
	return FieldError{Field: f.Name, Line: line, Message: linePrefix(line) + fmt.Sprintf(format, append([]any{f.Label}, args...)...)}
}

// normalize keeps the user fields of in at their level, parses them, drops
// blank lines and fills Default for absent keys (header keys on create, line
// keys on new lines). Unreadable values are field errors.
func normalize(dt *DocType, in ActionInput, parse func(Field, string) (any, error), isCreate bool) (normalized, []FieldError) {
	n := normalized{Header: Values{}, links: map[linkKey]doctype.MasterInfo{}, unread: map[string]bool{}}
	var errs []FieldError
	read := func(vals Values, unread map[string]bool, m map[string]string, line int, onLine, defaults bool) {
		for _, f := range dt.Fields {
			if !userField(f, onLine) {
				continue
			}
			s, ok := m[f.Name]
			if !ok {
				if !defaults || f.Default == "" {
					continue
				}
				s = f.Default
			}
			v, err := parse(f, s)
			if err != nil {
				errs = append(errs, fieldErr(f, line, msgUnreadable))
				unread[f.Name] = true
				continue
			}
			vals[f.Name] = v
		}
	}
	read(n.Header, n.unread, in.Values, 0, false, isCreate)
	for pos, m := range in.Lines {
		if blankLine(dt, m) {
			continue
		}
		line := len(n.Lines) + 1
		var id int64
		if s := m["id"]; s != "" {
			v, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				errs = append(errs, FieldError{Field: "id", Line: line, Message: linePrefix(line) + fmt.Sprintf(msgUnreadable, msgLineID)})
			}
			id = v
		}
		nl := nline{ID: id, Values: Values{}, Pos: pos, unread: map[string]bool{}}
		read(nl.Values, nl.unread, m, line, true, id == 0)
		n.Lines = append(n.Lines, nl)
	}
	return n, errs
}
