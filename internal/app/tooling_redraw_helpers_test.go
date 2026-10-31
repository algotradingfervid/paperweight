package app

import "github.com/vardhan/internaltools/internal/ui/docscreen"

// ToolingRedrawDeps is the app's own docscreen.Deps, so the redraw sweep
// (tooling_redraw_sweep_test.go) mounts the real screens with a probe Module
// added over whatever Modules the app sets.
func (a *App) ToolingRedrawDeps() docscreen.Deps { return a.docDeps() }
