package csvio

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list/listtest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// useLinkCheck installs a stub that refuses the codes in hidden with refusal
// and records every target it is asked about, until the test ends.
func useLinkCheck(t *testing.T, e *importEnv, refusal error, hidden ...string) *[]string {
	t.Helper()
	asked := &[]string{}
	old := linkCheck
	t.Cleanup(func() { linkCheck = old })
	SetLinkCheck(func(ctx context.Context, tx *db.Tx, a auth.Service, p auth.Principal, target *doctype.DocType, id int64) error {
		if a != e.auth || p.ID != e.p.ID {
			t.Errorf("hook got auth %v, principal %v", a, p)
		}
		var code string
		if err := tx.SQL().QueryRowContext(ctx, "SELECT code FROM "+target.Table+" WHERE id = ?", id).Scan(&code); err != nil {
			return err
		}
		*asked = append(*asked, code)
		if slices.Contains(hidden, code) {
			return refusal
		}
		return nil
	})
	return asked
}

func uomFile(uoms ...string) string {
	var b strings.Builder
	b.WriteString("Code,Name,Type,UoM\r\n")
	for i, u := range uoms {
		b.WriteString("N-" + string(rune('A'+i)) + ",Item,Goods," + u + "\r\n")
	}
	return b.String()
}

func TestLinkCheck_HiddenReadsAsUnknown(t *testing.T) {
	for name, refusal := range map[string]error{"not found": auth.ErrNotFound, "no right": &auth.DeniedError{}} {
		for run, fn := range map[string]func(*importEnv) func(context.Context, auth.Principal, *doctype.DocType, string) (Report, error){
			"preview": func(e *importEnv) func(context.Context, auth.Principal, *doctype.DocType, string) (Report, error) {
				return func(ctx context.Context, p auth.Principal, dt *doctype.DocType, s string) (Report, error) {
					return e.im.Preview(ctx, p, dt, strings.NewReader(s))
				}
			},
			"import": func(e *importEnv) func(context.Context, auth.Principal, *doctype.DocType, string) (Report, error) {
				return func(ctx context.Context, p auth.Principal, dt *doctype.DocType, s string) (Report, error) {
					return e.im.Import(ctx, p, dt, strings.NewReader(s))
				}
			},
		} {
			t.Run(name+"/"+run, func(t *testing.T) {
				e := newEnv(t)
				e.seed(t)
				e.disableBox(t)
				useLinkCheck(t, e, refusal, "kg", "Box")
				r, err := fn(e)(t.Context(), e.p, listtest.Item(), uomFile("kg", "Box", "Zed"))
				if err != nil {
					t.Fatal(err)
				}
				got := errorTexts(r)
				if len(got) != 3 {
					t.Fatalf("errors = %q", got)
				}
				for i, code := range []string{"kg", "Box", "Zed"} {
					want := "Row " + string(rune('2'+i)) + ": UoM '" + code + "' not found."
					if !strings.HasPrefix(got[i], want) {
						t.Errorf("row %d = %q, want it to start %q", i+2, got[i], want)
					}
					if strings.Contains(got[i], "disabled") || strings.Contains(got[i], "such as kg") {
						t.Errorf("row %d shows a hidden row: %q", i+2, got[i])
					}
				}
				if len(calls) != 0 {
					t.Errorf("RowFunc ran for a hidden target: %+v", calls)
				}
			})
		}
	}
}

func TestLinkCheck_ReadableRowsAnswerAsBefore(t *testing.T) {
	e := newEnv(t)
	e.seed(t)
	e.disableBox(t)
	asked := useLinkCheck(t, e, auth.ErrNotFound)
	r, err := e.im.Preview(t.Context(), e.p, listtest.Item(), strings.NewReader(uomFile("kg", "Box", "Kgs")))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Row 3: UoM 'Box' is disabled. Enable it or use another UoM.",
		"Row 4: UoM 'Kgs' not found. Use an existing UoM code such as kg.",
	}
	if got := errorTexts(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("errors = %q", got)
	}
	if !slices.Contains(*asked, "kg") || !slices.Contains(*asked, "Box") {
		t.Errorf("hook asked about %q", *asked)
	}
}

// The check comes before the disabled answer: Box is asked about and its
// disabled text never shows.
func TestLinkCheck_AheadOfDisabled(t *testing.T) {
	e := newEnv(t)
	e.seed(t)
	e.disableBox(t)
	useLinkCheck(t, e, auth.ErrNotFound, "Box")
	r, err := e.im.Preview(t.Context(), e.p, listtest.Item(), strings.NewReader(uomFile("Box")))
	if err != nil {
		t.Fatal(err)
	}
	if got := errorTexts(r); len(got) != 1 || !strings.HasPrefix(got[0], "Row 2: UoM 'Box' not found.") {
		t.Fatalf("errors = %q", got)
	}
}

// The closest readable row is the suggestion; with none, the text stops at
// "not found.".
func TestLinkCheck_SuggestionSkipsHiddenRows(t *testing.T) {
	e := newEnv(t)
	e.seed(t)
	useLinkCheck(t, e, auth.ErrNotFound, "kg")
	r, err := e.im.Preview(t.Context(), e.p, listtest.Item(), strings.NewReader(uomFile("Kgs")))
	if err != nil {
		t.Fatal(err)
	}
	got := errorTexts(r)
	if len(got) != 1 || strings.Contains(got[0], "such as kg") || !strings.Contains(got[0], "such as ") {
		t.Fatalf("errors = %q, want another code suggested", got)
	}

	e = newEnv(t)
	e.seed(t)
	useLinkCheck(t, e, auth.ErrNotFound, activeUoMs(t, e)...)
	r, err = e.im.Preview(t.Context(), e.p, listtest.Item(), strings.NewReader(uomFile("Kgs")))
	if err != nil {
		t.Fatal(err)
	}
	if got := errorTexts(r); !reflect.DeepEqual(got, []string{"Row 2: UoM 'Kgs' not found."}) {
		t.Fatalf("errors = %q, want no suggestion", got)
	}
}

