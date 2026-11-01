package docscreen_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// M0-26 (CD-282 R1 and R4, CD-286; hooks spec §4.1 "Errors", design-system
// §8.3): on a step with a Body a line error links to the Body's own input,
// LineFieldID(line, field), when the Body rendered that id, and to the step
// heading otherwise; and the step form starts with an unseen copy of the
// primary, so Enter never presses a Body button. Every test runs on
// docscreentest.Hooked with the real engine (m02Env): line 1 CEMENT-OPC 10,
// line 2 WIRE-18G 5.

const (
	m26QtyMsg    = "M26 line 2 quantity is not right."
	m26Qty1Msg   = "M26 line 1 quantity is not right."
	m26Qty3Msg   = "M26 line 3 quantity is not right."
	m26NoteMsg   = "M26 line 2 note is not right."
	m26LinesMsg  = "M26 lines are not right."
	m26LineMsg   = "M26 line 2 is not right."
	m26EngineMsg = "Line 2: Quantity must be more than 0." // DOC's Positive refusal for Hooked's qty
	m26FillLabel = "Receive all pending"
)

// m26BodyOpts says what m26Body renders.
type m26BodyOpts struct {
	omit   int  // a line rendered without its LineFieldID (0 = none)
	button bool // a submit button of the Body's own (GRN's Receive all pending)
}

// m26Body is a custom Items Body: #m26-body with, for each line n of the
// view, a text input m26_qty_{n} labelled for the line, with the id
// docscreen.LineFieldID(n, "qty") (m26-line-{n} for opts.omit), showing the
// typed value on a redraw and the stored quantity otherwise, and the line's
// own errors under it; then, with opts.button, a Receive all pending submit
// posting m26_fill=all.
func m26Body(opts m26BodyOpts) func(docscreen.Ctx, docscreen.StepState) (templ.Component, error) {
	return func(c docscreen.Ctx, s docscreen.StepState) (templ.Component, error) {
		var parts []templ.Component
		parts = append(parts, m17Raw(`<div id="m26-body">`))
		if c.View != nil {
			for i, l := range c.View.Lines {
				n := i + 1
				id := docscreen.LineFieldID(n, "qty")
				if n == opts.omit {
					id = "m26-line-" + strconv.Itoa(n)
				}
				name := "m26_qty_" + strconv.Itoa(n)
				value := l.Inputs["qty"]
				if s.Posted {
					value = c.Req.PostFormValue(name)
				}
				msg := ""
				for _, e := range s.Errors {
					if e.Line == n && e.Field == "qty" {
						msg = e.Message
					}
				}
				parts = append(parts, components.TextInput(components.FieldProps{
					ID: id, Name: name, Label: "Line " + strconv.Itoa(n) + " received", Value: value, Error: msg,
				}))
			}
		}
		if opts.button {
			parts = append(parts, components.Button(components.ButtonProps{Label: m26FillLabel, Name: "m26_fill", Value: "all", Variant: components.Secondary}))
		}
		parts = append(parts, m17Raw(`</div>`))
		return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
			for _, p := range parts {
				if err := p.Render(ctx, w); err != nil {
					return err
				}
			}
			return nil
		}), nil
	}
}

// m26SaveErrors is a Save returning errs as Save.Errors.
func m26SaveErrors(errs ...doctype.FieldError) func(docscreen.Ctx) (docscreen.Save, error) {
	return func(docscreen.Ctx) (docscreen.Save, error) { return docscreen.Save{Errors: errs}, nil }
}

// m26Links maps each error summary link's text to its href.
func m26Links(t *testing.T, page *goquery.Document) map[string]string {
	t.Helper()
	out := map[string]string{}
	page.Find(".error-summary a").Each(func(_ int, a *goquery.Selection) {
		out[norm(a.Text())] = m02Attr(a, "href")
	})
	return out
}

