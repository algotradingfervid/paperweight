package app_test

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// MOD-EXP S1 · Claim wizard (UX §5.3, §5.4, §5.6) on the real app: Claim
// details, Expenses and Check, through the signed-in pages.

const (
	s1Claims   = "/doc/" + expense.ClaimDocType
	s1Date     = "2026-10-24"
	s1PaidOnly = "Only advances marked as paid are listed. Ask the Director if yours is missing."
	s1Kavya    = "kavya@vardhan.test"
)

// s1World is a canonical world with the expense approval rules (reporting
// manager, Fallback Approver), as expensetest and the e2e harness set them.
type s1World struct {
	*world
	admin auth.Principal
}

func s1New(t *testing.T) *s1World {
	t.Helper()
	w := newWorld(t)
	admin, _, _ := w.canonical()
	ctx := auth.WithPrincipal(context.Background(), admin)
	for _, dt := range []string{expense.ClaimDocType, expense.AdvanceDocType} {
		if _, err := w.app.Approvals().CreateRule(ctx, admin, approval.RuleInput{
			DocType: dt, ApproverType: approval.ApproverManager, FallbackRole: expense.RoleFallbackApprover,
		}); err != nil {
			t.Fatalf("approval rule for %s: %v", dt, err)
		}
	}
	return &s1World{world: w, admin: admin}
}

func (s *s1World) do(p auth.Principal, dt string, id int64, a doctype.Action, in doc.ActionInput) doc.Result {
	s.t.Helper()
	res, err := s.app.Engine().Do(context.Background(), p, dt, id, a, in)
	if err != nil {
		var he doctype.HTTPError
		if errors.As(err, &he) {
			s.t.Fatalf("%s %s %d as %s: %v %+v", a, dt, id, p.Name, err, he.FieldErrors())
		}
		s.t.Fatalf("%s %s %d as %s: %v", a, dt, id, p.Name, err)
	}
	return res
}

func (s *s1World) id(q string, args ...any) string {
	s.t.Helper()
	return s.text(q, args...)
}

func (s *s1World) travel() string {
	return s.id(`SELECT CAST(id AS TEXT) FROM expense_type WHERE code = 'TRAVEL'`)
}

// advance is an approved advance of amount for p, approved by approver,
// marked as paid when paid is set.
func (s *s1World) advance(p, approver auth.Principal, amount string, paid bool) int64 {
	s.t.Helper()
	r := s.do(p, expense.AdvanceDocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"advance_date": s1Date, "purpose": "Pune Metro site visit"},
		Lines:  []map[string]string{{"expense_type_id": s.travel(), "line_amount": amount}},
	})
	r = s.do(p, expense.AdvanceDocType, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version})
	s.do(approver, expense.AdvanceDocType, r.ID, doctype.ActionApprove, doc.ActionInput{Version: r.Version})
	if paid {
		s.exec(`UPDATE employee_advance SET paid_on = advance_date WHERE id = ?`, r.ID)
	}
	return r.ID
}

// claim saves a draft claim as p; each line defaults to TRAVEL ₹1,000.00 on
// the business date with a no-receipt reason.
func (s *s1World) claim(p auth.Principal, header map[string]string, lines ...map[string]string) doc.Result {
	s.t.Helper()
	in := doc.ActionInput{Values: map[string]string{"claim_date": s1Date, "purpose": "Pune Metro site visit"}}
	maps.Copy(in.Values, header)
	for _, l := range lines {
		m := map[string]string{"expense_date": s1Date, "expense_type_id": s.travel(), "amount": "1000.00", "no_receipt_reason": "Paid in cash."}
		maps.Copy(m, l)
		in.Lines = append(in.Lines, m)
	}
	return s.do(p, expense.ClaimDocType, 0, doctype.ActionSave, in)
}

func (s *s1World) exec(q string, args ...any) {
	s.t.Helper()
	if _, err := s.app.DB().SQL().ExecContext(context.Background(), q, args...); err != nil {
		s.t.Fatalf("%s: %v", q, err)
	}
}

// attach attaches content to claim id as p and returns its stored Meta.
func (s *s1World) attach(p auth.Principal, id int64, name string, content []byte) files.Meta {
	s.t.Helper()
	fs := s.app.Files()
	sp, err := fs.Spool(bytes.NewReader(content), files.MaxFileBytes)
	if err != nil {
		s.t.Fatal(err)
	}
	defer func() { _ = sp.Cleanup() }()
	var m files.Meta
	err = s.app.DB().Write(context.Background(), func(tx *db.Tx) error {
		aid, err := fs.Attach(context.Background(), tx, p, auth.RecordRef{DocType: expense.ClaimDocType, ID: id}, name, sp)
		if err != nil {
			return err
		}
		m, err = fs.Meta(context.Background(), tx, aid)
		return err
	})
	if err != nil {
		s.t.Fatalf("attach %s to claim %d: %v", name, id, err)
	}
	return m
}

// setLines saves claim id as p with each stored line's values overridden
// by over[line no].
func (s *s1World) setLines(p auth.Principal, id int64, over map[int]map[string]string, extra ...map[string]string) doc.Result {
	s.t.Helper()
	var rec doc.Record
	if err := s.app.DB().Read(context.Background(), func(tx *db.Tx) error {
		var err error
		rec, err = s.app.Engine().Load(context.Background(), tx, expense.ClaimDocType, id)
		return err
	}); err != nil {
		s.t.Fatal(err)
	}
	in := doc.ActionInput{Version: rec.Version, Values: map[string]string{}}
	for i, l := range rec.Lines {
		m := maps.Clone(l)
		maps.Copy(m, over[i+1])
		in.Lines = append(in.Lines, m)
	}
	for _, l := range extra {
		m := map[string]string{"expense_date": s1Date, "expense_type_id": s.travel(), "amount": "1000.00", "no_receipt_reason": "Paid in cash."}
		maps.Copy(m, l)
		in.Lines = append(in.Lines, m)
	}
	return s.do(p, expense.ClaimDocType, id, doctype.ActionSave, in)
}

