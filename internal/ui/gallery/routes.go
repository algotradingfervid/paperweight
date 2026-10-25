// Package gallery is the component gallery: one page per component group,
// fed with the worst-case values, plus the docscreen pages over fakes. The
// browser checks in uibrowser run over it. It is imported only by tests and
// by cmd/app under the e2e tag (Decision U8).
package gallery

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/chromedp/chromedp"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/platform/brand"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/layout"
	"github.com/vardhan/internaltools/internal/ui/screens"
	"github.com/vardhan/internaltools/internal/ui/wizard"
)

// galleryPage is one page at /_gallery/<slug>.
type galleryPage struct {
	slug string
	opts layout.PageOpts
	body func(r *http.Request) templ.Component
}

func static(c templ.Component) func(*http.Request) templ.Component {
	return func(*http.Request) templ.Component { return c }
}

func titled(t string) layout.PageOpts { return layout.PageOpts{Title: t} }

func formWidth(t string) layout.PageOpts { return layout.PageOpts{Title: t, Width: layout.Form} }

// wizardOpts is wizard step n's page: the frame has the heading.
func wizardOpts(n int) layout.PageOpts {
	return layout.PageOpts{Title: "Wizard step " + strconv.Itoa(n), NoHeading: true}
}

var pages = []galleryPage{
	{"buttons", titled("Buttons"), static(buttonsBody())},
	{"fields", formWidth("Fields"), static(fieldsBody())},
	{"badges-banners", titled("Badges and banners"), static(badgesBody())},
	{"dialogs", titled("Dialogs and sheets"), static(dialogsBody())},
	{"datalist", titled("Data list"), static(datalistBody())},
	{"lines", titled("Lines"), static(linesBody())},
	{"picker", formWidth("Picker"), static(pickerBody())},
	{"upload", formWidth("File upload"), static(uploadBody())},
	{"wizard-1", wizardOpts(1), wizardBody(0)},
	{"wizard-2", wizardOpts(2), wizardBody(1)},
	{"wizard-3", wizardOpts(3), wizardBody(2)},
	{"wizard-4", wizardOpts(4), wizardBody(3)},
	{"actionbar", layout.PageOpts{Title: "Action bar", BarSlot: actionbarBar()}, static(actionbarBody())},
	{"warnings", titled("Warnings"), static(warningsBody())},
	{"filterbar", titled("Filter bar"), static(filterbarBody())},
	{"pagination", titled("Pagination"), static(paginationBody())},
	{"bare", layout.PageOpts{Title: "Sign in", Bare: true, Width: layout.Form}, static(bareBody())},
	{"auth-signin", layout.PageOpts{Title: "Sign in", Bare: true, Width: layout.Form}, static(authSignInBody())},
	{"auth-setup", layout.PageOpts{Title: "Set up the first account", Bare: true, Width: layout.Form}, static(authSetupBody())},
	{"auth-password", formWidth("Change password"), static(authPasswordBody())},
	{"auth-sessions", titled("Your sessions"), static(authSessionsBody())},
	{"auth-users", titled("Users"), static(authUsersBody())},
	{"auth-rights", layout.PageOpts{Title: "Rights for Employee", BarSlot: authRightsBar()}, static(authRightsBody())},
}

// strictPolicy is WP0's policy without 'unsafe-inline' for styles, reporting
// to screens.CSPReportPath (SG-UI-6).
func strictPolicy(wp0 string) string {
	return strings.Replace(wp0, " 'unsafe-inline'", "", 1) + "; report-uri " + screens.CSPReportPath
}

// Handler is the gallery app: WP0's router (security headers and CSP), the
// fake shell, the static files and Routes. ?csp=strict sends strictPolicy.
func Handler() http.Handler {
	r := web.NewRouter(web.Options{})
	r.Use(strictCSP, ui.ShellMiddleware(shell))
	r.Handle("/static/*", web.Static())
	r.Handle("/static/ui/*", http.StripPrefix("/static/ui", ui.Static()))
	r.Handle("/static/brand/*", brand.Static())
	r.Post(screens.CSPReportPath, cspReport)
	Routes()(r)
	return r
}

func strictCSP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("csp") == "strict" {
			h := w.Header()
			h.Set("Content-Security-Policy", strictPolicy(h.Get("Content-Security-Policy")))
		}
		next.ServeHTTP(w, r)
	})
}

