package views_test

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/views"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var b strings.Builder
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func TestViews_Implements(t *testing.T) {
	var _ authweb.Views = views.Default{}
}

// errorInSummary matches the error summary, which takes focus (tabindex="-1",
// autofocus; CD-159) and holds msg.
func errorInSummary(msg string) *regexp.Regexp {
	return regexp.MustCompile(`(?s)<div class="alert error-summary" tabindex="-1" autofocus[^>]*>.*?` + regexp.QuoteMeta(msg))
}

func TestViews_SignInFields(t *testing.T) {
	body := render(t, views.Default{}.Login(authweb.LoginPage{CSRF: "tok"}))
	for _, want := range []string{
		`autocomplete="username"`,
		`autocomplete="current-password"`,
		`data-toggle-password`,
		`Show password`,
		`<input type="hidden" name="csrf_token" value="tok">`,
		`action="/signin"`,
		`Sign in`,
		`class="solo"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("sign-in page lacks %q: %q", want, body)
		}
	}
	for _, bad := range []string{"<nav", "onpaste", "style=", "<table"} {
		if strings.Contains(body, bad) {
			t.Errorf("sign-in page contains %q: %q", bad, body)
		}
	}
	doc := parse(t, body)
	if _, ok := doc.Find(`input[name="email"]`).Attr("autofocus"); !ok {
		t.Error("email has no autofocus on a fresh sign-in page")
	}
	if n := doc.Find("[autofocus]").Length(); n != 1 {
		t.Errorf("%d autofocus elements, want 1", n)
	}
	if got := doc.Find(`input[name="password"]`).AttrOr("aria-describedby", ""); got != "f-password-msg" {
		t.Errorf("password aria-describedby %q with no error, want f-password-msg", got)
	}
	if n := doc.Find("#signin-error").Length(); n != 0 {
		t.Errorf("%d #signin-error with no error, want 0", n)
	}
	if n := doc.Find(".solo__card .banner--danger").Length(); n != 0 {
		t.Errorf("%d error banners with no error, want 0", n)
	}
	help := doc.Find(".solo .solo__help")
	if got := strings.Join(strings.Fields(help.Text()), " "); got != "Forgot your password? Ask your administrator to reset it. They can do it from Users in a minute." {
		t.Errorf("help line = %q", got)
	}
}

// A-7: a failed sign-in shows the kernel text in a role="alert" banner, marks
// no field invalid, keeps the typed email and focuses the empty password,
// which the banner describes. The hint follows only the invalid-login text
// (UI v2 §5.3, CD-361).
func TestViews_SignInRefused(t *testing.T) {
	for _, tc := range []struct {
		msg  string
		hint bool
	}{
		{auth.ErrLoginInvalid.Msg, true},
		{auth.ErrAccountLocked.Msg, false},
		{auth.ErrAccountDisabled.Msg, false},
	} {
		const typed = `ravi<b>@vardhan.test`
		body := render(t, views.Default{}.Login(authweb.LoginPage{CSRF: "tok", Error: tc.msg, Email: typed}))
		if strings.Contains(body, typed) {
			t.Errorf("%s: the typed email is not escaped", tc.msg)
		}
		doc := parse(t, body)
		if got := doc.Find("title").Text(); !strings.HasPrefix(got, "Error: Sign in") {
			t.Errorf("%s: title %q", tc.msg, got)
		}
		banner := doc.Find(`.solo__card .banner--danger[role="alert"]`)
		if banner.Length() != 1 {
			t.Fatalf("%s: %d danger banners with role=alert in the card, want 1", tc.msg, banner.Length())
		}
		text := strings.Join(strings.Fields(banner.Text()), " ")
		if !strings.Contains(text, tc.msg) {
			t.Errorf("banner %q lacks %q", text, tc.msg)
		}
		if got := strings.Contains(text, signInHint); got != tc.hint {
			t.Errorf("%s: hint shown = %v, want %v", tc.msg, got, tc.hint)
		}
		if n := doc.Find("[aria-invalid]").Length(); n != 0 {
			t.Errorf("%s: %d fields with aria-invalid, want 0", tc.msg, n)
		}
		if n := doc.Find(".error-summary").Length(); n != 0 {
			t.Errorf("%s: %d error summaries, want the banner only", tc.msg, n)
		}
		pw := doc.Find(`input[name="password"]`)
		if v := pw.AttrOr("value", ""); v != "" {
			t.Errorf("%s: password value %q, want empty", tc.msg, v)
		}
		if _, ok := pw.Attr("autofocus"); !ok {
			t.Errorf("%s: password has no autofocus", tc.msg)
		}
		if n := doc.Find("[autofocus]").Length(); n != 1 {
			t.Errorf("%s: %d autofocus elements, want 1", tc.msg, n)
		}
		// CD-361 (2): the email the user typed is kept, as text.
		if v := doc.Find(`input[name="email"]`).AttrOr("value", ""); v != typed {
			t.Errorf("%s: email value %q, want %q", tc.msg, v, typed)
		}
		// CD-361 (1): the focused password is described by its own message
		// and the banner, so the refusal is read with the field.
		if got := pw.AttrOr("aria-describedby", ""); got != "f-password-msg signin-error" {
			t.Errorf("%s: password aria-describedby %q, want %q", tc.msg, got, "f-password-msg signin-error")
		}
		if doc.Find(`#signin-error .banner--danger[role="alert"]`).Length() != 1 {
			t.Errorf("%s: the banner is not inside #signin-error", tc.msg)
		}
	}
}