// activeUoMs is every code suggestCode may offer.
func activeUoMs(t *testing.T, e *importEnv) []string {
	t.Helper()
	var active []string
	err := e.db.Read(t.Context(), func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(t.Context(), "SELECT code FROM numt_uom WHERE disabled = 0")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				return err
			}
			active = append(active, c)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return active
}

// disableBox makes the fixture's Box disabled, as TestImport's link case does.
func (e *importEnv) disableBox(t *testing.T) {
	t.Helper()
	err := e.db.Write(t.Context(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(t.Context(), "UPDATE numt_uom SET disabled = 1 WHERE code = 'Box'")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A failure that isn't a refusal stops the import, from either the cell's own
// check or a suggestion's.
func TestLinkCheck_OtherErrorsAbort(t *testing.T) {
	broken := errors.New("disk on fire")
	for name, file := range map[string]string{"cell": uomFile("kg"), "suggestion": uomFile("Zed")} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.seed(t)
			useLinkCheck(t, e, broken, activeUoMs(t, e)...)
			if _, err := e.im.Preview(t.Context(), e.p, listtest.Item(), strings.NewReader(file)); !errors.Is(err, broken) {
				t.Fatalf("err = %v, want %v", err, broken)
			}
		})
	}
}

// No hook (a binary with no masters package): nothing is asked and the answers
// are the ones TestImport covers.
func TestLinkCheck_NoHook(t *testing.T) {
	old := linkCheck
	t.Cleanup(func() { linkCheck = old })
	linkCheck = nil
	e := newEnv(t)
	e.seed(t)
	r, err := e.im.Preview(t.Context(), e.p, listtest.Item(), strings.NewReader(uomFile("Kgs")))
	if err != nil {
		t.Fatal(err)
	}
	if got := errorTexts(r); !reflect.DeepEqual(got, []string{"Row 2: UoM 'Kgs' not found. Use an existing UoM code such as kg."}) {
		t.Fatalf("errors = %q", got)
	}
}

// A bad cell repeated down a file asks about each row of the target once, not
// once per cell, and so does a repeated hidden code.
func TestLinkCheck_AsksOncePerRow(t *testing.T) {
	const cells = 6
	repeat := func(code string) []string { return slices.Repeat([]string{code}, cells) }
	for name, file := range map[string]string{"unknown": uomFile(repeat("Zed")...), "hidden": uomFile(repeat("kg")...)} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.seed(t)
			active := activeUoMs(t, e)
			asked := useLinkCheck(t, e, auth.ErrNotFound, active...)
			if _, err := e.im.Preview(t.Context(), e.p, listtest.Item(), strings.NewReader(file)); err != nil {
				t.Fatal(err)
			}
			if len(*asked) != len(active) {
				t.Errorf("%d checks for %d cells over %d rows, want one per row: %q", len(*asked), cells, len(active), *asked)
			}
		})
	}
}

// A readable row stops the search, so one suggestion costs one check when the
// closest row is readable.
func TestLinkCheck_StopsAtTheFirstReadableRow(t *testing.T) {
	e := newEnv(t)
	e.seed(t)
	asked := useLinkCheck(t, e, auth.ErrNotFound)
	if _, err := e.im.Preview(t.Context(), e.p, listtest.Item(), strings.NewReader(uomFile("Kgs", "Kgs"))); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*asked, []string{"kg"}) {
		t.Errorf("asked about %q, want kg once", *asked)
	}
}

// One scan per target: a row that appears later isn't seen until added says so,
// and added leaves out a disabled row.
func TestLinkMemo_ScansOncePerTarget(t *testing.T) {
	e := newEnv(t)
	e.seed(t)
	ctx := t.Context()
	uom := listtest.UoM()
	err := e.db.Write(ctx, func(tx *db.Tx) error {
		m := newLinkMemo(e.auth, e.p)
		suggest := func() string {
			s, err := suggestCode(ctx, tx, m, uom, "Zed")
			if err != nil {
				t.Fatal(err)
			}
			return s
		}
		insert := func(code string, disabled int) int64 {
			res, err := tx.SQL().ExecContext(ctx, "INSERT INTO numt_uom (code, name, disabled) VALUES (?, ?, ?)", code, code, disabled)
			if err != nil {
				t.Fatal(err)
			}
			id, err := res.LastInsertId()
			if err != nil {
				t.Fatal(err)
			}
			return id
		}
		first := suggest()
		on := insert("Zed-1", 0)
		if got := suggest(); got != first {
			t.Errorf("second suggestion = %q, want the memo's %q", got, first)
		}
		if err := m.added(ctx, tx, uom, on); err != nil {
			return err
		}
		if got := suggest(); got != "Zed-1" {
			t.Errorf("after added = %q, want Zed-1", got)
		}
		off := insert("Zed", 1)
		if err := m.added(ctx, tx, uom, off); err != nil {
			return err
		}
		if got := suggest(); got != "Zed-1" {
			t.Errorf("after a disabled row = %q, want Zed-1", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
