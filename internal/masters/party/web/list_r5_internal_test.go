package partyweb

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// R5: a list row's address read that fails for a reason other than a
// refused right fails the row, never shows it as if addresses were hidden.
// The failure is a finished transaction: its reads fail as a closed database
// would.
func TestListValues_ReadFailureIsAnError(t *testing.T) {
	e := msttest.New(t)
	s := records{env: e.Kit, addresses: address.New(e.Kit)}
	ctx := context.Background()
	row := list.Row{ID: 1, Values: map[string]any{"is_vendor": true}}
	var live map[string]string
	var liveErr error
	var done *db.Tx
	if err := e.Kit.DB.Read(ctx, func(tx *db.Tx) error {
		done = tx
		live, liveErr = s.listValues(ctx, tx, e.Kavya, row)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if liveErr != nil || live[roleField] != "Vendor" {
		t.Fatalf("live read = %v, %v; want the role and no error", live, liveErr)
	}
	if got, err := s.listValues(ctx, done, e.Kavya, row); err == nil {
		t.Errorf("listValues on a finished transaction = %v, nil; want the read error", got)
	}
}
