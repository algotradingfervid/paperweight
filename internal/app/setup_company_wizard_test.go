package app_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/icons"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// The setup wizard's steps (UI v2 §5.4, E2): each its own URL, the values in
// the session until Save company.
const (
	setupStep1 = "/setup/company/company"
	setupStep2 = "/setup/company/office"
	setupCheck = "/setup/company/check"
)

// setupStepFields are the fields each step posts.
var setupStepFields = [][]string{
	{"legal_name", "gstin", "state_code", "pan"},
	{"branch_code", "branch_name", "address_line1", "address_city", "address_pincode"},
}

// stepForm is form's values for step i.
func stepForm(form url.Values, i int) url.Values {
	out := url.Values{}
	for _, name := range setupStepFields[i] {
		out.Set(name, form.Get(name))
	}
	return out
}

// setupCompany walks the wizard with form and presses Save company. It
// returns the first answer that isn't a 303 to the next step, else the save's.
func (b *browser) setupCompany(form url.Values) *response {
	b.w.t.Helper()
	if r := b.post(setupStep1, stepForm(form, 0)); r.status != http.StatusSeeOther || r.location() != setupStep2 {
		return r
	}
	if r := b.post(setupStep2, stepForm(form, 1)); r.status != http.StatusSeeOther || r.location() != setupCheck {
		return r
	}
	return b.post(setupCheck, url.Values{})
}

// toCheck walks steps 1 and 2 with form and fails unless both go forward.
func (b *browser) toCheck(form url.Values) {
	b.w.t.Helper()
	if r := b.post(setupStep1, stepForm(form, 0)); r.status != http.StatusSeeOther || r.location() != setupStep2 {
		b.w.t.Fatalf("POST step 1 = %d to %q (%q), want 303 to %s", r.status, r.location(), r.pageText(), setupStep2)
	}
	if r := b.post(setupStep2, stepForm(form, 1)); r.status != http.StatusSeeOther || r.location() != setupCheck {
		b.w.t.Fatalf("POST step 2 = %d to %q (%q), want 303 to %s", r.status, r.location(), r.pageText(), setupCheck)
	}
}

func valueOf(r *response, name string) string {
	return r.doc.Find(`[name="`+name+`"]`).AttrOr("value", "")
}

// setupRows counts what Save company writes.
func setupRows(w *world) int {
	return w.count(`SELECT (SELECT count(*) FROM company) + (SELECT count(*) FROM branch) + (SELECT count(*) FROM address)`)
}

