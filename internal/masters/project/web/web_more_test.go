package projectweb_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/types"
)

// No case ID: the screen behaviour Task 22 adds beyond its named tests.

// A user restricted on Project sees their own project and no other, on every
// path, through the real auth.Service (CD-09, CD-64), and can't create one
// (CD-172): the producing package's 403, ahead of any 422 on the form.
func TestScreens_ProjectRestricted(t *testing.T) {
	w := newWorld(t)
	metro, tech := w.seed(t)
	kavya := w.h.Env.Kavya // every right on Project
	w.restrict(t, kavya.ID, metro.ID)

	list := w.h.Get(t, kavya, "/m/project")
	webtest.WantStatus(t, list, http.StatusOK)
	if _, ok := list.RowLink(path(metro.ID, "")); !ok {
		t.Error("restricted list lacks her own PRJ-METRO")
	}
	if _, ok := list.RowLink(path(tech.ID, "")); ok {
		t.Error("restricted list shows PRJ-TECHPARK")
	}
	webtest.WantText(t, list, "1 project")
	if _, ok := list.RowLink("/m/project/new"); ok {
		t.Error("restricted list links to /m/project/new")
	}

	page := w.h.Get(t, kavya, path(metro.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	if v, _ := page.Value("name"); v != metro.Name {
		t.Errorf("own record name = %q; want the form with %q", v, metro.Name)
	}
	webtest.WantStatus(t, w.h.Get(t, kavya, path(tech.ID, "")), http.StatusNotFound)

	got := w.h.GetHTMX(t, kavya, "/pick/Project?q=PRJ").PickRows()
	if len(got) != 1 || got[0].Value != id(metro.ID) {
		t.Errorf("restricted picker = %v; want only PRJ-METRO", got)
	}

	file := w.h.Get(t, kavya, "/m/project/export.csv")
	webtest.WantStatus(t, file, http.StatusOK)
	if !strings.Contains(file.Body, "PRJ-METRO") || strings.Contains(file.Body, "PRJ-TECHPARK") {
		t.Errorf("restricted export = %q; want PRJ-METRO alone", file.Body)
	}

	resp := w.h.Get(t, kavya, "/m/project/new")
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, msgRestrict)

	resp = w.h.Post(t, kavya, "/m/project/new", w.form("PRJ-NEW", "New"))
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, msgRestrict)

	// CD-172's 403 comes before the 422 of a form that doesn't parse.
	form := w.form("PRJ-NEW", "New")
	form.Set("start_date", "not a date")
	resp = w.h.Post(t, kavya, "/m/project/new", form)
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, msgRestrict)
	if _, ok := w.byCode(t, "PRJ-NEW"); ok {
		t.Error("restricted user created PRJ-NEW")
	}

	// A form that doesn't parse goes no further on a project she can't see,
	// and saves on her own.
	form = w.form("PRJ-TECHPARK", "Renamed")
	form.Set("version", ver(tech.Version))
	form.Set("start_date", "not a date")
	webtest.WantStatus(t, w.h.Post(t, kavya, path(tech.ID, ""), form), http.StatusNotFound)

	// Nor does any write that would parse: to her that project isn't there.
	form.Del("start_date")
	webtest.WantStatus(t, w.h.Post(t, kavya, path(tech.ID, ""), form), http.StatusNotFound)
	for _, suffix := range []string{"/disable", "/delete"} {
		resp := w.h.Post(t, kavya, path(tech.ID, suffix), url.Values{"version": {ver(tech.Version)}})
		webtest.WantStatus(t, resp, http.StatusNotFound)
	}
	if got := w.mustGet(t, "PRJ-TECHPARK"); got != tech {
		t.Errorf("PRJ-TECHPARK after refused writes = %+v; want unchanged %+v", got, tech)
	}

	form = w.form("PRJ-METRO", "Renamed")
	form.Set("version", ver(metro.Version))
	webtest.WantRedirect(t, w.h.Post(t, kavya, path(metro.ID, ""), form), path(metro.ID, ""))

	// An Administrator is never restricted: the check is CD-172's, not create's.
	admin := w.h.Env.Admin
	if _, ok := w.h.Get(t, admin, "/m/project").RowLink("/m/project/new"); !ok {
		t.Error("Administrator's list has no New project")
	}
}