// m26WantLinks fails unless the summary links exactly want (message → href),
// each href lands on one element of the page, and an input href lands on the
// Body's own input; then it runs the no-dead-links check.
func m26WantLinks(t *testing.T, rec *httptest.ResponseRecorder, want map[string]string, what string) {
	t.Helper()
	page := uitest.Doc(t, rec)
	got := m26Links(t, page)
	if len(got) != len(want) {
		t.Errorf("%s: summary links = %q, want %q", what, got, want)
	}
	for msg, href := range want {
		if got[msg] != href {
			t.Errorf("%s: summary link %q = %q, want %q", what, msg, got[msg], href)
		}
		id := strings.TrimPrefix(href, "#")
		if n := page.Find(`[id="` + id + `"]`).Length(); n != 1 {
			t.Errorf("%s: %d elements have id %q, want 1", what, n, id)
		}
		if strings.HasPrefix(id, "f-lines-") {
			if n := page.Find(`#m26-body input[id="` + id + `"]`).Length(); n != 1 {
				t.Errorf("%s: link %s isn't the Body's own input", what, href)
			}
		}
	}
	wantNoDeadLinks(t, rec)
}

// m26Hdr names the htmx variant.
var m26Hdr = map[bool][]string{false: nil, true: {"HX-Request", "true"}}

