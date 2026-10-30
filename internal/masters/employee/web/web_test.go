package employeeweb_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/masters/allmasters"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/employee"
	employeeweb "github.com/vardhan/internaltools/internal/masters/employee/web"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// Screens under test (MST Task 22, CD-50, CD-218): /m/employee list, record and
// More actions; the create wizard at /m/employee/new/{details,reporting,check};
// the per-section edit at /m/employee/{id}/edit/{details,reporting}. Field
// names are the DocType's Field names; user_id is an email box.
const (
	base       = "/m/employee"
	stepOne    = base + "/new/details"
	stepTwo    = base + "/new/reporting"
	stepCheck  = base + "/new/check"
	wizardMain = base + "/new"

	msgNoUser     = "Pick an existing, enabled user."                                                     // MST-112, CD-218
	msgSelf       = "An employee can't report to themselves."                                             // MST-113
	msgManagerOff = "Reporting manager EMP-008 has left. Pick an active manager."                         // MST-116
	msgReports    = "EMP-007 (Deepak Nair) still reports to EMP-004. Move them to another manager first." // MST-117
	msgBirth      = "Date of birth can't be in the future."                                               // MST-110
	msgNoBranch   = "Branch not found. Pick one from the list."                                           // CD-170
	msgNoManager  = "Reporting manager not found. Pick one from the list."                                // CD-170
	msgCodeForm   = "Code can use letters, digits and . _ / - only, must start with a letter or digit, and be at most 20 characters."
	msgRelieving  = "Relieving date can't be before date of joining."
	msgCodeReq    = "Code is required. Enter a short code such as SHREE."                                             // MST-001
	msgLinked     = "User priya@vardhan.test is already linked to employee EMP-004."                                  // MST-111
	msgForbidden  = "You don't have permission to create Employee. Ask your administrator for a role that allows it." // AUTH
	staleEmp007   = "Someone else changed Employee EMP-007 after you opened it. Reload it and make your changes again."
)

type world struct {
	h   *webtest.Harness
	svc *employee.Service
	m   msttest.Masters
}

// newWorld mounts the screens on the fixture masters and frees Deepak's user
// by unlinking it from EMP-007, so a test can link an enabled user.
func newWorld(t *testing.T) *world {
	t.Helper()
	h := webtest.NewSessions(t, func(k mstkit.Env, sm *scs.SessionManager) func(chi.Router) {
		return employeeweb.Routes(employee.New(k), k, sm)
	})
	w := &world{h: h, svc: employee.New(h.Env.Kit), m: msttest.SeedMasters(t, h.Env)}
	e := w.get(t, "EMP-007")
	in := input(e)
	in.UserID = 0
	if _, err := w.svc.Update(context.Background(), h.Env.Admin, e.ID, e.Version, in); err != nil {
		t.Fatalf("free deepak: %v", err)
	}
	return w
}

func input(e employee.Employee) employee.Input {
	return employee.Input{
		Code: e.Code, Name: e.Name, BranchID: e.BranchID, UserID: e.UserID, ManagerID: e.ManagerID,
		Joining: e.Joining, Birth: e.Birth, Relieving: e.Relieving, Status: e.Status,
	}
}

func (w *world) byCode(t *testing.T, code string) (employee.Employee, bool) {
	t.Helper()
	ctx := context.Background()
	var e employee.Employee
	var ok bool
	err := w.h.Env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		e, ok, err = w.svc.ByCode(ctx, tx, code)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return e, ok
}

func (w *world) get(t *testing.T, code string) employee.Employee {
	t.Helper()
	e, ok := w.byCode(t, code)
	if !ok {
		t.Fatalf("employee %s not found", code)
	}
	return e
}

func (w *world) wantNoRow(t *testing.T, code string) {
	t.Helper()
	if _, ok := w.byCode(t, code); ok {
		t.Errorf("employee %s exists; want nothing written", code)
	}
}

// count is one number from the database.
func (w *world) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	ctx := context.Background()
	var n int
	err := w.h.Env.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, query, args...).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (w *world) employees(t *testing.T) int {
	t.Helper()
	return w.count(t, `SELECT count(*) FROM employee`)
}

func (w *world) audits(t *testing.T, id int64) int {
	t.Helper()
	return w.count(t, `SELECT count(*) FROM audit_log WHERE doc_type = ? AND doc_id = ?`, employee.DocType, id)
}

func (w *world) allAudits(t *testing.T) int {
	t.Helper()
	return w.count(t, `SELECT count(*) FROM audit_log WHERE doc_type = ?`, employee.DocType)
}

// storeRelievingBeforeJoining writes what Validate refuses straight to the row.
func (w *world) storeRelievingBeforeJoining(t *testing.T, e employee.Employee) {
	t.Helper()
	w.exec(t, `UPDATE employee SET relieving_date = '2000-01-01' WHERE id = ?`, e.ID)
}

func (w *world) clearRelieving(t *testing.T, e employee.Employee) {
	t.Helper()
	w.exec(t, `UPDATE employee SET relieving_date = NULL WHERE id = ?`, e.ID)
}

func (w *world) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	ctx := context.Background()
	err := w.h.Env.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, query, args...)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func (w *world) branch() string { return strconv.FormatInt(w.m.Branches["PUNE"], 10) }

func (w *world) recordURL(e employee.Employee) string {
	return base + "/" + strconv.FormatInt(e.ID, 10)
}

func (w *world) editURL(e employee.Employee, section string) string {
	return w.recordURL(e) + "/edit/" + section
}

func details(code string) url.Values {
	return url.Values{"code": {code}, "name": {"Nisha Menon"}, "joining_date": {"01-04-2024"}, "status": {"Active"}}
}

func (w *world) reporting() url.Values {
	return url.Values{"branch_id": {w.branch()}, "manager_id": {""}, "user_id": {""}}
}

// toCheck walks a browser through both steps to the check page.
func (w *world) toCheck(t *testing.T, b *webtest.Browser, d, r url.Values) {
	t.Helper()
	webtest.WantRedirect(t, b.Post(t, stepOne, d), stepTwo)
	webtest.WantRedirect(t, b.Post(t, stepTwo, r), stepCheck)
}

func value(t *testing.T, r webtest.Response, name, want string) {
	t.Helper()
	got, ok := r.Value(name)
	if !ok {
		t.Fatalf("no control named %s\ntext: %s", name, r.Text())
	}
	if got != want {
		t.Errorf("%s = %q; want %q", name, got, want)
	}
}

