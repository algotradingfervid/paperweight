package app

import (
	"net/http"

	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

// newPage serves GET /new, the "Create new" sheet as a page for browsers
// without JavaScript (UI v2 §5.2, N2): the shell already holds what the user
// may create.
func newPage(w http.ResponseWriter, r *http.Request) {
	tiles := components.NewTiles(ui.ShellFrom(r.Context()).New, false)
	ui.Render(w, r, http.StatusOK, layout.Simple("Create new", tiles), nil)
}
