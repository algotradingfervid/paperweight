package app_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/modkit"
)

// fixture.Order opts in to comments here, so the comment route has a real
// document to post on (M0-14).
var _ = func() bool {
	modkit.RegisterComments(fixture.Order)
	return true
}()

// modkit's routes are mounted behind the signed-in guard (M0-6). The business
// settings page (M0-7) is Administrator-only by its own check; the comments
// route is M0-14's.
func TestApp_ModkitRoutesMounted(t *testing.T) {
	const (
		settingsPath = "/settings/business"
		msgAdminOnly = "Only an administrator can change business settings."
		terms        = "Payment within 30 days of the invoice."
	)
	w := newWorld(t)
	_, _, m := w.canonical()

	guest := w.browser()
	if r := guest.get(settingsPath); r.status != http.StatusSeeOther || !strings.HasPrefix(r.location(), "/signin") {
		t.Errorf("guest GET %s = %d Location %q, want 303 to /signin", settingsPath, r.status, r.location())
	}

	b := w.signedIn(adminEmail)
	page := b.get(settingsPath)
	if page.status != http.StatusOK || strings.TrimSpace(page.doc.Find("h1").First().Text()) != "Business settings" {
		t.Fatalf("GET %s as Administrator = %d h1 %q, want 200 \"Business settings\"; page %q",
			settingsPath, page.status, page.doc.Find("h1").First().Text(), page.pageText())
	}
	version := page.doc.Find(`input[name="version"]`).AttrOr("value", "")
	if version != "1" {
		t.Errorf("settings page version = %q, want 1", version)
	}
	audits := w.count(`SELECT count(*) FROM audit_log`)
	saved := b.post(settingsPath, url.Values{"version": {version}, "locked_up_to": {"31-03-2026"}, "default_po_terms": {terms}})
	if saved.status != http.StatusSeeOther || saved.location() != settingsPath {
		t.Errorf("POST %s as Administrator = %d Location %q (%q), want 303 to %s",
			settingsPath, saved.status, saved.location(), saved.pageText(), settingsPath)
	}
	if got := w.count(`SELECT count(*) FROM audit_log`); got != audits+1 {
		t.Errorf("audit_log rows after the save = %d, want %d (one entry through audit.Default)", got, audits+1)
	}
	after := b.get(settingsPath)
	for name, want := range map[string]string{"version": "2", "locked_up_to": "31-03-2026"} {
		if got := after.doc.Find(`input[name="`+name+`"]`).AttrOr("value", ""); got != want {
			t.Errorf("settings page after the save: %s = %q, want %q", name, got, want)
		}
	}
	if !strings.Contains(after.pageText(), terms) {
		t.Errorf("settings page after the save doesn't show the terms %q", terms)
	}

	deepak := w.signedIn(deepakEmail)
	if r := deepak.get(settingsPath); r.status != http.StatusForbidden || r.problem() != msgAdminOnly {
		t.Errorf("GET %s as Deepak = %d %q, want 403 %q", settingsPath, r.status, r.problem(), msgAdminOnly)
	}
	if r := deepak.post(settingsPath, url.Values{"version": {"2"}, "locked_up_to": {""}, "default_po_terms": {""}}); r.status != http.StatusForbidden || r.alert() != msgAdminOnly {
		t.Errorf("POST %s as Deepak = %d %q, want 403 %q", settingsPath, r.status, r.alert(), msgAdminOnly)
	}
	if got := w.count(`SELECT version FROM business_settings WHERE id = 1`); got != 2 {
		t.Errorf("business_settings version after Deepak's POST = %d, want 2 (unchanged)", got)
	}

	// Comments (PO spec §22): mounted with AUTH's guard and CSRF, read checked
	// on the record; a DocType that didn't opt in answers AUTH's 404.
	if r := b.post("/modules/comments/purchase.Order/1", url.Values{"body": {"Is this still needed?"}}); r.status != http.StatusNotFound || r.alert() != auth.MsgNotFound {
		t.Errorf("POST comment on an unregistered DocType = %d %q, want 404 %q", r.status, r.alert(), auth.MsgNotFound)
	}

	priya := w.signedIn(priyaEmail)
	id := priya.newOrder(m, "PRJ-METRO", "100.00")
	path := "/modules/comments/" + fixture.Order + "/" + idText(id)
	comments := func() int {
		return w.count(`SELECT count(*) FROM module_comment WHERE doctype = ? AND record_id = ?`, fixture.Order, id)
	}
	if r := priya.post(path, url.Values{"body": {"Checked the rate with the vendor."}}); r.status != http.StatusSeeOther || r.location() != orderURL(id)+"#comments" {
		t.Errorf("POST %s = %d Location %q, want 303 to %q", path, r.status, r.location(), orderURL(id)+"#comments")
	}
	if n := comments(); n != 1 {
		t.Errorf("comments after Priya's post = %d, want 1", n)
	}

	// No CSRF token: AUTH's refusal, nothing saved.
	noToken := *priya
	noToken.csrf = ""
	if r := noToken.post(path, url.Values{"body": {"Without a token."}}); r.status != http.StatusForbidden || r.alert() != auth.ErrCSRF.Msg {
		t.Errorf("POST %s without CSRF = %d %q, want 403 %q", path, r.status, r.alert(), auth.ErrCSRF.Msg)
	}
	// No read on orders (Deepak): the same 404 as a missing record.
	if r := deepak.post(path, url.Values{"body": {"Can I see this?"}}); r.status != http.StatusNotFound || r.alert() != auth.MsgNotFound {
		t.Errorf("POST %s as Deepak = %d %q, want 404 %q", path, r.status, r.alert(), auth.MsgNotFound)
	}
	if n := comments(); n != 1 {
		t.Errorf("comments after refused posts = %d, want 1", n)
	}
}

// M0-7: the admin nav has "Business settings" beside Numbering; nobody else
// sees it.
func TestApp_BusinessSettingsNav(t *testing.T) {
	const href = "/settings/business"
	w := newWorld(t)
	w.canonical()

	navLinks := func(b *browser) []*goquery.Selection {
		var out []*goquery.Selection
		b.get("/approvals").doc.Find(`.nav-list a`).Each(func(_ int, s *goquery.Selection) { out = append(out, s) })
		return out
	}

	links := navLinks(w.signedIn(adminEmail))
	at, numbering := -1, -1
	for i, s := range links {
		switch s.AttrOr("href", "") {
		case href:
			at = i
			if got := strings.Join(strings.Fields(s.Text()), " "); got != "Business settings" {
				t.Errorf("Administrator's nav item %s reads %q, want %q", href, got, "Business settings")
			}
		case "/settings/numbering":
			numbering = i
		}
	}
	if at < 0 {
		t.Fatalf("Administrator's nav has no %s", href)
	}
	if numbering < 0 || (at != numbering+1 && at != numbering-1) {
		t.Errorf("Administrator's nav: %s at %d, Numbering at %d; want them side by side", href, at, numbering)
	}

	for _, email := range []string{raviEmail, priyaEmail, deepakEmail, "meena@vardhan.test"} {
		for _, s := range navLinks(w.signedIn(email)) {
			if s.AttrOr("href", "") == href {
				t.Errorf("%s's nav has the administrator item %s", email, href)
			}
		}
	}
}
