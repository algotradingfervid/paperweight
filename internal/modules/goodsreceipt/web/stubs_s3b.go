package grnweb

import (
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// S3b's stub: the slot for the Bill details card on the document page
// (UX doc §5.7), drawn before the Purchase order card. S3b replaces this
// file.

// billPanel is the Bill details card of c's GRN; nil draws nothing.
func (m *module) billPanel(docscreen.Ctx) (templ.Component, error) {
	return nil, nil
}
