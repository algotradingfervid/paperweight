package purchase_test

import (
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// CD-337 (4): a PO line whose price is exactly ₹0 warns `Price is ₹0 on line
// {n}.` in OrderWarnings, once per such line; ₹0.01 and a blank price warn
// nothing. Removing the line removes the warning, and a ₹0 line still
// submits, keeping the warning.
func TestPO_ZeroPriceWarning(t *testing.T) {
	env := purchasetest.New(t)
	angle := func(price string) map[string]string { return p2Line(t, env, "MS-ANGLE-50", "KGS", "10", price) }

	for _, c := range []struct {
		what     string
		lines    []map[string]string
		warnings []string
	}{
		{"one line at 0", []map[string]string{angle("0")}, []string{"Price is ₹0 on line 1."}},
		{"lines 1 and 3 at 0", []map[string]string{angle("0"), angle("62.5"), angle("0")},
			[]string{"Price is ₹0 on line 1.", "Price is ₹0 on line 3."}},
		{"0.01", []map[string]string{angle("0.01")}, nil},
		{"blank", []map[string]string{angle("")}, nil},
	} {
		po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(c.lines...))
		if got := p2Warnings(t, env, env.Priya, po.ID); !slices.Equal(got, c.warnings) {
			t.Errorf("%s: OrderWarnings = %q, want %q", c.what, got, c.warnings)
		}
	}

	// Removing the ₹0 line removes its warning.
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(angle("62.5"), angle("0")))
	if got, want := p2Warnings(t, env, env.Priya, po.ID), []string{"Price is ₹0 on line 2."}; !slices.Equal(got, want) {
		t.Errorf("before removing: OrderWarnings = %q, want %q", got, want)
	}
	p2Must(t, "remove the ₹0 line")(p2Save(t, env, env.Priya, po.ID, nil, []map[string]string{angle("62.5")}))
	if got := p2Warnings(t, env, env.Priya, po.ID); len(got) != 0 {
		t.Errorf("after removing the ₹0 line: OrderWarnings = %q, want none", got)
	}

	// A warning, not a refusal: the ₹0 PO submits.
	sub := purchasetest.NewOrder(t, env, purchasetest.OrderLines(angle("0")))
	if sub.State != doctype.StateApproved {
		t.Errorf("₹0 PO after submit: state %q, want approved", sub.State)
	}
	if got, want := p2Warnings(t, env, env.Priya, sub.ID), []string{"Price is ₹0 on line 1."}; !slices.Equal(got, want) {
		t.Errorf("submitted ₹0 PO: OrderWarnings = %q, want %q", got, want)
	}
}
