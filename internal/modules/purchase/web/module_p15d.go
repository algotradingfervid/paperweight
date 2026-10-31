package purchaseweb

import (
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// P15d's Module stubs (Request screens). Each keeps docscreen's generic
// behaviour; P15d replaces them.

// requestCardsOnly keeps the Request's desktop Items columns.
const requestCardsOnly = false

// requestPage adds nothing to the Request's document page.
func (m *module) requestPage(docscreen.Ctx) (docscreen.PageExtra, error) {
	return docscreen.PageExtra{}, nil
}

// requestReason adds nothing to the Request's reason pages.
func (m *module) requestReason(docscreen.Ctx, doctype.Action) (docscreen.ReasonExtra, error) {
	return docscreen.ReasonExtra{}, nil
}

// requestHide hides no Request field.
func (m *module) requestHide(docscreen.Ctx, doctype.Field) bool { return false }

// requestCheck adds nothing to the Request's check page.
func (m *module) requestCheck(docscreen.Ctx) (docscreen.CheckExtra, error) {
	return docscreen.CheckExtra{}, nil
}

// requestLineCard keeps a Request line's card.
func (m *module) requestLineCard(_ docscreen.Ctx, l docscreen.LineView) components.LineCard {
	return l.Card
}
