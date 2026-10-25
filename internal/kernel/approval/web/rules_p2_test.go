package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
)

// pageText is the visible text of an HTML page.
func pageText(t *testing.T, body string) string {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		t.Fatalf("parse page: %v", err)
	}
	return doc.Text()
}

// rulesOf is every rule, as Anil (Administrator) sees them.
func rulesOf(t *testing.T, e *at.Env) []approval.Rule {
	t.Helper()
	rules, err := e.A.Rules(t.Context(), e.Anil)
	at.MustOK(t, err)
	return rules
}

// APR-004
func TestApprovalRule_FromAmountPrecision(t *testing.T) {
	e := at.New(t)
	const msg = "Amount can have at most 2 decimal places."
	code, body := post(t, e, e.Anil, rulesPath, url.Values{
		"doctype": {at.Order}, "from_amount": {"50000.005"}, "approver_type": {"role"}, "role": {"Director"},
	})
	if code != http.StatusUnprocessableEntity {
		t.Errorf("status %d, want 422; body %q", code, body)
	}
	if got := pageText(t, body); !strings.Contains(got, msg) {
		t.Errorf("page text %q does not contain %q", got, msg)
	}
	if rules := rulesOf(t, e); len(rules) != 3 {
		t.Errorf("%d rules after the refused POST, want the 3 seed rules: %+v", len(rules), rules)
	}
}

// An edit without a readable version is refused with AUTH's bad-form message
// and saves nothing (CD-94; CD-146 analogue).
func TestWeb_RuleEditNeedsVersion(t *testing.T) {
	for name, version := range map[string][]string{"missing": nil, "non-numeric": {"abc"}} {
		t.Run(name, func(t *testing.T) {
			e := at.New(t)
			before := rulesOf(t, e)
			form := url.Values{
				"doctype": {at.Order}, "from_amount": {"75000.00"}, "approver_type": {"role"}, "role": {"Purchase Manager"},
			}
			if version != nil {
				form["version"] = version
			}
			code, body := post(t, e, e.Anil, rulesPath+"/"+strconv.FormatInt(e.R1, 10), form)
			if code != http.StatusBadRequest {
				t.Errorf("status %d, want 400; body %q", code, body)
			}
			if got := pageText(t, body); !strings.Contains(got, badFormText) {
				t.Errorf("page text %q does not contain %q", got, badFormText)
			}
			after := rulesOf(t, e)
			if len(after) != len(before) {
				t.Fatalf("%d rules after the refused POST, want %d", len(after), len(before))
			}
			for i := range before {
				if after[i] != before[i] {
					t.Errorf("rule changed by the refused POST: %+v, was %+v", after[i], before[i])
				}
			}
		})
	}
}
