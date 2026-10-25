package doc

import "github.com/vardhan/internaltools/internal/kernel/naming"

// NumberFuncs is the seam over NAM's number rules (CD-28). A nil field means
// the naming package function; production never sets them. Display names are
// not in the seam: DOC calls naming.Label directly (CD-53).
type NumberFuncs struct {
	Guard          func(stored, supplied *string) error
	CheckAmendment func(root string, k int, docLabel string) error
}

// withDefaults fills every nil field with its naming package function.
func (n NumberFuncs) withDefaults() NumberFuncs {
	if n.Guard == nil {
		n.Guard = naming.GuardNumberField
	}
	if n.CheckAmendment == nil {
		n.CheckAmendment = naming.CheckAmendmentNo
	}
	return n
}
