package approval_test

import (
	"context"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/types"
)

// CD-327 (3), CD-334: every approval reader that shows a party (the inbox,
// My requests, Stuck and Overdue) shows Info.Party only when
// Deps.PartyVisible says the viewer's listing of that DocType shows it; with
// no PartyVisible it shows the party as before. Each reader asks the hook
// with its viewer and the card's DocType, once per DocType.
func TestApprovalReaders_PartyVisible(t *testing.T) {
	const party = "SHREE Shree Steel Traders"
	type ask struct {
		user    auth.UserID
		doctype string
	}
	for _, c := range []struct {
		name    string
		visible func(context.Context, auth.Principal, string) bool
		want    string
	}{
		{"no hook", nil, party},
		{"visible", func(context.Context, auth.Principal, string) bool { return true }, party},
		{"hidden", func(context.Context, auth.Principal, string) bool { return false }, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := at.New(t)
			e.Clock.Set(time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC))
			for _, n := range []int64{42, 43} {
				at.MustOK(t, e.Submit(e.Priya, e.NewDoc(t, at.Order, n, at.Rs(60000), e.Priya, at.Party("Shree Steel Traders"))))
			}
			var asked []ask
			a := e.NewApprovals(func(deps *approval.Deps) {
				if c.visible != nil {
					deps.PartyVisible = func(ctx context.Context, p auth.Principal, doctype string) bool {
						asked = append(asked, ask{p.ID, doctype})
						return c.visible(ctx, p, doctype)
					}
				}
			})
			cards := func(items []approval.InboxItem, err error) []approval.InboxItem {
				t.Helper()
				at.MustOK(t, err)
				return items
			}
			for _, r := range []struct {
				name  string
				who   auth.Principal
				setup func()
				read  func(p auth.Principal) []approval.InboxItem
			}{
				{"inbox", e.Ravi, nil, func(p auth.Principal) []approval.InboxItem {
					res, err := a.Inbox(t.Context(), p, types.Page{})
					return cards(res.Rows, err)
				}},
				{"inbox of type", e.Ravi, nil, func(p auth.Principal) []approval.InboxItem {
					res, _, err := a.InboxOfType(t.Context(), p, at.Order, types.Page{})
					return cards(res.Rows, err)
				}},
				{"my requests", e.Priya, nil, func(p auth.Principal) []approval.InboxItem {
					res, err := a.MyRequests(t.Context(), p, types.Page{})
					var out []approval.InboxItem
					for _, it := range res.Rows {
						out = append(out, it.InboxItem)
					}
					return cards(out, err)
				}},
				{"overdue", e.Anil, func() { e.Clock.Set(time.Date(2026, 10, 29, 5, 0, 0, 0, time.UTC)) }, func(p auth.Principal) []approval.InboxItem {
					items, err := a.Overdue(t.Context(), p)
					var out []approval.InboxItem
					for _, it := range items {
						out = append(out, it.InboxItem)
					}
					return cards(out, err)
				}},
				// Last: no one can approve once both approvers are disabled.
				{"stuck", e.Anil, func() { e.Disable(t, e.Ravi.ID); e.Disable(t, e.Meena.ID) }, func(p auth.Principal) []approval.InboxItem {
					items, err := a.Stuck(t.Context(), p)
					var out []approval.InboxItem
					for _, it := range items {
						out = append(out, it.InboxItem)
					}
					return cards(out, err)
				}},
			} {
				if r.setup != nil {
					r.setup()
				}
				asked = nil
				rows := r.read(r.who)
				if len(rows) != 2 {
					t.Fatalf("%s: %s's rows = %+v, want Draft 42 and Draft 43", r.name, r.who.Name, rows)
				}
				for _, it := range rows {
					if it.Party != c.want {
						t.Errorf("%s: %s Party = %q, want %q", r.name, it.Title, it.Party, c.want)
					}
				}
				if c.visible != nil && (len(asked) != 1 || asked[0] != (ask{r.who.ID, at.Order})) {
					t.Errorf("%s: PartyVisible asked %+v, want once, for %s on %s", r.name, asked, r.who.Name, at.Order)
				}
			}
		})
	}
}
