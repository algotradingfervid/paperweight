package purchaseweb

import "github.com/vardhan/internaltools/internal/ui/docscreen"

// P15c's Module stub (Add from requests). It keeps docscreen's generic
// behaviour; P15c replaces it.

// orderItemsStep is the Order's Items step: the generic one.
func (m *module) orderItemsStep() docscreen.Step { return docscreen.Step{} }
