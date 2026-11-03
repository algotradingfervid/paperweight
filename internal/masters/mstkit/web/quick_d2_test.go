package mstweb_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// No case ID: UI v2 D2 (§5.8, N10). The kit's quick route over a stub
// master: what it shows for each kind of refusal, and its declaration check.

// withQuick declares a quick form of name and code whose Save returns err
// (saving record 7 when nil).
func withQuick(err error) func(*screen) {
	return func(s *screen) {
		dt, _ := doctype.Lookup(s.DocType)
		code, _ := dt.Field("code")
		name, _ := dt.Field("name")
		s.Quick = &mstweb.Quick{
			Fields: []doctype.Field{code, name}, Nouns: map[string]components.Noun{"": {One: "stub", Many: "stubs"}},
			Save: func(context.Context, auth.Principal, string, map[string]string) (mstkit.Saved, error) {
				return mstkit.Saved{ID: 7}, err
			},
		}
	}
}

var stubQuick = url.Values{"role": {""}, "quick_code": {"S-1"}, "quick_name": {"Typed"}, "pick_list": {"f-stub-list"}, "pick_name": {"stub_id"}}

func TestQuickD2_KitRefusals(t *testing.T) {
	for _, c := range []struct {
		name       string
		err        error
		banner     string
		fieldError string
	}{
		{"unshown field only", &mstkit.ValidationError{Fields: []doctype.FieldError{{Field: "item_type", Message: "Type is wrong."}, {Message: "All wrong."}}}, "Type is wrong. All wrong.", ""},
		{"unshown beside shown", &mstkit.ValidationError{Fields: []doctype.FieldError{{Field: "item_type", Message: "Type is wrong."}, {Field: "name", Message: "Name is wrong."}}}, "", "Name is wrong."},
		{"no field errors", &refusal{text: "Producer's own words.", status: http.StatusUnprocessableEntity}, "Producer's own words.", ""},
	} {
		h := harness(t, stubService{rec: stubRecord{}}, withQuick(c.err))
		r := h.Browser(h.Env.Kavya).PostHTMX(t, base+"/quick", stubQuick)
		webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
		got := webtest.Squash(r.Doc.Find(".banner").Text())
		switch {
		case c.banner == "" && got != "":
			t.Errorf("%s: banner = %q, want none", c.name, got)
		case c.banner != "" && got != "Error: "+c.banner:
			t.Errorf("%s: banner = %q, want %q", c.name, got, "Error: "+c.banner)
		}
		if c.fieldError != "" {
			webtest.WantFieldError(t, r, "quick_name", c.fieldError)
		}
		if v, _ := r.Value("quick_name"); v != "Typed" {
			t.Errorf("%s: name = %q, want the typed Typed", c.name, v)
		}
	}
	// A refusal the sheet can't show, and a failure, aren't redrawn.
	h := harness(t, stubService{rec: stubRecord{}}, withQuick(&refusal{text: "No.", status: http.StatusConflict}))
	webtest.WantStatus(t, h.Browser(h.Env.Kavya).PostHTMX(t, base+"/quick", stubQuick), http.StatusConflict)
	h = harness(t, stubService{rec: stubRecord{}}, withQuick(errors.New("disk on fire")))
	r := h.Browser(h.Env.Kavya).PostHTMX(t, base+"/quick", stubQuick)
	webtest.WantStatus(t, r, http.StatusInternalServerError)
	webtest.WantNoText(t, r, "disk on fire")
}