// CD-50: values stay in the session and nothing is written until Check and
// save, which makes one row and one audit entry.
func TestScreens_EmployeeWizardKeepsDraft(t *testing.T) {
	w := newWorld(t)
	rows, audits := w.employees(t), w.allAudits(t)
	b := w.h.Browser(w.h.Env.Kavya)

	r := w.reporting()
	r.Set("manager_id", strconv.FormatInt(w.m.Employees["EMP-004"], 10))
	r.Set("user_id", "deepak@vardhan.test")
	d := details("EMP-101")
	d.Set("birth_date", "15-08-1990")
	webtest.WantRedirect(t, b.Post(t, stepOne, d), stepTwo)
	webtest.WantRedirect(t, b.Post(t, stepTwo, r), stepCheck)

	// Back to step 1 shows what was typed, by the Back button and by the stepper.
	back := b.Post(t, stepCheck, url.Values{"nav": {"back"}})
	webtest.WantRedirect(t, back, stepTwo)
	second := b.Get(t, stepTwo)
	value(t, second, "user_id", "deepak@vardhan.test")
	webtest.WantRedirect(t, b.Post(t, stepTwo, url.Values{"nav": {"back"}, "branch_id": {w.branch()}, "manager_id": r["manager_id"], "user_id": {"kept.on.back@vardhan.test"}}), stepOne)
	value(t, b.Get(t, stepTwo), "user_id", "kept.on.back@vardhan.test") // Back saves without validating
	webtest.WantStatus(t, b.Post(t, stepTwo, url.Values{"nav": {"back"}, "branch_id": {w.branch()}, "manager_id": r["manager_id"], "user_id": {"deepak@vardhan.test"}}), http.StatusSeeOther)
	first := b.Get(t, stepOne)
	value(t, first, "code", "EMP-101")
	value(t, first, "name", "Nisha Menon")
	value(t, first, "joining_date", "01-04-2024")
	value(t, first, "birth_date", "15-08-1990")

	if w.employees(t) != rows || w.allAudits(t) != audits {
		t.Fatalf("rows or audit entries changed before Check and save")
	}

	check := b.Get(t, stepCheck)
	webtest.WantStatus(t, check, http.StatusOK)
	webtest.WantText(t, check, "Check and save")
	webtest.WantText(t, check, "EMP-101")
	webtest.WantText(t, check, "01-04-2024")
	webtest.WantText(t, check, "PUNE")
	webtest.WantText(t, check, "EMP-004 Priya Shah")
	webtest.WantText(t, check, "deepak@vardhan.test")
	if _, found := check.RowLink(stepOne + "?return=check"); !found {
		t.Errorf("check page has no Change link to the first step\ntext: %s", check.Text())
	}
	if w.employees(t) != rows {
		t.Fatalf("the check page wrote a row")
	}

	saved := b.Post(t, stepCheck, url.Values{})
	e := w.get(t, "EMP-101")
	webtest.WantRedirect(t, saved, w.recordURL(e))
	webtest.WantBanner(t, b.Get(t, saved.Location), "Employee saved.")
	if w.employees(t) != rows+1 || w.allAudits(t) != audits+1 {
		t.Errorf("rows %d, audit entries %d; want %d and %d", w.employees(t), w.allAudits(t), rows+1, audits+1)
	}
	if e.ManagerID != w.m.Employees["EMP-004"] || e.UserID != 7 || e.Joining.ISO() != "2024-04-01" || e.Birth.ISO() != "1990-08-15" || e.Status != employee.Active {
		t.Errorf("saved %+v", e)
	}
}

// The wizard's forms post through htmx: a step answers with the next step's
// fragment and HX-Push-Url, and the save leaves through HX-Redirect.
func TestScreens_EmployeeWizardHTMX(t *testing.T) {
	w := newWorld(t)
	b := w.h.Browser(w.h.Env.Kavya)
	next := b.PostHTMX(t, stepOne, details("EMP-102"))
	webtest.WantStatus(t, next, http.StatusOK)
	if got := next.Header.Get("HX-Push-Url"); got != stepTwo {
		t.Errorf("HX-Push-Url = %q; want %q", got, stepTwo)
	}
	if !next.HasControl("branch_id") || strings.Contains(next.Body, "<html") {
		t.Errorf("want the reporting step's fragment\nbody: %s", webtest.Squash(next.Body))
	}
	webtest.WantStatus(t, b.PostHTMX(t, stepTwo, w.reporting()), http.StatusOK)
	saved := b.PostHTMX(t, stepCheck, url.Values{})
	webtest.WantStatus(t, saved, http.StatusOK)
	if got, want := saved.Header.Get("HX-Redirect"), w.recordURL(w.get(t, "EMP-102")); got != want {
		t.Errorf("HX-Redirect = %q; want %q", got, want)
	}
}

// §8.3: Change on the check page returns straight to it.
func TestScreens_EmployeeWizardChangeReturnsToCheck(t *testing.T) {
	w := newWorld(t)
	b := w.h.Browser(w.h.Env.Kavya)
	w.toCheck(t, b, details("EMP-103"), w.reporting())
	page := b.Get(t, stepOne+"?return=check")
	if got, _ := page.FormValue(stepOne, "return"); got != "check" {
		t.Fatalf("return = %q; want check", got)
	}
	d := details("EMP-104")
	d.Set("return", "check")
	webtest.WantRedirect(t, b.Post(t, stepOne, d), stepCheck)
	webtest.WantText(t, b.Get(t, stepCheck), "EMP-104")
}

// A step past the furthest reached isn't open yet, GET or POST.
func TestScreens_EmployeeWizardNoSkipping(t *testing.T) {
	w := newWorld(t)
	rows := w.employees(t)
	b := w.h.Browser(w.h.Env.Kavya)
	webtest.WantRedirect(t, b.Get(t, stepCheck), stepOne)
	webtest.WantRedirect(t, b.Post(t, stepTwo, w.reporting()), stepOne)
	webtest.WantRedirect(t, b.Post(t, stepCheck, url.Values{}), stepOne)
	if w.employees(t) != rows {
		t.Errorf("a skipped wizard wrote a row")
	}
	webtest.WantStatus(t, b.Get(t, base+"/new/nosuch"), http.StatusNotFound)
	webtest.WantStatus(t, b.Post(t, base+"/new/nosuch", url.Values{}), http.StatusNotFound)
}

// /new opens the first step; the stepper links only reached steps.
func TestScreens_EmployeeWizardFirstPage(t *testing.T) {
	w := newWorld(t)
	b := w.h.Browser(w.h.Env.Kavya)
	page := b.Get(t, wizardMain)
	webtest.WantStatus(t, page, http.StatusOK)
	webtest.WantText(t, page, "New employee")
	webtest.WantText(t, page, "Step 1 of 3")
	if _, found := page.RowLink(stepTwo); found {
		t.Errorf("the stepper links a step not reached yet")
	}
	if got := page.Choices("status"); len(got) != 2 {
		t.Errorf("status options = %+v", got)
	}
	checked, _ := page.Doc.Find(`input[type="radio"][name="status"][checked]`).Attr("value")
	if checked != "Active" {
		t.Errorf("preselected status = %q; want Active", checked)
	}
	if page.Doc.Find(`input[name="disabled"]`).Length() != 0 {
		t.Errorf("the form offers disabled")
	}
	if page.HasControl("branch_id") {
		t.Errorf("step 1 shows a reporting field")
	}
}

// A step reports only its own parse errors on Next, and composite and required
// rules wait for Check and save.
func TestScreens_EmployeeWizardStepErrors(t *testing.T) {
	w := newWorld(t)
	b := w.h.Browser(w.h.Env.Kavya)

	d := details("EMP-105")
	d.Set("joining_date", "not a date")
	bad := b.Post(t, stepOne, d)
	webtest.WantStatus(t, bad, http.StatusUnprocessableEntity)
	if msg, _, _ := bad.FieldMessage("joining_date"); msg == "" {
		t.Errorf("no parse error on joining_date")
	}
	value(t, bad, "joining_date", "not a date")

	// htmx gets the step alone, with the 422.
	frag := b.PostHTMX(t, stepOne, d)
	webtest.WantStatus(t, frag, http.StatusUnprocessableEntity)
	if strings.Contains(frag.Body, "<html") || frag.Doc.Find("#wizard").Length() != 1 {
		t.Errorf("want the #wizard fragment\nbody: %s", webtest.Squash(frag.Body))
	}

	// The stepper away and back: what was typed stays.
	value(t, b.Get(t, stepOne), "joining_date", "not a date")

	// Step 1 passes without a name: required waits for Check and save. Step 2's
	// Next then doesn't hear step 1's date, typed bad again after it passed.
	webtest.WantRedirect(t, b.Post(t, stepOne, url.Values{"code": {"EMP-105"}, "joining_date": {"01-04-2024"}, "status": {"Active"}}), stepTwo)
	d.Set("name", "")
	webtest.WantStatus(t, b.Post(t, stepOne, d), http.StatusUnprocessableEntity)
	webtest.WantRedirect(t, b.Post(t, stepTwo, w.reporting()), stepCheck)

	// Validate's rules on this step are heard on Next, as they are at save.
	d = details("EMP-105")
	d.Set("relieving_date", "01-01-2020")
	early := b.Post(t, stepOne, d)
	webtest.WantStatus(t, early, http.StatusUnprocessableEntity)
	webtest.WantFieldErrorID(t, early, components.FieldID("relieving_date"), "Relieving date can't be before date of joining.")
	d = details("EMP-105")
	d.Set("status", "Left")
	webtest.WantFieldErrorID(t, b.Post(t, stepOne, d), components.FieldID("relieving_date"), "Enter the relieving date.")

	// A parse error stands for its own field, not Validate's empty one.
	d = details("EMP-105")
	d.Set("joining_date", "soon")
	webtest.WantNoText(t, b.Post(t, stepOne, d), "Date of joining is required.")
}

