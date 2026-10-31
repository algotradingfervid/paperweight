package modkit_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// M0-14b: AddTx is the comment write for kernel effect hooks, which hold a
// write transaction and have checked rights already. It keeps the
// registered-DocType check, the trim and §22's two messages, writes through
// the caller's tx and checks no read.

const nameDeepak = "Deepak Nair"

// addTx runs AddTx inside its own db.Write and returns its result.
func (e *cmEnv) addTx(ctx context.Context, by auth.Principal, dt string, id int64, body string) (modkit.Comment, error) {
	var out modkit.Comment
	err := e.DB.Write(ctx, func(tx *db.Tx) error {
		var err error
		out, err = e.C.AddTx(ctx, tx, by.ID, dt, id, body)
		return err
	})
	return out, err
}

func TestComments_AddTx(t *testing.T) {
	e := newCommentsEnv(t)
	ctx := t.Context()
	metro := e.order(t, e.Ravi, "PRJ-METRO")
	techpark := e.order(t, e.Ravi, "PRJ-TECHPARK") // hidden from Priya by her restriction
	claim := e.NewClaim(t, e.Priya, "EMP-004", fixturetest.CL{Type: "Travel", Amount: "1500.00"}).ID

	// No read check: Deepak has no read on orders, Priya can't see the
	// PRJ-TECHPARK order, yet both are the author through AddTx. The name
	// comes from AUTH, the time from the clock, the body is trimmed.
	t0 := e.Clock.Now()
	deepak, err := e.addTx(ctx, e.Deepak, fixture.Order, metro, "  Goods handed over at the gate.\n")
	if err != nil {
		t.Fatalf("AddTx as Deepak (no read on orders): %v, want ok", err)
	}
	if deepak.ID <= 0 || deepak.DocType != fixture.Order || deepak.RecordID != metro ||
		deepak.Body != "Goods handed over at the gate." || deepak.CreatedBy != e.Deepak.ID ||
		deepak.CreatedByName != nameDeepak || !deepak.CreatedAt.Equal(t0) {
		t.Errorf("AddTx = %+v, want id > 0, %s %d, trimmed body, by %d %q at %v", deepak, fixture.Order, metro, e.Deepak.ID, nameDeepak, t0)
	}
	e.Clock.Advance(time.Minute)
	priya, err := e.addTx(ctx, e.Priya, fixture.Order, techpark, "Handed over to Ravi.")
	if err != nil {
		t.Fatalf("AddTx as Priya on an order hidden from her: %v, want ok", err)
	}
	if priya.ID <= deepak.ID || priya.RecordID != techpark || priya.CreatedBy != e.Priya.ID ||
		priya.CreatedByName != namePriya || !priya.CreatedAt.Equal(t0.Add(time.Minute)) {
		t.Errorf("AddTx on techpark = %+v, want a later id by %d %q at %v", priya, e.Priya.ID, namePriya, t0.Add(time.Minute))
	}

	// Both were committed with their authors.
	for _, c := range []struct {
		id   int64
		want modkit.Comment
	}{{metro, deepak}, {techpark, priya}} {
		got, err := e.C.ListComments(ctx, e.Ravi, fixture.Order, c.id)
		if err != nil {
			t.Fatalf("ListComments(%d) as Ravi: %v", c.id, err)
		}
		if len(got) != 1 || got[0].ID != c.want.ID || got[0].Body != c.want.Body ||
			got[0].CreatedBy != c.want.CreatedBy || got[0].CreatedByName != c.want.CreatedByName {
			t.Errorf("ListComments(%d) = %+v, want the one comment %+v", c.id, got, c.want)
		}
	}
	if n := e.rows(t); n != 2 {
		t.Fatalf("module_comment rows = %d, want 2", n)
	}

	// A DocType that didn't opt in answers AddComment's 404, adding nothing.
	for _, c := range []struct {
		what string
		dt   string
		id   int64
	}{
		{"claim (not registered for comments)", fixture.Claim, claim},
		{"unknown DocType", "nope.Thing", 1},
	} {
		_, err := e.addTx(ctx, e.Admin, c.dt, c.id, "Is this still needed?")
		wantNotFound(t, "AddTx: "+c.what, err)
	}

	// §22's two messages, length in characters after trimming.
	thousand := strings.Repeat("é", 1000) // 2,000 bytes
	for _, c := range []struct{ what, body, msg string }{
		{"empty", "", msgCommentEmpty},
		{"only spaces", " \n\t  ", msgCommentEmpty},
		{"1,001 characters", thousand + "x", msgCommentTooLong},
		{"1,001 characters after trimming", "  " + thousand + "x  ", msgCommentTooLong},
	} {
		_, err := e.addTx(ctx, e.Ravi, fixture.Order, metro, c.body)
		wantInvalid(t, "AddTx "+c.what, err, c.msg)
	}
	if n := e.rows(t); n != 2 {
		t.Errorf("module_comment rows after refusals = %d, want 2", n)
	}
	exact, err := e.addTx(ctx, e.Ravi, fixture.Order, metro, "  "+thousand+"\n")
	if err != nil || exact.Body != thousand {
		t.Errorf("AddTx padded 1,000 characters = %d characters, %v; want the trimmed 1,000, ok", len([]rune(exact.Body)), err)
	}
	if n := e.rows(t); n != 3 {
		t.Fatalf("module_comment rows = %d, want 3", n)
	}

	// The comment is part of the caller's transaction: an error the caller
	// returns afterwards rolls it back.
	errLater := errors.New("effect hook failed after the comment")
	err = e.DB.Write(ctx, func(tx *db.Tx) error {
		if _, err := e.C.AddTx(ctx, tx, e.Ravi.ID, fixture.Order, metro, "Rolled back."); err != nil {
			t.Errorf("AddTx before the caller's failure: %v, want ok", err)
		}
		return errLater
	})
	if !errors.Is(err, errLater) {
		t.Errorf("Write = %v, want the caller's error", err)
	}
	if n := e.rows(t); n != 3 {
		t.Errorf("module_comment rows after the caller's rollback = %d, want 3", n)
	}

	// One connection: AddTx reads AUTH and writes through the caller's tx,
	// never its own transaction or a second connection.
	var pooled modkit.Comment
	err = testkit.NoSecondConn(t, e.DB, func(ctx context.Context) error {
		return e.DB.Write(ctx, func(tx *db.Tx) error {
			var err error
			pooled, err = e.C.AddTx(ctx, tx, e.Deepak.ID, fixture.Order, metro, "On the pool of one.")
			return err
		})
	})
	if err != nil {
		t.Fatalf("AddTx inside db.Write on the pool of one: %v, want ok", err)
	}
	if pooled.CreatedByName != nameDeepak {
		t.Errorf("AddTx on the pool of one: name %q, want %q", pooled.CreatedByName, nameDeepak)
	}
	// The refusals take no second connection either.
	err = testkit.NoSecondConn(t, e.DB, func(ctx context.Context) error {
		_, err := e.addTx(ctx, e.Admin, fixture.Claim, claim, "Not registered.")
		return err
	})
	wantNotFound(t, "AddTx on a claim on the pool of one", err)
	if n := e.rows(t); n != 4 {
		t.Errorf("module_comment rows = %d, want 4", n)
	}
}

// AddComment still checks read before writing (unchanged by AddTx): a hidden
// record and no read grant answer the 404 and add nothing, while a reader's
// comment carries the author's name.
func TestComments_AddCommentStillChecksRead(t *testing.T) {
	e := newCommentsEnv(t)
	ctx := t.Context()
	metro := e.order(t, e.Ravi, "PRJ-METRO")
	techpark := e.order(t, e.Ravi, "PRJ-TECHPARK")

	_, err := e.C.AddComment(ctx, e.Priya, fixture.Order, techpark, "Hidden from me.")
	wantNotFound(t, "AddComment as Priya on PRJ-TECHPARK", err)
	_, err = e.C.AddComment(ctx, e.Deepak, fixture.Order, metro, "No read on orders.")
	wantNotFound(t, "AddComment as Deepak", err)
	if n := e.rows(t); n != 0 {
		t.Errorf("module_comment rows after refusals = %d, want 0", n)
	}
	got := e.add(t, e.Priya, fixture.Order, metro, " Rate checked. ")
	if got.Body != "Rate checked." || got.CreatedBy != e.Priya.ID || got.CreatedByName != namePriya {
		t.Errorf("AddComment = %+v, want trimmed body by %d %q", got, e.Priya.ID, namePriya)
	}
}
