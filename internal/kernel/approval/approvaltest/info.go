package approvaltest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

const docsTable = "aprtest_docs"

// harnessTables are the stand-in document tables, created in New (never in
// migrations/).
var harnessTables = []string{
	`CREATE TABLE IF NOT EXISTS aprtest_docs (
		doctype     TEXT    NOT NULL,
		id          INTEGER NOT NULL,
		state       TEXT    NOT NULL,
		number      TEXT    NOT NULL DEFAULT '',
		amount      INTEGER NOT NULL,
		created_by  INTEGER NOT NULL,
		employee_id INTEGER NOT NULL DEFAULT 0,
		project_id  INTEGER NOT NULL DEFAULT 0,
		party       TEXT    NOT NULL DEFAULT '',
		PRIMARY KEY (doctype, id)
	)`,
	`CREATE TABLE IF NOT EXISTS aprtest_effects (
		doctype TEXT    NOT NULL,
		id      INTEGER NOT NULL,
		n       INTEGER NOT NULL,
		PRIMARY KEY (doctype, id)
	)`,
	`CREATE TABLE IF NOT EXISTS aprtest_counters (
		prefix TEXT    PRIMARY KEY,
		n      INTEGER NOT NULL
	)`,
}

// Docs is the harness's doctype.InfoSource over aprtest_docs. It reads
// through the caller's tx, so its rows roll back and serialise with the
// db.Write that changes them (unlike docfake, which keeps Info in memory).
// Under the engine driver it is the engine's Info instead (engine.go).
type Docs struct{ engine doctype.InfoSource }

var _ doctype.InfoSource = (*Docs)(nil)

// Info returns the document's facts, or doctype.ErrNotFound (CD-54).
func (d *Docs) Info(ctx context.Context, tx *db.Tx, dt string, id int64) (doctype.Info, error) {
	if d.engine != nil {
		return d.engine.Info(ctx, tx, dt, id)
	}
	in := doctype.Info{DocType: dt, ID: id}
	var state string
	var project int64
	err := tx.SQL().QueryRowContext(ctx,
		`SELECT state, number, created_by, employee_id, project_id, party FROM aprtest_docs WHERE doctype = ? AND id = ?`,
		dt, id).Scan(&state, &in.Number, &in.CreatedBy, &in.EmployeeID, &project, &in.Party)
	if errors.Is(err, sql.ErrNoRows) {
		return doctype.Info{}, doctype.ErrNotFound
	}
	if err != nil {
		return doctype.Info{}, err
	}
	in.State = doctype.State(state)
	if d, ok := Lookup(dt); ok {
		in.DocLabel = d.Label
	}
	in.Display = in.Number
	if in.Display == "" {
		in.Display = fmt.Sprintf("Draft %d", id)
	}
	if project != 0 {
		in.ProjectIDs = []int64{project}
	}
	return in, nil
}

func docAmount(ctx context.Context, tx *db.Tx, d Doc) (types.Money, error) {
	var m types.Money
	err := tx.SQL().QueryRowContext(ctx, `SELECT amount FROM aprtest_docs WHERE doctype = ? AND id = ?`, d.Type, d.ID).Scan(&m)
	return m, err
}
