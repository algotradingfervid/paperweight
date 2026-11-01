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
// (CanTx read, as Page and Facts) and gets no line.
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

// Lines reads the GRN with CanTx, as Page, Facts and BillCard do (CD-245;
// CD-312's CheckLinkTx is for link targets): a hidden or missing GRN is
// AUTH's 404. Priya, restricted to project PRJ-METRO on the real
// auth.Service, gets the lines of Suresh's METRO-SITE GRN (project scope,
// not her own) and the 404 for the Administrator's TECH-SITE GRN. An unknown
// id is the 404 for the unrestricted Administrator too, not the raw no-rows
// error.
func TestGRN_LinesReadsLikeBillCard(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	g := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "100"), doctype.StateApproved)
	tech := g6TechPO(t, env)
	hidden := env.GRN(t, env.Admin, tech.ID, g4Line1(tech, "100"), doctype.StateApproved)

	if got, err := env.Service.Lines(t.Context(), env.Priya, g.ID); err != nil || len(got) != 1 {
		t.Errorf("Priya, Suresh's METRO-SITE GRN: %+v %v, want its line", got, err)
	}
	for _, c := range []struct {
		name string
		p    auth.Principal
		id   int64
	}{
		{"Priya, the TECH-SITE GRN", env.Priya, hidden.ID},
		{"Priya, an unknown id", env.Priya, 999999},
		{"the Administrator, an unknown id", env.Admin, 999999},
	} {
		_, linesErr := env.Service.Lines(t.Context(), c.p, c.id)
		_, cardErr := env.Service.BillCard(t.Context(), c.p, c.id)
		if !errors.Is(linesErr, auth.ErrNotFound) || !errors.Is(cardErr, auth.ErrNotFound) {
			t.Errorf("%s: Lines %v, BillCard %v; want auth.ErrNotFound from both", c.name, linesErr, cardErr)
		}
	}
}