const signInHint = "Check both and try again. Passwords are case-sensitive."

func parse(t *testing.T, body string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestViews_PasswordRulesBeforeFields(t *testing.T) {
	for name, c := range map[string]templ.Component{
		"setup":    views.Default{}.Setup(authweb.SetupPage{CSRF: "tok"}),
		"password": views.Default{}.Password(authweb.PasswordPage{CSRF: "tok"}),
	} {
		body := render(t, c)
		rules := strings.Index(body, "at least 10 characters")
		field := strings.Index(body, `type="password"`)
		if rules < 0 || field < 0 || rules > field {
			t.Errorf("%s: rules at %d, first password input at %d; want the rules first: %q", name, rules, field, body)
		}
	}
}

func TestViews_SessionsCards(t *testing.T) {
	at := time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)
	body := render(t, views.Default{}.Sessions(authweb.SessionsPage{CSRF: "tok", Sessions: []auth.SessionInfo{
		{ID: 1, UserAgent: "Chrome on Windows", CreatedAt: at, LastSeenAt: at},
		{ID: 2, UserAgent: "Safari on iPhone", CreatedAt: at, LastSeenAt: at.Add(time.Hour)},
	}}))
	if n := strings.Count(body, `class="card `); n != 2 {
		t.Errorf("sessions page has %d .card elements, want 2: %q", n, body)
	}
	if n := strings.Count(body, "End session"); n != 2 {
		t.Errorf("sessions page has %d End session buttons, want 2: %q", n, body)
	}
	if n := strings.Count(body, "End all other sessions"); n != 1 {
		t.Errorf("sessions page has %d End all other sessions buttons, want 1: %q", n, body)
	}
	if strings.Contains(body, "<table") {
		t.Errorf("sessions page contains <table: %q", body)
	}
}

func TestViews_SessionsUnknownDevice(t *testing.T) {
	body := render(t, views.Default{}.Sessions(authweb.SessionsPage{CSRF: "tok", Sessions: []auth.SessionInfo{{ID: 1}}}))
	if !strings.Contains(body, "Unknown device") {
		t.Errorf("session with no user agent lacks the Unknown device title: %q", body)
	}
}

