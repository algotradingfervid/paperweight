package goodsreceipt

import (
	"context"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// The hooks compose each task's functions in the order of the plan's hook
// table (MOD-GRN, Global constraints). Every hook reads its Deps first and
// returns the Wiring's error, a plain internal error, when nothing is wired
// for its database (CD-284). This file doesn't change after G1: each task
// replaces the functions in its own stubs file.

type receiptHooks struct{}

// Validate has no error return, so an internal failure panics: the engine
// recovers it into a plain internal error and rolls back. Every
// Hooks.Validate call runs inside engine.Do's recover.
func (receiptHooks) Validate(ctx context.Context, tx *db.Tx, id int64) []doctype.FieldError {
	errs, err := validate(ctx, tx, id)
	if err != nil {
		panic(fmt.Sprintf("goodsreceipt: Validate of %d: %v", id, err))
	}
	return errs
}

func validate(ctx context.Context, tx *db.Tx, id int64) ([]doctype.FieldError, error) {
	d, err := wiring.Get(tx)
	if err != nil {
		return nil, err
	}
	return validateReceipt(ctx, tx, d, id)
}

// BeforeSubmit runs on submit and again on approve (D-APR-15). The lock, as
// a receipt_date field error (CD-282 Q9), and the submit-only rules are
// returned together as one doc.Invalid, only when there are any (CD-264 (2)).
func (receiptHooks) BeforeSubmit(ctx context.Context, tx *db.Tx, id int64) error {
	d, err := wiring.Get(tx)
	if err != nil {
		return err
	}
	errs, err := checkLockFields(ctx, tx, d, id)
	if err != nil {
		return err
	}
	more, err := submitFields(ctx, tx, d, id)
	if err != nil {
		return err
	}
	if errs = append(errs, more...); len(errs) > 0 {
		return doc.Invalid(errs)
	}
	return nil
}

func (receiptHooks) ApplyEffects(ctx context.Context, tx *db.Tx, id int64) error {
	d, err := wiring.Get(tx)
	if err != nil {
		return err
	}
	if err := applyReceipt(ctx, tx, d, id); err != nil {
		return err
	}
	if err := copyBillOnAmend(ctx, tx, d, id); err != nil {
		return err
	}
	return restNotComing(ctx, tx, d, id, true)
}

// ReverseEffects refuses a cancel inside the period lock first, which rolls
// the cancel back (DOC-077).
func (receiptHooks) ReverseEffects(ctx context.Context, tx *db.Tx, id int64) error {
	d, err := wiring.Get(tx)
	if err != nil {
		return err
	}
	if err := checkLock(ctx, tx, d, id); err != nil {
		return err
	}
	if err := reverseReceipt(ctx, tx, d, id); err != nil {
		return err
	}
	return restNotComing(ctx, tx, d, id, false)
}

// Amount is accepted_value. No approval rule is seeded, so it matters only
// when an owner adds one.
func (receiptHooks) Amount(ctx context.Context, tx *db.Tx, id int64) (types.Money, error) {
	if _, err := wiring.Get(tx); err != nil {
		return 0, err
	}
	v, err := dbq.New(tx.SQL()).ReceiptAcceptedValue(ctx, id)
	if err != nil {
		return 0, err
	}
	return types.Money(v.Int64), nil
}

// Date is receipt_date, which sets the FY of the number (spec §6). The
// submit-time Required check stops a blank date first; if one got here,
// DateFromISO("") would return a plain error before naming.Assign runs.
func (receiptHooks) Date(ctx context.Context, tx *db.Tx, id int64) (types.Date, error) {
	if _, err := wiring.Get(tx); err != nil {
		return types.Date{}, err
	}
	s, err := dbq.New(tx.SQL()).ReceiptDate(ctx, id)
	if err != nil {
		return types.Date{}, err
	}
	return types.DateFromISO(s.String)
}