// Back on the first step stays there; Back isn't offered on it, and is on the
// next. A return asked for before Check and save is reached goes on in order.
func TestScreens_EmployeeWizardBackAndReturn(t *testing.T) {
	w := newWorld(t)
	b := w.h.Browser(w.h.Env.Kavya)
	backs := func(r webtest.Response) int { return r.Doc.Find(`button[value="back"]`).Length() }
	if n := backs(b.Get(t, stepOne)); n != 0 {
		t.Errorf("first step shows %d Back", n)
	}
	d := details("EMP-119")
	d.Set("nav", "back")
	webtest.WantRedirect(t, b.Post(t, stepOne, d), stepOne)
	d = details("EMP-119")
	d.Set("return", "check")
	webtest.WantRedirect(t, b.Post(t, stepOne, d), stepTwo)
	if n := backs(b.Get(t, stepTwo)); n != 1 {
		t.Errorf("second step shows %d Back", n)
	}
}

// Check and save: its button says Save, dates read dd-mm-yyyy however they were
// typed, one that doesn't parse shows as typed, and blanks leave no row.
func TestScreens_EmployeeWizardCheckPage(t *testing.T) {
	w := newWorld(t)
	b := w.h.Browser(w.h.Env.Kavya)
	d := details("EMP-120")
	d.Set("joining_date", "2024-04-01")
	w.toCheck(t, b, d, w.reporting())
	check := b.Get(t, stepCheck)
	webtest.WantText(t, check, "01-04-2024")
	webtest.WantNoText(t, check, "2024-04-01")
	webtest.WantNoText(t, check, "Date of birth")
	if got := webtest.Squash(check.Doc.Find(`button.btn-primary`).Text()); got != "Save" {
		t.Errorf("primary button = %q; want Save", got)
	}
	d.Set("birth_date", "soon")
	webtest.WantStatus(t, b.Post(t, stepOne, d), http.StatusUnprocessableEntity)
	webtest.WantText(t, b.Get(t, stepCheck), "soon")
}

// Too big for the session: refused on the step, nothing of it held, by Next and
// by Back, with the Field's MaxLen where it has one.
func TestScreens_EmployeeWizardSizeLimit(t *testing.T) {
	w := newWorld(t)
	b := w.h.Browser(w.h.Env.Kavya)
	webtest.WantRedirect(t, b.Post(t, stepOne, details("EMP-121")), stepTwo)
	huge := strings.Repeat("x", 5<<20)
	for _, nav := range []string{"", "back"} {
		d := details("EMP-121")
		d.Set("name", huge)
		d.Set("nav", nav)
		r := b.Post(t, stepOne, d)
		webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
		webtest.WantFieldErrorID(t, r, components.FieldID("name"), "Name can be at most 1,000 characters.")
		value(t, b.Get(t, stepOne), "name", "Nisha Menon")
	}
	d := details("EMP-121")
	d.Set("code", strings.Repeat("C", 21))
	r := b.Post(t, stepOne, d)
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantFieldErrorID(t, r, components.FieldID("code"), msgCodeForm) // the service's text, as on a section
}

// Start over drops the draft and writes nothing, and only shows with a draft.
func TestScreens_EmployeeWizardStartOver(t *testing.T) {
	w := newWorld(t)
	const startOver = wizardMain + "/start-over"
	rows, audits := w.employees(t), w.allAudits(t)
	b := w.h.Browser(w.h.Env.Kavya)
	offered := func(r webtest.Response) bool {
		return r.Doc.Find(`form[action="`+startOver+`"] button`).Length() == 1
	}
	if offered(b.Get(t, wizardMain)) {
		t.Errorf("Start over offered with no draft")
	}
	w.toCheck(t, b, details("EMP-122"), w.reporting())
	if !offered(b.Get(t, stepOne)) || !offered(b.Get(t, stepCheck)) {
		t.Fatalf("Start over not offered with a draft")
	}
	if got := webtest.Squash(b.Get(t, stepCheck).Doc.Find(`form[action="` + startOver + `"] button`).Text()); got != "Start over" {
		t.Errorf("button = %q", got)
	}
	// It comes after the action bar and outside the step form, so Enter can't
	// press it, on a step and on Check and save.
	for _, page := range []webtest.Response{b.Get(t, stepTwo), b.Get(t, stepCheck)} {
		if page.Doc.Find(`#wizard-form form, #wizard-form [formaction]`).Length() != 0 || strings.Contains(page.Doc.Find("#wizard-form").Text(), "Start over") {
			t.Errorf("Start over is inside the step form")
		}
		if bar, over := strings.Index(page.Body, `value="back"`), strings.Index(page.Body, startOver); bar < 0 || over < bar {
			t.Errorf("Start over at %d comes before the action bar at %d", over, bar)
		}
	}

	// Another user's press leaves this draft alone, and a reader is refused.
	webtest.WantRedirect(t, b.As(w.h.Env.Admin).Post(t, startOver, url.Values{}), stepOne)
	value(t, b.Get(t, stepOne), "code", "EMP-122")
	webtest.WantStatus(t, b.As(w.h.Env.Priya).Post(t, startOver, url.Values{}), http.StatusForbidden)
	value(t, b.Get(t, stepOne), "code", "EMP-122")

	webtest.WantRedirect(t, b.Post(t, startOver, url.Values{}), stepOne)
	value(t, b.Get(t, stepOne), "code", "")
	webtest.WantRedirect(t, b.Get(t, stepCheck), stepOne)
	if w.employees(t) != rows || w.allAudits(t) != audits {
		t.Errorf("Start over wrote something")
	}
}

// Two tabs press Save at once: one row, and the session can't make a second.
func TestScreens_EmployeeWizardDoubleSubmit(t *testing.T) {
	w := newWorld(t)
	b := w.h.Browser(w.h.Env.Kavya)
	w.toCheck(t, b, details("EMP-123"), w.reporting())
	rows, audits := w.employees(t), w.allAudits(t)
	const tabs = 4
	statuses := make([]int, tabs)
	var wg sync.WaitGroup
	for i := range tabs {
		tab := b.Clone()
		wg.Go(func() { statuses[i] = tab.Post(t, stepCheck, url.Values{}).Status })
	}
	wg.Wait()
	saved := 0
	for _, s := range statuses {
		switch s {
		case http.StatusSeeOther:
			saved++
		case http.StatusUnprocessableEntity:
		default:
			t.Errorf("status %d; want 303 or 422", s)
		}
	}
	if saved < 1 {
		t.Errorf("no tab saved: %v", statuses)
	}
	if w.employees(t) != rows+1 || w.allAudits(t) != audits+1 {
		t.Errorf("rows %d, audit %d; want %d, %d: statuses %v", w.employees(t), w.allAudits(t), rows+1, audits+1, statuses)
	}
}

// The links of the reporting step, as project and site: not numeric is the
// service's not-found text, by FieldID; blank is none.
func TestScreens_EmployeeWizardLinks(t *testing.T) {
	w := newWorld(t)
	b := w.h.Browser(w.h.Env.Kavya)
	webtest.WantRedirect(t, b.Post(t, stepOne, details("EMP-106")), stepTwo)
	bad := b.Post(t, stepTwo, url.Values{"branch_id": {"abc"}, "manager_id": {"12x"}})
	webtest.WantStatus(t, bad, http.StatusUnprocessableEntity)
	webtest.WantFieldErrorID(t, bad, components.FieldID("branch_id"), msgNoBranch)
	webtest.WantFieldErrorID(t, bad, components.FieldID("manager_id"), msgNoManager)

	// A value the check page can't look up still shows, and refuses on its step.
	webtest.WantStatus(t, b.Post(t, stepTwo, url.Values{"branch_id": {"999999"}}), http.StatusSeeOther)
	check := b.Get(t, stepCheck)
	webtest.WantStatus(t, check, http.StatusOK)
	webtest.WantText(t, check, "999999")
	refused := b.Post(t, stepCheck, url.Values{})
	webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
	webtest.WantFieldErrorID(t, refused, components.FieldID("branch_id"), msgNoBranch)

	// So does one that isn't a number, kept in the session by its refused Next.
	webtest.WantStatus(t, b.Post(t, stepTwo, url.Values{"branch_id": {"abc"}}), http.StatusUnprocessableEntity)
	webtest.WantText(t, b.Get(t, stepCheck), "abc")
}