func s1URL(id int64, rest string) string { return s1Claims + "/" + strconv.FormatInt(id, 10) + rest }

// s1Page GETs path as b and wants 200.
func s1Page(t *testing.T, b *browser, path string) *response {
	t.Helper()
	r := b.get(path)
	if r.status != http.StatusOK {
		t.Fatalf("GET %s = %d (%q), want 200", path, r.status, r.pageText())
	}
	return r
}

func s1Has(t *testing.T, what, text string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(text, w) {
			t.Errorf("%s: no %q in %q", what, w, text)
		}
	}
}

func s1Lacks(t *testing.T, what, text string, unwanted ...string) {
	t.Helper()
	for _, u := range unwanted {
		if strings.Contains(text, u) {
			t.Errorf("%s: %q shown, want none", what, u)
		}
	}
}

// s1Options are the advance radios' visible texts, "*" before the checked one.
func s1Options(r *response) []string {
	var out []string
	r.doc.Find(`input[type="radio"][name="advance_id"]`).Each(func(_ int, in *goquery.Selection) {
		text := strings.Join(strings.Fields(in.Parent().Text()), " ")
		if _, ok := in.Attr("checked"); ok {
			text = "*" + text
		}
		out = append(out, text)
	})
	return out
}

// Step labels, the advance radios (EUX-008, EUX-043), the closed-advance
// hint, the restricted user's label (CD-312) and a hidden advance left out
// (CD-233).
func TestS1_ClaimDetailsAdvances(t *testing.T) {
	s := s1New(t)
	deepak, priya, ravi := s.principal(deepakID), s.principal(priyaID), s.principal(raviID)
	paid := s.advance(deepak, priya, "4000.00", true)    // EA/26-27/00001
	unpaid := s.advance(deepak, priya, "3000.00", false) // EA/26-27/00002
	dk := s.signedIn(deepakEmail)

	r := s1Page(t, dk, s1Claims+"/new")
	if got := strings.TrimSpace(r.doc.Find("h2#step-title").Text()); got != "Details" {
		t.Errorf("new claim heading = %q, want Details", got)
	}
	s1Has(t, "new claim", r.pageText(), "Expenses", "Set against an advance", s1PaidOnly)
	got := s1Options(r)
	want := []string{"*No advance", "EA/26-27/00001 · ₹4,000.00 left Pune Metro site visit · 24-10-2026 · advance ₹4,000.00"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("new claim radios = %q, want %q", got, want)
	}
	s1Lacks(t, "new claim", r.pageText(), "EA/26-27/00002")
	if r.doc.Find("details[open]").Length() != 0 {
		t.Error("the advance disclosure is open with no advance chosen")
	}
	if v := r.doc.Find("#f-claim_date").AttrOr("value", ""); v != "24-10-2026" {
		t.Errorf("new claim date = %q, want today 24-10-2026", v)
	}

	// Picking the advance on the drawn radios saves it.
	r = dk.post(s1Claims+"/new", url.Values{"purpose": {"Picked"}, "claim_date": {"24-10-2026"},
		"advance_drawn": {"1"}, "advance_id": {strconv.FormatInt(paid, 10)}, "less_advance": {"250.00"}})
	if r.status != http.StatusSeeOther {
		t.Fatalf("POST new with an advance = %d (%q), want 303", r.status, r.pageText())
	}
	if got := s.text(`SELECT COALESCE(advance_id, '-') || '/' || COALESCE(less_advance, '-') FROM expense_claim WHERE purpose = 'Picked'`); got != strconv.FormatInt(paid, 10)+"/25000" {
		t.Errorf("picked advance/less = %s, want %d/25000", got, paid)
	}
	if n := s1Page(t, dk, s1Claims+"/new").doc.Find(`#wizard-form input[type="hidden"][name="advance_drawn"][value="1"]`).Length(); n != 1 {
		t.Errorf("the drawn radios carry %d advance_drawn inputs in the step form, want 1", n)
	}

	// A chosen advance shows checked, the disclosure open.
	c := s.claim(deepak, map[string]string{"advance_id": strconv.FormatInt(paid, 10)}, nil)
	r = s1Page(t, dk, s1URL(c.ID, "/edit/details"))
	if got := s1Options(r); len(got) != 2 || !strings.HasPrefix(got[1], "*EA/26-27/00001") {
		t.Errorf("chosen advance radios = %q", got)
	}
	if r.doc.Find("details[open]").Length() != 1 {
		t.Error("the advance disclosure is closed with an advance chosen")
	}
	if v, ok := r.doc.Find(`input[name="less_advance"]`).Attr("value"); !ok || v != "" {
		t.Errorf("Less advance input = %q (present %v), want blank", v, ok)
	}

	// A crafted unpaid advance: submit returns to Details with the
	// message at Advance and the disclosure open; the radio has no left figure.
	u := s.claim(deepak, map[string]string{"advance_id": strconv.FormatInt(unpaid, 10)}, nil)
	r = dk.post(s1URL(u.ID, "/submit"), url.Values{"version": {strconv.Itoa(u.Version)}})
	s1Has(t, "unpaid submit", r.pageText(), "EA/26-27/00002 hasn't been marked as paid yet.")
	if r.doc.Find("details[open]").Length() != 1 {
		t.Error("unpaid submit: the advance disclosure is closed")
	}
	if got := s1Options(r); len(got) != 3 || got[2] != "*EA/26-27/00002" {
		t.Errorf("unpaid submit radios = %q, want the unpaid advance checked with no left figure", got)
	}
	if st := s.text(`SELECT state FROM expense_claim WHERE id = ?`, u.ID); st != "draft" {
		t.Errorf("unpaid submit: state %s, want draft", st)
	}

	// Only unpaid advances: the hint alone; none at all: no disclosure.
	suresh := s.principal(sureshID)
	s.advance(suresh, ravi, "1000.00", false)
	r = s1Page(t, s.signedIn(sureshEmail), s1Claims+"/new")
	s1Has(t, "unpaid only", r.pageText(), "Set against an advance", s1PaidOnly)
	if n := r.doc.Find(`input[name="advance_id"], input[name="less_advance"]`).Length(); n != 0 {
		t.Errorf("unpaid only: %d advance inputs, want none", n)
	}
	// Kavya's only advance is pending, not approved: no disclosure.
	kavya := s.principal(6)
	ka := s.do(kavya, expense.AdvanceDocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"advance_date": s1Date, "purpose": "Site visit"},
		Lines:  []map[string]string{{"expense_type_id": s.travel(), "line_amount": "700.00"}},
	})
	s.do(kavya, expense.AdvanceDocType, ka.ID, doctype.ActionSubmit, doc.ActionInput{Version: ka.Version})
	r = s1Page(t, s.signedIn(s1Kavya), s1Claims+"/new")
	s1Lacks(t, "no approved advance", r.pageText(), "Set against an advance", s1PaidOnly)

	// A closed advance stays checked with its hint.
	closing := s.advance(deepak, priya, "2000.00", true) // EA/26-27/00004
	cc := s.claim(deepak, map[string]string{"advance_id": strconv.FormatInt(closing, 10)}, nil)
	meena := s.principal(2)
	ver, _ := strconv.Atoi(s.text(`SELECT CAST(version AS TEXT) FROM employee_advance WHERE id = ?`, closing))
	s.do(meena, expense.AdvanceDocType, closing, doctype.ActionClose, doc.ActionInput{Version: ver, Reason: "Returned in cash."})
	r = s1Page(t, dk, s1URL(cc.ID, "/edit/details"))
	s1Has(t, "closed advance", r.pageText(), "This advance is closed. Choose another, or No advance.")
	if got := s1Options(r); len(got) == 0 || !strings.HasPrefix(got[len(got)-1], "*EA/26-27/00004") {
		t.Errorf("closed advance radios = %q, want it checked last", got)
	}

	// A cancelled advance stays checked with its hint.
	cancelling := s.advance(deepak, priya, "1500.00", false) // EA/26-27/00005
	cx := s.claim(deepak, map[string]string{"advance_id": strconv.FormatInt(cancelling, 10)}, nil)
	ver, _ = strconv.Atoi(s.text(`SELECT CAST(version AS TEXT) FROM employee_advance WHERE id = ?`, cancelling))
	s.do(meena, expense.AdvanceDocType, cancelling, doctype.ActionCancel, doc.ActionInput{Version: ver, Reason: "Not needed."})
	r = s1Page(t, dk, s1URL(cx.ID, "/edit/details"))
	s1Has(t, "cancelled advance", r.pageText(), "This advance is cancelled. Choose another, or No advance.")

	// A post that can't be parsed is a 422 that redraws what was typed,
	// a crafted advance naming nothing.
	r = dk.post(s1URL(cx.ID, "/edit/details"), url.Values{"version": {strconv.Itoa(s1Version(s, cx.ID))},
		"purpose": {"Typed purpose"}, "claim_date": {"24-10-2026"}, "advance_id": {"abc"}, "less_advance": {"x"}, "advance_drawn": {"1"}})
	if r.status != http.StatusUnprocessableEntity {
		t.Errorf("unparsable details post = %d, want 422", r.status)
	}
	if v := r.doc.Find(`input[name="purpose"]`).AttrOr("value", ""); v != "Typed purpose" {
		t.Errorf("422 purpose = %q, want the typed value", v)
	}
	if v := r.doc.Find(`input[name="less_advance"]`).AttrOr("value", ""); v != "x" {
		t.Errorf("422 Less advance = %q, want the typed x", v)
	}
	s1Has(t, "unparsable details post", r.pageText(), "Less advance")
	if r.doc.Find(`a[href="#f-less_advance"]`).Length() != 1 {
		t.Error("422: the summary doesn't link the Less advance parse error")
	}

	// Priya, restricted to PRJ-METRO, reads her own advance: its label shows
	// in the radios and the check page's Less advance (CD-312).
	pa := s.advance(priya, ravi, "500.00", true) // EA/26-27/00006
	pc := s.claim(priya, map[string]string{"advance_id": strconv.FormatInt(pa, 10)}, nil)
	pb := s.signedIn(priyaEmail)
	r = s1Page(t, pb, s1URL(pc.ID, "/edit/details"))
	if got := s1Options(r); len(got) != 2 || !strings.HasPrefix(got[1], "*EA/26-27/00006 · ₹500.00 left") {
		t.Errorf("restricted Priya's radios = %q, want her advance by number", got)
	}
	r = s1Page(t, pb, s1URL(pc.ID, "/edit/check"))
	s1Has(t, "restricted check", r.pageText(), "Less advance (EA/26-27/00006)")

	// A claim with a typed Less advance, saved before the viewer loses read.
	kept := s.claim(deepak, map[string]string{"advance_id": strconv.FormatInt(paid, 10), "less_advance": "500.00"}, nil)

	// An advance the viewer can't read is never named.
	s.exec(`DELETE FROM role_rights WHERE doctype = ? AND right_name = 'read'
	  AND role_id IN (SELECT role_id FROM user_roles WHERE user_id = ?)`, expense.AdvanceDocType, int64(deepakID))
	s.exec(`UPDATE auth_meta SET perm_version = perm_version + 1 WHERE id = 1`)
	r = s1Page(t, dk, s1URL(c.ID, "/edit/details"))
	s1Lacks(t, "hidden advance", r.pageText(), "EA/26-27/00001", "₹4,000.00 left")
	if n := r.doc.Find(`input[name="advance_id"]`).Length(); n != 0 {
		t.Errorf("hidden advance: %d advance radios, want none", n)
	}
	r = s1Page(t, dk, s1URL(c.ID, "/edit/check"))
	var labels []string
	r.doc.Find(".summary-row dt").Each(func(_ int, dt *goquery.Selection) {
		if l := strings.TrimSpace(dt.Text()); strings.HasPrefix(l, "Less advance") {
			labels = append(labels, l)
		}
	})
	if strings.Join(labels, "|") != "Less advance" {
		t.Errorf("hidden advance: check totals label %q, want exactly Less advance", labels)
	}

	// Saving the step without the radios never clears the stored advance or
	// Less advance: the viewer saw neither. The engine still refuses a save
	// of a draft linking an advance the saver can't read (CD-240).
	r = s1Page(t, dk, s1URL(kept.ID, "/edit/details"))
	if n := r.doc.Find(`input[name="advance_id"], input[name="less_advance"]`).Length(); n != 0 {
		t.Fatalf("hidden advance: %d advance inputs on the step, want none", n)
	}
	r = dk.post(s1URL(kept.ID, "/edit/details"), url.Values{"version": {strconv.Itoa(s1Version(s, kept.ID))},
		"purpose": {"Changed purpose"}, "claim_date": {"24-10-2026"}, "remarks": {""}})
	if r.status != http.StatusForbidden || !strings.Contains(r.pageText(), "You can't use this Employee Advance. It isn't available to you.") {
		t.Errorf("hidden advance save = %d (%q), want AUTH's link refusal", r.status, r.alert())
	}
	stored := s.text(`SELECT COALESCE(advance_id, '-') || '/' || COALESCE(less_advance, '-') FROM expense_claim WHERE id = ?`, kept.ID)
	if want := strconv.FormatInt(paid, 10) + "/50000"; stored != want {
		t.Errorf("after the save without radios: advance/less = %s, want %s", stored, want)
	}

	// Suresh can no longer read his unpaid advance: no disclosure, no hint.
	r = s1Page(t, s.signedIn(sureshEmail), s1Claims+"/new")
	s1Lacks(t, "unreadable unpaid", r.pageText(), "Set against an advance", s1PaidOnly)
}

