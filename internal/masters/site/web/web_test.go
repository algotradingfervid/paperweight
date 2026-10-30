package siteweb_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	siteweb "github.com/vardhan/internaltools/internal/masters/site/web"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// Screens under test (MST Task 22): /m/site list, /new, /{id}, POST /{id} with
// version, /{id}/disable, /{id}/enable, /{id}/delete, /export.csv and the
// picker /pick/Site?q= (CD-195). Field names are the DocType's Field names.
const (
	msgType     = "Type must be Warehouse or Site." // MST-105
	msgBranch   = "Branch is required."             // MST-105
	staleYard   = "Someone else changed Site PUNE-YARD after you opened it. Reload it and make your changes again."
	puneDisplay = "PUNE Pune Works"            // CD-68
	metroLink   = "PRJ-METRO Pune Metro Depot" // CD-68
)

type world struct {
	h    *webtest.Harness
	svc  *site.Service
	pune int64 // branch PUNE
	blr  int64 // branch BLR
	prj  int64 // project PRJ-METRO, branch PUNE
}

// newWorld is the screens over the company with branches PUNE and BLR and the
// project PRJ-METRO, made through their services.
func newWorld(t *testing.T) *world {
	t.Helper()
	ctx := context.Background()
	h := webtest.New(t, func(k mstkit.Env) func(chi.Router) {
		return siteweb.Routes(site.New(k), k)
	})
	kavya := h.Env.Kavya
	co := company.New(h.Env.Kit)
	if err := co.SaveCompany(ctx, kavya, company.CompanyInput{LegalName: "Vardhan Infra Projects Pvt Ltd", PAN: "AAFCV2449G"}, 0); err != nil {
		t.Fatal(err)
	}
	pune, err := co.Create(ctx, kavya, company.BranchInput{Code: "PUNE", Name: "Pune Works", StateCode: "27", GSTIN: "27AAFCV2449G1Z7", IsHeadOffice: true})
	if err != nil {
		t.Fatal(err)
	}
	blr, err := co.Create(ctx, kavya, company.BranchInput{Code: "BLR", Name: "Bengaluru Site Office", StateCode: "29", GSTIN: "29AAFCV2449G1Z3"})
	if err != nil {
		t.Fatal(err)
	}
	metro, err := project.New(h.Env.Kit).Create(ctx, kavya, project.Input{Code: "PRJ-METRO", Name: "Pune Metro Depot", BranchID: pune.ID})
	if err != nil {
		t.Fatal(err)
	}
	return &world{h: h, svc: site.New(h.Env.Kit), pune: pune.ID, blr: blr.ID, prj: metro.ID}
}

func (w *world) read(t *testing.T, fn func(ctx context.Context, tx *db.Tx) error) {
	t.Helper()
	ctx := context.Background()
	if err := w.h.Env.DB.Read(ctx, func(tx *db.Tx) error { return fn(ctx, tx) }); err != nil {
		t.Fatal(err)
	}
}

func (w *world) byCode(t *testing.T, code string) (site.Site, bool) {
	t.Helper()
	var v site.Site
	var ok bool
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		var err error
		v, ok, err = w.svc.ByCode(ctx, tx, code)
		return err
	})
	return v, ok
}

func (w *world) mustGet(t *testing.T, code string) site.Site {
	t.Helper()
	v, ok := w.byCode(t, code)
	if !ok {
		t.Fatalf("site %s not found", code)
	}
	return v
}

// seed creates PUNE-WH, a warehouse with no project, and METRO-SITE, a site
// on PRJ-METRO (MST-105, MST-106).
func (w *world) seed(t *testing.T) (wh, metro site.Site) {
	t.Helper()
	ctx := context.Background()
	for _, in := range []site.Input{
		{Code: "PUNE-WH", Name: "Pune Warehouse", BranchID: w.pune, Type: site.Warehouse},
		{Code: "METRO-SITE", Name: "Metro Site", BranchID: w.pune, ProjectID: w.prj, Type: site.SiteKind},
	} {
		if _, err := w.svc.Create(ctx, w.h.Env.Kavya, in); err != nil {
			t.Fatalf("create %s: %v", in.Code, err)
		}
	}
	return w.mustGet(t, "PUNE-WH"), w.mustGet(t, "METRO-SITE")
}

