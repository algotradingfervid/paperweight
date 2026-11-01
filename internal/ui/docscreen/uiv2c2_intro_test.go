package docscreen_test

import (
	"net/http"
	"testing"

	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// TestModule_StepIntros (UI v2 §5.6): a Module's StepIntros is the line under
// the stepper on that step, on a draft and on /new; a step without one has
// none.
func TestModule_StepIntros(t *testing.T) {
	const detailsIntro, termsIntro = "Who you're buying from.", "How you'll pay."
	e, _, id := m02Env(t)
	m02Mount(e, docscreen.Module{StepIntros: map[string]string{"details": detailsIntro, "terms": termsIntro}})
	base := m02URL(id)
	for _, c := range []struct{ name, path, want string }{
		{"details", base + "/edit/details", detailsIntro},
		{"terms", base + "/edit/terms", termsIntro},
		{"items", base + "/edit/items", ""},
		{"/new", m02NewURL, detailsIntro},
	} {
		page := m02Page(t, e.get(t, c.path), http.StatusOK, "GET "+c.name)
		got := page.Find("#wizard > p.step-intro")
		if c.want == "" {
			if got.Length() != 0 {
				t.Errorf("%s: intro %q, want none", c.name, got.Text())
			}
			continue
		}
		if got.Length() != 1 || norm(got.Text()) != c.want {
			t.Errorf("%s: intro = %d %q, want %q", c.name, got.Length(), got.Text(), c.want)
		}
	}
}
