package expenseweb_test

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
)

// rr1aMsgNoReceipt is spec §5's submit refusal for line 2.
const rr1aMsgNoReceipt = "Line 2: Add a receipt or say why there isn't one."

// rr1aSection is the check page section titled title: its status badge and
// each problem it lists with its link.
func rr1aSection(d *goquery.Document, title string) (status string, listed, hrefs []string) {
	sec := d.Find("section.card").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return s2Text(s.Find(".card-title").First()) == title
	})
	sec.Find("ul > li").Each(func(_ int, s *goquery.Selection) {
		listed = append(listed, s2Text(s))
		hrefs = append(hrefs, s.Find("a").AttrOr("href", ""))
	})
	return s2Text(sec.Find(".check-status")), listed, hrefs
}

// RR1a 2 (CD-397 (4), R7b): a claim whose line 2 has neither a receipt nor
// a reason shows Expenses Needs attention on Check, listing the submit
// refusal's own text linked to line 2's card; Details stays Complete. With
// a reason on every line Expenses is Complete and lists nothing. The submit
// itself is refused with the same text, and the failing step's summary
// links to #line-2, the card, not the step heading.
func TestRR1a_ClaimCheckNoReceipt(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	deepak := e.P(t, s2Deepak)
	missing := e.DraftClaim(t, deepak, nil, nil, expensetest.Values{"no_receipt_reason": ""})
	check := e.page(t, s2Deepak, s2ClaimPath(missing.ID)+"/edit/check")
	status, listed, hrefs := rr1aSection(check, "Expenses")
	if status != "Needs attention" || !slices.Equal(listed, []string{rr1aMsgNoReceipt}) || !slices.Equal(hrefs, []string{"#line-2"}) {
		t.Errorf("Expenses = %q listing %q linked to %q; want Needs attention listing %q linked to #line-2", status, listed, hrefs, rr1aMsgNoReceipt)
	}
	if status, listed, _ := rr1aSection(check, "Details"); status != "Complete" || len(listed) != 0 {
		t.Errorf("Details = %q listing %q, want Complete and nothing", status, listed)
	}
	if check.Find("#line-2").Length() != 1 {
		t.Error("the check page has no #line-2 card for the link")
	}

	ok := e.DraftClaim(t, deepak, nil, nil, nil)
	check = e.page(t, s2Deepak, s2ClaimPath(ok.ID)+"/edit/check")
	if status, listed, _ := rr1aSection(check, "Expenses"); status != "Complete" || len(listed) != 0 {
		t.Errorf("with reasons: Expenses = %q listing %q, want Complete and nothing", status, listed)
	}

	rec := e.postForm(t, s2Deepak, s2ClaimPath(missing.ID)+"/submit", url.Values{"version": {strconv.Itoa(e.version(t, missing.ID))}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("submit: status %d, want 422 (body %q)", rec.Code, rec.Body.String())
	}
	links := s2Parse(t, rec).Find(".error-summary a")
	if links.Length() != 1 || s2Text(links) != rr1aMsgNoReceipt || links.AttrOr("href", "") != "#line-2" {
		t.Errorf("submit refusal summary: %d links, %q to %q; want one, %q to #line-2", links.Length(), s2Text(links), links.AttrOr("href", ""), rr1aMsgNoReceipt)
	}
}

// RR1a 1 (UI v2 §5.9): the line page of a line with a receipt shows it as
// the compact row with Change, above the upload; the upload's answer is the
// same row.
func TestRR1a_LineReceiptRow(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	c := e.DraftClaim(t, e.P(t, s2Deepak), nil)
	e.withReceipts(t, c.ID, map[int]string{1: "taxi.jpg"})
	d := e.page(t, s2Deepak, s2ClaimPath(c.ID)+"/lines/1")
	row := d.Find("#receipt-files li.attachments-change")
	if row.Length() != 1 || row.Find("details.attachments__change summary").Length() != 1 || !strings.Contains(s2Text(row), "taxi.jpg") {
		t.Errorf("receipt rows: %d compact with Change naming taxi.jpg, want 1", row.Length())
	}
	if files, upload := d.Find("#receipt-files"), d.Find("#receipt"); files.Length() != 1 || upload.Length() != 1 || files.Index() > upload.Index() || !files.Parent().IsSelection(upload.Parent()) {
		t.Error("#receipt-files isn't above the upload in the receipt card")
	}
	rec := e.upload(t, s2Deepak, strconv.FormatInt(c.ID, 10), strconv.Itoa(e.version(t, c.ID)), s2File("bus.jpg", s2Bytes("bus.jpg")))
	if got := s2Parse(t, rec).Find("#receipt-files li.attachments-change details.attachments__change"); rec.Code != http.StatusOK || got.Length() != 1 {
		t.Errorf("upload answer %d with %d compact rows, want 200 with one", rec.Code, got.Length())
	}
}
