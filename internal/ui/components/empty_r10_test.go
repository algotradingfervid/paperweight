package components_test

import (
	"context"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/components"
)

// UI v2 R10 (§5.14): ObjectEmpty draws the icon it is given, the heading, and
// the line only when there is one; EmptyList is it with the inbox.
func TestObjectEmpty(t *testing.T) {
	doc := func(t *testing.T, icon, heading, line string) *goquery.Document {
		t.Helper()
		var b strings.Builder
		if err := components.ObjectEmpty(icon, heading, line).Render(context.Background(), &b); err != nil {
			t.Fatal(err)
		}
		d, err := goquery.NewDocumentFromReader(strings.NewReader(b.String()))
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	with, without := doc(t, "package", "No items yet", "Items you add show up here."), doc(t, "package", "All items are disabled.", "")
	if got := with.Find(".empty-state-line").Text(); got != "Items you add show up here." {
		t.Errorf("line = %q", got)
	}
	if n := without.Find(".empty-state-line").Length(); n != 0 {
		t.Errorf("no line: %d line elements, want none", n)
	}
	if got := without.Find(".empty-state-text").Text(); got != "All items are disabled." {
		t.Errorf("heading = %q", got)
	}
	pkg, _ := with.Find(".empty-state-icon svg").Html()
	inbox, _ := doc(t, "inbox", "x", "").Find(".empty-state-icon svg").Html()
	if pkg == inbox {
		t.Error("the icon argument draws the inbox")
	}
}
