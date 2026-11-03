package purchaseweb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// R9 (§5.13 item 2, PO review): Mark as sent lands on the PO with the success
// block: its heading and who it went to, after the flash's own text, in place
// of the banner. The flash is taken once, so a reload shows no block; a
// hand-typed ?sent=1 with no flash shows none, nor does Mark as not sent.
func TestPO_MarkAsSentSuccessBlock(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	po := w.order(t, doctype.StateApproved)
	page := p15dDoc(purchase.DocType, po.ID)
	if n := w.get(t, env.Priya, page+"?sent=1").doc.Find(".success-block").Length(); n != 0 {
		t.Errorf("?sent=1 before the action: %d blocks, want none", n)
	}

	// One browser: the session cookie carries the flash from POST to GET.
	var jar []*http.Cookie
	serve := func(method, path string, form url.Values) *goquery.Document {
		req := httptest.NewRequestWithContext(auth.WithPrincipal(context.Background(), env.Priya), method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for _, c := range jar {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		w.h.ServeHTTP(rec, req)
		if cs := rec.Result().Cookies(); len(cs) > 0 {
			jar = cs
		}
		d, err := goquery.NewDocumentFromReader(rec.Body)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	serve(http.MethodPost, orderPath(po.ID, "/send"), url.Values{"version": {strconv.Itoa(0)}, "sent_on": {types.FormatDate(types.Today(env.Clock))}, "sent_via": {"email"}})
	first := serve(http.MethodGet, page+"?sent=1", nil)
	block := first.Find(`#main .success-block[role="status"]`)
	if got := p15dCollapse(block.Find("h2").Text()); got != "Sent to the vendor" {
		t.Errorf("heading = %q; main %.400s", got, p15dCollapse(first.Find("#main").Text()))
	}
	number := p15dCollapse(first.Find("h1").Text())
	want := number + " marked as sent. It went to Shree Steel Traders."
	if got := p15dCollapse(block.Find(".success-block__text").Text()); got != want {
		t.Errorf("line = %q, want %q", got, want)
	}
	if n := first.Find("#main .banner--success").Length(); n != 0 {
		t.Errorf("%d success banners beside the block", n)
	}
	if n := serve(http.MethodGet, page+"?sent=1", nil).Find(".success-block").Length(); n != 0 {
		t.Errorf("reload: %d blocks, want none: the flash is taken once", n)
	}

	// A flash on a sent PO without ?sent=1 stays a banner, and so does one on
	// a PO that isn't sent with ?sent=1 (Mark as not sent's, by a typed URL).
	for _, c := range []struct {
		name, path, version, query, flash string
	}{
		{"sent, no query", orderPath(po.ID, "/send"), "1", "", number + " marked as sent."},
		{"not sent, query", orderPath(po.ID, "/send/undo"), "2", "?sent=1", number + " marked as not sent."},
	} {
		serve(http.MethodPost, c.path, url.Values{"version": {c.version}, "sent_on": {types.FormatDate(types.Today(env.Clock))}, "sent_via": {"email"}})
		got := serve(http.MethodGet, page+c.query, nil)
		if n := got.Find(".success-block").Length(); n != 0 {
			t.Errorf("%s: %d blocks, want none", c.name, n)
		}
		if b := p15dCollapse(got.Find("#main .banner--success").Text()); !strings.Contains(b, c.flash) {
			t.Errorf("%s: banner = %q, want %q", c.name, b, c.flash)
		}
	}
}

// The block names the vendor only when the viewer may see it: with the vendor
// hidden, it says what happened and nothing about who (CD-319).
func TestPO_SentSuccessHonoursHide(t *testing.T) {
	dt, _ := doctype.Lookup(purchase.DocType)
	v := &docscreen.DocView{Display: map[string]string{"vendor_id": "SHREE Shree Steel Traders"}}
	hide := func(_ docscreen.Ctx, f doctype.Field) bool { return f.Name == "vendor_id" }
	for _, c := range []struct {
		name string
		mods map[string]docscreen.Module
		want string
	}{
		{"shown", nil, "It went to Shree Steel Traders."},
		{"hidden", map[string]docscreen.Module{purchase.DocType: {Hide: hide}}, ""},
	} {
		s := sentSuccess(docscreen.Ctx{DT: dt, View: v, Deps: docscreen.Deps{Modules: c.mods}})
		if s.Title != sentHeading || s.Message != c.want {
			t.Errorf("%s: %q %q, want %q %q", c.name, s.Title, s.Message, sentHeading, c.want)
		}
	}
}
