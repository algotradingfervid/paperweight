package goodsreceipt_test

import (
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// MOD-GRN G2b (CD-317, as amended): the list's ?q= finds a GRN by its
// number and by the vendor's challan number (the two GRNs have different
// challans), a q matching neither finds nothing, and restricted Suresh's q
// never returns the GRN on TECH-SITE that admin's same q does. Through
// list.Service, the path /doc/{doctype} serves ?q= on.
func TestGRN_Search(t *testing.T) {
	env := grntest.New(t)
	po := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved())
	metro := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "40"}}, doctype.StateApproved)
	techPO := grntest.POAs(t, env, env.Admin, doctype.StateApproved, map[string]string{
		"site_id": g1MasterID(t, env.DB, "site", "TECH-SITE"), "project_id": g1MasterID(t, env.DB, "project", "PRJ-TECHPARK"),
	})
	h := g2Header(t, env, techPO)
	h["challan_no"] = "DC-7781"
	techID := g2MustCreate(t, env, env.Admin, h, g2Line(techPO.Lines[0], "10", "", "")).ID
	if _, err := g2Submit(t, env, env.Admin, techID); err != nil {
		t.Fatalf("submit the TECH-SITE GRN: %v", err)
	}
	tech := env.Load(t, goodsreceipt.DocType, techID)
	if metro.Number != "GR/26-27/00001" || tech.Number != "GR/26-27/00002" {
		t.Fatalf("GRN numbers = %q, %q; want GR/26-27/00001, GR/26-27/00002", metro.Number, tech.Number)
	}

	for _, c := range []struct {
		name string
		as   auth.Principal
		q    string
		want []int64
	}{
		{"number", env.Admin, "GR/26-27/00001", []int64{metro.ID}},
		{"challan_no", env.Admin, "DC-1042", []int64{metro.ID}},
		{"no match", env.Admin, "ZZ-NOTHING", nil},
		{"admin, TECH-SITE number", env.Admin, "GR/26-27/00002", []int64{tech.ID}},
		{"admin, TECH-SITE challan_no", env.Admin, "DC-7781", []int64{tech.ID}},
		{"Suresh, own number", env.Suresh, "GR/26-27/00001", []int64{metro.ID}},
		{"Suresh, TECH-SITE number", env.Suresh, "GR/26-27/00002", nil},
		{"Suresh, TECH-SITE challan_no", env.Suresh, "DC-7781", nil},
	} {
		if got := g2bSearch(t, env, c.as, c.q); !slices.Equal(got, c.want) {
			t.Errorf("%s: ?q=%s as %s = ids %v, want %v", c.name, c.q, c.as.Name, got, c.want)
		}
	}
}

// g2bSearch is the sorted ids of the GRNs list.Service returns p for q.
func g2bSearch(t *testing.T, env *grntest.Env, p auth.Principal, q string) []int64 {
	t.Helper()
	dt, _ := doctype.Lookup(goodsreceipt.DocType)
	svc := &list.Service{DB: env.DB, Auth: env.Auth}
	res, err := svc.List(t.Context(), p, dt, list.Query{Search: q, Page: 1, PageSize: 500})
	if err != nil {
		t.Fatalf("list GRNs as %s with q %q: %v", p.Name, q, err)
	}
	var ids []int64
	for _, r := range res.Rows {
		ids = append(ids, r.ID)
	}
	slices.Sort(ids)
	return ids
}
