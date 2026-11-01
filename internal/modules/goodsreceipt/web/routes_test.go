package grnweb_test

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	grnweb "github.com/vardhan/internaltools/internal/modules/goodsreceipt/web"
)

// g1Routes are the module routes G1 mounts, each with the right its guard
// needs on goodsreceipt.Receipt (plan G1 Do 4; CD-145: the CSV routes are
// guarded by read and their handlers check export).
var g1Routes = []struct {
	method, pattern, path string
	right                 auth.Right
}{
	{http.MethodPost, "/modules/goodsreceipt/receipts/{id}/bill", "/modules/goodsreceipt/receipts/1/bill", auth.RightWrite},
	{http.MethodGet, "/reports/grn/bills", "/reports/grn/bills", auth.RightRead},
	{http.MethodGet, "/reports/grn/items", "/reports/grn/items", auth.RightRead},
	{http.MethodGet, "/reports/grn/bills/export.csv", "/reports/grn/bills/export.csv", auth.RightRead},
	{http.MethodGet, "/reports/grn/items/export.csv", "/reports/grn/items/export.csv", auth.RightRead},
}

// builtRoutes are the module routes built since G1, mounted and guarded like
// g1Routes but no longer stubs: S1b's PO search, guarded by read on the GRN
// (CD-323 (2)); its rows are read-scoped on the PO by the service. S4's
// reports entry and filter pages, guarded by read (UX doc §5.10 to §5.12).
var builtRoutes = []struct {
	method, pattern, path string
	right                 auth.Right
}{
	{http.MethodGet, "/modules/goodsreceipt/po-search", "/modules/goodsreceipt/po-search", auth.RightRead},
	{http.MethodGet, "/reports/grn", "/reports/grn", auth.RightRead},
	{http.MethodGet, "/reports/grn/bills/filter", "/reports/grn/bills/filter", auth.RightRead},
	{http.MethodGet, "/reports/grn/items/filter", "/reports/grn/items/filter", auth.RightRead},
}

// MOD-GRN G1 Do 4 (CD-46, CD-256; review focus "a stub never answers
// success"): Routes registers exactly the plan's routes with no middleware.
// Every G1 route is built now (G7, G9, S4), so none answers 501; each
// route's own tests check what it answers (AD-13).
func TestGRNWeb_RoutesAnswer501(t *testing.T) {
	r := chi.NewRouter()
	grnweb.Routes(grnweb.Deps{})(r)

	var got, want []string
	if err := chi.Walk(r, func(method, route string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		got = append(got, method+" "+route)
		if len(mws) != 0 {
			t.Errorf("%s %s has %d middlewares, want none (internal/app's mount drops them)", method, route, len(mws))
		}
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, rt := range append(slices.Clone(g1Routes), builtRoutes...) {
		want = append(want, rt.method+" "+rt.pattern)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("routes =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// MOD-GRN G1 Do 4 (CD-46, CD-145): Guard gives each route the GRN and its
// right, and no route outside the package. On the real auth.Service, which
// the app's guard asks with no record, Kavya (no GRN rights) is refused on
// every route, Priya (read, print, export) is refused the bill POST's write
// and let through the reports and CSVs, and Suresh (Stores) passes all.
func TestGRNWeb_Guards(t *testing.T) {
	env := grntest.New(t)
	for _, rt := range append(slices.Clone(g1Routes), builtRoutes...) {
		dt, right, ok := grnweb.Guard(rt.pattern)
		if !ok || dt != goodsreceipt.DocType || right != rt.right {
			t.Errorf("Guard(%s) = %q %q %v, want %q %q true", rt.pattern, dt, right, ok, goodsreceipt.DocType, rt.right)
			continue
		}
		for _, c := range []struct {
			who   string
			p     auth.Principal
			allow bool
		}{
			{"kavya (Masters Manager)", env.Kavya, false},
			{"priya (Purchase User)", env.Priya, right == auth.RightRead},
			{"suresh (Stores)", env.Suresh, true},
		} {
			err := env.Auth.Can(t.Context(), c.p, dt, right, nil)
			var de *auth.DeniedError
			switch {
			case c.allow && err != nil:
				t.Errorf("%s on %s: %v, want allowed", c.who, rt.pattern, err)
			case !c.allow && !errors.As(err, &de):
				t.Errorf("%s on %s: err = %v, want AUTH's 403 (*auth.DeniedError)", c.who, rt.pattern, err)
			}
		}
	}
	if _, _, ok := grnweb.Guard("/reports/grn/other"); ok {
		t.Errorf("Guard of an unregistered route: ok = true, want false")
	}
}
