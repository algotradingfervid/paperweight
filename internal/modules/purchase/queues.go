package purchase

import (
	"context"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
)

// Queue names: the `/modules/purchase/q/{queue}` slugs (UX §5.2).
const (
	QueueDrafts           = "drafts"
	QueueWaiting          = "waiting"
	QueueToSend           = "to-send"
	QueueToClose          = "to-close"
	QueueAwaitingDelivery = "awaiting-delivery"
	QueueRequestsToOrder  = "requests-to-order"
	QueueMyRequests       = "my-requests"
)

// QueuePageSize is a queue page's rows (UX §5.2).
const QueuePageSize = 25

// QueueRow is one queue row (UX §5.2): the document's DocType and id, Title
// (DOC's display, "PO/26-27/00042" or "Draft 57"), Total (the PO's rounded
// total; 0 for a request), State, and Meta, the row's UX §5.2 meta texts in
// the table's order:
//
//   - drafts: vendor; "{order date} · {project code}"; "Needs changes" when rejected
//   - waiting: vendor; APR's status line; "Waiting {n} days" when WaitingDays > 0
//   - to-send: vendor; "Approved on {date}"; "Raised by {owner}" for a viewer with approve
//   - to-close: vendor; "Vendor says the rest won't come · {date}" (the oldest row)
//   - awaiting-delivery: vendor; "Not received" or "Part received";
//     "Expected on {date}" when set, else "Required by {date}" when set; "Overdue"
//   - requests-to-order: requester; "{site code} · {needed by}" (the site alone
//     without one); "{k} rows to order" ("1 row to order")
//   - my-requests: "{site code} · {needed by}" (the site alone without one);
//     "Not ordered", "Part ordered" or "Ordered"
//
// Dates are dd-mm-yyyy (types.FormatDate).
type QueueRow struct {
	DocType string
	ID      int64
	Title   string
	Total   types.Money
	State   doctype.State
	Meta    []string
}

// QueueView is one page of a queue: Name, the page's Rows and Total, the
// row count over every page (the card's Count).
type QueueView struct {
	Name  string
	Rows  []QueueRow
	Total int
}

// errQueueNotBuilt is the P14 stub's error.
var errQueueNotBuilt = errors.New("purchase: Queue not implemented (P14)")

// Queue is page page (from 1, QueuePageSize rows) of queue name for p. An
// unknown name, or a queue whose card's right p lacks, is AUTH's 404. P14
// builds it.
func (s *Service) Queue(_ context.Context, _ auth.Principal, _ string, _ int) (QueueView, error) {
	return QueueView{}, errQueueNotBuilt
}
