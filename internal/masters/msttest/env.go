// Package msttest gives master tests a testkit database with the real
// kernel services (AUTH, DOC, naming, the production audit writer) and the
// canonical principals.
package msttest

import (
	"context"
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// Env is a migrated, seeded database and a mstkit.Env over it.
type Env struct {
	DB    *db.DB
	Kit   mstkit.Env
	Clock *clock.Fixed

	Admin, Kavya, Priya, Ravi auth.Principal
}

// engineRef lets AUTH use the engine as its Refs and Info before the engine,
// which needs AUTH, exists.
type engineRef struct{ doc.Engine }

// New returns an Env: testkit.NewDB and Seed, the fixture users and roles,
// AUTH's default rights, then through the matrix every applicable right on
// every registered master for Masters Manager (Kavya) and read only for
// Purchase User (Priya). Register test masters before calling it. The
// fixtures' user restrictions are applied through AUTH, as in AUTH's test
// world.
//
// Rights are granted for tests (Kavya every applicable right, Priya read;
// plan Task 1), not production DefaultRights (M1), which
// TestKit_RegisterMasterShape pins.
func New(t testing.TB) *Env {
	t.Helper()
	ctx := context.Background()
	d := testkit.NewDB(t)
	fx := testkit.Seed(t, d)
	clk := testkit.Clock()
	insertUsers(t, d, fx)

	refs := &engineRef{}
	a := auth.New(auth.Deps{DB: d, Clock: clk, Directory: testkit.Directory(fx), Audit: audit.Default, Refs: refs, Info: refs})
	refs.Engine = doc.NewEngine(doc.Deps{
		DB: d, Clock: clk, Auth: a, Naming: naming.New(d, clk, a, audit.Default), Audit: audit.Default,
	})

	e := &Env{
		DB:    d,
		Kit:   mstkit.Env{DB: d, Auth: a, Engine: refs.Engine, Auditor: audit.Default, Clock: clk},
		Clock: clk,
		Admin: testkit.Principal(t, "admin@vardhan.test"),
		Kavya: testkit.Principal(t, "kavya@vardhan.test"),
		Priya: testkit.Principal(t, "priya@vardhan.test"),
		Ravi:  testkit.Principal(t, "ravi@vardhan.test"),
	}
	if err := a.SeedDefaults(ctx, auth.SeedOptions{LinkedMasterRead: true}); err != nil {
		t.Fatalf("msttest.New: SeedDefaults: %v", err)
	}
	for _, dt := range doc.All() {
		if !dt.IsMaster() {
			continue
		}
		manager := map[auth.Right]auth.Scope{}
		for _, r := range auth.AllRights {
			if auth.Applies(dt, r) {
				manager[r] = auth.ScopeAll
			}
		}
		for role, rights := range map[string]map[auth.Right]auth.Scope{
			"Masters Manager": manager,
			"Purchase User":   {auth.RightRead: auth.ScopeAll},
		} {
			if err := a.SetRights(ctx, e.Admin, role, dt.Name, rights); err != nil {
				t.Fatalf("msttest.New: SetRights(%s, %s): %v", role, dt.Name, err)
			}
		}
	}
	restrict(t, a, e.Admin, fx)
	return e
}

// restrict applies the fixtures' user restrictions with ids from the fixtures.
func restrict(t testing.TB, a *auth.Auth, admin auth.Principal, fx testkit.Fixtures) {
	t.Helper()
	ctx := context.Background()
	users, projects, sites := map[string]auth.UserID{}, map[string]int64{}, map[string]int64{}
	for _, u := range fx.Users {
		users[u.Email] = u.ID
	}
	for _, p := range fx.Projects {
		projects[p.Code] = p.ID
	}
	for _, s := range fx.Sites {
		sites[s.Code] = s.ID
	}
	resolve := func(m map[string]int64, codes []string) []int64 {
		var out []int64
		for _, c := range codes {
			id, ok := m[c]
			if !ok {
				t.Fatalf("msttest.New: restriction code %q not in the fixtures", c)
			}
			out = append(out, id)
		}
		return out
	}
	for _, r := range fx.Restrictions {
		u, ok := users[r.Email]
		if !ok {
			t.Fatalf("msttest.New: restriction for unknown user %q", r.Email)
		}
		for dim, ids := range map[auth.Dimension][]int64{
			auth.DimProject: resolve(projects, r.ProjectCodes),
			auth.DimSite:    resolve(sites, r.SiteCodes),
		} {
			if len(ids) == 0 {
				continue
			}
			if err := a.ChangeRestrictions(ctx, admin, u, dim, ids, nil, nil); err != nil {
				t.Fatalf("msttest.New: restrict %s on %s: %v", r.Email, dim, err)
			}
		}
	}
}

// insertUsers adds the fixture users with their ids and roles. AUTH has no
// API that keeps a given id, so this is SQL, as in AUTH's own test world.
func insertUsers(t testing.TB, d *db.DB, fx testkit.Fixtures) {
	t.Helper()
	ctx := context.Background()
	at := auth.FormatTime(testkit.Clock().Now())
	if err := d.Write(ctx, func(tx *db.Tx) error {
		q := tx.SQL()
		for _, u := range fx.Users {
			if _, err := q.ExecContext(ctx,
				`INSERT INTO users (id, email, name, enabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
				int64(u.ID), u.Email, u.Name, !u.Disabled, at, at); err != nil {
				return err
			}
			for _, role := range u.Roles {
				res, err := q.ExecContext(ctx,
					`INSERT INTO user_roles (user_id, role_id) SELECT ?, id FROM roles WHERE name_key = lower(?)`,
					int64(u.ID), role)
				if err != nil {
					return err
				}
				n, err := res.RowsAffected()
				if err != nil {
					return err
				}
				if n != 1 {
					return fmt.Errorf("user %s: no role %q", u.Email, role)
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("msttest.New: users: %v", err)
	}
}
