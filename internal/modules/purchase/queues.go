package purchase

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
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

// The meta texts of UX §5.2.
const (
	metaNeedsChanges = "Needs changes"
	metaWaiting      = "Waiting %d days"
	metaApprovedOn   = "Approved on "
	metaRaisedBy     = "Raised by "
	metaRest         = "Vendor says the rest won't come · "
	metaExpectedOn   = "Expected on "
	metaRequiredBy   = "Required by "
	metaOverdue      = "Overdue"
	metaRowsToOrder  = "%d rows to order"
	metaRowToOrder   = "1 row to order"
	metaSep          = " · "
)

// queueList is every row of a queue in its order, without titles, and
// oldestDays, the waiting queue's first row's Approvals.WaitingDays.
type queueList struct {
	rows       []QueueRow
	oldestDays int
}

// lister reads a queue's rows in tx.
type lister func(ctx context.Context, tx *db.Tx) (queueList, error)

// Queue is page page (from 1, QueuePageSize rows) of queue name for p. An
// unknown name, or a queue whose card's right p lacks, is AUTH's 404.
func (s *Service) Queue(ctx context.Context, p auth.Principal, name string, page int) (QueueView, error) {
	list, ok, err := s.source(ctx, p, name)
	if err != nil {
		return QueueView{}, err
	}
	if !ok {
		return QueueView{}, auth.ErrNotFound
	}
	v := QueueView{Name: name}
	err = s.d.DB.Read(ctx, func(tx *db.Tx) error {
		l, err := list(ctx, tx)
		if err != nil {
			return err
		}
		v.Total = len(l.rows)
		from := min(max(page-1, 0)*QueuePageSize, v.Total)
		v.Rows = l.rows[from:min(from+QueuePageSize, v.Total)]
		for i := range v.Rows {
			info, err := s.d.Engine.Info(ctx, tx, v.Rows[i].DocType, v.Rows[i].ID)
			if err != nil {
				return err
			}
			v.Rows[i].Title = info.Display
		}
		return nil
	})
	if err != nil {
		return QueueView{}, err
	}
	return v, nil
}

// source is queue name's lister for p, with its Auth.Scope asked before any
// tx opens (Scope takes its own connection). ok is false for an unknown name
// or when p lacks the card's right (spec §25).
func (s *Service) source(ctx context.Context, p auth.Principal, name string) (lister, bool, error) {
	switch name {
	case QueueDrafts:
		pred, ok, err := s.scope(ctx, p, DocType, auth.RightCreate)
		return func(ctx context.Context, tx *db.Tx) (queueList, error) { return s.drafts(ctx, tx, p.ID, pred) }, ok, err
	case QueueWaiting:
		pred, ok, err := s.scope(ctx, p, DocType, auth.RightCreate)
		return func(ctx context.Context, tx *db.Tx) (queueList, error) { return s.waiting(ctx, tx, p.ID, pred) }, ok, err
	case QueueToSend:
		pred, approver, err := s.scope(ctx, p, DocType, auth.RightApprove)
		if err != nil || approver {
			return func(ctx context.Context, tx *db.Tx) (queueList, error) { return s.toSend(ctx, tx, 0, pred) }, approver, err
		}
		pred, ok, err := s.scope(ctx, p, DocType, auth.RightCreate)
		return func(ctx context.Context, tx *db.Tx) (queueList, error) { return s.toSend(ctx, tx, p.ID, pred) }, ok, err
	case QueueToClose:
		pred, ok, err := s.scope(ctx, p, DocType, auth.RightAmend)
		return func(ctx context.Context, tx *db.Tx) (queueList, error) { return toClose(ctx, tx, pred) }, ok, err
	case QueueAwaitingDelivery:
		pred, ok, err := s.scope(ctx, p, DocType, auth.RightRead)
		return func(ctx context.Context, tx *db.Tx) (queueList, error) { return s.awaiting(ctx, tx, pred) }, ok, err
	case QueueRequestsToOrder:
		if _, ok, err := s.scope(ctx, p, DocType, auth.RightCreate); err != nil || !ok {
			return nil, false, err
		}
		// Without read on requests, pred is noRows: the queue is empty.
		pred, _, err := s.scope(ctx, p, RequestDocType, auth.RightRead)
		return func(ctx context.Context, tx *db.Tx) (queueList, error) { return s.requestsToOrder(ctx, tx, pred) }, true, err
	case QueueMyRequests:
		pred, ok, err := s.scope(ctx, p, RequestDocType, auth.RightCreate)
		return func(ctx context.Context, tx *db.Tx) (queueList, error) { return myRequests(ctx, tx, p.ID, pred) }, ok, err
	}
	return nil, false, nil
}