// EUX-019 and the hidden employee: Anil's code input sets the employee
// (a leaver too); an unknown code is a 422 writing nothing; Deepak never
// sees the input, and his crafted employee_id is dropped.
func TestS1_ClaimDetailsEmployeeCode(t *testing.T) {
	s := s1New(t)
	ab := s.signedIn(adminEmail)
	r := s1Page(t, ab, s1Claims+"/new")
	if in := r.doc.Find(`input[name="employee_id"]`); in.Length() != 1 || in.AttrOr("value", "") != "" {
		t.Errorf("Anil's new claim: %d code inputs, value %q; want one, blank", in.Length(), in.AttrOr("value", ""))
	}
	r = ab.post(s1Claims+"/new", url.Values{"purpose": {"Own visit"}, "claim_date": {"24-10-2026"}, "employee_id": {" "}})
	if r.status != http.StatusSeeOther {
		t.Fatalf("POST new with a blank code = %d (%q), want 303", r.status, r.pageText())
	}
	if emp := s.text(`SELECT e.code FROM expense_claim c JOIN employee e ON e.id = c.employee_id WHERE c.id = (SELECT MAX(id) FROM expense_claim)`); emp != "EMP-001" {
		t.Errorf("Anil's blank-code claim employee = %s, want his own EMP-001", emp)
	}
	form := url.Values{"purpose": {"Site visit"}, "claim_date": {"24-10-2026"}, "employee_id": {"emp-008"}}
	r = ab.post(s1Claims+"/new", form)
	if r.status != http.StatusSeeOther {
		t.Fatalf("POST new with EMP-008 = %d (%q), want 303", r.status, r.pageText())
	}
	id := s.text(`SELECT CAST(MAX(id) AS TEXT) FROM expense_claim`)
	if emp := s.text(`SELECT e.code FROM expense_claim c JOIN employee e ON e.id = c.employee_id WHERE c.id = ?`, id); emp != "EMP-008" {
		t.Errorf("Anil's claim employee = %s, want EMP-008", emp)
	}
	n, _ := strconv.ParseInt(id, 10, 64)
	r = s1Page(t, ab, s1URL(n, "/edit/details"))
	if v := r.doc.Find(`input[name="employee_id"]`).AttrOr("value", ""); v != "EMP-008" {
		t.Errorf("reopened code = %q, want EMP-008", v)
	}
	s1Has(t, "admin details", r.pageText(), "Employee code",
		"Leave blank for your own claim. Type a code to enter a claim for someone else, including someone who has left.")

	before := s.count(`SELECT COUNT(*) FROM expense_claim`)
	form.Set("employee_id", "EMP-999")
	r = ab.post(s1Claims+"/new", form)
	if r.status != http.StatusUnprocessableEntity {
		t.Errorf("POST new with EMP-999 = %d, want 422", r.status)
	}
	s1Has(t, "unknown code", r.pageText(), "No employee has the code EMP-999.")
	if after := s.count(`SELECT COUNT(*) FROM expense_claim`); after != before {
		t.Errorf("unknown code wrote a claim: %d → %d", before, after)
	}
	// A numeric id is only a code that names no employee: the same 422.
	suresh := s.text(`SELECT CAST(id AS TEXT) FROM employee WHERE code = 'EMP-005'`)
	form.Set("employee_id", suresh)
	r = ab.post(s1Claims+"/new", form)
	if r.status != http.StatusUnprocessableEntity {
		t.Errorf("POST new with employee id %s = %d, want 422", suresh, r.status)
	}
	s1Has(t, "numeric id", r.pageText(), "No employee has the code "+suresh+".")
	if after := s.count(`SELECT COUNT(*) FROM expense_claim`); after != before {
		t.Errorf("a numeric id wrote a claim: %d → %d", before, after)
	}
	// The code is read from employee_id only: any other name is ignored.
	r = ab.post(s1Claims+"/new", url.Values{"purpose": {"Own visit"}, "claim_date": {"24-10-2026"},
		"employee_code": {"EMP-008"}, "employee": {"EMP-008"}, "code": {"EMP-008"}})
	if r.status != http.StatusSeeOther {
		t.Fatalf("POST new with the code under other names = %d (%q), want 303", r.status, r.pageText())
	}
	if emp := s.text(`SELECT e.code FROM expense_claim c JOIN employee e ON e.id = c.employee_id WHERE c.id = (SELECT MAX(id) FROM expense_claim)`); emp != "EMP-001" {
		t.Errorf("Anil's claim with the code under other names: employee = %s, want his own EMP-001", emp)
	}

	dk := s.signedIn(deepakEmail)
	r = s1Page(t, dk, s1Claims+"/new")
	if n := r.doc.Find(`input[name="employee_id"]`).Length(); n != 0 {
		t.Errorf("Deepak sees %d employee inputs, want none", n)
	}
	s1Lacks(t, "Deepak's details", r.pageText(), "Employee code")
	r = dk.post(s1Claims+"/new", url.Values{"purpose": {"Site visit"}, "claim_date": {"24-10-2026"},
		"employee_id": {suresh}})
	if r.status != http.StatusSeeOther {
		t.Fatalf("Deepak's POST new = %d (%q), want 303", r.status, r.pageText())
	}
	if emp := s.text(`SELECT e.code FROM expense_claim c JOIN employee e ON e.id = c.employee_id WHERE c.id = (SELECT MAX(id) FROM expense_claim)`); emp != "EMP-007" {
		t.Errorf("Deepak's crafted claim employee = %s, want his own EMP-007", emp)
	}
	// The check page lists the employee only to an Administrator.
	last, _ := strconv.ParseInt(s.text(`SELECT CAST(MAX(id) AS TEXT) FROM expense_claim`), 10, 64)
	s1Lacks(t, "Deepak's check page", s1Page(t, dk, s1URL(last, "/edit/check")).pageText(), "EmployeeEMP-007")
	s1Has(t, "Anil's check page", s1Page(t, ab, s1URL(last, "/edit/check")).pageText(), "EmployeeEMP-007")
}

