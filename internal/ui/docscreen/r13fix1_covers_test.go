package docscreen_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// coversDetails are the labels of the document page's Details rows when the
// module's Page returns x.
func coversDetails(t *testing.T, e *hookedEnv, id int64, x docscreen.PageExtra) []string {
	t.Helper()
	m04Mount(e, docscreentest.Hooked, docscreen.Module{
		Page: func(docscreen.Ctx) (docscreen.PageExtra, error) { return x, nil },
	})
	page := m04Page(t, e.get(t, m04URL(docscreentest.Hooked, id)), http.StatusOK, "GET document page")
	var labels []string
	checkSection(page.Selection, "Details").Find(".summary-row dt").Each(func(_ int, s *goquery.Selection) {
		labels = append(labels, norm(s.Text()))
	})
	return labels
}

// HOOK (R13, CD-393 (4)): a header field the module's page extras Cover is
// left out of Details, and only that one; with no Covers Details is as
// docscreen builds it.
func TestModule_PageExtraCovers(t *testing.T) {
	e := newHookedEnv(t)
	id := m04ApprovedHooked(t, e)
	plain := coversDetails(t, e, id, docscreen.PageExtra{})
	if !slices.Contains(plain, "Supplier") || !slices.Contains(plain, "Site") {
		t.Fatalf("Details with no Covers = %q, want Supplier and Site", plain)
	}
	covered := coversDetails(t, e, id, docscreen.PageExtra{Covers: []string{"site_id"}})
	want := slices.DeleteFunc(slices.Clone(plain), func(l string) bool { return l == "Site" })
	if !slices.Equal(covered, want) {
		t.Errorf("Details covering site_id = %q, want %q", covered, want)
	}
}
