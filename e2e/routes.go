//go:build e2e

package e2e

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/chromedp/chromedp"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	authscreens "github.com/vardhan/internaltools/internal/kernel/auth/web/screens"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/screens"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

const (
	sweepRole = "Sweep role, which no fixture user holds"

	idRoute  = "/{id:[1-9][0-9]*}"
	rowRoute = "/{row:[1-9][0-9]*}"

	longLines   = 3
	manyLines   = 500
	maxAmount   = "9999999999.99"
	maxQty      = "100000.000"
	maxRate     = "999999.9999"
	longRemarks = 400 // bytes of uitest.LongError, under fixture.Order's 500
	missingDoc  = 999999
)

// sweepState is what the screens need beyond the canonical seed: a row of
// worst-case values in every master and a document in every state.
type sweepState struct {
	long   testkit.LongRows
	order  docscreen.SampleIDs
	amend  int64
	claim  int64
	file   int64
	role   int64
	admin  int64
	rule   int64
	master map[string]int64 // record id by /m/ slug
}

// prepareSweep seeds sweepState. The stand-in masters' rows the reference order
// links to take the long values in place, so Priya's project restriction still
// lets her read the documents.
func (h *Harness) prepareSweep() sweepState {
	h.t.Helper()
	s := sweepState{long: testkit.SeedLongValues(h.t, h.App.DB(), h.Fx, testkit.LongValues{
		Name: uitest.LongName100, Devanagari: uitest.LongDevanagari, Code60: uitest.LongCode60, Email: uitest.LongEmail,
	})}
	ctx := context.Background()
	err := h.App.DB().Write(ctx, func(tx *db.Tx) error {
		for _, table := range []string{"fixture_party", "fixture_item", "fixture_project"} {
			if _, err := tx.SQL().ExecContext(ctx, `UPDATE `+table+` SET name = ?, code = ? || code`, uitest.LongName100, uitest.LongCode60[:40]); err != nil { //nolint:gosec // G202: the three table names above
				return err
			}
		}
		return nil
	})
	if err != nil {
		h.t.Fatalf("long stand-in masters: %v", err)
	}
	if err := h.App.Auth().CreateRole(ctx, h.Principal(admin), sweepRole); err != nil {
		h.t.Fatalf("create the sweep role: %v", err)
	}
	s.role = Scalar[int64](h, `SELECT id FROM roles WHERE name = ?`, sweepRole)
	s.admin = Scalar[int64](h, `SELECT id FROM roles WHERE name = ?`, auth.RoleAdministrator)
	s.rule = Scalar[int64](h, `SELECT id FROM approval_rules ORDER BY id LIMIT 1`)
	s.master = map[string]int64{
		"branch": s.long.Branch, "company": h.Fx.Company.ID, "employee": s.long.Employee, "expense-type": s.long.ExpenseType,
		"item": s.long.Item, "party": s.long.Party, "payment-terms": s.long.Terms, "project": s.long.Project,
		"site": s.long.Site, "tax-code": s.long.TaxCode, "uom": s.long.UoM,
	}

	// Past 5 lakh the approver is the Director.
	submitted := func() doc.Result {
		r := h.longOrder(longLines)
		return h.Do(priya, fixture.Order, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version})
	}
	approved := func() doc.Result {
		r := submitted()
		return h.Do(meena, fixture.Order, r.ID, doctype.ActionApprove, doc.ActionInput{Version: r.Version})
	}
	s.order.Draft = h.longOrder(longLines).ID
	s.order.Lines500 = h.longOrder(manyLines).ID
	s.order.Pending = submitted().ID
	s.order.Approved = approved().ID
	r := submitted()
	s.order.Rejected = h.Do(meena, fixture.Order, r.ID, doctype.ActionReject, doc.ActionInput{Version: r.Version, Reason: uitest.LongError}).ID
	r = approved()
	s.order.Closed = h.Do(priya, fixture.Order, r.ID, doctype.ActionClose, doc.ActionInput{Version: r.Version, Reason: uitest.LongError}).ID
	r = approved()
	r = h.Do(ravi, fixture.Order, r.ID, doctype.ActionCancel, doc.ActionInput{Version: r.Version, Reason: uitest.LongError})
	s.order.Cancelled = r.ID
	s.amend = h.Do(ravi, fixture.Order, r.ID, doctype.ActionAmend, doc.ActionInput{Version: r.Version}).ID

	c := h.Do(deepak, fixture.Claim, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"employee_id": strconv.FormatInt(h.employee(deepak), 10), "claim_date": plainDate, "remarks": uitest.LongName100},
		Lines:  []map[string]string{{"expense_type": uitest.LongName100, "description": uitest.LongName100, "amount": maxAmount}},
	})
	s.claim = c.ID
	s.file = h.attach(deepak, fixture.Claim, c.ID, uitest.LongName100+".jpg")
	return s
}