// HOOK-044: on an Items step with a Body, a line error with a field links to
// LineFieldID(line, field) when the Body rendered it, from Save.Errors, from
// the real engine's 422 on a save and from a submit's 422 that lands on the
// step; a whole-lines error, a line error without a field, and a line error
// whose id the Body didn't render link to the step heading. No link is dead.
func TestModule_BodyLineErrorLinks(t *testing.T) {
	if got := docscreen.LineFieldID(2, "qty"); got != "f-lines-2-qty" || got != components.FieldID("lines[2].qty") {
		t.Errorf(`LineFieldID(2, "qty") = %q, want f-lines-2-qty, components.FieldID("lines[2].qty")`, got)
	}
	if got := docscreen.LineFieldID(12, "received_qty"); got != "f-lines-12-received_qty" {
		t.Errorf(`LineFieldID(12, "received_qty") = %q, want f-lines-12-received_qty`, got)
	}

	e, _, id := m02Env(t)
	base := m02URL(id)
	items := base + "/edit/items"
	post := func(t *testing.T, hx bool) *httptest.ResponseRecorder {
		t.Helper()
		rec := e.post(t, items, url.Values{"version": {m02Version(t, e, id)}, "m26_qty_1": {"10"}, "m26_qty_2": {"5"}}, m26Hdr[hx]...)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("POST items = %d, want 422; body %.500s", rec.Code, rec.Body)
		}
		return rec
	}

	t.Run("Save.Errors", func(t *testing.T) {
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"items": {Body: m26Body(m26BodyOpts{}), Save: m26SaveErrors(
			doctype.FieldError{Line: 2, Field: "qty", Message: m26QtyMsg},
			doctype.FieldError{Field: "lines", Message: m26LinesMsg},
		)}}})
		for _, hx := range []bool{false, true} {
			what := m17What("Save.Errors on line 2 qty and the lines", hx)
			m26WantLinks(t, post(t, hx), map[string]string{m26QtyMsg: "#f-lines-2-qty", m26LinesMsg: "#step-title"}, what)
		}
	})

	t.Run("a line error without a field", func(t *testing.T) {
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"items": {Body: m26Body(m26BodyOpts{}), Save: m26SaveErrors(
			doctype.FieldError{Line: 2, Message: m26LineMsg},
		)}}})
		m26WantLinks(t, post(t, false), map[string]string{m26LineMsg: "#step-title"}, "Save.Errors on line 2, no field")
	})

	t.Run("an id the Body didn't render", func(t *testing.T) {
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"items": {Body: m26Body(m26BodyOpts{omit: 2}), Save: m26SaveErrors(
			doctype.FieldError{Line: 1, Field: "qty", Message: m26Qty1Msg},
			doctype.FieldError{Line: 2, Field: "qty", Message: m26QtyMsg},
		)}}})
		for _, hx := range []bool{false, true} {
			what := m17What("Save.Errors on lines 1 and 2, line 2's id not rendered", hx)
			m26WantLinks(t, post(t, hx), map[string]string{m26Qty1Msg: "#f-lines-1-qty", m26QtyMsg: "#step-title"}, what)
		}
	})

	t.Run("an id the Body rendered only as part of another attribute", func(t *testing.T) {
		// (a) line 1's id only as a prefix of another id, or inside line 11's;
		// (b) line 2's id only in a label's for, beside an input of another id;
		// (c) line 3's id only in a data-id attribute.
		body := func(docscreen.Ctx, docscreen.StepState) (templ.Component, error) {
			return m17Raw(`<div id="m26-body">` +
				`<input id="f-lines-1-qty-x" name="m26_qty_1"><input id="f-lines-11-qty" name="m26_qty_11">` +
				`<label for="f-lines-2-qty">Line 2 received</label><input id="m26-line-2" name="m26_qty_2">` +
				`<span data-id="f-lines-3-qty"></span>` +
				`</div>`), nil
		}
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"items": {Body: body, Save: m26SaveErrors(
			doctype.FieldError{Line: 1, Field: "qty", Message: m26Qty1Msg},
			doctype.FieldError{Line: 2, Field: "qty", Message: m26QtyMsg},
			doctype.FieldError{Line: 3, Field: "qty", Message: m26Qty3Msg},
		)}}})
		for _, hx := range []bool{false, true} {
			what := m17What("Save.Errors on lines 1 to 3, no input with their ids", hx)
			m26WantLinks(t, post(t, hx), map[string]string{m26Qty1Msg: "#step-title", m26QtyMsg: "#step-title", m26Qty3Msg: "#step-title"}, what)
		}
	})

	t.Run("a field the Body has no input for", func(t *testing.T) {
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"items": {Body: m26Body(m26BodyOpts{}), Save: m26SaveErrors(
			doctype.FieldError{Line: 2, Field: "note", Message: m26NoteMsg},
		)}}})
		m26WantLinks(t, post(t, false), map[string]string{m26NoteMsg: "#step-title"}, "Save.Errors on line 2 note")
	})

	t.Run("the engine's 422 on a line field", func(t *testing.T) {
		save := func(c docscreen.Ctx) (docscreen.Save, error) {
			var lines []map[string]string
			for i, l := range c.View.Lines {
				lines = append(lines, map[string]string{"id": l.ID, "qty": c.Req.PostFormValue("m26_qty_" + strconv.Itoa(i+1))})
			}
			return docscreen.Save{Lines: lines}, nil
		}
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"items": {Body: m26Body(m26BodyOpts{}), Save: save}}})
		before := m02Rec(t, e, id)
		audits := len(e.fx.Audit(t, docscreentest.Hooked, id))
		for _, hx := range []bool{false, true} {
			what := m17What("POST items with line 2 quantity 0", hx)
			rec := e.post(t, items, url.Values{"version": {strconv.Itoa(before.Version)}, "m26_qty_1": {"10"}, "m26_qty_2": {"0"}}, m26Hdr[hx]...)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("%s = %d, want 422; body %.500s", what, rec.Code, rec.Body)
			}
			m26WantLinks(t, rec, map[string]string{m26EngineMsg: "#f-lines-2-qty"}, what)
			if got := m02Attr(uitest.Doc(t, rec).Find("#f-lines-2-qty"), "value"); got != "0" {
				t.Errorf("%s: line 2's input = %q, want the typed 0 (Body redrawn from the post)", what, got)
			}
		}
		m02WantUnchanged(t, e, id, before, audits, "the refused items save")
	})

	t.Run("a submit 422 landing on the step", func(t *testing.T) {
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"items": {Body: m26Body(m26BodyOpts{})}}})
		e.fx.Exec(t, `UPDATE docscreentest_hooked_line SET qty = 0 WHERE parent_id = ? AND line_no = 2`, id)
		for _, hx := range []bool{false, true} {
			what := m17What("POST submit with line 2 quantity 0", hx)
			rec := e.post(t, base+"/submit", url.Values{"version": {m02Version(t, e, id)}}, m26Hdr[hx]...)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("%s = %d, want 422; body %.500s", what, rec.Code, rec.Body)
			}
			if got := norm(uitest.Doc(t, rec).Find("h2#step-title").Text()); got != "Items" {
				t.Fatalf("%s: heading = %q, want the submit to land on Items", what, got)
			}
			m26WantLinks(t, rec, map[string]string{m26EngineMsg: "#f-lines-2-qty"}, what)
		}
	})
}

