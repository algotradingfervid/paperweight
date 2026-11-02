package app_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/app"
	"github.com/vardhan/internaltools/internal/testkit"
)

// UI v2 E5b step dates (§5.13 item 4) on the real audit and approval
// history: created on 24-10, submitted on 26-10 and approved one minute after
// IST midnight on 28-10 (18:31 UTC on the 27th), so each step shows its own
// IST day and Approved is the one current step.
func TestApp_StepDatesFromHistory(t *testing.T) {
	clk := testkit.Clock()
	w := newWorld(t, func(c *app.Config) { c.Clock = clk })
	_, _, m := w.canonical()
	priya := w.signedIn(priyaEmail)
	w.signedIn(adminEmail).purchaseManagerRule()
	id := priya.newOrder(m, "PRJ-METRO", "94164.00")
	priya.addLine(m, id)
	clk.Advance(48 * time.Hour) // sessions expire: each later step signs in again
	priya = w.signedIn(priyaEmail)
	priya.act(id, "submit", nil)
	clk.Set(time.Date(2026, 10, 27, 18, 31, 0, 0, time.UTC))
	w.signedIn(raviEmail).act(id, "approve", nil)
	var steps []string
	w.signedIn(priyaEmail).get(orderURL(id)).doc.Find(".doc-head ol.lifecycle > li").Each(func(_ int, s *goquery.Selection) {
		steps = append(steps, strings.Join(strings.Fields(s.Text()), " ")+"|"+s.AttrOr("aria-current", ""))
	})
	if want := []string{"Draft (done) 24-10|", "Pending approval (done) 26-10|", "Approved 28-10|step"}; !slices.Equal(steps, want) {
		t.Errorf("steps = %q, want %q", steps, want)
	}
}