// longOrder is a draft of Priya's with lines lines of worst-case values.
func (h *Harness) longOrder(lines int) doc.Result {
	h.t.Helper()
	in := doc.ActionInput{Values: map[string]string{
		"supplier_id": strconv.FormatInt(h.M.Party["SHREE"], 10), "project_id": strconv.FormatInt(h.M.Project["PRJ-METRO"], 10),
		"order_date": plainDate, "amount": maxAmount, "remarks": uitest.LongError[:longRemarks], "supplier_ref": uitest.LongCode60,
	}}
	for range lines {
		in.Lines = append(in.Lines, map[string]string{
			"item_id": strconv.FormatInt(h.M.Item["MS-ANGLE-50"], 10), "qty": maxQty, "rate": maxRate, "uom": "kg", "description": uitest.LongName100,
		})
	}
	return h.Do(priya, fixture.Order, 0, doctype.ActionSave, in)
}

// employee is the fixture user's employee row.
func (h *Harness) employee(email string) int64 {
	h.t.Helper()
	return Scalar[int64](h, `SELECT e.id FROM employee e JOIN users u ON u.id = e.user_id WHERE u.email = ?`, email)
}

// attach adds receiptPhoto to a draft as email, and returns the attachment's id.
func (h *Harness) attach(email, dt string, id int64, name string) int64 {
	h.t.Helper()
	f, err := os.Open(receiptPhoto(h.t))
	if err != nil {
		h.t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	spooled, err := h.App.Files().Spool(f, files.MaxFileBytes)
	if err != nil {
		h.t.Fatal(err)
	}
	var att int64
	ctx := context.Background()
	err = h.App.DB().Write(ctx, func(tx *db.Tx) error {
		var err error
		att, err = h.App.Files().Attach(ctx, tx, h.Principal(email), auth.RecordRef{DocType: dt, ID: id}, name, spooled)
		return err
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return att
}

// submit is what a screen's Setup runs: js on the page, then wait until the
// selector wait is visible. uibrowser.Check gives it its deadline.
func submit(ctx context.Context, js, wait string) error {
	return chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](js), chromedp.WaitVisible(chromedp.CSS(wait)))
}

// appScreens lists every page state of the seeded app (plan Task 5).
func (h *Harness) appScreens(s sweepState) []screens.Screen {
	out := []screens.Screen{
		{Name: "home", Route: "/", URL: "/", As: priya},
		{Name: "inbox", Route: "/approvals", URL: "/approvals", As: meena},
		{Name: "inbox empty", Route: "/approvals", URL: "/approvals", As: deepak},
		{Name: "my requests", Route: "/approvals/mine", URL: "/approvals/mine", As: priya},
		{Name: "stuck approvals", Route: "/admin/approvals/stuck", URL: "/admin/approvals/stuck", As: admin},
		{Name: "approval rules", Route: "/admin/approval-rules", URL: "/admin/approval-rules", As: admin},
		{Name: "approval rule new", Route: "/admin/approval-rules/new", URL: "/admin/approval-rules/new", As: admin},
		{Name: "approval rule", Route: "/admin/approval-rules/{id}", URL: "/admin/approval-rules/" + strconv.FormatInt(s.rule, 10), As: admin},
		{Name: "numbering", Route: "/settings/numbering", URL: "/settings/numbering", As: admin},
		{Name: "numbering series", Route: "/settings/numbering/{doctype}", URL: "/settings/numbering/" + fixture.Order, As: admin},
		{Name: "phone menu", Route: "/approvals", URL: "/approvals", As: admin, Setup: func(ctx context.Context) error {
			if err := submit(ctx, `document.getElementById("nav-sheet").showModal()`, "#nav-sheet"); err != nil {
				return err
			}
			// axe reads colours, so the sheet's fade-in must have ended (it failed axe once under load).
			return chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](`Promise.all(document.getElementById("nav-sheet").getAnimations({subtree: true}).map((a) => a.finished))`, chromedp.EvalAwaitPromise))
		}},
		{Name: "not found", Route: "/doc/{doctype}/{id}", URL: DocPath(fixture.Order, missingDoc), As: priya},
		{Name: "refused", Route: "/users", URL: "/users", As: priya},
		{Name: "signed out", Route: "/signin", URL: "/signin?next=%2Fapprovals"},

		{Name: "order amendment", Route: "/doc/{doctype}/{id}", URL: DocPath(fixture.Order, s.amend), As: priya},
		{Name: "claim list", Route: "/doc/{doctype}", URL: "/doc/" + fixture.Claim, As: deepak},
		{Name: "claim list filtered", Route: "/doc/{doctype}", URL: "/doc/" + fixture.Claim + "?" + docscreen.ListQuery{
			Filters: []docscreen.Filter{{Field: "state", Op: "=", Values: []string{string(doctype.StateDraft)}}},
		}.Encode(), As: deepak},
		{Name: "claim new", Route: "/doc/{doctype}/new", URL: "/doc/" + fixture.Claim + "/new", As: deepak},
		{Name: "claim page", Route: "/doc/{doctype}/{id}", URL: DocPath(fixture.Claim, s.claim), As: deepak},
		{Name: "receipt new", Route: "/doc/{doctype}/new", URL: "/doc/" + fixture.Receipt + "/new", As: suresh},
		{Name: "company logo", Route: "/m/company/1/logo", URL: "/m/company/1/logo", As: admin},
	}
	for _, sc := range docscreen.Screens(fixture.Order, s.order) {
		switch {
		case strings.HasSuffix(sc.Name, " reject"):
			sc.As = meena // past 5 lakh the Director approves
		case strings.HasSuffix(sc.Name, " cancel"):
			sc.As = ravi // Purchase Manager holds cancel
		case strings.HasSuffix(sc.Name, " list filtered"):
			continue // fixture.Order declares no filterable field; the claim's list below has it
		}
		out = append(out, sc)
	}
	out = append(out, authscreens.Screens(authscreens.SweepIDs{
		Role: s.role, Admin: s.admin, User: s.long.User, NotShown: fixture.Item, Team: fixture.Claim,
	}, submit)...)
	return append(append(out, h.masterScreens(s)...), importScreens()...)
}

