package docscreen_test

import (
	"slices"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// e5Facts are the header card's key facts, label=value.
func e5Facts(page *goquery.Document) []string {
	var out []string
	page.Find("main#main .doc-head dl.doc-head__facts > div").Each(func(_ int, s *goquery.Selection) {
		out = append(out, norm(s.Find("dt").Text())+"="+norm(s.Find("dd").Text()))
	})
	return out
}

// UI v2 E5 (§5.13 item 3, N14): Module.KeyFacts names the header fields the
// document header shows, in its own order. A field the module hides is left
// out (Hide on page extras). The party line goes only when the facts name the
// party field (Hooked's supplier_id); other facts, or none, keep it.
func TestDocPage_KeyFactsObeyHide(t *testing.T) {
	facts := []string{"remarks", "ref", "supplier_id", "site_id"}
	for _, c := range []struct {
		name  string
		m     docscreen.Module
		want  []string
		party bool
	}{
		{"hidden ref", docscreen.Module{KeyFacts: facts, Hide: m01bHide("ref")}, []string{"Remarks", "Supplier", "Site"}, false},
		{"nothing hidden", docscreen.Module{KeyFacts: facts}, []string{"Remarks", "Reference", "Supplier", "Site"}, false},
		{"facts without the party field", docscreen.Module{KeyFacts: []string{"site_id", "remarks"}}, []string{"Site", "Remarks"}, true},
		{"no facts", docscreen.Module{}, nil, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			e, id := m01bHooked(t, c.m)
			m01bSubmit(t, e, id)
			v, err := e.d.View(t.Context(), e.open, docscreentest.Hooked, id)
			if err != nil {
				t.Fatalf("View: %v", err)
			}
			byLabel := map[string]string{"Remarks": v.Display["remarks"], "Reference": v.Display["ref"], "Supplier": v.Display["supplier_id"], "Site": v.Display["site_id"]}
			var want []string
			for _, l := range c.want {
				if byLabel[l] == "" {
					t.Fatalf("fixture: %s has no display value", l)
				}
				want = append(want, l+"="+byLabel[l])
			}
			page := m01bOK(t, e.get(t, m01bURL(id)), "GET the document page")
			if got := e5Facts(page); !slices.Equal(got, want) {
				t.Errorf("facts = %q, want %q", got, want)
			}
			var lines []string
			page.Find(".doc-main > p").Each(func(_ int, s *goquery.Selection) { lines = append(lines, norm(s.Text())) })
			if got := slices.Contains(lines, v.Info.Party); v.Info.Party == "" || got != c.party {
				t.Errorf("party line %q among %q = %v, want %v", v.Info.Party, lines, got, c.party)
			}
		})
	}
}

// The header card holds the page's one h1 (the document's display name), the
// state badge and the progress list, and the side panel no longer repeats
// the badge. Hooked isn't Approvable: Draft (done), then Approved current.
func TestDocPage_HeaderAndTimeline(t *testing.T) {
	e, id := m01bHooked(t, docscreen.Module{})
	m01bSubmit(t, e, id)
	page := m01bOK(t, e.get(t, m01bURL(id)), "GET the document page")
	v, err := e.d.View(t.Context(), e.open, docscreentest.Hooked, id)
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	if h := page.Find("main#main h1"); h.Length() != 1 || norm(h.Text()) != v.Info.Display || !h.Parent().HasClass("doc-head__top") {
		t.Errorf("h1: %d, %q in .doc-head__top=%v; want one, %q, in the header", h.Length(), norm(h.Text()), h.Parent().HasClass("doc-head__top"), v.Info.Display)
	}
	if got := norm(page.Find(".doc-head__top .badge--approved").Text()); got != "Approved" {
		t.Errorf("header badge = %q, want Approved", got)
	}
	if n := page.Find(".doc-side .badge").Length(); n != 0 {
		t.Errorf("side panel has %d state badges, want none", n)
	}
	var steps []string
	page.Find(".doc-head ol.lifecycle > li").Each(func(_ int, s *goquery.Selection) {
		steps = append(steps, norm(s.Text())+"|"+s.AttrOr("aria-current", ""))
	})
	if want := []string{"Draft (done)|", "Approved|step"}; !slices.Equal(steps, want) {
		t.Errorf("steps = %q, want %q", steps, want)
	}
}

// A KeyFacts name that isn't one of the DocType's header fields (unknown, or
// a line field) panics when the routes are built, so a typo can't silently
// show nothing; a module for an unregistered DocType too. Header names pass.
func TestRoutes_KeyFactsMustBeHeaderFields(t *testing.T) {
	docscreentest.RegisterHooked()
	for _, c := range []struct {
		name  string
		mods  map[string]docscreen.Module
		panic bool
	}{
		{"unknown name", map[string]docscreen.Module{docscreentest.Hooked: {KeyFacts: []string{"remarks", "nosuch"}}}, true},
		{"line field", map[string]docscreen.Module{docscreentest.Hooked: {KeyFacts: []string{"note"}}}, true},
		{"unregistered DocType", map[string]docscreen.Module{"e5.Nowhere": {KeyFacts: []string{"remarks"}}}, true},
		{"header fields", map[string]docscreen.Module{docscreentest.Hooked: {KeyFacts: []string{"remarks", "supplier_id"}}, "e5.Nowhere": {}}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if got := recover() != nil; got != c.panic {
					t.Errorf("Routes panicked = %v, want %v", got, c.panic)
				}
			}()
			docscreen.Routes(docscreen.Deps{Modules: c.mods})
		})
	}
}
