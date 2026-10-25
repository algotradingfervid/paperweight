package approvaltest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// RequestByID reads one approval request by id, open or closed (APR-067).
// Raw SQL, not dbq: approvaltest may not import approval/dbq (CD-85).
func (e *Env) RequestByID(t testing.TB, id int64) approval.Request {
	t.Helper()
	var r approval.Request
	var roles, submitted, decided string
	MustOK(t, e.DB.Read(context.Background(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(context.Background(),
			`SELECT id, rule_id, doctype, doc_id, amount, approver_roles, COALESCE(manager_user, 0), manager_name, allow_self,
				requester, requester_name, creator, COALESCE(claimant_user, 0), submitted_at, COALESCE(outcome, ''),
				COALESCE(decided_by, 0), decided_by_name, COALESCE(decided_at, ''), reason
			FROM approval_requests WHERE id = ?`, id).Scan(
			&r.ID, &r.RuleID, &r.DocType, &r.DocID, &r.Amount, &roles, &r.ManagerUser, &r.ManagerName, &r.AllowSelf,
			&r.Requester, &r.RequesterName, &r.Creator, &r.Claimant, &submitted, &r.Outcome,
			&r.DecidedBy, &r.DecidedByName, &decided, &r.Reason)
	}))
	MustOK(t, json.Unmarshal([]byte(roles), &r.ApproverRoles))
	r.SubmittedAt = parseTime(t, submitted)
	if decided != "" {
		r.DecidedAt = parseTime(t, decided)
	}
	return r
}

func parseTime(t testing.TB, s string) time.Time {
	t.Helper()
	v, err := types.ParseTimestamp(s)
	MustOK(t, err)
	return v.Time()
}

// Counter is how many numbers the driver's stand-in series prefix has used
// (0 when none; APR-072); under the engine driver, NAM's counter.
func (e *Env) Counter(t testing.TB, prefix string) int {
	t.Helper()
	if e.eng != nil {
		return e.engineCounter(t, prefix)
	}
	var n int
	MustOK(t, e.DB.Read(context.Background(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(context.Background(),
			`SELECT COALESCE((SELECT n FROM aprtest_counters WHERE prefix = ?), 0)`, prefix).Scan(&n)
	}))
	return n
}
