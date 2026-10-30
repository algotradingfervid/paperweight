package docscreen_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// AUTH-100 (CD-245): the new-document page prefills a header ProjectFields
// field with the user's only allowed value, and leaves it empty for two
// values or none.
func TestWizardForm_NewPrefillsSingleAllowedValue(t *testing.T) {
	registerScope()
	h := fixturetest.New(t)
	scopeTables(t, h)
	a := authfake.New()
	a.Allow(h.Priya.ID, scopeReqType, auth.AllRights...)
	se := scopeMount(t, h, h.Engine, a, h.Priya)
	metro, tech := h.M.Project["PRJ-METRO"], h.M.Project["PRJ-TECHPARK"]

	value := func(what string) string {
		t.Helper()
		rec := se.get(t, "/doc/"+scopeReqType+"/new")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: GET new status = %d, body %.300s", what, rec.Code, rec.Body)
		}
		v, _ := uitest.Doc(t, rec).Find(`input[name="other_project_id"]`).First().Attr("value")
		return v
	}
	if v := value("unrestricted"); v != "" {
		t.Errorf("unrestricted: other_project_id = %q, want empty", v)
	}
	a.Restrict(h.Priya.ID, []int64{metro, tech}, nil)
	if v := value("two projects"); v != "" {
		t.Errorf("two projects: other_project_id = %q, want empty", v)
	}
	a.Restrict(h.Priya.ID, []int64{metro}, nil)
	if v := value("one project"); v != strconv.FormatInt(metro, 10) {
		t.Errorf("one project: other_project_id = %q, want PRJ-METRO's id %d", v, metro)
	}
}
