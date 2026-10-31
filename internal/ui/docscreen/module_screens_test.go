package docscreen_test

import (
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/screens"
)

// M0-5 (hooks spec §6 "Tests and gates"): Screens works for any document
// DocType and its ids, so each module calls it with its own.

// Test DocTypes: m05Header keeps every header field on a "header" step and
// can't be closed; m05Amend is closed with amend, as purchase.Order is
// (CD-247 (3)).
const (
	m05Header = "m05.Header"
	m05Amend  = "m05.Amend"
	m05Ravi   = "ravi@vardhan.test"
	m05Draft  = "m05.Draft" // not submittable: drafts only, close right left blank
)

var m05Once sync.Once

func m05Register() {
	m05Once.Do(func() {
		doctype.Register(doctype.DocType{
			Name: m05Header, Label: "Header order", PluralLabel: "Header orders",
			Table: "m05_header", LinesTable: "m05_header_line", Submittable: true, CloseRight: doctype.CloseNone, Hooks: m04Hooks{},
			Fields: []doctype.Field{
				{Name: "ref", Label: "Reference", Type: doctype.Text, MaxLen: 20, Step: "header"},
				{Name: "note", Label: "Note", Type: doctype.Text, OnLine: true},
			},
		})
		doctype.Register(doctype.DocType{
			Name: m05Amend, Label: "Amend order", PluralLabel: "Amend orders",
			Table: "m05_amend", LinesTable: "m05_amend_line", Submittable: true, CloseRight: "amend", Hooks: m04Hooks{},
			Fields: []doctype.Field{
				{Name: "remarks", Label: "Remarks", Type: doctype.Text, MaxLen: 500},
				{Name: "note", Label: "Note", Type: doctype.Text, OnLine: true},
			},
		})
		doctype.Register(doctype.DocType{
			Name: m05Draft, Label: "Draft-only order", PluralLabel: "Draft-only orders",
			Table: "m05_draft", LinesTable: "m05_draft_line", Hooks: m04Hooks{},
			Fields: []doctype.Field{
				{Name: "remarks", Label: "Remarks", Type: doctype.Text, MaxLen: 500},
				{Name: "note", Label: "Note", Type: doctype.Text, OnLine: true},
			},
		})
	})
}

var m05IDs = docscreen.SampleIDs{Draft: 42, Pending: 43, Approved: 44, Rejected: 45, Cancelled: 46, Closed: 47, Lines500: 48}

func m05Find(list []screens.Screen, name string) (screens.Screen, bool) {
	i := slices.IndexFunc(list, func(s screens.Screen) bool { return s.Name == name })
	if i < 0 {
		return screens.Screen{}, false
	}
	return list[i], true
}

// The 422 and 409 variants open the DocType's first step, not "details".
func TestModule_ScreensFirstStep(t *testing.T) {
	m05Register()
	list := docscreen.Screens(m05Header, m05IDs)
	for _, v := range []string{"422", "409"} {
		s, ok := m05Find(list, m05Header+" header "+v)
		if !ok {
			t.Errorf("no screen %q", m05Header+" header "+v)
			continue
		}
		if want := "/doc/" + m05Header + "/42/edit/header"; s.URL != want || s.Setup == nil {
			t.Errorf("%s: URL %q, Setup set %v; want %q with a Setup", s.Name, s.URL, s.Setup != nil, want)
		}
	}
	for _, s := range list {
		if strings.Contains(s.URL, "/edit/"+"details") {
			t.Errorf("%s: URL %q opens a step %s doesn't have", s.Name, s.URL, m05Header)
		}
	}
	// Sample's first step still is details, so the sweep's names don't move.
	docscreentest.RegisterSample()
	if _, ok := m05Find(docscreen.Screens(docscreentest.Sample, m05IDs), docscreentest.Sample+" details 422"); !ok {
		t.Errorf("Sample lost its %q screen", docscreentest.Sample+" details 422")
	}
}

