package views_test

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/views"
)

// The real shell has no token before sign-in, so every AUTH form posts the
// token its page was given, whatever the shell holds.
func TestViews_FormsPostThePageToken(t *testing.T) {
	sessions := []auth.SessionInfo{{ID: 1}}
	for name, c := range map[string]templ.Component{
		"login":    views.Default{}.Login(authweb.LoginPage{CSRF: "tok"}),
		"setup":    views.Default{}.Setup(authweb.SetupPage{CSRF: "tok"}),
		"password": views.Default{}.Password(authweb.PasswordPage{CSRF: "tok"}),
		"sessions": views.Default{}.Sessions(authweb.SessionsPage{CSRF: "tok", Sessions: sessions}),
	} {
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(render(t, c)))
		if err != nil {
			t.Fatal(err)
		}
		forms := doc.Find(`form[method="post"]`)
		if forms.Length() == 0 {
			t.Fatalf("%s: no POST form", name)
		}
		forms.Each(func(_ int, f *goquery.Selection) {
			if got := f.Find(`input[name="csrf_token"]`).AttrOr("value", ""); got != "tok" {
				t.Errorf("%s: form %q posts token %q, want tok", name, f.AttrOr("action", ""), got)
			}
		})
	}
}

func TestViews_LoginKeepsNextInTheForm(t *testing.T) {
	body := render(t, views.Default{}.Login(authweb.LoginPage{CSRF: "tok", Next: "/doc/po"}))
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Find(`form input[type="hidden"][name="next"]`).AttrOr("value", ""); got != "/doc/po" {
		t.Errorf("next = %q, want /doc/po", got)
	}
}

// CD-91: the user pages sit in the app shell, with the error in the summary
// and the warnings in a banner, and keep their fields.
func TestViews_UserPagesUseTheShell(t *testing.T) {
	const msg, warn = "Something went wrong.", "Check this."
	u := auth.UserInfo{ID: 7, Name: "Priya Nair", Email: "priya@vardhan.test"}
	for name, c := range map[string]templ.Component{
		"users": views.Default{}.Users(authweb.UsersPage{CSRF: "tok", Error: msg, Warnings: []string{warn}, Users: []auth.UserInfo{u}}),
		"user":  views.Default{}.User(authweb.UserPage{CSRF: "tok", Error: msg, Warnings: []string{warn}, User: u, Version: 3, AccessURL: "/users/7/access"}),
	} {
		body := render(t, c)
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if doc.Find(`main#main`).Length() != 1 {
			t.Errorf("%s: not in the app shell: %q", name, body)
		}
		if !errorInSummary(msg).MatchString(body) {
			t.Errorf("%s: %q is not in the error summary: %q", name, msg, body)
		}
		if got := doc.Find(`main .banner`).Text(); !strings.Contains(got, warn) {
			t.Errorf("%s: warning banner = %q", name, got)
		}
		if got := doc.Find(`main input[name="csrf_token"]`).AttrOr("value", ""); got != "tok" {
			t.Errorf("%s: csrf_token = %q", name, got)
		}
		if !strings.Contains(doc.Find("main").Text(), u.Email) {
			t.Errorf("%s: no email in the page", name)
		}
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(render(t, views.Default{}.User(authweb.UserPage{User: u, Version: 3, AccessURL: "/users/7/access"}))))
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Find(`main input[type="hidden"][name="version"]`).AttrOr("value", ""); got != "3" {
		t.Errorf("version = %q, want 3", got)
	}
	if got := doc.Find(`main a[href="/users/7/access"]`).Text(); got != "Site and project access" {
		t.Errorf("access link = %q", got)
	}
}

// The users list and the access link are 44 px targets (design-system §10):
// each user is a list card, and the access link stands alone like a button.
func TestViews_UserLinksAreTargets(t *testing.T) {
	u := auth.UserInfo{ID: 7, Name: "Priya Nair", Email: "priya@vardhan.test"}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(render(t, views.Default{}.Users(authweb.UsersPage{Users: []auth.UserInfo{u}}))))
	if err != nil {
		t.Fatal(err)
	}
	card := doc.Find(`main a.datalist-link[href="/users/7"]`)
	if card.Length() != 1 || !strings.Contains(card.Text(), u.Name) || !strings.Contains(card.Text(), u.Email) {
		t.Errorf("users: card %d %q, want one list card with the name and email", card.Length(), card.Text())
	}
	doc, err = goquery.NewDocumentFromReader(strings.NewReader(render(t, views.Default{}.User(authweb.UserPage{User: u, AccessURL: "/users/7/access"}))))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Find(`main a.tap[href="/users/7/access"]`).Length() != 1 {
		t.Error("user: the access link is not a tap target")
	}
}
