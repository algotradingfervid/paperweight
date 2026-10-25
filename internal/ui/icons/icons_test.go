package icons_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/icons"
)

func TestIcons_GeneratedMatchesList(t *testing.T) {
	b, err := os.ReadFile("icons.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Fields(string(b))
	if got := icons.Names(); !slices.Equal(got, want) {
		t.Fatalf("Names() = %v, icons.txt has %v", got, want)
	}
}

func TestIcons_SVGAttributes(t *testing.T) {
	var buf bytes.Buffer
	if err := icons.Icon("lock").Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	if n := doc.Find("svg").Length(); n != 1 {
		t.Fatalf("got %d svg elements, want 1: %s", n, html)
	}
	svg := doc.Find("svg.icon")
	if svg.Length() != 1 {
		t.Fatalf("svg has no class icon: %s", html)
	}
	for attr, want := range map[string]string{"aria-hidden": "true", "focusable": "false"} {
		if got, _ := svg.Attr(attr); got != want {
			t.Errorf("%s = %q, want %q", attr, got, want)
		}
	}
	if doc.Find("[style]").Length() != 0 {
		t.Errorf("icon has a style attribute: %s", html)
	}
	if strings.Contains(html, `class="lucide`) {
		t.Errorf("icon keeps Lucide's class: %s", html)
	}
	if !strings.Contains(html, "<path") {
		t.Errorf("icon has no <path: %s", html)
	}
}

func TestIcons_LicenceHeader(t *testing.T) {
	b, err := os.ReadFile("icons_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte("@license lucide-static v1.61.0 - ISC")) {
		t.Fatal("icons_gen.go lacks the lucide-static licence header")
	}
}

func TestIcons_UpToDate(t *testing.T) {
	var stderr bytes.Buffer
	cmd := exec.Command("go", "run", "./gen", "-stdout")
	cmd.Stderr = &stderr
	got, err := cmd.Output()
	if err != nil {
		t.Fatalf("go run ./gen: %v\n%s", err, stderr.String())
	}
	want, err := os.ReadFile("icons_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("icons_gen.go is out of date; run make generate")
	}
}