// noRows is a predicate that matches no row.
var noRows = auth.Predicate{SQL: "0 = 1"}

// scope is Auth.Scope with a missing right as ok false rather than an
// error. It fails closed: without the right the predicate is noRows, never
// the empty "every row" filter.
func (s *Service) scope(ctx context.Context, p auth.Principal, dt string, r auth.Right) (auth.Predicate, bool, error) {
	pred, err := s.d.Auth.Scope(ctx, p, dt, r)
	var denied *auth.DeniedError
	if errors.As(err, &denied) {
		return noRows, false, nil
	}
	return pred, err == nil, err
}

// drafts is My drafts: owner's draft and rejected POs in pred.
func (s *Service) drafts(ctx context.Context, tx *db.Tx, owner auth.UserID, pred auth.Predicate) (queueList, error) {
	in, err := scopeIn(ctx, tx, "purchase_order", pred)
	if err != nil {
		return queueList{}, err
	}
	rows, err := dbq.New(tx.SQL()).HomeDrafts(ctx, int64(owner))
	if err != nil {
		return queueList{}, err
	}
	var l queueList
	for _, r := range rows {
		if !in(r.ID) {
			continue
		}
		on, err := isoDate(r.OrderDate)
		if err != nil {
			return queueList{}, err
		}
		st := doctype.State(r.State)
		var needs string
		if st == doctype.StateRejected {
			needs = metaNeedsChanges
		}
		l.rows = append(l.rows, orderRow(r.ID, r.RoundedTotal, st, r.SnapVendorName, join(on, r.ProjectCode), needs))
	}
	return l, nil
}

// waiting is Waiting for approval: owner's pending POs in pred, oldest
// submitted first, with APR's status line and WaitingDays.
func (s *Service) waiting(ctx context.Context, tx *db.Tx, owner auth.UserID, pred auth.Predicate) (queueList, error) {
	in, err := scopeIn(ctx, tx, "purchase_order", pred)
	if err != nil {
		return queueList{}, err
	}
	rows, err := dbq.New(tx.SQL()).HomeWaiting(ctx, int64(owner))
	if err != nil {
		return queueList{}, err
	}
	type item struct {
		row  QueueRow
		at   time.Time
		days int
	}
	var items []item
	for _, r := range rows {
		if !in(r.ID) {
			continue
		}
		req, _, err := s.d.Approvals.LatestRequest(ctx, tx, DocType, r.ID)
		if err != nil {
			return queueList{}, err
		}
		st, err := s.d.Approvals.Status(ctx, tx, DocType, r.ID)
		if err != nil {
			return queueList{}, err
		}
		days := s.d.Approvals.WaitingDays(req)
		var wait string
		if days > 0 {
			wait = fmt.Sprintf(metaWaiting, days)
		}
		items = append(items, item{orderRow(r.ID, r.RoundedTotal, doctype.State(r.State), r.SnapVendorName, st.Line, wait), req.SubmittedAt, days})
	}
	slices.SortStableFunc(items, func(a, b item) int { return a.at.Compare(b.at) })
	var l queueList
	for _, it := range items {
		l.rows = append(l.rows, it.row)
	}
	if len(items) > 0 {
		l.oldestDays = items[0].days
	}
	return l, nil
}