// masterScreens lists every master's list, new and record pages, the two create
// wizards step by step, and each record's edit pages. A master without a page has
// no route, so the router says which to list.
func (h *Harness) masterScreens(s sweepState) []screens.Screen {
	var out []screens.Screen
	routed := func(route string) bool {
		found := false
		_ = chi.Walk(h.routes(), func(method, r string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
			found = found || (method == http.MethodGet && r == route)
			return nil
		})
		return found
	}
	for slug, id := range s.master {
		base, record := "/m/"+slug, "/m/"+slug+"/"+strconv.FormatInt(id, 10)
		out = append(out,
			screens.Screen{Name: slug + " list", Route: base, URL: base, As: admin},
			screens.Screen{Name: slug + " page", Route: base + idRoute, URL: record, As: admin})
		if routed(base + "/new") {
			out = append(out, screens.Screen{Name: slug + " new", Route: base + "/new", URL: base + "/new", As: admin})
		}
	}
	wizard := func(slug string, steps ...wizardStep) {
		base := "/m/" + slug + "/new"
		for i, step := range steps {
			out = append(out, screens.Screen{
				Name: slug + " wizard " + step.slug, Route: base + "/{step}", URL: base + "/" + steps[0].slug, As: admin, Setup: walkWizard(steps[:i]),
			})
		}
	}
	section := func(slug, name string) {
		out = append(out, screens.Screen{
			Name: slug + " edit " + name, Route: "/m/" + slug + idRoute + "/edit/{section}",
			URL: "/m/" + slug + "/" + strconv.FormatInt(s.master[slug], 10) + "/edit/" + name, As: admin,
		})
	}
	wizard("employee", wizardStep{slug: "details", fill: fillEmployee},
		wizardStep{slug: "reporting", fill: `set("branch_id", "` + strconv.FormatInt(s.long.Branch, 10) + `");`}, wizardStep{slug: "check"})
	for _, name := range []string{"details", "reporting"} {
		section("employee", name)
	}
	wizard("party",
		wizardStep{slug: "details", fill: fillParty},
		wizardStep{slug: "addresses", rows: true, row: ".line1", fill: fillAddress},
		wizardStep{slug: "contacts", rows: true, row: ".first_name", fill: fillContact},
		wizardStep{slug: "check"})
	for _, name := range []string{"details", "gst", "terms"} {
		section("party", name)
	}
	out = append(out,
		screens.Screen{Name: "party wizard refused", Route: "/m/party/new/{step}", URL: "/m/party/new/details", As: admin, Setup: refuse(`input[name=is_vendor]`, `
  f.elements.is_vendor.checked = false;
  f.elements.is_customer.checked = false;`)},
		screens.Screen{Name: "party canonical", Route: "/m/party" + idRoute, URL: "/m/party/" + strconv.FormatInt(h.Fx.Parties[0].ID, 10), As: admin},
		screens.Screen{Name: "party edit refused", Route: "/m/party" + idRoute + "/edit/{section}", URL: "/m/party/" + strconv.FormatInt(s.long.Party, 10) + "/edit/details", As: admin,
			Setup: refuse(`input[name=name]`, `f.elements.name.value = "";`)},
		screens.Screen{Name: "employee edit refused", Route: "/m/employee" + idRoute + "/edit/{section}", URL: "/m/employee/" + strconv.FormatInt(s.long.Employee, 10) + "/edit/details", As: admin,
			Setup: refuse(`input[name=name]`, `f.elements.name.value = "";`)},
	)
	party := "/m/party/" + strconv.FormatInt(s.long.Party, 10)
	for _, kind := range []struct {
		name string
		row  int64
	}{{"addresses", s.long.Address}, {"contacts", s.long.Contact}} {
		out = append(out,
			screens.Screen{Name: "party " + kind.name + " new", Route: "/m/party" + idRoute + "/" + kind.name + "/new", URL: party + "/" + kind.name + "/new", As: admin},
			screens.Screen{Name: "party " + kind.name + " row", Route: "/m/party" + idRoute + "/" + kind.name + rowRoute, URL: party + "/" + kind.name + "/" + strconv.FormatInt(kind.row, 10), As: admin})
	}
	addresses, contacts := party+"/addresses/", party+"/contacts/"
	return append(out,
		screens.Screen{Name: "party address refused", Route: "/m/party" + idRoute + "/addresses" + rowRoute, URL: addresses + strconv.FormatInt(s.long.Address, 10), As: admin,
			Setup: refuse(`input[name=line1]`, `f.elements.line1.value = "";`)},
		screens.Screen{Name: "party contact refused", Route: "/m/party" + idRoute + "/contacts" + rowRoute, URL: contacts + strconv.FormatInt(s.long.Contact, 10), As: admin,
			Setup: refuse(`input[name=first_name]`, `f.elements.first_name.value = "";`)},
	)
}

