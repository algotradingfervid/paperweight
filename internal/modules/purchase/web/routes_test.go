package purchaseweb_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
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

// MOD-PO P1 Do 3 (CD-46, CD-256, CD-283; AD-13 ruling on the route tests):
// Routes registers exactly the plan's module routes, with no middleware
// (internal/app puts AUTH's guard on each). Each route is mounted: for an
// Administrator on real records it answers neither 404 nor 405, and no
// handler panics. What a route answers is its owner's test; the 501 of a stub
// is pinned next to one stub (stubs_web_p9_test.go).
func TestPurchaseWeb_RoutesStubbed(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved())
	pr := purchasetest.NewRequest(t, env, purchasetest.StopAtApproved())
	sessions := scs.New()
	r := chi.NewRouter()
	purchaseweb.Routes(purchaseweb.Deps{
		Service: env.Service, Approvals: env.Approvals, Comments: env.Comments, Sessions: sessions,
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

	h := sessions.LoadAndSave(r)
	for _, rt := range p1Routes {
		method, pattern, _ := strings.Cut(rt, " ")
		id := strconv.FormatInt(po.ID, 10)
		if strings.Contains(pattern, "/requests/") {
			id = strconv.FormatInt(pr.ID, 10)
		}
		path := strings.NewReplacer("{id}", id, "{queue}", "to-send").Replace(pattern)
		form := url.Values{"version": {strconv.Itoa(po.Version)}}
		req := httptest.NewRequestWithContext(auth.WithPrincipal(t.Context(), env.Admin), method, path, strings.NewReader(form.Encode()))
		if method == http.MethodPost {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		rec := httptest.NewRecorder()
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("%s %s panicked: %v", method, path, p)
				}
			}()
			h.ServeHTTP(rec, req)
		}()
		if rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
			t.Errorf("%s %s = %d, want the route mounted (neither 404 nor 405)", method, path, rec.Code)
		}
	}
}
