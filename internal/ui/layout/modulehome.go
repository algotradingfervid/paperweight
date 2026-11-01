package layout

import (
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui/components"
)

// The module home's words (UI v2 §5.12; HomeCaughtUp replaces the spec's
// "Nothing waiting for you." on the home, A-24).
const (
	HomeCaughtUp     = "You're all caught up."
	homeWaitingTitle = "Waiting for you"
	homeRecentTitle  = "Recent"
	homeQuickTitle   = "Quick actions"
	homeCountsLabel  = "Counts"
)

// HomeProps is a module home (UI v2 §5.12), filled by the module with only
// what the viewer may read:
//   - Counts are the count cards to show, in order (the module leaves out
//     zero counts);
//   - Waiting holds the rows waiting for the viewer, WaitingAll its "See all"
//     link;
//   - Recent holds the viewer's own recent documents, RecentAll its link;
//   - Quick are the quick actions the viewer's rights allow;
//   - Side, when set, follows the quick actions (recent comments).
type HomeProps struct {
	Title      string
	Counts     []components.StatCardProps
	Waiting    components.DataListProps
	WaitingAll *components.Link
	Recent     components.DataListProps
	RecentAll  *components.Link
	Quick      []components.QuickActionProps
	Side       templ.Component
}

// caughtUp reports whether nothing waits for the viewer: no count and no
// waiting row.
func (p HomeProps) caughtUp() bool {
	return len(p.Counts) == 0 && len(p.Waiting.Rows) == 0
}
