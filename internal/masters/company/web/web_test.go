package companyweb_test

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
	companyweb "github.com/vardhan/internaltools/internal/masters/company/web"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Screens under test (MST Task 22): the singleton company at /m/company and
// /m/company/1 (no create, disable or delete: MST-019), and branches at
// /m/branch with /new, /{id}, POST /{id} with version, /{id}/disable,
// /{id}/enable, /{id}/delete and the picker /m/branch/pick?q=. Field names
// are the DocTypes' Field names.
const (
	legalName = "Vardhan Infra Projects Pvt Ltd"
	newName   = "Vardhan Infra Projects Private Limited"
	pan       = "AAFCV2449G"

	msgPAN        = "PAN must be 10 characters: 5 letters, 4 digits, 1 letter (e.g. AAFCV2449G)." // MST-020
	msgState      = "State is required. Pick the state where this branch is located."             // MST-021
	msgLastBranch = "At least one branch must stay enabled."                                      // MST-027
	staleCompany  = "Someone else changed Company " + newName + " after you opened it. Reload it and make your changes again."
	staleNashik   = "Someone else changed Branch NASHIK after you opened it. Reload it and make your changes again."
)

type world struct {
	h   *webtest.Harness
	svc *company.Service
}

// newWorld mounts companyweb.Routes and seeds the fixture company and its
// branches PUNE (head office) and BLR (00-fixtures, M7).
func newWorld(t *testing.T) *world {
	t.Helper()
	h := webtest.New(t, func(k mstkit.Env) func(chi.Router) { return companyweb.Routes(company.New(k), k) })
	w := &world{h: h, svc: company.New(h.Env.Kit)}
	ctx := context.Background()
	if err := w.svc.SaveCompany(ctx, h.Env.Kavya, company.CompanyInput{LegalName: legalName, PAN: pan}, 0); err != nil {
		t.Fatalf("SaveCompany: %v", err)
	}
	for _, in := range []company.BranchInput{
		{Code: "PUNE", Name: "Pune Works", StateCode: "27", GSTIN: "27AAFCV2449G1Z7", IsHeadOffice: true},
		{Code: "BLR", Name: "Bengaluru Site Office", StateCode: "29", GSTIN: "29AAFCV2449G1Z3"},
	} {
		if _, err := w.svc.Create(ctx, h.Env.Kavya, in); err != nil {
			t.Fatalf("create branch %s: %v", in.Code, err)
		}
	}
	return w
}

func (w *world) read(t *testing.T, fn func(ctx context.Context, tx *db.Tx) error) {
	t.Helper()
	ctx := context.Background()
	if err := w.h.Env.DB.Read(ctx, func(tx *db.Tx) error { return fn(ctx, tx) }); err != nil {
		t.Fatal(err)
	}
}

func (w *world) company(t *testing.T) company.Company {
	t.Helper()
	var c company.Company
	var ok bool
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		var err error
		c, ok, err = w.svc.Company(ctx, tx)
		return err
	})
	if !ok {
		t.Fatal("company missing")
	}
	return c
}

func (w *world) branch(t *testing.T, code string) (company.Branch, bool) {
	t.Helper()
	var b company.Branch
	var ok bool
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		var err error
		b, ok, err = w.svc.ByCode(ctx, tx, code)
		return err
	})
	return b, ok
}

func (w *world) mustBranch(t *testing.T, code string) company.Branch {
	t.Helper()
	b, ok := w.branch(t, code)
	if !ok {
		t.Fatalf("branch %s not found", code)
	}
	return b
}

func branchPath(id int64, suffix string) string {
	return "/m/branch/" + strconv.FormatInt(id, 10) + suffix
}

func ver(v int) string { return strconv.Itoa(v) }

func deny(r auth.Right, label string) string {
	return (&auth.DeniedError{Right: r, Label: label}).Error()
}

