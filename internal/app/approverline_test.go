package app_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// m19Lines are the paragraphs of the claim page's main column, normalised.
func m19Lines(t *testing.T, who string, b *browser, id int64) []string {
	t.Helper()
	r := b.get(claimURL(id))
	if r.status != http.StatusOK {
		t.Fatalf("%s GET %s = %d (%q), want 200", who, claimURL(id), r.status, r.pageText())
	}
	var lines []string
	r.doc.Find(".doc-main > p").Each(func(_ int, s *goquery.Selection) {
		lines = append(lines, strings.Join(strings.Fields(s.Text()), " "))
	})
	return lines
}

func m19WantLine(t *testing.T, who string, lines []string, want string) {
	t.Helper()
	if !slices.Contains(lines, want) {
		t.Errorf("%s sees main column lines %q, want %q", who, lines, want)
	}
	for _, l := range lines {
		if strings.HasPrefix(l, "Needs your approval") && l != want {
			t.Errorf("%s sees %q besides %q", who, l, want)
		}
	}
}

// HOOK-032 (CD-263 G9(a), CD-266) on the real services: fixture.Claim under
// a reporting-manager rule from ₹0 with Director as the fallback role.
// Priya, Deepak's manager, is told she is his reporting manager; Meena, a
// Director, is told her role; Deepak, who can't approve his own claim, sees
// the waiting line. Priya keeps the manager form once she is a Director too.
func TestApp_ApproverLineReal(t *testing.T) {
	c := newAuthSeed(t).open(t)
	c.managerRule()
	id := c.newClaim(c.as(deepakEmail), empDeepak)
	priya, meena, deepak := c.as(priyaEmail), c.as(meenaEmail), c.as(deepakEmail)

	const manager = "Needs your approval: Deepak Nair's reporting manager, Any amount"
	m19WantLine(t, "Priya (manager)", m19Lines(t, "Priya", priya, id), manager)
	m19WantLine(t, "Meena (Director)", m19Lines(t, "Meena", meena, id), "Needs your approval: Director, Any amount")

	lines := m19Lines(t, "Deepak", deepak, id)
	if !slices.Contains(lines, "Waiting for approval from Priya Shah or Director") {
		t.Errorf("Deepak sees main column lines %q, want the waiting line", lines)
	}
	for _, l := range lines {
		if strings.HasPrefix(l, "Needs your approval") {
			t.Errorf("Deepak, the requester, sees %q", l)
		}
	}

	c.setRoles(priyaID, "Purchase User", "Employee", "Director")
	m19WantLine(t, "Priya (manager and Director)", m19Lines(t, "Priya", priya, id), manager)
}
