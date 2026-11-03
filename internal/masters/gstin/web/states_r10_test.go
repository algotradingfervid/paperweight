package gstinweb_test

import (
	"slices"
	"testing"

	gstinweb "github.com/vardhan/internaltools/internal/masters/gstin/web"
	"github.com/vardhan/internaltools/internal/ui/components"
)

func values(opts []components.Option) []string {
	out := make([]string, len(opts))
	for i, o := range opts {
		out[i] = o.Value
	}
	return out
}

// StateOptions starts blank and lists every state, Other Countries too for an
// address abroad; IndianStateOptions drops only what no branch can be in.
func TestStateOptions(t *testing.T) {
	all, indian := gstinweb.StateOptions(), gstinweb.IndianStateOptions()
	if all[0].Value != "" || all[0].Label != "Choose a state" || indian[0].Value != "" {
		t.Errorf("first options = %+v, %+v", all[0], indian[0])
	}
	if !slices.Contains(values(all), "96") {
		t.Error("StateOptions lacks Other Countries")
	}
	if slices.Contains(values(indian), "96") {
		t.Error("IndianStateOptions has Other Countries")
	}
	if !slices.Contains(values(indian), "27") {
		t.Error("IndianStateOptions lacks Maharashtra")
	}
	if len(indian) != len(all)-1 {
		t.Errorf("%d Indian options of %d", len(indian), len(all))
	}
}
