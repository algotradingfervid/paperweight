// Package approval decides who must approve a document and records the
// decisions (spec §5). APR owns it.
package approval

import (
	"context"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

type Requirement struct { // CD-24
	Needed        bool
	RuleID        int64
	ApproverRoles []string
	ManagerUser   auth.UserID
	Amount        types.Money
	Requester     auth.UserID
}

// InboxItem is one row of an approver's inbox.
type InboxItem struct {
	RequestID       int64
	DocType, Label  string // Label = Info.DocLabel
	DocID           int64
	Title, Party    string // Title = Info.Display; Party = Info.Party (CD-55; "" when none)
	Amount          types.Money
	SubmittedBy     types.UserID
	SubmittedByName string
	SubmittedAt     time.Time
}

type Service interface {
	Evaluate(ctx context.Context, tx *db.Tx, doctype string, id int64, amount types.Money, requester auth.Principal) (Requirement, error)
	Open(ctx context.Context, tx *db.Tx, doctype string, id int64, req Requirement) error // records "submitted"
	CheckApprover(ctx context.Context, tx *db.Tx, p auth.Principal, doctype string, id int64) error
	Record(ctx context.Context, tx *db.Tx, doctype string, id int64, action doctype.Action, p auth.Principal, reason string) error
	Inbox(ctx context.Context, p auth.Principal, page types.Page) (types.Result[InboxItem], error)
}

var _ Service = (*Approvals)(nil)

// DOC ↔ APR call protocol (CD-23), all inside DOC's one db.Write:
//   submit:   Evaluate → Open (always) → if !Needed: continue as approve WITHOUT CheckApprover, Record("approve")
//   approve:  CheckApprover BEFORE DOC's from-state guard → re-validate → BeforeSubmit → naming.Assign → ApplyEffects → Record("approve")
//   reject:   CheckApprover BEFORE DOC's from-state guard → reason check → Record("reject", reason)
//   revise:   Record("revise")
//   withdraw: DOC state guard and `submit` right first → Record("withdraw")
//   delete (draft): no APR call
