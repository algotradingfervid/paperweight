package filestest

import (
	"bytes"
	"io/fs"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// Clock is a settable clock.Clock for tests.
type Clock = clock.Fixed

// docLabels are the DocType labels Doc puts in Info.DocLabel.
var docLabels = map[string]string{
	"expense":        "Expense Claim",
	"purchase_order": "Purchase Order",
	"grn":            "GRN",
	"company":        "Company",
}

// Env is a files.Store over a migrated temp database and data directory, with
// fake auth, documents and audit.
type Env struct {
	t       testing.TB
	Store   *files.Store
	DB      *db.DB
	DataDir string
	Auth    *authfake.Fake
	Docs    *docfake.Fake
	Audit   *auditfake.Recorder
	Clock   *Clock
}

// New returns an Env whose clock starts at testkit.Clock().Now().
func New(t testing.TB) *Env {
	t.Helper()
	e := &Env{
		t:       t,
		DB:      testkit.NewDB(t),
		DataDir: testkit.DataDir(t),
		Auth:    authfake.New(),
		Docs:    docfake.New(),
		Audit:   auditfake.New(),
		Clock:   testkit.Clock(),
	}
	s, err := files.New(files.Config{
		DataDir: e.DataDir, DB: e.DB, Auth: e.Auth, Info: e.Docs, Clock: e.Clock, Audit: e.Audit,
	})
	if err != nil {
		t.Fatalf("filestest.New: %v", err)
	}
	e.Store = s
	return e
}

// User is the fixture user with this email.
func (e *Env) User(email string) auth.Principal {
	e.t.Helper()
	return testkit.Principal(e.t, email)
}

// Doc seeds the docfake with a document and returns its RecordRef.
func (e *Env) Doc(dt string, id int64, state, display string, createdBy auth.UserID) auth.RecordRef {
	e.Docs.SetInfo(doctype.Info{
		DocType: dt, DocLabel: docLabels[dt], ID: id,
		State: doctype.State(state), Display: display, CreatedBy: createdBy,
	})
	return auth.RecordRef{DocType: dt, ID: id, CreatedBy: createdBy}
}

// DocInfo edits the Info stored for ref, e.g. to set ProjectIDs.
func (e *Env) DocInfo(ref auth.RecordRef, edit func(*doctype.Info)) {
	e.t.Helper()
	info, err := e.Docs.Info(e.t.Context(), nil, ref.DocType, ref.ID)
	if err != nil {
		e.t.Fatalf("filestest.DocInfo(%s %d): %v", ref.DocType, ref.ID, err)
	}
	edit(&info)
	e.Docs.SetInfo(info)
}

// SetState changes the stored document's state.
func (e *Env) SetState(ref auth.RecordRef, state string) {
	e.Docs.SetState(ref.DocType, ref.ID, doctype.State(state))
}

// Attach spools b outside any transaction, then attaches it inside db.Write.
func (e *Env) Attach(p auth.Principal, ref auth.RecordRef, name string, b []byte) (int64, error) {
	sp, err := e.Store.Spool(bytes.NewReader(b), files.MaxFileBytes)
	if err != nil {
		return 0, err
	}
	defer func() { _ = sp.Cleanup() }()
	var id int64
	err = e.DB.Write(e.t.Context(), func(tx *db.Tx) error {
		var err error
		id, err = e.Store.Attach(e.t.Context(), tx, p, ref, name, sp)
		return err
	})
	return id, err
}

// Blobs lists the files under <DataDir>/files except tmp/, as sorted slash paths.
func (e *Env) Blobs() []string {
	e.t.Helper()
	root := filepath.Join(e.DataDir, "files")
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == filepath.Join(root, "tmp") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		e.t.Fatalf("filestest.Blobs: %v", err)
	}
	slices.Sort(out)
	return out
}

// Rows reads ref's attachment rows, newest first.
func (e *Env) Rows(ref auth.RecordRef) []files.Meta {
	e.t.Helper()
	rows, err := e.DB.SQL().QueryContext(e.t.Context(),
		`SELECT id, file_name, content_type, size, sha256, uploaded_by, uploaded_at FROM attachment
		 WHERE parent_doctype = ? AND parent_id = ? ORDER BY uploaded_at DESC, id DESC`, ref.DocType, ref.ID)
	if err != nil {
		e.t.Fatalf("filestest.Rows: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []files.Meta
	for rows.Next() {
		var m files.Meta
		var by int64
		var at string
		if err := rows.Scan(&m.ID, &m.Name, &m.MIME, &m.Size, &m.SHA256, &by, &at); err != nil {
			e.t.Fatalf("filestest.Rows: scan: %v", err)
		}
		m.UploadedBy = auth.UserID(by)
		if m.UploadedAt, err = time.Parse(time.RFC3339Nano, at); err != nil {
			e.t.Fatalf("filestest.Rows: uploaded_at %q: %v", at, err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		e.t.Fatalf("filestest.Rows: %v", err)
	}
	return out
}

// RowParent returns the parent columns of attachment id.
func (e *Env) RowParent(id int64) (doctype string, parentID int64) {
	e.t.Helper()
	if err := e.DB.SQL().QueryRowContext(e.t.Context(),
		`SELECT parent_doctype, parent_id FROM attachment WHERE id = ?`, id).Scan(&doctype, &parentID); err != nil {
		e.t.Fatalf("filestest.RowParent(%d): %v", id, err)
	}
	return doctype, parentID
}

// DeleteRow deletes attachment id directly, leaving its blob.
func (e *Env) DeleteRow(id int64) {
	e.t.Helper()
	err := e.DB.Write(e.t.Context(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(e.t.Context(), `DELETE FROM attachment WHERE id = ?`, id)
		return err
	})
	if err != nil {
		e.t.Fatalf("filestest.DeleteRow(%d): %v", id, err)
	}
}