// CD-218: an unknown user and a disabled user get MST-112's text, on Next,
// before anything else can tell them apart.
func TestScreens_EmployeeWizardUserEmail(t *testing.T) {
	for _, email := range []string{"nobody@vardhan.test", "old@vardhan.test"} {
		t.Run(email, func(t *testing.T) {
			w := newWorld(t)
			b := w.h.Browser(w.h.Env.Kavya)
			webtest.WantRedirect(t, b.Post(t, stepOne, details("EMP-107")), stepTwo)
			r := w.reporting()
			r.Set("user_id", email)
			bad := b.Post(t, stepTwo, r)
			webtest.WantStatus(t, bad, http.StatusUnprocessableEntity)
			webtest.WantFieldError(t, bad, "user_id", msgNoUser)
			value(t, bad, "user_id", email)
			w.wantNoRow(t, "EMP-107")
		})
	}
}

// The user box is an email box, not a number box.
func TestScreens_UserIsAnEmailBox(t *testing.T) {
	w := newWorld(t)
	b := w.h.Browser(w.h.Env.Kavya)
	webtest.WantRedirect(t, b.Post(t, stepOne, details("EMP-108")), stepTwo)
	for name, page := range map[string]webtest.Response{
		"wizard":  b.Get(t, stepTwo),
		"section": b.Get(t, w.editURL(w.get(t, "EMP-004"), "reporting")),
	} {
		if typ, _ := page.Doc.Find(`input[name="user_id"]`).Attr("type"); typ != "email" {
			t.Errorf("%s: user_id type = %q; want email", name, typ)
		}
	}
	// Blank is no user, spaces too; an email with capitals and spaces finds its
	// user.
	r := w.reporting()
	r.Set("user_id", "   ")
	webtest.WantRedirect(t, b.Post(t, stepTwo, r), stepCheck)
	r.Set("user_id", " Deepak@Vardhan.test ")
	webtest.WantRedirect(t, b.Post(t, stepTwo, r), stepCheck)
	webtest.WantRedirect(t, b.Post(t, stepCheck, url.Values{}), w.recordURL(w.get(t, "EMP-108")))
	if e := w.get(t, "EMP-108"); e.UserID != 7 {
		t.Errorf("user = %d; want 7", e.UserID)
	}
}

// A refusal at Check and save returns 422 on the step that owns the field,
// writes nothing and keeps the session (MST-116 on reporting, MST-110 on
// details; MST-113 can't occur on create, CD-218).
func TestScreens_EmployeeWizardRefusalOnItsStep(t *testing.T) {
	w := newWorld(t)
	audits := w.allAudits(t)

	t.Run("MST-116 on reporting", func(t *testing.T) {
		b := w.h.Browser(w.h.Env.Kavya)
		r := w.reporting()
		r.Set("manager_id", strconv.FormatInt(w.m.Employees["EMP-008"], 10))
		w.toCheck(t, b, details("EMP-109"), r)
		refused := b.Post(t, stepCheck, url.Values{})
		webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
		webtest.WantFieldErrorID(t, refused, components.FieldID("manager_id"), msgManagerOff)
		webtest.WantText(t, refused, "Step 2 of 3")
		w.wantNoRow(t, "EMP-109")
		if got, _ := refused.FormValue(stepTwo, "return"); got != "check" {
			t.Errorf("return = %q; want check", got)
		}
		if got := refused.Header.Get("HX-Push-Url"); got != stepTwo {
			t.Errorf("HX-Push-Url = %q; want %q", got, stepTwo)
		}
		// The session keeps the values, and the fix goes straight back to check.
		value(t, b.Get(t, stepOne), "code", "EMP-109")
		r.Set("manager_id", "")
		r.Set("return", "check")
		webtest.WantRedirect(t, b.Post(t, stepTwo, r), stepCheck)
		webtest.WantRedirect(t, b.Post(t, stepCheck, url.Values{}), w.recordURL(w.get(t, "EMP-109")))
	})

	audits++ // EMP-109, saved above

	t.Run("MST-110 on details", func(t *testing.T) {
		b := w.h.Browser(w.h.Env.Kavya)
		d := details("EMP-110")
		d.Set("birth_date", "01-11-2026")
		w.toCheck(t, b, d, w.reporting())
		refused := b.Post(t, stepCheck, url.Values{})
		webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
		webtest.WantFieldErrorID(t, refused, components.FieldID("birth_date"), msgBirth)
		webtest.WantText(t, refused, "Step 1 of 3")
		w.wantNoRow(t, "EMP-110")
		value(t, b.Get(t, stepOne), "birth_date", "01-11-2026")
	})

	t.Run("errors on two steps show on the first, alone", func(t *testing.T) {
		b := w.h.Browser(w.h.Env.Kavya)
		d := details("EMP-118")
		d.Set("birth_date", "01-11-2026")
		r := w.reporting()
		r.Set("manager_id", strconv.FormatInt(w.m.Employees["EMP-008"], 10))
		w.toCheck(t, b, d, r)
		refused := b.Post(t, stepCheck, url.Values{})
		webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
		webtest.WantText(t, refused, "Step 1 of 3")
		webtest.WantText(t, refused, msgBirth)
		webtest.WantNoText(t, refused, msgManagerOff)
	})

	t.Run("a required field", func(t *testing.T) {
		b := w.h.Browser(w.h.Env.Kavya)
		d := details("")
		w.toCheck(t, b, d, w.reporting())
		refused := b.Post(t, stepCheck, url.Values{})
		webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
		webtest.WantFieldErrorID(t, refused, components.FieldID("code"), msgCodeReq)
	})

	t.Run("MST-111 on reporting", func(t *testing.T) {
		b := w.h.Browser(w.h.Env.Kavya)
		r := w.reporting()
		r.Set("user_id", "priya@vardhan.test")
		w.toCheck(t, b, details("EMP-111"), r)
		refused := b.Post(t, stepCheck, url.Values{})
		webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
		webtest.WantFieldError(t, refused, "user_id", msgLinked)
	})

	if got := w.allAudits(t); got != audits {
		t.Errorf("audit entries = %d; want %d: a refusal wrote one", got, audits)
	}
}

// A parse error left in the session sends the user back to its step.
func TestScreens_EmployeeWizardParseAtCheck(t *testing.T) {
	w := newWorld(t)
	b := w.h.Browser(w.h.Env.Kavya)
	w.toCheck(t, b, details("EMP-112"), w.reporting())
	// An optional date: a failed parse must never save as no date (CD-37).
	d := details("EMP-112")
	d.Set("birth_date", "soon")
	webtest.WantStatus(t, b.Post(t, stepOne, d), http.StatusUnprocessableEntity)
	refused := b.Post(t, stepCheck, url.Values{})
	webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
	webtest.WantText(t, refused, "Step 1 of 3")
	value(t, refused, "birth_date", "soon")
	w.wantNoRow(t, "EMP-112")
}

// The session key names the user: two users, even on one browser, don't see
// each other's wizard.
func TestScreens_EmployeeWizardUsersDontMix(t *testing.T) {
	w := newWorld(t)
	kavya := w.h.Browser(w.h.Env.Kavya)
	webtest.WantRedirect(t, kavya.Post(t, stepOne, details("EMP-113")), stepTwo)

	other := w.h.Browser(w.h.Env.Admin)
	value(t, other.Get(t, stepOne), "code", "")

	shared := kavya.As(w.h.Env.Admin)
	value(t, shared.Get(t, stepOne), "code", "")
	webtest.WantRedirect(t, shared.Post(t, stepOne, details("EMP-114")), stepTwo)
	value(t, kavya.Get(t, stepOne), "code", "EMP-113")
}

