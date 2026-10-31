package approval

import (
	"context"
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/approval/dbq"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// OverdueAfterDays is when an open request counts as overdue: calendar days
// in IST since it was submitted (spec §14.3, CD-234).
const OverdueAfterDays = 3

// daysOpen is the calendar days in IST from r's submission to today.
func (a *Approvals) daysOpen(r Request) int {
	return types.DaysBetween(types.BusinessDate(r.SubmittedAt), types.Today(a.clock))
}

// waitingDays is r's days open while it is pending and overdue, else 0.
func (a *Approvals) waitingDays(r Request) int {
	if d := a.daysOpen(r); r.Outcome == "" && d >= OverdueAfterDays {
		return d
	}
	return 0
}

// WaitingDays is r's "Waiting {n} days" for other modules: the calendar days
// in IST since submit while r is pending and OverdueAfterDays or more, else 0
// (CD-254, CD-280).
func (a *Approvals) WaitingDays(r Request) int { return a.waitingDays(r) }

// Overdue lists, oldest first, the open requests waiting OverdueAfterDays or
// more, with who can approve each now (Administrators only). A request whose
// document can't be read is skipped.
func (a *Approvals) Overdue(ctx context.Context, p auth.Principal) ([]OverdueItem, error) {
	var items []OverdueItem
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		ok, err := a.auth.HasRoleTx(ctx, tx, p.ID, "Administrator")
		if err != nil {
			return err
		}
		if !ok {
			return notAdminOverdue()
		}
		rows, err := dbq.New(tx.SQL()).ListOpenRequests(ctx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			r, err := requestOf(row)
			if err != nil {
				return err
			}
			days := a.daysOpen(r)
			if days < OverdueAfterDays {
				continue
			}
			in, ok := a.resolve(ctx, tx, r)
			if !ok {
				continue
			}
			users, err := a.eligible(ctx, tx, snapshotOf(r), in)
			if err != nil {
				return err
			}
			var names []string
			for _, u := range users {
				name, err := a.userName(ctx, tx, u)
				if err != nil {
					return err
				}
				names = append(names, name)
			}
			slices.Sort(names)
			items = append(items, OverdueItem{InboxItem: inboxItem(r, in), Days: days, Approvers: names})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	hideParties(ctx, a, p, items, func(it *OverdueItem) *InboxItem { return &it.InboxItem })
	return items, nil
}
