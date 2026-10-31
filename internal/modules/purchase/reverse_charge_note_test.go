package purchase_test

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// P8c (CROSS-WP from AD-12): ReverseChargeNote is the print's reverse-charge
// note, read in the caller's tx, so GRN's print shows the same text.
func p8cNote(t *testing.T, env *purchasetest.Env, id int64) (string, error) {
	t.Helper()
	var note string
	err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		note, err = purchase.ReverseChargeNote(t.Context(), tx, id)
		return err
	})
	return note, err
}

func TestReverseChargeNote_On(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p8RefLines(t, env)...),
		purchasetest.OrderHeader(map[string]string{"reverse_charge": "true"}))
	got, err := p8cNote(t, env, po.ID)
	if want := "Reverse charge applies. GST of ₹14,364.00 is payable by us, not to the vendor."; err != nil || got != want {
		t.Errorf("note = %q, %v; want %q, nil", got, err, want)
	}
}

func TestReverseChargeNote_Off(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p8RefLines(t, env)...))
	if got, err := p8cNote(t, env, po.ID); err != nil || got != "" {
		t.Errorf("note = %q, %v; want \"\", nil", got, err)
	}
}

func TestReverseChargeNote_UnknownID(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	if got, err := p8cNote(t, env, po.ID+1000); !errors.Is(err, sql.ErrNoRows) || got != "" {
		t.Errorf("unknown id: note = %q, err = %v; want \"\", sql.ErrNoRows", got, err)
	}
}