// refuse is a Setup that runs change on f, the form of field, and saves it past
// the browser's own checks, so the server's error summary shows.
func refuse(field, change string) func(context.Context) error {
	return func(ctx context.Context) error {
		return submit(ctx, `(() => {
  const f = document.querySelector("`+field+`").form;`+change+`
  f.noValidate = true;
  f.requestSubmit();
})()`, ".error-summary")
	}
}

// wizardStep is one create-wizard step: fill runs on the page with set in
// scope, on the step's row if rows is set. row is the suffix of one of the row's
// field names.
type wizardStep struct {
	slug, fill, row string
	rows            bool
}

// What the wizards are given, through the real forms.
var (
	fillEmployee = `set("code", "` + uitest.LongCode60[:20] + `"); set("name", "` + uitest.LongName100 + `"); set("joining_date", "` + plainDate + `");`
	fillParty    = `set("code", "` + uitest.LongCode60[:19] + `"); set("name", "` + uitest.LongName100 + `"); check("is_vendor"); check("is_customer");`
	fillAddress  = `set(".title", "` + uitest.LongName100 + `"); set(".line1", "` + uitest.LongName100 + `"); set(".line2", "` + uitest.LongDevanagari + `");
  set(".city", "` + uitest.LongName100 + `"); set(".state_code", "27"); set(".pincode", "400093"); check(".is_billing");`
	fillContact = `set(".first_name", "` + uitest.LongName100 + `"); set(".last_name", "` + uitest.LongDevanagari + `");
  set(".email", "` + uitest.LongEmail + `"); set(".mobile", "9820012345"); check(".is_primary");`
)