// cspReport records a CSP violation report in the log.
func cspReport(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	slog.WarnContext(r.Context(), "gallery: CSP report", "report", string(b))
	w.WriteHeader(http.StatusNoContent)
}

// Routes mounts the gallery pages, the docscreen routes over the seeded
// fakes and the field validators (CD-48).
func Routes() func(chi.Router) {
	deps := docDeps()
	return func(r chi.Router) {
		r.Get("/_gallery", index)
		r.Get("/_gallery/{page}", show)
		r.Post("/_gallery/{page}", post)
		r.Post("/_gallery/lines/rows/{no}", saveRow)
		r.Post("/ui/validate/*", components.ValidateHandler(components.NUMParsers()).ServeHTTP)
		docscreen.Routes(deps)(r)
	}
}

func render(w http.ResponseWriter, r *http.Request, status int, o layout.PageOpts, body templ.Component) {
	ui.Render(w, r, status, full(o, body), frag(o, body))
}

func index(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusOK, layout.PageOpts{Title: "Component gallery"}, indexBody())
}

// show serves a gallery page. The pickers' search answers here too, at
// /_gallery/items, so that every GET route stays a screen (RoutesCovered).
func show(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "page")
	if slug == "items" {
		ui.Render(w, r, http.StatusOK, pickerResults(r.URL.Query().Get("list"), r.URL.Query().Get("q")), nil)
		return
	}
	i := slices.IndexFunc(pages, func(p galleryPage) bool { return p.slug == slug })
	if i < 0 {
		http.NotFound(w, r)
		return
	}
	render(w, r, http.StatusOK, pages[i].opts, pages[i].body(r))
}

// post serves the buttons page's slow form and wizard step 1.
func post(w http.ResponseWriter, r *http.Request) {
	switch chi.URLParam(r, "page") {
	case "buttons":
		time.Sleep(500 * time.Millisecond)
		w.Header().Set("HX-Trigger", `{"notice":{"level":"success","text":"Saved the sample."}}`)
		w.WriteHeader(http.StatusNoContent)
	case "wizard-1":
		v := stepValues{reference: r.PostFormValue("reference"), date: r.PostFormValue("order_date"), supplier: pickByID(r.PostFormValue("supplier_id"))}
		if strings.TrimSpace(v.reference) == "" {
			v.errs = append(v.errs, components.FieldErr{ID: "f-reference", Message: "Enter the reference."})
		}
		if strings.TrimSpace(v.date) == "" {
			v.errs = append(v.errs, components.FieldErr{ID: "f-order_date", Message: "Enter the order date."})
		}
		outcome, step := wizard.Advance, 1
		if len(v.errs) > 0 {
			outcome, step = wizard.Invalid, 0
		}
		wizard.Respond(w, r, outcome, "/_gallery/wizard-2", func(fragment bool) templ.Component {
			m := wizardModel(step, v.errs, "")
			if fragment {
				return wizardFrame(m, v)
			}
			return full(wizardOpts(1), wizardFrame(m, v))
		})
	default:
		http.NotFound(w, r)
	}
}

