package purchaseweb_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// PO-037 (PO spec §13): the CSV takes the report's vendor, project, site
// and item ids from the query, ANDed; an id that isn't a number is AUTH's
// bad form (400), with no CSV.
func TestPO_PendingExportFilters(t *testing.T) {
	env := purchasetest.New(t)
	shree := purchasetest.NewOrder(t, env)
	bharat := purchasetest.NewOrder(t, env, purchasetest.OrderHeader(map[string]string{"vendor_id": p9ID(t, env, "party", "BHARAT")}))
	tech := p9TechparkOrder(t, env)
	all := []string{shree.Number, bharat.Number, tech}
	slices.Sort(all)

	for _, c := range []struct {
		query string
		want  []string
	}{
		{"", all},
		{"?vendor=" + p9ID(t, env, "party", "BHARAT"), []string{bharat.Number}},
		{"?project=" + p9ID(t, env, "project", "PRJ-TECHPARK"), []string{tech}},
		{"?site=" + p9ID(t, env, "site", "METRO-SITE"), []string{bharat.Number, shree.Number}},
		{"?item=" + p9ID(t, env, "item", "WIRE-18G"), nil},
		{"?vendor=" + p9ID(t, env, "party", "SHREE") + "&site=" + p9ID(t, env, "site", "TECH-SITE"), []string{tech}},
	} {
		got := p9Numbers(p9Records(t, "Admin's CSV"+c.query, p9Get(t, env, env.Admin, p9ExportPath+c.query)))
		want := slices.Clone(c.want)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("CSV%s rows = %q, want %q", c.query, got, want)
		}
	}

	for _, key := range []string{"vendor", "project", "site", "item"} {
		rec := p9Get(t, env, env.Admin, p9ExportPath+"?"+key+"=x")
		if rec.Code != http.StatusBadRequest || rec.Header().Get("Content-Type") == "text/csv; charset=utf-8" {
			t.Errorf("CSV ?%s=x = %d %q, want 400 and no CSV", key, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
}
