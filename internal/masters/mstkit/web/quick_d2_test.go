package mstweb_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
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
			Fields: []doctype.Field{code, name}, Nouns: map[string]string{"": "stub"},
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
		"no fields": {Nouns: map[string]string{"": "stub"}},
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