// saveRow saves one line grid row: a quantity NUM can't parse is refused
// with 422 and the row shown again with the error.
func saveRow(w http.ResponseWriter, r *http.Request) {
	no, err := strconv.Atoi(chi.URLParam(r, "no"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	qty, status, errText := r.PostFormValue("qty"), http.StatusOK, ""
	if q, err := types.ParseQty(qty); err != nil {
		status, errText = http.StatusUnprocessableEntity, err.Error()
	} else {
		qty = types.FormatQty(q)
	}
	ui.Render(w, r, status, components.LineGridRowForm(gridRow(no, pickByID(r.PostFormValue("item_id")), qty, errText)), nil)
}

// stepValues are wizard step 1's values and errors.
type stepValues struct {
	reference, date string
	supplier        *components.PickRow
	errs            []components.FieldErr
}

func (v stepValues) errFor(id string) string {
	for _, e := range v.errs {
		if e.ID == id {
			return e.Message
		}
	}
	return ""
}

func pickByID(s string) *components.PickRow {
	id, _ := strconv.ParseInt(s, 10, 64)
	for i := range pickRows {
		if pickRows[i].ID == id {
			return &pickRows[i]
		}
	}
	return nil
}

// conflictText is DOC's stale-version message (DOC-069 wording).
const conflictText = "Someone else changed Draft 42 after you opened it. Reload it and make your changes again."

func wizardModel(step int, errs []components.FieldErr, conflict string) wizard.Model {
	m := wizard.Model{
		Mode: wizard.Document, Title: "New sample order", DocLabel: "Draft 42", BaseURL: "/_gallery",
		Steps: []wizard.Step{
			{Slug: "wizard-1", Label: "Supplier and date"},
			{Slug: "wizard-2", Label: "Items"},
			{Slug: "wizard-3", Label: "Terms"},
			{Slug: "wizard-4", Label: "Check and submit"},
		},
		Current: step, Furthest: 3, Version: 1, Errors: errs, Conflict: conflict,
	}
	if conflict != "" {
		m.ReloadURL = m.StepURL(step)
	}
	return m
}

func barLabel(m wizard.Model) string {
	if m.Last() {
		return "Submit for approval"
	}
	return ""
}

// wizardBody is step i; ?errors=1 shows step 1 refused and ?conflict=1 the
// conflict banner.
func wizardBody(i int) func(*http.Request) templ.Component {
	return func(r *http.Request) templ.Component {
		v := stepValues{reference: "PO-REF-1", date: "24-10-2026"}
		if i == 0 && r.URL.Query().Get("errors") == "1" {
			v = stepValues{errs: []components.FieldErr{{ID: "f-reference", Message: "Enter the reference."}, {ID: "f-order_date", Message: "Enter the order date."}}}
		}
		conflict := ""
		if r.URL.Query().Get("conflict") == "1" {
			conflict = conflictText
		}
		return wizardFrame(wizardModel(i, v.errs, conflict), v)
	}
}

// Screens lists every gallery page, a few page states, and the docscreen
// pages of the sample DocType, under base. Nobody signs in: the gallery's
// shell is Priya's.
func Screens(base string) []screens.Screen {
	const route = "/_gallery/{page}"
	out := []screens.Screen{{Name: "gallery index", Route: "/_gallery", URL: base + "/_gallery"}}
	for _, p := range pages {
		out = append(out, screens.Screen{Name: "gallery " + p.slug, Route: route, URL: base + "/_gallery/" + p.slug})
	}
	out = append(out,
		screens.Screen{Name: "gallery dialogs, dialog open", Route: route, URL: base + "/_gallery/dialogs",
			Setup: run(`document.querySelector('[data-open-dialog="sample-dialog"]').click()`, `document.getElementById("sample-dialog").open`)},
		screens.Screen{Name: "gallery dialogs, sheet open", Route: route, URL: base + "/_gallery/dialogs",
			Setup: run(`document.querySelector('[data-open-dialog="sample-sheet"]').click()`, `document.getElementById("sample-sheet").open`)},
		screens.Screen{Name: "gallery menu open", Route: route, URL: base + "/_gallery/buttons",
			Setup: run(`document.getElementById("nav-sheet").showModal()`, `document.getElementById("nav-sheet").open`)},
		screens.Screen{Name: "gallery picker, search open", Route: route, URL: base + "/_gallery/picker",
			Setup: func(ctx context.Context) error {
				return chromedp.Do(ctx, chromedp.Click(chromedp.CSS("#f-item")),
					settled(`document.querySelectorAll("#f-item-list [role=option]").length > 2 && document.querySelector(".htmx-request") === null`))
			}},
		screens.Screen{Name: "gallery wizard-1, refused", Route: route, URL: base + "/_gallery/wizard-1?errors=1"},
		screens.Screen{Name: "gallery wizard-3, conflict", Route: route, URL: base + "/_gallery/wizard-3?conflict=1"},
	)
	for _, s := range docscreen.Screens(docscreentest.Sample, galleryIDs) {
		s.URL, s.As = base+s.URL, ""
		out = append(out, s)
	}
	return out
}

// run is a Setup that evaluates js and waits until done is true and until
// CSS animations have ended (axe reads colours mid-fade otherwise).
func run(js, done string) func(context.Context) error {
	return func(ctx context.Context) error {
		return chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](js), settled(done))
	}
}

// settled waits until done is true and the page's transitions have ended.
func settled(done string) chromedp.Action[chromedp.Void] {
	return chromedp.Poll[chromedp.Void]("("+done+`) && !document.getAnimations().some(function (a) { return a instanceof CSSTransition; })`,
		chromedp.WithPollingTimeout(5*time.Second))
}