// Expenses: line cards (thumbnail, type, date with the project code, amount,
// GST, note, receipt notes; UX §5.4, UX-E3), the line nouns and Add an
// expense above the cards (EUX-037), To pay as lines are added (EUX-038),
// the hidden fields; the same thumbnail on the approver's page.
func TestS1_ExpensesCards(t *testing.T) {
	s := s1New(t)
	deepak := s.principal(deepakID)
	metro := s.text(`SELECT CAST(id AS TEXT) FROM project WHERE code = 'PRJ-METRO'`)
	adv := s.advance(deepak, s.principal(priyaID), "4000.00", true)
	c := s.claim(deepak, map[string]string{"advance_id": strconv.FormatInt(adv, 10)},
		map[string]string{"amount": "450.00", "no_receipt_reason": "", "description": "Lunch at site canteen", "project_id": metro},
		map[string]string{"amount": "1.00"},
		map[string]string{"amount": "1.00", "no_receipt_reason": ""})
	dk := s.signedIn(deepakEmail)

	r := s1Page(t, dk, s1URL(c.ID, "/edit/items"))
	cards := r.doc.Find(".lines-card")
	if cards.Length() != 3 {
		t.Fatalf("%d cards, want 3", cards.Length())
	}
	first := strings.Join(strings.Fields(cards.First().Text()), " ")
	s1Has(t, "card 1", first, "Travel", "₹450.00", "24-10-2026 · PRJ-METRO", "Lunch at site canteen")
	if code := strings.TrimSpace(cards.First().Find(".lines-name .code").Text()); code != "" {
		t.Errorf("card 1 code = %q, want none", code)
	}
	s1Has(t, "card 2", cards.Eq(1).Text(), "No receipt: Paid in cash.")
	third := cards.Eq(2)
	s1Has(t, "card 3", third.Text(), "No receipt yet")
	if third.Find(".lines-receipt-note svg").Length() == 0 {
		t.Error("card 3: No receipt yet has no alert icon")
	}
	if n := strings.Count(r.pageText(), "Add an expense"); n != 1 {
		t.Errorf("Add an expense shows %d times with 3 lines, want 1", n)
	}
	add := r.doc.Find(`a:contains("Add an expense")`).First()
	if add.Length() == 0 || add.NextAllFiltered("ol.lines").Length() == 0 {
		t.Error("Add an expense isn't above the cards")
	}
	s1Has(t, "items totals", r.pageText(), "To pay", "₹0.00")
	s1Lacks(t, "items totals", r.pageText(), "Period from", "Period to")

	// A receipt: the card shows the claim's own file as a thumbnail.
	m := s.attach(deepak, c.ID, "bill.jpg", filestest.JPEG(1000))
	s.setLines(deepak, c.ID, map[int]map[string]string{1: {"receipt_sha256": m.SHA256}},
		map[string]string{"amount": "5998.00"})
	r = s1Page(t, dk, s1URL(c.ID, "/edit/items"))
	img := r.doc.Find(`.lines-card a[href="/files/` + strconv.FormatInt(m.ID, 10) + `"] img[loading="lazy"]`)
	if img.AttrOr("alt", "") != "Receipt for line 1" {
		t.Errorf("card 1 thumbnail alt = %q, want Receipt for line 1", img.AttrOr("alt", ""))
	}
	s1Has(t, "items totals", r.pageText(), "₹2,450.00")
	if n := strings.Count(r.pageText(), "Add an expense"); n != 2 {
		t.Errorf("Add an expense shows %d times with 4 lines, want 2", n)
	}

	// GST on a company bill; a PDF and a HEIC receipt are file chips.
	g := s.claim(deepak, nil,
		map[string]string{"amount": "4500.00", "company_bill": "true", "gst_amount": "482.14", "no_receipt_reason": ""},
		map[string]string{"no_receipt_reason": ""})
	pdf := s.attach(deepak, g.ID, "bill.pdf", filestest.PDF(900))
	heic := s.attach(deepak, g.ID, "bill.heic", filestest.HEIC("heic"))
	s.setLines(deepak, g.ID, map[int]map[string]string{1: {"receipt_sha256": pdf.SHA256}, 2: {"receipt_sha256": heic.SHA256}})
	r = s1Page(t, dk, s1URL(g.ID, "/edit/items"))
	gc := r.doc.Find(".lines-card")
	s1Has(t, "GST card", gc.First().Text(), "GST ₹482.14", "bill.pdf")
	if gc.First().Find(`a.attachments__chip[href="/files/`+strconv.FormatInt(pdf.ID, 10)+`"]`).Length() != 1 {
		t.Error("the PDF receipt isn't a file chip")
	}
	if gc.Eq(1).Find(`a.attachments__chip[download][href="/files/`+strconv.FormatInt(heic.ID, 10)+`"]`).Length() != 1 {
		t.Error("the HEIC receipt isn't a download chip")
	}
	s1Lacks(t, "plain card", gc.Eq(1).Text(), "GST")

	// Line pages: Expense {n}, Back reads Expenses, and Billed to only while
	// the company has two branch GSTINs. S2's line page posts the receipt
	// hash itself (UX §5 intro), so it isn't checked here.
	r = s1Page(t, dk, s1URL(c.ID, "/lines/1"))
	s1Has(t, "line page", r.doc.Find("h1").Text(), "Expense 1")
	if r.doc.Find(`[name="billed_to_id"]`).Length() == 0 {
		t.Error("line page has no Billed to with two branch GSTINs")
	}
	r = s1Page(t, dk, s1URL(c.ID, "/lines/1/remove"))
	s1Has(t, "remove page", r.pageText(), "Expenses")
	s.exec(`UPDATE branch SET disabled = 1 WHERE code = 'BLR'`)
	r = s1Page(t, dk, s1URL(c.ID, "/lines/1"))
	if r.doc.Find(`[name="billed_to_id"]`).Length() != 0 {
		t.Error("line page shows Billed to with one branch GSTIN")
	}

	// The approver's page shows the same thumbnail beside line 1.
	s.setLines(deepak, c.ID, map[int]map[string]string{3: {"no_receipt_reason": "Paper file"}})
	v, _ := strconv.Atoi(s.text(`SELECT CAST(version AS TEXT) FROM expense_claim WHERE id = ?`, c.ID))
	s.do(deepak, expense.ClaimDocType, c.ID, doctype.ActionSubmit, doc.ActionInput{Version: v})
	r = s1Page(t, s.signedIn(priyaEmail), s1URL(c.ID, ""))
	if r.doc.Find(`.lines-card a[href="/files/`+strconv.FormatInt(m.ID, 10)+`"] img[alt="Receipt for line 1"]`).Length() != 1 {
		t.Error("the approver's page has no thumbnail beside line 1")
	}
}

