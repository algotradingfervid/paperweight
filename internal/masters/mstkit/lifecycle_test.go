package mstkit_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
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
	// csvio.IsImport sub-case (CD-60) added when NUM-M2 lands.
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
	k.write(t, func(tx *db.Tx) error {
		return k.env.SetDisabledTx(ctx, tx, k.kavya, party, id, 2, false)
	})
	if disabled, version, _ := k.thing(t, id); disabled != 0 || version != 3 {
		t.Errorf("after enable: disabled=%d version=%d; want 0, 3", disabled, version)
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
