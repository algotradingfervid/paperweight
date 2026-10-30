package mstkit

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Kind names one master: its DocType, the label used in messages, its table,
// and whether it has a code column.
type Kind struct {
	DocType, Label, Table string
	HasCode               bool
}

// Saved is what a Create or Update returns.
type Saved struct {
	ID       int64
	Version  int
	Warnings []string
}

// codePattern is checked on the upper-cased code, so "Ä" still fails (MST-004).
var codePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9._/-]{0,19}$`)

const (
	msgCodeRequired    = "Code is required. Enter a short code such as SHREE."
	msgCodeFormat      = "Code can use letters, digits and . _ / - only, must start with a letter or digit, and be at most 20 characters."
	msgNameRequired    = "Name is required."
	msgCodeChange      = "Only a Masters Manager can change a code."
	roleMastersManager = "Masters Manager"

	// MaxNameLen is the longest name, or name-like text such as an address
	// line, in characters (CD-223).
	MaxNameLen = 140
)

// NormalizeCode trims and upper-cases raw and checks its format (MST-001,
// MST-002, MST-004).
func NormalizeCode(raw string) (string, error) {
	code := strings.ToUpper(strings.TrimSpace(raw))
	if code == "" {
		return "", fieldError("code", msgCodeRequired)
	}
	if !codePattern.MatchString(code) {
		return "", fieldError("code", msgCodeFormat)
	}
	return code, nil
}

// NormalizeName trims raw; a blank or over-long name is an error (MST-017,
// CD-223).
func NormalizeName(raw string) (string, error) {
	return NormalizeLabelledName(raw, "Name")
}

// NormalizeLabelledName is NormalizeName for a name field labelled other than
// Name; label is shown in the length refusal.
func NormalizeLabelledName(raw, label string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", fieldError("name", msgNameRequired)
	}
	var es Errors
	es.CheckLen("name", label, name)
	if err := es.Err(); err != nil {
		return "", err
	}
	return name, nil
}

// CheckLen records field's refusal when value is over MaxNameLen characters,
// in the words of DOC's msgMaxLen for the same condition. It counts characters,
// not bytes: Hindi takes three bytes each.
func (es *Errors) CheckLen(field, label, value string) {
	if utf8.RuneCountInString(value) > MaxNameLen {
		es.Add(field, label+" can be at most "+strconv.Itoa(MaxNameLen)+" characters.")
	}
}

// CheckCodeFree fails when another row of k (not exceptID) already has code.
// It reads inside tx (CD-94).
func CheckCodeFree(ctx context.Context, tx *db.Tx, k Kind, code string, exceptID int64) error {
	var holder string
	err := tx.SQL().QueryRowContext(ctx,
		"SELECT name FROM "+k.Table+" WHERE code = ? AND id <> ?", //nolint:gosec // G202: Table/FKColumn come from each master's package-level Kind, never from a request
		code, exceptID).Scan(&holder)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return fieldError("code", "Code "+code+" is already used by "+k.Label+" "+holder+". Choose another code.")
}

// CheckCodeChange allows a code change only for a Masters Manager; an
// unchanged code (in any case) is always allowed. It reads inside tx (CD-213).
func (e Env) CheckCodeChange(ctx context.Context, tx *db.Tx, p auth.Principal, oldCode, newCode string) error {
	if strings.EqualFold(strings.TrimSpace(oldCode), strings.TrimSpace(newCode)) {
		return nil
	}
	ok, err := e.Auth.HasRoleTx(ctx, tx, p.ID, roleMastersManager)
	if err != nil {
		return err
	}
	if !ok {
		return fieldError("code", msgCodeChange)
	}
	return nil
}
