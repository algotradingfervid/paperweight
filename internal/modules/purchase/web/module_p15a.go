package purchaseweb

import (
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// P15a's Module stubs (Order wizard). Each keeps docscreen's generic
// behaviour; P15a replaces them.

// orderTermsStep is the Order's Terms step: the generic one.
func (m *module) orderTermsStep() docscreen.Step { return docscreen.Step{} }

// orderHide hides no Order field.
func (m *module) orderHide(docscreen.Ctx, doctype.Field) bool { return false }

// orderCheck adds nothing to the Order's check page.
func (m *module) orderCheck(docscreen.Ctx) (docscreen.CheckExtra, error) {
	return docscreen.CheckExtra{}, nil
}

// orderLineCard keeps an Order line's card.
func (m *module) orderLineCard(_ docscreen.Ctx, l docscreen.LineView) components.LineCard {
	return l.Card
}
