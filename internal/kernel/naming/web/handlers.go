package namingweb

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
)

type handlers struct{ d Deps }

// listRow is one series on the Numbering page.
type listRow struct{ Href, Text string }

func (h *handlers) list(w http.ResponseWriter, r *http.Request) {
	all, err := h.d.Admin.List(r.Context())
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	today := types.Today(h.d.Clock)
	rows := make([]listRow, 0, len(all))
	for _, s := range all {
		next, err := h.next(r.Context(), s.DocType, today)
		if err != nil {
			web.ErrorFrom(w, r, err)
			return
		}
		rows = append(rows, listRow{Href: editPath(s.DocType), Text: s.Label + " · " + s.Template + " · Next: " + next})
	}
	ui.Render(w, r, http.StatusOK, listPage(rows), nil)
}

// next is the number the next document dated today gets, or naming's
// message when the series has used all its numbers.
func (h *handlers) next(ctx context.Context, dt string, today types.Date) (string, error) {
	num, err := h.d.Svc.Preview(ctx, dt, today)
	var ne *naming.Error
	if errors.Is(err, naming.ErrExhausted) && errors.As(err, &ne) {
		return ne.Msg, nil
	}
	return num, err
}

func (h *handlers) preview(w http.ResponseWriter, r *http.Request) {
	out, err := h.d.Admin.PreviewTemplate(r.Context(), r.URL.Query().Get("tpl"), types.Today(h.d.Clock))
	var ne *naming.Error
	if errors.As(err, &ne) {
		frag := previewError(ne.Msg)
		ui.Render(w, r, ne.Status(), frag, frag)
		return
	}
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	frag := previewResult(out)
	ui.Render(w, r, http.StatusOK, frag, frag)
}

func (h *handlers) edit(w http.ResponseWriter, r *http.Request) {
	s, err := h.series(r)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	prefix, err := h.prefix(r.Context(), s)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, editForm{S: s, Template: s.Template, Prefix: prefix})
}

// prefix is s's prefix for a document dated today, blank when the series
// has used all its numbers.
func (h *handlers) prefix(ctx context.Context, s naming.SeriesInfo) (string, error) {
	next, err := h.d.Svc.Preview(ctx, s.DocType, types.Today(h.d.Clock))
	switch {
	case err == nil: // the number part is last and always its full width
		return next[:len(next)-strings.Count(s.Template, "#")], nil
	case errors.Is(err, naming.ErrExhausted):
		return "", nil
	}
	return "", err
}

func (h *handlers) saveTemplate(w http.ResponseWriter, r *http.Request) {
	s, p, ok := h.load(w, r)
	if !ok {
		return
	}
	prefix, err := h.prefix(r.Context(), s)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	f := editForm{S: s, Template: r.PostFormValue("template"), Prefix: prefix}
	h.finish(w, r, f, h.d.Admin.SetTemplate(r.Context(), p, s.DocType, f.Template))
}

func (h *handlers) saveCounter(w http.ResponseWriter, r *http.Request) {
	s, p, ok := h.load(w, r)
	if !ok {
		return
	}
	f := editForm{S: s, Template: s.Template, Prefix: r.PostFormValue("prefix"), Last: r.PostFormValue("last")}
	last, err := naming.ParseLastNumber(s.Template, f.Last)
	if err == nil {
		err = h.d.Admin.SetCounter(r.Context(), p, s.DocType, f.Prefix, last)
	}
	h.finish(w, r, f, err)
}

// load is the series in the URL and the signed-in user; it answers the
// request itself when either is missing.
func (h *handlers) load(w http.ResponseWriter, r *http.Request) (naming.SeriesInfo, auth.Principal, bool) {
	p, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		web.ErrorFrom(w, r, auth.ErrForbidden)
		return naming.SeriesInfo{}, auth.Principal{}, false
	}
	s, err := h.series(r)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return naming.SeriesInfo{}, auth.Principal{}, false
	}
	return s, p, true
}

// finish answers a save: a flash and 303 to the edit page, the form again
// with its field errors (422), or the error page.
func (h *handlers) finish(w http.ResponseWriter, r *http.Request, f editForm, err error) {
	var he doctype.HTTPError
	if errors.As(err, &he) && len(he.FieldErrors()) > 0 {
		f.Errs = he.FieldErrors()
		h.render(w, r, he.Status(), f)
		return
	}
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	web.Flash(r.Context(), "success", "Numbering for "+f.S.Label+" saved.")
	http.Redirect(w, r, editPath(f.S.DocType), http.StatusSeeOther)
}

// series is the series for the DocType in the URL; an unknown one is
// doctype.ErrNotFound (CD-54).
func (h *handlers) series(r *http.Request) (naming.SeriesInfo, error) {
	all, err := h.d.Admin.List(r.Context())
	if err != nil {
		return naming.SeriesInfo{}, err
	}
	dt := chi.URLParam(r, "doctype")
	for _, s := range all {
		if s.DocType == dt {
			return s, nil
		}
	}
	return naming.SeriesInfo{}, doctype.ErrNotFound
}

func (h *handlers) render(w http.ResponseWriter, r *http.Request, status int, f editForm) {
	ui.Render(w, r, status, editPage(f), nil)
}

func editPath(dt string) string { return basePath + "/" + dt }

// editForm is the edit page's values, as stored or as posted, with any
// field errors.
type editForm struct {
	S                      naming.SeriesInfo
	Template, Prefix, Last string
	Errs                   []doctype.FieldError
}

// errFor is the message on field, "" when none.
func (f editForm) errFor(field string) string {
	for _, e := range f.Errs {
		if e.Field == field {
			return e.Message
		}
	}
	return ""
}

func (f editForm) summary() []components.FieldErr {
	out := make([]components.FieldErr, 0, len(f.Errs))
	for _, e := range f.Errs {
		out = append(out, components.FieldErr{ID: components.FieldID(e.Field), Message: e.Message})
	}
	return out
}

// counters are the series' counters, prefix → last number, by prefix.
func (f editForm) counters() []components.Pair {
	out := make([]components.Pair, 0, len(f.S.Counters))
	for prefix, last := range f.S.Counters {
		out = append(out, components.Pair{Label: prefix, Value: strconv.FormatInt(last, 10), Num: true})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	return out
}
