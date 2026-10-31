package docscreen_test

import (
	"net/http"
	"slices"
	"strconv"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// The hooks spec §3 names, with the field types it gives. These compile only
// when module.go declares each type and field exactly as named.
var (
	_ = docscreen.Module{
		StepLabels:  map[string]string(nil),
		Steps:       map[string]docscreen.Step(nil),
		Line:        (*docscreen.Line)(nil),
		LineCard:    (func(c docscreen.Ctx, l docscreen.LineView) components.LineCard)(nil),
		CardsOnly:   false,
		NoLinePages: false,
		Hide:        (func(c docscreen.Ctx, f doctype.Field) bool)(nil),
		Check:       (func(c docscreen.Ctx) (docscreen.CheckExtra, error))(nil),
		Page:        (func(c docscreen.Ctx) (docscreen.PageExtra, error))(nil),
		Reason:      (func(c docscreen.Ctx, a doctype.Action) (docscreen.ReasonExtra, error))(nil),
	}
	_ = docscreen.Ctx{
		Req:       (*http.Request)(nil),
		Principal: auth.Principal{},
		DT:        (*doctype.DocType)(nil),
		View:      (*docscreen.DocView)(nil),
		Deps:      docscreen.Deps{},
	}
	_ = docscreen.Step{
		Body: (func(c docscreen.Ctx, s docscreen.StepState) (templ.Component, error))(nil),
		Save: (func(c docscreen.Ctx) (docscreen.Save, error))(nil),
	}
	_ = docscreen.StepState{Posted: false, Errors: []doctype.FieldError(nil)}
	_ = docscreen.Save{
		Values: map[string]string(nil),
		Lines:  []map[string]string(nil),
		Errors: []doctype.FieldError(nil),
	}
	_ = docscreen.Line{
		Body: (func(c docscreen.Ctx, no int, s docscreen.StepState) (templ.Component, error))(nil),
		Save: (func(c docscreen.Ctx, no int) (docscreen.Save, error))(nil),
	}
	_ = docscreen.CheckExtra{Notes: []string(nil), Warnings: []string(nil), Totals: []components.Pair(nil)}
	_ = docscreen.PageExtra{
		Warnings:  []string(nil),
		LineNotes: map[int]string(nil),
		Pairs:     []components.Pair(nil),
		Cards:     []templ.Component(nil),
	}
	_ = docscreen.ReasonExtra{Note: "", Hint: ""}

	_ map[string]docscreen.Module = docscreen.Deps{}.Modules
	_ string                      = docscreen.LineView{}.ID
	_ []string                    = docscreen.DocView{}.HeaderNames
)

// M0-1: DocView.HeaderNames runs parallel to Header: HeaderNames[i] is the
// name of the field Header[i] shows, so an empty field, skipped in Header,
// is skipped in HeaderNames too.
func TestModule_HeaderNames(t *testing.T) {
	cases := []struct {
		name   string
		values map[string]string
		want   []string
	}{
		{
			name:   "empty fields skipped",
			values: map[string]string{"supplier_id": "7", "order_date": "2026-10-24", "amount": "94164.00", "remarks": ""},
			want:   []string{"supplier_id", "order_date", "amount"},
		},
		{
			name:   "every header field",
			values: map[string]string{"supplier_id": "7", "order_date": "2026-10-24", "priority": "Urgent", "remarks": "Gate 2", "amount": "94164.00"},
			want:   []string{"supplier_id", "order_date", "priority", "remarks", "amount"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newDocEnv(t, allRights...)
			rec := sampleRecord()
			rec.Values = c.values
			docscreentest.Seed(e.docs, sampleInfo(42, doctype.StatePending), rec)
			v, err := e.d.View(t.Context(), priya, docscreentest.Sample, 42)
			if err != nil {
				t.Fatalf("View: %v", err)
			}
			if !slices.Equal(v.HeaderNames, c.want) {
				t.Fatalf("HeaderNames = %q, want %q", v.HeaderNames, c.want)
			}
			if len(v.Header) != len(v.HeaderNames) {
				t.Fatalf("len(Header) = %d, len(HeaderNames) = %d; want them parallel", len(v.Header), len(v.HeaderNames))
			}
			dt, _ := doctype.Lookup(docscreentest.Sample)
			for i, name := range v.HeaderNames {
				f, ok := dt.Field(name)
				if !ok {
					t.Fatalf("HeaderNames[%d] = %q names no field", i, name)
				}
				if v.Header[i].Label != f.Label || v.Header[i].Value != v.Display[name] {
					t.Errorf("Header[%d] = %+v, want field %q (label %q, value %q)", i, v.Header[i], name, f.Label, v.Display[name])
				}
			}
		})
	}
}

// M0-1, relied on by HOOK-016: LineView.ID is the stored line's id on the
// real engine, so a Save can return ids and update lines in place. The
// document is the second one created, so its line ids (3 and 4) differ from
// its line numbers (1 and 2).
func TestModule_LineViewIDReal(t *testing.T) {
	e := newScopeEnv(t)
	h := e.fx
	res := h.MustDo(t, t.Context(), h.Admin, scopeType, 0, doc.ActionSave, doc.ActionInput{
		Values: map[string]string{"supplier_id": itoa(h.M.Party["SHREE"]), "ref": "second"},
		Lines: []map[string]string{
			{"item_id": itoa(h.M.Item["WIRE-18G"]), "note": "a", "qty": "2.000"},
			{"item_id": itoa(h.M.Item["CEMENT-OPC"]), "note": "b", "qty": "3.000"},
		},
	})
	var want []string
	err := h.DB.Read(t.Context(), func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(t.Context(), `SELECT id FROM stepscope_line WHERE parent_id = ? ORDER BY line_no`, res.ID)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			want = append(want, strconv.FormatInt(id, 10))
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("reading line ids: %v", err)
	}
	if len(want) != 2 || want[0] == "1" {
		t.Fatalf("stored line ids = %q, want two ids that differ from the line numbers", want)
	}
	v, err := e.d.View(t.Context(), e.who, scopeType, res.ID)
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	var got []string
	for _, l := range v.Lines {
		got = append(got, l.ID)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("LineView IDs = %q, want the stored ids %q", got, want)
	}
}
