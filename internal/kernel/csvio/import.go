package csvio

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Importer runs master imports, all rows in one transaction (CD-32).
type Importer struct {
	db    *db.DB
	auth  auth.Service
	clock clock.Clock
	audit audit.Writer
}

// NewImporter returns an Importer; production passes audit.Default (CD-03).
func NewImporter(d *db.DB, a auth.Service, c clock.Clock, w audit.Writer) *Importer {
	return &Importer{db: d, auth: a, clock: c, audit: w}
}

// Template is the import file for dt: a BOM and the Importable field labels.
func Template(dt *doctype.DocType) []byte {
	var labels []string
	for _, f := range dt.Fields {
		if f.Importable {
			labels = append(labels, f.Label)
		}
	}
	var b bytes.Buffer
	b.Write(utf8BOM)
	w := csv.NewWriter(&b)
	w.UseCRLF = true
	_ = w.Write(labels) // writes to a bytes.Buffer never fail
	w.Flush()
	return b.Bytes()
}

// RequiredColumns lists the labels of dt's Required Importable fields.
func RequiredColumns(dt *doctype.DocType) []string {
	var labels []string
	for _, f := range dt.Fields {
		if f.Required && f.Importable {
			labels = append(labels, f.Label)
		}
	}
	return labels
}

// Preview checks every row and rolls everything back.
func (im *Importer) Preview(ctx context.Context, p auth.Principal, dt *doctype.DocType, r io.Reader) (Report, error) {
	return im.run(ctx, p, dt, r, true)
}

// Import creates every row, or nothing when any row has an error.
func (im *Importer) Import(ctx context.Context, p auth.Principal, dt *doctype.DocType, r io.Reader) (Report, error) {
	return im.run(ctx, p, dt, r, false)
}

// errRollback makes db.Write roll back a preview or a failed import.
var errRollback = errors.New("csvio: roll back")

// excelNumber matches a value Excel turned into scientific notation.
var excelNumber = regexp.MustCompile(`^[0-9.]+E\+?[0-9]+$`)

func (im *Importer) run(ctx context.Context, p auth.Principal, dt *doctype.DocType, r io.Reader, preview bool) (Report, error) {
	if err := im.auth.Can(ctx, p, dt.Name, "import", nil); err != nil {
		return Report{}, err
	}
	fn, ok := importers[dt.Name]
	if !ok {
		return Report{}, ErrNoImporter
	}
	sheet, err := ReadSheet(r)
	if err != nil {
		return Report{}, err
	}
	cols, warns, err := MapHeader(dt, sheet.Header)
	if err != nil {
		return Report{}, err
	}
	rep := Report{Rows: len(sheet.Rows), Warnings: warns, PluralLabel: dt.PluralLabel, preview: preview}
	err = im.db.Write(ctx, func(tx *db.Tx) error {
		denied := false
		if err := im.auth.Can(ctx, p, dt.Name, "create", nil); errors.Is(err, auth.ErrForbidden) {
			denied = true
		} else if err != nil {
			return err
		}
		codes := map[string]int{} // asciiUpper(code) → first row
		var ids []int64
		for _, sr := range sheet.Rows {
			var errs []RowError
			if denied {
				errs = append(errs, RowError{Row: sr.Num, Message: "you don't have permission to create " + dt.Label + "."})
			}
			row, cellErrs, err := checkRow(ctx, tx, dt, cols, sr, codes)
			if err != nil {
				return err
			}
			errs = append(errs, cellErrs...)
			if len(errs) == 0 {
				id, fieldErrs, err := callRow(ctx, tx, p, fn, row)
				if err != nil {
					return err
				}
				for _, fe := range fieldErrs {
					msg := fe.Message
					if excelNumber.MatchString(row[fe.Field]) {
						msg += " Excel may have changed this value. Format the column as Text and retype it."
					}
					errs = append(errs, RowError{Row: sr.Num, Message: msg})
				}
				if len(fieldErrs) == 0 {
					ids = append(ids, id)
				}
			}
			rep.Errors = append(rep.Errors, errs...)
		}
		rep.Ready = len(ids)
		if preview || len(rep.Errors) > 0 {
			return errRollback
		}
		// Audit entries are written only now, after every row passed, on
		// purpose: the import is all-or-nothing, and a Writer (auditfake
		// keeps entries in memory) needn't follow the transaction's rollback.
		for _, id := range ids {
			err := im.audit.Append(ctx, tx, audit.Entry{
				DocType: dt.Name, DocID: id, Action: "create", Reason: "created by import", By: p.ID, At: im.clock.Now(),
			})
			if err != nil {
				return err
			}
		}
		rep.Imported = len(ids)
		return nil
	})
	if err != nil && !errors.Is(err, errRollback) {
		return Report{}, err
	}
	return rep, nil
}

