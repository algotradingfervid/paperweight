package app_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/masters/expensetype"
)

// RR1b finding 3: in header search a master's row shows the Disabled badge
// only when the record is disabled; an enabled record shows no badge, never
// an empty one.
func TestApp_SearchDisabledBadgeOnlyWhenDisabled(t *testing.T) {
	w := newWorld(t)
	admin, _, _ := w.canonical()
	ctx := context.Background()
	disabled := map[string]bool{}
	for i := range 3 {
		s, err := w.app.Masters().ExpenseType.Create(ctx, admin, expensetype.Input{Code: "ZEB" + strconv.Itoa(i), Name: "Zebra " + strconv.Itoa(i)})
		if err != nil {
			t.Fatalf("create expense type: %v", err)
		}
		href := "/m/expense-type/" + strconv.FormatInt(s.ID, 10)
		disabled[href] = i == 1
		if i == 1 {
			if err := w.app.Masters().ExpenseType.SetDisabled(ctx, admin, s.ID, s.Version, true); err != nil {
				t.Fatalf("disable: %v", err)
			}
		}
	}
	r := w.signedIn(adminEmail).get("/search?q=Zebra")
	if r.status != http.StatusOK {
		t.Fatalf("GET /search = %d, want 200", r.status)
	}
	seen := 0
	r.doc.Find("a.datalist-link").Each(func(_ int, a *goquery.Selection) {
		off, ok := disabled[a.AttrOr("href", "")]
		if !ok {
			return
		}
		seen++
		var words []string
		a.Find(".badge").Each(func(_ int, b *goquery.Selection) { words = append(words, strings.TrimSpace(b.Text())) })
		want := []string(nil)
		if off {
			want = []string{"Disabled"}
		}
		if strings.Join(words, "|") != strings.Join(want, "|") || len(words) != len(want) {
			t.Errorf("%s (disabled %v): badges %q, want %q", a.AttrOr("href", ""), off, words, want)
		}
	})
	if seen != 3 {
		t.Errorf("%d of the 3 expense types listed", seen)
	}
}

// RR1b finding 4: every auth form with a required control is novalidate,
// so a blank required field reaches the server and its error summary
// instead of the browser's own bubble: first-run setup, sign-in, change
// password, new user and reset password. The roles forms aren't (their
// blank name has no redraw, RR1b fix 1; TestApp_NoValidateFormsRedraw).
func TestApp_RequiredFormsNoValidate(t *testing.T) {
	w := newWorld(t)
	check := func(path string, r *response) {
		t.Helper()
		if r.status != http.StatusOK {
			t.Fatalf("GET %s = %d", path, r.status)
		}
		n := 0
		r.doc.Find("form:has([required])").Each(func(_ int, f *goquery.Selection) {
			n++
			if _, ok := f.Attr("novalidate"); !ok {
				t.Errorf("%s: form %s has a required control and no novalidate", path, f.AttrOr("action", ""))
			}
		})
		if n == 0 {
			t.Errorf("%s: no form with a required control to check", path)
		}
	}
	check("/setup", w.browser().get("/setup"))
	w.canonical()
	other := int64(w.count(`SELECT id FROM users WHERE email = ?`, priyaEmail))
	check("/signin", w.browser().get("/signin"))
	a := w.signedIn(adminEmail)
	for _, p := range []string{"/account/password", "/users/new", "/users/" + strconv.FormatInt(other, 10) + "/password"} {
		check(p, a.get(p))
	}
}

// RR1b fix 1: a form that is novalidate lets a blank required field post,
// so its server answers with a 4xx redraw of the same page and its error
// summary (sign-in: its refusal banner), never a problem page ("The form
// could not be read…"). Every novalidate form on the auth pages and the
// roles page is posted as drawn, its required fields blank.
func TestApp_NoValidateFormsRedraw(t *testing.T) {
	w := newWorld(t)
	check := func(b *browser, path string) {
		t.Helper()
		page := b.get(path)
		if page.status != http.StatusOK {
			t.Fatalf("GET %s = %d", path, page.status)
		}
		title := strings.TrimSpace(page.doc.Find("title").Text())
		page.doc.Find("form[novalidate]").Each(func(_ int, f *goquery.Selection) {
			action := f.AttrOr("action", "")
			form := url.Values{}
			f.Find("input[name], select[name], textarea[name]").Each(func(_ int, in *goquery.Selection) {
				v := in.AttrOr("value", "")
				if _, req := in.Attr("required"); req {
					v = ""
				}
				form.Add(in.AttrOr("name", ""), v)
			})
			r := b.post(action, form)
			what := path + " form " + action
			if r.status < 400 || r.status > 499 {
				t.Errorf("%s: blank post = %d, want a 4xx redraw", what, r.status)
			}
			if strings.Contains(r.body, "could not be read") {
				t.Errorf("%s: blank post answers the problem page %q", what, r.pageText())
			}
			if got := strings.TrimSpace(r.doc.Find("title").Text()); got != "Error: "+title {
				t.Errorf("%s: blank post draws %q, want the same page %q", what, got, "Error: "+title)
			}
			// Sign-in's summary is its refusal banner, which marks no field
			// (UI v2 §5.3); every other form lists its field errors.
			if r.doc.Find(".error-summary li").Length() == 0 && (path != "/signin" || strings.TrimSpace(r.doc.Find(`#signin-error [role="alert"]`).Text()) == "") {
				t.Errorf("%s: blank post has no error summary", what)
			}
		})
	}
	check(w.browser(), "/setup")
	w.canonical()
	other := int64(w.count(`SELECT id FROM users WHERE email = ?`, priyaEmail))
	check(w.browser(), "/signin")
	a := w.signedIn(adminEmail)
	for _, p := range []string{"/account/password", "/users/new", "/users/" + strconv.FormatInt(other, 10) + "/password", "/roles"} {
		check(a, p)
	}
}
