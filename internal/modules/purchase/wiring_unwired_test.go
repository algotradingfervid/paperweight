package purchase_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// hookCall runs one hook method and returns its error.
type hookCall func(ctx context.Context, h doctype.Hooks, tx *db.Tx, id int64) error

var unwiredCalls = map[string]hookCall{
	"BeforeSubmit": func(ctx context.Context, h doctype.Hooks, tx *db.Tx, id int64) error {
		return h.BeforeSubmit(ctx, tx, id)
	},
	"ApplyEffects": func(ctx context.Context, h doctype.Hooks, tx *db.Tx, id int64) error {
		return h.ApplyEffects(ctx, tx, id)
	},
	"ReverseEffects": func(ctx context.Context, h doctype.Hooks, tx *db.Tx, id int64) error {
		return h.ReverseEffects(ctx, tx, id)
	},
	"Amount": func(ctx context.Context, h doctype.Hooks, tx *db.Tx, id int64) error {
		_, err := h.Amount(ctx, tx, id)
		return err
	},
	"Date": func(ctx context.Context, h doctype.Hooks, tx *db.Tx, id int64) error {
		_, err := h.Date(ctx, tx, id)
		return err
	},
	"OnClose": func(ctx context.Context, h doctype.Hooks, tx *db.Tx, id int64) error {
		c, ok := h.(doctype.Closer)
		if !ok {
			return fmt.Errorf("%T is not a doctype.Closer", h)
		}
		return c.OnClose(ctx, tx, id)
	},
}

// MOD-PO P1 "Service shape" and Do 2 (CD-284; P1 review finding): every hook
// method of both DocTypes, run on a database handle nothing was wired for,
// fails with the wiring's plain internal error, even though the rows exist on
// the file. Validate returns only field errors, so it panics with the wiring
// error instead (the engine turns that into its internal error). Deleting any
// one wiring.Get or mustWired call fails a row here.
func TestPurchase_HooksUnwired(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	pr := purchasetest.NewRequest(t, env, purchasetest.StopAtDraft())

	unwired, err := db.Open(testkit.DBPath(t))
	if err != nil {
		t.Fatalf("open a second handle: %v", err)
	}
	t.Cleanup(func() { _ = unwired.Close() })

	cases := []struct {
		dt      string
		id      int64
		methods []string
	}{
		{purchase.DocType, po.ID, []string{"Validate", "BeforeSubmit", "Amount", "Date", "ApplyEffects", "ReverseEffects", "OnClose"}},
		{purchase.RequestDocType, pr.ID, []string{"Validate", "BeforeSubmit", "Amount", "Date", "ApplyEffects", "ReverseEffects"}},
	}
	for _, c := range cases {
		hooks := lookup(t, c.dt).Hooks
		for _, m := range c.methods {
			t.Run(c.dt+"/"+m, func(t *testing.T) {
				if err := unwired.Write(t.Context(), func(tx *db.Tx) error {
					_, wiringErr := purchase.DepsFor(tx)
					if wiringErr == nil {
						t.Fatal("Get(tx) on the unwired handle: error = nil, want the wiring error")
					}
					if m == "Validate" {
						got := validatePanic(t.Context(), hooks, tx, c.id)
						if got == nil || got.Error() != wiringErr.Error() {
							t.Errorf("%s Validate on an unwired database panicked with %v, want the wiring error %q", c.dt, got, wiringErr)
						}
						return nil
					}
					err := unwiredCalls[m](t.Context(), hooks, tx, c.id)
					var he doctype.HTTPError
					switch {
					case err == nil:
						t.Errorf("%s %s on an unwired database = nil, want the wiring error %q", c.dt, m, wiringErr)
					case errors.As(err, &he):
						t.Errorf("%s %s on an unwired database = %v (status %d), want a plain internal error", c.dt, m, err, he.Status())
					case errors.Is(err, auth.ErrNotFound), errors.Is(err, doctype.ErrNotFound):
						t.Errorf("%s %s on an unwired database = %v, want a plain internal error, not not-found", c.dt, m, err)
					case err.Error() != wiringErr.Error():
						t.Errorf("%s %s on an unwired database = %v, want the wiring error %q", c.dt, m, err, wiringErr)
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

// validatePanic calls Validate and returns the error it panicked with, or nil
// when it returned.
func validatePanic(ctx context.Context, h doctype.Hooks, tx *db.Tx, id int64) (got error) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(error); ok {
				got = e
			} else {
				got = fmt.Errorf("panic %v", r)
			}
		}
	}()
	h.Validate(ctx, tx, id)
	return nil
}

// MOD-PO P1 Do 2 (CD-247 (4)): the registered Order hooks implement
// doctype.Closer, so close runs OnClose in its transaction.
func TestPurchase_OrderHooksCloser(t *testing.T) {
	if _, ok := lookup(t, purchase.DocType).Hooks.(doctype.Closer); !ok {
		t.Errorf("%s Hooks %T don't implement doctype.Closer", purchase.DocType, lookup(t, purchase.DocType).Hooks)
	}
}
