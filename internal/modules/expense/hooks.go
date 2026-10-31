package expense

import (
	"context"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// The hooks compose each task's functions in the order of the plan's hook
// table (MOD-EXP, Global constraints). Every hook reads its Deps first and
// returns the Wiring's error, a plain internal error, when nothing is wired
// for its database (CD-284). This file doesn't change after E1: each task
// replaces the functions in its own stubs file.

type claimHooks struct{}

// Validate has no error return, so an internal failure panics: the engine
// recovers it into a plain internal error and rolls back, as fixture.Claim's
// hooks do.
func (claimHooks) Validate(ctx context.Context, tx *db.Tx, id int64) []doctype.FieldError {
	d, err := wiring.Get(tx)
	if err != nil {
		validateFailed(ClaimDocType, id, err)
	}
	errs, err := validateClaim(ctx, tx, d, id)
	if err != nil {
		validateFailed(ClaimDocType, id, err)
	}
	more, err := checkReceipts(ctx, tx, d, id)
	if err != nil {
		validateFailed(ClaimDocType, id, err)
	}
	return append(errs, more...)
}

// BeforeSubmit runs on submit and again on approve (D-APR-15). The field
// errors of every submit rule are returned together, so the check page
// highlights every row at once.
func (claimHooks) BeforeSubmit(ctx context.Context, tx *db.Tx, id int64) error {
	d, err := wiring.Get(tx)
	if err != nil {
		return err
	}
	if err := checkLock(ctx, tx, ClaimDocType, id); err != nil {
		return err
	}
	var errs []doctype.FieldError
	for _, rule := range []func(context.Context, *db.Tx, Deps, int64) ([]doctype.FieldError, error){
		submitClaimFields, submitReceipts, submitAdvanceFields,
	} {
		fe, err := rule(ctx, tx, d, id)
		if err != nil {
			return err
		}
		errs = append(errs, fe...)
	}
	if len(errs) > 0 {
		return doc.Invalid(errs)
	}
	return checkAdvanceLeft(ctx, tx, d, id)
}

func (claimHooks) ApplyEffects(ctx context.Context, tx *db.Tx, id int64) error {
	d, err := wiring.Get(tx)
	if err != nil {
		return err
	}
	return applyClaim(ctx, tx, d, id)
}

func (claimHooks) ReverseEffects(ctx context.Context, tx *db.Tx, id int64) error {
	d, err := wiring.Get(tx)
	if err != nil {
		return err
	}
	if err := checkLock(ctx, tx, ClaimDocType, id); err != nil {
		return err
	}
	return reverseClaim(ctx, tx, d, id)
}

func (claimHooks) Amount(ctx context.Context, tx *db.Tx, id int64) (types.Money, error) {
	if _, err := wiring.Get(tx); err != nil {
		return 0, err
	}
	return amountClaim(ctx, tx, id)
}

func (claimHooks) Date(ctx context.Context, tx *db.Tx, id int64) (types.Date, error) {
	if _, err := wiring.Get(tx); err != nil {
		return types.Date{}, err
	}
	return dateClaim(ctx, tx, id)
}

type advanceHooks struct{}

// Validate panics on an internal failure; see claimHooks.Validate.
func (advanceHooks) Validate(ctx context.Context, tx *db.Tx, id int64) []doctype.FieldError {
	d, err := wiring.Get(tx)
	if err != nil {
		validateFailed(AdvanceDocType, id, err)
	}
	errs, err := validateAdvance(ctx, tx, d, id)
	if err != nil {
		validateFailed(AdvanceDocType, id, err)
	}
	return errs
}

func (advanceHooks) BeforeSubmit(ctx context.Context, tx *db.Tx, id int64) error {
	if _, err := wiring.Get(tx); err != nil {
		return err
	}
	return checkLock(ctx, tx, AdvanceDocType, id)
}

// ApplyEffects has no work: an advance changes nothing else on approval.
func (advanceHooks) ApplyEffects(_ context.Context, tx *db.Tx, _ int64) error {
	_, err := wiring.Get(tx)
	return err
}

func (advanceHooks) ReverseEffects(ctx context.Context, tx *db.Tx, id int64) error {
	if _, err := wiring.Get(tx); err != nil {
		return err
	}
	if err := checkLock(ctx, tx, AdvanceDocType, id); err != nil {
		return err
	}
	return checkNotPaid(ctx, tx, id)
}

func (advanceHooks) Amount(ctx context.Context, tx *db.Tx, id int64) (types.Money, error) {
	if _, err := wiring.Get(tx); err != nil {
		return 0, err
	}
	return amountAdvance(ctx, tx, id)
}

func (advanceHooks) Date(ctx context.Context, tx *db.Tx, id int64) (types.Date, error) {
	if _, err := wiring.Get(tx); err != nil {
		return types.Date{}, err
	}
	return dateAdvance(ctx, tx, id)
}

// validateFailed reports an internal failure in Validate.
func validateFailed(docType string, id int64, err error) {
	panic(fmt.Sprintf("expense: %s Validate of %d: %v", docType, id, err))
}
