package docscreen_test

import (
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// M0-2, Save.Lines (CD-256 (3), (12); CD-207).

// m02LineIDs are the stored line ids of Hooked id, in order.
func m02LineIDs(t *testing.T, e *hookedEnv, id int64) []string {
	t.Helper()
	var out []string
	for _, l := range m02Rec(t, e, id).Lines {
		out = append(out, l["id"])
	}
	return out
}

// m02WithoutID is l without its "id".
func m02WithoutID(l map[string]string) map[string]string {
	out := maps.Clone(l)
	delete(out, "id")
	return out
}

// HOOK-008: a Save returning Lines nil keeps the stored lines: docscreen
// sends the loaded lines, ids and all (the engine treats nil as delete-all),
// with Only the step's fields alone.
func TestModule_LinesKept(t *testing.T) {
	e, spy, id := m02Env(t)
	log := &m02Log{}
	save := func(c docscreen.Ctx) (docscreen.Save, error) {
		return docscreen.Save{Values: map[string]string{"ref": c.Req.PostFormValue("m02_ref")}}, nil
	}
	m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"terms": {Body: log.m02Body("terms"), Save: save}}})
	base := m02URL(id)
	before := m02Rec(t, e, id)
	spy.take()
	if got := location(t, e.post(t, base+"/edit/terms", m02Typing(strconv.Itoa(before.Version), m02Typed))); got != base+"/edit/items" {
		t.Errorf("terms save went to %q, want the items step", got)
	}
	after := m02Rec(t, e, id)
	if after.Values["ref"] != m02Typed {
		t.Errorf("stored ref = %q, want the Save's %s", after.Values["ref"], m02Typed)
	}
	if !slices.EqualFunc(after.Lines, before.Lines, maps.Equal) {
		t.Errorf("stored lines = %v, want %v kept (ids and values)", after.Lines, before.Lines)
	}
	calls := spy.take()
	if len(calls) != 1 {
		t.Fatalf("Engine.Do calls = %+v, want one", calls)
	}
	if got := calls[0].In.Lines; len(got) != 2 || got[0]["id"] != before.Lines[0]["id"] || got[1]["id"] != before.Lines[1]["id"] {
		t.Errorf("Engine.Do Lines = %v, want the loaded lines with their ids", got)
	}
	if got, want := m02Sorted(calls[0].In.Only), []string{"ref", "remarks"}; !slices.Equal(got, want) {
		t.Errorf("Engine.Do Only = %q, want %q: no lines when Save.Lines is nil", calls[0].In.Only, want)
	}
}

// HOOK-017: a non-nil empty Save.Lines removes every line.
func TestModule_ClearLines(t *testing.T) {
	e, spy, id := m02Env(t)
	save := func(docscreen.Ctx) (docscreen.Save, error) {
		return docscreen.Save{Lines: []map[string]string{}}, nil
	}
	m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"items": {Save: save}}})
	base := m02URL(id)
	spy.take()
	if got := location(t, e.post(t, base+"/edit/items", m02Typing(m02Version(t, e, id), ""))); got != base+"/edit/check" {
		t.Errorf("items save went to %q, want the check step", got)
	}
	if got := m02Rec(t, e, id).Lines; len(got) != 0 {
		t.Errorf("stored lines = %v, want none", got)
	}
	calls := spy.take()
	if len(calls) != 1 || calls[0].In.Lines == nil || len(calls[0].In.Lines) != 0 || !slices.Contains(calls[0].In.Only, "lines") {
		t.Errorf("Engine.Do calls = %+v, want one with empty non-nil Lines and lines in Only", calls)
	}
}

