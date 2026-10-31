package app

import (
	"github.com/alexedwards/scs/v2"

	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// ToolingRedrawDeps is the app's own docscreen.Deps, so the redraw sweep
// (tooling_redraw_sweep_test.go) mounts the real screens with a probe Module
// added over whatever Modules the app sets.
func (a *App) ToolingRedrawDeps() docscreen.Deps { return a.docDeps() }

// ToolingRedrawSessions is the session manager the app's Modules were built
// with: the sweep's router loads it, as AUTH's middleware does, so a page
// hook that reads the session (purchase's comments card) finds it.
func (a *App) ToolingRedrawSessions() *scs.SessionManager { return a.sm }

// ToolingExpense is the app's expense Service, so the sweep can mark an
// advance as paid through MarkPaid.
func (a *App) ToolingExpense() *expense.Service { return a.expense }