// No case ID: UI v2 §5.4 and §5.6. Three steps with the v2 labels, an intro on
// each, one h1, the welcome on the first visit only, and every hint that says
// why a field matters.
func TestSetupWizard_StepsIntrosAndHints(t *testing.T) {
	w := newWorld(t)
	w.seedUsers()
	b := w.signedIn(adminEmail)

	for _, path := range []string{"/setup/company", setupStep1} {
		r := b.get(path)
		if r.status != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, r.status)
		}
		var labels []string
		r.doc.Find("nav.stepper .stepper__label").Each(func(_ int, s *goquery.Selection) {
			labels = append(labels, strings.TrimSpace(s.Text()))
		})
		if strings.Join(labels, "|") != "Company|Head office|Check" {
			t.Errorf("GET %s: step labels %q, want Company, Head office, Check", path, labels)
		}
		if n := r.doc.Find("h1").Length(); n != 1 {
			t.Errorf("GET %s: %d h1, want 1", path, n)
		}
		if got := strings.TrimSpace(r.doc.Find("h2#step-title").Text()); !strings.Contains(got, "Company") {
			t.Errorf("GET %s: step heading %q, want Company", path, got)
		}
		if got := strings.TrimSpace(r.doc.Find(".step-intro").Text()); got != "These details print on every purchase order and decide how GST is worked out." {
			t.Errorf("GET %s: intro %q", path, got)
		}
		if !strings.Contains(r.pageText(), "Welcome to Paperweight. Three short steps and your team can raise its first order. It takes about two minutes.") {
			t.Errorf("GET %s on the first visit: no welcome in %q", path, r.pageText())
		}
		if a := r.doc.Find("form#wizard-form").AttrOr("action", ""); a != setupStep1 {
			t.Errorf("GET %s: the form posts to %q, want %s", path, a, setupStep1)
		}
	}
	r := b.get(setupStep1)
	for id, want := range map[string]string{
		"f-legal_name-hint": "Exactly as on your GST certificate. Vendors see it on orders.",
		"f-gstin-tip":       "Your GSTIN tells the app which state you're in. When a vendor is in the same state, orders show CGST and SGST; otherwise IGST. Leave it blank only if the company isn't registered for GST.",
		"f-state_code-hint": "Only needed without a GSTIN. The two-digit GST state code.",
		"f-pan-hint":        "Needed when there is no GSTIN. With a GSTIN, it's taken from the GSTIN when you leave it blank.",
	} {
		if got := strings.Join(strings.Fields(r.doc.Find("#"+id).Text()), " "); got != want {
			t.Errorf("#%s = %q, want %q", id, got, want)
		}
	}
	if got := r.doc.Find("#f-gstin").AttrOr("placeholder", ""); got != "e.g. 27AABCS1429B1ZU" {
		t.Errorf("GSTIN placeholder = %q", got)
	}
	if !strings.Contains(r.pageText(), "Continue") || strings.Contains(r.pageText(), "Save company") {
		t.Errorf("step 1's primary isn't Continue: %q", r.pageText())
	}

	b.toCheck(goodSetup())
	r = b.get(setupStep2)
	if got := strings.TrimSpace(r.doc.Find(".step-intro").Text()); got != "Orders are billed to a branch. You can add more branches later." {
		t.Errorf("step 2 intro %q", got)
	}
	if got := strings.Join(strings.Fields(r.doc.Find("#f-branch_code-tip").Text()), " "); got != "A short code printed on documents and used in numbering, for example HO." {
		t.Errorf("head office code tip %q", got)
	}
	if got := strings.Join(strings.Fields(r.doc.Find("#f-address_line1-hint").Text()), " "); got != "Prints on the letterhead of every order." {
		t.Errorf("address line 1 hint %q", got)
	}
	r = b.get(setupStep1)
	if strings.Contains(r.pageText(), "Welcome to Paperweight") {
		t.Error("the welcome shows again once the draft has values")
	}
	r = b.get(setupCheck)
	if got := strings.TrimSpace(r.doc.Find(".step-intro").Text()); got != "Nothing is saved until you press Save company." {
		t.Errorf("check intro %q", got)
	}
	if !strings.Contains(r.pageText(), "Save company") {
		t.Errorf("the check step has no Save company: %q", r.pageText())
	}
}

// No case ID: A-8. A GSTIN on step 1 echoes its state and PAN under the field,
// and State code waits in a closed More details. The live echo is the browser
// test's (e2e TestE2E_SetupWizardPhone).
func TestSetupWizard_A8_GSTINEcho(t *testing.T) {
	w := newWorld(t)
	w.seedUsers()
	b := w.signedIn(adminEmail)
	form := goodSetup()
	form.Set("state_code", "")
	b.toCheck(form)
	r := b.get(setupStep1)
	echo := strings.Join(strings.Fields(r.doc.Find("#f-gstin-msg").Text()), " ")
	for _, want := range []string{"Maharashtra (27)", "PAN AAFCV2449G"} {
		if !strings.Contains(echo, want) {
			t.Errorf("GSTIN echo %q, want %q", echo, want)
		}
	}
	d := r.doc.Find("#wizard-form details.disclosure")
	if d.Length() != 1 || d.Find(`[name="state_code"]`).Length() != 1 || d.Find(`[name="pan"]`).Length() != 1 {
		t.Fatalf("%d More details; State code and PAN inside: %d, %d", d.Length(), d.Find(`[name="state_code"]`).Length(), d.Find(`[name="pan"]`).Length())
	}
	if _, open := d.Attr("open"); open {
		t.Error("More details is open with State code and PAN blank")
	}
	if got := strings.TrimSpace(d.Find("summary").Text()); !strings.Contains(got, "More details · state code, PAN") {
		t.Errorf("More details summary %q", got)
	}
}

