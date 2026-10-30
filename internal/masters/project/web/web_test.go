package projectweb_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/project"
	projectweb "github.com/vardhan/internaltools/internal/masters/project/web"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// Screens under test (MST Task 22): /m/project list, /new, /{id}, POST /{id}
// with version, /{id}/disable, /{id}/enable, /{id}/delete and the picker
// /pick/Project?q= (CD-195). Field names are the DocType's Field names.
const (
	msgDates     = "End date can't be before start date." // MST-100
	msgStatus    = "Status must be Open, On Hold, Completed or Cancelled."
	msgBranch    = "Branch is required."                       // MST-104
	msgNoBranch  = "Branch not found. Pick one from the list." // CD-170
	msgNoParty   = "Customer not found. Pick one from the list."
	msgOffBranch = "Branch BLR is disabled. Pick another branch."                                               // CD-170
	msgOffParty  = "Customer LOCAL is disabled. Pick another customer."                                         // CD-170
	msgRestrict  = "You can only work with your assigned projects. Ask your administrator to add this project." // CD-172
	staleMetro   = "Someone else changed Project PRJ-METRO after you opened it. Reload it and make your changes again."

	puneTitle  = "PUNE Pune Works"
	shreeTitle = "SHREE Shree Steel Traders"
)

type world struct {
	h      *webtest.Harness
	svc    *project.Service
	branch int64
	shree  int64
}

// newWorld has the company, branch PUNE and customer SHREE, made through their
// services.
func newWorld(t *testing.T) *world {
	t.Helper()
	ctx := context.Background()
	h := webtest.New(t, func(k mstkit.Env) func(chi.Router) { return projectweb.Routes(project.New(k), k) })
	co := company.New(h.Env.Kit)
	kavya := h.Env.Kavya
	if err := co.SaveCompany(ctx, kavya, company.CompanyInput{LegalName: "Vardhan Infra Projects Pvt Ltd", PAN: "AAFCV2449G"}, 0); err != nil {
		t.Fatalf("SaveCompany: %v", err)
	}
	b, err := co.Create(ctx, kavya, company.BranchInput{Code: "PUNE", Name: "Pune Works", StateCode: "27", GSTIN: "27AAFCV2449G1Z7", IsHeadOffice: true})
	if err != nil {
		t.Fatalf("create branch PUNE: %v", err)
	}
	c, err := party.New(h.Env.Kit).Create(ctx, kavya, party.Input{Code: "SHREE", Name: "Shree Steel Traders", IsCustomer: true})
	if err != nil {
		t.Fatalf("create party SHREE: %v", err)
	}
	return &world{h: h, svc: project.New(h.Env.Kit), branch: b.ID, shree: c.ID}
}

func (w *world) read(t *testing.T, fn func(ctx context.Context, tx *db.Tx) error) {
	t.Helper()
	ctx := context.Background()
	if err := w.h.Env.DB.Read(ctx, func(tx *db.Tx) error { return fn(ctx, tx) }); err != nil {
		t.Fatal(err)
	}
}

func (w *world) byCode(t *testing.T, code string) (project.Project, bool) {
	t.Helper()
	var v project.Project
	var ok bool
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		var err error
		v, ok, err = w.svc.ByCode(ctx, tx, code)
		return err
	})
	return v, ok
}

func (w *world) mustGet(t *testing.T, code string) project.Project {
	t.Helper()
	v, ok := w.byCode(t, code)
	if !ok {
		t.Fatalf("project %s not found", code)
	}
	return v
}

// seed creates the fixture projects PRJ-METRO and PRJ-TECHPARK, out of name
// order.
func (w *world) seed(t *testing.T) (metro, tech project.Project) {
	t.Helper()
	for _, in := range []project.Input{
		{Code: "PRJ-TECHPARK", Name: "Whitefield Tech Park, Bengaluru", BranchID: w.branch},
		{Code: "PRJ-METRO", Name: "Pune Metro Depot", BranchID: w.branch},
	} {
		if _, err := w.svc.Create(context.Background(), w.h.Env.Kavya, in); err != nil {
			t.Fatalf("create %s: %v", in.Code, err)
		}
	}
	return w.mustGet(t, "PRJ-METRO"), w.mustGet(t, "PRJ-TECHPARK")
}

// full creates PRJ-FULL with every field set: customer SHREE, both dates, On
// Hold.
func (w *world) full(t *testing.T) project.Project {
	t.Helper()
	in := project.Input{
		Code: "PRJ-FULL", Name: "Full Project", BranchID: w.branch, CustomerID: w.shree, Status: project.OnHold,
		Start: types.Date{Year: 2026, Month: time.July, Day: 1}, End: types.Date{Year: 2027, Month: time.March, Day: 31},
	}
	if _, err := w.svc.Create(context.Background(), w.h.Env.Kavya, in); err != nil {
		t.Fatal(err)
	}
	return w.mustGet(t, "PRJ-FULL")
}

