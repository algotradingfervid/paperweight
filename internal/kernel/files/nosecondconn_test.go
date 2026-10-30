package files_test

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// CD-213: non-Administrators on the real auth.Service with the pool cut to one
// connection, so canOnParent and AttachParent must read in the caller's tx.

// nsDocType is registered because the real service refuses an unknown DocType.
const (
	nsDocType = "filestest.NoSecondConnDoc"
	nsDocName = "Draft 42"
	nsWriter  = "No Second Conn Writer"
	nsReader  = "No Second Conn Reader"
)

var _ = func() bool {
	doctype.Register(doctype.DocType{Name: nsDocType, Label: "Expense Claim", PluralLabel: "Expense Claims"})
	return true
}()

// nsFiles is filestest's Env with its Store on the real auth.Service, a parent
// document, and a user with write and a user with only read on it.
type nsFiles struct {
	*filestest.Env
	writer, reader auth.Principal
	parent         auth.RecordRef
}

func newNsFiles(t *testing.T) *nsFiles {
	t.Helper()
	ctx := t.Context()
	env := filestest.New(t)
	a := auth.New(auth.Deps{DB: env.DB, Clock: env.Clock, Directory: authfake.NewDirectory(), Audit: auditfake.New()})
	id, err := a.CreateFirstAdmin(ctx, "ns-admin@vardhan.test", "No Second Conn Admin", "Correct-Horse-42")
	if err != nil {
		t.Fatalf("CreateFirstAdmin: %v", err)
	}
	admin := auth.Principal{ID: id, Roles: []string{auth.RoleAdministrator}}
	for role, rights := range map[string]map[auth.Right]auth.Scope{
		nsWriter: {auth.RightRead: auth.ScopeAll, auth.RightWrite: auth.ScopeAll},
		nsReader: {auth.RightRead: auth.ScopeAll},
	} {
		if err := a.CreateRole(ctx, admin, role); err != nil {
			t.Fatalf("CreateRole(%s): %v", role, err)
		}
		if err := a.SetRights(ctx, admin, role, nsDocType, rights); err != nil {
			t.Fatalf("SetRights(%s): %v", role, err)
		}
	}
	s, err := files.New(files.Config{DataDir: env.DataDir, DB: env.DB, Auth: a, Info: env.Docs, Clock: env.Clock, Audit: env.Audit})
	if err != nil {
		t.Fatalf("files.New: %v", err)
	}
	env.Store = s
	deepak, ravi := env.User("deepak@vardhan.test"), env.User("ravi@vardhan.test")
	deepak.Roles, ravi.Roles = []string{nsWriter}, []string{nsReader}
	env.Docs.SetInfo(doctype.Info{
		DocType: nsDocType, DocLabel: "Expense Claim", ID: 42, State: doctype.StateDraft, Display: nsDocName, CreatedBy: deepak.ID,
	})
	return &nsFiles{Env: env, writer: deepak, reader: ravi, parent: auth.RecordRef{DocType: nsDocType, ID: 42, CreatedBy: deepak.ID}}
}

// attach spools outside any transaction and attaches on the pool of one.
func (e *nsFiles) attach(t *testing.T, p auth.Principal) (int64, error) {
	t.Helper()
	sp, err := e.Store.Spool(bytes.NewReader(filestest.PDF(100)), files.MaxFileBytes)
	if err != nil {
		t.Fatalf("Spool: %v", err)
	}
	defer func() { _ = sp.Cleanup() }()
	var id int64
	err = testkit.NoSecondConn(t, e.DB, func(ctx context.Context) error {
		return e.DB.Write(ctx, func(tx *db.Tx) (err error) {
			id, err = e.Store.Attach(ctx, tx, p, e.parent, "receipt.pdf", sp)
			return err
		})
	})
	return id, err
}

// remove removes on the pool of one.
func (e *nsFiles) remove(t *testing.T, p auth.Principal, id int64) error {
	t.Helper()
	return testkit.NoSecondConn(t, e.DB, func(ctx context.Context) error {
		return e.DB.Write(ctx, func(tx *db.Tx) error { return e.Store.Remove(ctx, tx, p, id) })
	})
}

