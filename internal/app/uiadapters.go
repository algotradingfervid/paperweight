package app

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// The UI's consumer adapters (CD-78, CD-91). They hold no business logic:
// every value comes from the owning service.

const (
	defaultPageSize = 25

	systemName = "System"
)

// listQuery is docscreen's URL state as a kernel query. It adds no default
// filters, so a disabled master stays findable from /search (CD-215).
func listQuery(dt *doctype.DocType, q docscreen.ListQuery) (list.Query, error) {
	sort, err := list.ParseSort(dt, strings.Replace(q.Sort, ".", " ", 1))
	if err != nil {
		return list.Query{}, err
	}
	out := list.Query{Search: q.Search, Sort: sort, Page: max(q.Page, 1), PageSize: q.Size}
	if out.PageSize == 0 {
		out.PageSize = defaultPageSize
	}
	for _, f := range q.Filters {
		out.Filters = append(out.Filters, list.Filter{Field: f.Field, Op: list.Op(f.Op), Values: f.Values})
	}
	return out, nil
}

// lister is docscreen.Lister over list.Service, which checks read and ANDs the
// read scope into the rows and the count.
type lister struct {
	svc    *list.Service
	db     *db.DB
	engine doc.Engine
}

func (l lister) List(ctx context.Context, p auth.Principal, dt *doctype.DocType, q docscreen.ListQuery) (types.Result[docscreen.ListRow], error) {
	lq, err := listQuery(dt, q)
	if err != nil {
		return types.Result[docscreen.ListRow]{}, err
	}
	res, err := l.svc.List(ctx, p, dt, lq)
	if err != nil {
		return types.Result[docscreen.ListRow]{}, err
	}
	out := types.Result[docscreen.ListRow]{Total: res.Total, Rows: make([]docscreen.ListRow, 0, len(res.Rows))}
	err = l.db.Read(ctx, func(tx *db.Tx) error {
		for _, r := range res.Rows {
			info, err := l.info(ctx, tx, dt, r.ID)
			if err != nil {
				return err
			}
			values := make(map[string]string, len(r.Values))
			for _, f := range list.HeaderFields(dt) {
				values[f.Name] = doc.FormatPlain(f, r.Values[f.Name])
			}
			out.Rows = append(out.Rows, docscreen.ListRow{Info: info, Values: values})
		}
		return nil
	})
	if err != nil {
		return types.Result[docscreen.ListRow]{}, err
	}
	return out, nil
}

// info is a row's Info: the engine serves documents only, so a master answers
// through its own lookup as "{Code} {Name}" (CD-68).
func (l lister) info(ctx context.Context, tx *db.Tx, dt *doctype.DocType, id int64) (doctype.Info, error) {
	if !dt.IsMaster() {
		return l.engine.Info(ctx, tx, dt.Name, id)
	}
	m, _, err := dt.Master.Lookup(ctx, tx, id)
	if err != nil {
		return doctype.Info{}, err
	}
	return doctype.Info{
		DocType: dt.Name, DocLabel: dt.Label, ID: id, Display: strings.TrimSpace(m.Code + " " + m.Name),
	}, nil
}

// extras is docscreen.Extras over APR, DOC, audit and FILE.
type extras struct{ app *App }

func (e extras) Approval(ctx context.Context, p auth.Principal, name string, id int64) (docscreen.ApprovalView, error) {
	dt, _ := doctype.Lookup(name) // docscreen looked it up before asking
	var v docscreen.ApprovalView
	err := e.app.db.Read(ctx, func(tx *db.Tx) error {
		rows, err := e.app.approvals.History(ctx, tx, name, id)
		if err != nil {
			return err
		}
		v.History = approvalHistory(rows)
		if !dt.Approvable {
			return nil
		}
		info, err := e.app.engine.Info(ctx, tx, name, id)
		if err != nil {
			return err
		}
		if info.State == doctype.StateDraft {
			amount, err := dt.Hooks.Amount(ctx, tx, id)
			if err != nil {
				return err
			}
			pre, err := e.app.approvals.Preview(ctx, tx, name, id, amount, p)
			v.Needed, v.Preview = pre.Needed, pre.Text
			return err
		}
		st, err := e.app.approvals.Status(ctx, tx, name, id)
		v.Needed, v.WaitingOn = st.Pending, st.WaitingOn
		return err
	})
	return v, err
}

