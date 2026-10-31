package purchase_test

import (
	"errors"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-PO P15e (UX §5.13, §5.20; spec §23; AD-13 ruling on P15e): SendState
// and ExpectedState carry the page summary from the PO's snapshots (vendor,
// the rounded total as HandOverState formats it, and required by), the Send
// and Expected on pages' today for their date buttons and the sent record's
// author by name. Each value is asserted, so deleting its fill fails.
func TestPO_PageStates(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	svc := env.Service
	// Just after IST midnight on 25-10 (18:31 UTC on 24-10): today is 25-10.
	env.SetNow(t, time.Date(2026, time.October, 24, 18, 31, 0, 0, time.UTC))
	po := purchasetest.NewOrder(t, env, purchasetest.OrderHeader(map[string]string{"required_by": "28-10-2026"}))
	const vendor, total = "Shree Steel Traders", "₹7,375.00" // 100 kg × ₹62.50 + 18% GST

	t.Run("SendState", func(t *testing.T) {
		st, err := svc.SendState(ctx, env.Priya, po.ID)
		if err != nil {
			t.Fatalf("SendState: %v", err)
		}
		today := types.FormatDate(st.Today)
		if st.Vendor != vendor || st.Total != total || today != "25-10-2026" || st.SentByName != "" {
			t.Errorf("SendState = Vendor %q, Total %q, Today %s, SentByName %q; want %q, %q, 25-10-2026, blank",
				st.Vendor, st.Total, today, st.SentByName, vendor, total)
		}
		on, _ := types.ParseDate("25-10-2026")
		if err := svc.MarkSent(ctx, env.Priya, po.ID, 0, purchase.SentInput{SentOn: on, SentVia: "whatsapp"}); err != nil {
			t.Fatalf("MarkSent: %v", err)
		}
		st, err = svc.SendState(ctx, env.Ravi, po.ID)
		if err != nil || st.SentByName != "Priya Shah" {
			t.Errorf("SendState after Priya marked it = SentByName %q, %v; want %q", st.SentByName, err, "Priya Shah")
		}
	})

	t.Run("ExpectedState", func(t *testing.T) {
		st, err := svc.ExpectedState(ctx, env.Priya, po.ID)
		if err != nil {
			t.Fatalf("ExpectedState: %v", err)
		}
		if got, today := types.FormatDate(st.RequiredBy), types.FormatDate(st.Today); st.Vendor != vendor || st.Total != total || got != "28-10-2026" || today != "25-10-2026" {
			t.Errorf("ExpectedState = Vendor %q, Total %q, RequiredBy %s, Today %s; want %q, %q, 28-10-2026, 25-10-2026", st.Vendor, st.Total, got, today, vendor, total)
		}
		blank := purchasetest.NewOrder(t, env)
		st, err = svc.ExpectedState(ctx, env.Priya, blank.ID)
		if err != nil || st.RequiredBy != (types.Date{}) {
			t.Errorf("ExpectedState with no required by = %v, %v; want the zero date", st.RequiredBy, err)
		}
	})
}

// MOD-PO P15e: Service.Can is AUTH's check with no record, on the real
// auth.Service: Priya (Purchase User) may create orders, Suresh (Stores)
// may not and gets AUTH's refusal.
func TestPO_Can(t *testing.T) {
	env := purchasetest.New(t)
	if err := env.Service.Can(t.Context(), env.Priya, purchase.DocType, auth.RightCreate); err != nil {
		t.Errorf("Can(Priya, create Order) = %v, want nil", err)
	}
	if err := env.Service.Can(t.Context(), env.Suresh, purchase.DocType, auth.RightCreate); !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("Can(Suresh, create Order) = %v, want AUTH's refusal", err)
	}
}
