package modkit_test

import (
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// AddTx with an author id AUTH doesn't know is a caller bug: a plain internal
// error, not the 404 or a 422, and nothing saved.
func TestComments_AddTxUnknownAuthor(t *testing.T) {
	e := newCommentsEnv(t)
	ctx := t.Context()
	metro := e.order(t, e.Ravi, "PRJ-METRO")

	err := e.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := e.C.AddTx(ctx, tx, types.UserID(999999), fixture.Order, metro, "Who wrote this?")
		return err
	})
	if err == nil {
		t.Fatal("AddTx by an unknown user = ok, want an error")
	}
	if errors.Is(err, auth.ErrNotFound) {
		t.Errorf("AddTx by an unknown user = %v, want a plain error, not auth.ErrNotFound", err)
	}
	var se statusErr
	if errors.As(err, &se) && se.Status() == 422 {
		t.Errorf("AddTx by an unknown user = %v, want a plain error, not a 422", err)
	}
	if n := e.rows(t); n != 0 {
		t.Errorf("module_comment rows = %d, want 0", n)
	}
}
