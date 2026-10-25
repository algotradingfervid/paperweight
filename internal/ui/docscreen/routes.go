package docscreen

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Routes registers every /doc/{doctype} route. Each handler checks rights
// itself through auth.Service.Can (CD-46); WP3 mounts them with AUTH's
// CheckedInHandler guard. /ui/validate/{kind} is not here: it takes the Self
// guard (CD-87), so WP3 and the gallery mount components.ValidateHandler.
func Routes(d Deps) func(chi.Router) {
	return func(r chi.Router) {
		r.Get("/doc/{doctype}", d.list)
		r.Get("/doc/{doctype}/new", notYet)
		r.Post("/doc/{doctype}/new", notYet)
		r.Get("/doc/{doctype}/{id}", notYet)
		r.Get("/doc/{doctype}/{id}/edit/{step}", notYet)
		r.Post("/doc/{doctype}/{id}/edit/{step}", notYet)
		r.Get("/doc/{doctype}/{id}/lines/new", notYet)
		r.Post("/doc/{doctype}/{id}/lines/new", notYet)
		r.Get("/doc/{doctype}/{id}/lines/{no}", notYet)
		r.Post("/doc/{doctype}/{id}/lines/{no}", notYet)
		r.Get("/doc/{doctype}/{id}/lines/{no}/remove", notYet)
		r.Post("/doc/{doctype}/{id}/lines/{no}/remove", notYet)
		r.Post("/doc/{doctype}/{id}/lines/{no}/move", notYet)
		// {action} is an action (submit, withdraw, approve, revise, amend;
		// POST) or a reason page (reject, cancel, close, delete; GET and POST).
		r.Get("/doc/{doctype}/{id}/{action}", notYet)
		r.Post("/doc/{doctype}/{id}/{action}", notYet)
	}
}

// notYet answers the routes Tasks 13 and 14 fill.
func notYet(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, http.StatusText(http.StatusNotImplemented), http.StatusNotImplemented)
}
