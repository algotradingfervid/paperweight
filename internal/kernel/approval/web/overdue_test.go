package web

import (
	"fmt"
	"net/http"
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

// The Overdue approvals page and the "Waiting {n} days" line on My requests
// (spec §14.3, PO-042, PO-043). Page text is compared with goquery (lessons §10).

const overduePath = "/admin/approvals/overdue"

// titledGet fetches path as p through Routes and returns the page and the
// title the handler gave Layout; any status but 200 fails the test.
func titledGet(t *testing.T, e *at.Env, p auth.Principal, path string) (*goquery.Document, string) {
	t.Helper()
	var title string
	r := chi.NewRouter()
	r.Group(Routes(Deps{
		Approvals: e.A,
		Layout: func(tl string, body templ.Component) templ.Component {
			title = tl
			return body
		},
		DocURL: func(dt string, id int64) string { return fmt.Sprintf("/doc/%s/%d", dt, id) },
	}))
	h := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.ServeHTTP(w, req.WithContext(auth.WithPrincipal(req.Context(), p)))
	})
	rec := uitest.Do(t, h, http.MethodGet, path, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s as %s: status %d, body %q", path, p.Name, rec.Code, rec.Body.String())
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rec.Body.String()))
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return doc, title
}

// cardByTitle is the one card titled title.
func cardByTitle(t *testing.T, doc *goquery.Document, title string) *goquery.Selection {
	t.Helper()
	card := doc.Find("section.card").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return strings.TrimSpace(s.Find(".card-title").Text()) == title
	})
	if card.Length() != 1 {
		t.Fatalf("%d cards titled %s, want 1", card.Length(), title)
	}
	return card
}

func TestWeb_OverdueListedForAdmin(t *testing.T) { // PO-043
	e := at.New(t)
	e.AddUser(t, "Aarti Desai", "aarti@vardhan.test", "Director")
	// 43 is submitted first but 42 is older.
	e.Clock.Set(time.Date(2026, 10, 25, 5, 0, 0, 0, time.UTC))
	at.MustOK(t, e.Submit(e.Priya, e.NewDoc(t, at.Order, 43, at.Rs(6_00_000), e.Priya)))
	e.Clock.Set(time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC))
	at.MustOK(t, e.Submit(e.Priya, e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)))
	e.Clock.Set(time.Date(2026, 10, 26, 5, 0, 0, 0, time.UTC))
	at.MustOK(t, e.Submit(e.Priya, e.NewDoc(t, at.Order, 44, at.Rs(60000), e.Priya)))

	// 26 Oct: 42 is 2 days old, nothing is overdue.
	doc, title := titledGet(t, e, e.Anil, overduePath)
	if title != "Overdue approvals" {
		t.Errorf("page title %q, want %q", title, "Overdue approvals")
	}
	if got := cardTitles(doc); len(got) != 0 {
		t.Errorf("overdue cards on 26 Oct %q, want none", got)
	}
	wantEmptyState(t, doc, "No approvals are overdue.")

	// 28 Oct: 42 (4 days), then 43 (3 days); 44 (2 days) isn't listed.
	e.Clock.Set(time.Date(2026, 10, 28, 5, 0, 0, 0, time.UTC))
	doc, _ = titledGet(t, e, e.Anil, overduePath)
	if got, want := cardTitles(doc), []string{"Draft 42", "Draft 43"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("overdue cards %q, want %q (oldest first)", got, want)
	}
	if n := doc.Find(".empty-state").Length(); n != 0 {
		t.Errorf("overdue page with cards also shows %d empty states", n)
	}
	for _, c := range []struct {
		title, waiting string
		show, hide     []string
	}{
		{"Draft 42", "Waiting 4 days", []string{"Aarti Desai", "Meena Iyer", "Ravi Kulkarni", "Purchase Order", "₹60,000.00", "Priya Shah"}, nil},
		{"Draft 43", "Waiting 3 days", []string{"Aarti Desai", "Meena Iyer", "₹6,00,000.00"}, []string{"Ravi Kulkarni"}},
	} {
		card := cardByTitle(t, doc, c.title)
		txt := squash(card.Text())
		if !strings.Contains(txt, c.waiting) {
			t.Errorf("card %s does not show %q; card text %q", c.title, c.waiting, txt)
		}
		for _, s := range c.show {
			if !strings.Contains(txt, s) {
				t.Errorf("card %s does not show %q; card text %q", c.title, s, txt)
			}
		}
		for _, s := range c.hide {
			if strings.Contains(txt, s) {
				t.Errorf("card %s shows %q, who can't approve it; card text %q", c.title, s, txt)
			}
		}
		// Approvers in name order: Aarti (id 1001) before Meena (2).
		if i, j := strings.Index(txt, "Aarti Desai"), strings.Index(txt, "Meena Iyer"); i < 0 || j < 0 || i > j {
			t.Errorf("card %s approvers not in name order; card text %q", c.title, txt)
		}
	}
	if n := doc.Find(`section.card a[href="/doc/fixture.Order/42"]`).Length(); n != 1 {
		t.Errorf("overdue card has %d links to /doc/fixture.Order/42, want 1", n)
	}
}

