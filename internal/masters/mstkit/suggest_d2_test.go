package mstkit_test

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// No case ID: UI v2 D2, CD-341 (6). Quick-create suggests a code from the
// name: its letters and digits upper-cased, at most 20, made unique with a
// number when taken.
func TestKit_SuggestCode(t *testing.T) {
	k := newKit(t)
	k.insertThing(t, "SAIKRUPA", "Sai Krupa Traders")
	k.insertThing(t, "SAIKRUPA2", "Sai Krupa Steel")
	k.insertThing(t, "ABCDEFGHIJKLMNOPQRST", "Alphabet Traders")
	ctx := context.Background()
	for name, want := range map[string]string{
		"Shree Krishna & Sons":             "SHREEKRISHNASONS",
		"  m/s. 4 star  ":                  "MS4STAR",
		"Sai Krupa":                        "SAIKRUPA3",
		"Zinc a-z 0-9 @[`{/:":              "ZINCAZ09",
		"Abcdefghij klmnopqrst uvwxyz":     "ABCDEFGHIJKLMNOPQRS2",
		"Bharat Heavy Electricals Limited": "BHARATHEAVYELECTRICA",
		"श्री गणेश":                        "",
		"":                                 "",
	} {
		k.write(t, func(tx *db.Tx) error {
			got, err := mstkit.SuggestCode(ctx, tx, party, name)
			if err != nil || got != want {
				t.Errorf("SuggestCode(%q) = %q, %v; want %q", name, got, err, want)
			}
			return nil
		})
	}
}

// The search for a free number stops at 1000: Q, Q2…Q999 taken gives Q1000;
// Q1000 taken too gives "", and the user types a code.
func TestKit_SuggestCodeBounded(t *testing.T) {
	k := newKit(t)
	ctx := context.Background()
	k.write(t, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 999)
			INSERT INTO kit_thing (code, name, created_at, updated_at) SELECT CASE i WHEN 1 THEN 'Q' ELSE 'Q' || i END, 'Q ' || i, '', '' FROM n`)
		return err
	})
	suggest := func() string {
		var got string
		k.write(t, func(tx *db.Tx) error {
			var err error
			got, err = mstkit.SuggestCode(ctx, tx, party, "q")
			return err
		})
		return got
	}
	if got := suggest(); got != "Q1000" {
		t.Errorf("SuggestCode with Q…Q999 taken = %q, want Q1000", got)
	}
	k.insertThing(t, "Q1000", "Q 1000")
	if got := suggest(); got != "" {
		t.Errorf("SuggestCode with Q…Q1000 taken = %q, want \"\"", got)
	}
}