// A finished wizard can't be replayed, and the next one starts empty.
func TestScreens_EmployeeWizardNoReplay(t *testing.T) {
	w := newWorld(t)
	b := w.h.Browser(w.h.Env.Kavya)
	w.toCheck(t, b, details("EMP-115"), w.reporting())
	webtest.WantStatus(t, b.Post(t, stepCheck, url.Values{}), http.StatusSeeOther)
	rows := w.employees(t)

	webtest.WantRedirect(t, b.Post(t, stepCheck, url.Values{}), stepOne)
	if got := w.employees(t); got != rows {
		t.Errorf("employees = %d after a replay; want %d", got, rows)
	}
	value(t, b.Get(t, stepOne), "code", "")
	webtest.WantRedirect(t, b.Get(t, stepTwo), stepOne)
}

// Every step, GET and POST, refuses a user without create with AUTH's 403.
func TestScreens_EmployeeWizardRefusesWithoutCreate(t *testing.T) {
	w := newWorld(t)
	priya := w.h.Browser(w.h.Env.Priya)
	for _, target := range []string{wizardMain, stepOne, stepTwo, stepCheck} {
		r := priya.Get(t, target)
		webtest.WantStatus(t, r, http.StatusForbidden)
		webtest.WantText(t, r, msgForbidden)
	}
	for _, target := range []string{stepOne, stepTwo, stepCheck} {
		r := priya.Post(t, target, details("EMP-116"))
		webtest.WantStatus(t, r, http.StatusForbidden)
		webtest.WantText(t, r, msgForbidden)
	}
	if _, found := priya.Get(t, base).RowLink(wizardMain); found {
		t.Errorf("the list offers New to a user without create")
	}
	if _, found := w.h.Browser(w.h.Env.Kavya).Get(t, base).RowLink(wizardMain); !found {
		t.Errorf("the list offers no New to Kavya")
	}
}

// CD-50: a section saves only its fields, with the posted version; the rest
// goes through Update as stored, and the version moves by one.
func TestScreens_EditSavesPerSection(t *testing.T) {
	w := newWorld(t)
	b := w.h.Browser(w.h.Env.Kavya)
	before := w.get(t, "EMP-004")
	audits := w.audits(t, before.ID)

	page := b.Get(t, w.recordURL(before))
	webtest.WantStatus(t, page, http.StatusOK)
	webtest.WantText(t, page, "EMP-004 Priya Shah")
	webtest.WantText(t, page, "Details")
	webtest.WantText(t, page, "Reporting")
	webtest.WantText(t, page, "EMP-003 Ravi Kulkarni")
	webtest.WantText(t, page, "priya@vardhan.test")
	for _, s := range []string{"details", "reporting"} {
		if label, found := page.RowLink(w.editURL(before, s)); !found || !strings.HasPrefix(label, "Change") {
			t.Errorf("Change link to %s: %q, %v", s, label, found)
		}
	}
	if page.HasControl("name") {
		t.Errorf("the record page shows the form; want the summary")
	}

	section := b.Get(t, w.editURL(before, "details"))
	webtest.WantStatus(t, section, http.StatusOK)
	value(t, section, "joining_date", types.FormatDate(before.Joining))
	if section.HasControl("branch_id") {
		t.Errorf("the details section shows a reporting field")
	}
	version, _ := section.FormValue(w.editURL(before, "details"), "version")

	done := b.Post(t, w.editURL(before, "details"), url.Values{
		"version": {version}, "code": {"EMP-004"}, "name": {"Priya S"}, "joining_date": {types.FormatDate(before.Joining)},
		"status": {"Active"},
	})
	webtest.WantRedirect(t, done, w.recordURL(before))
	webtest.WantBanner(t, b.Get(t, done.Location), "Employee saved.")

	after := w.get(t, "EMP-004")
	if after.Name != "Priya S" || after.Version != before.Version+1 {
		t.Errorf("name %q version %d; want Priya S, %d", after.Name, after.Version, before.Version+1)
	}
	if after.BranchID != before.BranchID || after.ManagerID != before.ManagerID || after.UserID != before.UserID {
		t.Errorf("the reporting fields moved: %+v; were %+v", after, before)
	}
	if got := w.audits(t, before.ID); got != audits+1 {
		t.Errorf("audit entries = %d; want %d", got, audits+1)
	}

	// The reporting section keeps the details as stored, dates included.
	reporting := b.Get(t, w.editURL(after, "reporting"))
	value(t, reporting, "user_id", "priya@vardhan.test")
	webtest.WantRedirect(t, b.Post(t, w.editURL(after, "reporting"), url.Values{
		"version": {strconv.Itoa(after.Version)}, "branch_id": {strconv.FormatInt(w.m.Branches["BLR"], 10)},
		"manager_id": {strconv.FormatInt(after.ManagerID, 10)}, "user_id": {"priya@vardhan.test"},
	}), w.recordURL(after))
	moved := w.get(t, "EMP-004")
	if moved.BranchID != w.m.Branches["BLR"] || moved.Name != "Priya S" || moved.Joining != after.Joining || moved.Version != after.Version+1 {
		t.Errorf("after reporting save: %+v", moved)
	}
}

// wantVersion checks the form still carries the version the page posted, never
// a fresh one: a refused save must stay stale until reloaded (CD-94).
func wantVersion(t *testing.T, r webtest.Response, action string, want int) {
	t.Helper()
	if got, _ := r.FormValue(action, "version"); got != strconv.Itoa(want) {
		t.Errorf("hidden version = %q; want %d", got, want)
	}
}

// A stale section save is DOC's 409 with the typed values kept and a Reload.
func TestScreens_EditSectionStale(t *testing.T) {
	w := newWorld(t)
	b := w.h.Browser(w.h.Env.Kavya)
	e := w.get(t, "EMP-007")
	stale := b.Post(t, w.editURL(e, "details"), url.Values{
		"version": {strconv.Itoa(e.Version - 1)}, "code": {"EMP-007"}, "name": {"Typed Name"},
		"joining_date": {types.FormatDate(e.Joining)}, "status": {"Active"},
	})
	webtest.WantStatus(t, stale, http.StatusConflict)
	webtest.WantBanner(t, stale, staleEmp007)
	value(t, stale, "name", "Typed Name")
	wantVersion(t, stale, w.editURL(e, "details"), e.Version-1)
	if _, found := stale.RowLink(w.editURL(e, "details")); !found {
		t.Errorf("the 409 has no Reload to the section\ntext: %s", stale.Text())
	}
	if got := w.get(t, "EMP-007"); got.Name != e.Name || got.Version != e.Version {
		t.Errorf("a stale save wrote: %+v", got)
	}
	webtest.WantStatus(t, b.Post(t, w.editURL(e, "details"), url.Values{"name": {"x"}}), http.StatusBadRequest)
}

// MST-113 on the section that owns manager_id.
func TestScreens_EditSelfManager(t *testing.T) {
	w := newWorld(t)
	b := w.h.Browser(w.h.Env.Kavya)
	e := w.get(t, "EMP-007")
	refused := b.Post(t, w.editURL(e, "reporting"), url.Values{
		"version": {strconv.Itoa(e.Version)}, "branch_id": {strconv.FormatInt(e.BranchID, 10)},
		"manager_id": {strconv.FormatInt(e.ID, 10)},
	})
	webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
	webtest.WantFieldErrorID(t, refused, components.FieldID("manager_id"), msgSelf)
	wantVersion(t, refused, w.editURL(e, "reporting"), e.Version)
	if got := w.get(t, "EMP-007"); got.ManagerID != e.ManagerID || got.Version != e.Version {
		t.Errorf("a refused section wrote: %+v", got)
	}
}