// m26WantNoCopy fails unless the step form has no Enter copy: no submit with
// tabindex -1 and aria-hidden, and the first submit the form owns is label.
func m26WantNoCopy(t *testing.T, page *goquery.Document, label, what string) {
	t.Helper()
	if n := page.Find(`[type="submit"][tabindex="-1"], button:not([type])[tabindex="-1"], [type="submit"][aria-hidden]`).Length(); n != 0 {
		t.Errorf("%s: %d Enter copies, want none", what, n)
	}
	owned := m26Owned(page, "wizard-form")
	if len(owned) == 0 || norm(owned[0].Text()) != label {
		first := "<none>"
		if len(owned) > 0 {
			first, _ = goquery.OuterHtml(owned[0])
		}
		t.Errorf("%s: the step form's first submit is %.200s, want %q", what, first, label)
	}
}

// m26Owned is every submit button the form form owns, in tree order: a
// submit inside it without a form attribute, or one anywhere naming it.
func m26Owned(page *goquery.Document, form string) []*goquery.Selection {
	var out []*goquery.Selection
	page.Find("button, input").Each(func(_ int, s *goquery.Selection) {
		typ := strings.ToLower(s.AttrOr("type", ""))
		if goquery.NodeName(s) == "button" && typ != "" && typ != "submit" || goquery.NodeName(s) == "input" && typ != "submit" && typ != "image" {
			return
		}
		if f, ok := s.Attr("form"); ok {
			if f == form {
				out = append(out, s)
			}
			return
		}
		if s.Closest("form").AttrOr("id", "") == form {
			out = append(out, s)
		}
	})
	return out
}

// m26WantCopy fails unless the first submit #wizard-form owns is the Enter
// copy of the primary posting name=value (name "" posts nothing of its own),
// ahead of the Body's own button: an empty <button type=submit> in the step
// form, tabindex -1, aria-hidden, no form attribute, sized by a class and
// never hidden; there is one, and the bar's visible primary still reads
// label.
func m26WantCopy(t *testing.T, page *goquery.Document, name, value, label, what string) {
	t.Helper()
	owned := m26Owned(page, "wizard-form")
	if len(owned) == 0 {
		t.Fatalf("%s: #wizard-form owns no submit", what)
	}
	c := owned[0]
	html, _ := goquery.OuterHtml(c)
	if goquery.NodeName(c) != "button" || m02Attr(c, "type") != "submit" || c.Closest("#wizard-form").Length() != 1 {
		t.Errorf("%s: the step form's first submit is %.200s, want the Enter copy, a <button type=submit> inside it", what, html)
	}
	if txt := norm(c.Text()); txt != "" || c.Children().Length() != 0 {
		t.Errorf("%s: the form's first submit holds %q, want the empty Enter copy: %.200s", what, txt, html)
	}
	if m02Attr(c, "tabindex") != "-1" || m02Attr(c, "aria-hidden") != "true" {
		t.Errorf("%s: the form's first submit has tabindex %q aria-hidden %q, want -1 and true: %.200s", what, m02Attr(c, "tabindex"), m02Attr(c, "aria-hidden"), html)
	}
	if name == "" {
		if got := m02Attr(c, "name"); got != "<none>" {
			t.Errorf("%s: the Enter copy has name %q, want none, as the primary: %.200s", what, got, html)
		}
	} else if n, v := m02Attr(c, "name"), m02Attr(c, "value"); n != name || v != value {
		t.Errorf("%s: the Enter copy posts %s=%s, want the primary's %s=%s: %.200s", what, n, v, name, value, html)
	}
	for _, a := range []string{"form", "hidden", "style", "disabled"} {
		if _, ok := c.Attr(a); ok {
			t.Errorf("%s: the Enter copy has a %s attribute, want none: %.200s", what, a, html)
		}
	}
	if strings.TrimSpace(c.AttrOr("class", "")) == "" {
		t.Errorf("%s: the Enter copy has no class, want the 0×0 @utility: %.200s", what, html)
	}
	if n := page.Find(`#wizard-form [tabindex="-1"][aria-hidden="true"]`).Filter("button").Length(); n != 1 {
		t.Errorf("%s: %d Enter copies in the step form, want 1", what, n)
	}
	fill := page.Find(`button[name="m26_fill"]`)
	fillAt := -1
	for i, s := range owned {
		if s.IsSelection(fill) {
			fillAt = i
		}
	}
	if fill.Length() != 1 || fillAt <= 0 {
		t.Errorf("%s: the Body's %s is submit %d of the form, want it after the Enter copy", what, m26FillLabel, fillAt)
	}
	if p := m17Primary(t, page, what); norm(p.Text()) != label {
		t.Errorf("%s: visible primary = %q, want %q", what, norm(p.Text()), label)
	}
}

