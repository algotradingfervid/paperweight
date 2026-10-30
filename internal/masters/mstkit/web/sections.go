package mstweb

import (
	"context"
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

// sectionCard is one section of a summary. Change is nil for a user who can't
// write.
type sectionCard struct {
	Title  string
	Pairs  []components.Pair
	Rows   [][]components.Pair // a wizard row step's, one summary each
	Change *components.Link
}

// sectionCards is the record page's summary.
func (h *handlers[R, In]) sectionCards(ctx context.Context, tx *db.Tx, id int64, vals map[string]string, canWrite bool) ([]sectionCard, error) {
	cards := make([]sectionCard, 0, len(h.Sections))
	show := h.display
	if canWrite {
		show = h.writerDisplay
	}
	for _, s := range h.Sections {
		pairs, err := h.stepPairs(ctx, tx, s, vals, show)
		if err != nil {
			return nil, err
		}
		c := sectionCard{Title: s.Label, Pairs: pairs}
		if canWrite {
			c.Change = &components.Link{Label: "Change", Href: h.sectionPath(id, s)}
		}
		cards = append(cards, c)
	}
	return cards, nil
}

// writerDisplay is display with a Lookup's own text.
func (h *handlers[R, In]) writerDisplay(ctx context.Context, tx *db.Tx, fd doctype.Field, value string) (string, error) {
	if l, ok := h.Lookups[fd.Name]; ok && value != "" {
		return l.Show(ctx, tx, value)
	}
	return h.display(ctx, tx, fd, value)
}

// stepPairs is s's fields with a value, as show renders them.
func (h *handlers[R, In]) stepPairs(ctx context.Context, tx *db.Tx, s docscreen.FormStep, vals map[string]string, show func(context.Context, *db.Tx, doctype.Field, string) (string, error)) ([]components.Pair, error) {
	var out []components.Pair
	for _, name := range s.Fields {
		if vals[name] == "" {
			continue
		}
		fd, _ := h.dt.Field(name) // handlers checked every step's fields
		text, err := show(ctx, tx, fd, vals[name])
		if err != nil {
			return nil, err
		}
		out = append(out, components.Pair{Label: fd.Label, Value: text})
	}
	return out, nil
}

func (h *handlers[R, In]) sectionPath(id int64, s docscreen.FormStep) string {
	return h.recordPath(id) + "/edit/" + s.Slug
}

// section is the section the URL names.
func (h *handlers[R, In]) section(r *http.Request) (docscreen.FormStep, error) {
	slug := chi.URLParam(r, "section")
	i := slices.IndexFunc(h.Sections, func(s docscreen.FormStep) bool { return s.Slug == slug })
	if i < 0 {
		return docscreen.FormStep{}, doctype.ErrNotFound
	}
	return h.Sections[i], nil
}

// sectionPage serves a section's form with the stored values, Lookups shown.
func (h *handlers[R, In]) sectionPage(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	ctx := r.Context()
	id := RecordID(r)
	s, err := h.section(r)
	if err != nil {
		return err
	}
	if err := h.checkSubmit(ctx, p, id); err != nil {
		return err
	}
	var rec R
	var vals map[string]string
	err = h.Env.DB.Read(ctx, func(tx *db.Tx) error {
		if rec, err = h.Svc.Get(ctx, tx, id); err != nil {
			return err
		}
		if vals, err = h.formValues(rec); err != nil {
			return err
		}
		for name, l := range h.Lookups {
			if vals[name] != "" {
				if vals[name], err = l.Show(ctx, tx, vals[name]); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return h.renderSection(w, r, http.StatusOK, id, s, rec, form{Values: vals, Version: h.Head(rec).Version})
}

// sectionSave is Update at the posted version (CD-94) with this section's
// fields as typed and the others as stored. The record is read before the
// write, but a change since the page was rendered moves the version, so
// Update refuses it: stored values can't overwrite a newer save.
func (h *handlers[R, In]) sectionSave(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	ctx := r.Context()
	id := RecordID(r)
	s, err := h.section(r)
	if err != nil {
		return err
	}
	version, err := PostedVersion(r)
	if err != nil {
		return err
	}
	if err := h.checkSubmit(ctx, p, id); err != nil {
		return err
	}
	var rec R
	var vals map[string]string
	err = h.Env.DB.Read(ctx, func(tx *db.Tx) error {
		if rec, err = h.Svc.Get(ctx, tx, id); err != nil {
			return err
		}
		vals, err = h.formValues(rec)
		return err
	})
	if err != nil {
		return err
	}
	typed := h.postedStep(r, s)
	f := form{Values: typed, Version: version}
	merged := maps.Clone(vals)
	maps.Copy(merged, typed)
	resolved, errs, err := h.resolve(ctx, merged, s.Fields, vals)
	if err != nil {
		return err
	}
	in, parsed := h.ToInput(resolved)
	errs = append(errs, parsed...)
	if len(errs) > 0 {
		f.Errs = withRules(errs, stepErrors(s, h.rules(in)))
		return h.renderSection(w, r, http.StatusUnprocessableEntity, id, s, rec, f)
	}
	saved, err := h.Svc.Update(ctx, p, id, version, in)
	if err != nil {
		return h.sectionRefused(w, r, id, s, rec, f, err)
	}
	return Done(w, r, h.Copy.Saved, h.recordPath(id), saved.Warnings...)
}

// rules is Validate's errors for in, none without a Validate.
func (h *handlers[R, In]) rules(in In) []doctype.FieldError {
	if h.Validate == nil {
		return nil
	}
	return validationErrors(h.Validate(in))
}

// sectionRefused shows a refusal on the section: field errors of its own by
// their fields, the rest (a stale version, a refusal on another section's
// field) in the banner.
func (h *handlers[R, In]) sectionRefused(w http.ResponseWriter, r *http.Request, id int64, s docscreen.FormStep, rec R, f form, err error) error {
	f, status, ok := f.Refused(err, s.Fields)
	if !ok {
		return err
	}
	return h.renderSection(w, r, status, id, s, rec, f)
}

// renderSection is the section's form on the record page's template.
func (h *handlers[R, In]) renderSection(w http.ResponseWriter, r *http.Request, status int, id int64, s docscreen.FormStep, rec R, f form) error {
	head := h.Head(rec)
	v := recordView{
		ID: id, Title: s.Label, Action: h.sectionPath(id, s), CanWrite: true, Width: layout.Form, Form: f,
		Back: components.Link{Label: strings.TrimSpace(head.Code + " " + head.Name), Href: h.recordPath(id)},
	}
	for _, name := range s.Fields {
		fd, _ := h.dt.Field(name) // handlers checked every step's fields
		v.Inputs = append(v.Inputs, h.input(fd, name, f))
	}
	ui.Render(w, r, status, recordPage(v), nil)
	return nil
}
