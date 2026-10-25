// Package fixture holds the test-only DocTypes that prove the document
// engine: fixture.Order, fixture.Receipt and fixture.Claim, and the stand-in
// master DocTypes they link to (CD-21). Its tables are test-only migrations
// (MigrationsFS, applied with goose table "goose_fixture") and never ship in
// the release binary.
package fixture

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"

	"github.com/vardhan/internaltools/internal/fixture/migrations"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// DocType names.
const (
	Order   = "fixture.Order"
	Receipt = "fixture.Receipt"
	Claim   = "fixture.Claim"
	Party   = "fixture.Party"
	Item    = "fixture.Item"
	Project = "fixture.Project"
)

// MigrationsFS holds the fixture tables' migrations (CD-114).
var MigrationsFS = migrations.FS

func init() {
	for _, m := range masterDocTypes() {
		doctype.Register(m)
	}
	doctype.Register(orderDocType())
	doctype.Register(receiptDocType())
	doctype.Register(claimDocType())
}

// rights builds a DefaultRights entry: every right in rights gets scope.
func rights(scope string, rights ...string) map[string]string {
	m := make(map[string]string, len(rights))
	for _, r := range rights {
		m[r] = scope
	}
	return m
}

func i64(v int64) *int64 { return &v }

// hookBase records a hook call in the probe and applies the probe's faults.
type hookBase struct{ name string } // "Order", "Receipt" or "Claim"

// enter appends "<name>.<hook>" to the probe, then returns Fail[hook] if set,
// then panics if Panic[hook] is set.
func (b hookBase) enter(ctx context.Context, hook string) error {
	p := ProbeFrom(ctx)
	if p == nil {
		return nil
	}
	p.Add(b.name + "." + hook)
	if err := p.fail(hook); err != nil {
		return err
	}
	if p.panics(hook) {
		panic("fixture: " + b.name + "." + hook + " panicked (probe)")
	}
	return nil
}

// validate is the common Validate hook: record, then Probe.ValidateErrors.
// Validate has no error return, so Probe.Fail["Validate"] surfaces as one
// FieldError with an empty Field: a form-level 422.
func (b hookBase) validate(ctx context.Context) []doctype.FieldError {
	if err := b.enter(ctx, "Validate"); err != nil {
		return []doctype.FieldError{{Message: err.Error()}}
	}
	return ProbeFrom(ctx).validateErrors()
}

// readDate reads a nullable YYYY-MM-DD column of row id.
func readDate(ctx context.Context, tx *db.Tx, query string, id int64) (types.Date, error) {
	var s sql.NullString
	if err := tx.SQL().QueryRowContext(ctx, query, id).Scan(&s); err != nil {
		return types.Date{}, err
	}
	if !s.Valid || s.String == "" {
		return types.Date{}, nil
	}
	t, err := time.Parse(time.DateOnly, s.String)
	if err != nil {
		return types.Date{}, err
	}
	return types.Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}, nil
}

// plainQty prints q as a plain decimal without trailing zeros: 6000 -> "6".
func plainQty(q int64) string {
	s := strconv.FormatInt(q/1000, 10)
	if frac := q % 1000; frac != 0 {
		s += "." + strings.TrimRight(strconv.FormatInt(1000+frac, 10)[1:], "0")
	}
	return s
}
