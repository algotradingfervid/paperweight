package uom_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// seeded is migration 01251 (Task 8 Produces; M9, CD-39, CD-62).
var seeded = []struct {
	code, name, uqc string
	whole           bool
}{
	{"NOS", "Numbers", "NOS", true},
	{"KGS", "Kilograms", "KGS", false},
	{"MTR", "Meters", "MTR", false},
	{"LTR", "Litres", "LTR", false},
	{"BOX", "Box", "BOX", true},
	{"SET", "Sets", "SET", true},
	{"BAG", "Bags", "BAG", true},
	{"TON", "Tonnes", "TON", false},
	{"SQM", "Square meters", "SQM", false},
	{"HRS", "Hours", "OTH", false},
	{"DRUM", "Drums", "DRM", true},
}

func countUoMs(t *testing.T, d *db.DB) int {
	t.Helper()
	ctx := context.Background()
	var n int
	if err := d.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT count(*) FROM uom`).Scan(&n)
	}); err != nil {
		t.Fatalf("count uom: %v", err)
	}
	return n
}

func TestUoM_Basic(t *testing.T) { // MST-077
	env := msttest.New(t)
	ctx := context.Background()
	svc := uom.New(env.Kit)

	// The seed: exactly the eleven rows, enabled.
	if n := countUoMs(t, env.DB); n != len(seeded) {
		t.Errorf("seeded uom rows = %d; want %d", n, len(seeded))
	}
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		for _, s := range seeded {
			u, ok, err := svc.ByCode(ctx, tx, s.code)
			if err != nil {
				return err
			}
			if !ok {
				t.Errorf("ByCode(%s): not found; want the seeded UoM", s.code)
				continue
			}
			if u.Code != s.code || u.Name != s.name || u.UQC != s.uqc || u.WholeNumber != s.whole || u.Disabled {
				t.Errorf("seeded %s = {Code %q, Name %q, UQC %q, WholeNumber %v, Disabled %v}; want {%q, %q, %q, %v, false}",
					s.code, u.Code, u.Name, u.UQC, u.WholeNumber, u.Disabled, s.code, s.name, s.uqc, s.whole)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("read seed: %v", err)
	}

	// NOS "Numbers" whole-number and KGS "Kilograms" not whole-number save
	// (custom codes, since the seed holds NOS and KGS).
	for _, c := range []struct {
		in   uom.Input
		want uom.UoM
	}{
		{uom.Input{Code: "NOS2", Name: "Numbers", WholeNumber: true, UQC: "NOS"}, uom.UoM{Code: "NOS2", Name: "Numbers", WholeNumber: true, UQC: "NOS"}},
		{uom.Input{Code: "KGS2", Name: "Kilograms", WholeNumber: false, UQC: "KGS"}, uom.UoM{Code: "KGS2", Name: "Kilograms", WholeNumber: false, UQC: "KGS"}},
	} {
		saved, err := svc.Create(ctx, env.Kavya, c.in)
		if err != nil {
			t.Fatalf("Create(%s): %v", c.in.Code, err)
		}
		var got uom.UoM
		if err := env.DB.Read(ctx, func(tx *db.Tx) error {
			var err error
			got, err = svc.Get(ctx, tx, saved.ID)
			return err
		}); err != nil {
			t.Fatalf("Get(%s): %v", c.in.Code, err)
		}
		if got.ID != saved.ID || got.Code != c.want.Code || got.Name != c.want.Name || got.WholeNumber != c.want.WholeNumber || got.UQC != c.want.UQC || got.Disabled {
			t.Errorf("Get(%s) = %+v; want ID %d, Code %q, Name %q, WholeNumber %v, UQC %q, enabled",
				c.in.Code, got, saved.ID, c.want.Code, c.want.Name, c.want.WholeNumber, c.want.UQC)
		}
	}

	// Codes are unique, case-insensitively (MST-003 with Label "UoM").
	const dupMsg = "Code NOS is already used by UoM Numbers. Choose another code."
	_, err := svc.Create(ctx, env.Kavya, uom.Input{Code: "nos", Name: "Numbers again", WholeNumber: true, UQC: "NOS"})
	var ve *mstkit.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("Create(nos) err = %v (%T); want *mstkit.ValidationError %q", err, err, dupMsg)
	}
	if ve.Status() != 422 {
		t.Errorf("Create(nos) Status() = %d; want 422", ve.Status())
	}
	want := doctype.FieldError{Field: "code", Message: dupMsg}
	if got := ve.FieldErrors(); len(got) != 1 || got[0] != want {
		t.Errorf("Create(nos) FieldErrors() = %+v; want [%+v]", got, want)
	}
	if n := countUoMs(t, env.DB); n != len(seeded)+2 {
		t.Errorf("uom rows after duplicate = %d; want %d (nothing stored)", n, len(seeded)+2)
	}
}
