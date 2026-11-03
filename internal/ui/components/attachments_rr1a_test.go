package components_test

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// RR1a 1: an Attached tile with Change is the compact row (class
// attachments-change) ending in a closed Change disclosure whose summary
// says "Change" and, for screen readers, the file's name; without Change
// the tile has no disclosure and no compact class.
func TestAttachments_ChangeRow(t *testing.T) {
	a := components.Attachment{ID: 7, Kind: "image", Name: "bill.jpg", Size: "2 MB", URL: "/files/7", Attached: "Attached to this expense", Change: true}
	li := uitest.Render(t, components.Attachments([]components.Attachment{a})).Find("ul.attachments > li")
	if !li.HasClass("attachments-tile") || !li.HasClass("attachments-change") {
		t.Errorf("class %q, want attachments-tile attachments-change", li.AttrOr("class", ""))
	}
	d := li.Find("details.attachments__change")
	if _, open := d.Attr("open"); d.Length() != 1 || open {
		t.Fatalf("%d Change disclosures (open %v), want one closed", d.Length(), open)
	}
	s := d.Find("summary")
	if got := strings.Join(strings.Fields(s.Text()), " "); got != "Change bill.jpg" || s.Find(".visually-hidden").Text() != " bill.jpg" || !s.HasClass("btn") {
		t.Errorf("summary %q (hidden %q, class %q), want a button saying Change with the name hidden", got, s.Find(".visually-hidden").Text(), s.AttrOr("class", ""))
	}
	a.Change = false
	li = uitest.Render(t, components.Attachments([]components.Attachment{a})).Find("ul.attachments > li")
	if li.HasClass("attachments-change") || li.Find("details").Length() != 0 || !li.HasClass("attachments-tile") {
		t.Errorf("without Change: class %q with %d disclosures, want the plain tile", li.AttrOr("class", ""), li.Find("details").Length())
	}
}
