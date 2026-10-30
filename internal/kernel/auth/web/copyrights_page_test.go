package authweb_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
)

// A source the copy refuses is 422 and keeps the page (CD-200), with the
// message on the field and in the summary.
func TestCopyRightsPost_RefusedSourceKeepsThePage(t *testing.T) {
	w, _, admin := p1Setup(t)
	path := p1Path(t, w, p1Target)
	version := t17RoleVersion(t, w, p1Target)

	for _, tc := range []struct{ name, source, msg string }{
		{"source missing", strconv.Itoa(p1NoRoleID), p1NoSource},
		{"source is the target", p1IDText(t, w, p1Target), p1Same},
		{"source is Administrator", p1IDText(t, w, auth.RoleAdministrator), p1FromAdm},
	} {
		r := admin.Post(path, p1Post(version, tc.source))
		wantStatus(t, tc.name, r, http.StatusUnprocessableEntity)
		doc := p1Doc(t, tc.name, r)
		if got := p1Text(doc.Find("h1")); got != p1Title {
			t.Errorf("%s: h1 = %q, want %q", tc.name, got, p1Title)
		}
		if got := p1Text(doc.Find(".error-summary")); !strings.Contains(got, tc.msg) {
			t.Errorf("%s: summary %q lacks %q", tc.name, got, tc.msg)
		}
		f := p1Form(t, tc.name, doc, path)
		if got := p1Text(f); !strings.Contains(got, tc.msg) {
			t.Errorf("%s: form %q lacks the field error %q", tc.name, got, tc.msg)
		}
		if got := p1Hidden(t, tc.name, f, "version"); got != version {
			t.Errorf("%s: hidden version = %q, want the posted %q", tc.name, got, version)
		}
	}
}

// The way back and the warning's look (design-system §4 rule 9).
func TestCopyRightsPage_BackLinkAndWarningBanner(t *testing.T) {
	w, _, admin := p1Setup(t)

	r := admin.Get(p1Path(t, w, p1Target))
	wantStatus(t, "GET copy rights", r, http.StatusOK)
	doc := p1Doc(t, "GET copy rights", r)
	if n := doc.Find(`a.back-link[href="/roles"]`).Length(); n != 1 {
		t.Errorf("back links to /roles = %d, want 1", n)
	}
	if got := p1Text(doc.Find(".banner--warning")); !strings.Contains(got, p1Warning) {
		t.Errorf("warning banner text %q lacks %q", got, p1Warning)
	}
}

// Six on offer is the most that still use radios.
func TestCopyRightsPage_RadiosAtSixRoles(t *testing.T) {
	w, _, admin := p1Setup(t) // seven on offer
	p1Exec(t, w, `DELETE FROM user_roles WHERE role_id = ?`, t17RoleID(t, w, "Director"))
	p1Exec(t, w, `DELETE FROM roles WHERE name = 'Director'`)
	path := p1Path(t, w, p1Target)

	r := admin.Get(path)
	wantStatus(t, "GET copy rights", r, http.StatusOK)
	form := p1Form(t, "GET copy rights", p1Doc(t, "GET copy rights", r), path)
	if n := form.Find(`input[type=radio][name=source_id]`).Length(); n != 6 {
		t.Errorf("radios = %d, want 6", n)
	}
	if n := form.Find(`select[name=source_id]`).Length(); n != 0 {
		t.Errorf("select[name=source_id] = %d, want 0 at six roles", n)
	}
	p1WantOffered(t, "GET copy rights", w, form)
}

// With no role to copy from the page says so and offers the way back, not a
// form that can only be refused.
func TestCopyRightsPage_NoOtherRole(t *testing.T) {
	w, _, admin := p1Setup(t)
	gone := `(SELECT id FROM roles WHERE is_admin = 0 AND name <> '` + p1Target + `')`
	p1Exec(t, w, `DELETE FROM user_roles WHERE role_id IN `+gone)
	p1Exec(t, w, `DELETE FROM roles WHERE id IN `+gone)

	r := admin.Get(p1Path(t, w, p1Target))
	wantStatus(t, "GET copy rights", r, http.StatusOK)
	doc := p1Doc(t, "GET copy rights", r)
	if got := p1Text(doc.Find("h1")); got != p1Title {
		t.Errorf("h1 = %q, want %q", got, p1Title)
	}
	empty := doc.Find(".empty-state")
	if got := p1Text(empty); !strings.Contains(got, "There is no other role to copy from.") {
		t.Errorf("empty state text = %q, want the no-role message", got)
	}
	if n := empty.Find(`a[href="/roles"]`).Length(); n != 1 {
		t.Errorf("empty state links to /roles = %d, want 1", n)
	}
	main := doc.Find("main")
	if n := main.Find("form").Length(); n != 0 {
		t.Errorf("forms = %d, want 0", n)
	}
	if n := main.Find("button").Length(); n != 0 {
		t.Errorf("buttons = %d, want 0", n)
	}
	if text := p1Text(main); strings.Contains(text, p1Warning) {
		t.Errorf("page text %q shows the warning", text)
	}
}

// The page that would show the field error refuses a target the copy refuses.
func TestCopyRightsPost_NoSourcePickedBadTarget(t *testing.T) {
	w, _, admin := p1Setup(t)

	t14Want404(t, "none picked, no such role", admin.Post("/roles/"+strconv.Itoa(p1NoRoleID)+"/copy-rights", p1Post("1", "")))

	r := admin.Post(p1Path(t, w, auth.RoleAdministrator), p1Post(t17RoleVersion(t, w, auth.RoleAdministrator), ""))
	wantStatus(t, "none picked, Administrator", r, http.StatusBadRequest)
	wantBody(t, "none picked, Administrator", r, p1Fixed)
}

// A select opens on nothing picked, so a stray submit can't replace the
// target's rights with the first role's.
func TestCopyRightsPage_SelectOpensEmpty(t *testing.T) {
	w, _, admin := p1Setup(t) // seven on offer: past the radios
	path := p1Path(t, w, p1Target)

	r := admin.Get(path)
	wantStatus(t, "GET copy rights", r, http.StatusOK)
	form := p1Form(t, "GET copy rights", p1Doc(t, "GET copy rights", r), path)
	first := form.Find(`select[name=source_id] option`).First()
	if first.Length() != 1 || first.AttrOr("value", "x") != "" {
		t.Errorf("first option value = %q, want empty", first.AttrOr("value", "x"))
	}
	if v := form.Find(`select[name=source_id] option[selected]`).AttrOr("value", ""); v != "" {
		t.Errorf("selected option value = %q, want empty", v)
	}
}
