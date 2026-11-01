package goodsreceipt_test

import (
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
)

// S3a: Service.Page reads only for a viewer of the GRN (CheckLinkTx, as
// Facts): a user with no GRN rights is refused and gets nothing.
func TestGRN_PageNeedsRead(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	g := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "800"}}, doctype.StateApproved)
	if _, err := env.Service.Page(t.Context(), env.Suresh, g.ID); err != nil {
		t.Fatalf("Page as Suresh: %v", err)
	}
	rp, err := env.Service.Page(t.Context(), env.NoReceiptRights(t), g.ID)
	if !errors.Is(err, auth.ErrForbidden) || rp.PO != nil || rp.Site != "" {
		t.Errorf("Page without GRN read = %+v, %v; want nothing and auth.ErrForbidden", rp, err)
	}
}
