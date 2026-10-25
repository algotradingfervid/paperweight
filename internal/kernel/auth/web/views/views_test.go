package views_test

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

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
	const msg = "Invalid email or password."
	body := render(t, views.Default{}.Login(authweb.LoginPage{CSRF: "tok", Error: msg}))
	for _, want := range []string{
		`autocomplete="username"`,
		`autocomplete="current-password"`,
		`data-toggle-password`,
		`Show password`,
		`<input type="hidden" name="csrf_token" value="tok">`,
		`action="/signin"`,
		`Sign in`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("sign-in page lacks %q: %q", want, body)
		}
	}
	if !errorInSummary(msg).MatchString(body) {
		t.Errorf("sign-in page does not show %q inside a focused .error-summary: %q", msg, body)
	}
	if regexp.MustCompile(`role="alert"[^>]*>\s*<div class="alert error-summary"`).MatchString(body) {
		t.Errorf("sign-in page wraps the error summary in role=\"alert\", so it is announced twice (CD-159): %q", body)
	}
	for _, bad := range []string{"<nav", "onpaste", "style=", "<table"} {
		if strings.Contains(body, bad) {
			t.Errorf("sign-in page contains %q: %q", bad, body)
		}
	}
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
