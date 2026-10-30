package auth_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// CD-216(7): ValueAllowedTx reads in the caller's transaction and takes no
// second connection. The answers themselves are authtest's contract row.

// vaChecks collects what ValueAllowedTx got wrong. The checks run inside
// NoSecondConn's goroutine, so they report after it has returned.
type vaChecks struct {
	a   auth.Service
	bad []string
}

func (c *vaChecks) want(ctx context.Context, tx *db.Tx, p auth.Principal, dim auth.Dimension, id int64, want bool) {
	got, err := c.a.ValueAllowedTx(ctx, tx, p, string(dim), id)
	if err != nil || got != want {
		c.bad = append(c.bad, fmt.Sprintf("ValueAllowedTx(%s, %s, %d) = (%v, %v), want (%v, nil)", p.Name, dim, id, got, err, want))
	}
}

func (c *vaChecks) report(t *testing.T) {
	t.Helper()
	for _, msg := range c.bad {
		t.Error(msg)
	}
}

// A restriction added in the open write is seen, on one connection, and gone
// again after the rollback.
func TestValueAllowedTx_SeesRestrictionAddedInCallersTx(t *testing.T) {
	w, p := ctxWorld(t)
	metro, techpark := w.Projects["PRJ-METRO"], w.Projects["PRJ-TECHPARK"]
	c := &vaChecks{a: w.Auth}

	err := testkit.NoSecondConn(t, w.DB, func(ctx context.Context) error {
		return w.DB.Write(ctx, func(tx *db.Tx) error {
			c.want(ctx, tx, p, auth.DimProject, techpark, true)
			if _, err := tx.SQL().ExecContext(ctx,
				`INSERT INTO user_restrictions (user_id, dimension, value_id) VALUES (?, ?, ?)`, int64(p.ID), string(auth.DimProject), metro); err != nil {
				return err
			}
			c.want(ctx, tx, p, auth.DimProject, metro, true)
			c.want(ctx, tx, p, auth.DimProject, techpark, false)
			return errCtxRollback
		})
	})
	if !errors.Is(err, errCtxRollback) {
		t.Fatalf("Write: %v, want the rollback", err)
	}
	if err := w.DB.Read(t.Context(), func(tx *db.Tx) error {
		c.want(t.Context(), tx, p, auth.DimProject, techpark, true)
		return nil
	}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	c.report(t)
}

// A restriction removed in the open write is gone too, so the answer can't come
// from a copy of the list read earlier.
func TestValueAllowedTx_SeesRestrictionRemovedInCallersTx(t *testing.T) {
	w := authtest.NewWorld(t)
	priya := w.P(t, "priya@vardhan.test")
	techpark := w.Projects["PRJ-TECHPARK"]
	c := &vaChecks{a: w.Auth}

	err := testkit.NoSecondConn(t, w.DB, func(ctx context.Context) error {
		return w.DB.Write(ctx, func(tx *db.Tx) error {
			c.want(ctx, tx, priya, auth.DimProject, techpark, false)
			if _, err := tx.SQL().ExecContext(ctx, `DELETE FROM user_restrictions WHERE user_id = ?`, int64(priya.ID)); err != nil {
				return err
			}
			c.want(ctx, tx, priya, auth.DimProject, techpark, true)
			return errCtxRollback
		})
	})
	if !errors.Is(err, errCtxRollback) {
		t.Fatalf("Write: %v, want the rollback", err)
	}
	c.report(t)
}

// Every answer on a pool of one, in a read and in a write transaction: a
// restricted user, an unrestricted dimension and the Administrator.
func TestValueAllowedTx_TakesNoSecondConn(t *testing.T) {
	w := authtest.NewWorld(t)
	priya, admin := w.P(t, "priya@vardhan.test"), w.P(t, "admin@vardhan.test")
	metro, techpark := w.Projects["PRJ-METRO"], w.Projects["PRJ-TECHPARK"]
	const anySite = 1
	for _, tc := range []struct {
		name string
		run  func(context.Context, func(*db.Tx) error) error
	}{{"read", w.DB.Read}, {"write", w.DB.Write}} {
		t.Run(tc.name, func(t *testing.T) {
			c := &vaChecks{a: w.Auth}
			err := testkit.NoSecondConn(t, w.DB, func(ctx context.Context) error {
				return tc.run(ctx, func(tx *db.Tx) error {
					c.want(ctx, tx, priya, auth.DimProject, metro, true)
					c.want(ctx, tx, priya, auth.DimProject, techpark, false)
					c.want(ctx, tx, priya, auth.DimSite, anySite, true)
					c.want(ctx, tx, admin, auth.DimProject, techpark, true)
					return nil
				})
			})
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			c.report(t)
		})
	}
}
