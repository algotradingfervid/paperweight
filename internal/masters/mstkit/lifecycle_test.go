package mstkit_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// party is a Kind over the test-only kit_thing table; the label gives the
// messages the 06-masters.md wording ("Party V-SHARMA").
var party = mstkit.Kind{DocType: "Party", Label: "Party", Table: "kit_thing", HasCode: true}

// kitThingDDL has the Global Constraints' master columns (CD-34 types only).
const kitThingDDL = `CREATE TABLE kit_thing (
	id INTEGER PRIMARY KEY,
	code TEXT NOT NULL UNIQUE CHECK (code = upper(code)),
	name TEXT NOT NULL,
	disabled INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0,1)),
	version INTEGER NOT NULL DEFAULT 1,
	created_at TEXT NOT NULL,
	created_by INTEGER,
	updated_at TEXT NOT NULL,
	updated_by INTEGER
)`

// kitChildDDL is a child table with a RESTRICT foreign key and no registry link.
const kitChildDDL = `CREATE TABLE kit_child (
	id INTEGER PRIMARY KEY,
	thing_id INTEGER NOT NULL REFERENCES kit_thing(id) ON DELETE RESTRICT
)`

// owned is a Kind over kit_owned, rows that belong to a kit_thing.
var owned = mstkit.Kind{DocType: "Party Bank Account", Label: "Party Bank Account", Table: "kit_owned"}

// kitOwnedDDL is a child table whose rows are deleted with their kit_thing.
const kitOwnedDDL = `CREATE TABLE kit_owned (
	id INTEGER PRIMARY KEY,
	thing_id INTEGER NOT NULL REFERENCES kit_thing(id) ON DELETE CASCADE,
	name TEXT NOT NULL
)`

const seedTS = "2026-10-24T05:00:00.000Z"

type kit struct {
	env          mstkit.Env
	db           *db.DB
	auth         *authfake.Fake
	docs         *docfake.Fake
	audit        *auditfake.Recorder
	clock        *clock.Fixed
	kavya, priya auth.Principal
}

func newKit(t *testing.T) *kit {
	t.Helper()
	k := &kit{
		db:    testkit.NewDB(t),
		auth:  authfake.New(),
		docs:  docfake.New(),
		audit: auditfake.New(),
		clock: testkit.Clock(),
		kavya: testkit.Principal(t, "kavya@vardhan.test"),
		priya: testkit.Principal(t, "priya@vardhan.test"),
	}
	k.env = mstkit.Env{DB: k.db, Auth: k.auth, Engine: k.docs, Auditor: k.audit, Clock: k.clock}
	k.auth.AddRole(k.kavya.ID, "Masters Manager")
	k.auth.Allow(k.kavya.ID, party.DocType, "read", "create", "write", "delete", "export", "import")
	k.write(t, func(tx *db.Tx) error {
		if _, err := tx.SQL().ExecContext(context.Background(), kitThingDDL); err != nil {
			return err
		}
		_, err := tx.SQL().ExecContext(context.Background(), kitChildDDL)
		return err
	})
	return k
}

