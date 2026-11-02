package expenseweb_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/types"
)

// UI v2 §5.9 (over CD-326 (6) for this area): the line page's receipt area
// is the attached-file tile, with the name, "size · uploaded time" and the
// caller's words, and no uploader, both when the page draws a line's receipt
// and in the upload route's answer.
func TestExpLine_ReceiptTile(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil)
	taxi := e.withReceipts(t, c.ID, map[int]string{1: "taxi.jpg"})["taxi.jpg"]
	page := e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/1")
	m053WantTile(t, "line 1 page", page.Find("#receipt-files"), taxi)
	if page.Find(".upload.upload--card-named").Length() != 1 {
		t.Error("the page's FileUpload repeats the card's name and hint")
	}
	if got := page.Find("#receipt-status").AttrOr("data-done", ""); got != "Receipt attached." {
		t.Errorf("the status line's done text = %q", got)
	}

	v := strconv.Itoa(e.version(t, c.ID))
	rec := e.upload(t, s2Deepak, strconv.FormatInt(c.ID, 10), v, s2File("bill.jpg", s2Bytes("bill.jpg")))
	var bill files.Meta
	for _, m := range e.FilesOf(t, expense.ClaimDocType, c.ID) {
		if m.Name == "bill.jpg" {
			bill = m
		}
	}
	if bill.ID == 0 {
		t.Fatalf("bill.jpg wasn't attached (status %d, body %q)", rec.Code, rec.Body.String())
	}
	m053WantTile(t, "upload answer", s2Parse(t, rec).Selection, bill)
}

// m053WantTile checks that s holds m as one attached-file tile.
func m053WantTile(t *testing.T, what string, s *goquery.Selection, m files.Meta) {
	t.Helper()
	items := s.Find("li.attachments-tile")
	if items.Length() != 1 {
		t.Fatalf("%s: %d tiles, want 1 (%q)", what, items.Length(), s2Text(s))
	}
	if got := s2Text(items.Find(".attachments__name")); got != m.Name {
		t.Errorf("%s: tile name %q, want %q", what, got, m.Name)
	}
	if got, want := s2Text(items.Find(".attachments__meta")), files.HumanSize(m.Size)+" · uploaded "+types.FormatIST(m.UploadedAt); got != want {
		t.Errorf("%s: tile line %q, want %q", what, got, want)
	}
	if got := s2Text(items.Find(".attachments__attached")); got != "Attached to this expense" {
		t.Errorf("%s: attached line %q", what, got)
	}
	if text := s2Text(s); strings.Contains(text, "Uploaded") {
		t.Errorf("%s: shows an Uploaded line (%q)", what, text)
	}
}
