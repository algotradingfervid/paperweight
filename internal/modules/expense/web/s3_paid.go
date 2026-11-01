package expenseweb

import (
	"errors"
	"net/http"
	"path"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

// The Mark as paid and Undo paid pages (UX §5.18), built like docscreen's
// reason page and delete confirm. E9's POST handlers redraw them through
// renderPaidPage.

// paidView is what E9's POST handlers pass to renderPaidPage: the advance and
// the values typed, so a redraw keeps them.
type paidView struct {
	AdvanceID int64
	Version   int
	PaidOn    string // as typed; blank on Undo paid
}

// paidScreen is everything a Mark as paid or Undo paid page renders.
type paidScreen struct {
	opts     layout.PageOpts
	summary  string
	question string // Undo paid's text under the summary
	action   string
	version  int
	date     *components.DateProps // nil on Undo paid
	button   components.ButtonProps
	danger   string
	reload   *components.Link // the GET page, on a 409
	errs     []components.FieldErr
	// focusHeading focuses the Undo paid confirm's heading (UX §5.18). The
	// danger banner always takes focus (as the wizard's conflict banner);
	// ErrorSummary and Paid on focus themselves.
	focusHeading bool
}

// paidPage answers GET /modules/expense/advances/{id}/paid.
func (h *handlers) paidPage(w http.ResponseWriter, r *http.Request) { h.paidGet(w, r) }

// unpaidPage answers GET /modules/expense/advances/{id}/unpaid.
func (h *handlers) unpaidPage(w http.ResponseWriter, r *http.Request) { h.paidGet(w, r) }

// paidGet draws either page for a user holding cancel on the advance: a bad
// or unreadable id is AUTH's 404, no cancel AUTH's 403 (PaidPageOf).
func (h *handlers) paidGet(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		web.ErrorFrom(w, r, auth.ErrNotFound)
		return
	}
	p, _ := auth.PrincipalFrom(r.Context())
	pg, err := h.d.Service.PaidPageOf(r.Context(), p, id)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	s := paidScreenOf(r, pg, paidView{AdvanceID: id, Version: pg.Version, PaidOn: types.FormatDate(pg.Today)}, nil)
	ui.Render(w, r, http.StatusOK, paidFull(s), paidMain(s))
}

// renderPaidPage redraws the Mark as paid form or the Undo paid page with err
// (a 422 or 409), keeping the posted version and the typed date. The
// advance is read again, with the same checks as the GET.
func (h *handlers) renderPaidPage(w http.ResponseWriter, r *http.Request, v paidView, err error) {
	p, _ := auth.PrincipalFrom(r.Context())
	pg, lerr := h.d.Service.PaidPageOf(r.Context(), p, v.AdvanceID)
	if lerr != nil {
		web.ErrorFrom(w, r, lerr)
		return
	}
	status := http.StatusUnprocessableEntity
	var se interface{ Status() int }
	if errors.As(err, &se) {
		status = se.Status()
	}
	s := paidScreenOf(r, pg, v, err)
	ui.Render(w, r, status, paidFull(s), paidMain(s))
}

// paidScreenOf is the page for the request's route (…/paid or …/unpaid),
// with err's field messages at Paid on and its other message in the danger
// banner (with a Reload link on a 409).
func paidScreenOf(r *http.Request, pg expense.PaidPage, v paidView, err error) paidScreen {
	id := strconv.FormatInt(v.AdvanceID, 10)
	page := "/doc/" + expense.AdvanceDocType + "/" + id
	route := path.Base(r.URL.Path)
	s := paidScreen{
		opts:    layout.PageOpts{Width: layout.Form, Back: &components.Link{Label: pg.Label, Href: page}, NoHeading: true, Focus: true},
		summary: pg.Label + " · " + pg.Employee + " · " + types.FormatINR(pg.Amount),
		action:  "/modules/expense/advances/" + id + "/" + route,
		version: v.Version,
	}
	if route == "unpaid" {
		s.opts.Title = "Undo paid on " + pg.Label + "?"
		if pg.PaidOn != nil {
			s.summary += " · Paid on " + types.FormatDate(*pg.PaidOn)
		}
		s.question = "Claims can't use this advance until it is marked as paid again."
		s.button = components.ButtonProps{Label: "Undo paid", Busy: "Undo paid…", Variant: components.Danger}
	} else {
		s.opts.Title = "Mark " + pg.Label + " as paid"
		s.date = &components.DateProps{
			FieldProps: components.FieldProps{
				ID: components.FieldID("paid_on"), Name: "paid_on", Label: "Paid on",
				Hint: "The day the money reached " + pg.Employee + ".", Value: v.PaidOn,
			},
			Today: types.FormatDate(pg.Today), Yesterday: types.FormatDate(types.AddDays(pg.Today, -1)),
		}
		s.button = components.ButtonProps{Label: "Mark as paid", Busy: "Mark as paid…", Variant: components.Primary}
	}
	var de *doc.Error
	switch {
	case err == nil:
		if s.date != nil {
			s.date.Attrs = map[string]any{"autofocus": true}
		} else {
			s.focusHeading = true
		}
	case errors.As(err, &de) && len(de.Fields) > 0 && s.date != nil:
		s.date.Error = de.Fields[0].Message
		s.errs = []components.FieldErr{{ID: s.date.ID, Message: s.date.Error}}
	default:
		s.danger = err.Error()
		var se interface{ Status() int }
		if errors.As(err, &se) && se.Status() == http.StatusConflict {
			s.reload = &components.Link{Label: "Reload", Href: s.action}
		}
	}
	s.opts.Error = err != nil
	return s
}

// paidOpts puts the danger banner and the error summary above the heading.
func paidOpts(s paidScreen) layout.PageOpts {
	o := s.opts
	o.BannerSlot = paidBanners(s)
	return o
}