// Check (EUX-009, EUX-038): Total, Less advance with the advance
// and To pay after the grand figure; the preview; ClaimWarnings, a hidden
// claim never named (EUX-010, EUX-048); a typed Less advance over the total
// returns to Details; the approver lines (CD-266, EUX-013).
func TestS1_CheckTotalsAndWarnings(t *testing.T) {
	s := s1New(t)
	deepak, suresh := s.principal(deepakID), s.principal(sureshID)
	adv := s.advance(deepak, s.principal(priyaID), "4000.00", true)
	content := filestest.JPEG(1200)

	// Suresh's pending claim holds the same photo.
	sc := s.claim(suresh, nil, map[string]string{"no_receipt_reason": ""})
	sm := s.attach(suresh, sc.ID, "bill.jpg", content)
	sr := s.setLines(suresh, sc.ID, map[int]map[string]string{1: {"receipt_sha256": sm.SHA256}})
	s.do(suresh, expense.ClaimDocType, sc.ID, doctype.ActionSubmit, doc.ActionInput{Version: sr.Version})

	c := s.claim(deepak, map[string]string{"advance_id": strconv.FormatInt(adv, 10)},
		map[string]string{"amount": "450.00", "no_receipt_reason": ""},
		map[string]string{"amount": "6000.00"},
		map[string]string{"amount": "6000.00"})
	dm := s.attach(deepak, c.ID, "bill.jpg", content)
	s.setLines(deepak, c.ID, map[int]map[string]string{1: {"receipt_sha256": dm.SHA256}})
	dk := s.signedIn(deepakEmail)
	r := s1Page(t, dk, s1URL(c.ID, "/edit/check"))
	text := r.pageText()
	s1Has(t, "check page", text,
		"This expense claim needs approval from Priya Shah.",
		"Line 1: This receipt is also on another claim.",
		"Line 3: This looks the same as line 2.",
		"Less advance (EA/26-27/00001)", "−₹4,000.00", "To pay", "₹8,450.00",
	)
	s1Lacks(t, "check page", text, s.text(`SELECT COALESCE(NULLIF(number, ''), 'Draft '||id) FROM expense_claim WHERE id = ?`, sc.ID))
	// The totals come after the grand figure and its words (UX-E6); the page
	// text has no space between a label and its value.
	words := strings.Index(text, "₹12,450.00Rupees Twelve Thousand Four Hundred Fifty Only")
	totals := strings.Index(text, "Total₹12,450.00Less advance (EA/26-27/00001)−₹4,000.00To pay₹8,450.00")
	if words < 0 || totals < words {
		t.Errorf("totals order: words at %d, totals at %d; want the grand figure and words, then Total, Less advance, To pay", words, totals)
	}

	// Without an advance only the grand figure shows.
	plain := s.claim(deepak, nil, nil)
	r = s1Page(t, dk, s1URL(plain.ID, "/edit/check"))
	s1Lacks(t, "no advance", r.pageText(), "Less advance")

	// A typed Less advance over the total saves, and submit returns to Claim
	// details with the message at Less advance.
	over := s.claim(deepak, map[string]string{"advance_id": strconv.FormatInt(adv, 10), "less_advance": "3000.00"},
		map[string]string{"amount": "2000.00"})
	r = dk.post(s1URL(over.ID, "/submit"), url.Values{"version": {strconv.Itoa(over.Version)}})
	s1Has(t, "over submit", r.pageText(), "Less advance can't be more than the total.")
	if v := r.doc.Find(`input[name="less_advance"]`).AttrOr("value", ""); v != "3,000.00" {
		t.Errorf("over submit: Less advance input = %q, want 3,000.00", v)
	}
	if r.doc.Find("details[open]").Length() != 1 {
		t.Error("over submit: the advance disclosure is closed")
	}

	// The approver lines on the pending claim (CD-266).
	v, _ := strconv.Atoi(s.text(`SELECT CAST(version AS TEXT) FROM expense_claim WHERE id = ?`, c.ID))
	s.do(deepak, expense.ClaimDocType, c.ID, doctype.ActionSubmit, doc.ActionInput{Version: v})
	m19WantLine(t, "Priya", s1MainLines(t, s.signedIn(priyaEmail), c.ID),
		"Needs your approval: Deepak Nair's reporting manager, Any amount")
	s.exec(`INSERT INTO user_roles (user_id, role_id) SELECT 6, id FROM roles WHERE name_key = lower(?)`, expense.RoleFallbackApprover)
	s.exec(`UPDATE auth_meta SET perm_version = perm_version + 1 WHERE id = 1`)
	m19WantLine(t, "Kavya", s1MainLines(t, s.signedIn(s1Kavya), c.ID),
		"Needs your approval: Fallback Approver, Any amount")
}