// Without Save, ToInput and the service's Create save; a value ToInput can't
// read refuses next to its field and the service isn't called (CD-37).
func TestQuickD2_KitDefaultSave(t *testing.T) {
	calls := 0
	var in stubRecord
	plain := func(s *screen) {
		withQuick(nil)(s)
		s.Quick.Save = nil
		s.Quick.Defaults = map[string]string{"item_type": "Goods"}
	}
	h := harness(t, stubService{rec: stubRecord{}, saved: mstkit.Saved{ID: 7}, calls: &calls, in: &in}, plain)
	r := h.Browser(h.Env.Kavya).PostHTMX(t, base+"/quick", stubQuick)
	webtest.WantStatus(t, r, http.StatusOK)
	if calls != 1 || in["code"] != "S-1" || in["name"] != "Typed" || in["item_type"] != "Goods" {
		t.Errorf("Create called %d times with %v", calls, in)
	}
	if got := webtest.Squash(r.Doc.Find("[data-picker-summary]").Text()); got != "STUB Stub" {
		t.Errorf("chosen = %q, want the Head of record 7", got)
	}
	calls = 0
	h = harness(t, stubService{rec: stubRecord{}, calls: &calls}, plain, func(s *screen) {
		s.ToInput = func(v map[string]string) (stubRecord, []doctype.FieldError) {
			return v, []doctype.FieldError{{Field: "name", Message: "Name can't be read."}}
		}
	})
	r = h.Browser(h.Env.Kavya).PostHTMX(t, base+"/quick", stubQuick)
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, r, "quick_name", "Name can't be read.")
	if calls != 0 {
		t.Errorf("Create called %d times after a parse error, want 0", calls)
	}
}

// A Quick needs its fields and at least one noun.
func TestQuickD2_KitDeclarationChecked(t *testing.T) {
	for name, q := range map[string]*mstweb.Quick{
		"no fields": {Nouns: map[string]components.Noun{"": {One: "stub", Many: "stubs"}}},
		"no nouns":  {Fields: []doctype.Field{{Name: "name"}}},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: Routes didn't panic", name)
				}
			}()
			harness(t, stubService{}, func(s *screen) { s.Quick = q })
		}()
	}
}

// A master without Quick offers no New option and mounts no quick route.
func TestQuickD2_KitWithoutQuick(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{}})
	r := h.GetHTMX(t, h.Env.Kavya, "/pick/Item?q=zz")
	webtest.WantStatus(t, r, http.StatusOK)
	if n := r.Doc.Find("[data-quick], dialog").Length(); n != 0 {
		t.Errorf("%d quick-create parts without Quick, want none", n)
	}
	// Without Quick the empty text names the master by the screen's Copy.
	if got := webtest.Squash(r.Doc.Find(`[role="option"][aria-disabled="true"]`).Text()); got != `No stub matches "zz"` {
		t.Errorf("empty text = %q, want the Copy's noun", got)
	}
	if got := h.Browser(h.Env.Kavya).PostHTMX(t, base+"/quick", stubQuick).Status; got != http.StatusNotFound && got != http.StatusMethodNotAllowed {
		t.Errorf("POST %s/quick without Quick = %d, want 404 or 405", base, got)
	}
}

// The answer names the saved record only through the read check on its row
// (CD-233): with read on own records only, a saved row someone else created
// is AUTH's refusal, never a label.
func TestQuickD2_KitChosenNeedsRead(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{}}, withQuick(nil), func(s *screen) {
		save := s.Quick.Save
		s.Quick.Save = func(ctx context.Context, p auth.Principal, role string, v map[string]string) (mstkit.Saved, error) {
			saved, err := save(ctx, p, role, v)
			saved.ID = 999999 // no row, so not Kavya's own
			return saved, err
		}
	})
	a, ok := h.Env.Kit.Auth.(*auth.Auth)
	if !ok {
		t.Fatalf("Kit.Auth is %T", h.Env.Kit.Auth)
	}
	if err := a.SetRights(context.Background(), h.Env.Admin, "Masters Manager", item.DocType,
		map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeOwn, auth.RightCreate: auth.ScopeAll}); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	r := h.Browser(h.Env.Kavya).PostHTMX(t, base+"/quick", stubQuick)
	if r.Status != http.StatusNotFound && r.Status != http.StatusForbidden {
		t.Errorf("status = %d, want AUTH's 404 or 403", r.Status)
	}
	if r.Doc.Find(".picker").Length() != 0 {
		t.Error("the picker is drawn without the read check")
	}
}

