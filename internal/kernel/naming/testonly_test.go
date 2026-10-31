package naming

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// CD-291: a TestOnly DocType's prefix never clashes with a real DocType's.
// These names are registered here so naming can tell real from TestOnly.
const (
	dtRealA = "nam.RealA"
	dtRealB = "nam.RealB"
	dtTestA = "nam.TestA"
	dtTestB = "nam.TestB"
)

func init() {
	doctype.Register(doctype.DocType{Name: dtRealA, Label: "Real A", Table: "nam_real_a"})
	doctype.Register(doctype.DocType{Name: dtRealB, Label: "Real B", Table: "nam_real_b"})
	doctype.Register(doctype.DocType{Name: dtTestA, Label: "Test A", Table: "nam_test_a", TestOnly: true})
	doctype.Register(doctype.DocType{Name: dtTestB, Label: "Test B", Table: "nam_test_b", TestOnly: true})
}

func TestEnsureSeries_TestOnlyPrefixClash(t *testing.T) {
	const tpl = "ZZ/{FY}/{####}"
	allowed := []struct{ name, first, second string }{
		{"testonly_then_real", dtTestA, dtRealA},
		{"real_then_testonly", dtRealA, dtTestA},
	}
	for _, c := range allowed {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			ctx := context.Background()
			if err := h.nm.EnsureSeries(ctx, c.first, "First", tpl); err != nil {
				t.Fatalf("EnsureSeries(%s): %v", c.first, err)
			}
			if err := h.nm.EnsureSeries(ctx, c.second, "Second", tpl); err != nil {
				t.Fatalf("EnsureSeries(%s) after %s: %v, want no clash with a TestOnly DocType", c.second, c.first, err)
			}
			if got := seriesTemplate(t, h, c.second); got != tpl {
				t.Errorf("%s template = %q, want %q", c.second, got, tpl)
			}
		})
	}
	refused := []struct{ name, first, second string }{
		{"real_vs_real", dtRealA, dtRealB},
		{"testonly_vs_testonly", dtTestA, dtTestB},
	}
	for _, c := range refused {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			ctx := context.Background()
			if err := h.nm.EnsureSeries(ctx, c.first, "First", tpl); err != nil {
				t.Fatalf("EnsureSeries(%s): %v", c.first, err)
			}
			err := h.nm.EnsureSeries(ctx, c.second, "Second", tpl)
			wantNamingError(t, "EnsureSeries("+c.second+")", err, ErrDuplicatePrefix,
				"Prefix ZZ/{FY}/ is already used by First. Use a different prefix.")
			if n := h.count(`SELECT COUNT(*) FROM naming_series WHERE doctype = ?`, c.second); n != 0 {
				t.Errorf("naming_series rows for %s = %d, want 0", c.second, n)
			}
		})
	}
}

// The motivating case: a real module DocType seeds the series of a fixture
// DocType (purchase.Order beside fixture.Order, expense.Claim beside
// fixture.Claim), so each fixture DocType with a series is TestOnly.
func TestEnsureSeries_RealBesideFixtureDocTypes(t *testing.T) {
	for _, s := range harnessSeries {
		t.Run(s.doctype, func(t *testing.T) {
			h := newHarness(t)
			if err := h.nm.EnsureSeries(context.Background(), dtRealA, s.label, s.tpl); err != nil {
				t.Fatalf("EnsureSeries(%s, %s) beside %s: %v", dtRealA, s.tpl, s.doctype, err)
			}
		})
	}
}

func TestSetTemplate_TestOnlyPrefixClash(t *testing.T) {
	cases := []struct {
		name, other, target string
		refused             bool
	}{
		{"real_to_testonly_prefix", dtTestA, dtRealA, false},
		{"testonly_to_real_prefix", dtRealA, dtTestA, false},
		{"real_to_real_prefix", dtRealB, dtRealA, true},
		{"testonly_to_testonly_prefix", dtTestB, dtTestA, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			ctx := context.Background()
			if err := h.nm.EnsureSeries(ctx, c.other, "Other", "ZZ/{FY}/{####}"); err != nil {
				t.Fatalf("EnsureSeries(%s): %v", c.other, err)
			}
			if err := h.nm.EnsureSeries(ctx, c.target, "Target", "YY/{FY}/{####}"); err != nil {
				t.Fatalf("EnsureSeries(%s): %v", c.target, err)
			}
			err := h.nm.SetTemplate(ctx, admin, c.target, "ZZ/{FY}/{####}")
			if c.refused {
				wantNamingError(t, "SetTemplate("+c.target+")", err, ErrDuplicatePrefix,
					"Prefix ZZ/{FY}/ is already used by Other. Use a different prefix.")
				if got := seriesTemplate(t, h, c.target); got != "YY/{FY}/{####}" {
					t.Errorf("%s template = %q, want unchanged YY/{FY}/{####}", c.target, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("SetTemplate(%s, ZZ/{FY}/{####}): %v, want no clash with a TestOnly DocType", c.target, err)
			}
			if got := seriesTemplate(t, h, c.target); got != "ZZ/{FY}/{####}" {
				t.Errorf("%s template = %q, want ZZ/{FY}/{####}", c.target, got)
			}
		})
	}
}
