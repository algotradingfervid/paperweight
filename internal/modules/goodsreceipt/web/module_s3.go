package grnweb

import (
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// S3's Module stubs (Check page, document page, cancel reason). Each keeps
// docscreen's generic behaviour; S3 replaces them.

// check adds nothing to the GRN's check page.
func (m *module) check(docscreen.Ctx) (docscreen.CheckExtra, error) {
	return docscreen.CheckExtra{}, nil
}

// page adds nothing to the GRN's document page.
func (m *module) page(docscreen.Ctx) (docscreen.PageExtra, error) {
	return docscreen.PageExtra{}, nil
}

// reason adds nothing to the GRN's reason pages.
func (m *module) reason(docscreen.Ctx, doctype.Action) (docscreen.ReasonExtra, error) {
	return docscreen.ReasonExtra{}, nil
}
