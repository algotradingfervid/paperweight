package docscreen_test

import (
	"slices"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// labelsIn are the follow-up labels under sel.
func labelsIn(sel *goquery.Selection) []string {
	return sel.Map(func(_ int, s *goquery.Selection) string { return norm(s.Text()) })
}

// R9 (§5.13 item 7): the footer's ellipsis opens a sheet with the same More
// actions as the 80rem card, for exactly the viewer's rights. No More actions,
// no ellipsis and no sheet. An action POST in the sheet carries the version.
func TestDocPage_MoreSheet(t *testing.T) {
	for _, c := range []struct {
		name   string
		state  doctype.State
		rights []auth.Right
		more   []string
	}{
		{"approved, all rights", doctype.StateApproved, allRights, []string{"Close", "Cancel", "Print"}},
		{"approved, print only", doctype.StateApproved, []auth.Right{"read", "print"}, []string{"Print"}},
		{"pending approver who holds submit", doctype.StatePending, allRights, []string{"Withdraw", "Print"}},
		{"read only", doctype.StateApproved, []auth.Right{"read"}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newDocEnv(t, c.rights...)
			e.seed(c.state)
			page := uitest.Doc(t, e.get(t, doc42))
			sheet := page.Find(`#main dialog#doc-more`)
			if got := labelsIn(sheet.Find(".more-actions-items li")); !slices.Equal(got, c.more) {
				t.Errorf("sheet = %q, want %q", got, c.more)
			}
			if got := labelsIn(page.Find(".more-actions li")); !slices.Equal(got, c.more) {
				t.Errorf("card = %q, want %q", got, c.more)
			}
			open := page.Find(`.actionbar > [data-open-dialog="doc-more"]`)
			if want := min(len(c.more), 1); open.Length() != want {
				t.Errorf("%d ellipses, want %d", open.Length(), want)
			}
			if c.state == doctype.StatePending {
				if sheet.Find(`form[action$="/withdraw"] input[name="version"]`).Length() != 1 {
					t.Error("the sheet's Withdraw posts no version")
				}
			}
		})
	}
}
