package approval

import (
	"reflect"
	"testing"

	"github.com/vardhan/internaltools/internal/types"
)

func po(id int64, from types.Money, role string) Rule {
	return Rule{ID: id, RuleInput: RuleInput{DocType: "fixture.Order", FromAmount: from, ApproverType: ApproverRole, Role: role}, Enabled: true}
}

var r1, r2 = po(1, 5000000, "Purchase Manager"), po(2, 50000000, "Director")

func TestApprovalMatch_EqualToThresholdNeedsApproval(t *testing.T) { // APR-017
	m, _, ok := match([]Rule{r1}, 5000000)
	if !ok || m.ID != 1 {
		t.Fatalf("₹50,000.00 must match R1, got ok=%v id=%d", ok, m.ID)
	}
}

func TestApprovalMatch_ThresholdTable(t *testing.T) { // APR-018
	for amt, want := range map[types.Money]bool{4999999: false, 5000000: true, 5000001: true, 0: false} {
		if _, _, ok := match([]Rule{r1}, amt); ok != want {
			t.Errorf("amount %d: needs=%v want %v", amt, ok, want)
		}
	}
	if _, _, ok := match([]Rule{po(3, 0, "Purchase Manager")}, 0); !ok {
		t.Error("₹0.00 rule must match ₹0.00")
	}
}

func TestApprovalMatch_HighestThresholdWins(t *testing.T) { // APR-021
	m, higher, _ := match([]Rule{r2, r1}, 6000000)
	if m.ID != 1 || len(higher) != 1 || higher[0].ID != 2 {
		t.Errorf("₹60,000: %d %v", m.ID, higher)
	}
	if got, want := approverRoles(m, higher), []string{"Purchase Manager", "Director"}; !reflect.DeepEqual(got, want) {
		t.Errorf("₹60,000 approver roles = %q, want %q", got, want)
	}
	m, higher, _ = match([]Rule{r1, r2}, 60000000)
	if m.ID != 2 || len(higher) != 0 {
		t.Errorf("₹6,00,000: %d %v", m.ID, higher)
	}
}

func TestApprovalMatch_OtherDocTypeIgnored(t *testing.T) { // APR-022 (unit half; Evaluate only passes rules of the document's type)
	if _, _, ok := match(nil, 100000000); ok {
		t.Error("no PO rules → no approval for ₹10,00,000.00")
	}
	// Control: with the PO rules present the same amount does need approval (R2),
	// so the empty-rules result above comes from the missing rules, not the amount.
	if m, _, ok := match([]Rule{r1, r2}, 100000000); !ok || m.ID != 2 {
		t.Errorf("₹10,00,000.00 with PO rules: ok=%v id=%d, want R2", ok, m.ID)
	}
}