// toSend is To send: approved POs with no Sent record in pred, owner's or
// (owner 0, a viewer with approve) everyone's with "Raised by".
func (s *Service) toSend(ctx context.Context, tx *db.Tx, owner auth.UserID, pred auth.Predicate) (queueList, error) {
	in, err := scopeIn(ctx, tx, "purchase_order", pred)
	if err != nil {
		return queueList{}, err
	}
	rows, err := dbq.New(tx.SQL()).HomeToSend(ctx, int64(owner))
	if err != nil {
		return queueList{}, err
	}
	names := map[int64]string{}
	var l queueList
	for _, r := range rows {
		if !in(r.ID) {
			continue
		}
		on, err := tsDate(r.ApprovedAt)
		if err != nil {
			return queueList{}, err
		}
		var raised string
		if owner == 0 {
			if raised, err = s.userName(ctx, tx, names, r.CreatedBy); err != nil {
				return queueList{}, err
			}
			raised = metaRaisedBy + raised
		}
		l.rows = append(l.rows, orderRow(r.ID, r.RoundedTotal, doctype.State(r.State), r.SnapVendorName, metaApprovedOn+on, raised))
	}
	return l, nil
}

// toClose is To close: approved POs with a rest row in pred, the oldest
// row's date as the meta; no GRN is named (CD-240 family).
func toClose(ctx context.Context, tx *db.Tx, pred auth.Predicate) (queueList, error) {
	in, err := scopeIn(ctx, tx, "purchase_order", pred)
	if err != nil {
		return queueList{}, err
	}
	rows, err := dbq.New(tx.SQL()).HomeToClose(ctx)
	if err != nil {
		return queueList{}, err
	}
	var l queueList
	for _, r := range rows {
		if !in(r.ID) {
			continue
		}
		since, err := tsDate(r.Since)
		if err != nil {
			return queueList{}, err
		}
		l.rows = append(l.rows, orderRow(r.ID, r.RoundedTotal, doctype.State(r.State), r.SnapVendorName, metaRest+since))
	}
	return l, nil
}

// awaiting is Awaiting delivery: approved POs not fully received in pred,
// with the receipt status, the due date (spec §27.1) and Overdue when it is
// before today.
func (s *Service) awaiting(ctx context.Context, tx *db.Tx, pred auth.Predicate) (queueList, error) {
	in, err := scopeIn(ctx, tx, "purchase_order", pred)
	if err != nil {
		return queueList{}, err
	}
	rows, err := dbq.New(tx.SQL()).HomeAwaiting(ctx)
	if err != nil {
		return queueList{}, err
	}
	today := types.Today(s.d.Clock)
	var l queueList
	for _, r := range rows {
		if !in(r.ID) {
			continue
		}
		var due, overdue string
		if r.Due != "" {
			d, err := types.DateFromISO(r.Due)
			if err != nil {
				return queueList{}, err
			}
			due = metaRequiredBy + types.FormatDate(d)
			if r.ExpectedOn != "" {
				due = metaExpectedOn + types.FormatDate(d)
			}
			if d.Compare(today) < 0 {
				overdue = metaOverdue
			}
		}
		l.rows = append(l.rows, orderRow(r.ID, r.RoundedTotal, doctype.State(r.State), r.SnapVendorName, ReceiptLabel(r.ReceiptStatus), due, overdue))
	}
	return l, nil
}

// requestsToOrder is Requests to order: submitted requests in pred with a
// row pending to order (PendingToOrder), with the requester, site and needed
// by, and the number of such rows.
func (s *Service) requestsToOrder(ctx context.Context, tx *db.Tx, pred auth.Predicate) (queueList, error) {
	in, err := scopeIn(ctx, tx, "purchase_request", pred)
	if err != nil {
		return queueList{}, err
	}
	q := dbq.New(tx.SQL())
	lines, err := q.HomeRequestLines(ctx)
	if err != nil {
		return queueList{}, err
	}
	var ids []int64
	for _, ln := range lines {
		if in(ln.ParentID) {
			ids = append(ids, ln.ID)
		}
	}
	pending, err := PendingToOrder(ctx, tx, ids)
	if err != nil {
		return queueList{}, err
	}
	open := map[int64]int{}
	for _, ln := range lines {
		if pending[ln.ID] > 0 {
			open[ln.ParentID]++
		}
	}
	rows, err := q.HomeRequestsToOrder(ctx)
	if err != nil {
		return queueList{}, err
	}
	names := map[int64]string{}
	var l queueList
	for _, r := range rows {
		k := open[r.ID] // only lines of requests in pred are counted
		if k == 0 {
			continue
		}
		by, err := s.userName(ctx, tx, names, r.CreatedBy)
		if err != nil {
			return queueList{}, err
		}
		needed, err := isoDate(r.RequiredBy)
		if err != nil {
			return queueList{}, err
		}
		count := metaRowToOrder
		if k != 1 {
			count = fmt.Sprintf(metaRowsToOrder, k)
		}
		l.rows = append(l.rows, requestRow(r.ID, doctype.State(r.State), by, join(r.SiteCode, needed), count))
	}
	return l, nil
}