// The close screen is skipped for CloseNone (that page is a 404, CD-277) and
// signs in as a user holding the close right.
func TestModule_ScreensCloseRight(t *testing.T) {
	m05Register()
	docscreentest.RegisterSample()
	closeOf := func(name string, ids docscreen.SampleIDs) (screens.Screen, bool) {
		return m05Find(docscreen.Screens(name, ids), name+" close")
	}
	for _, s := range docscreen.Screens(m05Header, m05IDs) {
		if strings.HasSuffix(s.URL, "/close") {
			t.Errorf("%s: %s can't be closed, so its close page is a 404 and no screen", s.Name, m05Header)
		}
	}
	if s, ok := closeOf(docscreentest.Sample, m05IDs); !ok || s.As != "priya@vardhan.test" {
		t.Errorf("Sample close = %+v (found %v), want it as the sweep user, who holds submit", s, ok)
	}
	ids := m05IDs
	ids.CloseAs = m05Ravi
	if s, ok := closeOf(m05Amend, ids); !ok || s.As != m05Ravi || s.URL != "/doc/"+m05Amend+"/44/close" {
		t.Errorf("m05.Amend close = %+v (found %v), want /doc/%s/44/close as %s", s, ok, m05Amend, m05Ravi)
	}
	if s, ok := closeOf(docscreentest.Sample, ids); !ok || s.As != m05Ravi {
		t.Errorf("Sample close with CloseAs = %+v (found %v), want it as %s", s, ok, m05Ravi)
	}
	func() {
		defer func() {
			r := recover()
			if r == nil || !strings.Contains(r.(string), "amend") || !strings.Contains(r.(string), m05Amend) {
				t.Errorf("Screens(%s) with no CloseAs recovered %v; want a panic naming the amend right and the DocType", m05Amend, r)
			}
		}()
		docscreen.Screens(m05Amend, m05IDs)
	}()
}

// The states Screens visits come from the DocType: m05.Header isn't
// approvable and can't be closed, so it has no pending, rejected or closed
// document, and its ids for them may be zero.
func TestModule_ScreensStatesFromDocType(t *testing.T) {
	m05Register()
	ids := m05IDs
	ids.Pending, ids.Rejected, ids.Closed = 0, 0, 0
	list := docscreen.Screens(m05Header, ids)
	for _, s := range list {
		if strings.Contains(s.URL, "/0") {
			t.Errorf("%s: URL %q", s.Name, s.URL)
		}
	}
	for _, n := range []string{"pending", "rejected", "reject", "closed", "close"} {
		if _, ok := m05Find(list, m05Header+" "+n); ok {
			t.Errorf("screen %q kept for a DocType without that state", m05Header+" "+n)
		}
	}
	for _, n := range []string{"approved", "cancel", "cancelled", "header 422", "items 500", "delete"} {
		if _, ok := m05Find(list, m05Header+" "+n); !ok {
			t.Errorf("screen %q dropped", m05Header+" "+n)
		}
	}
}

// A DocType that isn't submittable has only drafts: its blank close right
// still reads as submit, but it has no approved document to close, so
// Screens gives no close or closed screen and needs no id past Draft.
func TestModule_ScreensNotSubmittable(t *testing.T) {
	m05Register()
	if dt, _ := doctype.Lookup(m05Draft); dt.Submittable || !dt.IsDocument() {
		t.Fatalf("%s: Submittable %v, document %v; want a document DocType that isn't submittable", m05Draft, dt.Submittable, dt.IsDocument())
	}
	ids := docscreen.SampleIDs{Draft: 42, Lines500: 48}
	var list []screens.Screen
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Screens(%s) with only Draft and Lines500 panicked: %v", m05Draft, r)
			}
		}()
		list = docscreen.Screens(m05Draft, ids)
	}()
	for _, n := range []string{"close", "closed", "approved", "cancel", "cancelled", "pending", "rejected", "reject"} {
		if _, ok := m05Find(list, m05Draft+" "+n); ok {
			t.Errorf("screen %q kept for a DocType that can't be submitted", m05Draft+" "+n)
		}
	}
	for _, n := range []string{"details 422", "items 500", "delete", "list"} {
		if _, ok := m05Find(list, m05Draft+" "+n); !ok {
			t.Errorf("screen %q dropped", m05Draft+" "+n)
		}
	}
}

