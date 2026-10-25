package gallery

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// authPage gets /_gallery/<slug> and checks that Screens lists it.
func authPage(t *testing.T, slug string) *goquery.Document {
	t.Helper()
	found := false
	for _, s := range Screens("") {
		if s.Route == "/_gallery/{page}" && s.URL == "/_gallery/"+slug {
			found = true
		}
	}
	if !found {
		t.Errorf("Screens has no entry for /_gallery/%s", slug)
	}
	d := get(t, Handler(), "/_gallery/"+slug)
	if d == nil {
		t.FailNow()
	}
	return d
}

func TestGallery_AuthSignInShape(t *testing.T) {
	d := authPage(t, "auth-signin")
	form := d.Find(`form[method="post"][action="/signin"]`)
	if form.Length() != 1 {
		t.Fatalf("%d POST forms to /signin, want 1 (CD-63)", form.Length())
	}
	for _, sel := range []string{
		`input[type="email"][autocomplete="username"]`,
		`input[type="password"][autocomplete="current-password"]`,
		`button[type="button"][data-toggle-password]`,
		`input[type="hidden"][name="csrf_token"]`,
	} {
		if form.Find(sel).Length() != 1 {
			t.Errorf("sign-in form: want one %s", sel)
		}
	}
	if got := strings.TrimSpace(form.Find("[data-toggle-password]").Text()); got != "Show password" {
		t.Errorf("password toggle reads %q, want %q", got, "Show password")
	}
	if v, _ := form.Find(`input[type="email"]`).Attr("value"); v != uitest.LongEmail {
		t.Error("sign-in email is not uitest.LongEmail")
	}
	if !strings.Contains(d.Find(".error-summary li").Text(), uitest.LongError) {
		t.Error("no uitest.LongError in .error-summary")
	}
	if d.Find("nav").Length() != 0 {
		t.Error("sign-in has a nav; want the Bare shell")
	}
}

func TestGallery_AuthRightsSegmented(t *testing.T) {
	d := authPage(t, "auth-rights")
	for _, c := range []struct {
		name string
		want []string
	}{
		{"expense_claim.read", []string{"None", "Own", "Team", "All"}},
		{"purchase_order.read", []string{"None", "Own", "All"}},
	} {
		radios := d.Find(`input[type="radio"][name="` + c.name + `"]`)
		if radios.Length() != len(c.want) {
			t.Errorf("%s: %d radios, want %d", c.name, radios.Length(), len(c.want))
			continue
		}
		var got []string
		radios.Each(func(_ int, s *goquery.Selection) {
			got = append(got, strings.TrimSpace(s.Parent().Text()))
		})
		if strings.Join(got, "/") != strings.Join(c.want, "/") {
			t.Errorf("%s: options %q, want %q", c.name, got, c.want)
		}
		if radios.Closest(".segmented").Length() != 1 {
			t.Errorf("%s: radios are not one Segmented control", c.name)
		}
	}
	if d.Find(`.actionbar button[type="submit"]`).FilterFunction(func(_ int, s *goquery.Selection) bool {
		return strings.TrimSpace(s.Text()) == "Save rights"
	}).Length() != 1 {
		t.Error(`no "Save rights" submit in the action bar`)
	}
}

func TestGallery_AuthSessionsPost(t *testing.T) {
	d := authPage(t, "auth-sessions")
	if !strings.Contains(d.Text(), uitest.LongName100) {
		t.Error("sessions: uitest.LongName100 not shown")
	}
	count := map[string]int{}
	d.Find("button").Each(func(_ int, b *goquery.Selection) {
		label := strings.TrimSpace(b.Text())
		if label != "End session" && label != "End all other sessions" {
			return
		}
		count[label]++
		if typ, _ := b.Attr("type"); typ != "submit" {
			t.Errorf("%q: type %q, want submit", label, typ)
		}
		form := b.Closest("form")
		if m, _ := form.Attr("method"); m != "post" {
			t.Errorf("%q: not inside a POST form", label)
		}
		if form.Find(`input[type="hidden"][name="csrf_token"]`).Length() != 1 {
			t.Errorf("%q: its form has no csrf_token", label)
		}
	})
	if count["End session"] < 2 {
		t.Errorf("%d End session buttons, want at least 2", count["End session"])
	}
	if count["End all other sessions"] != 1 {
		t.Errorf("%d End all other sessions buttons, want 1 (CD-159)", count["End all other sessions"])
	}
}
