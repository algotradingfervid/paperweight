package siteweb_test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// No case ID: the screen behaviour Task 22 adds beyond its named tests.

const (
	// The service's refusals of a link that doesn't exist (CD-170).
	msgNoBranch  = "Branch not found. Pick one from the list."
	msgNoProject = "Project not found. Pick one from the list."
	// The service's refusal of a restricted user's create (CD-172).
	msgRestricted = "You can only work with your assigned sites. Ask your administrator to add this site."
	msgNoCode     = "Code is required. Enter a short code such as SHREE."
)

// restrictPriya gives Priya rights on Site and restricts her to site id, on
// the real auth.Service msttest wires (lessons §5).
func (w *world) restrictPriya(t *testing.T, id int64, rights ...auth.Right) {
	t.Helper()
	ctx := context.Background()
	a, ok := w.h.Env.Kit.Auth.(*auth.Auth)
	if !ok {
		t.Fatalf("msttest Kit.Auth is %T; want *auth.Auth", w.h.Env.Kit.Auth)
	}
	grant := map[auth.Right]auth.Scope{}
	for _, r := range rights {
		grant[r] = auth.ScopeAll
	}
	admin, priya := w.h.Env.Admin, w.h.Env.Priya
	if err := a.SetRights(ctx, admin, "Purchase User", site.DocType, grant); err != nil {
		t.Fatal(err)
	}
	before, err := a.UserRestrictions(ctx, priya.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ChangeRestrictions(ctx, admin, priya.ID, auth.DimSite, []int64{id}, before[auth.DimSite], nil); err != nil {
		t.Fatal(err)
	}
}

// A blank project is none, on create and on update; a stored none is empty on
// the form. A blank branch is Validate's own message.
func TestScreens_SiteBlankLinks(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	_, metro := w.seed(t)

	resp := w.h.Post(t, kavya, "/m/site/new", form(w.pune))
	yard, ok := w.byCode(t, "PUNE-YARD")
	if !ok {
		t.Fatalf("blank project on create saved nothing: %d %s", resp.Status, webtest.Squash(resp.Body))
	}
	if yard.ProjectID != 0 {
		t.Errorf("PUNE-YARD project = %d; want 0", yard.ProjectID)
	}
	page := w.h.Get(t, kavya, path(yard.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	if v, ok := page.Value("project_id"); !ok || v != "" {
		t.Errorf("form project of PUNE-YARD = %q (found %v); want empty", v, ok)
	}

	clear := url.Values{
		"version": {ver(metro.Version)}, "code": {"METRO-SITE"}, "name": {"Metro Site"}, "type": {"site"},
		"branch_id": {dec(w.pune)}, "project_id": {""},
	}
	webtest.WantRedirect(t, w.h.Post(t, kavya, path(metro.ID, ""), clear), path(metro.ID, ""))
	if got := w.mustGet(t, "METRO-SITE"); got.ProjectID != 0 {
		t.Errorf("METRO-SITE project after clearing = %d; want 0", got.ProjectID)
	}

	clear.Set("version", ver(metro.Version+1))
	clear.Set("branch_id", "")
	resp = w.h.Post(t, kavya, path(metro.ID, ""), clear)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	wantFieldError(t, resp, "branch_id", msgBranch)
}

// A branch or project that isn't a number gets the message of one that doesn't
// exist, on its own field, next to the other fields' errors; strconv's text
// never shows and the service isn't reached (CD-37).
func TestScreens_SiteBadLinks(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	wh, _ := w.seed(t)

	noStrconv := func(t *testing.T, resp webtest.Response) {
		t.Helper()
		webtest.WantNoText(t, resp, "strconv")
		webtest.WantNoText(t, resp, "invalid syntax")
		webtest.WantNoText(t, resp, "out of range")
	}
	for _, bad := range []string{"abc", "12x", "99999999999999999999"} {
		// Both pickers wrong, and Validate's error on another field.
		both := form(w.pune)
		both.Set("code", "")
		both.Set("branch_id", bad)
		both.Set("project_id", bad)
		resp := w.h.Post(t, kavya, "/m/site/new", both)
		webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
		wantFieldError(t, resp, "branch_id", msgNoBranch)
		wantFieldError(t, resp, "project_id", msgNoProject)
		wantFieldError(t, resp, "code", msgNoCode)
		noStrconv(t, resp)
		if v, _ := resp.Value("name"); v != "Pune Yard" {
			t.Errorf("422 page name value = %q; want the typed Pune Yard", v)
		}

		// One picker wrong: the other shows no error, and nothing is saved.
		one := form(w.pune)
		one.Set("project_id", bad)
		resp = w.h.Post(t, kavya, "/m/site/new", one)
		webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
		wantFieldError(t, resp, "project_id", msgNoProject)
		webtest.WantNoText(t, resp, msgNoBranch)
		if _, ok := w.byCode(t, "PUNE-YARD"); ok {
			t.Fatalf("project %q saved PUNE-YARD", bad)
		}

		edit := form(w.pune)
		edit.Set("version", ver(wh.Version))
		edit.Set("code", "PUNE-WH")
		edit.Set("branch_id", bad)
		resp = w.h.Post(t, kavya, path(wh.ID, ""), edit)
		webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
		wantFieldError(t, resp, "branch_id", msgNoBranch)
		noStrconv(t, resp)
		if got := w.mustGet(t, "PUNE-WH"); got != wh {
			t.Errorf("PUNE-WH after branch %q = %+v; want unchanged %+v", bad, got, wh)
		}

		edit.Set("branch_id", dec(w.pune))
		edit.Set("project_id", bad)
		resp = w.h.Post(t, kavya, path(wh.ID, ""), edit)
		webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
		wantFieldError(t, resp, "project_id", msgNoProject)
		webtest.WantNoText(t, resp, msgNoBranch)
		noStrconv(t, resp)
		if got := w.mustGet(t, "PUNE-WH"); got != wh {
			t.Errorf("PUNE-WH after project %q = %+v; want unchanged %+v", bad, got, wh)
		}
	}

	// The form's right goes first: no 422 for a user who can't save at all.
	bad := form(w.pune)
	bad.Set("branch_id", "abc")
	resp := w.h.Post(t, w.h.Env.Priya, "/m/site/new", bad)
	webtest.WantStatus(t, resp, http.StatusForbidden)
	bad.Set("version", ver(wh.Version))
	resp = w.h.Post(t, w.h.Env.Priya, path(wh.ID, ""), bad)
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, (&auth.DeniedError{Right: auth.RightWrite, Label: "Site"}).Error())
}

// The service's link checks show on their fields, on create and on update: a
// number that is no record reads as a non-number does, and a disabled branch
// or project names itself. A refused update changes nothing.
func TestScreens_SiteServiceLinkRefusals(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	ctx := context.Background()
	wh, _ := w.seed(t)

	// refused posts values with a create and with an update of PUNE-WH.
	refused := func(t *testing.T, values url.Values, branchMsg, projectMsg string) {
		t.Helper()
		resp := w.h.Post(t, kavya, "/m/site/new", values)
		webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
		wantFieldError(t, resp, "branch_id", branchMsg)
		wantFieldError(t, resp, "project_id", projectMsg)

		edit := url.Values{"version": {ver(wh.Version)}}
		for name, v := range values {
			edit[name] = v
		}
		edit.Set("code", "PUNE-WH")
		resp = w.h.Post(t, kavya, path(wh.ID, ""), edit)
		webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
		wantFieldError(t, resp, "branch_id", branchMsg)
		wantFieldError(t, resp, "project_id", projectMsg)
		if got := w.mustGet(t, "PUNE-WH"); got != wh {
			t.Errorf("PUNE-WH after a refused update = %+v; want unchanged %+v", got, wh)
		}
	}

	missing := form(w.pune)
	missing.Set("branch_id", "999999")
	missing.Set("project_id", "999999")
	refused(t, missing, msgNoBranch, msgNoProject)

	var blr company.Branch
	var metro project.Project
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		var err error
		if blr, err = company.New(w.h.Env.Kit).Get(ctx, tx, w.blr); err != nil {
			return err
		}
		metro, err = project.New(w.h.Env.Kit).Get(ctx, tx, w.prj)
		return err
	})
	if err := company.New(w.h.Env.Kit).SetDisabled(ctx, kavya, blr.ID, blr.Version, true); err != nil {
		t.Fatal(err)
	}
	if err := project.New(w.h.Env.Kit).SetDisabled(ctx, kavya, metro.ID, metro.Version, true); err != nil {
		t.Fatal(err)
	}
	off := form(w.blr)
	off.Set("project_id", dec(w.prj))
	refused(t, off, "Branch BLR is disabled. Pick another branch.", "Project PRJ-METRO is disabled. Pick another project.")
}