// s1MainLines are the paragraphs of claim id's page main column, normalised.
func s1MainLines(t *testing.T, b *browser, id int64) []string {
	t.Helper()
	var lines []string
	s1Page(t, b, s1URL(id, "")).doc.Find(".doc-main > p").Each(func(_ int, s *goquery.Selection) {
		lines = append(lines, strings.Join(strings.Fields(s.Text()), " "))
	})
	return lines
}

// s1Version is claim id's stored version.
func s1Version(s *s1World, id int64) int {
	s.t.Helper()
	v, err := strconv.Atoi(s.text(`SELECT CAST(version AS TEXT) FROM expense_claim WHERE id = ?`, id))
	if err != nil {
		s.t.Fatal(err)
	}
	return v
}

// EUX-045: a company-bill card reads "GST ₹{gst} · Billed to {GSTIN}" from
// the line's snapshot while the company has two branch GSTINs, on the
// Expenses step and on Priya's approval page; with one, "GST ₹{gst}" only.
func TestS1_BilledToOnCard(t *testing.T) {
	s := s1New(t)
	deepak := s.principal(deepakID)
	pune := s.text(`SELECT CAST(id AS TEXT) FROM branch WHERE code = 'PUNE'`)
	c := s.claim(deepak, nil, map[string]string{
		"amount": "4500.00", "company_bill": "true", "gst_amount": "482.14", "supplier_gstin": "27AABCS1429B1ZU",
		"invoice_no": "INV-881", "billed_to_id": pune, "no_receipt_reason": "Paper file",
	})
	const billed = "GST ₹482.14 · Billed to 27AAFCV2449G1Z7"
	r := s1Page(t, s.signedIn(deepakEmail), s1URL(c.ID, "/edit/items"))
	s1Has(t, "Expenses card", r.doc.Find(".lines-card").First().Text(), billed)
	s.do(deepak, expense.ClaimDocType, c.ID, doctype.ActionSubmit, doc.ActionInput{Version: c.Version})
	pb := s.signedIn(priyaEmail)
	r = s1Page(t, pb, s1URL(c.ID, ""))
	s1Has(t, "approval page card", r.doc.Find(".lines-card").First().Text(), billed)

	s.exec(`UPDATE branch SET disabled = 1 WHERE code = 'BLR'`)
	card := s1Page(t, pb, s1URL(c.ID, "")).doc.Find(".lines-card").First().Text()
	s1Has(t, "one-GSTIN card", card, "GST ₹482.14")
	s1Lacks(t, "one-GSTIN card", card, "Billed to")
}