// No case ID: each step's validation (CD-165 texts, the address's own texts,
// CD-337 (7)). A refused step is 422 with the service's text on its field and
// every typed value kept; nothing is written.
func TestSetupWizard_StepValidation(t *testing.T) {
	w := newWorld(t)
	admin := w.seedUsers()
	b := w.signedIn(adminEmail)
	good := goodSetup()

	serviceMsgs := func(in company.SetupInput) []string {
		t.Helper()
		return w.refusal(func(tx *db.Tx) error {
			return w.app.Masters().Company.SetupTx(context.Background(), tx, admin, in)
		})
	}
	goodIn := company.SetupInput{LegalName: good.Get("legal_name"), StateCode: "27", GSTIN: good.Get("gstin"), BranchCode: "PUNE", BranchName: "Pune Works"}
	noLegal, badGSTIN, noCode := goodIn, goodIn, goodIn
	noLegal.LegalName = ""
	badGSTIN.GSTIN = "27AABCS1429B1Z5"
	noCode.BranchCode = ""

	cases := []struct {
		name, path, field, value string
		step                     int
		want                     []string
	}{
		{"blank legal name", setupStep1, "legal_name", "", 0, serviceMsgs(noLegal)},
		{"bad gstin", setupStep1, "gstin", "27AABCS1429B1Z5", 0, serviceMsgs(badGSTIN)},
		{"no gstin and no state", setupStep1, "gstin", "", 0, nil},
		{"blank head office code", setupStep2, "branch_code", "", 1, serviceMsgs(noCode)},
		{"blank address line 1", setupStep2, "address_line1", "", 1, []string{"Address line 1 is required."}},
		{"blank city", setupStep2, "address_city", "", 1, []string{"City is required."}},
		{"pincode out of state", setupStep2, "address_pincode", "110001", 1, []string{"Pincode 110001 is not in Maharashtra. Check the pincode or the state."}},
		{"bad pincode", setupStep2, "address_pincode", "01234", 1, []string{"Pincode must be 6 digits and can't start with 0."}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.step == 1 {
				if r := b.post(setupStep1, stepForm(good, 0)); r.status != http.StatusSeeOther {
					t.Fatalf("POST step 1 = %d (%q)", r.status, r.pageText())
				}
			}
			form := stepForm(good, c.step)
			form.Set(c.field, c.value)
			msgField := c.field
			if c.name == "no gstin and no state" {
				// CD-165: no GSTIN needs a state and a PAN.
				form.Set("pan", "")
				form.Set("state_code", "")
				c.want = serviceMsgs(company.SetupInput{LegalName: goodIn.LegalName, BranchCode: "PUNE"})
				msgField = "state_code"
			}
			r := b.post(c.path, form)
			if r.status != http.StatusUnprocessableEntity {
				t.Fatalf("POST %s = %d to %q, want 422", c.path, r.status, r.location())
			}
			if len(c.want) == 0 {
				t.Fatal("no expected message")
			}
			for _, m := range c.want {
				if !strings.Contains(r.pageText(), m) {
					t.Errorf("page text %q, want %q", r.pageText(), m)
				}
			}
			if got := strings.Join(strings.Fields(r.doc.Find("#f-"+msgField+"-msg").Text()), " "); !strings.Contains(got, c.want[0]) {
				t.Errorf("%s message = %q, want %q", msgField, got, c.want[0])
			}
			if c.name == "bad gstin" && strings.TrimSpace(r.doc.Find("#f-pan-msg").Text()) != "" {
				t.Errorf("a bad GSTIN also shows a PAN error %q: it gives no PAN", r.doc.Find("#f-pan-msg").Text())
			}
			if r.doc.Find(".error-summary").Length() != 1 {
				t.Error("no error summary")
			}
			for name := range form {
				if got := valueOf(r, name); got != form.Get(name) {
					t.Errorf("%s after the refusal = %q, want the typed %q", name, got, form.Get(name))
				}
			}
			if n := setupRows(w); n != 0 {
				t.Errorf("%d rows written by a step, want none", n)
			}
		})
	}

	t.Run("pan from the gstin is enough", func(t *testing.T) {
		form := stepForm(good, 0)
		form.Set("pan", "")
		if r := b.post(setupStep1, form); r.status != http.StatusSeeOther || r.location() != setupStep2 {
			t.Errorf("POST step 1 with a GSTIN and no PAN = %d to %q (%q), want 303 to step 2", r.status, r.location(), r.pageText())
		}
	})
	t.Run("a refused step 1 waits for no later field", func(t *testing.T) {
		r := b.post(setupStep1, url.Values{"legal_name": {""}, "gstin": {good.Get("gstin")}})
		for _, name := range setupStepFields[1] {
			if r.doc.Find("#f-"+name+"-msg").Length() != 0 || strings.Contains(r.pageText(), "Address line 1 is required.") {
				t.Errorf("step 1 shows step 2's %s", name)
			}
		}
	})
}