// disabledBranch is the id of branch BLR, disabled.
func (w *world) disabledBranch(t *testing.T) int64 {
	t.Helper()
	ctx := context.Background()
	co := company.New(w.h.Env.Kit)
	b, err := co.Create(ctx, w.h.Env.Kavya, company.BranchInput{Code: "BLR", Name: "Bengaluru", StateCode: "29", GSTIN: "29AAFCV2449G1Z3"})
	if err != nil {
		t.Fatalf("create branch BLR: %v", err)
	}
	if err := co.SetDisabled(ctx, w.h.Env.Kavya, b.ID, b.Version, true); err != nil {
		t.Fatal(err)
	}
	return b.ID
}

// disabledCustomer is the id of customer LOCAL, disabled.
func (w *world) disabledCustomer(t *testing.T) int64 {
	t.Helper()
	ctx := context.Background()
	parties := party.New(w.h.Env.Kit)
	c, err := parties.Create(ctx, w.h.Env.Kavya, party.Input{Code: "LOCAL", Name: "Local Hardware", IsCustomer: true})
	if err != nil {
		t.Fatalf("create party LOCAL: %v", err)
	}
	if err := parties.SetDisabled(ctx, w.h.Env.Kavya, c.ID, c.Version, true); err != nil {
		t.Fatal(err)
	}
	return c.ID
}

func (w *world) form(code, name string) url.Values {
	return url.Values{"code": {code}, "name": {name}, "branch_id": {id(w.branch)}, "status": {"Open"}}
}

func path(n int64, suffix string) string { return "/m/project/" + id(n) + suffix }

func id(n int64) string { return strconv.FormatInt(n, 10) }

func ver(v int) string { return strconv.Itoa(v) }

// wantFieldError checks msg shows next to field's own control: a form with two
// pickers has two search inputs named q.
func wantFieldError(t *testing.T, r webtest.Response, field, msg string) {
	t.Helper()
	webtest.WantFieldErrorID(t, r, components.FieldID(field), msg)
}