// HOOK-045: a step with a Body starts its form with the Enter copy of the
// primary, ahead of the Body's own submit, in the full page, the htmx frame
// and a 422 redraw, on the Items step and on a header step; a Step.Bar with
// Stay makes the copy post nav=stay too. A step without a Body (the generic
// step, or a Top only) has no copy.
func TestModule_EnterPrimary(t *testing.T) {
	e, _, id := m02Env(t)
	base := m02URL(id)
	items, terms := base+"/edit/items", base+"/edit/terms"
	body := m26Body(m26BodyOpts{button: true})

	t.Run("Body", func(t *testing.T) {
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{
			"items": {Body: body, Save: m26SaveErrors(doctype.FieldError{Line: 2, Field: "qty", Message: m26QtyMsg})},
			"terms": {Body: body},
		}})
		for _, hx := range []bool{false, true} {
			for _, u := range []string{items, terms} {
				what := m17What("GET "+u, hx)
				m26WantCopy(t, m02Page(t, e.get(t, u, m26Hdr[hx]...), http.StatusOK, what), "", "", "Save and continue", what)
			}
			what := m17What("POST items 422", hx)
			rec := e.post(t, items, url.Values{"version": {m02Version(t, e, id)}}, m26Hdr[hx]...)
			m26WantCopy(t, m02Page(t, rec, http.StatusUnprocessableEntity, what), "", "", "Save and continue", what)
		}
	})

	t.Run("nav=stay primary", func(t *testing.T) {
		bar := func(docscreen.Ctx, docscreen.StepState) (docscreen.StepBar, error) {
			return docscreen.StepBar{Primary: "Receive and stay", Stay: true}, nil
		}
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"items": {Body: body, Bar: bar}}})
		for _, hx := range []bool{false, true} {
			what := m17What("GET items with a Stay bar", hx)
			page := m02Page(t, e.get(t, items, m26Hdr[hx]...), http.StatusOK, what)
			m26WantCopy(t, page, "nav", "stay", "Receive and stay", what)
			m17WantStayPrimary(t, page, "Receive and stay", what)
		}
	})

	t.Run("no Body", func(t *testing.T) {
		m02Mount(e, docscreen.Module{})
		for _, hx := range []bool{false, true} {
			for _, u := range []string{items, terms} {
				what := m17What("GET "+u+" with no module step", hx)
				m26WantNoCopy(t, m02Page(t, e.get(t, u, m26Hdr[hx]...), http.StatusOK, what), "Save and continue", what)
			}
		}
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"items": m17SetTop(t, docscreen.Step{}, m17Top)}})
		what := "GET items with a Top only"
		m26WantNoCopy(t, m02Page(t, e.get(t, items), http.StatusOK, what), "Save and continue", what)
	})
}