// No case ID: Back saves without checking, Continue and Back keep every typed
// value, a step can't be skipped to, and Change on Check comes back to Check.
func TestSetupWizard_BackAndForwardKeepValues(t *testing.T) {
	w := newWorld(t)
	w.seedUsers()
	b := w.signedIn(adminEmail)
	good := goodSetup()

	for _, path := range []string{setupStep2, setupCheck} {
		if r := b.get(path); r.status != http.StatusSeeOther || r.location() != setupStep1 {
			t.Errorf("GET %s with no draft = %d to %q, want 303 to step 1", path, r.status, r.location())
		}
	}
	if r := b.post(setupCheck, url.Values{}); r.status != http.StatusSeeOther || r.location() != setupStep1 {
		t.Errorf("POST Check with no draft = %d to %q, want 303 to step 1", r.status, r.location())
	}
	if n := setupRows(w); n != 0 {
		t.Fatalf("%d rows after a skipped-to Check, want none", n)
	}

	if r := b.post(setupStep1, stepForm(good, 0)); r.status != http.StatusSeeOther || r.location() != setupStep2 {
		t.Fatalf("POST step 1 = %d to %q (%q)", r.status, r.location(), r.pageText())
	}
	// Back from step 2 with a half-typed, refusable step keeps it unchecked.
	half := url.Values{"branch_code": {"PUNE"}, "address_pincode": {"01"}, "nav": {"back"}}
	if r := b.post(setupStep2, half); r.status != http.StatusSeeOther || r.location() != setupStep1 {
		t.Fatalf("Back from step 2 = %d to %q (%q), want 303 to step 1", r.status, r.location(), r.pageText())
	}
	r := b.get(setupStep1)
	for _, name := range setupStepFields[0] {
		if got := valueOf(r, name); got != good.Get(name) {
			t.Errorf("step 1 %s after Back = %q, want %q", name, got, good.Get(name))
		}
	}
	if r.doc.Find(".stepper__step a[href=\""+setupStep2+"\"]").Length() != 1 {
		t.Error("the stepper doesn't link forward to the reached Head office step")
	}
	if r := b.post(setupStep1, stepForm(good, 0)); r.status != http.StatusSeeOther || r.location() != setupStep2 {
		t.Fatalf("Continue on step 1 = %d to %q", r.status, r.location())
	}
	r = b.get(setupStep2)
	if got := valueOf(r, "branch_code"); got != "PUNE" {
		t.Errorf("branch_code after Back and Continue = %q, want PUNE", got)
	}
	if got := valueOf(r, "address_pincode"); got != "01" {
		t.Errorf("address_pincode after Back and Continue = %q, want the typed 01", got)
	}
	if r := b.get(setupCheck); r.status != http.StatusSeeOther || r.location() != setupStep2 {
		t.Errorf("GET Check before step 2 passes = %d to %q, want 303 to step 2", r.status, r.location())
	}
	if r := b.post(setupStep2, stepForm(good, 1)); r.status != http.StatusSeeOther || r.location() != setupCheck {
		t.Fatalf("Continue on step 2 = %d to %q (%q)", r.status, r.location(), r.pageText())
	}

	// Check shows what will be saved, with Change on each step.
	r = b.get(setupCheck)
	text := r.pageText()
	for _, want := range []string{"Vardhan Infra Projects Pvt Ltd", "27AAFCV2449G1Z7", "Maharashtra (27)", "AAFCV2449G", "PUNE", "Pune Works", "Plot 12, MIDC Bhosari", "Pune", "411026"} {
		if !strings.Contains(text, want) {
			t.Errorf("Check doesn't show %q: %q", want, text)
		}
	}
	pairs := map[string]string{}
	r.doc.Find(".summary-row").Each(func(_ int, row *goquery.Selection) {
		pairs[strings.TrimSpace(row.Find("dt").Text())] = strings.TrimSpace(row.Find("dd").Text())
	})
	if pairs["PAN"] != "AAFCV2449G" || pairs["State"] != "Maharashtra (27)" {
		t.Errorf("Check's PAN %q and State %q, want those the GSTIN gives", pairs["PAN"], pairs["State"])
	}
	for _, href := range []string{setupStep1 + "?return=check", setupStep2 + "?return=check"} {
		if r.doc.Find(`a[href="`+href+`"]`).Length() != 1 {
			t.Errorf("Check has no Change link to %s", href)
		}
	}
	// UI v2 C4b (§5.6): each card's icon tile.
	for title, icon := range map[string]string{"Company": "building-2", "Head office": "map-pin"} {
		sec := r.doc.Find("section.card").FilterFunction(func(_ int, s *goquery.Selection) bool {
			return strings.TrimSpace(s.Find(".card-title").Text()) == title
		})
		got, _ := sec.Find(".card-head .card-icon svg").Html()
		if want, _ := uitest.Render(t, icons.Icon(icon)).Find("svg").Html(); sec.Length() != 1 || got != want {
			t.Errorf("Check's %s card (%d) tile = %q, want the %s icon", title, sec.Length(), got, icon)
		}
	}
	// Back from Check goes to step 2 and writes nothing.
	if r := b.post(setupCheck, url.Values{"nav": {"back"}}); r.status != http.StatusSeeOther || r.location() != setupStep2 {
		t.Errorf("Back from Check = %d to %q, want 303 to step 2", r.status, r.location())
	}
	// Change comes back to Check.
	r = b.get(setupStep1 + "?return=check")
	if r.doc.Find(`input[name="return"][value="check"]`).Length() != 1 {
		t.Fatal("step 1 opened from Change doesn't carry return=check")
	}
	form := stepForm(good, 0)
	form.Set("legal_name", "Vardhan Infra Pvt Ltd")
	form.Set("return", "check")
	if r := b.post(setupStep1, form); r.status != http.StatusSeeOther || r.location() != setupCheck {
		t.Errorf("Continue from Change = %d to %q, want 303 to Check", r.status, r.location())
	}
	if !strings.Contains(b.get(setupCheck).pageText(), "Vardhan Infra Pvt Ltd") {
		t.Error("Check doesn't show the changed legal name")
	}
	if n := setupRows(w); n != 0 {
		t.Errorf("%d rows before Save company, want none", n)
	}
}