// withCodeQuick is withQuick where a Save refuses the code it was given, as a
// code taken between the suggestion and the write is.
func withCodeQuick(saved *string) func(*screen) {
	return func(s *screen) {
		dt, _ := doctype.Lookup(s.DocType)
		code, _ := dt.Field("code")
		name, _ := dt.Field("name")
		s.Quick = &mstweb.Quick{
			Fields: []doctype.Field{code, name}, Nouns: map[string]components.Noun{"": {One: "stub", Many: "stubs"}},
			Save: func(_ context.Context, _ auth.Principal, _ string, v map[string]string) (mstkit.Saved, error) {
				*saved = v["code"]
				return mstkit.Saved{}, &mstkit.ValidationError{Fields: []doctype.FieldError{{Field: "code", Message: "Code is taken."}}}
			},
		}
	}
}

// R8 (CD-388): a code refused after it was generated shows, as generated.
func TestQuickR8_RefusedCodeShowsAsGenerated(t *testing.T) {
	var saved string
	h := harness(t, stubService{rec: stubRecord{}}, withCodeQuick(&saved))
	form := url.Values{}
	for k, v := range stubQuick {
		form[k] = v
	}
	form.Set("quick_code", "")
	form.Set("quick_name", "Typed Name")
	r := h.Browser(h.Env.Kavya).PostHTMX(t, base+"/quick", form)
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	if saved != "TYPEDNAME" {
		t.Errorf("code saved = %q, want the one made from the name", saved)
	}
	webtest.WantFieldError(t, r, "quick_code", "Code is taken.")
	if got := r.Doc.Find(`[name="quick_code"]`).AttrOr("value", ""); got != "TYPEDNAME" {
		t.Errorf("Code = %q, want it as generated", got)
	}
	if r.Doc.Find(`div[hidden]:has([name="quick_code"])`).Length() != 0 {
		t.Error("the refused code stays hidden")
	}
}

// R8 (CD-388): the kit's parts of the quick form. A blank code stays hidden,
// another field stays shown, and a field By gives is left blank for the save,
// hides behind the echo, and is kept when By doesn't give it.
func TestQuickR8_KitHiddenAndGiven(t *testing.T) {
	var saved map[string]string
	refuse := func(fields ...string) func(*screen) {
		return func(s *screen) {
			dt, _ := doctype.Lookup(s.DocType)
			code, _ := dt.Field("code")
			name, _ := dt.Field("name")
			code.Tip = "What the code is for."
			s.Codes = map[string]components.CodeKind{"code": components.GSTIN}
			s.Quick = &mstweb.Quick{
				Fields: []doctype.Field{code, name}, Nouns: map[string]components.Noun{"": {One: "stub", Many: "stubs"}},
				Given: map[string]mstweb.Given{"name": {By: "code", Check: components.GSTINState, Echo: func(by string) string {
					if by == "GIVES" {
						return "Given by the code"
					}
					return ""
				}}},
				Save: func(_ context.Context, _ auth.Principal, _ string, v map[string]string) (mstkit.Saved, error) {
					saved = v
					var es []doctype.FieldError
					for _, f := range fields {
						es = append(es, doctype.FieldError{Field: f, Message: f + " refused."})
					}
					return mstkit.Saved{}, &mstkit.ValidationError{Fields: es}
				},
			}
		}
	}
	post := func(h *webtest.Harness, code, name string) webtest.Response {
		form := url.Values{}
		for k, v := range stubQuick {
			form[k] = v
		}
		form.Set("quick_code", code)
		form.Set("quick_name", name)
		return h.Browser(h.Env.Kavya).PostHTMX(t, base+"/quick", form)
	}
	hiddenCode := `div[hidden]:has([name="quick_code"])`

	h := harness(t, stubService{rec: stubRecord{}}, refuse("name"))
	sheet := h.GetHTMX(t, h.Env.Kavya, "/pick/Item?q=zz").Doc.Find("dialog")
	if got := sheet.Find(`[name="quick_code"]`).AttrOr("hx-post", ""); got != "/ui/validate/gstin-state" {
		t.Errorf("By's live check = %q, want its Given.Check, not the field's own code kind", got)
	}
	if sheet.Find(`[data-gives] [name="quick_code"]`).Length() != 1 || sheet.Find(`[data-gives] #f-quick_code-tip`).Length() != 1 {
		t.Error("By's control lost the data-gives marker or control's Tip")
	}
	if sheet.Find(hiddenCode).Length() != 1 {
		t.Error("the sheet's code isn't hidden")
	}
	if sheet.Find(`.quick__given [name="quick_name"]`).Length() != 1 || sheet.Find(`div[hidden]:has([name="quick_name"])`).Length() != 0 {
		t.Error("the given field should sit in .quick__given, not in a hidden wrapper")
	}

	r := post(h, "GIVES", "Typed")
	if saved["name"] != "" {
		t.Errorf("saved name = %q, want it left to the save: By gives it", saved["name"])
	}
	if got := webtest.Squash(r.Doc.Find("#f-quick_code-msg").Text()); got != "Given by the code" {
		t.Errorf("echo = %q", got)
	}
	if r.Doc.Find(hiddenCode).Length() != 0 {
		t.Error("a typed code hides")
	}

	r = post(h, "OTHER", "Typed")
	if saved["name"] != "Typed" || r.Doc.Find(`[name="quick_name"]`).AttrOr("value", "") != "Typed" {
		t.Errorf("saved %q: a value By doesn't give is kept", saved["name"])
	}
	if got := webtest.Squash(r.Doc.Find("#f-quick_code-msg").Text()); got != "" {
		t.Errorf("echo without a gift = %q", got)
	}

	// A code refused with no name to make it from stays out of the summary.
	h = harness(t, stubService{rec: stubRecord{}}, refuse("code", "name"))
	r = post(h, "", "")
	if r.Doc.Find(hiddenCode).Length() != 1 || r.Doc.Find(".error-summary li").Length() != 1 {
		t.Errorf("blank name: Code hidden %d, summary rows %d, want 1, 1", r.Doc.Find(hiddenCode).Length(), r.Doc.Find(".error-summary li").Length())
	}
	// A typed code keeps its value and refusal.
	r = post(h, "TYPED", "")
	if got := r.Doc.Find(`[name="quick_code"]`).AttrOr("value", ""); got != "TYPED" || r.Doc.Find(".error-summary li").Length() != 2 {
		t.Errorf("typed code: value %q, summary rows %d, want TYPED, 2", got, r.Doc.Find(".error-summary li").Length())
	}
}