// Dates round-trip as dd-mm-yyyy: typed, stored as the date, shown on the form
// and to a reader in the same form (CD-33).
func TestScreens_ProjectDateRoundTrip(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya

	form := w.form("PRJ-NEW", "New")
	form.Set("start_date", "01-07-2026")
	form.Set("end_date", "31-03-2027")
	resp := w.h.Post(t, kavya, "/m/project/new", form)
	created, ok := w.byCode(t, "PRJ-NEW")
	if !ok {
		t.Fatalf("valid POST saved nothing: %d %s", resp.Status, webtest.Squash(resp.Body))
	}
	if created.Start.ISO() != "2026-07-01" || created.End.ISO() != "2027-03-31" {
		t.Errorf("stored dates = %s, %s", created.Start.ISO(), created.End.ISO())
	}

	page := w.h.Get(t, kavya, path(created.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	if v, _ := page.Value("start_date"); v != "01-07-2026" {
		t.Errorf("form start_date = %q; want 01-07-2026", v)
	}
	if v, _ := page.Value("end_date"); v != "31-03-2027" {
		t.Errorf("form end_date = %q; want 31-03-2027", v)
	}

	// Saving the page as it came back changes no date.
	form.Set("version", ver(created.Version))
	webtest.WantRedirect(t, w.h.Post(t, kavya, path(created.ID, ""), form), path(created.ID, ""))
	if got := w.mustGet(t, "PRJ-NEW"); got.Start != created.Start || got.End != created.End {
		t.Errorf("after resave dates = %s, %s", got.Start.ISO(), got.End.ISO())
	}
}

// Blank dates clear the stored ones, and show blank on the form and to a
// reader, never as a zero date.
func TestScreens_ProjectBlankDates(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	full := w.full(t)

	form := w.form("PRJ-FULL", "Full Project")
	form.Set("version", ver(full.Version))
	form.Set("start_date", "")
	form.Set("end_date", "  ")
	webtest.WantRedirect(t, w.h.Post(t, kavya, path(full.ID, ""), form), path(full.ID, ""))
	if got := w.mustGet(t, "PRJ-FULL"); got.Start != (types.Date{}) || got.End != (types.Date{}) {
		t.Errorf("after clearing dates = %+v; want both blank", got)
	}

	page := w.h.Get(t, kavya, path(full.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	if v, ok := page.Value("start_date"); !ok || v != "" {
		t.Errorf("form start_date = %q (found %v); want empty", v, ok)
	}
	if v, ok := page.Value("end_date"); !ok || v != "" {
		t.Errorf("form end_date = %q (found %v); want empty", v, ok)
	}

	w.restrict(t, w.h.Env.Priya.ID, full.ID)
	webtest.WantStatus(t, w.h.Get(t, w.h.Env.Priya, path(full.ID, "")), http.StatusOK)
}

// A date that doesn't parse gets NUM's message on its field, next to the other
// fields' errors, and never becomes a blank date (CD-37).
func TestScreens_ProjectBadDate(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	metro, _ := w.seed(t)

	form := w.form("PRJ-NEW", "New")
	form.Del("branch_id")
	form.Set("start_date", "31-02-2026")
	form.Set("end_date", "01-01-1800")
	resp := w.h.Post(t, kavya, "/m/project/new", form)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	wantFieldError(t, resp, "start_date", "31-02-2026 is not a valid date. Use dd-mm-yyyy, for example 31-03-2025.")
	wantFieldError(t, resp, "end_date", "Year must be between 1900 and 2099.")
	wantFieldError(t, resp, "branch_id", msgBranch) // Validate's
	if _, ok := w.byCode(t, "PRJ-NEW"); ok {
		t.Fatal("bad date saved PRJ-NEW")
	}

	form = w.form("PRJ-METRO", "Renamed")
	form.Set("version", ver(metro.Version))
	form.Set("start_date", "junk")
	resp = w.h.Post(t, kavya, path(metro.ID, ""), form)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	wantFieldError(t, resp, "start_date", "junk is not a valid date. Use dd-mm-yyyy, for example 31-03-2025.")
	if got := w.mustGet(t, "PRJ-METRO"); got != metro {
		t.Errorf("PRJ-METRO after a bad date = %+v; want unchanged %+v", got, metro)
	}
}

// A status outside the four is the service's message on its field (MST-101);
// a blank one on a new project is Open.
func TestScreens_ProjectStatus(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya

	blank := w.h.Get(t, kavya, "/m/project/new")
	if v, ok := blank.Doc.Find(`input[name="status"][checked]`).Attr("value"); !ok || v != "Open" {
		t.Errorf("new form status = %q (found %v); want Open ticked", v, ok)
	}

	form := w.form("PRJ-NEW", "New")
	form.Set("status", "Done")
	resp := w.h.Post(t, kavya, "/m/project/new", form)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, "status", msgStatus)

	form.Del("status")
	webtest.WantStatus(t, w.h.Post(t, kavya, "/m/project/new", form), http.StatusSeeOther)
	if got := w.mustGet(t, "PRJ-NEW"); got.Status != project.Open {
		t.Errorf("status = %q; want Open", got.Status)
	}
}

// A blank Link is none: the service's Branch is required for the branch, no
// customer for the customer, on create and on update.
func TestScreens_ProjectBlankLinks(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	full := w.full(t)

	form := w.form("PRJ-NEW", "New")
	form.Set("branch_id", "")
	resp := w.h.Post(t, kavya, "/m/project/new", form)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	wantFieldError(t, resp, "branch_id", msgBranch)

	form.Set("branch_id", id(w.branch))
	form.Set("customer_id", "")
	webtest.WantStatus(t, w.h.Post(t, kavya, "/m/project/new", form), http.StatusSeeOther)
	created := w.mustGet(t, "PRJ-NEW")
	if created.CustomerID != 0 {
		t.Errorf("PRJ-NEW customer = %d; want 0", created.CustomerID)
	}

	form = w.form("PRJ-FULL", "Full Project")
	form.Set("version", ver(full.Version))
	form.Set("customer_id", "")
	webtest.WantRedirect(t, w.h.Post(t, kavya, path(full.ID, ""), form), path(full.ID, ""))
	if got := w.mustGet(t, "PRJ-FULL"); got.CustomerID != 0 {
		t.Errorf("PRJ-FULL customer after clearing = %d; want 0", got.CustomerID)
	}

	// Stored 0 is empty on the form, never "0".
	page := w.h.Get(t, kavya, path(created.ID, ""))
	if v, ok := page.Value("customer_id"); !ok || v != "" {
		t.Errorf("form customer_id = %q (found %v); want empty", v, ok)
	}
	webtest.WantStatus(t, w.h.Get(t, w.h.Env.Admin, path(created.ID, "")), http.StatusOK)
}

// Each refused Link, on create and on update, on its own field. A value that
// isn't a number gets the message of a record that doesn't exist, so strconv's
// text never shows and the service isn't reached (CD-37); the rest are the
// service's (CD-170).
func TestScreens_ProjectBadLinks(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	metro, _ := w.seed(t)
	offBranch, offCustomer := id(w.disabledBranch(t)), id(w.disabledCustomer(t))

	cases := []struct {
		name   string
		values map[string]string // posted link values
		want   map[string]string // field to message
	}{
		{"branch abc", map[string]string{"branch_id": "abc"}, map[string]string{"branch_id": msgNoBranch}},
		{"branch 12x", map[string]string{"branch_id": "12x"}, map[string]string{"branch_id": msgNoBranch}},
		{"branch too long", map[string]string{"branch_id": "99999999999999999999"}, map[string]string{"branch_id": msgNoBranch}},
		{"branch not found", map[string]string{"branch_id": "999999"}, map[string]string{"branch_id": msgNoBranch}},
		{"branch disabled", map[string]string{"branch_id": offBranch}, map[string]string{"branch_id": msgOffBranch}},
		{"customer abc", map[string]string{"customer_id": "abc"}, map[string]string{"customer_id": msgNoParty}},
		{"customer 12x", map[string]string{"customer_id": "12x"}, map[string]string{"customer_id": msgNoParty}},
		{"customer too long", map[string]string{"customer_id": "99999999999999999999"}, map[string]string{"customer_id": msgNoParty}},
		{"customer not found", map[string]string{"customer_id": "999999"}, map[string]string{"customer_id": msgNoParty}},
		{"customer disabled", map[string]string{"customer_id": offCustomer}, map[string]string{"customer_id": msgOffParty}},
		{
			"both not numbers", map[string]string{"branch_id": "abc", "customer_id": "abc"},
			map[string]string{"branch_id": msgNoBranch, "customer_id": msgNoParty},
		},
		{
			"both refused by the service", map[string]string{"branch_id": "999999", "customer_id": offCustomer},
			map[string]string{"branch_id": msgNoBranch, "customer_id": msgOffParty},
		},
	}
	refused := func(t *testing.T, resp webtest.Response, want map[string]string) {
		t.Helper()
		webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
		for field, msg := range want {
			wantFieldError(t, resp, field, msg)
		}
		webtest.WantNoText(t, resp, "strconv")
		webtest.WantNoText(t, resp, "invalid syntax")
		webtest.WantNoText(t, resp, "out of range")
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			form := w.form("PRJ-NEW", "New")
			for k, v := range c.values {
				form.Set(k, v)
			}
			refused(t, w.h.Post(t, kavya, "/m/project/new", form), c.want)
			if _, ok := w.byCode(t, "PRJ-NEW"); ok {
				t.Error("refused link saved PRJ-NEW")
			}

			form.Set("code", "PRJ-METRO")
			form.Set("name", "Renamed")
			form.Set("version", ver(metro.Version))
			refused(t, w.h.Post(t, kavya, path(metro.ID, ""), form), c.want)
			if got := w.mustGet(t, "PRJ-METRO"); got != metro {
				t.Errorf("PRJ-METRO after a refused link = %+v; want unchanged %+v", got, metro)
			}
		})
	}

	// The form's right goes first: no 422 for a user who can't save at all.
	priya := w.h.Env.Priya
	form := w.form("PRJ-NEW", "New")
	form.Set("branch_id", "abc")
	resp := w.h.Post(t, priya, "/m/project/new", form)
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, (&auth.DeniedError{Right: auth.RightCreate, Label: "Project"}).Error())
}