func TestScreens_ProjectCreateEditDisable(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	metro, tech := w.seed(t)

	list := w.h.Get(t, kavya, "/m/project")
	webtest.WantStatus(t, list, http.StatusOK)
	for _, v := range []project.Project{metro, tech} {
		text, ok := list.RowLink(path(v.ID, ""))
		if !ok || !strings.Contains(text, v.Code+" "+v.Name) || !strings.Contains(text, puneTitle) {
			t.Errorf("list row %s = %q (found %v); want %q and its branch", v.Code, text, ok, v.Code+" "+v.Name)
		}
	}
	if _, ok := list.RowLink("/m/project/new"); !ok {
		t.Error("list has no link to /m/project/new for Kavya (create right)")
	}

	blank := w.h.Get(t, kavya, "/m/project/new")
	webtest.WantStatus(t, blank, http.StatusOK)
	for _, name := range []string{"code", "name", "branch_id", "customer_id", "start_date", "end_date", "status"} {
		if !blank.HasControl(name) {
			t.Errorf("new project form has no control named %q", name)
		}
	}
	var want []webtest.Choice
	for _, v := range []string{"Open", "On Hold", "Completed", "Cancelled"} {
		want = append(want, webtest.Choice{Value: v, Label: v})
	}
	if got := blank.Choices("status"); !slices.Equal(got, want) {
		t.Errorf("status choices = %v; want %v", got, want)
	}

	// End before start: 422 with MST-100's message next to the field.
	form := w.form("PRJ-NEW", "New Project")
	form.Set("start_date", "01-07-2026")
	form.Set("end_date", "30-06-2026")
	resp := w.h.Post(t, kavya, "/m/project/new", form)
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	wantFieldError(t, resp, "end_date", msgDates)
	if v, _ := resp.Value("code"); v != "PRJ-NEW" {
		t.Errorf("422 page code value = %q; want the typed PRJ-NEW", v)
	}
	if v, _ := resp.Value("end_date"); v != "30-06-2026" {
		t.Errorf("422 page end_date value = %q; want the typed 30-06-2026", v)
	}
	if _, ok := w.byCode(t, "PRJ-NEW"); ok {
		t.Fatal("PRJ-NEW saved despite the 422")
	}

	// A valid create: 303 to the record.
	form.Set("end_date", "31-03-2027")
	form.Set("customer_id", id(w.shree))
	form.Set("status", "On Hold")
	resp = w.h.Post(t, kavya, "/m/project/new", form)
	created, ok := w.byCode(t, "PRJ-NEW")
	if !ok {
		t.Fatalf("valid POST saved nothing: %d %s", resp.Status, webtest.Squash(resp.Body))
	}
	webtest.WantRedirect(t, resp, path(created.ID, ""))
	if created.Name != "New Project" || created.BranchID != w.branch || created.CustomerID != w.shree || created.Status != project.OnHold ||
		created.Start.ISO() != "2026-07-01" || created.End.ISO() != "2027-03-31" {
		t.Errorf("stored PRJ-NEW = %+v", created)
	}

	page := w.h.Get(t, kavya, path(created.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	if v, _ := page.Value("version"); v != ver(created.Version) {
		t.Errorf("record page version = %q; want %d", v, created.Version)
	}

	// Edit with the page's version.
	form.Set("version", ver(created.Version))
	form.Set("name", "Renamed Project")
	resp = w.h.Post(t, kavya, path(created.ID, ""), form)
	webtest.WantRedirect(t, resp, path(created.ID, ""))
	edited := w.mustGet(t, "PRJ-NEW")
	if edited.Name != "Renamed Project" || edited.Version != created.Version+1 {
		t.Fatalf("after edit PRJ-NEW = %+v; want Renamed Project at version %d", edited, created.Version+1)
	}

	// Stale version: 409, DOC's message, typed values kept, nothing written.
	form = w.form("PRJ-METRO", "Stale name")
	form.Set("version", ver(metro.Version-1))
	resp = w.h.Post(t, kavya, path(metro.ID, ""), form)
	webtest.WantStatus(t, resp, http.StatusConflict)
	webtest.WantText(t, resp, staleMetro)
	if v, _ := resp.Value("name"); v != "Stale name" {
		t.Errorf("409 page name value = %q; want the typed Stale name", v)
	}
	if got := w.mustGet(t, "PRJ-METRO"); got != metro {
		t.Errorf("stale save changed PRJ-METRO: %+v", got)
	}

	// Disable: hidden by default, tagged Disabled with include_disabled.
	resp = w.h.Post(t, kavya, path(metro.ID, "/disable"), url.Values{"version": {ver(metro.Version)}})
	webtest.WantRedirect(t, resp, path(metro.ID, ""))
	off := w.mustGet(t, "PRJ-METRO")
	if !off.Disabled {
		t.Fatal("PRJ-METRO not disabled after POST /disable")
	}
	if _, ok := w.h.Get(t, kavya, "/m/project").RowLink(path(metro.ID, "")); ok {
		t.Error("disabled PRJ-METRO still in the default list")
	}
	text, ok := w.h.Get(t, kavya, "/m/project?include_disabled=true").RowLink(path(metro.ID, ""))
	if !ok || !strings.Contains(text, "Disabled") {
		t.Errorf("include_disabled row PRJ-METRO = %q (found %v); want it tagged Disabled", text, ok)
	}
	webtest.WantText(t, w.h.Get(t, kavya, path(metro.ID, "")), "Disabled")

	resp = w.h.Post(t, kavya, path(metro.ID, "/enable"), url.Values{"version": {ver(off.Version)}})
	webtest.WantRedirect(t, resp, path(metro.ID, ""))
	if w.mustGet(t, "PRJ-METRO").Disabled {
		t.Fatal("PRJ-METRO still disabled after POST /enable")
	}
	text, ok = w.h.Get(t, kavya, "/m/project").RowLink(path(metro.ID, ""))
	if !ok || strings.Contains(text, "Disabled") {
		t.Errorf("after enable PRJ-METRO row = %q (found %v); want it listed, not Disabled", text, ok)
	}
}

// Project's status (Open, On Hold, Completed, Cancelled; M13) shows as a plain
// value in the list, never as the Enabled or Disabled tag.
func TestScreens_ProjectListStatus(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	w.seed(t)
	full := w.full(t)

	list := w.h.Get(t, kavya, "/m/project")
	text, ok := list.RowLink(path(full.ID, ""))
	if !ok || !strings.Contains(text, "On Hold") {
		t.Errorf("list row PRJ-FULL = %q (found %v); want its status On Hold", text, ok)
	}
	if strings.Contains(text, "Enabled") {
		t.Errorf("list row PRJ-FULL = %q; want no Enabled tag", text)
	}
	if err := w.svc.SetDisabled(context.Background(), kavya, full.ID, full.Version, true); err != nil {
		t.Fatal(err)
	}
	text, _ = w.h.Get(t, kavya, "/m/project?include_disabled=true").RowLink(path(full.ID, ""))
	if !strings.Contains(text, "On Hold") || !strings.Contains(text, "Disabled") {
		t.Errorf("include_disabled row PRJ-FULL = %q; want On Hold and Disabled", text)
	}
}

// restrict limits user to project ids through the real auth.Service
// (lessons §5: authfake alone would hide a difference).
func (w *world) restrict(t *testing.T, user auth.UserID, ids ...int64) {
	t.Helper()
	ctx := context.Background()
	a, ok := w.h.Env.Kit.Auth.(*auth.Auth)
	if !ok {
		t.Fatalf("Kit.Auth is %T; want the real *auth.Auth", w.h.Env.Kit.Auth)
	}
	before, err := a.UserRestrictions(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ChangeRestrictions(ctx, w.h.Env.Admin, user, auth.DimProject, ids, before[auth.DimProject], nil); err != nil {
		t.Fatalf("ChangeRestrictions(%d, %v): %v", user, ids, err)
	}
}

// Priya's fixture restriction names the stand-in table's ids, so this test
// restricts her to the real PRJ-METRO. A read-only user gets AUTH's messages.
func TestScreens_ProjectReadOnlyForbidden(t *testing.T) {
	w := newWorld(t)
	priya := w.h.Env.Priya
	metro, _ := w.seed(t)
	w.restrict(t, priya.ID, metro.ID)
	deny := func(r auth.Right) string { return (&auth.DeniedError{Right: r, Label: "Project"}).Error() }

	list := w.h.Get(t, priya, "/m/project")
	webtest.WantStatus(t, list, http.StatusOK)
	if _, ok := list.RowLink(path(metro.ID, "")); !ok {
		t.Error("Priya's list lacks PRJ-METRO")
	}
	if _, ok := list.RowLink("/m/project/new"); ok {
		t.Error("Priya's list links to /m/project/new without the create right")
	}

	resp := w.h.Post(t, priya, "/m/project/new", w.form("PRJ-NEW", "New"))
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightCreate))
	if _, ok := w.byCode(t, "PRJ-NEW"); ok {
		t.Error("Priya's create saved PRJ-NEW")
	}

	form := w.form("PRJ-METRO", "Renamed by Priya")
	form.Set("version", ver(metro.Version))
	resp = w.h.Post(t, priya, path(metro.ID, ""), form)
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightWrite))

	resp = w.h.Post(t, priya, path(metro.ID, "/disable"), url.Values{"version": {ver(metro.Version)}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightWrite))

	resp = w.h.Post(t, priya, path(metro.ID, "/delete"), url.Values{"version": {ver(metro.Version)}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightDelete))

	if got := w.mustGet(t, "PRJ-METRO"); got != metro {
		t.Errorf("PRJ-METRO after Priya's refused writes = %+v; want unchanged %+v", got, metro)
	}
}

