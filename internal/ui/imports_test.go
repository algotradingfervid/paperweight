package ui_test

import (
	"os/exec"
	"strings"
	"testing"
)

// TestUI_NoAuthwebImport guards CD-88: AUTH's views import UI, never the
// other way round.
func TestUI_NoAuthwebImport(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "github.com/vardhan/internaltools/internal/ui/...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps ./internal/ui/...: %v\n%s", err, out)
	}
	deps := string(out)
	if !strings.Contains(deps, "github.com/vardhan/internaltools/internal/ui/components") {
		t.Fatalf("go list -deps output does not list internal/ui/components:\n%s", deps)
	}
	for _, p := range strings.Fields(deps) {
		if p == "github.com/vardhan/internaltools/internal/kernel/auth/web" || strings.HasPrefix(p, "github.com/vardhan/internaltools/internal/kernel/auth/web/") {
			t.Errorf("internal/ui depends on %s (CD-88)", p)
		}
	}
}
