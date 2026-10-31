package csvio

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// FormatCell is the export text of v, a list.Row value of field f. A Link
// value is the target's code, a document's label, or a codeless master's
// name or id (Export resolves it); nil is "".
func FormatCell(f doctype.Field, v any) string {
	if v == nil {
		return ""
	}
	switch f.Type {
	case doctype.Money:
		return v.(types.Money).Plain()
	case doctype.Qty:
		return v.(types.Qty).Plain()
	case doctype.Rate:
		return v.(types.Rate).Plain()
	case doctype.Percent:
		return v.(types.Percent).Plain()
	case doctype.Factor:
		return v.(types.Factor).Plain()
	case doctype.Date:
		return v.(types.Date).ISO()
	case doctype.Timestamp:
		return types.FormatISTExport(v.(types.Timestamp).Time())
	case doctype.Check:
		if v.(bool) {
			return "Yes"
		}
		return "No"
	case doctype.Text, doctype.Select, doctype.Link:
		return EscapeFormula(v.(string))
	}
	return fmt.Sprint(v)
}

// MaxExportRows is the most rows one export writes.
const MaxExportRows = 50000

// Exporter writes list results as CSV.
type Exporter struct {
	db   *db.DB
	auth auth.Service
}

// NewExporter returns an Exporter; link targets come from doctype.Lookup.
func NewExporter(d *db.DB, a auth.Service) *Exporter {
	return &Exporter{db: d, auth: a}
}

// Export writes every row matching q that p may read: a BOM, a header of
// field labels, then one CRLF-terminated line per row. It returns the
// number of data rows. Without the export right, or when more than
// MaxExportRows rows match, it writes nothing.
func (ex *Exporter) Export(ctx context.Context, w io.Writer, p auth.Principal, dt *doctype.DocType, q list.Query) (int, error) {
	if err := ex.auth.Can(ctx, p, dt.Name, "export", nil); err != nil {
		return 0, err
	}
	scope, err := ex.auth.Scope(ctx, p, dt.Name, "read")
	if err != nil {
		return 0, err
	}
	query, args, err := list.BuildAll(dt, q, scope)
	if err != nil {
		return 0, err
	}
	countQ, countArgs, err := list.BuildCount(dt, q, scope)
	if err != nil {
		return 0, err
	}
	// The columns ScanRow fills: line fields live on the lines table.
	fields := list.HeaderFields(dt)
	n := 0
	err = ex.db.Read(ctx, func(tx *db.Tx) error {
		var total int64
		if err := tx.SQL().QueryRowContext(ctx, countQ, countArgs...).Scan(&total); err != nil {
			return err
		}
		if total > MaxExportRows {
			return &list.QueryError{Msg: "Too many rows to export (" + types.FormatCount(total) + "). Narrow the filters to 50,000 rows or fewer."}
		}
		codes, docs, plain, err := linkCodes(ctx, tx, fields)
		if err != nil {
			return err
		}
		rows, err := tx.SQL().QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		if _, err := w.Write(utf8BOM); err != nil {
			return err
		}
		cw := csv.NewWriter(w)
		cw.UseCRLF = true
		texts := map[linkKey]string{} // masterText per target row, for this export only
		rec := make([]string, len(fields))
		for i, f := range fields {
			rec[i] = f.Label
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
		for rows.Next() {
			r, err := list.ScanRow(dt, rows)
			if err != nil {
				return err
			}
			for i, f := range fields {
				v := r.Values[f.Name]
				if id, ok := v.(int64); ok && f.Type == doctype.Link {
					// A missing link target exports an empty cell on purpose:
					// the map lookup gives "".
					v = codes[f.Name][id]
					if target := plain[f.Name]; target != nil {
						k := linkKey{target.Name, id}
						text, seen := texts[k]
						if !seen {
							if text, err = ex.masterText(ctx, tx, p, target, id); err != nil {
								return err
							}
							texts[k] = text
						}
						v = text
					}
					if target := docs[f.Name]; target != "" {
						// CD-297, CD-233: CheckLinkTx is CanTx(read) on the target's
						// full RecordRef, in this read tx. A Forbidden or NotFound
						// refusal (hidden or gone) blanks the cell; any other error
						// ends the export.
						err := ex.auth.CheckLinkTx(ctx, tx, p, target, id)
						switch {
						case errors.Is(err, auth.ErrForbidden), errors.Is(err, auth.ErrNotFound):
							v = ""
						case err != nil:
							return err
						}
					}
				}
				rec[i] = FormatCell(f, v)
			}
			if err := cw.Write(rec); err != nil {
				return err
			}
			n++
		}
		if err := rows.Err(); err != nil {
			return err
		}
		cw.Flush()
		return cw.Error()
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

// masterText is the export text of row id of target, a master without a
// code (CD-318): its display name, or the id when the name is blank. A row
// the link check refuses (hidden, no read right or gone) is "" (CD-233).
func (ex *Exporter) masterText(ctx context.Context, tx *db.Tx, p auth.Principal, target *doctype.DocType, id int64) (string, error) {
	if linkCheck != nil {
		err := linkCheck(ctx, tx, ex.auth, p, target, id)
		switch {
		case errors.Is(err, auth.ErrForbidden), errors.Is(err, auth.ErrNotFound):
			return "", nil
		case err != nil:
			return "", err
		}
	}
	mi, found, err := target.Master.Lookup(ctx, tx, id)
	if err != nil || !found {
		return "", err
	}
	if mi.Name == "" {
		return strconv.FormatInt(id, 10), nil
	}
	return mi.Name, nil
}

// linkCodes maps each Link field's name to its target's id → code. A target
// that isn't a master gives naming.Label(number, id) instead, and docs maps
// the field's name to that target's DocType name (CD-297). A master without
// a code field is left to masterText: plain maps the field's name to it
// (CD-318).
func linkCodes(ctx context.Context, tx *db.Tx, fields []doctype.Field) (codes map[string]map[int64]string, docs map[string]string, plain map[string]*doctype.DocType, err error) {
	codes, docs, plain = map[string]map[int64]string{}, map[string]string{}, map[string]*doctype.DocType{}
	for _, f := range fields {
		if f.Type != doctype.Link {
			continue
		}
		target, ok := doctype.Lookup(f.Link)
		if !ok {
			return nil, nil, nil, fmt.Errorf("csvio: field %s links to unknown DocType %q", f.Name, f.Link)
		}
		if _, hasCode := target.Field("code"); target.IsMaster() && !hasCode {
			plain[f.Name] = target
			continue
		}
		col := target.Table + ".code"
		if !target.IsMaster() {
			col = "COALESCE(" + target.Table + ".number, '')"
		}
		m, err := tableCodes(ctx, tx, target.Table, col)
		if err != nil {
			return nil, nil, nil, err
		}
		if !target.IsMaster() {
			for id, number := range m {
				m[id] = naming.Label(number, id)
			}
			docs[f.Name] = target.Name
		}
		codes[f.Name] = m
	}
	return codes, docs, plain, nil
}

func tableCodes(ctx context.Context, tx *db.Tx, table, col string) (map[int64]string, error) {
	rows, err := tx.SQL().QueryContext(ctx, "SELECT "+table+".id, "+col+" FROM "+table)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	m := map[int64]string{}
	for rows.Next() {
		var id int64
		var code string
		if err := rows.Scan(&id, &code); err != nil {
			return nil, err
		}
		m[id] = code
	}
	return m, rows.Err()
}
