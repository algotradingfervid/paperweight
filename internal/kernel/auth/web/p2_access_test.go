package authweb_test

import (
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
)

var (
	t20FormRe  = regexp.MustCompile(`(?is)<form\b([^>]*)>(.*?)</form>`)
	t20InputRe = regexp.MustCompile(`(?is)<input\b[^>]*>`)
	t20AttrRe  = regexp.MustCompile(`(?is)\s([a-z][\w:.-]*)="([^"]*)"`)
	// t20SitePickerRe is an attribute whose value is the site picker's
	// search URL (Config.PickerURL(masterFor(site)), the probe's route).
	t20SitePickerRe = regexp.MustCompile(`(?is)\s[a-z][\w:.-]*="/site/picker"`)
)

// t20Form is one rendered <form>: its attributes, inner HTML and the
// name/value pairs of its inputs.
type t20Form struct {
	Attrs  map[string]string
	Inner  string
	Fields url.Values
}

func t20Attrs(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range t20AttrRe.FindAllStringSubmatch(tag, -1) {
		out[strings.ToLower(m[1])] = m[2]
	}
	return out
}

func t20Forms(body string) []t20Form {
	var out []t20Form
	for _, m := range t20FormRe.FindAllStringSubmatch(body, -1) {
		f := t20Form{Attrs: t20Attrs(m[1]), Inner: m[2], Fields: url.Values{}}
		for _, in := range t20InputRe.FindAllString(m[2], -1) {
			a := t20Attrs(in)
			if name, ok := a["name"]; ok {
				f.Fields.Add(name, a["value"])
			}
		}
		out = append(out, f)
	}
	return out
}

// t20RestrictionForms returns the page's POST forms to path for dim that
// carry field (remove or add).
func t20RestrictionForms(body, path string, dim auth.Dimension, field string) []t20Form {
	var out []t20Form
	for _, f := range t20Forms(body) {
		if f.Attrs["action"] != path || !strings.EqualFold(f.Attrs["method"], http.MethodPost) {
			continue
		}
		if f.Fields.Get("dimension") != string(dim) {
			continue
		}
		if _, ok := f.Fields[field]; ok {
			out = append(out, f)
		}
	}
	return out
}

// t20Page GETs the access page as c, wants 200 and no table markup.
func t20Page(t *testing.T, c *webtest.Client, what, path string) string {
	t.Helper()
	r := c.Get(path)
	wantStatus(t, what, r, http.StatusOK)
	if strings.Contains(strings.ToLower(r.Body), "<table") {
		t.Fatalf("%s: body has table markup: %q", what, r.Body)
	}
	return r.Body
}

// t20Removes returns the site codes the page offers to remove, each from a
// form with a "Remove" button, sorted, and the forms by code.
func t20Removes(t *testing.T, w *authtest.World, what, body, post string) ([]string, map[string]t20Form) {
	t.Helper()
	codes := map[string]string{}
	for code, id := range w.Sites {
		codes[strconv.FormatInt(id, 10)] = code
	}
	byCode := map[string]t20Form{}
	got := []string{}
	for _, f := range t20RestrictionForms(body, post, auth.DimSite, "remove") {
		code, ok := codes[f.Fields.Get("remove")]
		if !ok {
			t.Fatalf("%s: a remove form posts remove=%q, not a site id", what, f.Fields.Get("remove"))
		}
		if !strings.Contains(f.Inner, "Remove") {
			t.Fatalf("%s: the remove form for %s has no \"Remove\" button: %q", what, code, f.Inner)
		}
		byCode[code] = f
		got = append(got, code)
	}
	slices.Sort(got)
	if forms := t20RestrictionForms(body, post, auth.DimProject, "remove"); len(forms) != 0 {
		t.Fatalf("%s: %d project remove forms, want none (no project restriction)", what, len(forms))
	}
	return got, byCode
}

