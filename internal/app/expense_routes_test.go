package app_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
)

// Plan MOD-EXP E1 Do 5: every expense module route is behind AUTH's right
// guard on the DocType and right below, found the way TestApp_RouteGuards
// finds guards. The CSV handlers check export, and Mark as paid and Undo paid
// check cancel on the record, in the handler (CD-145, spec §4).
func TestApp_ExpenseRouteGuards(t *testing.T) {
	type guard struct {
		docType string
		right   auth.Right
	}
	claimRead := guard{"expense.Claim", auth.RightRead}
	advanceRead := guard{"expense.Advance", auth.RightRead}
	want := map[string]guard{
		"POST /modules/expense/claims/{id}/receipt":  {"expense.Claim", auth.RightWrite},
		"GET /reports/expense/open":                  claimRead,
		"GET /reports/expense/open/export.csv":       claimRead,
		"GET /reports/expense/lines":                 claimRead,
		"GET /reports/expense/lines/export.csv":      claimRead,
		"GET /reports/expense/advances":              advanceRead,
		"GET /reports/expense/advances/export.csv":   advanceRead,
		"GET /modules/expense/advances/{id}/paid":    advanceRead,
		"POST /modules/expense/advances/{id}/paid":   advanceRead,
		"GET /modules/expense/advances/{id}/unpaid":  advanceRead,
		"POST /modules/expense/advances/{id}/unpaid": advanceRead,
	}

	w := newWorld(t)
	got := map[string]guard{}
	err := chi.Walk(w.routes(), func(method, route string, h http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/modules/expense/") && !strings.HasPrefix(route, "/reports/expense/") {
			return nil
		}
		key := method + " " + route
		g, ok := h.(*authweb.Guarded)
		if !ok {
			t.Errorf("%s is not an *authweb.Guarded handler (%T)", key, h)
			return nil
		}
		if g.Req.Kind != authweb.KindRight {
			t.Errorf("%s: guard kind %d, want KindRight", key, g.Req.Kind)
		}
		got[key] = guard{g.Req.DocType, g.Req.Right}
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	if len(got) < len(want) {
		t.Errorf("found %d guarded expense routes, want at least %d: an empty walk proves nothing", len(got), len(want))
	}
	for route, g := range want {
		if have, ok := got[route]; !ok {
			t.Errorf("%s: not mounted", route)
		} else if have != g {
			t.Errorf("%s: guard %s %s, want %s %s", route, have.docType, have.right, g.docType, g.right)
		}
	}
	for route := range got {
		if _, ok := want[route]; !ok {
			t.Errorf("%s: mounted, but plan E1 Do 5 names no such route", route)
		}
	}
}