func TestScreens_ProjectPicker(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	metro, tech := w.seed(t)

	rows := w.h.GetHTMX(t, kavya, "/pick/Project?q=METRO")
	webtest.WantStatus(t, rows, http.StatusOK)
	got := rows.PickRows()
	if len(got) != 1 || got[0].Value != id(metro.ID) || !strings.Contains(got[0].Text, "PRJ-METRO Pune Metro Depot") {
		t.Errorf("pick?q=METRO = %v; want one row %d \"PRJ-METRO Pune Metro Depot\"", got, metro.ID)
	}
	got = w.h.GetHTMX(t, kavya, "/pick/Project?q=Whitefield").PickRows()
	if len(got) != 1 || got[0].Value != id(tech.ID) {
		t.Errorf("pick?q=Whitefield = %v; want PRJ-TECHPARK", got)
	}

	if err := w.svc.SetDisabled(context.Background(), kavya, metro.ID, metro.Version, true); err != nil {
		t.Fatal(err)
	}
	if got := w.h.GetHTMX(t, kavya, "/pick/Project?q=METRO").PickRows(); len(got) != 0 {
		t.Errorf("pick?q=METRO after disabling PRJ-METRO = %v; want no rows", got)
	}
}

func TestScreens_ProjectDelete(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	metro, _ := w.seed(t)

	resp := w.h.Post(t, kavya, path(metro.ID, "/delete"), url.Values{"version": {ver(metro.Version)}})
	webtest.WantRedirect(t, resp, "/m/project")
	if _, ok := w.byCode(t, "PRJ-METRO"); ok {
		t.Fatal("PRJ-METRO still exists after POST /delete")
	}
	var err error
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		_, err = w.svc.Get(ctx, tx, metro.ID)
		return nil
	})
	if !errors.Is(err, doctype.ErrNotFound) {
		t.Fatalf("Get after delete = %v; want doctype.ErrNotFound", err)
	}
	page := w.h.Get(t, kavya, path(metro.ID, ""))
	webtest.WantStatus(t, page, http.StatusNotFound)
	webtest.WantText(t, page, doctype.ErrNotFound.Error()) // CD-76
}
