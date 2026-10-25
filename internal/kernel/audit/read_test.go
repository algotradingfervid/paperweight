package audit_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// An entry appended with nil Changes reads back with nil Changes, and one
// appended with an empty slice reads back with an empty slice.
func TestAudit_ForDoc_NilChangesRoundTrip(t *testing.T) {
	d := testkit.NewDB(t)
	nilChanges := audit.Entry{
		DocType: "fixture.Order", DocID: 1, Action: "submit",
		From: "draft", To: "pending_approval",
		By: types.UserID(4), At: testkit.Clock().Now(),
	}
	emptyChanges := nilChanges
	emptyChanges.Changes = []audit.Change{}
	appendEntry(t, d, audit.Store{}, nilChanges)
	appendEntry(t, d, audit.Store{}, emptyChanges)

	got := forDoc(t, d, "fixture.Order", 1)
	if len(got) != 2 {
		t.Fatalf("ForDoc returned %d entries, want 2: %+v", len(got), got)
	}
	if got[0].Changes != nil || !equalIgnoringAt(got[0], nilChanges) {
		t.Errorf("ForDoc[0] = %+v, want %+v (Changes nil)", got[0], nilChanges)
	}
	if got[1].Changes == nil || !equalIgnoringAt(got[1], emptyChanges) {
		t.Errorf("ForDoc[1] = %+v, want %+v (Changes empty, not nil)", got[1], emptyChanges)
	}
}
