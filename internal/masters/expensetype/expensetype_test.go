package expensetype_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/expensetype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Unit checks use the literal 06-masters.md values (MST-121, MST-122).

const sacMsg = "SAC code must be 6 digits starting with 99."

// wantOnlyMessage checks that err is a 422 *mstkit.ValidationError holding
// exactly one field error, with message msg.
func wantOnlyMessage(t *testing.T, what string, err error, msg string) {
	t.Helper()
	var ve *mstkit.ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("%s: err = %v (%T); want *mstkit.ValidationError %q", what, err, err, msg)
		return
	}
	if ve.Status() != 422 {
		t.Errorf("%s: Status() = %d; want 422", what, ve.Status())
	}
	if got := ve.FieldErrors(); len(got) != 1 || got[0].Message != msg || got[0].Field == "" {
		t.Errorf("%s: FieldErrors() = %+v; want exactly one on a field, with message %q", what, got, msg)
	}
}

func countExpenseTypes(t *testing.T, d *db.DB) int {
	t.Helper()
	ctx := context.Background()
	var n int
	if err := d.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT count(*) FROM expense_type`).Scan(&n)
	}); err != nil {
		t.Fatalf("count expense_type: %v", err)
	}
	return n
}

func TestExpenseType_Basic(t *testing.T) { // MST-121
	// Registry name and table (Task 11 Produces).
	if expensetype.DocType != "Expense Type" {
		t.Errorf("DocType = %q; want %q", expensetype.DocType, "Expense Type")
	}
	if k := expensetype.Kind; k.DocType != "Expense Type" || k.Table != "expense_type" || !k.HasCode {
		t.Errorf("Kind = %+v; want DocType %q, Table %q, HasCode true", k, "Expense Type", "expense_type")
	}

	// TRAVEL "Local travel" with no default HSN/SAC or tax code is valid.
	in := expensetype.Input{Code: "TRAVEL", Name: "Local travel"}
	out, err := expensetype.Validate(in)
	if err != nil {
		t.Fatalf("Validate(TRAVEL, no defaults): %v; want nil", err)
	}
	if out != in {
		t.Errorf("Validate(TRAVEL, no defaults) = %+v; want %+v", out, in)
	}

	// And it saves, with no defaults stored.
	env := msttest.New(t)
	ctx := context.Background()
	svc := expensetype.New(env.Kit)
	before := countExpenseTypes(t, env.DB)
	saved, err := svc.Create(ctx, env.Kavya, in)
	if err != nil {
		t.Fatalf("Create(TRAVEL, no defaults): %v", err)
	}
	if saved.ID == 0 || saved.Version != 1 {
		t.Errorf("Create(TRAVEL) = %+v; want a new id at version 1", saved)
	}
	if n := countExpenseTypes(t, env.DB); n != before+1 {
		t.Errorf("expense_type rows after Create = %d; want %d", n, before+1)
	}
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		got, err := svc.Get(ctx, tx, saved.ID)
		if err != nil {
			return err
		}
		want := expensetype.ExpenseType{ID: saved.ID, Code: "TRAVEL", Name: "Local travel", Version: 1}
		if got != want {
			t.Errorf("Get(TRAVEL) = %+v; want %+v", got, want)
		}

		byCode, ok, err := svc.ByCode(ctx, tx, "TRAVEL")
		if err != nil {
			return err
		}
		if !ok || byCode != want {
			t.Errorf("ByCode(TRAVEL) = %+v, %v; want %+v, true", byCode, ok, want)
		}

		hsnSAC, taxCodeID, err := svc.Defaults(ctx, tx, saved.ID)
		if err != nil {
			return err
		}
		if hsnSAC != "" || taxCodeID != 0 {
			t.Errorf("Defaults(TRAVEL) = %q, %d; want \"\", 0", hsnSAC, taxCodeID)
		}
		return nil
	}); err != nil {
		t.Fatalf("read TRAVEL: %v", err)
	}
}

func TestExpenseType_DefaultHSN(t *testing.T) { // MST-122
	// 9964 starts with 99, so it is a SAC and must be 6 digits.
	_, err := expensetype.Validate(expensetype.Input{Code: "TRAVEL", Name: "Local travel", DefaultHSNSAC: "9964"})
	wantOnlyMessage(t, "Validate(default 9964)", err, sacMsg)

	// 996411 (local transport) is a valid SAC; 4820 (stationery) is a goods
	// HSN that passes the MST-071 rules.
	for _, c := range []struct{ code, name, hsnSAC string }{
		{"TRAVEL", "Local travel", "996411"},
		{"STATIONERY", "Stationery", "4820"},
	} {
		out, err := expensetype.Validate(expensetype.Input{Code: c.code, Name: c.name, DefaultHSNSAC: c.hsnSAC})
		if err != nil {
			t.Errorf("Validate(%s default %s): %v; want nil", c.code, c.hsnSAC, err)
			continue
		}
		if out.DefaultHSNSAC != c.hsnSAC {
			t.Errorf("Validate(%s default %s).DefaultHSNSAC = %q; want %q", c.code, c.hsnSAC, out.DefaultHSNSAC, c.hsnSAC)
		}
	}

	// A code not starting 99 is checked with the goods HSN rules (MST-071).
	_, err = expensetype.Validate(expensetype.Input{Code: "STATIONERY", Name: "Stationery", DefaultHSNSAC: "482"})
	wantOnlyMessage(t, "Validate(default 482)", err, "HSN code must be 4, 6 or 8 digits.")

	// Create applies the same rule: 9964 is refused and nothing is stored;
	// 996411 is stored as given.
	env := msttest.New(t)
	ctx := context.Background()
	svc := expensetype.New(env.Kit)
	before := countExpenseTypes(t, env.DB)
	_, err = svc.Create(ctx, env.Kavya, expensetype.Input{Code: "TRAVEL", Name: "Local travel", DefaultHSNSAC: "9964"})
	wantOnlyMessage(t, "Create(TRAVEL default 9964)", err, sacMsg)
	if n := countExpenseTypes(t, env.DB); n != before {
		t.Errorf("expense_type rows after refused Create = %d; want %d (nothing stored)", n, before)
	}

	saved, err := svc.Create(ctx, env.Kavya, expensetype.Input{Code: "TRAVEL", Name: "Local travel", DefaultHSNSAC: "996411"})
	if err != nil {
		t.Fatalf("Create(TRAVEL default 996411): %v", err)
	}
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		got, err := svc.Get(ctx, tx, saved.ID)
		if err != nil {
			return err
		}
		if got.DefaultHSNSAC != "996411" {
			t.Errorf("Get(TRAVEL).DefaultHSNSAC = %q; want %q", got.DefaultHSNSAC, "996411")
		}
		hsnSAC, taxCodeID, err := svc.Defaults(ctx, tx, saved.ID)
		if err != nil {
			return err
		}
		if hsnSAC != "996411" || taxCodeID != 0 {
			t.Errorf("Defaults(TRAVEL) = %q, %d; want %q, 0", hsnSAC, taxCodeID, "996411")
		}
		return nil
	}); err != nil {
		t.Fatalf("read TRAVEL: %v", err)
	}
}
