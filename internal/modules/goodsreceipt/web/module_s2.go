package grnweb

import (
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// S2's Module stubs (Received quantities). Each keeps docscreen's generic
// behaviour; S2 replaces them.

// itemsStep is the GRN's Items step: the generic one.
func (m *module) itemsStep() docscreen.Step { return docscreen.Step{} }

// noLinePages keeps docscreen's line pages.
func (m *module) noLinePages() bool { return false }