func path(id int64, suffix string) string {
	return "/m/site/" + strconv.FormatInt(id, 10) + suffix
}

func ver(v int) string { return strconv.Itoa(v) }

func dec(id int64) string { return strconv.FormatInt(id, 10) }

// form is a save of PUNE-YARD, a warehouse at branch; tests change a field.
func form(branch int64) url.Values {
	return url.Values{"code": {"PUNE-YARD"}, "name": {"Pune Yard"}, "type": {"warehouse"}, "branch_id": {dec(branch)}, "project_id": {""}}
}

// wantFieldError checks msg is next to the control with field's id. The form
// has two pickers, whose search inputs share the name q, so the name can't
// tell them apart.
func wantFieldError(t *testing.T, r webtest.Response, field, msg string) {
	t.Helper()
	webtest.WantFieldErrorID(t, r, components.FieldID(field), msg)
}

// checkedType is the stored value of the type radio that is ticked.
func checkedType(r webtest.Response) string {
	v, _ := r.Doc.Find(`input[type="radio"][name="type"][checked]`).Attr("value")
	return v
}

func TestScreens_SiteCreateEditDisable(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	wh, metro := w.seed(t)

	list := w.h.Get(t, kavya, "/m/site")
	webtest.WantStatus(t, list, http.StatusOK)
	for _, v := range []site.Site{wh, metro} {
		text, ok := list.RowLink(path(v.ID, ""))
		if !ok || !strings.Contains(text, v.Code+" "+v.Name) {
			t.Errorf("list row %s = %q (found %v); want a link containing %q", v.Code, text, ok, v.Code+" "+v.Name)
		}
		if !strings.Contains(text, puneDisplay) {
			t.Errorf("list row %s = %q; want the branch as %q (CD-68)", v.Code, text, puneDisplay)
		}
	}
	if _, ok := list.RowLink("/m/site/new"); !ok {
		t.Error("list has no link to /m/site/new for Kavya (create right, unrestricted)")
	}

	blank := w.h.Get(t, kavya, "/m/site/new")
	webtest.WantStatus(t, blank, http.StatusOK)
	for _, name := range []string{"code", "name", "type", "branch_id", "project_id"} {
		if !blank.HasControl(name) {
			t.Errorf("new site form has no control named %q", name)
		}
	}
	// The form posts the stored values (CD-175).
	if got := blank.Choices("type"); len(got) != 2 || got[0].Value != "warehouse" || got[1].Value != "site" {
		t.Errorf("type choices = %+v; want the stored values warehouse, site", got)
	}

	// An unknown type and no branch: 422, each message next to its field.
	bad := form(w.pune)
	bad.Set("type", "depot")
	bad.Set("branch_id", "")
	resp := w.h.Post(t, kavya, "/m/site/new", bad)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	wantFieldError(t, resp, "type", msgType)
	wantFieldError(t, resp, "branch_id", msgBranch)
	if v, _ := resp.Value("code"); v != "PUNE-YARD" {
		t.Errorf("422 page code value = %q; want the typed PUNE-YARD", v)
	}
	if _, ok := w.byCode(t, "PUNE-YARD"); ok {
		t.Fatal("PUNE-YARD saved despite the 422")
	}

	// A valid create: 303 to the record, type stored in lower case (CD-175).
	resp = w.h.Post(t, kavya, "/m/site/new", form(w.pune))
	yard, ok := w.byCode(t, "PUNE-YARD")
	if !ok {
		t.Fatalf("valid POST saved nothing: %d %s", resp.Status, webtest.Squash(resp.Body))
	}
	webtest.WantRedirect(t, resp, path(yard.ID, ""))
	if yard.Name != "Pune Yard" || yard.Type != site.Warehouse || yard.BranchID != w.pune || yard.ProjectID != 0 {
		t.Errorf("stored PUNE-YARD = %+v; want Pune Yard, warehouse, PUNE, no project", yard)
	}

	// The record page posts its version and shows the branch as "{Code} {Name}".
	page := w.h.Get(t, kavya, path(yard.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	if v, _ := page.Value("version"); v != ver(yard.Version) {
		t.Errorf("record page version = %q; want %d", v, yard.Version)
	}
	webtest.WantText(t, page, puneDisplay)

	// An edit moves it to a site on PRJ-METRO.
	edit := form(w.pune)
	edit.Set("version", ver(yard.Version))
	edit.Set("name", "Yard Two")
	edit.Set("type", "site")
	edit.Set("project_id", dec(w.prj))
	resp = w.h.Post(t, kavya, path(yard.ID, ""), edit)
	webtest.WantRedirect(t, resp, path(yard.ID, ""))
	edited := w.mustGet(t, "PUNE-YARD")
	if edited.Name != "Yard Two" || edited.Type != site.SiteKind || edited.ProjectID != w.prj || edited.Version != yard.Version+1 {
		t.Fatalf("after edit PUNE-YARD = %+v; want Yard Two, site, PRJ-METRO, version %d", edited, yard.Version+1)
	}

	// Stale version: 409, DOC's message, typed values kept, nothing written.
	stale := form(w.pune)
	stale.Set("version", ver(yard.Version))
	stale.Set("name", "Stale Yard")
	resp = w.h.Post(t, kavya, path(yard.ID, ""), stale)
	webtest.WantStatus(t, resp, http.StatusConflict)
	webtest.WantText(t, resp, staleYard)
	if v, _ := resp.Value("name"); v != "Stale Yard" {
		t.Errorf("409 page name value = %q; want the typed Stale Yard", v)
	}
	if got := w.mustGet(t, "PUNE-YARD"); got != edited {
		t.Errorf("stale save changed PUNE-YARD: %+v; want %+v", got, edited)
	}

	// Disable: hidden by default, tagged Disabled with include_disabled.
	resp = w.h.Post(t, kavya, path(yard.ID, "/disable"), url.Values{"version": {ver(edited.Version)}})
	webtest.WantRedirect(t, resp, path(yard.ID, ""))
	off := w.mustGet(t, "PUNE-YARD")
	if !off.Disabled {
		t.Fatal("PUNE-YARD not disabled after POST /disable")
	}
	if _, ok := w.h.Get(t, kavya, "/m/site").RowLink(path(yard.ID, "")); ok {
		t.Error("disabled PUNE-YARD still in the default list")
	}
	text, ok := w.h.Get(t, kavya, "/m/site?include_disabled=true").RowLink(path(yard.ID, ""))
	if !ok || !strings.Contains(text, "Disabled") {
		t.Errorf("include_disabled row PUNE-YARD = %q (found %v); want it tagged Disabled", text, ok)
	}
	webtest.WantText(t, w.h.Get(t, kavya, path(yard.ID, "")), "Disabled")

	resp = w.h.Post(t, kavya, path(yard.ID, "/enable"), url.Values{"version": {ver(off.Version)}})
	webtest.WantRedirect(t, resp, path(yard.ID, ""))
	if w.mustGet(t, "PUNE-YARD").Disabled {
		t.Fatal("PUNE-YARD still disabled after POST /enable")
	}
	text, ok = w.h.Get(t, kavya, "/m/site").RowLink(path(yard.ID, ""))
	if !ok || strings.Contains(text, "Disabled") {
		t.Errorf("after enable PUNE-YARD row = %q (found %v); want it listed, not Disabled", text, ok)
	}
}

func TestScreens_SiteReadOnlyForbidden(t *testing.T) {
	w := newWorld(t)
	priya := w.h.Env.Priya
	wh, _ := w.seed(t)
	deny := func(r auth.Right) string { return (&auth.DeniedError{Right: r, Label: "Site"}).Error() }

	list := w.h.Get(t, priya, "/m/site")
	webtest.WantStatus(t, list, http.StatusOK)
	if _, ok := list.RowLink(path(wh.ID, "")); !ok {
		t.Error("Priya's list lacks PUNE-WH")
	}
	if _, ok := list.RowLink("/m/site/new"); ok {
		t.Error("Priya's list links to /m/site/new without the create right")
	}

	resp := w.h.Post(t, priya, "/m/site/new", form(w.pune))
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightCreate))
	if _, ok := w.byCode(t, "PUNE-YARD"); ok {
		t.Error("Priya's create saved PUNE-YARD")
	}

	edit := form(w.pune)
	edit.Set("version", ver(wh.Version))
	resp = w.h.Post(t, priya, path(wh.ID, ""), edit)
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightWrite))

	resp = w.h.Post(t, priya, path(wh.ID, "/disable"), url.Values{"version": {ver(wh.Version)}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightWrite))

	resp = w.h.Post(t, priya, path(wh.ID, "/delete"), url.Values{"version": {ver(wh.Version)}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightDelete))

	if got := w.mustGet(t, "PUNE-WH"); got != wh {
		t.Errorf("PUNE-WH after Priya's refused writes = %+v; want unchanged %+v", got, wh)
	}
}