// CD-218 on the reporting section: the same text for unknown and disabled.
func TestScreens_EditUserEmail(t *testing.T) {
	for _, email := range []string{"nobody@vardhan.test", "old@vardhan.test"} {
		t.Run(email, func(t *testing.T) {
			w := newWorld(t)
			b := w.h.Browser(w.h.Env.Kavya)
			e := w.get(t, "EMP-007")
			refused := b.Post(t, w.editURL(e, "reporting"), url.Values{
				"version": {strconv.Itoa(e.Version)}, "branch_id": {strconv.FormatInt(e.BranchID, 10)},
				"manager_id": {strconv.FormatInt(e.ManagerID, 10)}, "user_id": {email},
			})
			webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
			webtest.WantFieldError(t, refused, "user_id", msgNoUser)
			value(t, refused, "user_id", email)
			if got := w.get(t, "EMP-007"); got.UserID != 0 || got.Version != e.Version {
				t.Errorf("a refused link wrote: %+v", got)
			}
		})
	}
}

// A leaver's reporting section saves with its disabled user unchanged, and the
// link itself can be set and cleared.
func TestScreens_EditKeepsDisabledUser(t *testing.T) {
	w := newWorld(t)
	b := w.h.Browser(w.h.Env.Kavya)
	e := w.get(t, "EMP-008")
	page := b.Get(t, w.editURL(e, "reporting"))
	value(t, page, "user_id", "old@vardhan.test")
	webtest.WantRedirect(t, b.Post(t, w.editURL(e, "reporting"), url.Values{
		"version": {strconv.Itoa(e.Version)}, "branch_id": {strconv.FormatInt(w.m.Branches["BLR"], 10)}, "user_id": {"Old@Vardhan.test"},
	}), w.recordURL(e))
	if got := w.get(t, "EMP-008"); got.BranchID != w.m.Branches["BLR"] || got.UserID != e.UserID {
		t.Errorf("leaver after save: %+v", got)
	}

	free := w.get(t, "EMP-007")
	webtest.WantRedirect(t, b.Post(t, w.editURL(free, "reporting"), url.Values{
		"version": {strconv.Itoa(free.Version)}, "branch_id": {strconv.FormatInt(free.BranchID, 10)},
		"manager_id": {strconv.FormatInt(free.ManagerID, 10)}, "user_id": {"deepak@vardhan.test"},
	}), w.recordURL(free))
	if got := w.get(t, "EMP-007"); got.UserID != 7 {
		t.Errorf("user = %d; want 7", got.UserID)
	}
}

// A read-only user sees the summary with no Change; the section is refused.
func TestScreens_EditReadOnly(t *testing.T) {
	w := newWorld(t)
	priya := w.h.Browser(w.h.Env.Priya)
	e := w.get(t, "EMP-007")
	page := priya.Get(t, w.recordURL(e))
	webtest.WantStatus(t, page, http.StatusOK)
	webtest.WantText(t, page, "EMP-007 Deepak Nair")
	webtest.WantNoText(t, page, "Date of birth") // a blank field has no row
	webtest.WantText(t, page, "EMP-004 Priya Shah")
	webtest.WantNoText(t, page, "Change")
	const msg = "You don't have permission to change Employee. Ask your administrator for a role that allows it."
	for _, r := range []webtest.Response{
		priya.Get(t, w.editURL(e, "details")),
		priya.Post(t, w.editURL(e, "details"), url.Values{"version": {strconv.Itoa(e.Version)}, "name": {"x"}}),
	} {
		webtest.WantStatus(t, r, http.StatusForbidden)
		webtest.WantText(t, r, msg)
	}
	// The right comes before the form's own errors.
	bad := priya.Post(t, w.editURL(e, "reporting"), url.Values{"version": {strconv.Itoa(e.Version)}, "branch_id": {"abc"}, "user_id": {"nobody@vardhan.test"}})
	webtest.WantStatus(t, bad, http.StatusForbidden)
	if got := w.get(t, "EMP-007"); got.Name != e.Name {
		t.Errorf("a read-only post wrote %q", got.Name)
	}
	kavya := w.h.Browser(w.h.Env.Kavya)
	webtest.WantStatus(t, kavya.Get(t, w.recordURL(e)+"/edit/nosuch"), http.StatusNotFound)
	webtest.WantStatus(t, kavya.Post(t, w.recordURL(e)+"/edit/nosuch", url.Values{"version": {"1"}}), http.StatusNotFound)
	webtest.WantStatus(t, kavya.Get(t, base+"/999999/edit/details"), http.StatusNotFound)
	webtest.WantStatus(t, kavya.Post(t, base+"/999999/edit/details", url.Values{"version": {"1"}}), http.StatusNotFound)
}

// Sections replace the whole-record form: a post to the record isn't a save.
func TestScreens_EditNoWholeRecordSave(t *testing.T) {
	w := newWorld(t)
	e := w.get(t, "EMP-007")
	r := w.h.Browser(w.h.Env.Kavya).Post(t, w.recordURL(e), url.Values{"version": {strconv.Itoa(e.Version)}, "name": {"x"}})
	webtest.WantStatus(t, r, http.StatusMethodNotAllowed)
}

// An unparsable value in a section is its parse error, and the service isn't
// called; a blank link is none.
func TestScreens_EditParseErrors(t *testing.T) {
	w := newWorld(t)
	b := w.h.Browser(w.h.Env.Kavya)
	e := w.get(t, "EMP-007")
	bad := b.Post(t, w.editURL(e, "reporting"), url.Values{
		"version": {strconv.Itoa(e.Version)}, "branch_id": {"abc"}, "manager_id": {"12x"},
	})
	webtest.WantStatus(t, bad, http.StatusUnprocessableEntity)
	webtest.WantFieldErrorID(t, bad, components.FieldID("branch_id"), msgNoBranch)
	webtest.WantFieldErrorID(t, bad, components.FieldID("manager_id"), msgNoManager)

	// Refused at an old version, the form stays at it: 422 with the version
	// posted, not the record's.
	old := b.Post(t, w.editURL(e, "reporting"), url.Values{"version": {strconv.Itoa(e.Version - 1)}, "branch_id": {"abc"}})
	webtest.WantStatus(t, old, http.StatusUnprocessableEntity)
	wantVersion(t, old, w.editURL(e, "reporting"), e.Version-1)

	// Validate's errors on the section come with a parse error; the service
	// can't be asked while one stands.
	rules := b.Post(t, w.editURL(e, "details"), url.Values{
		"version": {strconv.Itoa(e.Version)}, "code": {"EMP-007"}, "name": {""}, "joining_date": {"soon"}, "status": {"Active"},
	})
	webtest.WantStatus(t, rules, http.StatusUnprocessableEntity)
	webtest.WantFieldErrorID(t, rules, components.FieldID("name"), "Name is required.")

	// A stored value on another section that fails Validate isn't this
	// section's to show.
	w.storeRelievingBeforeJoining(t, e)
	other := b.Post(t, w.editURL(e, "reporting"), url.Values{"version": {strconv.Itoa(e.Version)}, "branch_id": {"abc"}})
	webtest.WantStatus(t, other, http.StatusUnprocessableEntity)
	webtest.WantFieldErrorID(t, other, components.FieldID("branch_id"), msgNoBranch)
	webtest.WantNoText(t, other, msgRelieving)
	w.clearRelieving(t, e)

	// CD-217: the optional picker has Clear, which posts the link blank.
	form := b.Get(t, w.editURL(e, "reporting"))
	if n := form.Doc.Find(`[data-picker-clear][aria-label="Clear Reporting manager"]`).Length(); n != 1 {
		t.Errorf("Clear controls on manager_id = %d; want 1", n)
	}
	webtest.WantRedirect(t, b.Post(t, w.editURL(e, "reporting"), url.Values{
		"version": {strconv.Itoa(e.Version)}, "branch_id": {strconv.FormatInt(e.BranchID, 10)}, "manager_id": {""},
	}), w.recordURL(e))
	if got := w.get(t, "EMP-007"); got.ManagerID != 0 {
		t.Errorf("manager = %d; want none", got.ManagerID)
	}
}

