package expenseweb_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/modules/expense"
)

// CD-326 (6): the line page's receipt area is the compact Attachments item,
// "name · size" on one line and no "Uploaded …" line, both when the page
// draws a line's receipt and in the upload route's answer.
func TestExpLine_ReceiptCompact(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil)
	taxi := e.withReceipts(t, c.ID, map[int]string{1: "taxi.jpg"})["taxi.jpg"]
	page := e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/1")
	m053WantCompact(t, "line 1 page", page.Find("#receipt-files"), taxi)

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
	m053WantCompact(t, "upload answer", s2Parse(t, rec).Selection, bill)
}

// m053WantCompact checks that s holds m as one compact item.
func m053WantCompact(t *testing.T, what string, s *goquery.Selection, m files.Meta) {
	t.Helper()
	items := s.Find("li.attachments-compact")
	if items.Length() != 1 {
		t.Fatalf("%s: %d compact items, want 1 (%q)", what, items.Length(), s2Text(s))
	}
	if got, want := s2Text(items.Find(".attachments__meta")), m.Name+" · "+files.HumanSize(m.Size); got != want {
		t.Errorf("%s: compact line %q, want %q", what, got, want)
	}
	if text := s2Text(s); strings.Contains(text, "Uploaded") {
		t.Errorf("%s: shows an Uploaded line (%q)", what, text)
	}
}