func TestScreens_CompanyEdit(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya

	list := w.h.Get(t, kavya, "/m/company")
	webtest.WantStatus(t, list, http.StatusOK)
	if text, ok := list.RowLink("/m/company/1"); !ok || !strings.Contains(text, legalName) {
		t.Errorf("company list row = %q (found %v); want a link to /m/company/1 with %q", text, ok, legalName)
	}

	page := w.h.Get(t, kavya, "/m/company/1")
	webtest.WantStatus(t, page, http.StatusOK)
	for name, want := range map[string]string{"version": "1", "name": legalName, "pan": pan} {
		if got, _ := page.Value(name); got != want {
			t.Errorf("company page %s = %q; want %q", name, got, want)
		}
	}

	// An invalid PAN: 422 with MST-020's message next to the PAN field.
	resp := w.h.Post(t, kavya, "/m/company/1", url.Values{"version": {"1"}, "name": {legalName}, "pan": {"AAFCV2449"}})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, "pan", msgPAN)
	if v, _ := resp.Value("pan"); v != "AAFCV2449" {
		t.Errorf("422 page pan value = %q; want the typed AAFCV2449", v)
	}
	if got := w.company(t); got.PAN != pan || got.Version != 1 {
		t.Fatalf("company after the 422 = %+v; want unchanged", got)
	}

	// A valid save: 303 back to the record; PAN normalised (MST-020).
	resp = w.h.Post(t, kavya, "/m/company/1", url.Values{"version": {"1"}, "name": {newName}, "pan": {"aafcv2449g"}})
	webtest.WantRedirect(t, resp, "/m/company/1")
	if got := w.company(t); got.LegalName != newName || got.PAN != pan || got.Version != 2 {
		t.Fatalf("company after save = %+v; want %q, %s, version 2", got, newName, pan)
	}

	// A stale version: 409 with DOC's stale message, typed values kept.
	resp = w.h.Post(t, kavya, "/m/company/1", url.Values{"version": {"1"}, "name": {"Stale Name Pvt Ltd"}, "pan": {pan}})
	webtest.WantStatus(t, resp, http.StatusConflict)
	webtest.WantText(t, resp, staleCompany)
	if v, _ := resp.Value("name"); v != "Stale Name Pvt Ltd" {
		t.Errorf("409 page name value = %q; want the typed Stale Name Pvt Ltd", v)
	}
	if got := w.company(t); got.LegalName != newName {
		t.Errorf("stale save overwrote the company: %q", got.LegalName)
	}

	// Company delete is not offered (MST-019, plan Task 3 Step 4).
	resp = w.h.Post(t, kavya, "/m/company/1/delete", url.Values{"version": {"2"}})
	if resp.Status < 400 {
		t.Errorf("POST /m/company/1/delete = %d; want a refusal (no company delete)", resp.Status)
	}
	w.company(t)
}

func TestScreens_CompanyReadOnlyForbidden(t *testing.T) {
	w := newWorld(t)
	priya := w.h.Env.Priya

	webtest.WantStatus(t, w.h.Get(t, priya, "/m/company"), http.StatusOK)
	resp := w.h.Post(t, priya, "/m/company/1", url.Values{"version": {"1"}, "name": {newName}, "pan": {pan}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightWrite, "Company"))
	if got := w.company(t); got.LegalName != legalName || got.Version != 1 {
		t.Errorf("company after Priya's refused save = %+v; want unchanged", got)
	}
}