// fillJS runs fill with set and check, which find a field by name, or by the end
// of its name for a row's.
func fillJS(fill string) string {
	return `(() => {
  const el = (n) => document.querySelector(n[0] === "." ? '[name$="' + n + '"]' : '[name="' + n + '"]');
  const set = (n, v) => { el(n).value = v; };
  const check = (n) => { el(n).checked = true; };
  ` + fill + `
  document.getElementById("wizard-form").requestSubmit();
})()`
}

// walkWizard is a Setup that fills and leaves each of steps in turn, from the
// first, adding the row of a row step unless the session still holds it. The
// sweep comes here at every width on one session, so it must work again.
func walkWizard(steps []wizardStep) func(context.Context) error {
	return func(ctx context.Context) error {
		for i, st := range steps {
			if st.rows {
				row := `[name$="` + st.row + `"]`
				add := `document.querySelector('` + row + `') || document.querySelector('button[name=_rows][value=add]').click()`
				if err := submit(ctx, add, row); err != nil {
					return fmt.Errorf("add a row on %s: %w", st.slug, err)
				}
			}
			at := `progress.stepper__bar[value="` + strconv.Itoa(i+2) + `"]`
			if err := chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](fillJS(st.fill)), chromedp.WaitReady(chromedp.CSS(at))); err != nil {
				return fmt.Errorf("leave %s: %w", st.slug, err)
			}
		}
		return nil
	}
}

// importCSV has one good row and one a master refuses, so Check results shows
// both. importReady has only a good row, and a column no master has, so Import
// shows its warning.
const (
	importCSV   = `code,name\nGOOD,Good row\n,` + uitest.LongName100 + `\n`
	importReady = `code,name,colour\nREADY,Ready row,red\n`
)