// A zero id for a state the DocType has panics, naming the DocType and the
// id: Screens is test-time only, and a dropped screen would go unswept.
func TestModule_ScreensMissingIDPanics(t *testing.T) {
	m05Register()
	docscreentest.RegisterSample() // approvable, submittable, closed with submit
	for _, c := range []struct {
		name, field string
		zero        func(*docscreen.SampleIDs)
	}{
		{docscreentest.Sample, "Pending", func(i *docscreen.SampleIDs) { i.Pending = 0 }},
		{docscreentest.Sample, "Rejected", func(i *docscreen.SampleIDs) { i.Rejected = 0 }},
		{docscreentest.Sample, "Closed", func(i *docscreen.SampleIDs) { i.Closed = 0 }},
		{m05Header, "Draft", func(i *docscreen.SampleIDs) { i.Draft = 0 }},
		{m05Header, "Lines500", func(i *docscreen.SampleIDs) { i.Lines500 = 0 }},
		{m05Header, "Approved", func(i *docscreen.SampleIDs) { i.Approved = 0 }},
		{m05Header, "Cancelled", func(i *docscreen.SampleIDs) { i.Cancelled = 0 }},
	} {
		ids := m05IDs
		c.zero(&ids)
		func() {
			defer func() {
				msg, _ := recover().(string)
				if !strings.Contains(msg, c.name) || !strings.Contains(msg, "SampleIDs."+c.field) {
					t.Errorf("Screens(%s) with %s 0 recovered %q; want a panic naming both", c.name, c.field, msg)
				}
			}()
			docscreen.Screens(c.name, ids)
		}()
	}
	// Hooked isn't approvable: no Pending or Rejected needed.
	docscreentest.RegisterHooked()
	ids := m05IDs
	ids.Pending, ids.Rejected = 0, 0
	if list := docscreen.Screens(docscreentest.Hooked, ids); len(list) == 0 {
		t.Error("Screens(Hooked) is empty")
	}
}

// Unswept names each registered document DocType a sweep list misses one of
// Screens' routes for, and only those.
func TestModule_UnsweptNamesMissingDocTypes(t *testing.T) {
	m05Register()
	docscreentest.RegisterHooked()
	full := docscreen.Screens(docscreentest.Hooked, m05IDs)
	if got := docscreen.Unswept(full); slices.Contains(got, docscreentest.Hooked) || !slices.Contains(got, m05Header) {
		t.Errorf("Unswept(Hooked's screens) = %q, want m05.Header and not Hooked", got)
	}
	noSteps := slices.DeleteFunc(slices.Clone(full), func(s screens.Screen) bool { return strings.Contains(s.URL, "/edit/") })
	if got := docscreen.Unswept(noSteps); !slices.Contains(got, docscreentest.Hooked) {
		t.Errorf("Unswept(Hooked's screens without its steps) = %q, want Hooked named", got)
	}
	// Another DocType's screens don't count for this one.
	other := docscreen.Screens(m05Header, m05IDs)
	if got := docscreen.Unswept(other); !slices.Contains(got, docscreentest.Hooked) {
		t.Errorf("Unswept(m05.Header's screens) = %q, want Hooked named", got)
	}
	if !slices.IsSorted(docscreen.Unswept(nil)) {
		t.Error("Unswept is not sorted")
	}
	for _, n := range docscreen.Unswept(nil) {
		if dt, _ := doctype.Lookup(n); !dt.IsDocument() {
			t.Errorf("Unswept names %s, which is not a document DocType", n)
		}
	}
}
