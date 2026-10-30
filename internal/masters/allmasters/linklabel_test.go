package allmasters_test

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// CD-233 on the real auth.Service: a posted link value shows its label only to
// a user who can read the target, so a form can't be used to read a hidden row.

const llHiddenCode, llHiddenName = "HIDE-1", "Secret Branch"

// llScoped is an own-scope user on the Branch target, screen and all.
func llScoped(t *testing.T, w *nsWorld, source mstkit.Kind) auth.Principal {
	t.Helper()
	return w.lrScoped(t, lrLink{source: source, target: company.Kind})
}

// llBigIDs makes the next branch ids long, so the id can be told from every
// other number on a page.
func llBigIDs(t *testing.T, w *nsWorld) {
	t.Helper()
	err := w.DB.Write(t.Context(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(t.Context(), `UPDATE sqlite_sequence SET seq = 7770000 WHERE name = 'branch'`)
		return err
	})
	if err != nil {
		t.Fatalf("sqlite_sequence: %v", err)
	}
}

// llPage is r's page with the posted id and the per-request tokens taken out.
var llTokens = regexp.MustCompile(`(nonce|value)="[A-Za-z0-9+/=_-]{20,}"`)

func llPage(r webtest.Response, id int64) string {
	return llTokens.ReplaceAllString(strings.ReplaceAll(r.Body, strconv.FormatInt(id, 10), "<id>"), "")
}

func llWantHidden(t *testing.T, what, hidden, unknown string) {
	t.Helper()
	if hidden != unknown {
		at := 0
		for at < min(len(hidden), len(unknown)) && hidden[at] == unknown[at] {
			at++
		}
		t.Errorf("%s: the page for a hidden id differs from an unknown id's at %d\nhidden:  %.120q\nunknown: %.120q", what, at, hidden[at:], unknown[at:])
	}
	for _, secret := range []string{llHiddenCode, llHiddenName} {
		if strings.Contains(hidden, secret) {
			t.Errorf("%s: the page names the target: %s", what, secret)
		}
	}
}

func llProjectForm(branch int64) url.Values {
	return url.Values{"code": {"SRC-1"}, "name": {"Row"}, "branch_id": {strconv.FormatInt(branch, 10)}, "status": {"Open"}}
}

// TestCD233_FormRedrawHidesTheLabel: the 422 page of a create and of a save.
func TestCD233_FormRedrawHidesTheLabel(t *testing.T) {
	w, h := pgScreens(t)
	own := llScoped(t, w, project.Kind)
	llBigIDs(t, w)
	hidden := lrMaster(t, company.DocType).ops(w)
	secret := pgCreate(t, hidden, w.Kavya, llHiddenCode, llHiddenName)
	mine := pgCreate(t, hidden, own, "MINE-1", "Own Branch")
	unknown := lrUnknown

	refuse := func(path string, form url.Values, id int64) string {
		t.Helper()
		r := h.Post(t, own, path, form)
		webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
		return llPage(r, id)
	}
	llWantHidden(t, "create",
		refuse("/m/project/new", llProjectForm(secret.ID), secret.ID),
		refuse("/m/project/new", llProjectForm(unknown), unknown))

	// A readable posted id keeps its label on the same refusal.
	bad := llProjectForm(mine.ID)
	bad.Set("code", "")
	r := h.Post(t, own, "/m/project/new", bad)
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantText(t, r, "MINE-1 Own Branch")

	saved := w.save(t, func(ctx context.Context) (mstkit.Saved, error) {
		in := nsProjectInput(w, "SRC-2", "Row")
		in.BranchID = mine.ID
		return w.svc.Project.Create(ctx, own, in)
	})
	path, form := pgRecord("/m/project", saved), func(id int64) url.Values {
		f := llProjectForm(id)
		f.Set("code", "SRC-2")
		f.Set("version", strconv.Itoa(saved.Version))
		return f
	}
	llWantHidden(t, "save", refuse(path, form(secret.ID), secret.ID), refuse(path, form(unknown), unknown))

	// A stored link keeps its label after the user stops reading its target,
	// on the record page and on a refused save that leaves it alone (CD-228).
	if err := w.pgAuth(t).SetRights(t.Context(), w.Admin, lrRole, company.DocType, map[auth.Right]auth.Scope{}); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	page := h.Get(t, own, path)
	webtest.WantStatus(t, page, http.StatusOK)
	webtest.WantText(t, page, "MINE-1 Own Branch")
	bad = form(mine.ID)
	bad.Set("code", "")
	r = h.Post(t, own, path, bad)
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantText(t, r, "MINE-1 Own Branch")
}