func TestWeb_OverdueRefusesNonAdmin(t *testing.T) { // PO-043
	e := at.New(t)
	at.MustOK(t, e.Submit(e.Priya, e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)))
	e.Clock.Set(time.Date(2026, 10, 28, 5, 0, 0, 0, time.UTC))
	const msg = "You don't have permission to see overdue approvals. Ask an Administrator."
	for _, p := range []auth.Principal{e.Ravi, e.Priya} {
		rec := uitest.Do(t, handler(e, p), http.MethodGet, overduePath, nil)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403; body %q", p.Name, rec.Code, rec.Body.String())
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(rec.Body.String()))
		if err != nil {
			t.Fatalf("parse refusal page: %v", err)
		}
		if got := doc.Find(`[role="alert"]`).Text(); !strings.Contains(got, msg) {
			t.Errorf("%s: alert text %q does not contain the ErrNotAdmin message %q", p.Name, got, msg)
		}
		if strings.Contains(doc.Text(), "Draft 42") {
			t.Errorf("%s: refusal page names Draft 42: %q", p.Name, squash(doc.Text()))
		}
	}
}

func TestWeb_MyRequestsShowsWaitingDays(t *testing.T) { // PO-042
	e := at.New(t)
	d42 := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d42))
	d43 := e.NewDoc(t, at.Order, 43, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d43))

	// Day 2 (26 Oct): nothing says Waiting.
	e.Clock.Set(time.Date(2026, 10, 26, 5, 0, 0, 0, time.UTC))
	doc := page(t, e, e.Priya, "/approvals/mine")
	if got := cardTitles(doc); len(got) != 2 {
		t.Fatalf("My requests cards %q, want Draft 42 and Draft 43", got)
	}
	if txt := squash(doc.Text()); strings.Contains(txt, "Waiting") {
		t.Errorf("My requests on day 2 says Waiting: %q", txt)
	}

	// Day 3 (27 Oct): both open cards say it; 43 is then approved and stops.
	e.Clock.Set(time.Date(2026, 10, 27, 5, 0, 0, 0, time.UTC))
	doc = page(t, e, e.Priya, "/approvals/mine")
	for _, title := range []string{"Draft 42", "Draft 43"} {
		if txt := squash(cardByTitle(t, doc, title).Text()); !strings.Contains(txt, "Waiting 3 days") {
			t.Errorf("card %s on day 3 does not show %q; card text %q", title, "Waiting 3 days", txt)
		}
	}
	at.MustOK(t, e.Approve(e.Ravi, d43))
	e.Clock.Set(time.Date(2026, 10, 28, 5, 0, 0, 0, time.UTC))
	doc = page(t, e, e.Priya, "/approvals/mine")
	if txt := squash(cardByTitle(t, doc, "Draft 42").Text()); !strings.Contains(txt, "Waiting 4 days") {
		t.Errorf("card Draft 42 on day 4 does not show %q; card text %q", "Waiting 4 days", txt)
	}
	if txt := squash(cardByTitle(t, doc, "PO/26-27/00001").Text()); strings.Contains(txt, "Waiting") {
		t.Errorf("approved card PO/26-27/00001 still says Waiting: %q", txt)
	}
}
