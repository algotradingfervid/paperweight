package goodsreceipt_test

import (
	"cmp"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
)

// GRN-001, paging (AD-12 ruling on UX doc §5.2: page is 1-based, 25 rows a
// page): 26 approved POs with pending on METRO-SITE, readable by restricted
// Suresh. Vendors cycle, so the ruled order (vendor name, then PO number)
// differs from id order. Page 1 is the first 25 in that order and page 2 is
// exactly the 26th.
func TestGRN_POPickerPaging(t *testing.T) {
	env := grntest.New(t)
	vendors := []struct{ code, name string }{
		{"SHREE", "Shree Steel Traders"}, {"BHARAT", "Bharat Tools"}, {"RAJKOT", "Rajkot Castings"},
	}
	want := make([]goodsreceipt.POChoice, 0, 26)
	for i := range 26 {
		v := vendors[i%len(vendors)]
		po := g6Order(t, env, map[string]string{"vendor_id": g1MasterID(t, env.DB, "party", v.code)})
		want = append(want, g6Choice(t, env, po, v.name, 1))
	}
	slices.SortFunc(want, func(a, b goodsreceipt.POChoice) int {
		return cmp.Or(cmp.Compare(a.VendorName, b.VendorName), cmp.Compare(a.Display, b.Display))
	})
	if slices.IsSortedFunc(want, func(a, b goodsreceipt.POChoice) int { return cmp.Compare(a.ID, b.ID) }) {
		t.Fatalf("setup: ruled order %v is id order", want)
	}

	g6WantChoices(t, "page 1", g6Choices(t, env, "page 1", false, "", 1), want[:25]...)
	g6WantChoices(t, "page 2", g6Choices(t, env, "page 2", false, "", 2), want[25])
}
