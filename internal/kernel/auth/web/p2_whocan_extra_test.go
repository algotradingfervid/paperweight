package authweb_test

import (
	"html"
	"net/http"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
)

// An unknown right (not in auth.AllRights, matched case-sensitively) is
// errBadForm, not a page.
func TestWhoCan_UnknownRight(t *testing.T) {
	h := webtest.New(t, authtest.NewWorld(t))
	admin := signedIn(t, h, "admin@vardhan.test")
	for _, right := range []string{"foo", "Approve"} {
		r := admin.Get(t19Path + "?doctype=authtest.Order&right=" + right)
		wantStatus(t, "GET who-can Order "+right, r, http.StatusBadRequest)
		if !strings.Contains(html.UnescapeString(r.Body), auth.ErrBadForm.Error()) {
			t.Fatalf("GET who-can Order %s: want %q in body %q", right, auth.ErrBadForm.Error(), r.Body)
		}
	}
}
