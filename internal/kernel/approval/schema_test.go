package approval_test

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

func TestSchema_HistoryIsAppendOnlyAndOneOpenRequest(t *testing.T) {
	d := testkit.NewDB(t)
	ctx := t.Context()
	err := d.Write(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		rid, err := q.InsertRule(ctx, dbq.InsertRuleParams{DocType: "fixture.Order", FromAmount: 5000000,
			ApproverType: "role", ApproverRole: "Purchase Manager", CreatedAt: "2026-10-24T05:00:00.000Z", CreatedBy: 1})
		if err != nil {
			return err
		}
		req := dbq.InsertRequestParams{DocType: "fixture.Order", DocID: 42, RuleID: rid, Amount: 6000000,
			ApproverRoles: `["Purchase Manager","Director"]`, Requester: 4, RequesterName: "Priya Shah", Creator: 4,
			SubmittedAt: "2026-10-24T05:00:00.000Z", CreatedBy: 4}
		if _, err := q.InsertRequest(ctx, req); err != nil {
			return err
		}
		if _, err := q.InsertRequest(ctx, req); err == nil {
			t.Error("second open request for the same document was accepted")
		}
		hid, err := q.InsertHistory(ctx, dbq.InsertHistoryParams{DocType: "fixture.Order", DocID: 42, Action: "submitted",
			UserID: 4, UserName: "Priya Shah", At: "2026-10-24T05:00:00.000Z"})
		if err != nil {
			return err
		}
		_, uerr := tx.SQL().ExecContext(ctx, `UPDATE approval_history SET reason='x' WHERE id=?`, hid)
		_, derr := tx.SQL().ExecContext(ctx, `DELETE FROM approval_history WHERE id=?`, hid)
		for _, e := range []error{uerr, derr} {
			if e == nil || !strings.Contains(e.Error(), "approval history is append-only") {
				t.Errorf("got %v", e)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
