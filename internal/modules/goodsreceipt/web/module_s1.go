package grnweb

import (
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// S1's Module stubs (Choose purchase order and Delivery details). Each keeps
// docscreen's generic behaviour; S1 replaces them.

// stepLabels keeps docscreen's step labels.
func (m *module) stepLabels() map[string]string { return nil }

// detailsStep is the GRN's first step: the generic one.
func (m *module) detailsStep() docscreen.Step { return docscreen.Step{} }

// deliveryStep is the GRN's Delivery step: the generic one.
func (m *module) deliveryStep() docscreen.Step { return docscreen.Step{} }

// hide hides no GRN field.
func (m *module) hide(docscreen.Ctx, doctype.Field) bool { return false }
