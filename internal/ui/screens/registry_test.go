package screens

import (
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func reset(t *testing.T) {
	t.Helper()
	saved := registry
	registry = nil
	t.Cleanup(func() { registry = saved })
}

func TestScreens_UncoveredListsMissingGET(t *testing.T) {
	reset(t)
	ok := func(http.ResponseWriter, *http.Request) {}
	r := chi.NewRouter()
	r.Get("/a", ok)
	r.Get("/b/{id}", ok)
	r.Post("/c", ok)
	r.Get("/static/x", ok)
	Register(Screen{Name: "a", Route: "/a", URL: "/a"})

	got := Uncovered(r, DefaultSkip)
	if want := []string{"/b/{id}"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Uncovered = %q, want %q", got, want)
	}
}

func TestScreens_DuplicateNamePanics(t *testing.T) {
	reset(t)
	Register(Screen{Name: "x", Route: "/x"})
	defer func() {
		if recover() == nil {
			t.Fatal("second Register with the same Name did not panic")
		}
	}()
	Register(Screen{Name: "x", Route: "/y"})
}

func TestScreens_AllSortedByName(t *testing.T) {
	reset(t)
	Register(Screen{Name: "b"}, Screen{Name: "a"})
	got := All()
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "b" {
		t.Fatalf("All = %+v, want a then b", got)
	}
}

func TestScreens_OverflowJSIsResearchVerbatim(t *testing.T) {
	b, err := os.ReadFile("../../../docs/research/frontend-tech.md")
	if err != nil {
		t.Fatal(err)
	}
	const start = "const overflowJS = `"
	s := string(b)
	i := strings.Index(s, start)
	if i < 0 {
		t.Fatal("overflowJS not found in research §6.4")
	}
	s = s[i+len(start):]
	j := strings.Index(s, "`")
	if j < 0 {
		t.Fatal("overflowJS has no closing backtick")
	}
	if OverflowJS != s[:j] {
		t.Fatalf("OverflowJS differs from research §6.4:\n%s", OverflowJS)
	}
}
