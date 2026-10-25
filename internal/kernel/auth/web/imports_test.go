package authweb_test

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// authweb and webtest must never import internal/ui (CD-49); only
// authscreens may.
func TestImports_NoUI(t *testing.T) {
	var stderr bytes.Buffer
	cmd := exec.Command("go", "list", "-f", "{{.ImportPath}} {{join .Imports \" \"}}", ".", "./webtest")
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, stderr.String())
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pkg, imports, _ := strings.Cut(line, " ")
		for _, imp := range strings.Fields(imports) {
			if imp == "github.com/vardhan/internaltools/internal/ui" || strings.HasPrefix(imp, "github.com/vardhan/internaltools/internal/ui/") {
				t.Errorf("%s imports %s; only authscreens may import internal/ui (CD-49)", pkg, imp)
			}
		}
	}
}
