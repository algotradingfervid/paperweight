package allmasters

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// rowFunc is a csvio.RowFunc that also gets the Services.
type rowFunc func(s *Services, ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string) (int64, []doctype.FieldError, error)

var (
	importServices atomic.Pointer[Services]
	importersOnce  sync.Once
)

// RegisterImporters makes s the Services every master's import uses. It is
// safe to repeat: csvio refuses a second registration and a RowFunc has no
// env, so the RowFuncs are registered once and read the latest s.
func RegisterImporters(s *Services) {
	importServices.Store(s)
	importersOnce.Do(func() {
		for _, group := range []map[string]rowFunc{partyImporters(), catalogueImporters(), orgImporters()} {
			for name, fn := range group {
				csvio.RegisterImporter(name, withServices(fn))
			}
		}
	})
}

// withServices is fn over the latest Services.
func withServices(fn rowFunc) csvio.RowFunc {
	return func(ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string) (int64, []doctype.FieldError, error) {
		return fn(importServices.Load(), ctx, tx, p, row)
	}
}

// fieldErrors turns a master's validation error into a row's field errors;
// any other error is returned and aborts the import.
func fieldErrors(err error) ([]doctype.FieldError, error) {
	var ve *mstkit.ValidationError
	if errors.As(err, &ve) {
		return ve.Fields, nil
	}
	return nil, err
}

// cellBool reads a Check cell. csvio has already refused any other word.
func cellBool(cell string) bool {
	switch strings.ToLower(cell) {
	case "yes", "true", "1":
		return true
	}
	return false
}

// cellOption returns the option cell names, as stored. A cell naming none
// comes back unchanged, so the master's validation refuses it.
func cellOption(cell string, options []string) string {
	for _, o := range options {
		if strings.EqualFold(cell, o) {
			return o
		}
	}
	return cell
}

// cellSelect reads a Select cell against the Options of docType's field; see
// cellOption.
func cellSelect(docType, field, cell string) string {
	dt, _ := doctype.Lookup(docType)
	f, _ := dt.Field(field)
	return cellOption(cell, f.Options)
}

// cellInt64 reads a Link cell: the id csvio resolved, in decimal, or "" for
// none (0).
func cellInt64(cell string) (int64, error) {
	if cell == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(cell, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("allmasters: link cell %q is not an id: %w", cell, err)
	}
	return id, nil
}

// cellValue reads a cell with csvio's parser for its type, "" being the zero
// value. csvio has already refused a cell that doesn't parse.
func cellValue[T any](cell string, parse func(string) (T, error)) (T, error) {
	var zero T
	if cell == "" {
		return zero, nil
	}
	v, err := parse(cell)
	if err != nil {
		return zero, fmt.Errorf("allmasters: cell %q: %w", cell, err)
	}
	return v, nil
}

// applyDisabled disables the row just created when its Disabled cell is true.
// No Input carries disabled. set is the master's SetDisabledTx.
func applyDisabled(ctx context.Context, tx *db.Tx, p auth.Principal, saved mstkit.Saved, row map[string]string,
	set func(context.Context, *db.Tx, auth.Principal, int64, int, bool) error) error {
	if !cellBool(row["disabled"]) {
		return nil
	}
	return set(ctx, tx, p, saved.ID, saved.Version, true)
}

// rowResult ends a RowFunc on the master's create: it applies the Disabled cell
// to the saved row and returns the id, or the row's field errors.
func rowResult(ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string, saved mstkit.Saved, err error,
	set func(context.Context, *db.Tx, auth.Principal, int64, int, bool) error) (int64, []doctype.FieldError, error) {
	if err == nil {
		err = applyDisabled(ctx, tx, p, saved, row, set)
	}
	return savedRow(saved, err)
}

// savedRow is a RowFunc's return for a row saved with err.
func savedRow(saved mstkit.Saved, err error) (int64, []doctype.FieldError, error) {
	if err != nil {
		errs, err := fieldErrors(err)
		return 0, errs, err
	}
	return saved.ID, nil, nil
}
