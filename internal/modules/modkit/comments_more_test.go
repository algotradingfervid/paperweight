package modkit_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// countInfo counts Info calls.
type countInfo struct {
	doctype.InfoSource
	n atomic.Int32
}

func (c *countInfo) Info(ctx context.Context, tx *db.Tx, dt string, id int64) (doctype.Info, error) {
	c.n.Add(1)
	return c.InfoSource.Info(ctx, tx, dt, id)
}

// CD-213: RecentComments reads AUTH and the records in its own transaction.
func TestNoSecondConn_RecentComments(t *testing.T) {
	e := newCommentsEnv(t)
	id := e.order(t, e.Priya, "PRJ-METRO")
	e.add(t, e.Ravi, fixture.Order, id, "Please check the rate.")
	err := testkit.NoSecondConn(t, e.DB, func(ctx context.Context) error {
		got, err := e.C.RecentComments(ctx, e.Priya)
		if err != nil {
			return err
		}
		if len(got) != 1 || got[0].Body != "Please check the rate." {
			return errors.New("RecentComments: want Ravi's one comment")
		}
		return nil
	})
	if err != nil {
		t.Errorf("RecentComments as Priya on the pool of one: %v, want ok", err)
	}
}

// RecentComments resolves and checks each record once, however many of its
// comments are in the window.
func TestComments_RecentChecksEachRecordOnce(t *testing.T) {
	h := fixturetest.New(t)
	info := doc.NewEngine(doc.Deps{DB: h.DB, Clock: h.Clock, Naming: h.Naming, Numbers: fixturetest.Numbers(), Files: h.Files})
	au := auth.New(auth.Deps{
		DB: h.DB, Clock: h.Clock, Directory: testkit.Directory(h.F), Audit: auditfake.New(), Refs: &authtest.Refs{}, Info: info,
	})
	if err := au.SeedDefaults(t.Context(), auth.SeedOptions{}); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}
	ck := testkit.Clock()
	e := &cmEnv{H: h, Clock: ck, C: modkit.NewComments(modkit.CommentsDeps{DB: h.DB, Auth: au, Info: info, Clock: ck})}
	a, b := e.order(t, e.Priya, "PRJ-METRO"), e.order(t, e.Priya, "PRJ-METRO")
	for _, body := range []string{"One.", "Two.", "Three."} {
		e.add(t, e.Ravi, fixture.Order, a, body)
	}
	e.add(t, e.Ravi, fixture.Order, b, "Four.")
	ci := &countInfo{InfoSource: info}
	c := modkit.NewComments(modkit.CommentsDeps{DB: h.DB, Auth: au, Info: ci, Clock: ck})
	got, err := c.RecentComments(t.Context(), e.Priya)
	if err != nil {
		t.Fatalf("RecentComments: %v", err)
	}
	eqStrings(t, "Priya's recent comments", bodies(got), []string{"Four.", "Three.", "Two.", "One."})
	if n := ci.n.Load(); n != 2 {
		t.Errorf("Info calls = %d, want 2 (one per record)", n)
	}
}
