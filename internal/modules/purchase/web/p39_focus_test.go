package purchaseweb

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/ui"
)

// P39 · the purchase action pages we draw ourselves are focus pages (CD-352
// (4)): Hand over, Send, Expected on and Create PO from request have no
// bottom nav; the purchase home keeps it.

// p39Get is GET path as p inside a signed-in shell, as internal/app's shell
// middleware gives every signed-in page.
func p39Get(t *testing.T, w *p15dWeb, p auth.Principal, path string) (int, *goquery.Document) {
	t.Helper()
	ctx := ui.WithShell(auth.WithPrincipal(context.Background(), p), ui.Shell{AppName: "P39", User: p.Name, Path: path, SignedIn: true})
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	w.h.ServeHTTP(rec, req)
	d, err := goquery.NewDocumentFromReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("GET %s: parse body: %v", path, err)
	}
	return rec.Code, d
}

// CD-352 (4): each action page answers 200 for Priya, who may act, with no
// bottom nav and no room kept for one; the purchase home for Priya in the
// same shell has the nav, so the shell does draw it.
func TestPO_ActionPagesHaveNoBottomNav(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	draft := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	po := purchasetest.NewOrder(t, env)
	pr := w.prA(t)

	code, home := p39Get(t, w, env.Priya, "/modules/purchase")
	if code != http.StatusOK || home.Find("nav.bottomnav").Length() != 1 || !home.Find("body").HasClass("with-bottomnav") {
		t.Fatalf("home = %d with %d bottom navs; want 200 with one (the control)", code, home.Find("nav.bottomnav").Length())
	}

	for name, path := range map[string]string{
		"hand over":   orderPath(draft.ID, "/handover"),
		"send":        orderPath(po.ID, "/send"),
		"expected on": orderPath(po.ID, "/expected"),
		"create PO":   p15dOrderPath(pr.ID),
	} {
		code, d := p39Get(t, w, env.Priya, path)
		if code != http.StatusOK || d.Find("h1").Length() != 1 {
			t.Errorf("%s: GET %s = %d with %d h1; want 200 with the page", name, path, code, d.Find("h1").Length())
			continue
		}
		if n := d.Find("nav.bottomnav").Length(); n != 0 {
			t.Errorf("%s: %d bottom navs, want none (focus page)", name, n)
		}
		if d.Find("body").HasClass("with-bottomnav") {
			t.Errorf("%s: body keeps room for a bottom nav", name)
		}
	}
}
