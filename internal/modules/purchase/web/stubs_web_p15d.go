package purchaseweb

import (
	"net/http"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// P15d's stubs (request screens). P15d deletes this file and adds its own.

func (h *handlers) fromRequestPage(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }

// renderFromRequest redraws the Create PO page with status and errs; P13's
// POST calls it for a refusal other than 403 and 404. The page reloads its
// own view from the path id and reads the typed values from r.PostForm.
var renderFromRequest = func(w http.ResponseWriter, _ *http.Request, status int, errs []doctype.FieldError) { //nolint:unused // called by P13, built by P15d
	plainErrors(w, status, errs)
}