// EUX-011: files attached to the claim that no line holds get a warning and
// the card "Photos not on any expense" with Remove; removing them reloads
// the check page without the card. A line's receipt is never listed.
func TestS1_UnusedPhotos(t *testing.T) {
	s := s1New(t)
	deepak := s.principal(deepakID)
	c := s.claim(deepak, nil, map[string]string{"no_receipt_reason": ""})
	used := s.attach(deepak, c.ID, "used.jpg", filestest.JPEG(1100))
	s.setLines(deepak, c.ID, map[int]map[string]string{1: {"receipt_sha256": used.SHA256}})
	dk := s.signedIn(deepakEmail)
	check := s1URL(c.ID, "/edit/check")

	r := s1Page(t, dk, check)
	s1Lacks(t, "no unused photos", r.pageText(), "Photos not on any expense", "isn't on any expense")

	one := s.attach(deepak, c.ID, "IMG_2041.jpg", filestest.JPEG(1300))
	r = s1Page(t, dk, check)
	s1Has(t, "one unused", r.pageText(), "1 photo isn't on any expense. Remove it below, or use it on an expense.", "Photos not on any expense", "IMG_2041.jpg")
	// The line card names its receipt (CD-326 (6)): only the unused card's
	// list must leave used.jpg out.
	unused := r.doc.Find("section.card").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return strings.TrimSpace(s.Find("h2.card-title").Text()) == "Photos not on any expense"
	})
	if unused.Length() != 1 {
		t.Fatalf("one unused: %d Photos not on any expense cards, want 1", unused.Length())
	}
	s1Lacks(t, "one unused", unused.Text(), "used.jpg")
	if r.doc.Find(`button[aria-label="Remove IMG_2041.jpg"]`).Length() != 1 || r.doc.Find(`form[action="/files/`+strconv.FormatInt(one.ID, 10)+`/remove"]`).Length() != 1 {
		t.Error("one unused: no Remove control for IMG_2041.jpg")
	}

	two := s.attach(deepak, c.ID, "IMG_2042.jpg", filestest.JPEG(1500))
	r = s1Page(t, dk, check)
	s1Has(t, "two unused", r.pageText(), "2 photos aren't on any expense. Remove them below, or use them on an expense.", "IMG_2042.jpg")

	for _, f := range []files.Meta{one, two} {
		res := dk.postHeaders("/files/"+strconv.FormatInt(f.ID, 10)+"/remove", url.Values{}, map[string]string{"HX-Request": "true"})
		if res.status >= http.StatusBadRequest {
			t.Fatalf("remove %s = %d (%q)", f.Name, res.status, res.pageText())
		}
	}
	r = s1Page(t, dk, check)
	s1Lacks(t, "after remove", r.pageText(), "Photos not on any expense", "on any expense.")
}