func TestViews_PasswordRulesDifferent(t *testing.T) {
	const rule = "be different from your current password"
	if body := render(t, views.Default{}.Password(authweb.PasswordPage{CSRF: "tok"})); !strings.Contains(body, rule) {
		t.Errorf("change-password rules lack %q: %q", rule, body)
	}
	if body := render(t, views.Default{}.Setup(authweb.SetupPage{CSRF: "tok"})); strings.Contains(body, rule) {
		t.Errorf("setup rules contain %q: %q", rule, body)
	}
}

// M8: a session's card names the browser and system, never the raw user agent.
func TestViews_SessionsDeviceName(t *testing.T) {
	const (
		webkit = "AppleWebKit/537.36 (KHTML, like Gecko)"
		mac    = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
		win    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "
		linux  = "Mozilla/5.0 (X11; Linux x86_64) "
		phone  = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) "
		pad    = "Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) "
		droid  = "Mozilla/5.0 (Linux; Android 14; Pixel 8) "
		chrome = webkit + " Chrome/130.0.0.0 Safari/537.36"
	)
	for ua, want := range map[string]string{
		mac + chrome:   "Chrome on macOS",
		win + chrome:   "Chrome on Windows",
		linux + chrome: "Chrome on Linux",
		droid + chrome: "Chrome on Android",
		"Mozilla/5.0 (X11; CrOS x86_64) " + chrome:                                "Chrome on ChromeOS",
		mac + webkit + " Version/17.0 Safari/605.1.15":                            "Safari on macOS",
		phone + webkit + " Version/17.0 Mobile Safari/604.1":                      "Safari on iPhone",
		pad + webkit + " Version/17.0 Mobile Safari/604.1":                        "Safari on iPad",
		phone + webkit + " CriOS/130.0 Mobile Safari/604.1":                       "Chrome on iPhone",
		phone + webkit + " FxiOS/130.0 Mobile Safari/604.1":                       "Firefox on iPhone",
		win + chrome + " Edg/130.0.0.0":                                           "Edge on Windows",
		mac + chrome + " OPR/115.0.0.0":                                           "Opera on macOS",
		droid + webkit + " SamsungBrowser/25.0 Chrome/121.0 Mobile Safari/537.36": "Samsung Internet on Android",
		"Mozilla/5.0 (Windows NT 10.0; rv:131.0) Gecko/20100101 Firefox/131.0":    "Firefox on Windows",
		"Mozilla/5.0 (X11; Linux x86_64) Gecko/20100101":                          "Linux",
		"Firefox/131.0": "Firefox",
		"curl/8.7.1":    "Unknown device",
		"":              "Unknown device",
	} {
		body := render(t, views.Default{}.Sessions(authweb.SessionsPage{CSRF: "tok", Sessions: []auth.SessionInfo{{ID: 1, UserAgent: ua}}}))
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(doc.Find(".card-title").Text()); got != want {
			t.Errorf("%q: title = %q, want %q", ua, got, want)
		}
		if ua != "" && strings.Contains(body, ua) {
			t.Errorf("%q: the page shows the raw user agent", ua)
		}
	}
}

// Only the session in use is marked, and it can't be ended from its own card.
func TestViews_SessionsMarksCurrent(t *testing.T) {
	body := render(t, views.Default{}.Sessions(authweb.SessionsPage{CSRF: "tok", Sessions: []auth.SessionInfo{
		{ID: 1, UserAgent: "Chrome/130 Windows", Current: true},
		{ID: 2, UserAgent: "Firefox/131 Linux"},
	}}))
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	cards := doc.Find(".card")
	if got := strings.TrimSpace(cards.Eq(0).Find(".badge").Text()); got != "This session" {
		t.Errorf("current card's mark = %q", got)
	}
	if n := cards.Eq(1).Find(".badge").Length(); n != 0 {
		t.Errorf("the other card has %d marks", n)
	}
	if got := doc.Find("h1").Text(); !strings.Contains(got, "My sessions") {
		t.Errorf("h1 = %q, want the link's words", got)
	}
}