// Leaver rules are the service's, and the screens show its refusals: Disable
// on the record page (MST-117, MST-118) and a status change on details, whose
// refusal on another section's field still shows (CD-189).
func TestScreens_LeaverRefusals(t *testing.T) {
	w := newWorld(t)
	b := w.h.Browser(w.h.Env.Kavya)

	boss := w.get(t, "EMP-004")
	refused := b.Post(t, w.recordURL(boss)+"/disable", url.Values{"version": {strconv.Itoa(boss.Version)}})
	webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
	webtest.WantBanner(t, refused, "Enter the relieving date.")
	webtest.WantText(t, refused, "Reporting")

	section := b.Post(t, w.editURL(boss, "details"), url.Values{
		"version": {strconv.Itoa(boss.Version)}, "code": {"EMP-004"}, "name": {boss.Name}, "joining_date": {types.FormatDate(boss.Joining)},
		"status": {"Left"}, "relieving_date": {"31-10-2026"},
	})
	webtest.WantStatus(t, section, http.StatusUnprocessableEntity)
	webtest.WantFieldErrorID(t, section, components.FieldID("status"), msgReports)

	// EMP-008's user wasn't disabled by a leave in this service's records, so
	// the re-join is refused with MST-112 on a field of another section.
	left := w.get(t, "EMP-008")
	rejoin := b.Post(t, w.editURL(left, "details"), url.Values{
		"version": {strconv.Itoa(left.Version)}, "code": {"EMP-008"}, "name": {left.Name}, "joining_date": {types.FormatDate(left.Joining)},
		"status": {"Active"},
	})
	webtest.WantStatus(t, rejoin, http.StatusUnprocessableEntity)
	webtest.WantBanner(t, rejoin, msgNoUser)

	// Its own field's error and the other section's together.
	both := b.Post(t, w.editURL(left, "details"), url.Values{
		"version": {strconv.Itoa(left.Version)}, "code": {"EMP-008"}, "name": {left.Name}, "joining_date": {types.FormatDate(left.Joining)},
		"birth_date": {"01-11-2026"}, "status": {"Active"},
	})
	webtest.WantStatus(t, both, http.StatusUnprocessableEntity)
	webtest.WantFieldErrorID(t, both, components.FieldID("birth_date"), msgBirth)
	webtest.WantBanner(t, both, msgNoUser)

	enable := b.Post(t, w.recordURL(left)+"/enable", url.Values{"version": {strconv.Itoa(left.Version)}})
	webtest.WantStatus(t, enable, http.StatusUnprocessableEntity)
	webtest.WantBanner(t, enable, msgNoUser)
}

// Disable, Enable and Delete work from the record page, and the delete confirm
// step is on it.
func TestScreens_EmployeeDisableEnableDelete(t *testing.T) {
	w := newWorld(t)
	b := w.h.Browser(w.h.Env.Kavya)
	e := w.get(t, "EMP-007")
	in := input(e)
	in.Relieving = in.Joining
	if _, err := w.svc.Update(context.Background(), w.h.Env.Kavya, e.ID, e.Version, in); err != nil {
		t.Fatal(err)
	}
	e = w.get(t, "EMP-007")
	off := b.Post(t, w.recordURL(e)+"/disable", url.Values{"version": {strconv.Itoa(e.Version)}})
	webtest.WantRedirect(t, off, w.recordURL(e))
	webtest.WantBanner(t, b.Get(t, off.Location), "Employee disabled.")
	if got := w.get(t, "EMP-007"); got.Status != employee.Left {
		t.Fatalf("status = %q", got.Status)
	}

	e = w.get(t, "EMP-007")
	on := b.Post(t, w.recordURL(e)+"/enable", url.Values{"version": {strconv.Itoa(e.Version)}})
	webtest.WantRedirect(t, on, w.recordURL(e))
	webtest.WantBanner(t, b.Get(t, on.Location), "Employee enabled.")

	e = w.get(t, "EMP-007")
	webtest.WantText(t, b.Get(t, w.recordURL(e)+"?delete"), "This can't be undone.")
	gone := b.Post(t, w.recordURL(e)+"/delete", url.Values{"version": {strconv.Itoa(e.Version)}})
	webtest.WantRedirect(t, gone, base)
	webtest.WantBanner(t, b.Get(t, gone.Location), "Employee deleted.")
	w.wantNoRow(t, "EMP-007")
}

// The list: titles, the status column with its stored word, Left hidden until
// asked for; search; the empty search; export; the picker hides leavers.
func TestScreens_EmployeeList(t *testing.T) {
	w := newWorld(t)
	b := w.h.Browser(w.h.Env.Kavya)
	page := b.Get(t, base)
	webtest.WantStatus(t, page, http.StatusOK)
	webtest.WantText(t, page, "Employees")
	webtest.WantText(t, page, "7 employees")
	row, found := page.RowLink(w.recordURL(w.get(t, "EMP-007")))
	if !found || !strings.Contains(row, "EMP-007 Deepak Nair") || !strings.Contains(row, "Active") || !strings.Contains(row, "EMP-004 Priya Shah") {
		t.Errorf("row = %q, %v", row, found)
	}
	if _, found := page.RowLink(w.recordURL(w.get(t, "EMP-008"))); found {
		t.Errorf("a leaver is listed by default")
	}
	all := b.Get(t, base+"?include_disabled=true")
	if row, _ := all.RowLink(w.recordURL(w.get(t, "EMP-008"))); !strings.Contains(row, "Left") || !strings.Contains(row, "Disabled") {
		t.Errorf("leaver row = %q", row)
	}
	webtest.WantText(t, b.Get(t, base+"?q=nosuchperson"), "No results.")

	csv := b.Get(t, base+"/export.csv")
	webtest.WantStatus(t, csv, http.StatusOK)
	if !strings.Contains(csv.Body, "EMP-007") {
		t.Errorf("export lacks EMP-007: %s", csv.Body)
	}

	pick := w.h.GetHTMX(t, w.h.Env.Kavya, "/pick/Employee?q=EMP")
	webtest.WantStatus(t, pick, http.StatusOK)
	var seen []string
	for _, row := range pick.PickRows() {
		seen = append(seen, row.Text)
	}
	if text := strings.Join(seen, "|"); !strings.Contains(text, "EMP-007") || strings.Contains(text, "EMP-008") {
		t.Errorf("picker rows = %q", text)
	}
}

// Ref is the service's: on the real auth.Service a user who may read only
// their own records is refused the record as the service refuses it, and a
// restricted user is unaffected, since Employee declares no dimension.
func TestScreens_EmployeeScopeOnRealAuth(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	e := w.get(t, "EMP-007")
	env := w.h.Env
	if got := w.h.Browser(env.Priya).Get(t, w.recordURL(e)); got.Status != http.StatusOK {
		t.Fatalf("restricted reader: status %d", got.Status)
	}
	a := env.Kit.Auth.(*auth.Auth)
	if err := a.SetRights(ctx, env.Admin, "Purchase User", employee.DocType, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeOwn}); err != nil {
		t.Fatal(err)
	}
	if err := a.SetRights(ctx, env.Admin, "Employee", employee.DocType, map[auth.Right]auth.Scope{}); err != nil {
		t.Fatal(err)
	}
	webtest.WantStatus(t, w.h.Browser(env.Priya).Get(t, w.recordURL(e)), http.StatusNotFound)
	if err := a.SetRights(ctx, env.Admin, "Purchase User", employee.DocType, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightWrite: auth.ScopeOwn}); err != nil {
		t.Fatal(err)
	}
	page := w.h.Browser(env.Priya).Get(t, w.recordURL(e))
	webtest.WantStatus(t, page, http.StatusOK)
	webtest.WantNoText(t, page, "Change")
	r := w.h.Browser(env.Priya).Post(t, w.editURL(e, "details"), url.Values{"version": {strconv.Itoa(e.Version)}})
	webtest.WantStatus(t, r, http.StatusForbidden)
	_, err := w.svc.Update(ctx, env.Priya, e.ID, e.Version, input(e))
	if err == nil {
		t.Fatal("the service let an own-scope user save")
	}
	webtest.WantText(t, r, err.Error())
}