// checkRow checks one row's cells, required fields and code, and returns
// the RowFunc's row map (CD-60) with links resolved to ids.
func checkRow(ctx context.Context, tx *db.Tx, dt *doctype.DocType, cols []ColumnMap, sr SheetRow, codes map[string]int) (map[string]string, []RowError, error) {
	row := map[string]string{}
	var errs []RowError
	for _, c := range cols {
		f := c.Field
		cell := UnescapeFormula(sr.Cells[c.Index])
		row[f.Name] = cell
		if cell == "" {
			continue
		}
		if f.Type == doctype.Link {
			id, msg, err := resolveLink(ctx, tx, f, cell)
			if err != nil {
				return nil, nil, err
			}
			if msg != "" {
				errs = append(errs, RowError{Row: sr.Num, Message: msg})
				continue
			}
			row[f.Name] = strconv.FormatInt(id, 10)
			continue
		}
		if msg := checkCell(f, cell); msg != "" {
			errs = append(errs, RowError{Row: sr.Num, Column: f.Label, Message: msg})
		}
	}
	for _, c := range cols {
		if c.Field.Required && row[c.Field.Name] == "" {
			errs = append(errs, RowError{Row: sr.Num, Message: c.Field.Label + " is required."})
		}
	}
	if code := row["code"]; code != "" {
		// Both sides are upper-cased ASCII-only: SQLite's upper() leaves
		// non-ASCII letters alone, and Go's strings.ToUpper doesn't.
		up := asciiUpper(code)
		if first, ok := codes[up]; ok {
			errs = append(errs, RowError{Row: sr.Num, Message: "Code " + code + " is repeated (also on row " + strconv.Itoa(first) + ")."})
		} else {
			codes[up] = sr.Num
			var one int
			err := tx.SQL().QueryRowContext(ctx,
				"SELECT 1 FROM "+dt.Table+" WHERE upper("+dt.Table+".code) = ?", up).Scan(&one)
			switch {
			case err == nil:
				errs = append(errs, RowError{Row: sr.Num, Message: "Code " + code + " already exists."})
			case !errors.Is(err, sql.ErrNoRows):
				return nil, nil, err
			}
		}
	}
	return row, errs, nil
}

// callRow runs fn inside a savepoint, rolled back when fn reports field errors.
func callRow(ctx context.Context, tx *db.Tx, p auth.Principal, fn RowFunc, row map[string]string) (int64, []doctype.FieldError, error) {
	if _, err := tx.SQL().ExecContext(ctx, "SAVEPOINT r"); err != nil {
		return 0, nil, err
	}
	id, errs, err := fn(context.WithValue(ctx, importKey{}, true), tx, p, row)
	if err != nil {
		return 0, nil, err
	}
	if len(errs) > 0 {
		if _, err := tx.SQL().ExecContext(ctx, "ROLLBACK TO r"); err != nil {
			return 0, nil, err
		}
	}
	if _, err := tx.SQL().ExecContext(ctx, "RELEASE r"); err != nil {
		return 0, nil, err
	}
	return id, errs, nil
}

// asciiUpper upper-cases a-z only, as SQLite's upper() does.
func asciiUpper(s string) string {
	return strings.Map(func(r rune) rune {
		if 'a' <= r && r <= 'z' {
			return r - 'a' + 'A'
		}
		return r
	}, s)
}