// importScreens walks the CSV import wizard: each step's URL sends the browser
// back to the step that is missing, and the Setups go forward from there.
func importScreens() []screens.Screen {
	const base = "/m/import/"
	choose := `(() => {
  const r = document.querySelector("select[name=doctype]");
  r.value = "UoM";
  r.form.requestSubmit();
})()`
	upload := func(csv string) string {
		return `(() => {
  const input = document.querySelector("input[type=file]"), dt = new DataTransfer();
  dt.items.add(new File(["` + csv + `"], "uom.csv", {type: "text/csv"}));
  input.files = dt.files;
  input.form.requestSubmit();
})()`
	}
	toUpload := func(ctx context.Context) error { return submit(ctx, choose, "input[type=file]") }
	toCheck := func(csv string) func(ctx context.Context) error {
		return func(ctx context.Context) error {
			if err := toUpload(ctx); err != nil {
				return err
			}
			return submit(ctx, upload(csv), `progress.stepper__bar[value="3"]`)
		}
	}
	toImport := func(ctx context.Context) error {
		if err := toCheck(importReady)(ctx); err != nil {
			return err
		}
		if err := submit(ctx, `document.getElementById("wizard-form").requestSubmit()`, `progress.stepper__bar[value="4"]`); err != nil {
			return err
		}
		warned, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.body.innerText.includes("Column 'colour'")`))
		if err == nil && !warned {
			err = errors.New("import ready shows no warning for the colour column")
		}
		return err
	}
	return []screens.Screen{
		{Name: "import choose", Route: base + "choose", URL: base + "choose", As: admin},
		{Name: "import upload", Route: base + "upload", URL: base + "choose", As: admin, Setup: toUpload},
		{Name: "import check", Route: base + "check", URL: base + "choose", As: admin, Setup: toCheck(importCSV)},
		{Name: "import ready", Route: base + "import", URL: base + "choose", As: admin, Setup: toImport},
	}
}

// freshScreens are the first-run pages: only an empty install shows /setup,
// and only an admin with no company /setup/company.
func freshScreens() (setup, company []screens.Screen) {
	empty := `(() => {
  const f = document.querySelector("input[name=password]").form;
  f.noValidate = true;
  f.requestSubmit();
})()`
	noCompany := `(() => {
  const f = document.querySelector("input[name=legal_name]").form;
  f.noValidate = true;
  f.requestSubmit();
})()`
	return []screens.Screen{
			{Name: "setup", Route: "/setup", URL: "/setup"},
			{Name: "setup refused", Route: "/setup", URL: "/setup", Setup: func(ctx context.Context) error { return submit(ctx, empty, ".error-summary") }},
		}, []screens.Screen{
			{Name: "setup company", Route: "/setup/company", URL: "/setup/company", As: freshAdmin},
			{Name: "setup company refused", Route: "/setup/company", URL: "/setup/company", As: freshAdmin, Setup: func(ctx context.Context) error { return submit(ctx, noCompany, ".error-summary") }},
		}
}

// notPage is a GET route with no screen, because it answers with no page: the
// sweep can't look at it, and TestE2E_EveryRouteCovered fetches url, a sample of
// the route, to prove that it doesn't answer with one.
type notPage struct {
	route, url, why string
}

// notPageStatus is the status a sample answers with when it isn't 200.
var notPageStatus = map[string]int{"/files/*": http.StatusNotFound}

// notPageAfter names the screen whose state a sample needs, opened first.
var notPageAfter = map[string]string{"/m/import/errors.csv": "import check"}

// notPages lists them.
func notPages(s sweepState) []notPage {
	out := []notPage{
		{"/approvals/count", "/approvals/count", "the header's htmx count"},
		{"/settings/numbering/preview", "/settings/numbering/preview?tpl=PO%2F%7B%23%23%23%23%23%7D", "the template form's htmx preview"},
		{"/doc/{doctype}/export.csv", "/doc/" + fixture.Order + "/export.csv", "a CSV"},
		{"/m/import/template", "/m/import/template?doctype=UoM", "a CSV"},
		{"/m/import/errors.csv", "/m/import/errors.csv", "a CSV of a checked file's refused rows; alone it redirects to Choose"},
		{"/files/{id}", "/files/" + strconv.FormatInt(s.file, 10), "a file"},
		{"/files/*", "/files/no/such", "FILE's plain-text 404 for any other path under /files/; no page"},
	}
	for slug := range s.master {
		out = append(out, notPage{"/m/" + slug + "/export.csv", "/m/" + slug + "/export.csv", "a CSV"})
	}
	for _, name := range []string{"Branch", "Company", "Employee", "Expense Type", "Item", "Party", "Payment Terms", "Project", "Site", "Tax Code", "UoM"} {
		out = append(out, notPage{"/pick/" + name, "/pick/" + strings.ReplaceAll(name, " ", "%20") + "?q=a", "a picker's htmx rows"})
	}
	return out
}

// notPageProblems refuses an entry with no sample, which nothing would check.
func notPageProblems(pages []notPage) []string {
	var out []string
	for _, p := range pages {
		if p.url == "" {
			out = append(out, p.route+" has no sample URL")
		}
	}
	return out
}

// uncovered is the GET routes of r with no screen. DefaultSkip matches by
// prefix, but a not-page route only exactly, so /files/{id} can't hide a later
// /files/{id}/thumb.
func uncovered(r chi.Routes, pages []notPage) []string {
	return slices.DeleteFunc(screens.Uncovered(r, screens.DefaultSkip), func(route string) bool {
		return slices.ContainsFunc(pages, func(p notPage) bool { return p.route == route })
	})
}

// routes is the real router.
func (h *Harness) routes() chi.Routes {
	return h.App.Handler().(chi.Routes)
}
