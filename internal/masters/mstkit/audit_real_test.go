package mstkit_test

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// audited is a test master over kit_audit_thing, registered so msttest.New
// grants Kavya its rights through the real AUTH matrix.
var audited = mstkit.Kind{DocType: "mstkit.AuditThing", Label: "Audit Thing", Table: "kit_audit_thing", HasCode: true}

var registerAudited sync.Once // doctype.Register panics on a duplicate (-count > 1)

// kitAuditThingDDL has the master columns, ids never reused (CD-156).
const kitAuditThingDDL = `CREATE TABLE kit_audit_thing (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	code TEXT NOT NULL UNIQUE CHECK (code = upper(code)),
	name TEXT NOT NULL,
	disabled INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0,1)),
	version INTEGER NOT NULL DEFAULT 1,
	created_at TEXT NOT NULL,
	created_by INTEGER,
	updated_at TEXT NOT NULL,
	updated_by INTEGER
)`

// TestKit_AuditWritesThroughRealStore has no case ID (MST Task 1c): every
// entry mstkit writes must pass the production audit store's checks
// (audit.Default), which auditfake doesn't make.
func TestKit_AuditWritesThroughRealStore(t *testing.T) {
	registerAudited.Do(func() {
		mstkit.RegisterMaster(doctype.DocType{
			Name:        audited.DocType,
			Label:       audited.Label,
			PluralLabel: "Audit Things",
			Table:       audited.Table,
			Fields: []doctype.Field{
				{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, Required: true},
				{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, Required: true},
				{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check},
			},
		}, mstkit.StandardLookup(audited))
	})
	env := msttest.New(t)
	ctx := context.Background()
	if env.Kit.Auditor != audit.Default {
		t.Fatalf("Kit.Auditor = %T; want audit.Default", env.Kit.Auditor)
	}
	ts := types.TimestampOf(env.Clock.Now()).String()
	if err := env.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, kitAuditThingDDL)
		return err
	}); err != nil {
		t.Fatalf("create kit_audit_thing: %v", err)
	}
	insert := func(code, name string) int64 {
		t.Helper()
		var id int64
		if err := env.DB.Write(ctx, func(tx *db.Tx) error {
			res, err := tx.SQL().ExecContext(ctx,
				`INSERT INTO kit_audit_thing (code, name, created_at, updated_at) VALUES (?, ?, ?, ?)`,
				code, name, ts, ts)
			if err != nil {
				return err
			}
			id, err = res.LastInsertId()
			return err
		}); err != nil {
			t.Fatalf("insert %s: %v", code, err)
		}
		return id
	}
	saved := insert("V-SHARMA", "Sharma Steel")
	disabled := insert("C-TATA", "Tata Projects")
	deleted := insert("K-KUMAR", "Kumar Hardware")

	// create, then a save that changes one field (bookkeeping columns differ too).
	if err := env.Kit.Write(ctx, env.Kavya, audited, auth.RightCreate, func(tx *db.Tx) error {
		return env.Kit.Audit(ctx, tx, env.Kavya, audited, saved, "create", nil,
			map[string]string{"code": "V-SHARMA", "name": "Sharma Steel"})
	}); err != nil {
		t.Errorf("Audit create through audit.Default = %v; want nil", err)
	}
	if err := env.Kit.Write(ctx, env.Kavya, audited, auth.RightWrite, func(tx *db.Tx) error {
		if _, err := tx.SQL().ExecContext(ctx,
			`UPDATE kit_audit_thing SET name = ?, version = 2, updated_at = ?, updated_by = ? WHERE id = ?`,
			"Sharma Steel Traders", ts, env.Kavya.ID, saved); err != nil {
			return err
		}
		return env.Kit.Audit(ctx, tx, env.Kavya, audited, saved, "save",
			map[string]string{"code": "V-SHARMA", "name": "Sharma Steel", "version": "1", "updated_at": ts, "updated_by": ""},
			map[string]string{"code": "V-SHARMA", "name": "Sharma Steel Traders", "version": "2", "updated_at": ts, "updated_by": "6"})
	}); err != nil {
		t.Errorf("Audit save through audit.Default = %v; want nil", err)
	}
	if err := env.Kit.Write(ctx, env.Kavya, audited, auth.RightWrite, func(tx *db.Tx) error {
		return env.Kit.SetDisabledTx(ctx, tx, env.Kavya, audited, disabled, 1, true)
	}); err != nil {
		t.Errorf("SetDisabledTx through audit.Default = %v; want nil", err)
	}
	if err := env.Kit.Write(ctx, env.Kavya, audited, auth.RightDelete, func(tx *db.Tx) error {
		return env.Kit.DeleteTx(ctx, tx, env.Kavya, audited, deleted, 1)
	}); err != nil {
		t.Errorf("DeleteTx through audit.Default = %v; want nil", err)
	}

	set := func(field, before, after string) audit.Change {
		return audit.Change{Field: field, Before: before, After: after, Op: audit.OpSet}
	}
	type want struct {
		action  string
		changes []audit.Change
	}
	for _, c := range []struct {
		row  string
		id   int64
		want []want
	}{
		{"V-SHARMA (create, save)", saved, []want{
			{"create", []audit.Change{set("code", "", "V-SHARMA"), set("name", "", "Sharma Steel")}},
			{"save", []audit.Change{set("name", "Sharma Steel", "Sharma Steel Traders")}},
		}},
		{"C-TATA (disable)", disabled, []want{
			{"disable", []audit.Change{set("disabled", "false", "true")}},
		}},
		{"K-KUMAR (delete)", deleted, []want{
			{"delete", []audit.Change{set("code", "K-KUMAR", ""), set("disabled", "false", ""), set("name", "Kumar Hardware", "")}},
		}},
	} {
		var got []audit.Entry
		if err := env.DB.Read(ctx, func(tx *db.Tx) error {
			var err error
			got, err = audit.ForDoc(ctx, tx, audited.DocType, c.id)
			return err
		}); err != nil {
			t.Fatalf("audit.ForDoc(%s): %v", c.row, err)
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: audit_log entries = %+v; want %d", c.row, got, len(c.want))
			continue
		}
		for i, w := range c.want {
			e := got[i]
			if e.Action != w.action || e.By != env.Kavya.ID || !e.At.Equal(env.Clock.Now()) {
				t.Errorf("%s entry %d = %+v; want Action %s, By %d, At %v", c.row, i, e, w.action, env.Kavya.ID, env.Clock.Now())
			}
			if !reflect.DeepEqual(e.Changes, w.changes) {
				t.Errorf("%s %s changes = %+v; want %+v", c.row, w.action, e.Changes, w.changes)
			}
		}
	}
}
