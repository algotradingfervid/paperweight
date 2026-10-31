package grnweb

import (
	"maps"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
)

// MOD-GRN S0 (CD-256): Modules has one Module for the GRN, with a custom
// step for details, delivery and items and every hook set, each calling its
// screen task's function. It asserts the wiring only: S1 to S3 build what
// the hooks do.
func TestModules_GRNWired(t *testing.T) {
	mods := Modules(Deps{})
	m, ok := mods[goodsreceipt.DocType]
	if !ok || len(mods) != 1 {
		t.Fatalf("Modules = %v, want one entry for %s", slices.Collect(maps.Keys(mods)), goodsreceipt.DocType)
	}
	var slugs []string
	for slug := range m.Steps {
		slugs = append(slugs, slug)
	}
	slices.Sort(slugs)
	if want := []string{"delivery", "details", "items"}; !slices.Equal(slugs, want) {
		t.Errorf("Steps = %v, want %v", slugs, want)
	}
	if m.Hide == nil || m.LineCard == nil || m.Check == nil || m.Page == nil || m.Reason == nil {
		t.Errorf("Module leaves a hook nil: %+v", m)
	}
}