// No case ID: htmx posts answer as the master wizards do: the next step's
// fragment with HX-Push-Url, a 422 fragment, and HX-Redirect after the save.
func TestSetupWizard_HTMX(t *testing.T) {
	w := newWorld(t)
	w.seedUsers()
	b := w.signedIn(adminEmail)
	hx := map[string]string{"HX-Request": "true"}
	good := goodSetup()

	bad := stepForm(good, 0)
	bad.Set("legal_name", "")
	r := b.postHeaders(setupStep1, bad, hx)
	if r.status != http.StatusUnprocessableEntity || r.doc.Find("#wizard").Length() != 1 || r.doc.Find("main#main").Length() != 0 {
		t.Errorf("htmx refusal = %d, want 422 and the #wizard fragment", r.status)
	}
	// Each fragment's <title> updates the document title (R2).
	if got := r.doc.Find("title").Text(); !strings.HasPrefix(got, "Error: Company · ") {
		t.Errorf("htmx refusal title = %q, want Error: Company · …", got)
	}
	r = b.postHeaders(setupStep1, stepForm(good, 0), hx)
	if r.status != http.StatusOK || r.header.Get("HX-Push-Url") != setupStep2 || r.doc.Find(`#wizard [name="branch_code"]`).Length() != 1 {
		t.Errorf("htmx step 1 = %d, push %q; want 200, %s and step 2's fields", r.status, r.header.Get("HX-Push-Url"), setupStep2)
	}
	if got := r.doc.Find("title").Text(); !strings.HasPrefix(got, "Head office · ") {
		t.Errorf("htmx step 1 title = %q, want step 2's", got)
	}
	r = b.postHeaders(setupStep2, stepForm(good, 1), hx)
	if r.status != http.StatusOK || r.header.Get("HX-Push-Url") != setupCheck {
		t.Errorf("htmx step 2 = %d, push %q; want 200, %s", r.status, r.header.Get("HX-Push-Url"), setupCheck)
	}
	r = b.postHeaders(setupCheck, url.Values{}, hx)
	if r.status != http.StatusOK || r.header.Get("HX-Redirect") != "/" {
		t.Errorf("htmx save = %d, HX-Redirect %q; want 200, /", r.status, r.header.Get("HX-Redirect"))
	}
	if n := w.count(`SELECT count(*) FROM company`); n != 1 {
		t.Errorf("company rows = %d, want 1", n)
	}
}