// write runs fn in db.Write and fails the test on error.
func (k *kit) write(t *testing.T, fn func(tx *db.Tx) error) {
	t.Helper()
	if err := k.db.Write(context.Background(), fn); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// insertThing adds a kit_thing row at version 1 and returns its id.
func (k *kit) insertThing(t *testing.T, code, name string) int64 {
	t.Helper()
	var id int64
	k.write(t, func(tx *db.Tx) error {
		res, err := tx.SQL().ExecContext(context.Background(),
			`INSERT INTO kit_thing (code, name, created_at, updated_at) VALUES (?, ?, ?, ?)`,
			code, name, seedTS, seedTS)
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	return id
}

// thing reads disabled and version of a kit_thing row; ok is false when it is gone.
func (k *kit) thing(t *testing.T, id int64) (disabled, version int, ok bool) {
	t.Helper()
	err := k.db.Read(context.Background(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(context.Background(),
			`SELECT disabled, version FROM kit_thing WHERE id = ?`, id).Scan(&disabled, &version)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false
	}
	if err != nil {
		t.Fatalf("read kit_thing %d: %v", id, err)
	}
	return disabled, version, true
}

// changes strips a change list down to field, before and after.
func changes(cs []audit.Change) []audit.Change {
	out := make([]audit.Change, 0, len(cs))
	for _, c := range cs {
		out = append(out, audit.Change{Field: c.Field, Before: c.Before, After: c.After})
	}
	return out
}

func TestKit_CodeFreeNamesHolder(t *testing.T) {
	k := newKit(t)
	ctx := context.Background()
	id := k.insertThing(t, "V-SHARMA", "Sharma Steel Traders")
	const msg = "Code V-SHARMA is already used by Party Sharma Steel Traders. Choose another code."
	k.write(t, func(tx *db.Tx) error {
		if err := mstkit.CheckCodeFree(ctx, tx, party, "V-SHARMA", 0); err == nil || err.Error() != msg {
			t.Errorf("CheckCodeFree(V-SHARMA, 0) = %v; want %q", err, msg)
		}
		if err := mstkit.CheckCodeFree(ctx, tx, party, "V-SHARMA", id); err != nil {
			t.Errorf("CheckCodeFree(V-SHARMA, own id) = %v; want nil", err)
		}
		if err := mstkit.CheckCodeFree(ctx, tx, party, "C-TATA", 0); err != nil {
			t.Errorf("CheckCodeFree(C-TATA, 0) = %v; want nil", err)
		}
		return nil
	})
}

func TestKit_AuditWritesOnlyChanges(t *testing.T) {
	k := newKit(t)
	ctx := context.Background()
	id := k.insertThing(t, "V-SHARMA", "Sharma Steel")
	before := map[string]string{"code": "V-SHARMA", "name": "Sharma Steel", "version": "1", "updated_at": seedTS, "updated_by": ""}
	after := map[string]string{"code": "V-SHARMA", "name": "Sharma Steel Traders", "version": "2", "updated_at": "2026-10-24T05:01:00.000Z", "updated_by": "6"}
	k.write(t, func(tx *db.Tx) error {
		if err := k.env.Audit(ctx, tx, k.kavya, party, id, "save", before, after); err != nil {
			t.Fatalf("Audit save: %v", err)
		}
		// Only version, updated_at and updated_by differ: nothing to record.
		unchanged := map[string]string{"code": "V-SHARMA", "name": "Sharma Steel Traders", "version": "3", "updated_at": "2026-10-24T05:02:00.000Z", "updated_by": "4"}
		if err := k.env.Audit(ctx, tx, k.kavya, party, id, "save", after, unchanged); err != nil {
			t.Fatalf("Audit empty save: %v", err)
		}
		return nil
	})
	got := k.audit.Entries()
	if len(got) != 1 {
		t.Fatalf("entries = %+v; want exactly 1", got)
	}
	e := got[0]
	if e.DocType != "Party" || e.DocID != id || e.Action != "save" || e.By != k.kavya.ID || !e.At.Equal(k.clock.Now()) {
		t.Errorf("entry = %+v; want DocType Party, DocID %d, Action save, By %d, At %v", e, id, k.kavya.ID, k.clock.Now())
	}
	want := []audit.Change{{Field: "name", Before: "Sharma Steel", After: "Sharma Steel Traders"}}
	if c := changes(e.Changes); !reflect.DeepEqual(c, want) {
		t.Errorf("changes = %+v; want %+v", c, want)
	}
}

func TestKit_AuditSkipsCreateWhenImporting(t *testing.T) {
	k := newKit(t)
	plain := context.Background()
	if mstkit.IsImporting(plain) {
		t.Fatal("IsImporting(background) = true; want false")
	}
	ctx := mstkit.Importing(plain)
	if !mstkit.IsImporting(ctx) {
		t.Fatal("IsImporting(Importing(ctx)) = false; want true")
	}
	id := k.insertThing(t, "V-SHARMA", "Sharma Steel")
	created := map[string]string{"code": "V-SHARMA", "name": "Sharma Steel"}
	saved := map[string]string{"code": "V-SHARMA", "name": "Sharma Steel Traders"}
	k.write(t, func(tx *db.Tx) error {
		if err := k.env.Audit(ctx, tx, k.kavya, party, id, "create", nil, created); err != nil {
			t.Fatalf("Audit create (importing): %v", err)
		}
		if n := len(k.audit.Entries()); n != 0 {
			t.Errorf("after create under Importing: %d entries; want 0", n)
		}
		if err := k.env.Audit(ctx, tx, k.kavya, party, id, "save", created, saved); err != nil {
			t.Fatalf("Audit save (importing): %v", err)
		}
		return nil
	})
	got := k.audit.Entries()
	if len(got) != 1 || got[0].Action != "save" || got[0].DocID != id {
		t.Fatalf("entries = %+v; want one save entry for %d", got, id)
	}
	// Outside an import, create is recorded.
	other := k.insertThing(t, "C-TATA", "Tata Projects")
	k.write(t, func(tx *db.Tx) error {
		return k.env.Audit(plain, tx, k.kavya, party, other, "create", nil, map[string]string{"code": "C-TATA", "name": "Tata Projects"})
	})
	if es := k.audit.For("Party", other); len(es) != 1 || es[0].Action != "create" {
		t.Errorf("entries for %d = %+v; want one create entry", other, es)
	}
	// csvio's real import flags the RowFunc's ctx (CD-60): with no
	// mstkit.Importing, mstkit's create entry is still skipped, and the only
	// create entry is csvio's own "created by import" one (M12, NUM-145).
	t.Run("csvio import", func(t *testing.T) {
		k := newKit(t)
		registerImported.Do(func() {
			mstkit.RegisterMaster(doctype.DocType{
				Name:        imported.DocType,
				Label:       imported.Label,
				PluralLabel: "Imported Things",
				Table:       imported.Table,
				Importable:  true,
				Fields: []doctype.Field{
					{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, Required: true, Importable: true},
					{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, Required: true, Importable: true},
					{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check},
				},
			}, mstkit.StandardLookup(imported))
			csvio.RegisterImporter(imported.DocType, importedRow)
		})
		importRun = &importCall{kit: k}
		t.Cleanup(func() { importRun = nil })
		k.auth.Allow(k.kavya.ID, imported.DocType, "read", "create", "import")
		dt, ok := doctype.Lookup(imported.DocType)
		if !ok {
			t.Fatalf("doctype.Lookup(%q) not found", imported.DocType)
		}
		im := csvio.NewImporter(k.db, k.auth, k.clock, k.audit)
		rep, err := im.Import(context.Background(), k.kavya, dt, strings.NewReader("Code,Name\r\nK-KUMAR,Kumar Hardware\r\n"))
		if err != nil {
			t.Fatalf("csvio Import: %v", err)
		}
		if rep.Imported != 1 || len(rep.Errors) != 0 {
			t.Fatalf("report = %+v; want 1 imported, no errors", rep)
		}
		if importRun.calls != 1 || importRun.id == 0 {
			t.Fatalf("RowFunc calls = %d, id = %d; want one call that created a row", importRun.calls, importRun.id)
		}
		if !importRun.isImporting {
			t.Error("mstkit.IsImporting(RowFunc ctx) = false under csvio's import; want true (csvio.IsImport, CD-60)")
		}
		es := k.audit.For(imported.DocType, importRun.id)
		var mine, csvios int
		for _, e := range es {
			if e.Action != "create" {
				continue
			}
			if e.Reason == "created by import" {
				csvios++
			} else {
				mine++
			}
		}
		if mine != 0 {
			t.Errorf("mstkit create entries during csvio import = %d; want 0 (entries %+v)", mine, es)
		}
		if csvios != 1 {
			t.Errorf(`csvio "created by import" entries = %d; want 1 (entries %+v)`, csvios, es)
		}
	})
}

// imported is the importable test master the csvio sub-case registers.
var imported = mstkit.Kind{DocType: "kit.Imported", Label: "Thing", Table: "kit_thing", HasCode: true}

// registerImported guards the global registrations (both panic on a
// duplicate, -count > 1).
var registerImported sync.Once

// importCall is what importedRow sees during one csvio sub-case run.
type importCall struct {
	kit         *kit
	calls       int
	id          int64
	isImporting bool
}

var importRun *importCall

// importedRow is the csvio RowFunc: it inserts the row and audits a create
// through mstkit, as a master's CreateTx does.
func importedRow(ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string) (int64, []doctype.FieldError, error) {
	run := importRun
	run.calls++
	run.isImporting = mstkit.IsImporting(ctx)
	res, err := tx.SQL().ExecContext(ctx,
		`INSERT INTO kit_thing (code, name, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		row["code"], row["name"], seedTS, seedTS)
	if err != nil {
		return 0, nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, nil, err
	}
	run.id = id
	after := map[string]string{"code": row["code"], "name": row["name"]}
	if err := run.kit.env.Audit(ctx, tx, p, imported, id, "create", nil, after); err != nil {
		return 0, nil, err
	}
	return id, nil, nil
}

func TestKit_DisableWritesAudit(t *testing.T) {
	k := newKit(t)
	ctx := context.Background()
	id := k.insertThing(t, "V-SHARMA", "Sharma Steel Traders")
	k.write(t, func(tx *db.Tx) error {
		return k.env.SetDisabledTx(ctx, tx, k.kavya, party, id, 1, true)
	})
	if disabled, version, _ := k.thing(t, id); disabled != 1 || version != 2 {
		t.Errorf("after disable: disabled=%d version=%d; want 1, 2", disabled, version)
	}
	// Disabling an already-disabled row is a no-op: nil, no version bump, no audit.
	if err := k.db.Write(ctx, func(tx *db.Tx) error {
		return k.env.SetDisabledTx(ctx, tx, k.kavya, party, id, 2, true)
	}); err != nil {
		t.Errorf("disable already-disabled row = %v; want nil", err)
	}
	if disabled, version, _ := k.thing(t, id); disabled != 1 || version != 2 {
		t.Errorf("after second disable: disabled=%d version=%d; want 1, 2", disabled, version)
	}
	if n := len(k.audit.For("Party", id)); n != 1 {
		t.Errorf("entries after second disable = %d; want 1", n)
	}
	k.write(t, func(tx *db.Tx) error {
		return k.env.SetDisabledTx(ctx, tx, k.kavya, party, id, 2, false)
	})
	if disabled, version, _ := k.thing(t, id); disabled != 0 || version != 3 {
		t.Errorf("after enable: disabled=%d version=%d; want 0, 3", disabled, version)
	}
	// Enabling an already-enabled row is a no-op: nil, no version bump, no audit.
	if err := k.db.Write(ctx, func(tx *db.Tx) error {
		return k.env.SetDisabledTx(ctx, tx, k.kavya, party, id, 3, false)
	}); err != nil {
		t.Errorf("enable already-enabled row = %v; want nil", err)
	}
	if disabled, version, _ := k.thing(t, id); disabled != 0 || version != 3 {
		t.Errorf("after second enable: disabled=%d version=%d; want 0, 3", disabled, version)
	}
	got := k.audit.For("Party", id)
	if len(got) != 2 {
		t.Fatalf("entries = %+v; want 2", got)
	}
	for i, want := range []struct {
		action        string
		before, after string
	}{{"disable", "false", "true"}, {"enable", "true", "false"}} {
		e := got[i]
		if e.Action != want.action || e.By != k.kavya.ID {
			t.Errorf("entry %d = %+v; want action %s by %d", i, e, want.action, k.kavya.ID)
		}
		wc := []audit.Change{{Field: "disabled", Before: want.before, After: want.after}}
		if c := changes(e.Changes); !reflect.DeepEqual(c, wc) {
			t.Errorf("entry %d changes = %+v; want %+v", i, c, wc)
		}
	}
}

func TestKit_DeleteRefusedWhenReferenced(t *testing.T) {
	k := newKit(t)
	ctx := context.Background()
	id := k.insertThing(t, "V-SHARMA", "Sharma Steel Traders")
	k.docs.AddReference("kit_thing", id, doc.Reference{DocType: "Purchase Order", Field: "party", ID: 1, Display: "PO/26-27/00001", State: doc.StateApproved})
	const msg = "Cannot delete Party V-SHARMA: it is used in PO/26-27/00001. Disable it instead."
	err := k.db.Write(ctx, func(tx *db.Tx) error {
		return k.env.DeleteTx(ctx, tx, k.kavya, party, id, 1)
	})
	var refused *mstkit.DeleteRefusedError
	if !errors.As(err, &refused) || err.Error() != msg {
		t.Fatalf("DeleteTx = %v; want *DeleteRefusedError %q", err, msg)
	}
	if _, _, ok := k.thing(t, id); !ok {
		t.Error("referenced row was deleted")
	}
	if es := k.audit.For("Party", id); len(es) != 0 {
		t.Errorf("entries after refused delete = %+v; want none", es)
	}
	// An unreferenced row is deleted and audited.
	free := k.insertThing(t, "C-TATA", "Tata Projects")
	k.write(t, func(tx *db.Tx) error {
		return k.env.DeleteTx(ctx, tx, k.kavya, party, free, 1)
	})
	if _, _, ok := k.thing(t, free); ok {
		t.Error("unreferenced row still exists after DeleteTx")
	}
	if es := k.audit.For("Party", free); len(es) != 1 || es[0].Action != "delete" {
		t.Errorf("entries after delete = %+v; want one delete entry", es)
	}

	// A reference to a child row (CASCADE) blocks the master's delete too.
	t.Run("child row referenced", func(t *testing.T) {
		k := newKit(t)
		// A filler row first, so the master (id 2) and its child row (id 1)
		// don't share an id and a mixed-up table/id lookup can't pass.
		k.insertThing(t, "FILLER", "Filler")
		id := k.insertThing(t, "V-SHARMA", "Sharma Steel Traders")
		var childID int64
		k.write(t, func(tx *db.Tx) error {
			if _, err := tx.SQL().ExecContext(ctx, kitOwnedDDL); err != nil {
				return err
			}
			res, err := tx.SQL().ExecContext(ctx, `INSERT INTO kit_owned (thing_id, name) VALUES (?, ?)`, id, "Bank account")
			if err != nil {
				return err
			}
			childID, err = res.LastInsertId()
			return err
		})
		k.docs.AddReference("kit_owned", childID, doc.Reference{DocType: "Purchase Order", Field: "bank_account", ID: 1, Display: "PO/26-27/00001", State: doc.StateApproved})
		err := k.db.Write(ctx, func(tx *db.Tx) error {
			return k.env.DeleteTx(ctx, tx, k.kavya, party, id, 1, mstkit.Child{Kind: owned, FKColumn: "thing_id"})
		})
		var refused *mstkit.DeleteRefusedError
		if !errors.As(err, &refused) || err.Error() != msg {
			t.Fatalf("DeleteTx = %v; want *DeleteRefusedError %q", err, msg)
		}
		if _, _, ok := k.thing(t, id); !ok {
			t.Error("master row was deleted although its child row is referenced")
		}
		var n int
		if err := k.db.Read(ctx, func(tx *db.Tx) error {
			return tx.SQL().QueryRowContext(ctx, `SELECT count(*) FROM kit_owned WHERE id = ?`, childID).Scan(&n)
		}); err != nil {
			t.Fatalf("read kit_owned: %v", err)
		}
		if n != 1 {
			t.Errorf("kit_owned row %d count = %d; want 1 (still exists)", childID, n)
		}
		if es := k.audit.For("Party", id); len(es) != 0 {
			t.Errorf("entries after refused delete = %+v; want none", es)
		}
	})
}

func TestKit_UsedOutsideDrafts(t *testing.T) {
	k := newKit(t)
	ctx := context.Background()
	used := k.insertThing(t, "V-SHARMA", "Sharma Steel Traders")
	draftOnly := k.insertThing(t, "C-TATA", "Tata Projects")
	draft := doc.Reference{DocType: "Purchase Order", Field: "party", ID: 42, Display: "Draft 42", State: doc.StateDraft}
	master := doc.Reference{DocType: "Expense Type", Field: "party", ID: 7, Display: "Expense Type TRAVEL", State: ""}
	po := doc.Reference{DocType: "Purchase Order", Field: "party", ID: 1, Display: "PO/26-27/00001", State: doc.StateApproved}
	k.docs.AddReference("kit_thing", used, draft)
	k.docs.AddReference("kit_thing", used, master)
	k.docs.AddReference("kit_thing", used, po)
	k.docs.AddReference("kit_thing", draftOnly, draft)
	k.docs.AddReference("kit_thing", draftOnly, master)
	k.write(t, func(tx *db.Tx) error {
		ref, ok, err := k.env.UsedOutsideDrafts(ctx, tx, "kit_thing", used)
		if err != nil || !ok || ref != po {
			t.Errorf("UsedOutsideDrafts(draft, master, PO) = %+v, %v, %v; want %+v, true, nil", ref, ok, err, po)
		}
		ref, ok, err = k.env.UsedOutsideDrafts(ctx, tx, "kit_thing", draftOnly)
		if err != nil || ok {
			t.Errorf("UsedOutsideDrafts(draft, master) = %+v, %v, %v; want ok false, nil error", ref, ok, err)
		}
		return nil
	})
}

func TestKit_WriteChecksRight(t *testing.T) {
	k := newKit(t)
	ctx := context.Background()
	// Priya may read and write Party, but not delete it.
	k.auth.Allow(k.priya.ID, party.DocType, "read", "write")
	ran := false
	err := k.env.Write(ctx, k.priya, party, auth.Right("delete"), func(*db.Tx) error {
		ran = true
		return nil
	})
	if !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("Env.Write(priya, delete) = %v; want auth.ErrForbidden", err)
	}
	if ran {
		t.Error("Env.Write ran fn although the right was denied")
	}
}

func TestKit_DeleteMapsFKError(t *testing.T) {
	k := newKit(t)
	ctx := context.Background()
	id := k.insertThing(t, "V-SHARMA", "Sharma Steel Traders")
	k.write(t, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `INSERT INTO kit_child (thing_id) VALUES (?)`, id)
		return err
	})
	const msg = "Cannot delete Party V-SHARMA: it is used in another record. Disable it instead."
	err := k.db.Write(ctx, func(tx *db.Tx) error {
		return k.env.DeleteTx(ctx, tx, k.kavya, party, id, 1)
	})
	var refused *mstkit.DeleteRefusedError
	if !errors.As(err, &refused) || err.Error() != msg {
		t.Fatalf("DeleteTx = %v; want *DeleteRefusedError %q", err, msg)
	}
	if _, _, ok := k.thing(t, id); !ok {
		t.Error("row with a child was deleted")
	}
}

func TestKit_CodeChangeNeedsRole(t *testing.T) {
	k := newKit(t)
	ctx := context.Background()
	const msg = "Only a Masters Manager can change a code."
	if err := k.env.CheckCodeChange(ctx, k.priya, "V-SHARMA", "V-SHARMA-2"); err == nil || err.Error() != msg {
		t.Errorf("Priya: CheckCodeChange = %v; want %q", err, msg)
	}
	if err := k.env.CheckCodeChange(ctx, k.kavya, "V-SHARMA", "V-SHARMA-2"); err != nil {
		t.Errorf("Kavya: CheckCodeChange = %v; want nil", err)
	}
	if err := k.env.CheckCodeChange(ctx, k.priya, "V-SHARMA", "V-SHARMA"); err != nil {
		t.Errorf("Priya, same code: CheckCodeChange = %v; want nil", err)
	}
	if err := k.env.CheckCodeChange(ctx, k.priya, "V-SHARMA", "v-sharma"); err != nil {
		t.Errorf("Priya, same code different case: CheckCodeChange = %v; want nil", err)
	}
}

// TestKit_ErrorsAreHTTPErrors has no case ID (CD-44, CD-54).
func TestKit_ErrorsAreHTTPErrors(t *testing.T) {
	var es mstkit.Errors
	if err := es.Err(); err != nil {
		t.Errorf("empty Errors.Err() = %v; want nil", err)
	}
	es.Add("code", "Code is required. Enter a short code such as SHREE.")
	es.Add("name", "Name is required.")
	err := es.Err()
	var verr *mstkit.ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("Errors.Err() = %T %v; want *mstkit.ValidationError", err, err)
	}
	if want := "Code is required. Enter a short code such as SHREE.; Name is required."; err.Error() != want {
		t.Errorf("Error() = %q; want %q", err.Error(), want)
	}
	var he doctype.HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("ValidationError is not a doctype.HTTPError")
	}
	if he.Status() != 422 {
		t.Errorf("ValidationError Status() = %d; want 422", he.Status())
	}
	wantFields := []doctype.FieldError{
		{Field: "code", Message: "Code is required. Enter a short code such as SHREE."},
		{Field: "name", Message: "Name is required."},
	}
	if got := he.FieldErrors(); !reflect.DeepEqual(got, wantFields) {
		t.Errorf("FieldErrors() = %+v; want %+v", got, wantFields)
	}

	var lines mstkit.Errors
	lines.AddLine(2, "uom", "Line 2 needs a unit.")
	wantLine := []doctype.FieldError{{Field: "uom", Line: 2, Message: "Line 2 needs a unit."}}
	if !errors.As(lines.Err(), &he) || !reflect.DeepEqual(he.FieldErrors(), wantLine) {
		t.Errorf("AddLine: Err() = %v; want field errors %+v", lines.Err(), wantLine)
	}

	direct := &mstkit.ValidationError{Fields: wantFields}
	if !errors.As(error(direct), &he) || he.Status() != 422 || !reflect.DeepEqual(he.FieldErrors(), wantFields) {
		t.Errorf("&ValidationError{Fields} is not a 422 HTTPError with its fields")
	}

	const refusal = "Cannot delete Party V-SHARMA: it is used in PO/26-27/00001. Disable it instead."
	var rerr error = &mstkit.DeleteRefusedError{Message: refusal}
	if rerr.Error() != refusal {
		t.Errorf("DeleteRefusedError.Error() = %q; want %q", rerr.Error(), refusal)
	}
	if !errors.As(rerr, &he) {
		t.Fatalf("DeleteRefusedError is not a doctype.HTTPError")
	}
	if he.Status() != 422 || he.FieldErrors() != nil {
		t.Errorf("DeleteRefusedError Status()=%d FieldErrors()=%v; want 422, nil", he.Status(), he.FieldErrors())
	}

	// A missing record: StandardLookup reports ok=false; SetDisabledTx and
	// DeleteTx return doctype.ErrNotFound (CD-54).
	k := newKit(t)
	ctx := context.Background()
	id := k.insertThing(t, "V-SHARMA", "Sharma Steel Traders")
	const missing = int64(999)
	k.write(t, func(tx *db.Tx) error {
		lookup := mstkit.StandardLookup(party)
		info, ok, err := lookup.Lookup(ctx, tx, missing)
		if err != nil || ok {
			t.Errorf("Lookup(missing) = %+v, %v, %v; want ok false, nil", info, ok, err)
		}
		info, ok, err = lookup.Lookup(ctx, tx, id)
		want := doctype.MasterInfo{Code: "V-SHARMA", Name: "Sharma Steel Traders"}
		if err != nil || !ok || info != want {
			t.Errorf("Lookup(%d) = %+v, %v, %v; want %+v, true, nil", id, info, ok, err, want)
		}
		return nil
	})
	err = k.db.Write(ctx, func(tx *db.Tx) error {
		return k.env.SetDisabledTx(ctx, tx, k.kavya, party, missing, 1, true)
	})
	if !errors.Is(err, doctype.ErrNotFound) {
		t.Errorf("SetDisabledTx(missing) = %v; want doctype.ErrNotFound", err)
	}
	err = k.db.Write(ctx, func(tx *db.Tx) error {
		return k.env.DeleteTx(ctx, tx, k.kavya, party, missing, 1)
	})
	if !errors.Is(err, doctype.ErrNotFound) {
		t.Errorf("DeleteTx(missing) = %v; want doctype.ErrNotFound", err)
	}
}
