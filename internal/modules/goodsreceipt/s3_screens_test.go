package goodsreceipt_test

import (
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
)

// S3a: Service.Page reads only for a viewer of the GRN (CanTx, as
// BillCard): a user with no GRN rights is refused and gets nothing.
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

// Page reads the GRN with CanTx, as BillCard does (CD-245): a hidden or
// missing GRN is AUTH's 404. Priya, restricted to project PRJ-METRO on the
// real auth.Service with read but no write on GRNs, gets the page of
// Suresh's METRO-SITE GRN (project scope, not her own) and the 404 for the
// Administrator's TECH-SITE GRN.
func TestGRN_PageReadsLikeBillCard(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	g := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "100"), doctype.StateApproved)
	tech := g6TechPO(t, env)
	hidden := env.GRN(t, env.Admin, tech.ID, g4Line1(tech, "100"), doctype.StateApproved)

	if rp, err := env.Service.Page(t.Context(), env.Priya, g.ID); err != nil || rp.Site == "" {
		t.Errorf("Priya, Suresh's METRO-SITE GRN: %+v %v, want the page", rp, err)
	}
	for _, c := range []struct {
		name string
		id   int64
	}{{"Priya, the TECH-SITE GRN", hidden.ID}, {"an unknown id", 999999}} {
		_, pageErr := env.Service.Page(t.Context(), env.Priya, c.id)
		_, cardErr := env.Service.BillCard(t.Context(), env.Priya, c.id)
		if !errors.Is(pageErr, auth.ErrNotFound) || !errors.Is(cardErr, auth.ErrNotFound) {
			t.Errorf("%s: Page %v, BillCard %v; want auth.ErrNotFound from both", c.name, pageErr, cardErr)
		}
	}
}

// Facts reads the GRN with CanTx, as Page and BillCard do (CD-245; CD-312's
// CheckLinkTx is for link targets): a hidden or missing GRN is AUTH's 404.
// Priya, restricted to project PRJ-METRO on the real auth.Service, gets the
// facts of Suresh's METRO-SITE GRN (project scope, not her own) and the 404
// for the Administrator's TECH-SITE GRN. An unknown id is the 404 for the
// unrestricted Administrator too, not the raw no-rows error.
func TestGRN_FactsReadsLikeBillCard(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	g := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "100"), doctype.StateApproved)
	tech := g6TechPO(t, env)
	hidden := env.GRN(t, env.Admin, tech.ID, g4Line1(tech, "100"), doctype.StateApproved)

	if f, err := env.Service.Facts(t.Context(), env.Priya, g.ID); err != nil || f.SiteName != "METRO-SITE" {
		t.Errorf("Priya, Suresh's METRO-SITE GRN: %+v %v, want its facts", f, err)
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
		_, factsErr := env.Service.Facts(t.Context(), c.p, c.id)
		_, cardErr := env.Service.BillCard(t.Context(), c.p, c.id)
		if !errors.Is(factsErr, auth.ErrNotFound) || !errors.Is(cardErr, auth.ErrNotFound) {
			t.Errorf("%s: Facts %v, BillCard %v; want auth.ErrNotFound from both", c.name, factsErr, cardErr)
		}
	}
}
