package internal_test

import (
	"errors"
	"fmt"
	"go/build"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// moduleOrder is PO spec §14.5: a module imports only earlier modules.
var moduleOrder = []string{"modkit", "purchase", "goodsreceipt", "expense"}

// moduleImportRule reports whether package pkg under internal/modules may
// import imp (both module-relative). Only a module's web tree
// (internal/modules/<m>/web and folders under it) may reach internal/ui
// (CD-237); a service package imports no UI, app or web package, so it can't
// reach internal/ui through one either.
func moduleImportRule(pkg, imp string) (bool, string) {
	parts := strings.Split(strings.TrimPrefix(pkg, "internal/modules/"), "/")
	mine := slices.Index(moduleOrder, parts[0])
	if mine < 0 {
		return false, fmt.Sprintf("%s is not in the module order %v (PO spec §14.5)", pkg, moduleOrder)
	}
	isWeb := len(parts) > 1 && parts[1] == "web"
	if !isWeb && (underOrEqual(imp, "internal/ui") || underOrEqual(imp, "internal/app") ||
		underOrEqual(imp, "internal/platform/web") || slices.Contains(strings.Split(imp, "/"), "web")) {
		return false, pkg + " is a module service package; it may not import internal/ui, internal/app or any web package, which only a module's web tree may reach (CD-237)"
	}
	if underOrEqual(imp, "internal/modules") {
		other := strings.Split(strings.TrimPrefix(imp, "internal/modules/"), "/")[0]
		if slices.Index(moduleOrder, other) > mine {
			return false, fmt.Sprintf("module %s may import only earlier modules in %v (PO spec §14.5)", parts[0], moduleOrder)
		}
	}
	return true, ""
}

func TestModules_ImportsFollowOrder(t *testing.T) {
	t.Run("real_tree", func(t *testing.T) {
		seen := map[string]bool{}
		err := filepath.WalkDir("modules", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				return nil
			}
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			bp, err := build.ImportDir(path, 0)
			var noGo *build.NoGoError
			if errors.As(err, &noGo) {
				return nil
			}
			if err != nil {
				t.Errorf("go/build cannot read %s: %v", path, err)
				return nil
			}
			pkg := "internal/" + filepath.ToSlash(path)
			seen[pkg] = true
			for _, imp := range bp.Imports {
				if !strings.HasPrefix(imp, modulePath+"/") {
					continue
				}
				rel := strings.TrimPrefix(imp, modulePath+"/")
				if ok, why := moduleImportRule(pkg, rel); !ok {
					t.Errorf("%s imports %s, which breaks the rule: %s", pkg, rel, why)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking internal/modules: %v", err)
		}
		// Floors: a rename must not leave this test green with nothing scanned.
		for _, p := range []string{
			"internal/modules/modkit", "internal/modules/modkit/web",
			"internal/modules/purchase", "internal/modules/purchase/web", "internal/modules/purchase/purchasetest",
		} {
			if !seen[p] {
				t.Errorf("package %s was not scanned", p)
			}
		}
	})

	cases := []struct {
		name, pkg, imp string
		want           bool
	}{
		{"modkit_imports_kernel", "internal/modules/modkit", "internal/kernel/doc", true},
		{"modkit_imports_masters", "internal/modules/modkit", "internal/masters/party", true},
		{"modkit_imports_ui", "internal/modules/modkit", "internal/ui", false},
		{"modkit_imports_ui_components", "internal/modules/modkit", "internal/ui/components", false},
		{"modkit_dbq_imports_ui", "internal/modules/modkit/dbq", "internal/ui", false},
		{"modkit_web_imports_ui", "internal/modules/modkit/web", "internal/ui/components", true},
		{"purchase_imports_ui", "internal/modules/purchase", "internal/ui", false},
		{"purchasetest_imports_ui", "internal/modules/purchase/purchasetest", "internal/ui", false},
		{"purchase_web_imports_ui", "internal/modules/purchase/web", "internal/ui", true},
		{"purchase_web_views_imports_ui", "internal/modules/purchase/web/views", "internal/ui/docscreen", true},
		{"purchase_imports_modkit", "internal/modules/purchase", "internal/modules/modkit", true},
		{"modkit_imports_purchase", "internal/modules/modkit", "internal/modules/purchase", false},
		{"modkit_web_imports_purchase_web", "internal/modules/modkit/web", "internal/modules/purchase/web", false},
		{"goodsreceipt_imports_purchase", "internal/modules/goodsreceipt", "internal/modules/purchase", true},
		{"purchase_imports_goodsreceipt", "internal/modules/purchase", "internal/modules/goodsreceipt", false},
		{"purchase_imports_expense", "internal/modules/purchase/web", "internal/modules/expense", false},
		{"expense_imports_goodsreceipt", "internal/modules/expense", "internal/modules/goodsreceipt", true},
		{"purchase_imports_own_dbq", "internal/modules/purchase", "internal/modules/purchase/dbq", true},
		{"purchase_imports_modkit_web", "internal/modules/purchase", "internal/modules/modkit/web", false},
		{"purchase_imports_approval_web", "internal/modules/purchase", "internal/kernel/approval/web", false},
		{"purchase_imports_app", "internal/modules/purchase", "internal/app", false},
		{"purchase_imports_platform_web", "internal/modules/purchase", "internal/platform/web", false},
		{"purchase_imports_own_web", "internal/modules/purchase", "internal/modules/purchase/web", false},
		{"purchase_dbq_web_imports_ui", "internal/modules/purchase/dbq/web", "internal/ui", false},
		{"purchase_web_imports_modkit_web", "internal/modules/purchase/web", "internal/modules/modkit/web", true},
		{"unknown_module", "internal/modules/sales", "internal/types", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got, _ := moduleImportRule(c.pkg, c.imp); got != c.want {
				t.Errorf("moduleImportRule(%q, %q) = %v, want %v", c.pkg, c.imp, got, c.want)
			}
		})
	}
}
