package wizard

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
)

type nowKey struct{}

// WithNow is ctx with now as the save cue's clock: the app passes its
// business clock (app middleware), tests a fixed one. Without it a step
// save carries no HX-Trigger saved.
func WithNow(ctx context.Context, now func() time.Time) context.Context {
	return context.WithValue(ctx, nowKey{}, now)
}

// savedTrigger is the HX-Trigger of a step save: the event saved, whose
// value app.js writes into the save cue (#saved-at): "Saved {HH:MM}" on the
// business clock from WithNow, in IST (UI v2 §5.6); "" with no clock.
func savedTrigger(ctx context.Context) string {
	now, ok := ctx.Value(nowKey{}).(func() time.Time)
	if !ok {
		return ""
	}
	b, _ := json.Marshal(map[string]string{"saved": "Saved " + now().In(types.IST).Format("15:04")})
	return string(b)
}

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

// Nav is the step form's nav button: "back", "finish", "stay" (save and
// redraw the same step) or "".
func Nav(r *http.Request) string {
	switch v := r.PostFormValue("nav"); v {
	case "back", "finish", "stay":
		return v
	}
	return ""
}

// Respond writes a step POST's response (research §7.3). page(true) is the
// #wizard fragment and page(false) the full page; it is not called when the
// response is a redirect. An htmx Advance or Finish (a step save, Back
// included) carries HX-Trigger saved for the save cue, given a WithNow
// clock; a master wizard has no cue, so there the event changes nothing.
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
		if t := savedTrigger(r.Context()); t != "" {
			w.Header().Set("HX-Trigger", t)
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