func TestScreens_SitePicker(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	wh, metro := w.seed(t)

	rows := w.h.GetHTMX(t, kavya, "/pick/Site?q=Pune")
	webtest.WantStatus(t, rows, http.StatusOK)
	got := rows.PickRows()
	if len(got) != 1 || got[0].Value != dec(wh.ID) || !strings.Contains(got[0].Text, "PUNE-WH Pune Warehouse") {
		t.Errorf("pick?q=Pune = %v; want one row %d \"PUNE-WH Pune Warehouse\"", got, wh.ID)
	}
	got = w.h.GetHTMX(t, kavya, "/pick/Site?q=Metro").PickRows()
	if len(got) != 1 || got[0].Value != dec(metro.ID) || !strings.Contains(got[0].Text, "METRO-SITE Metro Site") {
		t.Errorf("pick?q=Metro = %v; want one row %d \"METRO-SITE Metro Site\"", got, metro.ID)
	}

	if err := w.svc.SetDisabled(context.Background(), kavya, metro.ID, metro.Version, true); err != nil {
		t.Fatal(err)
	}
	if got := w.h.GetHTMX(t, kavya, "/pick/Site?q=Metro").PickRows(); len(got) != 0 {
		t.Errorf("pick?q=Metro after disabling METRO-SITE = %v; want no rows", got)
	}
}

func TestScreens_SiteDelete(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	wh, _ := w.seed(t)

	resp := w.h.Post(t, kavya, path(wh.ID, "/delete"), url.Values{"version": {ver(wh.Version)}})
	webtest.WantRedirect(t, resp, "/m/site")
	if _, ok := w.byCode(t, "PUNE-WH"); ok {
		t.Fatal("PUNE-WH still exists after POST /delete")
	}
	var err error
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		_, err = w.svc.Get(ctx, tx, wh.ID)
		return nil
	})
	if !errors.Is(err, doctype.ErrNotFound) {
		t.Fatalf("Get after delete = %v; want doctype.ErrNotFound", err)
	}
	page := w.h.Get(t, kavya, path(wh.ID, ""))
	webtest.WantStatus(t, page, http.StatusNotFound)
	webtest.WantText(t, page, doctype.ErrNotFound.Error()) // CD-76
}