// CD-327 (2): a non-Administrator doesn't get the employee as an input on a
// new claim or a draft, but approvers and readers see the claimant on a
// submitted claim's page and its reason pages.
func TestS1_EmployeeShownPastDraft(t *testing.T) {
	s := s1New(t)
	deepak := s.principal(deepakID)
	c := s.claim(deepak, nil, nil)
	dk := s.signedIn(deepakEmail)
	s1Lacks(t, "Deepak's new claim", s1Page(t, dk, s1Claims+"/new").pageText(), "EMP-007")
	s1Lacks(t, "Deepak's draft check", s1Page(t, dk, s1URL(c.ID, "/edit/check")).pageText(), "EmployeeEMP-007")

	s.do(deepak, expense.ClaimDocType, c.ID, doctype.ActionSubmit, doc.ActionInput{Version: c.Version})
	pb := s.signedIn(priyaEmail)
	s1Has(t, "Priya's pending page", s1Page(t, pb, s1URL(c.ID, "")).pageText(), "EmployeeEMP-007 Deepak Nair")
	s1Has(t, "Priya's reject page", s1Page(t, pb, s1URL(c.ID, "/reject")).pageText(), "Deepak Nair")
	s1Has(t, "Deepak's pending page", s1Page(t, dk, s1URL(c.ID, "")).pageText(), "EmployeeEMP-007 Deepak Nair")
}
