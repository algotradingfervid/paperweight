package web

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
)

// page fetches path as p (200 required) and parses it.
func page(t *testing.T, e *at.Env, p auth.Principal, path string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(get(t, e, p, path)))
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return doc
}

// cardTitles are the inbox card titles in page order.
func cardTitles(doc *goquery.Document) []string {
	var titles []string
	doc.Find("section.card .card-title").Each(func(_ int, s *goquery.Selection) {
		titles = append(titles, strings.TrimSpace(s.Text()))
	})
	return titles
}

func TestApprovalInbox_CountMatches(t *testing.T) { // APR-078
	e := at.New(t)
	var docs []at.Doc
	for i, id := range []int64{42, 43, 44} {
		e.Clock.Set(time.Date(2026, 10, 24, 5, i, 0, 0, time.UTC))
		d := e.NewDoc(t, at.Order, id, at.Rs(60000), e.Priya)
		at.MustOK(t, e.Submit(e.Priya, d))
		docs = append(docs, d)
	}
	check := func(when string, want string, cards int) {
		t.Helper()
		if got := strings.TrimSpace(page(t, e, e.Meena, "/approvals/count").Text()); got != want {
			t.Errorf("%s: Meena's badge = %q, want %q", when, got, want)
		}
		if got := cardTitles(page(t, e, e.Meena, "/approvals")); len(got) != cards {
			t.Errorf("%s: Meena's list has %d cards %q, want %d", when, len(got), got, cards)
		}
	}
	check("before approving", "3", 3)
	at.MustOK(t, e.Approve(e.Meena, docs[0]))
	check("after approving Draft 42", "2", 2)
}

// Handler-level support for APR-079 (the e2e case is WP3's
// TestE2E_ApprovalInboxRowContents, CD-40, CD-74).
func TestWeb_InboxRowContents(t *testing.T) {
	e := at.New(t)
	// Draft 42 at the fixture clock (2026-10-24T05:00:00.000Z = 10:30 IST);
	// Draft 41 a minute later, so oldest first is not id order.
	d := e.NewDoc(t, at.Order, 42, at.Rs(94164), e.Priya, at.Party("Shree Steel Traders"))
	at.MustOK(t, e.Submit(e.Priya, d))
	e.Clock.Set(time.Date(2026, 10, 24, 5, 1, 0, 0, time.UTC))
	d41 := e.NewDoc(t, at.Order, 41, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d41))

	doc := page(t, e, e.Ravi, "/approvals")
	if got, want := cardTitles(doc), []string{"Draft 42", "Draft 41"}; !slices.Equal(got, want) {
		t.Errorf("card titles %q, want %q (oldest first)", got, want)
	}
	card := doc.Find("section.card").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return strings.TrimSpace(s.Find(".card-title").Text()) == "Draft 42"
	})
	if card.Length() != 1 {
		t.Fatalf("%d cards titled Draft 42, want 1", card.Length())
	}
	txt := card.Text()
	for _, s := range []string{"Purchase Order", "Draft 42", "Shree Steel Traders", "₹94,164.00", "Priya Shah", "24-10-2026 10:30"} {
		if !strings.Contains(txt, s) {
			t.Errorf("Draft 42 card does not show %q; card text %q", s, txt)
		}
	}
	if n := card.Find(`a[href="/doc/fixture.Order/42"]`).Length(); n != 1 {
		t.Errorf("Draft 42 card has %d links to /doc/fixture.Order/42, want 1", n)
	}
}
