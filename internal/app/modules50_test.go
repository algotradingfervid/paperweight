package app_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/app"
	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// M0-50: Config.Modules takes only registered TestOnly DocTypes, so a test's
// Module never silently replaces a real one (maps.Copy in routes).
func TestApp_ConfigModulesTestOnly(t *testing.T) {
	for _, name := range []string{purchase.DocType, "m50.NotRegistered"} {
		t.Run(name, func(t *testing.T) {
			a, err := app.New(context.Background(), app.Config{
				DataDir: t.TempDir(), Clock: testkit.Clock(), LogOutput: io.Discard,
				Modules: map[string]docscreen.Module{name: {}},
			})
			if err == nil {
				_ = a.Close()
				t.Fatalf("app.New with a Module for %s = nil error, want a refusal", name)
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("app.New error = %q, want it to name %s", err, name)
			}
		})
	}
	t.Run(fixture.Order, func(t *testing.T) {
		newWorld(t, func(c *app.Config) { c.Modules = map[string]docscreen.Module{fixture.Order: {}} })
	})
}
