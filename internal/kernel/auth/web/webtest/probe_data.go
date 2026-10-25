package webtest

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Doc is one probe row for AddDoc. Zero IDs are stored as NULL. On a master,
// Number is its code and name. Line i gets LineSiteIDs[i] and
// LineProjectIDs[i] where present.
type Doc struct {
	Number, State                         string
	CreatedBy                             auth.UserID
	EmployeeID, ProjectID, SiteID, Amount int64
	LineSiteIDs, LineProjectIDs           []int64
}

// attachmentTable holds AddAttachment's files; WithProbe creates it.
const attachmentTable = `CREATE TABLE IF NOT EXISTS probe_attachment (id INTEGER PRIMARY KEY, doctype TEXT NOT NULL, parent_id INTEGER NOT NULL, name TEXT NOT NULL, body BLOB NOT NULL)`

// nullID stores 0 as NULL.
func nullID(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func (h *Harness) write(t testing.TB, what string, fn func(ctx context.Context, q db.DBTX) error) {
	t.Helper()
	ctx := context.Background()
	if err := h.W.DB.Write(ctx, func(tx *db.Tx) error { return fn(ctx, tx.SQL()) }); err != nil {
		t.Fatalf("webtest.%s: %v", what, err)
	}
}

func mustDocType(t testing.TB, name string) *doctype.DocType {
	t.Helper()
	dt, ok := doctype.Lookup(name)
	if !ok {
		t.Fatalf("webtest: unknown DocType %q", name)
	}
	return dt
}

// AddDoc inserts d into doctype's probe table (and its lines) and returns its ID.
func (h *Harness) AddDoc(t testing.TB, doctypeName string, d Doc) int64 {
	t.Helper()
	dt := mustDocType(t, doctypeName)
	var id int64
	h.write(t, "AddDoc", func(ctx context.Context, q db.DBTX) error {
		if dt.Master != nil {
			return q.QueryRowContext(ctx,
				`INSERT INTO `+dt.Table+` (code, name, created_by) VALUES (?, ?, ?) RETURNING id`, //nolint:gosec // G202: table is a probe constant
				d.Number, d.Number, nullID(int64(d.CreatedBy))).Scan(&id)
		}
		var number any
		if d.Number != "" {
			number = d.Number
		}
		if err := q.QueryRowContext(ctx,
			`INSERT INTO `+dt.Table+` (number, state, created_by, created_at, employee_id, project_id, site_id, amount) VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`, //nolint:gosec // G202: table is a probe constant
			number, d.State, nullID(int64(d.CreatedBy)), auth.FormatTime(h.W.Clock.Now()),
			nullID(d.EmployeeID), nullID(d.ProjectID), nullID(d.SiteID), d.Amount).Scan(&id); err != nil {
			return err
		}
		return insertLines(ctx, q, dt, id, d.LineSiteIDs, d.LineProjectIDs)
	})
	return id
}

// insertLines adds max(len(sites), len(projects)) lines to parent.
func insertLines(ctx context.Context, q db.DBTX, dt *doctype.DocType, parent int64, sites, projects []int64) error {
	for i := range max(len(sites), len(projects)) {
		var site, project int64
		if i < len(sites) {
			site = sites[i]
		}
		if i < len(projects) {
			project = projects[i]
		}
		if _, err := q.ExecContext(ctx,
			`INSERT INTO `+dt.LinesTable+` (parent_id, line_site_id, line_project_id) VALUES (?, ?, ?)`, //nolint:gosec // G202: table is a probe constant
			parent, nullID(site), nullID(project)); err != nil {
			return err
		}
	}
	return nil
}

// AddAttachment stores a file on record parent of doctype and returns its ID
// (GET /files/{id}). Needs WithProbe.
func (h *Harness) AddAttachment(t testing.TB, doctypeName string, parent int64, name string, body []byte) int64 {
	t.Helper()
	mustDocType(t, doctypeName)
	var id int64
	h.write(t, "AddAttachment", func(ctx context.Context, q db.DBTX) error {
		return q.QueryRowContext(ctx,
			`INSERT INTO probe_attachment (doctype, parent_id, name, body) VALUES (?, ?, ?, ?) RETURNING id`,
			doctypeName, parent, name, body).Scan(&id)
	})
	return id
}

// Ref returns the RecordRef of record id, built from authtest.Info.
func (h *Harness) Ref(t testing.TB, doctypeName string, id int64) auth.RecordRef {
	t.Helper()
	ctx := context.Background()
	var info doctype.Info
	if err := h.W.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		info, err = h.W.Info.Info(ctx, tx, doctypeName, id)
		return err
	}); err != nil {
		t.Fatalf("webtest.Ref(%s, %d): %v", doctypeName, id, err)
	}
	return refOf(info)
}

func refOf(info doctype.Info) auth.RecordRef {
	return auth.RecordRef{
		DocType: info.DocType, ID: info.ID, CreatedBy: info.CreatedBy, EmployeeID: info.EmployeeID,
		ProjectIDs: info.ProjectIDs, SiteIDs: info.SiteIDs,
	}
}
