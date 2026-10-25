package web

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// handler builds Routes on a chi router with p on every request's context
// (auth.WithPrincipal, CD-12), Layout returning the body unchanged and DocURL
// returning /doc/{doctype}/{id}.
func handler(e *at.Env, p auth.Principal) http.Handler {
	r := chi.NewRouter()
	r.Group(Routes(Deps{
		Approvals: e.A,
		Layout:    func(_ string, body templ.Component) templ.Component { return body },
		DocURL:    func(dt string, id int64) string { return fmt.Sprintf("/doc/%s/%d", dt, id) },
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.ServeHTTP(w, req.WithContext(auth.WithPrincipal(req.Context(), p)))
	})
}

// get fetches path as p and returns the body; any status but 200 fails the test.
func get(t *testing.T, e *at.Env, p auth.Principal, path string) string {
	t.Helper()
	rec := uitest.Do(t, handler(e, p), http.MethodGet, path, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s as %s: status %d, body %q", path, p.Name, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// post sends form to path as p and returns the status and body.
func post(t *testing.T, e *at.Env, p auth.Principal, path string, form url.Values) (int, string) {
	t.Helper()
	rec := uitest.Do(t, handler(e, p), http.MethodPost, path, form)
	return rec.Code, rec.Body.String()
}

func TestWeb_InboxListsCardsOldestFirst(t *testing.T) {
	e := at.New(t)
	for i, id := range []int64{43, 42} {
		e.Clock.Set(time.Date(2026, 10, 24, 5, i, 0, 0, time.UTC))
		d := e.NewDoc(t, at.Order, id, at.Rs(60000), e.Priya)
		at.MustOK(t, e.Submit(e.Priya, d))
	}
	body := get(t, e, e.Ravi, "/approvals")
	if i, j := strings.Index(body, "Draft 43"), strings.Index(body, "Draft 42"); i < 0 || j < 0 || i > j {
		t.Errorf("inbox not oldest first (Draft 43 at %d, Draft 42 at %d): %s", i, j, body)
	}
	if got := get(t, e, e.Ravi, "/approvals/count"); strings.TrimSpace(got) != "2" {
		t.Errorf("badge for Ravi = %q, want 2", got)
	}
	// The count fragment is empty when there is nothing to approve.
	if got := get(t, e, e.Priya, "/approvals/count"); strings.TrimSpace(got) != "" {
		t.Errorf("badge for Priya = %q, want empty", got)
	}
}

func TestWeb_RuleFormRefusesNonAdmin(t *testing.T) {
	e := at.New(t)
	const msg = "You don't have permission to change approval rules. Ask an Administrator."
	code, body := post(t, e, e.Ravi, "/admin/approval-rules", url.Values{
		"doctype": {at.Order}, "from_amount": {"200000.00"}, "approver_type": {"role"}, "role": {"Director"},
	})
	if code != http.StatusForbidden {
		t.Errorf("status %d, want 403; body %q", code, body)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse refusal page: %v", err)
	}
	if got := doc.Find(`[role="alert"]`).Text(); !strings.Contains(got, msg) {
		t.Errorf("alert text %q does not contain the ErrNotAdmin message %q", got, msg)
	}
	rules, err := e.A.Rules(t.Context(), e.Anil)
	at.MustOK(t, err)
	if len(rules) != 3 {
		t.Errorf("%d rules after the refused POST, want the 3 seed rules", len(rules))
	}
}

// Review Focus 5: user-typed text (reject reasons, party names, user names)
// on the inbox and My requests screens is HTML-escaped.
func TestWeb_InboxEscapesUserText(t *testing.T) {
	e := at.New(t)
	const (
		party    = `<script>alert(1)</script>`
		reason   = `<img src=x onerror=alert(1)>`
		submName = `<em>Evil</em> Submitter`
		apprName = `<em>Evil</em> Approver`
	)
	evilSubmitter := e.AddUser(t, submName, "evil.submitter@example.com", "Purchase User")
	evilApprover := e.AddUser(t, apprName, "evil.approver@example.com", "Purchase Manager")

	// My requests: party and reject reason (Ravi rejects 42), decider name (44).
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya, at.Party(party))
	at.MustOK(t, e.Submit(e.Priya, d))
	at.MustOK(t, e.Reject(e.Ravi, d, reason))
	d44 := e.NewDoc(t, at.Order, 44, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d44))
	at.MustOK(t, e.Reject(evilApprover, d44, "Rate too high."))

	// Inbox: party and submitter name (43, pending for Ravi).
	d43 := e.NewDoc(t, at.Order, 43, at.Rs(60000), evilSubmitter, at.Party(party))
	at.MustOK(t, e.Submit(evilSubmitter, d43))

	for _, c := range []struct {
		who  auth.Principal
		path string
		text []string // must show as text, not markup
	}{
		{e.Priya, "/approvals/mine", []string{party, reason, apprName}},
		{e.Ravi, "/approvals", []string{party, submName}},
	} {
		body := get(t, e, c.who, c.path)
		if strings.Contains(body, "<script>alert(1)") || strings.Contains(body, "<img src=x") || strings.Contains(body, "<em>Evil") {
			t.Errorf("%s not escaped: %s", c.path, body)
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
		if err != nil {
			t.Fatalf("%s: parse: %v", c.path, err)
		}
		if n := doc.Find("script, img, em").Length(); n != 0 {
			t.Errorf("%s: %d injected script/img/em elements", c.path, n)
		}
		txt := doc.Text()
		for _, s := range c.text {
			if !strings.Contains(txt, s) {
				t.Errorf("%s: text %q not shown", c.path, s)
			}
		}
	}
}
