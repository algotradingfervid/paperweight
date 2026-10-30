//go:build e2e

package uibrowser

import "testing"

// The header form is hidden on a phone, so the page's own input is what a
// phone user has (design-system §6, §10).
func TestSearchPage_InputVisibleOnAPhone(t *testing.T) {
	g := startGallery(t)
	ctx := New(t)
	for _, path := range []string{"/search", "/search?q=Draft", "/search?q=no-such-thing-zz"} {
		if err := At(ctx, 320, g.URL+path); err != nil {
			t.Fatal(err)
		}
		height := eval[float64](t, ctx, `(() => {
  const e = document.querySelector('main form[action="/search"] input[name="q"]');
  return e ? e.getBoundingClientRect().height : 0;
})()`)
		if height < 44 {
			t.Errorf("%s at 320 px: search input is %.1f px high, want visible and at least 44", path, height)
		}
	}
}