// AUTH-121
func TestAccessEditor_ShowsAddsRemoves(t *testing.T) {
	w := authtest.NewWorld(t)
	h := t14Probe(t, w)
	admin := signedIn(t, h, t13Admin)
	page := t13UserPath(w, t13Suresh, "/access")
	post := t13UserPath(w, t13Suresh, "/restrictions")

	body := t20Page(t, admin, "admin GET "+page, page)
	got, forms := t20Removes(t, w, "admin GET "+page, body, post)
	if want := []string{"METRO-SITE", "PUNE-WH"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("admin GET %s: sites with Remove = %q, want %q", page, got, want)
	}
	if !strings.Contains(body, "All projects (no restriction)") {
		t.Fatalf("admin GET %s: body lacks \"All projects (no restriction)\": %q", page, body)
	}
	if strings.Contains(body, "All sites (no restriction)") {
		t.Fatalf("admin GET %s: body says \"All sites (no restriction)\" while suresh is restricted: %q", page, body)
	}
	adds := t20RestrictionForms(body, post, auth.DimSite, "add")
	if len(adds) != 1 {
		t.Fatalf("admin GET %s: %d site add forms (dimension=site with an add field), want 1: %q", page, len(adds), body)
	}
	if !t20SitePickerRe.MatchString(adds[0].Inner) {
		t.Fatalf("admin GET %s: the site add form has no attribute with the value \"/site/picker\": %q", page, adds[0].Inner)
	}

	// Remove METRO-SITE with the page's own form (its version, CD-146).
	r := admin.Post(post, forms["METRO-SITE"].Fields)
	wantRedirect(t, "admin removes METRO-SITE", r, http.StatusSeeOther, page)
	body = t20Page(t, admin, "admin GET "+page+" after remove", page)
	if got, _ := t20Removes(t, w, "after remove", body, post); !reflect.DeepEqual(got, []string{"PUNE-WH"}) {
		t.Fatalf("after removing METRO-SITE: sites with Remove = %q, want [PUNE-WH]", got)
	}
	if strings.Contains(body, "METRO-SITE") {
		t.Fatalf("after removing METRO-SITE: the page still shows METRO-SITE: %q", body)
	}
	if want := "Site and project access for Suresh Patil saved."; !strings.Contains(body, want) {
		t.Fatalf("after removing METRO-SITE: body lacks the flash %q: %q", want, body)
	}

	// Add TECH-SITE through the page's site picker form.
	adds = t20RestrictionForms(body, post, auth.DimSite, "add")
	if len(adds) != 1 {
		t.Fatalf("after remove: %d site add forms, want 1: %q", len(adds), body)
	}
	f := adds[0].Fields
	f.Del("q")
	f.Set("add", strconv.FormatInt(w.Sites["TECH-SITE"], 10))
	r = admin.Post(post, f)
	wantRedirect(t, "admin adds TECH-SITE", r, http.StatusSeeOther, page)
	body = t20Page(t, admin, "admin GET "+page+" after add", page)
	if got, _ := t20Removes(t, w, "after add", body, post); !reflect.DeepEqual(got, []string{"PUNE-WH", "TECH-SITE"}) {
		t.Fatalf("after adding TECH-SITE: sites with Remove = %q, want [PUNE-WH TECH-SITE]", got)
	}

	// AUTH-105: a disabled site stays listed, marked.
	w.SetMasterEnabled(t, authtest.DTSite, "PUNE-WH", false)
	body = t20Page(t, admin, "admin GET "+page+" after disabling PUNE-WH", page)
	if !strings.Contains(body, "PUNE-WH (disabled)") {
		t.Fatalf("after disabling PUNE-WH: body lacks \"PUNE-WH (disabled)\": %q", body)
	}

	suresh := signedIn(t, h, t13Suresh)
	r = suresh.Get(page)
	wantStatus(t, "suresh GET "+page, r, http.StatusForbidden)
	wantBody(t, "suresh GET "+page, r, t13AdminOnlyAccess)
}
