package docscreen

import "github.com/go-chi/chi/v5"

// Routes registers every /doc/{doctype} route. Each handler checks rights
// itself through auth.Service.Can (CD-46); WP3 mounts them with AUTH's
// CheckedInHandler guard, except /search, which takes Self (see search).
// /ui/validate/{kind} is not here: it takes the Self guard (CD-87), so WP3
// and the gallery mount components.ValidateHandler.
func Routes(d Deps) func(chi.Router) {
	return func(r chi.Router) {
		r.Get("/search", d.search)
		r.Get("/doc/{doctype}", d.list)
		r.Get("/doc/{doctype}/new", d.newGet)
		r.Post("/doc/{doctype}/new", d.newPost)
		r.Get("/doc/{doctype}/{id}", d.page)
		r.Get("/doc/{doctype}/{id}/edit/{step}", d.stepGet)
		r.Post("/doc/{doctype}/{id}/edit/{step}", d.stepPost)
		r.Get("/doc/{doctype}/{id}/lines/new", d.lineGet)
		r.Post("/doc/{doctype}/{id}/lines/new", d.linePost)
		r.Get("/doc/{doctype}/{id}/lines/{no}", d.lineGet)
		r.Post("/doc/{doctype}/{id}/lines/{no}", d.linePost)
		r.Get("/doc/{doctype}/{id}/lines/{no}/remove", d.removeGet)
		r.Post("/doc/{doctype}/{id}/lines/{no}/remove", d.removePost)
		r.Post("/doc/{doctype}/{id}/lines/{no}/move", d.movePost)
		// {action} is an action (submit, withdraw, approve, revise, amend;
		// POST) or a reason page (reject, cancel, close, delete; GET and POST).
		// Unknown names are 404 without calling the engine.
		r.Get("/doc/{doctype}/{id}/{action}", d.actionGet)
		r.Post("/doc/{doctype}/{id}/{action}", d.actionPost)
	}
}
