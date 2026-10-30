package web

import (
	"slices"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// The URLs the inbox and My requests build (wp/polish plan P2 review).

// openRequests opens n requests of type dt for Meena as Director. Fixture
// names have no space or "&", so the type exists only in this test: it has a
// rule and rights, Evaluate and Open run directly, and the Env must use the
// reference driver (the engine driver knows the fixture types only).
func openRequests(t *testing.T, e *at.Env, dt string, n int) {
	t.Helper()
	e.Auth.Grant("Director", dt, "read", "all")
	e.Auth.Grant("Director", dt, "approve", "all")
	e.AddRule(t, approval.RuleInput{DocType: dt, FromAmount: 0, ApproverType: approval.ApproverRole, Role: "Director"})
	for i := range n {
		e.Clock.Set(time.Date(2026, 10, 24, 5, i, 0, 0, time.UTC))
		d := e.NewDoc(t, dt, int64(100+i), at.Rs(60000), e.Priya)
		at.MustOK(t, e.DB.Write(t.Context(), func(tx *db.Tx) error {
			req, err := e.A.Evaluate(t.Context(), tx, d.Type, d.ID, at.Rs(60000), e.Priya)
			if err != nil {
				return err
			}
			return e.A.Open(t.Context(), tx, d.Type, d.ID, req)
		}))
	}
}

// hrefs are the href of every element matching selector.
func hrefs(doc *goquery.Document, selector string) []string {
	var out []string
	doc.Find(selector).Each(func(_ int, s *goquery.Selection) {
		out = append(out, s.AttrOr("href", ""))
	})
	return out
}

func TestInboxLinks_EscapeType(t *testing.T) {
	for _, c := range []struct{ doctype, query string }{
		{"a&b", "a%26b"},
		{"Purchase Order", "Purchase+Order"},
	} {
		t.Run(c.doctype, func(t *testing.T) {
			t.Setenv("APR_DRIVER", "ref")
			e := at.New(t)
			openRequests(t, e, c.doctype, 26)
			list := "/approvals?type=" + c.query

			first := page(t, e, e.Meena, list)
			if got := len(cardTitles(first)); got != 25 {
				t.Fatalf("%d cards on the first page, want 25", got)
			}
			if got, want := hrefs(first, `.filterbar-row a`), []string{list}; !slices.Equal(got, want) {
				t.Errorf("type links %q, want %q", got, want)
			}
			if got, want := hrefs(first, `a[rel="next"]`), []string{list + "&page=2"}; !slices.Equal(got, want) {
				t.Errorf("next link %q, want %q", got, want)
			}

			second := page(t, e, e.Meena, list+"&page=2")
			if got := len(cardTitles(second)); got != 1 {
				t.Errorf("%d cards on the second page, want 1", got)
			}
			if got, want := hrefs(second, `a[rel="prev"]`), []string{list + "&page=1"}; !slices.Equal(got, want) {
				t.Errorf("previous link %q, want %q", got, want)
			}
		})
	}
}

func TestMine_PagerLinks(t *testing.T) {
	e := at.New(t)
	for i := range 26 {
		e.Clock.Set(time.Date(2026, 10, 24, 5, i, 0, 0, time.UTC))
		at.MustOK(t, e.Submit(e.Priya, e.NewDoc(t, at.Order, int64(100+i), at.Rs(60000), e.Priya)))
	}
	if got, want := hrefs(page(t, e, e.Priya, "/approvals/mine"), `a[rel="next"]`), []string{"/approvals/mine?page=2"}; !slices.Equal(got, want) {
		t.Errorf("next link %q, want %q", got, want)
	}
	if got, want := hrefs(page(t, e, e.Priya, "/approvals/mine?page=2"), `a[rel="prev"]`), []string{"/approvals/mine?page=1"}; !slices.Equal(got, want) {
		t.Errorf("previous link %q, want %q", got, want)
	}
}

// The chip names a known type by its label, even one absent from this inbox,
// and shows anything else as it was asked for.
func TestInboxFilter_ChipText(t *testing.T) {
	e := ordersAndClaim(t)
	for path, want := range map[string]string{
		"/approvals?type=no.such.Type": "Type: no.such.Type",
		claimTypePath:                  "Type: Expense Claim",
	} {
		chip := page(t, e, e.Ravi, path).Find(chipSelector).Clone()
		chip.Find(".visually-hidden").Remove()
		if got := squash(chip.Text()); got != want {
			t.Errorf("GET %s: chip %q, want %q", path, got, want)
		}
	}
}

// A page past the end is not an empty inbox: the pager says so and leads back.
func TestInbox_PastLastPageIsNotEmpty(t *testing.T) {
	e := ordersAndClaim(t)
	for path, prev := range map[string]string{
		"/approvals?page=9":       "/approvals?page=8",
		orderTypePath + "&page=9": orderTypePath + "&page=8",
	} {
		doc := page(t, e, e.Meena, path)
		if n := doc.Find(".empty-state").Length(); n != 0 {
			t.Errorf("GET %s: %d empty states, want none: the inbox has requests", path, n)
		}
		if got := squash(doc.Find("nav.pagination p").Text()); got != "No more results." {
			t.Errorf("GET %s: pager says %q, want No more results.", path, got)
		}
		if got, want := hrefs(doc, `a[rel="prev"]`), []string{prev}; !slices.Equal(got, want) {
			t.Errorf("GET %s: previous link %q, want %q", path, got, want)
		}
	}
}