func TestScreens_BranchCreateEditDisable(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	pune, blr := w.mustBranch(t, "PUNE"), w.mustBranch(t, "BLR")

	list := w.h.Get(t, kavya, "/m/branch")
	webtest.WantStatus(t, list, http.StatusOK)
	for _, b := range []company.Branch{pune, blr} {
		text, ok := list.RowLink(branchPath(b.ID, ""))
		if !ok || !strings.Contains(text, b.Code+" "+b.Name) {
			t.Errorf("list row %s = %q (found %v); want a link containing %q", b.Code, text, ok, b.Code+" "+b.Name)
		}
	}
	if _, ok := list.RowLink("/m/branch/new"); !ok {
		t.Error("list has no link to /m/branch/new for Kavya (create right)")
	}

	blank := w.h.Get(t, kavya, "/m/branch/new")
	webtest.WantStatus(t, blank, http.StatusOK)
	for _, name := range []string{"code", "name", "state_code", "gstin"} {
		if !blank.HasControl(name) {
			t.Errorf("new branch form has no control named %q", name)
		}
	}

	// No state: 422 with MST-021's message next to the state field.
	resp := w.h.Post(t, kavya, "/m/branch/new", url.Values{"code": {"MUM"}, "name": {"Mumbai Office"}, "state_code": {""}})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, resp, "state_code", msgState)
	if v, _ := resp.Value("code"); v != "MUM" {
		t.Errorf("422 page code value = %q; want the typed MUM", v)
	}
	if _, ok := w.branch(t, "MUM"); ok {
		t.Fatal("MUM saved despite the 422")
	}

	// A valid create (MST-024: no GSTIN): 303 to the record.
	resp = w.h.Post(t, kavya, "/m/branch/new", url.Values{"code": {"NASHIK"}, "name": {"Nashik Office"}, "state_code": {"27"}, "gstin": {""}})
	nashik, ok := w.branch(t, "NASHIK")
	if !ok {
		t.Fatalf("valid POST /m/branch/new saved nothing: %d %s", resp.Status, webtest.Squash(resp.Body))
	}
	webtest.WantRedirect(t, resp, branchPath(nashik.ID, ""))
	if nashik.Name != "Nashik Office" || nashik.StateCode != "27" || nashik.GSTIN != "" {
		t.Errorf("stored NASHIK = %+v", nashik)
	}

	page := w.h.Get(t, kavya, branchPath(nashik.ID, ""))
	webtest.WantStatus(t, page, http.StatusOK)
	if v, _ := page.Value("version"); v != ver(nashik.Version) {
		t.Errorf("record page version = %q; want %d", v, nashik.Version)
	}

	// Edit with the page's version.
	resp = w.h.Post(t, kavya, branchPath(nashik.ID, ""), url.Values{
		"version": {ver(nashik.Version)}, "code": {"NASHIK"}, "name": {"Nashik Road Office"}, "state_code": {"27"},
	})
	webtest.WantRedirect(t, resp, branchPath(nashik.ID, ""))
	edited := w.mustBranch(t, "NASHIK")
	if edited.Name != "Nashik Road Office" || edited.Version != nashik.Version+1 {
		t.Fatalf("after edit NASHIK = %+v; want Nashik Road Office at version %d", edited, nashik.Version+1)
	}

	// Stale version: 409, DOC's message, typed values kept, nothing written.
	resp = w.h.Post(t, kavya, branchPath(nashik.ID, ""), url.Values{
		"version": {ver(nashik.Version)}, "code": {"NASHIK"}, "name": {"Stale Office"}, "state_code": {"27"},
	})
	webtest.WantStatus(t, resp, http.StatusConflict)
	webtest.WantText(t, resp, staleNashik)
	if v, _ := resp.Value("name"); v != "Stale Office" {
		t.Errorf("409 page name value = %q; want the typed Stale Office", v)
	}
	if got := w.mustBranch(t, "NASHIK"); got.Name != "Nashik Road Office" {
		t.Errorf("stale save overwrote NASHIK: %q", got.Name)
	}

	// Disable BLR: hidden by default, tagged Disabled with include_disabled.
	resp = w.h.Post(t, kavya, branchPath(blr.ID, "/disable"), url.Values{"version": {ver(blr.Version)}})
	webtest.WantRedirect(t, resp, branchPath(blr.ID, ""))
	blrOff := w.mustBranch(t, "BLR")
	if !blrOff.Disabled {
		t.Fatal("BLR not disabled after POST /disable")
	}
	if _, ok := w.h.Get(t, kavya, "/m/branch").RowLink(branchPath(blr.ID, "")); ok {
		t.Error("disabled BLR still in the default list")
	}
	text, ok := w.h.Get(t, kavya, "/m/branch?include_disabled=true").RowLink(branchPath(blr.ID, ""))
	if !ok || !strings.Contains(text, "Disabled") {
		t.Errorf("include_disabled row BLR = %q (found %v); want it tagged Disabled", text, ok)
	}
	webtest.WantText(t, w.h.Get(t, kavya, branchPath(blr.ID, "")), "Disabled")

	// Enable brings it back.
	resp = w.h.Post(t, kavya, branchPath(blr.ID, "/enable"), url.Values{"version": {ver(blrOff.Version)}})
	webtest.WantRedirect(t, resp, branchPath(blr.ID, ""))
	if w.mustBranch(t, "BLR").Disabled {
		t.Fatal("BLR still disabled after POST /enable")
	}
	text, ok = w.h.Get(t, kavya, "/m/branch").RowLink(branchPath(blr.ID, ""))
	if !ok || strings.Contains(text, "Disabled") {
		t.Errorf("after enable BLR row = %q (found %v); want it listed, not Disabled", text, ok)
	}

	// The last enabled branch can't be disabled: 422 with MST-027's
	// message on the page (a refusal with no field, CD-44).
	ctx := context.Background()
	for _, code := range []string{"BLR", "NASHIK"} {
		b := w.mustBranch(t, code)
		if err := w.svc.SetDisabled(ctx, kavya, b.ID, b.Version, true); err != nil {
			t.Fatalf("disable %s: %v", code, err)
		}
	}
	resp = w.h.Post(t, kavya, branchPath(pune.ID, "/disable"), url.Values{"version": {ver(pune.Version)}})
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantText(t, resp, msgLastBranch)
	if w.mustBranch(t, "PUNE").Disabled {
		t.Error("PUNE disabled despite the 422")
	}
}