// approvalHistory is APR's history, newest first.
func approvalHistory(rows []approval.HistoryRow) []docscreen.HistoryRow {
	out := make([]docscreen.HistoryRow, 0, len(rows))
	for _, h := range slices.Backward(rows) {
		action := approvalAction(h.Action)
		if h.Reason != "" {
			action += ": " + h.Reason
		}
		out = append(out, docscreen.HistoryRow{At: types.FormatIST(h.At), By: h.ByName, Action: action})
	}
	return out
}

func approvalAction(action string) string {
	switch action {
	case approval.HistSubmitted:
		return "Submitted"
	case approval.HistApproved:
		return "Approved"
	case approval.HistAutoApproved:
		return "Approved on submit (no approval needed)"
	case approval.HistRejected:
		return "Rejected"
	case approval.HistRevised:
		return "Revised"
	case approval.HistWithdrawn:
		return "Withdrawn"
	}
	return action
}

func (e extras) Origin(ctx context.Context, _ auth.Principal, name string, id int64) (docscreen.Origin, error) {
	var o docscreen.Origin
	err := e.app.db.Read(ctx, func(tx *db.Tx) error {
		d, err := doc.LoadDoc(ctx, tx, name, id)
		if err != nil {
			return err
		}
		o.StateReason, o.AmendedFromID = d.StateReason, d.AmendedFromID
		if d.AmendedFromID == 0 {
			return nil
		}
		from, err := e.app.engine.Info(ctx, tx, name, d.AmendedFromID)
		o.AmendedFrom = from.Display
		return err
	})
	return o, err
}

func (e extras) Audit(ctx context.Context, p auth.Principal, name string, id int64) ([]docscreen.HistoryRow, error) {
	entries, err := doc.History(ctx, e.app.db, e.app.auth, p, name, id)
	if err != nil {
		return nil, err
	}
	dt, _ := doctype.Lookup(name) // doc.History refused an unknown name
	shown, err := e.readableCauses(ctx, p, entries)
	if err != nil {
		return nil, err
	}
	names := nameCache{auth: e.app.auth, names: map[types.UserID]string{}}
	out := make([]docscreen.HistoryRow, 0, len(entries))
	for _, entry := range slices.Backward(entries) {
		by, err := names.of(ctx, entry.By)
		if err != nil {
			return nil, err
		}
		action := auditAction(entry, entry.CausedBy != nil && shown[causeKey{entry.CausedBy.DocType, entry.CausedBy.DocID}])
		if entry.Reason != "" {
			action += ": " + entry.Reason
		}
		row := docscreen.HistoryRow{At: types.FormatIST(entry.At), By: by, Action: action}
		if entry.Action != actionCreate { // DOC-089: a created row is the one line
			row.Changes = changeRows(ctx, dt, entry.Changes)
		}
		out = append(out, row)
	}
	return out, nil
}

// causeKey is the document a roll-up entry names as its cause.
type causeKey struct {
	docType string
	id      int64
}

// readableCauses are the causing documents p may read, checked in one read
// tx: History names only those (CD-240's rule, CD-248). A cause that is gone
// or hidden is not shown.
func (e extras) readableCauses(ctx context.Context, p auth.Principal, entries []audit.Entry) (map[causeKey]bool, error) {
	out := map[causeKey]bool{}
	err := e.app.db.Read(ctx, func(tx *db.Tx) error {
		for _, entry := range entries {
			c := entry.CausedBy
			if c == nil {
				continue
			}
			k := causeKey{c.DocType, c.DocID}
			if _, done := out[k]; done {
				continue
			}
			info, err := e.app.engine.Info(ctx, tx, c.DocType, c.DocID)
			if err == nil {
				rec := auth.RecordRef{
					DocType: info.DocType, ID: info.ID, CreatedBy: info.CreatedBy,
					EmployeeID: info.EmployeeID, ProjectIDs: info.ProjectIDs, SiteIDs: info.SiteIDs,
				}
				err = e.app.auth.CanTx(ctx, tx, p, c.DocType, auth.RightRead, &rec)
			}
			switch {
			case errors.Is(err, auth.ErrForbidden), errors.Is(err, auth.ErrNotFound):
				out[k] = false
			case err != nil:
				return err
			default:
				out[k] = true
			}
		}
		return nil
	})
	return out, err
}

// actionCreate is the audit action of a new document; it is not a doctype.Action.
const actionCreate = "create"

// linkedUpdate is a roll-up whose cause the viewer may not read, or that has none.
const linkedUpdate = "Updated by a linked document"

