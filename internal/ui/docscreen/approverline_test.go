package docscreen_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// m19Lines are the paragraphs of the document's main column, normalised.
func m19Lines(t *testing.T, e *docEnv) (lines []string, note string) {
	t.Helper()
	res := e.get(t, doc42)
	if res.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, body %.300s", doc42, res.Code, res.Body)
	}
	page := uitest.Doc(t, res)
	page.Find(".doc-main > p").Each(func(_ int, s *goquery.Selection) { lines = append(lines, norm(s.Text())) })
	return lines, norm(page.Find(".actionbar-note").Text())
}

// HOOK-030 (CD-263 G9(a), CD-266). On a pending document the user offered
// Approve reads `Needs your approval: {Roles}, {Why}` in place of the
// waiting line; anyone else, and an approver APR blocks, keeps
// `Waiting for approval from {WaitingOn}`.
func TestPage_ApproverNeedsYourApproval(t *testing.T) {
	const (
		waiting = "Waiting for approval from Purchase Manager"
		blocked = "You can't approve your own Sample Order. Ask another Purchase Manager to approve it."
	)
	readOnly := []auth.Right{"read", "print"}
	for _, c := range []struct {
		name       string
		rights     []auth.Right
		block      error
		roles, why string
		want       string // the line shown; "" = the waiting line, no "Needs your approval"
		note       string
	}{
		{name: "approver through a role", rights: allRights, roles: "Purchase Manager", why: "₹50,000 or more",
			want: "Needs your approval: Purchase Manager, ₹50,000 or more"},
		{name: "approver through two roles", rights: allRights, roles: "Purchase Manager or Director", why: "₹50,000 or more",
			want: "Needs your approval: Purchase Manager or Director, ₹50,000 or more"},
		{name: "approver as the reporting manager", rights: allRights, roles: "Deepak Nair's reporting manager", why: "Any amount",
			want: "Needs your approval: Deepak Nair's reporting manager, Any amount"},
		{name: "non-approver", rights: readOnly, roles: "Purchase Manager", why: "₹50,000 or more"},
		{name: "self-approval-blocked approver", rights: allRights, roles: "Purchase Manager", why: "₹50,000 or more",
			block: &approval.UserError{Kind: approval.ErrSelfApproval, Msg: blocked}, note: blocked},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newDocEnv(t, c.rights...)
			e.seed(doctype.StatePending)
			e.apr.block = c.block
			e.extras.SetApproval(docscreentest.Sample, 42, docscreen.ApprovalView{
				Needed: true, WaitingOn: "Purchase Manager", Roles: c.roles, Why: c.why,
			})
			lines, note := m19Lines(t, e)
			if c.want != "" {
				if !slices.Contains(lines, c.want) {
					t.Errorf("main column lines = %q, want %q", lines, c.want)
				}
				if slices.Contains(lines, waiting) {
					t.Errorf("main column lines = %q: the approver's line replaces %q", lines, waiting)
				}
				return
			}
			if !slices.Contains(lines, waiting) {
				t.Errorf("main column lines = %q, want %q", lines, waiting)
			}
			for _, l := range lines {
				if strings.HasPrefix(l, "Needs your approval") {
					t.Errorf("main column shows %q to a user who can't approve now", l)
				}
			}
			if note != c.note {
				t.Errorf("action bar note = %q, want %q", note, c.note)
			}
		})
	}
}
