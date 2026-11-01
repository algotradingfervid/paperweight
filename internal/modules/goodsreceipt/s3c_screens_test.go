package goodsreceipt_test

import (
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/types"
)

// S3c: Service.Lines gives a viewer of the GRN each line's PO line number
// and its own snapshots, by line id; a user with no GRN rights is refused
// (CheckLinkTx, as Page and Facts) and gets no line.
func TestGRN_LinesNeedRead(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	g := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[1].ID, Received: "50"}}, doctype.StateApproved)
	got, err := env.Service.Lines(t.Context(), env.Suresh, g.ID)
	if err != nil {
		t.Fatalf("Lines as Suresh: %v", err)
	}
	want := goodsreceipt.ReceiptLine{PONo: 2, ItemCode: "WIRE-18G", ItemName: "Binding Wire 18G", UoM: "KGS", Rate: types.Rate(960_000)}
	if len(got) != 1 || got[g.Lines[0].ID] != want {
		t.Errorf("Lines as Suresh = %+v, want {%d: %+v}", got, g.Lines[0].ID, want)
	}
	got, err = env.Service.Lines(t.Context(), env.NoReceiptRights(t), g.ID)
	if !errors.Is(err, auth.ErrForbidden) || len(got) != 0 {
		t.Errorf("Lines without GRN read = %+v, %v; want nothing and auth.ErrForbidden", got, err)
	}
}
