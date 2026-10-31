package purchaseweb_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	purchaseweb "github.com/vardhan/internaltools/internal/modules/purchase/web"
)

// p1Routes is every module route P1 mounts against a 501 stub (plan P1 Do 3),
// as "METHOD pattern".
var p1Routes = []string{
	"GET /modules/purchase",
	"GET /modules/purchase/q/{queue}",
	"GET /modules/purchase/orders/{id}/handover",
	"POST /modules/purchase/orders/{id}/handover",
	"GET /modules/purchase/orders/{id}/send",
	"POST /modules/purchase/orders/{id}/send",
	"POST /modules/purchase/orders/{id}/send/undo",
	"POST /modules/purchase/orders/{id}/copy",
	"GET /modules/purchase/orders/{id}/expected",
	"POST /modules/purchase/orders/{id}/expected",
	"POST /modules/purchase/orders/{id}/expected/clear",
	"GET /modules/purchase/requests/{id}/order",
	"POST /modules/purchase/requests/{id}/order",
	"GET /reports/purchase/pending",
	"GET /reports/purchase/pending/filter",
	"GET /reports/purchase/pending/export.csv",
}

// MOD-PO P1 Do 3 (review focus: a stub route answers 501, never success;
// CD-256 one stub file per owner; CD-283 Deps.ReceiveLink): Routes registers
// exactly P1's module routes, with no middleware (internal/app puts AUTH's
// guard on each, CD-46), and every one answers 501 Not Implemented.
func TestPurchaseWeb_RoutesStubbed(t *testing.T) {
	env := purchasetest.New(t)
	r := chi.NewRouter()
	purchaseweb.Routes(purchaseweb.Deps{
		Service: env.Service, Approvals: env.Approvals, Comments: env.Comments, Sessions: scs.New(),
		CSRF: func(*http.Request) string { return "token" },
		ReceiveLink: func(context.Context, auth.Principal, int64) (string, bool) {
			return "", false
		},
	})(r)

	var got []string
	if err := chi.Walk(r, func(method, route string, _ http.Handler, mws ...func(http.Handler) http.Handler) error {
		got = append(got, method+" "+route)
		if len(mws) != 0 {
			t.Errorf("%s %s has %d middlewares, want none (internal/app's mount drops them)", method, route, len(mws))
		}
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	want := slices.Clone(p1Routes)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("routes =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	fill := strings.NewReplacer("{id}", "1", "{queue}", "to-send")
	for _, rt := range p1Routes {
		method, pattern, _ := strings.Cut(rt, " ")
		req := httptest.NewRequestWithContext(t.Context(), method, fill.Replace(pattern), strings.NewReader("version=1"))
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotImplemented {
			t.Errorf("%s = %d, want 501 (a stub never answers success)", rt, rec.Code)
		}
	}
}
