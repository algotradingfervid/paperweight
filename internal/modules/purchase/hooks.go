package purchase

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Every hook reads the wiring for its tx's database first (CD-284), so a
// database nothing was wired for fails each hook with a plain internal error.
// Each method then calls one function in its owner's file (stubs_<task>.go
// until the owner builds it; CD-256).

// mustWired is Validate's wiring check: Validate returns only field errors,
// so an unwired database panics, which the engine turns into its plain
// internal error (doc/engine.go). The panic is safe only because every
// Hooks.Validate call (doc/save.go, doc/transitions.go, doc/terminal.go) runs
// inside engine.Do's recover: a caller outside Do must not call Validate
// directly.
func mustWired(tx *db.Tx) {
	if _, err := wiring.Get(tx); err != nil {
		panic(err)
	}
}

// orderHooks are purchase.Order's hooks. OnClose makes them a doctype.Closer.
type orderHooks struct{}

func (orderHooks) Validate(ctx context.Context, tx *db.Tx, id int64) []doctype.FieldError {
	mustWired(tx)
	return validateOrder(ctx, tx, id)
}

func (orderHooks) BeforeSubmit(ctx context.Context, tx *db.Tx, id int64) error {
	if _, err := wiring.Get(tx); err != nil {
		return err
	}
	return beforeSubmitOrder(ctx, tx, id)
}

func (orderHooks) ApplyEffects(ctx context.Context, tx *db.Tx, id int64) error {
	if _, err := wiring.Get(tx); err != nil {
		return err
	}
	return applyOrder(ctx, tx, id)
}

func (orderHooks) ReverseEffects(ctx context.Context, tx *db.Tx, id int64) error {
	if _, err := wiring.Get(tx); err != nil {
		return err
	}
	return reverseOrder(ctx, tx, id)
}

func (orderHooks) OnClose(ctx context.Context, tx *db.Tx, id int64) error {
	if _, err := wiring.Get(tx); err != nil {
		return err
	}
	return closeOrder(ctx, tx, id)
}

func (orderHooks) Amount(ctx context.Context, tx *db.Tx, id int64) (types.Money, error) {
	if _, err := wiring.Get(tx); err != nil {
		return 0, err
	}
	return amountOrder(ctx, tx, id)
}

func (orderHooks) Date(ctx context.Context, tx *db.Tx, id int64) (types.Date, error) {
	if _, err := wiring.Get(tx); err != nil {
		return types.Date{}, err
	}
	return dateOrder(ctx, tx, id)
}

// dateOrder is the PO's order_date, which sets the FY of its number
// (D-NAM-04). A zero date would make naming.Assign refuse every approve.
func dateOrder(ctx context.Context, tx *db.Tx, id int64) (types.Date, error) {
	s, err := dbq.New(tx.SQL()).OrderDate(ctx, id)
	if err != nil {
		return types.Date{}, err
	}
	return types.DateFromISO(s.String)
}

// requestHooks are purchase.Request's hooks. A request has no effects and no
// amount: it isn't approvable, and ordered quantities are kept by the PO's
// hooks (§20.3).
type requestHooks struct{}

func (requestHooks) Validate(ctx context.Context, tx *db.Tx, id int64) []doctype.FieldError {
	mustWired(tx)
	return validateRequest(ctx, tx, id)
}

func (requestHooks) BeforeSubmit(ctx context.Context, tx *db.Tx, id int64) error {
	if _, err := wiring.Get(tx); err != nil {
		return err
	}
	return beforeSubmitRequest(ctx, tx, id)
}

func (requestHooks) ApplyEffects(_ context.Context, tx *db.Tx, _ int64) error {
	_, err := wiring.Get(tx)
	return err
}

func (requestHooks) ReverseEffects(_ context.Context, tx *db.Tx, _ int64) error {
	_, err := wiring.Get(tx)
	return err
}

func (requestHooks) Amount(_ context.Context, tx *db.Tx, _ int64) (types.Money, error) {
	_, err := wiring.Get(tx)
	return 0, err
}

func (requestHooks) Date(ctx context.Context, tx *db.Tx, id int64) (types.Date, error) {
	if _, err := wiring.Get(tx); err != nil {
		return types.Date{}, err
	}
	return dateRequest(ctx, tx, id)
}

// dateRequest is the request's request_date, which sets the FY of its number.
func dateRequest(ctx context.Context, tx *db.Tx, id int64) (types.Date, error) {
	s, err := dbq.New(tx.SQL()).RequestDate(ctx, id)
	if err != nil {
		return types.Date{}, err
	}
	return types.DateFromISO(s.String)
}