// HOOK-016: a Save returning lines with their ids updates them in place: the
// line ids are unchanged, and an entry with only {id, a changed key} is
// merged over the loaded line, the hook's keys winning. Only changed lines
// count under Only (CD-207): with line 2's item disabled since, a save
// changing line 1 by id succeeds and leaves line 2's error a Problem; the
// same lines without ids are all new and the save refuses. An id not on the
// record is a plain internal error (500), with nothing written.
func TestModule_LineIDs(t *testing.T) {
	e, spy, id := m02Env(t)
	var lines []map[string]string
	save := func(docscreen.Ctx) (docscreen.Save, error) { return docscreen.Save{Lines: lines}, nil }
	m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"items": {Save: save}}})
	base := m02URL(id)
	ids := m02LineIDs(t, e, id)
	if len(ids) != 2 || ids[0] == "" || ids[1] == "" || ids[0] == ids[1] {
		t.Fatalf("stored line ids = %q, want two distinct ids", ids)
	}
	post := func() int {
		t.Helper()
		return e.post(t, base+"/edit/items", m02Typing(m02Version(t, e, id), "")).Code
	}

	t.Run("merged in place", func(t *testing.T) {
		before := m02Rec(t, e, id)
		lines = []map[string]string{{"id": ids[0], "note": "changed"}, {"id": ids[1]}}
		spy.take()
		if code := post(); code != http.StatusSeeOther {
			t.Fatalf("items save with ids = %d, want 303", code)
		}
		after := m02Rec(t, e, id)
		if got := m02LineIDs(t, e, id); !slices.Equal(got, ids) {
			t.Errorf("line ids after the save = %q, want %q unchanged (updated in place)", got, ids)
		}
		want1 := maps.Clone(before.Lines[0])
		want1["note"] = "changed"
		if len(after.Lines) != 2 || !maps.Equal(after.Lines[0], want1) || !maps.Equal(after.Lines[1], before.Lines[1]) {
			t.Errorf("stored lines = %v, want line 1 %v (other keys kept) and line 2 %v unchanged", after.Lines, want1, before.Lines[1])
		}
		calls := spy.take()
		if len(calls) != 1 || len(calls[0].In.Lines) != 2 || calls[0].In.Lines[0]["id"] != ids[0] || calls[0].In.Lines[1]["id"] != ids[1] {
			t.Fatalf("Engine.Do calls = %+v, want one with both ids passed through", calls)
		}
		if got := calls[0].In.Lines[0]; got["note"] != "changed" || got["item_id"] != before.Lines[0]["item_id"] || got["qty"] != before.Lines[0]["qty"] {
			t.Errorf("Engine.Do line 1 = %v, want the loaded line with note changed", got)
		}
	})

	t.Run("only changed lines count", func(t *testing.T) {
		e.disable(t, "item", "WIRE-18G") // line 2's item
		problems := func() []doc.FieldError {
			t.Helper()
			var out []doc.FieldError
			err := e.fx.DB.Read(t.Context(), func(tx *db.Tx) error {
				var err error
				out, err = e.engine.Problems(t.Context(), tx, docscreentest.Hooked, id)
				return err
			})
			if err != nil {
				t.Fatalf("Problems: %v", err)
			}
			return out
		}
		if ps := problems(); !slices.ContainsFunc(ps, func(p doc.FieldError) bool { return p.Line == 2 && strings.Contains(p.Message, "is disabled") }) {
			t.Fatalf("stored problems = %+v, want line 2's disabled item: the setup decides the outcome", ps)
		}
		before := m02Rec(t, e, id)
		audits := len(e.fx.Audit(t, docscreentest.Hooked, id))
		lines = []map[string]string{m02WithoutID(before.Lines[0]), m02WithoutID(before.Lines[1])}
		lines[0]["note"] = "again"
		if code := post(); code != http.StatusUnprocessableEntity {
			t.Errorf("items save with the ids dropped = %d, want 422: every line is new, so line 2's error refuses", code)
		}
		m02WantUnchanged(t, e, id, before, audits, "the save without ids")

		lines = []map[string]string{{"id": ids[0], "note": "again"}, {"id": ids[1]}}
		if code := post(); code != http.StatusSeeOther {
			t.Fatalf("items save with ids = %d, want 303: line 2 is unchanged, its error out of scope", code)
		}
		after := m02Rec(t, e, id)
		if after.Version != before.Version+1 || after.Lines[0]["note"] != "again" || !slices.Equal(m02LineIDs(t, e, id), ids) {
			t.Errorf("after the save with ids: version %d, line 1 note %q, ids %q; want %d, again, %q", after.Version, after.Lines[0]["note"], m02LineIDs(t, e, id), before.Version+1, ids)
		}
		if ps := problems(); !slices.ContainsFunc(ps, func(p doc.FieldError) bool { return p.Line == 2 }) {
			t.Errorf("stored problems = %+v, want line 2's error kept as a Problem", ps)
		}
		e.enable(t, "item", "WIRE-18G")
	})

	t.Run("id not on the record", func(t *testing.T) {
		before := m02Rec(t, e, id)
		audits := len(e.fx.Audit(t, docscreentest.Hooked, id))
		other, err := e.draft(t, e.open, "METRO-SITE")
		if err != nil {
			t.Fatalf("saving another draft: %v", err)
		}
		foreign := e.fx.Record(t, docscreentest.Hooked, other.ID).Lines[0]["id"]
		for _, bad := range []string{"999999", foreign} {
			lines = []map[string]string{{"id": ids[0], "note": "never"}, {"id": bad, "note": "never"}}
			rec := e.post(t, base+"/edit/items", m02Typing(strconv.Itoa(before.Version), ""))
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("items save with line id %s not on the record = %d, want a plain 500; body %.300s", bad, rec.Code, rec.Body)
			}
			m02WantUnchanged(t, e, id, before, audits, "the save with id "+bad)
		}
	})
}
