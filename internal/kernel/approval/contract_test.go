package approval_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
)

// TestContract_Approval runs the approval contract suite against the real
// service only (WP3 Task 1, CD-182, CD-184).
func TestContract_Approval(t *testing.T) {
	approvaltest.Run(t, func(t *testing.T) (approval.Service, approvaltest.Setup) {
		e := approvaltest.New(t)
		return e.A, e.ContractSetup()
	})
}