// The record page's form holds every stored value, shows both Links as
// "{Code} {Name}" (CD-68) and its title is "{Code} {Name}"; a reader without
// write sees the same as text.
func TestScreens_ProjectRecordValues(t *testing.T) {
	w := newWorld(t)
	full := w.full(t)

	page := w.h.Get(t, w.h.Env.Kavya, path(full.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	want := map[string]string{"code": "PRJ-FULL", "name": "Full Project", "branch_id": id(w.branch), "customer_id": id(w.shree)}
	for name, value := range want {
		if v, ok := page.Value(name); !ok || v != value {
			t.Errorf("form %s = %q (found %v); want %q", name, v, ok, value)
		}
	}
	if v, ok := page.Doc.Find(`input[name="status"][checked]`).Attr("value"); !ok || v != "On Hold" {
		t.Errorf("checked status = %q (found %v); want On Hold", v, ok)
	}
	if got := webtest.Squash(page.Doc.Find("h1").First().Text()); got != "PRJ-FULL Full Project" {
		t.Errorf("record title = %q; want PRJ-FULL Full Project", got)
	}
	webtest.WantText(t, page, puneTitle)
	webtest.WantText(t, page, shreeTitle)

	priya := w.h.Env.Priya
	w.restrict(t, priya.ID, full.ID)
	read := w.h.Get(t, priya, path(full.ID, ""))
	webtest.WantStatus(t, read, http.StatusOK)
	if read.HasControl("name") {
		t.Error("a reader without write got the form")
	}
	for _, text := range []string{"PRJ-FULL", "Full Project", puneTitle, shreeTitle, "01-07-2026", "31-03-2027", "On Hold"} {
		webtest.WantText(t, read, text)
	}
}

// The list's text and the page after each write, in the master's words.
func TestScreens_ProjectCopy(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya

	empty := w.h.Get(t, kavya, "/m/project")
	webtest.WantStatus(t, empty, http.StatusOK)
	webtest.WantText(t, empty, "Projects")
	webtest.WantText(t, empty, "No projects yet.")
	webtest.WantText(t, empty, "New project")
	if !strings.Contains(empty.Body, "Search projects") {
		t.Error("list has no Search projects label")
	}

	resp := w.h.Post(t, kavya, "/m/project/new", w.form("PRJ-ONE", "One"))
	one, ok := w.byCode(t, "PRJ-ONE")
	if !ok {
		t.Fatalf("valid POST saved nothing: %d %s", resp.Status, webtest.Squash(resp.Body))
	}
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Project saved.")
	webtest.WantText(t, w.h.Get(t, kavya, "/m/project"), "1 project")

	resp = w.h.Post(t, kavya, path(one.ID, "/disable"), url.Values{"version": {ver(one.Version)}})
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Project disabled.")
	webtest.WantText(t, w.h.Get(t, kavya, "/m/project"), "All projects are disabled.")

	off := w.mustGet(t, "PRJ-ONE")
	resp = w.h.Post(t, kavya, path(one.ID, "/enable"), url.Values{"version": {ver(off.Version)}})
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Project enabled.")

	w.seed(t)
	list := w.h.Get(t, kavya, "/m/project")
	webtest.WantText(t, list, "3 projects")
	webtest.WantText(t, list, "Show disabled projects")

	on := w.mustGet(t, "PRJ-ONE")
	resp = w.h.Post(t, kavya, path(one.ID, "/delete"), url.Values{"version": {ver(on.Version)}})
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Project deleted.")
}

// Writes carry the page's version: without it AUTH's 400, with an old one
// DOC's 409 on the record page. Nothing changes.
func TestScreens_ProjectWriteVersion(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	metro, _ := w.seed(t)
	for _, suffix := range []string{"", "/disable", "/delete"} {
		resp := w.h.Post(t, kavya, path(metro.ID, suffix), w.form("PRJ-METRO", "Renamed"))
		webtest.WantStatus(t, resp, http.StatusBadRequest)
		webtest.WantText(t, resp, auth.ErrBadForm.Error())
	}
	stale := ver(metro.Version - 1)
	for _, suffix := range []string{"/disable", "/delete"} {
		resp := w.h.Post(t, kavya, path(metro.ID, suffix), url.Values{"version": {stale}})
		webtest.WantStatus(t, resp, http.StatusConflict)
		webtest.WantBanner(t, resp, staleMetro)
	}
	if got := w.mustGet(t, "PRJ-METRO"); got != metro {
		t.Errorf("PRJ-METRO after refused writes = %+v; want unchanged %+v", got, metro)
	}
}

// The delete confirm step shows the record's title and posts its version.
func TestScreens_ProjectDeleteConfirm(t *testing.T) {
	w := newWorld(t)
	metro, _ := w.seed(t)

	confirm := w.h.Get(t, w.h.Env.Kavya, path(metro.ID, "?delete"))
	webtest.WantStatus(t, confirm, http.StatusOK)
	webtest.WantText(t, confirm, "Delete PRJ-METRO Pune Metro Depot?")
	if v, ok := confirm.FormValue(path(metro.ID, "/delete"), "version"); !ok || v != ver(metro.Version) {
		t.Errorf("confirm form version = %q (found %v); want %d", v, ok, metro.Version)
	}
}

// Export is the shared route under this master's Base: a CSV with its fields
// for a user with the right, AUTH's message for one without.
func TestScreens_ProjectExport(t *testing.T) {
	w := newWorld(t)
	w.full(t)

	file := w.h.Get(t, w.h.Env.Kavya, "/m/project/export.csv")
	webtest.WantStatus(t, file, http.StatusOK)
	if got := file.Header.Get("Content-Disposition"); got != `attachment; filename="project.csv"` {
		t.Errorf("Content-Disposition = %q", got)
	}
	if !strings.Contains(file.Body, "PRJ-FULL,Full Project") {
		t.Errorf("export = %q; want the PRJ-FULL row", file.Body)
	}

	resp := w.h.Get(t, w.h.Env.Priya, "/m/project/export.csv")
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, (&auth.DeniedError{Right: auth.RightExport, Label: "Project"}).Error())
}