// myRequests is My requests: owner's draft and submitted requests in pred,
// newest first, with the site, needed by and order status.
func myRequests(ctx context.Context, tx *db.Tx, owner auth.UserID, pred auth.Predicate) (queueList, error) {
	in, err := scopeIn(ctx, tx, "purchase_request", pred)
	if err != nil {
		return queueList{}, err
	}
	rows, err := dbq.New(tx.SQL()).HomeMyRequests(ctx, int64(owner))
	if err != nil {
		return queueList{}, err
	}
	var l queueList
	for _, r := range rows {
		if !in(r.ID) {
			continue
		}
		needed, err := isoDate(r.RequiredBy)
		if err != nil {
			return queueList{}, err
		}
		l.rows = append(l.rows, requestRow(r.ID, doctype.State(r.State), join(r.SiteCode, needed), orderStatusLabel(r.OrderStatus)))
	}
	return l, nil
}

// orderRow is a PO queue row; empty meta texts are left out.
func orderRow(id, rounded int64, st doctype.State, meta ...string) QueueRow {
	return QueueRow{DocType: DocType, ID: id, Total: types.Money(rounded), State: st, Meta: metaOf(meta)}
}

// requestRow is a request queue row; empty meta texts are left out.
func requestRow(id int64, st doctype.State, meta ...string) QueueRow {
	return QueueRow{DocType: RequestDocType, ID: id, State: st, Meta: metaOf(meta)}
}

// metaOf is meta without its empty texts.
func metaOf(meta []string) []string {
	return slices.DeleteFunc(meta, func(m string) bool { return m == "" })
}

// join is the non-empty parts joined with " · " ("METRO-SITE · 05-11-2026",
// or the site alone).
func join(parts ...string) string {
	return strings.Join(metaOf(parts), metaSep)
}

// isoDate is a stored ISO date as dd-mm-yyyy; "" stays "".
func isoDate(iso string) (string, error) {
	if iso == "" {
		return "", nil
	}
	d, err := types.DateFromISO(iso)
	if err != nil {
		return "", err
	}
	return types.FormatDate(d), nil
}

// tsDate is a stored timestamp's IST business date as dd-mm-yyyy.
func tsDate(stored string) (string, error) {
	ts, err := types.ParseTimestamp(stored)
	if err != nil {
		return "", err
	}
	return types.FormatDate(types.BusinessDate(ts.Time())), nil
}

// userName is user id's name, read once per list through names.
func (s *Service) userName(ctx context.Context, tx *db.Tx, names map[int64]string, id int64) (string, error) {
	if n, ok := names[id]; ok {
		return n, nil
	}
	u, _, err := s.d.Auth.User(ctx, tx, auth.UserID(id))
	if err != nil {
		return "", err
	}
	names[id] = u.Name
	return u.Name, nil
}

// scopeIn reports whether an id of table is in pred, read in tx; an empty
// pred is every row. table is always a constant in code, never user input.
// Scope is asked before the tx opens (it takes its own connection).
func scopeIn(ctx context.Context, tx *db.Tx, table string, pred auth.Predicate) (func(int64) bool, error) {
	if pred.SQL == "" {
		return func(int64) bool { return true }, nil
	}
	rows, err := tx.SQL().QueryContext(ctx, `SELECT "`+table+`".id FROM "`+table+`" WHERE `+pred.SQL, pred.Args...) //nolint:gosec // G202: a constant table and AUTH's predicate, with its args
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return func(id int64) bool { return ids[id] }, nil
}
