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

func render(t *testing.T, name string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := icons.Icon(name).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

// TestIcons_Stroke: every icon draws at stroke 1.5 (UI v2 §5.1, A-31); the
// listed exceptions also render as "<name>-heavy" with the same glyph at a
// heavier stroke.
func TestIcons_Stroke(t *testing.T) {
	for _, n := range icons.Names() {
		if got := render(t, n); !strings.Contains(got, ` stroke-width="1.5" `) {
			t.Errorf("%s: no stroke-width 1.5: %s", n, got)
		}
	}
	for name, width := range map[string]string{
		"check":         "2.5", // inside a done step dot
		"plus":          "2.5", // bottom-nav New button
		"circle-dashed": "2",   // state badges at 14 px
		"clock":         "2",
		"circle-check":  "2",
		"circle-x":      "2",
		"ban":           "2",
		"circle-stop":   "2",
	} {
		base, heavy := render(t, name), render(t, name+"-heavy")
		if !strings.Contains(heavy, ` stroke-width="`+width+`" `) {
			t.Errorf("%s-heavy: no stroke-width %s: %s", name, width, heavy)
		}
		if strings.Replace(heavy, `stroke-width="`+width+`"`, `stroke-width="1.5"`, 1) != base {
			t.Errorf("%s-heavy draws a different glyph from %s", name, name)
		}
	}
}

func TestIcons_V2Glyphs(t *testing.T) {
	for _, n := range []string{
		"house", "layout-dashboard", "clipboard-list", "receipt-indian-rupee",
		"cloud-check", "circle-help", "info", "wifi-off", "file-question",
		"user-plus", "history", "shield-alert",
	} {
		if !slices.Contains(icons.Names(), n) {
			t.Errorf("icons.txt lacks %s", n)
		}
	}
}
