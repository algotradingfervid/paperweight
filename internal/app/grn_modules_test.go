package app_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// MOD-GRN S0: the app wires grnweb's Module for the GRN into docscreen's
// Deps.Modules beside purchase's. Mounted only; S1 to S3 test what it does.
func TestApp_GRNModuleWired(t *testing.T) {
	w := newWorld(t)
	mods := w.app.DocDeps().Modules
	for _, dt := range []string{goodsreceipt.DocType, purchase.DocType, purchase.RequestDocType} {
		if _, ok := mods[dt]; !ok {
			t.Errorf("docscreen Deps.Modules has no %s entry", dt)
		}
	}
}
