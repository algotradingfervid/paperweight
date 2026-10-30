package allmasters

import (
	"github.com/go-chi/chi/v5"

	uomweb "github.com/vardhan/internaltools/internal/masters/uom/web"
)

// Routes mounts every master's screens under /m/{doctype-slug}/ and their
// pickers at /pick/{DocType} (MST Task 22, CD-195); WP3 mounts it behind
// AUTH's middleware. Each master adds one line.
func (s *Services) Routes(r chi.Router) {
	uomweb.Routes(s.UoM, s.env)(r)
}