// No case ID: CD-165 and CD-337 (7). Save company writes the company, the head
// office and its address in one write, shows the next step, and closes setup.
func TestSetupWizard_SaveWritesAll(t *testing.T) {
	w := newWorld(t)
	w.seedUsers()
	b := w.signedIn(adminEmail)
	r := b.setupCompany(goodSetup())
	if r.status != http.StatusSeeOther || r.location() != "/" {
		t.Fatalf("Save company = %d to %q (%q), want 303 to /", r.status, r.location(), r.pageText())
	}
	if got := w.text(`SELECT name || '/' || pan FROM company`); got != "Vardhan Infra Projects Pvt Ltd/AAFCV2449G" {
		t.Errorf("company = %q", got)
	}
	if got := w.text(`SELECT code || '/' || name || '/' || state_code || '/' || gstin || '/' || is_head_office FROM branch`); got != "PUNE/Pune Works/27/27AAFCV2449G1Z7/1" {
		t.Errorf("branch = %q", got)
	}
	got := w.text(`SELECT a.line1 || '/' || a.city || '/' || a.pincode || '/' || a.state_code || '/' || a.is_billing || a.is_primary_billing
		FROM address a JOIN branch b ON b.id = a.branch_id WHERE a.party_id IS NULL`)
	if got != "Plot 12, MIDC Bhosari/Pune/411026/27/11" {
		t.Errorf("head office address = %q", got)
	}
	r = b.get("/approvals")
	if !strings.Contains(r.pageText(), "Your company is set up. Next, add your users.") {
		t.Errorf("no success banner after the save: %q", r.pageText())
	}
	for _, path := range []string{"/setup/company", setupStep1, setupStep2, setupCheck} {
		if r := b.get(path); r.status != http.StatusNotFound {
			t.Errorf("GET %s after the save = %d, want 404", path, r.status)
		}
		if r := b.post(path, goodSetup()); r.status != http.StatusNotFound {
			t.Errorf("POST %s after the save = %d, want 404", path, r.status)
		}
	}
	if r := b.get("/setup/company/nope"); r.status != http.StatusNotFound {
		t.Errorf("GET an unknown step = %d, want 404", r.status)
	}
}

