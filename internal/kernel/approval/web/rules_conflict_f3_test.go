package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
)

// CD-362, CD-381: a stale rule edit is the ConflictBanner holding focus, with
// the typed values kept and a plain Reload to the rule's page; it saves nothing.
func TestWeb_RuleEditStaleConflictBanner(t *testing.T) {
	e := at.New(t)
	page := rulesPath + "/" + strconv.FormatInt(e.R1, 10)
	before := rulesOf(t, e)
	code, body := post(t, e, e.Anil, page, url.Values{
		"version": {"99"}, "doctype": {at.Order}, "from_amount": {"75000.00"}, "approver_type": {"role"}, "role": {"Purchase Manager"},
	})
	if code != http.StatusConflict {
		t.Fatalf("status %d, want 409; body %q", code, body)
	}
	d, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	b := d.Find("#rule-alert[autofocus] .banner--warning")
	if b.Length() != 1 || !strings.Contains(b.Text(), "This rule was changed by someone else.") {
		t.Fatalf("no focused warning banner with the text: %q", body)
	}
	a := b.Find("a.btn")
	if got := strings.TrimSpace(a.Text()); a.Length() != 1 || got != "Reload" || a.AttrOr("href", "") != page {
		t.Errorf("Reload = %d, %q to %q, want one Reload to %s", a.Length(), got, a.AttrOr("href", ""), page)
	}
	if got := d.Find(`input[name="from_amount"]`).AttrOr("value", ""); !strings.Contains(got, "75") {
		t.Errorf("from_amount = %q, want the typed value kept", got)
	}
	if got := d.Find(`input[name="version"]`).AttrOr("value", ""); got != "99" {
		t.Errorf("version = %q, want the posted 99 (CD-94)", got)
	}
	after := rulesOf(t, e)
	for i := range before {
		if after[i] != before[i] {
			t.Errorf("rule %d changed: %+v", i, after[i])
		}
	}
}