// auditActions are the audit log's actions as History words them (DOC-089).
var auditActions = map[string]string{
	actionCreate:                   "Created",
	string(doctype.ActionSave):     "Saved",
	string(doctype.ActionSubmit):   "Submitted",
	string(doctype.ActionWithdraw): "Withdrawn",
	string(doctype.ActionApprove):  "Approved",
	string(doctype.ActionReject):   "Rejected",
	string(doctype.ActionRevise):   "Revised",
	string(doctype.ActionClose):    "Closed",
	string(doctype.ActionCancel):   "Cancelled",
	string(doctype.ActionDelete):   "Deleted",
	"amended":                      "Amended",
	"attachment_added":             "Attachment added",
	"attachment_removed":           "Attachment removed",
	"imported":                     "Imported",
	"effect":                       linkedUpdate,
}

// auditAction is an entry's action in words: a roll-up names the document that
// caused it when the viewer may read it (causeShown). An action with no words
// shows as it is, like approvalAction.
func auditAction(e audit.Entry, causeShown bool) string {
	if e.CausedBy != nil && causeShown {
		return "Updated by " + e.CausedBy.Number
	}
	if words, ok := auditActions[e.Action]; ok {
		return words
	}
	return e.Action
}

// changeRows are an audit entry's changes under their field labels, each value
// as the document shows it. A change on a field the DocType doesn't declare
// (auto_approved, number, snapshot) is left out: the page shows those itself.
func changeRows(ctx context.Context, dt *doctype.DocType, changes []audit.Change) []docscreen.ChangeRow {
	out := make([]docscreen.ChangeRow, 0, len(changes))
	for _, c := range changes {
		f, known := dt.Field(c.Field)
		var label, before, after string
		switch {
		case known:
			label = f.Label
			before, after = changeText(ctx, f, c.Before), changeText(ctx, f, c.After)
		case c.Field == "":
			before, after = lineText(ctx, dt, c.Before), lineText(ctx, dt, c.After)
		default:
			continue
		}
		if c.LineNo > 0 {
			label = strings.TrimSpace("Line " + strconv.Itoa(c.LineNo) + " " + label)
		}
		out = append(out, docscreen.ChangeRow{Field: label, Before: before, After: after})
	}
	return out
}

// changeText is one audit value as the document shows it. A link is its code
// alone, so it never reads the master, whose row may have changed since.
func changeText(ctx context.Context, f doctype.Field, raw string) string {
	plain := doc.AuditPlain(f, raw)
	if f.Type == doctype.Link {
		return plain
	}
	text, err := docscreen.Display(ctx, nil, nil, f, plain)
	if err != nil {
		return plain // an entry older than the field's type
	}
	return text
}

// lineText is a whole added or removed line: "Item X, Quantity 5".
func lineText(ctx context.Context, dt *doctype.DocType, raw string) string {
	vals := doc.AuditLine(raw)
	var parts []string
	for _, f := range dt.LineFields() {
		if v, ok := vals[f.Name]; ok {
			parts = append(parts, f.Label+" "+changeText(ctx, f, v))
		}
	}
	return strings.Join(parts, ", ")
}

func (e extras) Attachments(ctx context.Context, p auth.Principal, rec auth.RecordRef) ([]components.Attachment, error) {
	metas, err := e.app.files.List(ctx, p, rec)
	if err != nil {
		return nil, err
	}
	names := nameCache{auth: e.app.auth, names: map[types.UserID]string{}}
	out := make([]components.Attachment, 0, len(metas))
	for _, m := range metas {
		by, err := names.of(ctx, m.UploadedBy)
		if err != nil {
			return nil, err
		}
		out = append(out, components.Attachment{
			ID: m.ID, Name: m.Name, Size: files.HumanSize(m.Size), UploadedBy: by, UploadedAt: types.FormatIST(m.UploadedAt),
			URL: "/files/" + strconv.FormatInt(m.ID, 10), Kind: attachmentKind(m.MIME),
		})
	}
	return out, nil
}

// attachmentKind is how components.Attachments shows a file: browsers can't
// show HEIC, so it is a download (FILE-033).
func attachmentKind(mime string) string {
	switch {
	case mime == "image/heic", mime == "image/heif":
		return "heic"
	case strings.HasPrefix(mime, "image/"):
		return "image"
	}
	return "file"
}

// nameCache reads each user's display name once. User 0 is the system.
type nameCache struct {
	auth  *auth.Auth
	names map[types.UserID]string
}

func (c nameCache) of(ctx context.Context, u types.UserID) (string, error) {
	if u == 0 {
		return systemName, nil
	}
	if name, ok := c.names[u]; ok {
		return name, nil
	}
	name, err := c.auth.DisplayName(ctx, u)
	if err != nil {
		return "", err
	}
	c.names[u] = name
	return name, nil
}
