package docscreen_test

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// AD ruling extending CD-256 (9): StepLabels["items"] is the remove page's
// Back link too, on GET and on a refused remove (stale version, 409).
func TestModule_RemoveBackLabel(t *testing.T) {
	const label = "Received quantities"
	e, id := m01bHooked(t, docscreen.Module{StepLabels: map[string]string{"items": label}})
	base := m01bURL(id)
	m03Back(t, m01bOK(t, e.get(t, base+"/lines/1/remove"), "GET /lines/1/remove"), label, base+"/edit/items")

	stale := strconv.Itoa(m01bRecord(t, e, id).Version + 5)
	rec := e.post(t, base+"/lines/1/remove", url.Values{"version": {stale}})
	m03Status(t, rec.Code, http.StatusConflict, "POST /lines/1/remove with a stale version", rec.Body.String())
	m03Back(t, uitest.Doc(t, rec), label, base+"/edit/items")
}

// The remove question names the line by the module's LineCard, as the Items
// step shows it, on GET and on a refused remove.
func TestModule_RemoveNamesLineCard(t *testing.T) {
	card := func(_ docscreen.Ctx, l docscreen.LineView) components.LineCard {
		c := l.Card
		c.Item = "M03 fix card " + l.Inputs["note"]
		return c
	}
	e, id := m01bHooked(t, docscreen.Module{LineCard: card})
	base := m01bURL(id)
	const want = "Remove line 1 (M03 fix card first)?"
	page := m01bOK(t, e.get(t, base+"/lines/1/remove"), "GET /lines/1/remove")
	if got := norm(page.Find("main").Text()); !strings.Contains(got, want) {
		t.Errorf("remove page = %q, want %q", got, want)
	}

	stale := strconv.Itoa(m01bRecord(t, e, id).Version + 5)
	rec := e.post(t, base+"/lines/1/remove", url.Values{"version": {stale}})
	m03Status(t, rec.Code, http.StatusConflict, "POST /lines/1/remove with a stale version", rec.Body.String())
	if got := norm(uitest.Doc(t, rec).Find("main").Text()); !strings.Contains(got, want) {
		t.Errorf("refused remove page = %q, want %q", got, want)
	}
}

// With Module.Line there is no grid, so an htmx post to a line is Line.Save's
// and its refusal redraws Line.Body, never a grid row.
func TestModule_LineHTMXNoGridRow(t *testing.T) {
	e, id, h := m03Env(t, nil)
	base := m01bURL(id)
	before := m01bRecord(t, e, id)
	rec := e.post(t, base+"/lines/1", url.Values{"version": {strconv.Itoa(before.Version)}, "qty": {"abc"}}, "HX-Request", "true")
	m03Status(t, rec.Code, http.StatusUnprocessableEntity, "htmx POST /lines/1 with qty abc", rec.Body.String())
	m03Unchanged(t, e, id, before, "a refused htmx post")
	page := uitest.Doc(t, rec)
	if page.Find("#m03-body").Length() != 1 {
		t.Errorf("htmx refusal has no Line.Body: %.400s", rec.Body)
	}
	if n := page.Find(`[id^="grid-row-"]`).Length(); n != 0 {
		t.Errorf("htmx refusal has %d grid rows, want none", n)
	}
	if calls := h.taken(); len(calls) != 2 || calls[0].Hook != "save" || calls[1].Hook != "body" {
		t.Errorf("hook calls = %+v, want save then body", calls)
	}
}

// A Line.Body that fails on the redraw after a refusal is a 500, with
// nothing written.
func TestModule_LineRedrawBodyError(t *testing.T) {
	e, id, _ := m03Env(t, func(h *m03Hooks, _ *docscreen.Module) { h.bodyErr = errors.New("m03 redraw failed") })
	base := m01bURL(id)
	before := m01bRecord(t, e, id)
	for _, path := range []string{"/lines/1", "/lines/new"} {
		rec := e.post(t, base+path, url.Values{"version": {strconv.Itoa(before.Version)}, "qty": {"abc"}})
		m03Status(t, rec.Code, http.StatusInternalServerError, "POST "+path+" redrawn by a failing Body", rec.Body.String())
		m03Unchanged(t, e, id, before, "a failing redraw on "+path)
	}
}

// A line removed between the save and the redraw of its refusal is a 404,
// as linePost's own line-number check answers, never a panic. The Save
// removes line 2 through the engine, then refuses, so the redraw's fresh
// read has one line.
func TestModule_LineGoneBeforeRedraw(t *testing.T) {
	e, id := m01bHooked(t, docscreen.Module{})
	refused := doctype.FieldError{Field: "qty", Message: "m03 refused"}
	line := &docscreen.Line{
		Body: (&m03Hooks{e: e}).body,
		Save: func(docscreen.Ctx, int) (docscreen.Save, error) {
			rec := m01bRecord(t, e, id)
			if _, err := e.engine.Do(t.Context(), e.open, docscreentest.Hooked, id, doctype.ActionSave, doc.ActionInput{Version: rec.Version, Lines: rec.Lines[:1]}); err != nil {
				t.Errorf("removing line 2: %v", err)
			}
			return docscreen.Save{Errors: []doctype.FieldError{refused}}, nil
		},
	}
	m01bMount(e, docscreentest.Hooked, docscreen.Module{Line: line})
	rec := e.post(t, m01bURL(id)+"/lines/2", url.Values{"version": {m01bVersion(t, e, id)}, "qty": {"4"}})
	m03Status(t, rec.Code, http.StatusNotFound, "POST /lines/2 refused after line 2 was removed", rec.Body.String())
	if n := len(m01bRecord(t, e, id).Lines); n != 1 {
		t.Errorf("stored lines = %d, want the 1 left by the removal", n)
	}
}
