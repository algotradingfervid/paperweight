package app_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
)

// UX 5.10 through the app: the htmx redraw's form posts the session's CSRF
// token, and a refused post without htmx returns to the panel (PRG).
func TestApp_CommentsRedrawAndRefusal(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	priya := w.signedIn(priyaEmail)
	id := priya.newOrder(m, "PRJ-METRO", "100.00")
	path := "/modules/comments/" + fixture.Order + "/" + idText(id)

	r := priya.postHeaders(path, url.Values{"body": {"Checked the rate."}}, map[string]string{"HX-Request": "true"})
	if r.status != http.StatusOK {
		t.Fatalf("htmx POST %s = %d, want 200; body %q", path, r.status, r.body)
	}
	if got := r.doc.Find(`form input[name="csrf_token"]`).AttrOr("value", ""); got == "" || got != priya.csrf {
		t.Errorf("redrawn form's csrf_token = %q, want the session's %q", got, priya.csrf)
	}

	if r := priya.post(path, url.Values{"body": {"  "}}); r.status != http.StatusSeeOther || r.location() != orderURL(id)+"#comments" {
		t.Errorf("no-script refused POST = %d Location %q, want 303 to %q", r.status, r.location(), orderURL(id)+"#comments")
	}
	if n := w.count(`SELECT count(*) FROM module_comment WHERE record_id = ?`, id); n != 1 {
		t.Errorf("comments = %d, want 1", n)
	}
}
