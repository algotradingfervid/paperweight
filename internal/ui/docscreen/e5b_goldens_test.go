package docscreen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A-26 across the module_none goldens: every rendered document page (one
// with the header card) has exactly one current step.
func TestGoldens_OneCurrentStep(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "module_none", "*.golden"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no goldens: %v", err)
	}
	pages := 0
	for _, f := range files {
		b, err := os.ReadFile(f) //nolint:gosec // G304: the test reads its own testdata goldens
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		if !strings.Contains(s, `class="doc-head"`) {
			continue
		}
		pages++
		if n := strings.Count(s, `aria-current="step"`); n != 1 {
			t.Errorf("%s: %d current steps, want exactly one", filepath.Base(f), n)
		}
	}
	if pages < 5 {
		t.Errorf("only %d document pages among the goldens, want the five states at least", pages)
	}
}