// seeded attaches a file on the full pool.
func (e *nsFiles) seeded(t *testing.T) int64 {
	t.Helper()
	id, err := e.Attach(e.writer, e.parent, "receipt.pdf", filestest.PDF(100))
	if err != nil {
		t.Fatalf("setup Attach: %v", err)
	}
	return id
}

// Attach checks write on the parent inside the caller's write (AttachParent).
func TestNoSecondConn_FilesAttach(t *testing.T) {
	e := newNsFiles(t)
	id, err := e.attach(t, e.writer)
	if err != nil {
		t.Fatalf("Attach as a writer: %v, want ok", err)
	}
	if rows := e.Rows(e.parent); len(rows) != 1 || rows[0].ID != id {
		t.Errorf("Rows = %+v, want attachment %d", rows, id)
	}
}

// Attach without write on the parent is the 403 naming the document.
func TestNoSecondConn_FilesAttachRefused(t *testing.T) {
	e := newNsFiles(t)
	_, err := e.attach(t, e.reader)
	wantRefused(t, "Attach as a reader", err, files.KindForbidden, 403, "You don't have permission to add files to "+nsDocName+".")
	if rows := e.Rows(e.parent); len(rows) != 0 {
		t.Errorf("Rows = %+v, want none", rows)
	}
}

// List checks read on the parent inside its read transaction (canOnParent).
func TestNoSecondConn_FilesList(t *testing.T) {
	e := newNsFiles(t)
	id := e.seeded(t)
	var got []files.Meta
	err := testkit.NoSecondConn(t, e.DB, func(ctx context.Context) (err error) {
		got, err = e.Store.List(ctx, e.reader, e.parent)
		return err
	})
	if err != nil {
		t.Fatalf("List as a reader: %v, want ok", err)
	}
	if !slices.ContainsFunc(got, func(m files.Meta) bool { return m.ID == id }) {
		t.Errorf("List = %+v, want attachment %d", got, id)
	}
}

// List without read on the parent is the 403 worded for read.
func TestNoSecondConn_FilesListRefused(t *testing.T) {
	e := newNsFiles(t)
	e.seeded(t)
	none := e.User("kavya@vardhan.test")
	none.Roles = nil
	err := testkit.NoSecondConn(t, e.DB, func(ctx context.Context) error {
		_, err := e.Store.List(ctx, none, e.parent)
		return err
	})
	wantRefused(t, "List with no role", err, files.KindForbidden, 403,
		"You don't have permission to read Expense Claim. Ask your administrator for a role that allows it.")
}

// Open checks read on the parent inside its read transaction (canOnParent).
func TestNoSecondConn_FilesOpen(t *testing.T) {
	e := newNsFiles(t)
	id := e.seeded(t)
	var got files.Meta
	err := testkit.NoSecondConn(t, e.DB, func(ctx context.Context) error {
		rc, m, err := e.Store.Open(ctx, e.reader, id)
		if err != nil {
			return err
		}
		got = m
		return rc.Close()
	})
	if err != nil {
		t.Fatalf("Open as a reader: %v, want ok", err)
	}
	if got.ID != id || got.Name != "receipt.pdf" {
		t.Errorf("Open meta = %+v, want attachment %d receipt.pdf", got, id)
	}
}

// Remove checks write on the parent inside the caller's write (canOnParent).
func TestNoSecondConn_FilesRemove(t *testing.T) {
	e := newNsFiles(t)
	id := e.seeded(t)
	if err := e.remove(t, e.writer, id); err != nil {
		t.Fatalf("Remove as a writer: %v, want ok", err)
	}
	if rows := e.Rows(e.parent); len(rows) != 0 {
		t.Errorf("Rows = %+v, want none", rows)
	}
}

// Remove without write on the parent is the 403 worded for change.
func TestNoSecondConn_FilesRemoveRefused(t *testing.T) {
	e := newNsFiles(t)
	id := e.seeded(t)
	wantRefused(t, "Remove as a reader", e.remove(t, e.reader, id), files.KindForbidden, 403,
		"You don't have permission to change Expense Claim. Ask your administrator for a role that allows it.")
	if rows := e.Rows(e.parent); len(rows) != 1 {
		t.Errorf("Rows = %+v, want the file kept", rows)
	}
}
