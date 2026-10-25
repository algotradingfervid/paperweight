package wizard

import (
	"net/http"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui"
)

// Outcome is what a step POST did.
type Outcome int

// Step outcomes.
const (
	// Advance: the step saved; go to nextURL.
	Advance Outcome = iota
	// Invalid: re-show the step from the posted values (422).
	Invalid
	// Conflict: the version was stale (409), typed values kept.
	Conflict
	// Finish: leave the wizard for nextURL.
	Finish
)

// Nav is the step form's nav button: "back", "finish" or "".
func Nav(r *http.Request) string {
	switch v := r.PostFormValue("nav"); v {
	case "back", "finish":
		return v
	}
	return ""
}

// Respond writes a step POST's response (research §7.3). page(true) is the
// #wizard fragment and page(false) the full page; it is not called when the
// response is a redirect.
func Respond(w http.ResponseWriter, r *http.Request, o Outcome, nextURL string, page func(fragment bool) templ.Component) {
	htmx := ui.IsHTMX(r)
	switch o {
	case Invalid:
		ui.Render(w, r, http.StatusUnprocessableEntity, page(htmx), nil)
	case Conflict:
		ui.Render(w, r, http.StatusConflict, page(htmx), nil)
	default:
		if !htmx {
			w.Header().Add("Vary", "HX-Request")
			http.Redirect(w, r, nextURL, http.StatusSeeOther)
			return
		}
		if o == Finish {
			w.Header().Add("Vary", "HX-Request")
			w.Header().Set("HX-Redirect", nextURL)
			w.WriteHeader(http.StatusOK)
			return
		}
		w.Header().Set("HX-Push-Url", nextURL)
		ui.Render(w, r, http.StatusOK, page(true), nil)
	}
}
