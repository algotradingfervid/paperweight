package purchaseweb

import (
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// P15b's Module stubs (Order page). Each keeps docscreen's generic
// behaviour; P15b replaces them.

// orderPage adds nothing to the Order's document page.
func (m *module) orderPage(docscreen.Ctx) (docscreen.PageExtra, error) {
	return docscreen.PageExtra{}, nil
}

// orderReason adds nothing to the Order's reason pages.
func (m *module) orderReason(docscreen.Ctx, doctype.Action) (docscreen.ReasonExtra, error) {
	return docscreen.ReasonExtra{}, nil
}