// The production wiring mounts the wizard on the session manager WP3 passes.
func TestScreens_EmployeeMountedByAllmasters(t *testing.T) {
	h := webtest.NewSessions(t, func(k mstkit.Env, sm *scs.SessionManager) func(chi.Router) {
		return func(r chi.Router) {
			allmasters.New(k).Routes(r, allmasters.WebDeps{Sessions: sm, Files: (*files.Store)(nil)})
		}
	})
	b := h.Browser(h.Env.Kavya)
	webtest.WantRedirect(t, b.Post(t, stepOne, details("EMP-117")), stepTwo)
	value(t, b.Get(t, stepOne), "code", "EMP-117")
}

// CD-222 on the real auth.Service: a reader sees Linked and the email nowhere in
// the page, nothing at all when no user is linked; a writer sees the email.
func TestScreens_EmailOnlyForWriters(t *testing.T) {
	w := newWorld(t)
	env := w.h.Env
	const email = "ravi@vardhan.test"
	linked, free := w.get(t, "EMP-003"), w.get(t, "EMP-007")

	reader := w.h.Browser(env.Priya).Get(t, w.recordURL(linked))
	webtest.WantStatus(t, reader, http.StatusOK)
	webtest.WantText(t, reader, "Linked")
	if strings.Contains(reader.Body, email) {
		t.Errorf("a reader's page holds the email")
	}
	none := w.h.Browser(env.Priya).Get(t, w.recordURL(free))
	webtest.WantNoText(t, none, "Linked")

	writer := w.h.Browser(env.Kavya).Get(t, w.recordURL(linked))
	webtest.WantText(t, writer, email)
	webtest.WantNoText(t, writer, "Linked")

	// By the right on this record, not by role: own-scope write on another's
	// record is a reader.
	a := env.Kit.Auth.(*auth.Auth)
	if err := a.SetRights(context.Background(), env.Admin, "Masters Manager", employee.DocType, map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeAll, auth.RightWrite: auth.ScopeOwn, auth.RightCreate: auth.ScopeAll,
	}); err != nil {
		t.Fatal(err)
	}
	own := w.h.Browser(env.Kavya).Get(t, w.recordURL(linked))
	webtest.WantText(t, own, "Linked")
	if strings.Contains(own.Body, email) {
		t.Errorf("an own-scope writer's page holds another record's email")
	}

	// The list has no user column to leak through.
	if body := w.h.Browser(env.Priya).Get(t, base).Body; strings.Contains(body, email) {
		t.Errorf("the list holds the email")
	}
}

// CD-179, CD-181: a section that changes a leaver's user is the service's
// refusal, next to the field.
func TestScreens_EditSeparateSaves(t *testing.T) {
	w := newWorld(t)
	e := w.get(t, "EMP-008")
	r := w.h.Browser(w.h.Env.Kavya).Post(t, w.editURL(e, "reporting"), url.Values{
		"version": {strconv.Itoa(e.Version)}, "branch_id": {strconv.FormatInt(e.BranchID, 10)}, "user_id": {"deepak@vardhan.test"},
	})
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, r, "user_id", "Change the user and the status in separate saves.")
	if got := w.get(t, "EMP-008"); got.UserID != e.UserID {
		t.Errorf("user = %d", got.UserID)
	}
}

// MST-119 through the details section: leaving disables the user and
// re-joining enables it again.
func TestScreens_EditLeaveAndRejoin(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	b := w.h.Browser(w.h.Env.Kavya)
	enabled := func() bool {
		t.Helper()
		var on bool
		err := w.h.Env.DB.Read(ctx, func(tx *db.Tx) error {
			u, _, err := w.h.Env.Kit.Auth.User(ctx, tx, 5)
			on = u.Enabled
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return on
	}
	post := func(status, relieving string) {
		t.Helper()
		e := w.get(t, "EMP-005")
		webtest.WantRedirect(t, b.Post(t, w.editURL(e, "details"), url.Values{
			"version": {strconv.Itoa(e.Version)}, "code": {"EMP-005"}, "name": {e.Name}, "joining_date": {types.FormatDate(e.Joining)},
			"status": {status}, "relieving_date": {relieving},
		}), w.recordURL(e))
	}
	post("Left", "31-10-2026")
	if enabled() {
		t.Errorf("the user is enabled after leaving")
	}
	post("Active", "")
	if !enabled() {
		t.Errorf("the user is disabled after re-joining")
	}
}

// Own scope on the real auth.Service, on each path: the screens refuse as the
// service refuses, and lists show only what the user created.
func TestScreens_OwnScopeEveryPath(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	env := w.h.Env
	a := env.Kit.Auth.(*auth.Auth)
	e := w.get(t, "EMP-007")
	version := url.Values{"version": {strconv.Itoa(e.Version)}}
	if err := a.SetRights(ctx, env.Admin, "Employee", employee.DocType, map[auth.Right]auth.Scope{}); err != nil {
		t.Fatal(err)
	}

	// Read everything, write and delete own: disable and delete are refused
	// with the service's words, and the actions don't show.
	if err := a.SetRights(ctx, env.Admin, "Purchase User", employee.DocType, map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeAll, auth.RightWrite: auth.ScopeOwn, auth.RightDelete: auth.ScopeOwn,
	}); err != nil {
		t.Fatal(err)
	}
	priya := w.h.Browser(env.Priya)
	off := priya.Post(t, w.recordURL(e)+"/disable", version)
	webtest.WantStatus(t, off, http.StatusForbidden)
	webtest.WantText(t, off, env.Kit.Auth.Can(ctx, env.Priya, employee.DocType, auth.RightWrite, &auth.RecordRef{DocType: employee.DocType, ID: e.ID}).Error())
	del := priya.Post(t, w.recordURL(e)+"/delete", version)
	webtest.WantStatus(t, del, http.StatusForbidden)
	webtest.WantText(t, del, env.Kit.Auth.Can(ctx, env.Priya, employee.DocType, auth.RightDelete, &auth.RecordRef{DocType: employee.DocType, ID: e.ID}).Error())
	page := priya.Get(t, w.recordURL(e))
	webtest.WantNoText(t, page, "Disable")
	webtest.WantNoText(t, page, "Delete")
	if got := w.get(t, "EMP-007"); got.Status != e.Status || got.Version != e.Version {
		t.Errorf("a refused action wrote: %+v", got)
	}

	// Read, create and export own: the wizard works, and the list, picker and
	// export show the user's own row alone.
	if err := a.SetRights(ctx, env.Admin, "Purchase User", employee.DocType, map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeOwn, auth.RightCreate: auth.ScopeOwn, auth.RightExport: auth.ScopeOwn,
	}); err != nil {
		t.Fatal(err)
	}
	priya = w.h.Browser(env.Priya)
	w.toCheck(t, priya, details("EMP-124"), w.reporting())
	webtest.WantStatus(t, priya.Post(t, stepCheck, url.Values{}), http.StatusSeeOther)
	mine := w.get(t, "EMP-124")

	list := priya.Get(t, base)
	if _, found := list.RowLink(w.recordURL(mine)); !found {
		t.Errorf("the list lacks the user's own row\ntext: %s", list.Text())
	}
	webtest.WantNoText(t, list, "EMP-007")
	webtest.WantNoText(t, list, "EMP-003")

	pick := w.h.GetHTMX(t, env.Priya, "/pick/Employee?q=EMP")
	if rows := pick.PickRows(); len(rows) != 1 || !strings.Contains(rows[0].Text, "EMP-124") {
		t.Errorf("picker rows = %+v", rows)
	}

	csv := priya.Get(t, base+"/export.csv")
	webtest.WantStatus(t, csv, http.StatusOK)
	if !strings.Contains(csv.Body, "EMP-124") || strings.Contains(csv.Body, "EMP-007") {
		t.Errorf("export = %s", csv.Body)
	}
	webtest.WantStatus(t, priya.Get(t, w.recordURL(e)), http.StatusNotFound)
}
