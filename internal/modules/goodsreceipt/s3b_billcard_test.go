package goodsreceipt_test

import (
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
)

// MOD-GRN S3b (UX doc §5.7; CD-245): BillCard reads a GRN only for a viewer
// who can read it. Suresh, restricted to PUNE-WH and METRO-SITE on the real
// auth.Service, gets AUTH's 404 for the Administrator's TECH-SITE GRN and
// the card of his own approved GRN, with write.
func TestGRNBill_CardRead(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	mine := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "100"), doctype.StateApproved)
	tech := g6TechPO(t, env)
	hidden := env.GRN(t, env.Admin, tech.ID, g4Line1(tech, "100"), doctype.StateApproved)

	if _, err := env.Service.BillCard(t.Context(), env.Suresh, hidden.ID); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("Suresh, the TECH-SITE GRN: %v, want auth.ErrNotFound", err)
	}
	if _, err := env.Service.BillCard(t.Context(), env.Admin, 999999); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("an unknown id: %v, want auth.ErrNotFound", err)
	}
	c, err := env.Service.BillCard(t.Context(), env.Suresh, mine.ID)
	if err != nil || c.State != doctype.StateApproved || !c.CanWrite || c.Bill.Version != 0 {
		t.Errorf("Suresh, his GRN: %+v %v, want approved, writable, no bill", c, err)
	}
}