// No case ID: the final write is one transaction. A refusal after the company
// row went in (MST-023: a typed PAN the GSTIN disagrees with) and a failure
// at the address's insert both leave nothing, and the draft stays.
func TestSetupWizard_SaveIsAtomic(t *testing.T) {
	w := newWorld(t)
	admin := w.seedUsers()
	b := w.signedIn(adminEmail)

	t.Run("refused after the company row", func(t *testing.T) {
		form := goodSetup()
		form.Set("pan", "AABCS1429B")
		in := company.SetupInput{LegalName: form.Get("legal_name"), GSTIN: form.Get("gstin"), PAN: "AABCS1429B", BranchCode: "PUNE"}
		msgs := w.refusal(func(tx *db.Tx) error { return w.app.Masters().Company.SetupTx(context.Background(), tx, admin, in) })
		b.toCheck(form)
		r := b.post(setupCheck, url.Values{})
		if r.status != http.StatusUnprocessableEntity {
			t.Fatalf("Save company = %d (%q), want 422", r.status, r.pageText())
		}
		if got := strings.Join(strings.Fields(r.doc.Find("#f-gstin-msg").Text()), " "); !strings.Contains(got, msgs[0]) {
			t.Errorf("GSTIN message = %q, want the service's %q", got, msgs[0])
		}
		if r.doc.Find(`input[name="return"][value="check"]`).Length() != 1 {
			t.Error("the refused step doesn't return to Check")
		}
		if n := setupRows(w); n != 0 {
			t.Errorf("%d rows after the refused save, want none", n)
		}
		if got := valueOf(b.get(setupStep1), "pan"); got != "AABCS1429B" {
			t.Errorf("the draft's PAN after the refusal = %q, want it kept", got)
		}
		form.Set("pan", "")
		b.toCheck(form)
	})

	t.Run("failure at the address", func(t *testing.T) {
		ctx := context.Background()
		if err := w.app.DB().Write(ctx, func(tx *db.Tx) error {
			_, err := tx.SQL().ExecContext(ctx, `CREATE TRIGGER uiv2e2_no_address BEFORE INSERT ON address BEGIN SELECT RAISE(ABORT, 'no address'); END`)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		r := b.post(setupCheck, url.Values{})
		if r.status != http.StatusInternalServerError {
			t.Errorf("Save company with the address insert failing = %d, want 500", r.status)
		}
		if n := setupRows(w); n != 0 {
			t.Errorf("%d rows after the failed save, want none", n)
		}
		if err := w.app.DB().Write(ctx, func(tx *db.Tx) error {
			_, err := tx.SQL().ExecContext(ctx, `DROP TRIGGER uiv2e2_no_address`)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if r := b.post(setupCheck, url.Values{}); r.status != http.StatusSeeOther {
			t.Fatalf("Save company once the address can go in = %d (%q), want 303: the draft is kept", r.status, r.pageText())
		}
		if n := w.count(`SELECT count(*) FROM address`); n != 1 {
			t.Errorf("addresses = %d, want 1", n)
		}
	})
}

// No case ID: CD-165. Only an administrator runs setup, on every step; a
// second administrator on Check when the first saves is refused as today.
func TestSetupWizard_AdminsOnly(t *testing.T) {
	w := newWorld(t)
	admin := w.seedUsers()
	if _, _, err := w.app.Auth().CreateUser(context.Background(), admin, auth.NewUser{Email: "second.admin@vardhan.test", Name: "Second Admin", Password: testPassword, Roles: []string{auth.RoleAdministrator}}); err != nil {
		t.Fatal(err)
	}
	p := w.signedIn(priyaEmail)
	for _, path := range []string{"/setup/company", setupStep1, setupStep2, setupCheck} {
		if r := p.get(path); r.status != http.StatusForbidden || r.problem() != "Only an administrator can set up the company." {
			t.Errorf("GET %s as Priya = %d %q, want 403 and the administrator text", path, r.status, r.problem())
		}
		if r := p.post(path, goodSetup()); r.status != http.StatusForbidden {
			t.Errorf("POST %s as Priya = %d, want 403", path, r.status)
		}
	}

	first, second := w.signedIn(adminEmail), w.signedIn("second.admin@vardhan.test")
	first.toCheck(goodSetup())
	other := goodSetup()
	other.Set("branch_code", "HO")
	second.toCheck(other)
	if r := first.post(setupCheck, url.Values{}); r.status != http.StatusSeeOther {
		t.Fatalf("first admin's save = %d (%q), want 303", r.status, r.pageText())
	}
	if r := second.post(setupCheck, url.Values{}); r.status != http.StatusNotFound {
		t.Errorf("second admin's save after the first = %d (%q), want 404", r.status, r.pageText())
	}
	if got := w.text(`SELECT group_concat(code) FROM branch`); got != "PUNE" {
		t.Errorf("branches = %q, want only the first admin's PUNE", got)
	}
}

// No case ID (E2 review 1): Back stores a step unchecked, so Save company
// checks the whole draft first. An over-long head office name typed before
// Back is refused on step 2 under its own field, never as SetupTx's "name",
// which the form would show under Legal name.
func TestSetupWizard_SaveChecksDraftBackedOut(t *testing.T) {
	w := newWorld(t)
	w.seedUsers()
	b := w.signedIn(adminEmail)
	good := goodSetup()
	b.toCheck(good)

	long := strings.Repeat("x", mstkit.MaxNameLen+1)
	_, err := company.ValidateBranch(company.BranchInput{Code: "PUNE", Name: long, StateCode: "27", IsHeadOffice: true}, "")
	var refused doctype.HTTPError
	if !errors.As(err, &refused) || len(refused.FieldErrors()) != 1 {
		t.Fatalf("ValidateBranch with a long name = %v, want one field error", err)
	}
	want := refused.FieldErrors()[0].Message

	form := stepForm(good, 1)
	form.Set("branch_name", long)
	form.Set("nav", "back")
	if r := b.post(setupStep2, form); r.status != http.StatusSeeOther || r.location() != setupStep1 {
		t.Fatalf("Back from step 2 = %d to %q, want 303 to step 1", r.status, r.location())
	}
	r := b.post(setupCheck, url.Values{})
	if r.status != http.StatusUnprocessableEntity {
		t.Fatalf("Save company = %d (%q), want 422", r.status, r.pageText())
	}
	if got := r.header.Get("HX-Push-Url"); got != setupStep2 {
		t.Errorf("refused save shows %q, want step 2", got)
	}
	if got := strings.Join(strings.Fields(r.doc.Find("#f-branch_name-msg").Text()), " "); got != want {
		t.Errorf("head office name message = %q, want %q", got, want)
	}
	if r.doc.Find("#f-legal_name").Length() != 0 {
		t.Error("the refusal is shown on step 1")
	}
	if n := setupRows(w); n != 0 {
		t.Errorf("%d rows after the refused save, want none", n)
	}
}

// No case ID (E2 review 2): return=check skips to Check only once every
// step before it has passed. Continue on step 1 from a Change link, while
// step 2 never passed, shows step 2.
func TestSetupWizard_ReturnToCheckNeedsEveryStep(t *testing.T) {
	w := newWorld(t)
	w.seedUsers()
	b := w.signedIn(adminEmail)
	form := stepForm(goodSetup(), 0)
	form.Set("return", "check")
	r := b.postHeaders(setupStep1, form, map[string]string{"HX-Request": "true"})
	if r.status != http.StatusOK || r.header.Get("HX-Push-Url") != setupStep2 {
		t.Fatalf("htmx Continue with return=check = %d, push %q; want 200, %s", r.status, r.header.Get("HX-Push-Url"), setupStep2)
	}
	if r.doc.Find(`#wizard [name="branch_code"]`).Length() != 1 || strings.Contains(r.pageText(), "Save company") {
		t.Errorf("the fragment isn't step 2: %q", r.pageText())
	}
}

// No case ID (E2 review 3): a successful save clears the draft. With the
// saved rows removed, setup opens fresh: the welcome shows and Check
// sends the browser back to step 1.
func TestSetupWizard_SaveClearsDraft(t *testing.T) {
	w := newWorld(t)
	w.seedUsers()
	b := w.signedIn(adminEmail)
	if r := b.setupCompany(goodSetup()); r.status != http.StatusSeeOther {
		t.Fatalf("Save company = %d (%q), want 303", r.status, r.pageText())
	}
	ctx := context.Background()
	if err := w.app.DB().Write(ctx, func(tx *db.Tx) error {
		for _, q := range []string{`DELETE FROM address`, `DELETE FROM branch`, `DELETE FROM company`} {
			if _, err := tx.SQL().ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if r := b.get(setupCheck); r.status != http.StatusSeeOther || r.location() != setupStep1 {
		t.Errorf("GET Check after the save = %d to %q, want 303 to step 1: the draft was kept", r.status, r.location())
	}
	r := b.get(setupStep1)
	if !strings.Contains(r.pageText(), "Welcome to Paperweight") || valueOf(r, "legal_name") != "" {
		t.Errorf("step 1 after the save shows the old draft: legal name %q", valueOf(r, "legal_name"))
	}
}
