package approval_test

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

func preview(t *testing.T, e *at.Env, d at.Doc, amount types.Money) approval.Preview {
	t.Helper()
	var got approval.Preview
	at.MustOK(t, e.DB.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		got, err = e.A.Preview(context.Background(), tx, d.Type, d.ID, amount, e.Priya)
		return err
	}))
	return got
}

// Design-system §8.3: the check page's approval line.
func TestApprovalPreview(t *testing.T) {
	e := at.New(t)
	small := e.NewDoc(t, at.Order, 1, at.Rs(1000), e.Priya)
	if got, want := preview(t, e, small, at.Rs(1000)), (approval.Preview{Text: approval.MsgNoApproval}); got != want {
		t.Errorf("no rule: %+v, want %+v", got, want)
	}
	big := e.NewDoc(t, at.Order, 2, at.Rs(60000), e.Priya)
	got := preview(t, e, big, at.Rs(60000))
	if want := "This purchase order needs approval from a Purchase Manager or Director because it is ₹50,000 or more."; !got.Needed || got.Text != want {
		t.Errorf("a rule: %+v, want %q", got, want)
	}
}