// R8: PickLine fills a search's rows for the row's own id and the chosen
// record of a quick save; its failure is the search's.
func TestPickR8_KitPickLine(t *testing.T) {
	line := func(_ context.Context, _ *db.Tx, _ auth.Principal, id int64) (string, error) {
		return "line " + strconv.FormatInt(id, 10), nil
	}
	found := func(s *screen) {
		s.PickLine = line
		s.PickRole = func(context.Context, auth.Principal, string, string) ([]components.PickRow, error) {
			return []components.PickRow{{ID: 5, Code: "A", Name: "Aye"}, {ID: 6, Code: "B", Name: "Bee"}}, nil
		}
	}
	h := harness(t, stubService{rec: stubRecord{}}, withQuick(nil), found)
	r := h.GetHTMX(t, h.Env.Kavya, "/pick/Item?role=any")
	webtest.WantStatus(t, r, http.StatusOK)
	rows := r.Doc.Find(`[role="option"][data-value]`)
	if rows.Length() == 0 {
		t.Fatal("no rows to carry a line")
	}
	rows.Each(func(_ int, s *goquery.Selection) {
		if got := s.Find(".picker__line").Text(); got != "line "+s.AttrOr("data-value", "") {
			t.Errorf("row %s line = %q", s.AttrOr("data-value", ""), got)
		}
	})
	c := h.Browser(h.Env.Kavya).PostHTMX(t, base+"/quick", stubQuick)
	webtest.WantStatus(t, c, http.StatusOK)
	if got := c.Doc.Find("[data-picker-line]").Text(); got != "line 7" {
		t.Errorf("chosen line = %q, want line 7", got)
	}

	failing := harness(t, stubService{rec: stubRecord{}}, found, func(s *screen) {
		s.PickLine = func(context.Context, *db.Tx, auth.Principal, int64) (string, error) {
			return "", errors.New("read failed")
		}
	})
	webtest.WantStatus(t, failing.GetHTMX(t, failing.Env.Kavya, "/pick/Item?role=any"), http.StatusInternalServerError)

	noRows := harness(t, stubService{rec: stubRecord{}}, found, func(s *screen) {
		s.PickRole = func(context.Context, auth.Principal, string, string) ([]components.PickRow, error) {
			return nil, auth.ErrBadForm
		}
	})
	webtest.WantStatus(t, noRows.GetHTMX(t, noRows.Env.Kavya, "/pick/Item?role=any"), http.StatusBadRequest)
}