// TestCD233_SectionKeepsAStoredLabel: a section form on a record the user can
// see shows its stored link's label after the target stops being readable, and
// the raw id for a posted hidden one.
func TestCD233_SectionKeepsAStoredLabel(t *testing.T) {
	w, h := pgScreens(t)
	own := llScoped(t, w, employee.Kind)
	llBigIDs(t, w)
	branches := lrMaster(t, company.DocType).ops(w)
	secret := pgCreate(t, branches, w.Kavya, llHiddenCode, llHiddenName)
	mine := pgCreate(t, branches, own, "MINE-1", "Own Branch")
	saved := w.save(t, func(ctx context.Context) (mstkit.Saved, error) {
		in := nsEmployeeInput(w, "EMP-901", "Row")
		in.BranchID = mine.ID
		return w.svc.Employee.Create(ctx, own, in)
	})
	path := pgRecord("/m/employee", saved) + "/edit/reporting"
	posted := func(id int64) url.Values {
		return url.Values{"version": {strconv.Itoa(saved.Version)}, "branch_id": {strconv.FormatInt(id, 10)}, "manager_id": {""}}
	}
	refuse := func(id int64) string {
		t.Helper()
		r := h.Post(t, own, path, posted(id))
		webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
		return llPage(r, id)
	}
	llWantHidden(t, "section", refuse(secret.ID), refuse(lrUnknown))

	if err := w.pgAuth(t).SetRights(t.Context(), w.Admin, lrRole, company.DocType, map[auth.Right]auth.Scope{}); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	page := h.Get(t, own, path)
	webtest.WantStatus(t, page, http.StatusOK)
	webtest.WantText(t, page, "MINE-1 Own Branch")
}

// TestCD233_WizardHidesTheLabel: every step redraw and Check and save.
func TestCD233_WizardHidesTheLabel(t *testing.T) {
	const (
		details   = "/m/employee/new/details"
		reporting = "/m/employee/new/reporting"
		check     = "/m/employee/new/check"
	)
	w, h := pgScreens(t)
	own := llScoped(t, w, employee.Kind)
	llBigIDs(t, w)
	branches := lrMaster(t, company.DocType).ops(w)
	secret := pgCreate(t, branches, w.Kavya, llHiddenCode, llHiddenName)
	mine := pgCreate(t, branches, own, "MINE-1", "Own Branch")

	// Back from Check to the reporting step, then Check: each a page with the posted id.
	pages := func(id int64) (step, checked string) {
		t.Helper()
		b := h.Browser(own)
		webtest.WantStatus(t, b.Post(t, details, url.Values{
			"code": {"EMP-901"}, "name": {"Row"}, "joining_date": {"01-04-2024"}, "status": {"Active"},
		}), http.StatusSeeOther)
		idText := strconv.FormatInt(id, 10)
		webtest.WantStatus(t, b.Post(t, reporting, url.Values{"branch_id": {idText}}), http.StatusSeeOther)
		c := b.Get(t, check)
		webtest.WantStatus(t, c, http.StatusOK)
		s := b.Get(t, reporting)
		webtest.WantStatus(t, s, http.StatusOK)
		return llPage(s, id), llPage(c, id)
	}
	hiddenStep, hiddenCheck := pages(secret.ID)
	unknownStep, unknownCheck := pages(lrUnknown)
	llWantHidden(t, "step", hiddenStep, unknownStep)
	llWantHidden(t, "check", hiddenCheck, unknownCheck)

	mineStep, mineCheck := pages(mine.ID)
	for name, page := range map[string]string{"step": mineStep, "check": mineCheck} {
		if !strings.Contains(webtest.Squash(page), "MINE-1") {
			t.Errorf("%s: a readable id lost its label: %s", name, webtest.Squash(page))
		}
	}
}