func TestScreens_BranchReadOnlyForbidden(t *testing.T) {
	w := newWorld(t)
	priya := w.h.Env.Priya
	blr := w.mustBranch(t, "BLR")

	list := w.h.Get(t, priya, "/m/branch")
	webtest.WantStatus(t, list, http.StatusOK)
	if _, ok := list.RowLink(branchPath(blr.ID, "")); !ok {
		t.Error("Priya's list lacks BLR")
	}
	if _, ok := list.RowLink("/m/branch/new"); ok {
		t.Error("Priya's list links to /m/branch/new without the create right")
	}

	resp := w.h.Post(t, priya, "/m/branch/new", url.Values{"code": {"NASHIK"}, "name": {"Nashik Office"}, "state_code": {"27"}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightCreate, "Branch"))
	if _, ok := w.branch(t, "NASHIK"); ok {
		t.Error("Priya's create saved NASHIK")
	}

	resp = w.h.Post(t, priya, branchPath(blr.ID, ""), url.Values{
		"version": {ver(blr.Version)}, "code": {"BLR"}, "name": {"Renamed by Priya"}, "state_code": {"29"}, "gstin": {blr.GSTIN},
	})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightWrite, "Branch"))

	resp = w.h.Post(t, priya, branchPath(blr.ID, "/disable"), url.Values{"version": {ver(blr.Version)}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightWrite, "Branch"))

	resp = w.h.Post(t, priya, branchPath(blr.ID, "/delete"), url.Values{"version": {ver(blr.Version)}})
	webtest.WantStatus(t, resp, http.StatusForbidden)
	webtest.WantText(t, resp, deny(auth.RightDelete, "Branch"))

	if got := w.mustBranch(t, "BLR"); got.Name != blr.Name || got.Disabled || got.Version != blr.Version {
		t.Errorf("BLR after Priya's refused writes = %+v; want unchanged %+v", got, blr)
	}
}

func TestScreens_BranchPicker(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	pune, blr := w.mustBranch(t, "PUNE"), w.mustBranch(t, "BLR")

	rows := w.h.GetHTMX(t, kavya, "/m/branch/pick?q=Pune")
	webtest.WantStatus(t, rows, http.StatusOK)
	got := rows.PickRows()
	if len(got) != 1 || got[0].Value != strconv.FormatInt(pune.ID, 10) || !strings.Contains(got[0].Text, "PUNE Pune Works") {
		t.Errorf("pick?q=Pune = %v; want one row %d \"PUNE Pune Works\"", got, pune.ID)
	}
	got = w.h.GetHTMX(t, kavya, "/m/branch/pick?q=Bengaluru").PickRows()
	if len(got) != 1 || got[0].Value != strconv.FormatInt(blr.ID, 10) || !strings.Contains(got[0].Text, "BLR Bengaluru Site Office") {
		t.Errorf("pick?q=Bengaluru = %v; want one row %d \"BLR Bengaluru Site Office\"", got, blr.ID)
	}

	if err := w.svc.SetDisabled(context.Background(), kavya, blr.ID, blr.Version, true); err != nil {
		t.Fatal(err)
	}
	if got := w.h.GetHTMX(t, kavya, "/m/branch/pick?q=Bengaluru").PickRows(); len(got) != 0 {
		t.Errorf("pick?q=Bengaluru after disabling BLR = %v; want no rows", got)
	}
}

func TestScreens_BranchDelete(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Env.Kavya
	saved, err := w.svc.Create(context.Background(), kavya, company.BranchInput{Code: "NASHIK", Name: "Nashik Office", StateCode: "27"})
	if err != nil {
		t.Fatal(err)
	}

	resp := w.h.Post(t, kavya, branchPath(saved.ID, "/delete"), url.Values{"version": {ver(saved.Version)}})
	webtest.WantRedirect(t, resp, "/m/branch")
	if _, ok := w.branch(t, "NASHIK"); ok {
		t.Fatal("NASHIK still exists after POST /delete")
	}
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		_, err = w.svc.Get(ctx, tx, saved.ID)
		return nil
	})
	if !errors.Is(err, doctype.ErrNotFound) {
		t.Fatalf("Get after delete = %v; want doctype.ErrNotFound", err)
	}
	page := w.h.Get(t, kavya, branchPath(saved.ID, ""))
	webtest.WantStatus(t, page, http.StatusNotFound)
	webtest.WantText(t, page, doctype.ErrNotFound.Error()) // CD-76
}
