package expenseweb_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/expense"
)

// htmxPost is post with htmx's HX-Request header, as the paid page's form
// sends it.
func (e *webEnv) htmxPost(t *testing.T, email string, id int64, route string, f url.Values) *httptest.ResponseRecorder {
	t.Helper()
	path := "/modules/expense/advances/" + strconv.FormatInt(id, 10) + "/" + route
	req := httptest.NewRequestWithContext(auth.WithPrincipal(context.Background(), e.P(t, email)),
		http.MethodPost, path, strings.NewReader(f.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

// TestAdv_PaidHTMXRedirects: the paid page's form posts through htmx, which
// follows a 303 with a fragment request that never shows the flash, and
// keeps the paid page's address. So an htmx post of Mark as paid or Undo
// paid answers 200 with HX-Redirect to the advance page, which htmx loads
// in full with the flash (AD ruling on S6, wizard.Respond's Finish); the
// plain post's 303 is TestAdv_MarkAsPaidRoutes'. The flash on the advance
// page is e2e's TestE2E_PaidFlashAfterHTMXPost.
func TestAdv_PaidHTMXRedirects(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	adv := e.approvedAdvance(t)
	page := "/doc/" + expense.AdvanceDocType + "/" + strconv.FormatInt(adv.ID, 10)
	for _, route := range []string{"paid", "unpaid"} {
		v, _ := e.stored(t, adv.ID)
		f := form(v, "")
		if route == "paid" {
			f = form(v, "21-10-2026")
		}
		rec := e.htmxPost(t, webMeena, adv.ID, route, f)
		if rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") != page || rec.Header().Get("Location") != "" {
			t.Errorf("htmx POST %s: status %d, HX-Redirect %q, Location %q; want 200, %q, none (body %q)",
				route, rec.Code, rec.Header().Get("HX-Redirect"), rec.Header().Get("Location"), page, rec.Body.String())
		}
		if after, _ := e.stored(t, adv.ID); after != v+1 {
			t.Errorf("htmx POST %s: version %d, want %d (saved)", route, after, v+1)
		}
	}
}
