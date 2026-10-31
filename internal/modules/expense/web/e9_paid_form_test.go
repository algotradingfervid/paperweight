package expenseweb_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
)

// The Paid on and id parsing of POST …/paid (plan E9): a blank or malformed
// date is a 422 with its text, a bad id AUTH's 404.
func TestAdv_MarkAsPaidForm(t *testing.T) {
	t.Parallel()
	e := newWebEnv(t)
	adv := e.approvedAdvance(t)
	v, _ := e.stored(t, adv.ID)

	e.wantRefusedPost(t, "blank date", webMeena, adv.ID, "paid", form(v, ""), http.StatusUnprocessableEntity, "Paid on is required.")
	e.wantRefusedPost(t, "bad date", webMeena, adv.ID, "paid", form(v, "31-02-2026"), http.StatusUnprocessableEntity,
		"31-02-2026 is not a valid date. Use dd-mm-yyyy, for example 31-03-2025.")

	for _, route := range []string{"paid", "unpaid"} {
		req := httptest.NewRequestWithContext(auth.WithPrincipal(context.Background(), e.P(t, webMeena)),
			http.MethodPost, "/modules/expense/advances/x/"+route, strings.NewReader(form(v, "21-10-2026").Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		e.router.ServeHTTP(rec, req)
		wantStatus(t, "bad id "+route, rec, http.StatusNotFound, auth.MsgNotFound)
	}
}