// The record page's form holds every stored value and its title is
// "{Code} {Name}" (CD-68); a reader without write sees the same as text, and
// links as "{Code} {Name}".
func TestScreens_SiteRecordValues(t *testing.T) {
	w := newWorld(t)
	_, metro := w.seed(t)

	page := w.h.Get(t, w.h.Env.Kavya, path(metro.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	want := map[string]string{"code": "METRO-SITE", "name": "Metro Site", "branch_id": dec(w.pune), "project_id": dec(w.prj)}
	for name, value := range want {
		if v, ok := page.Value(name); !ok || v != value {
			t.Errorf("form %s = %q (found %v); want %q", name, v, ok, value)
		}
	}
	if got := checkedType(page); got != "site" {
		t.Errorf("ticked type = %q; want the stored site", got)
	}
	if got := webtest.Squash(page.Doc.Find("h1").First().Text()); got != "METRO-SITE Metro Site" {
		t.Errorf("record title = %q; want METRO-SITE Metro Site", got)
	}
	webtest.WantText(t, page, puneDisplay)
	webtest.WantText(t, page, metroLink)

	read := w.h.Get(t, w.h.Env.Priya, path(metro.ID, ""))
	webtest.WantStatus(t, read, http.StatusOK)
	if read.HasControl("code") {
		t.Error("a reader without write gets the form")
	}
	webtest.WantText(t, read, puneDisplay)
	webtest.WantText(t, read, metroLink)
}

// The list's text and the page after each write, in the master's words.
func TestScreens_SiteCopy(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya

	empty := w.h.Get(t, kavya, "/m/site")
	webtest.WantStatus(t, empty, http.StatusOK)
	webtest.WantText(t, empty, "Sites")
	webtest.WantText(t, empty, "No sites yet.")
	webtest.WantText(t, empty, "New site")
	if !strings.Contains(empty.Body, "Search sites") {
		t.Error("list has no Search sites label")
	}

	resp := w.h.Post(t, kavya, "/m/site/new", form(w.pune))
	yard, ok := w.byCode(t, "PUNE-YARD")
	if !ok {
		t.Fatalf("valid POST saved nothing: %d %s", resp.Status, webtest.Squash(resp.Body))
	}
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Site saved.")
	webtest.WantText(t, w.h.Get(t, kavya, "/m/site"), "1 site")

	resp = w.h.Post(t, kavya, path(yard.ID, "/disable"), url.Values{"version": {ver(yard.Version)}})
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Site disabled.")
	webtest.WantText(t, w.h.Get(t, kavya, "/m/site"), "All sites are disabled.")

	off := w.mustGet(t, "PUNE-YARD")
	resp = w.h.Post(t, kavya, path(yard.ID, "/enable"), url.Values{"version": {ver(off.Version)}})
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Site enabled.")

	w.seed(t)
	list := w.h.Get(t, kavya, "/m/site")
	webtest.WantText(t, list, "3 sites")
	webtest.WantText(t, list, "Show disabled sites")

	on := w.mustGet(t, "PUNE-YARD")
	resp = w.h.Post(t, kavya, path(yard.ID, "/delete"), url.Values{"version": {ver(on.Version)}})
	webtest.WantBanner(t, w.h.Follow(t, kavya, resp), "Site deleted.")
}

// Writes carry the page's version: without it AUTH's 400, with an old one
// DOC's 409 on the record page. Nothing changes.
func TestScreens_SiteWriteVersion(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	wh, _ := w.seed(t)
	for _, suffix := range []string{"", "/disable", "/delete"} {
		resp := w.h.Post(t, kavya, path(wh.ID, suffix), form(w.pune))
		webtest.WantStatus(t, resp, http.StatusBadRequest)
		webtest.WantText(t, resp, auth.ErrBadForm.Error())
	}
	stale := ver(wh.Version - 1)
	want := strings.ReplaceAll(staleYard, "PUNE-YARD", "PUNE-WH")
	for _, suffix := range []string{"/disable", "/delete"} {
		resp := w.h.Post(t, kavya, path(wh.ID, suffix), url.Values{"version": {stale}})
		webtest.WantStatus(t, resp, http.StatusConflict)
		webtest.WantBanner(t, resp, want)
	}
	if got := w.mustGet(t, "PUNE-WH"); got != wh {
		t.Errorf("PUNE-WH after refused writes = %+v; want unchanged %+v", got, wh)
	}
}

// The delete confirm step shows the record's title and posts its version.
func TestScreens_SiteDeleteConfirm(t *testing.T) {
	w := newWorld(t)
	wh, _ := w.seed(t)

	confirm := w.h.Get(t, w.h.Env.Kavya, path(wh.ID, "?delete"))
	webtest.WantStatus(t, confirm, http.StatusOK)
	webtest.WantText(t, confirm, "Delete PUNE-WH Pune Warehouse?")
	if v, ok := confirm.FormValue(path(wh.ID, "/delete"), "version"); !ok || v != ver(wh.Version) {
		t.Errorf("confirm form version = %q (found %v); want %d", v, ok, wh.Version)
	}
}

// Export is the shared route under this master's Base: a CSV with its fields
// for a user with the right, AUTH's message for one without.
func TestScreens_SiteExport(t *testing.T) {
	w := newWorld(t)
	w.seed(t)

	file := w.h.Get(t, w.h.Env.Kavya, "/m/site/export.csv")
	webtest.WantStatus(t, file, http.StatusOK)
	if got := file.Header.Get("Content-Disposition"); got != `attachment; filename="site.csv"` {
		t.Errorf("Content-Disposition = %q", got)
	}
	if !strings.Contains(file.Body, "METRO-SITE,Metro Site") {
		t.Errorf("export = %q; want the METRO-SITE row", file.Body)
	}

	resp := w.h.Get(t, w.h.Env.Priya, "/m/site/export.csv")
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, (&auth.DeniedError{Right: auth.RightExport, Label: "Site"}).Error())
}

// Site type shows Warehouse and Site on the form, the list and the read-only
// page; the form posts, and the database and the export keep, the lower-case
// stored values (CD-175).
func TestScreens_SiteTypeLabels(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	wh, metro := w.seed(t)

	want := []webtest.Choice{{Value: "warehouse", Label: "Warehouse"}, {Value: "site", Label: "Site"}}
	if got := w.h.Get(t, kavya, "/m/site/new").Choices("type"); !slices.Equal(got, want) {
		t.Errorf("type choices = %+v; want %+v", got, want)
	}
	page := w.h.Get(t, kavya, path(metro.ID, ""))
	if got := page.Choices("type"); !slices.Equal(got, want) {
		t.Errorf("record type choices = %+v; want %+v", got, want)
	}
	if got := checkedType(page); got != "site" {
		t.Errorf("ticked type = %q; want the stored site", got)
	}

	list := w.h.Get(t, kavya, "/m/site")
	for _, c := range []struct {
		s    site.Site
		want string
	}{{wh, "TypeWarehouse"}, {metro, "TypeSite"}} {
		if text, _ := list.RowLink(path(c.s.ID, "")); !strings.Contains(text, c.want) {
			t.Errorf("list row %s = %q; want %q", c.s.Code, text, c.want)
		}
	}
	webtest.WantText(t, w.h.Get(t, w.h.Env.Priya, path(wh.ID, "")), "TypeWarehouse")
	webtest.WantText(t, w.h.Get(t, w.h.Env.Priya, path(metro.ID, "")), "TypeSite")

	file := w.h.Get(t, kavya, "/m/site/export.csv")
	webtest.WantStatus(t, file, http.StatusOK)
	for _, stored := range []string{",warehouse", ",site"} {
		if !strings.Contains(file.Body, stored) {
			t.Errorf("export = %q; want the stored %q", file.Body, stored)
		}
	}
	if strings.Contains(file.Body, ",Warehouse") || strings.Contains(file.Body, ",Site") {
		t.Errorf("export = %q; want no label", file.Body)
	}
}

// A user restricted to one site sees and changes only that site, on the real
// auth.Service, through every screen: list, record, picker and export use the
// site's own id (CD-09, CD-64).
func TestScreens_SiteRestrictedUserSeesOwnSite(t *testing.T) {
	w := newWorld(t)
	own, other := w.seed(t)
	w.restrictPriya(t, own.ID, auth.RightRead, auth.RightWrite, auth.RightDelete, auth.RightExport)
	priya := w.h.Env.Priya

	list := w.h.Get(t, priya, "/m/site")
	webtest.WantStatus(t, list, http.StatusOK)
	if _, ok := list.RowLink(path(own.ID, "")); !ok {
		t.Error("restricted list lacks her own PUNE-WH")
	}
	if _, ok := list.RowLink(path(other.ID, "")); ok {
		t.Error("restricted list shows METRO-SITE")
	}

	page := w.h.Get(t, priya, path(own.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	if !page.HasControl("code") {
		t.Error("her own record has no form, though she holds write")
	}
	webtest.WantStatus(t, w.h.Get(t, priya, path(other.ID, "")), http.StatusNotFound)

	var got []string
	for _, r := range w.h.GetHTMX(t, priya, "/pick/Site?q=").PickRows() {
		got = append(got, r.Value)
	}
	if !slices.Equal(got, []string{dec(own.ID)}) {
		t.Errorf("restricted picker = %v; want only her own %d", got, own.ID)
	}

	file := w.h.Get(t, priya, "/m/site/export.csv")
	webtest.WantStatus(t, file, http.StatusOK)
	if !strings.Contains(file.Body, "PUNE-WH") || strings.Contains(file.Body, "METRO-SITE") {
		t.Errorf("restricted export = %q; want PUNE-WH only", file.Body)
	}

	// A form with a parse error passes the right on her own site, so she gets
	// the 422; on another it is the same not found as the page.
	bad := form(w.pune)
	bad.Set("version", ver(own.Version))
	bad.Set("code", "PUNE-WH")
	bad.Set("branch_id", "abc")
	resp := w.h.Post(t, priya, path(own.ID, ""), bad)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	wantFieldError(t, resp, "branch_id", msgNoBranch)
	webtest.WantStatus(t, w.h.Post(t, priya, path(other.ID, ""), bad), http.StatusNotFound)

	resp = w.h.Post(t, priya, path(own.ID, "/disable"), url.Values{"version": {ver(own.Version)}})
	webtest.WantRedirect(t, resp, path(own.ID, ""))
}

// A user restricted on Site can't create one although she holds the right
// (CD-172): New is hidden, the form is refused with the service's message, and
// a form with a parse error gets that 403 before any 422.
func TestScreens_SiteRestrictedUserCannotCreate(t *testing.T) {
	w := newWorld(t)
	own, _ := w.seed(t)
	w.restrictPriya(t, own.ID, auth.RightRead, auth.RightCreate)
	priya := w.h.Env.Priya

	if _, ok := w.h.Get(t, priya, "/m/site").RowLink("/m/site/new"); ok {
		t.Error("restricted list links to /m/site/new")
	}
	refused := func(resp webtest.Response) {
		t.Helper()
		webtest.WantStatus(t, resp, http.StatusForbidden)
		webtest.WantText(t, resp, msgRestricted)
	}
	refused(w.h.Get(t, priya, "/m/site/new"))
	refused(w.h.Post(t, priya, "/m/site/new", form(w.pune)))
	bad := form(w.pune)
	bad.Set("branch_id", "abc")
	refused(w.h.Post(t, priya, "/m/site/new", bad))
	if _, ok := w.byCode(t, "PUNE-YARD"); ok {
		t.Error("restricted create saved PUNE-YARD")
	}
}