// Review R8 (CD-388): a blank code saves with the code made from the name;
// one taken before the write (here the first Save takes it, as a racing save
// would) is tried once more with the next free code, then shown; a typed code
// is never retried.
func TestQuickR8_GeneratedCodeRetriedOnce(t *testing.T) {
	var codes []string
	var h *webtest.Harness
	retry := func(okOn int) func(*screen) {
		return func(s *screen) {
			dt, _ := doctype.Lookup(s.DocType)
			code, _ := dt.Field("code")
			name, _ := dt.Field("name")
			s.Quick = &mstweb.Quick{
				Fields: []doctype.Field{code, name}, Nouns: map[string]components.Noun{"": {One: "stub", Many: "stubs"}},
				Save: func(ctx context.Context, _ auth.Principal, _ string, v map[string]string) (mstkit.Saved, error) {
					codes = append(codes, v["code"])
					if len(codes) == okOn {
						return mstkit.Saved{ID: 7}, nil
					}
					if len(codes) == 1 {
						if err := h.Env.Kit.DB.Write(ctx, func(tx *db.Tx) error {
							_, err := tx.SQL().ExecContext(ctx, `INSERT INTO item (code, name, item_type, stock_uom_id, purchase_uom_id, created_at, updated_at)
								SELECT ?, 'Racer', 'Goods', id, id, '2026-10-24T00:00:00Z', '2026-10-24T00:00:00Z' FROM uom LIMIT 1`, v["code"])
							return err
						}); err != nil {
							t.Errorf("take the code: %v", err)
						}
					}
					return mstkit.Saved{}, &mstkit.ValidationError{Fields: []doctype.FieldError{{Field: "code", Message: "Code is taken."}}}
				},
			}
		}
	}
	post := func(code string) webtest.Response {
		form := url.Values{}
		for k, v := range stubQuick {
			form[k] = v
		}
		form.Set("quick_code", code)
		form.Set("quick_name", "Typed")
		return h.Browser(h.Env.Kavya).PostHTMX(t, base+"/quick", form)
	}
	for _, c := range []struct {
		name   string
		okOn   int
		code   string
		codes  []string
		status int
	}{
		{"first save", 1, "", []string{"TYPED"}, http.StatusOK},
		{"second try saves", 2, "", []string{"TYPED", "TYPED2"}, http.StatusOK},
		{"still taken", 0, "", []string{"TYPED", "TYPED2"}, http.StatusUnprocessableEntity},
		{"typed code", 2, "S-1", []string{"S-1"}, http.StatusUnprocessableEntity},
	} {
		codes = nil
		h = harness(t, stubService{rec: stubRecord{}}, retry(c.okOn))
		webtest.WantStatus(t, post(c.code), c.status)
		if !slices.Equal(codes, c.codes) {
			t.Errorf("%s: saves got codes %q, want %q", c.name, codes, c.codes)
		}
	}
}

// Review R8: PickLine runs for the rows the picker shows, never past them.
func TestPickR8_LinesOnlyForShownRows(t *testing.T) {
	asked := 0
	h := harness(t, stubService{rec: stubRecord{}}, func(s *screen) {
		s.PickLine = func(context.Context, *db.Tx, auth.Principal, int64) (string, error) { asked++; return "l", nil }
		s.PickRole = func(context.Context, auth.Principal, string, string) ([]components.PickRow, error) {
			rows := make([]components.PickRow, components.MaxPickRows+1)
			for i := range rows {
				rows[i] = components.PickRow{ID: int64(i + 1), Code: "C", Name: "N"}
			}
			return rows, nil
		}
	})
	webtest.WantStatus(t, h.GetHTMX(t, h.Env.Kavya, "/pick/Item?role=any"), http.StatusOK)
	if asked != components.MaxPickRows {
		t.Errorf("PickLine ran %d times for %d rows, want %d (the rows shown)", asked, components.MaxPickRows+1, components.MaxPickRows)
	}
}
