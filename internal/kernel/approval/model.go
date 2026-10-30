package approval

import (
	"time"

	"github.com/vardhan/internaltools/internal/types"
)

// ApproverType says who approves under a rule.
type ApproverType string

const (
	ApproverRole    ApproverType = "role"
	ApproverManager ApproverType = "reporting_manager"
)

// History actions (approval_history.action).
const (
	HistSubmitted    = "submitted"
	HistApproved     = "approved"
	HistAutoApproved = "approved_no_rule"
	HistRejected     = "rejected"
	HistRevised      = "revised"
	HistWithdrawn    = "withdrawn"
)

// Request outcomes (approval_requests.outcome; "" = open).
const (
	OutcomeApproved  = "approved"
	OutcomeRejected  = "rejected"
	OutcomeWithdrawn = "withdrawn"
)

// RuleInput is what the rule form posts.
type RuleInput struct {
	DocType           string
	FromAmount        types.Money
	ApproverType      ApproverType
	Role              string
	FallbackRole      string
	AllowSelfApproval bool
}

// Rule is a stored approval rule.
type Rule struct {
	ID int64
	RuleInput
	Enabled bool
	Version int64
}

// Request is one approval request of a document.
type Request struct {
	ID, RuleID    int64
	FromAmount    types.Money // the rule's from_amount at Open
	DocType       string
	DocID         int64
	Amount        types.Money
	ApproverRoles []string
	ManagerUser   types.UserID
	ManagerName   string
	AllowSelf     bool
	Requester     types.UserID
	RequesterName string
	Creator       types.UserID
	Claimant      types.UserID
	SubmittedAt   time.Time
	Outcome       string // "" = open
	DecidedBy     types.UserID
	DecidedByName string
	DecidedAt     time.Time
	Reason        string
}

// HistoryRow is one append-only approval history entry.
type HistoryRow struct {
	Action       string
	By           types.UserID
	ByName       string
	At           time.Time
	Amount       types.Money
	RuleID       int64
	Reason       string
	SelfApproved bool
}

// Status is the approval line shown on a document.
type Status struct {
	Line      string
	Pending   bool
	WaitingOn string // who a pending document waits on, as named in Line
}

// InboxType is one document type in an inbox, with how many requests it holds.
type InboxType struct {
	DocType, Label string
	Count          int
}

// RequestItem is one row of "My requests".
type RequestItem struct {
	InboxItem
	Outcome, DecidedByName, Reason string
	DecidedAt                      time.Time
	State                          string
}

// StuckItem is an open request no one can approve.
type StuckItem struct {
	InboxItem
	Message string
}
