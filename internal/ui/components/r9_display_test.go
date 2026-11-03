package components_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// R9 (§5.13 item 7): a note sits above the bar's buttons, which it no longer
// replaces. With More actions the bar has the ellipsis, labelled, opening the
// sheet; alone it has its word, so a page with only More actions still has a
// readable footer. The bar shows for any of the four.
func TestActionBar_NoteButtonsAndMore(t *testing.T) {
	withdraw := &components.ButtonProps{Label: "Withdraw", Variant: components.Secondary}
	for _, c := range []struct {
		name string
		p    components.ActionBarProps
		text string
	}{
		{"note and a button", components.ActionBarProps{Note: "Ask someone else.", Secondary: withdraw, MoreSheet: "m"}, "Ask someone else.Withdraw"},
		{"buttons and more", components.ActionBarProps{Secondary: withdraw, MoreSheet: "m"}, "Withdraw"},
		{"a primary and more", components.ActionBarProps{Primary: &components.ButtonProps{Label: "Approve", Variant: components.Primary}, MoreSheet: "m"}, "Approve"},
		{"only more", components.ActionBarProps{MoreSheet: "m"}, "More actions"},
		{"only a note", components.ActionBarProps{Note: "Ask someone else."}, "Ask someone else."},
	} {
		t.Run(c.name, func(t *testing.T) {
			doc := uitest.Render(t, components.ActionBar(c.p))
			bar := doc.Find("div.actionbar")
			if bar.Length() != 1 || text(bar) != c.text {
				t.Fatalf("bar = %d %q, want %q", bar.Length(), text(bar), c.text)
			}
			open := bar.Find(`button[data-open-dialog]`)
			if c.p.MoreSheet == "" {
				if open.Length() != 0 {
					t.Errorf("%d openers, want none", open.Length())
				}
				return
			}
			if open.Length() != 1 || open.AttrOr("data-open-dialog", "") != "m" || open.AttrOr("aria-label", "") != "More actions" ||
				open.AttrOr("type", "") != "button" || open.Find("svg").Length() != 1 {
				t.Errorf("opener = %d %q %q %q", open.Length(), open.AttrOr("data-open-dialog", ""), open.AttrOr("aria-label", ""), open.AttrOr("type", ""))
			}
		})
	}
}

// R9: a More action marked Danger is never a filled button: Cancel must not
// be the loudest control on the page. It keeps its danger colour as text, and
// a plain item stays ghost.
func TestMoreActions_DangerIsQuiet(t *testing.T) {
	doc := uitest.Render(t, components.MoreActions([]components.MoreAction{
		{Label: "Cancel", Href: "/c", Danger: true}, {Label: "Print", Href: "/p"},
	}))
	if n := doc.Find(".btn-error, .btn-primary").Length(); n != 0 {
		t.Errorf("%d filled buttons, want none", n)
	}
	if got := doc.Find(`a[href="/c"]`).AttrOr("class", ""); !strings.Contains(got, "btn--danger-quiet") || !strings.Contains(got, "btn-ghost") {
		t.Errorf("Cancel's classes = %q", got)
	}
	if got := doc.Find(`a[href="/p"]`).AttrOr("class", ""); strings.Contains(got, "danger") {
		t.Errorf("Print's classes = %q", got)
	}
}

// R9 (§5.13 item 2): the block has a heading when the caller gives one and
// none otherwise, and its text under it.
func TestSuccessBlock_Title(t *testing.T) {
	doc := uitest.Render(t, components.SuccessBlock(components.SuccessProps{Title: "Approved", Message: "PO/1 has been numbered."}))
	if got := text(doc.Find(".success-block h2.success-block__title")); got != "Approved" {
		t.Errorf("title = %q", got)
	}
	if got := text(doc.Find(".success-block__text")); got != "PO/1 has been numbered." {
		t.Errorf("text = %q", got)
	}
	doc = uitest.Render(t, components.SuccessBlock(components.SuccessProps{Message: "Done."}))
	if n := doc.Find(".success-block__title").Length(); n != 0 {
		t.Errorf("%d titles with none given", n)
	}
}

// R9 review: dates and document numbers go in their own inline box, words and
// money don't, a code stays as it is, and the text is the same character for
// character, so copying it gives what the value says.
func TestWhole_Tokens(t *testing.T) {
	const period = "24-10-2026 to 30-10-2026, PO/26-27/00001."
	doc := uitest.Render(t, components.SummaryList([]components.Pair{
		{Label: "Period", Value: period},
		{Label: "Amount", Value: "₹1,250.00", Num: true},
		{Label: "Name", Value: "Well-known A-1 Traders"},
		{Label: "GSTIN", Value: "24-10-2026", Code: true},
	}))
	var got []string
	doc.Find(".whole").Each(func(_ int, s *goquery.Selection) { got = append(got, s.Text()) })
	if want := []string{"24-10-2026", "30-10-2026,", "PO/26-27/00001."}; !slices.Equal(got, want) {
		t.Errorf("whole tokens = %q, want %q", got, want)
	}
	if got := doc.Find("dd").First().Text(); got != period {
		t.Errorf("text = %q, want %q exactly", got, period)
	}
}

// R9 merge trial: a field's error keeps its example date and the full stop
// after it in one inline box (GRN bill 422 at 320 px split "31-03-2025."),
// and the message reads the same character for character.
func TestFieldMsg_ErrorDateWhole(t *testing.T) {
	const msg = "x is not a valid date. Use dd-mm-yyyy, for example 31-03-2025."
	doc := uitest.Render(t, components.TextInput(components.FieldProps{ID: "d", Name: "d", Label: "Date", Error: msg}))
	var got []string
	doc.Find("#d-msg .whole").Each(func(_ int, s *goquery.Selection) { got = append(got, s.Text()) })
	if want := []string{"31-03-2025."}; !slices.Equal(got, want) {
		t.Errorf("whole tokens = %q, want %q", got, want)
	}
	if got := doc.Find("#d-msg span").First().Text(); got != msg {
		t.Errorf("message = %q, want %q exactly", got, msg)
	}
}

// R9 merge trial, the sibling path: the error summary keeps each message's
// dates whole as the field message does, and each link reads the same.
func TestErrorSummary_DatesWhole(t *testing.T) {
	const msg = "31-02-2026 is not a valid date. Use dd-mm-yyyy, for example 31-03-2025."
	doc := uitest.Render(t, components.ErrorSummary([]components.FieldErr{{ID: "d", Message: msg}, {ID: "n", Message: "Enter the invoice number."}}))
	var got []string
	doc.Find(".error-summary li .whole").Each(func(_ int, s *goquery.Selection) { got = append(got, s.Text()) })
	if want := []string{"31-02-2026", "31-03-2025."}; !slices.Equal(got, want) {
		t.Errorf("whole tokens = %q, want %q", got, want)
	}
	if got := doc.Find(`.error-summary a[href="#d"]`).Text(); got != msg {
		t.Errorf("link text = %q, want %q exactly", got, msg)
	}
	// The link is an inline flex box: its text sits in one child, or each run
	// and each whole box would stand side by side as its own column.
	if kids := doc.Find(`.error-summary a[href="#d"]`).Children(); kids.Length() != 1 || !kids.Is("span:not(.whole)") || kids.Text() != msg {
		t.Errorf("link has %d children, want one plain span holding the message", kids.Length())
	}
	if got := doc.Find(`.error-summary a[href="#n"]`).Text(); got != "Enter the invoice number." {
		t.Errorf("second link text = %q", got)
	}
}

// R9 (GRN M8): the header amount steps down by length, each class on its own
// length boundary, and stays a tabular figure.
func TestDocHeader_AmountSteps(t *testing.T) {
	for _, c := range []struct {
		amount, want string
	}{
		{"₹5,00,000.00", "doc-head__amount num"},
		{"₹99,99,999.00", "doc-head__amount num"},
		{"₹9,99,99,999.00", "doc-head__amount doc-head__amount--long num"},
		{"₹9,99,99,99,999.00", "doc-head__amount doc-head__amount--long num"},
		{"₹99,99,99,99,999.00", "doc-head__amount doc-head__amount--huge num"},
	} {
		doc := uitest.Render(t, components.DocHeader(components.DocHeaderProps{Title: "PO", Amount: c.amount}))
		if got := doc.Find("p.doc-head__amount").AttrOr("class", ""); got != c.want {
			t.Errorf("%s: class = %q, want %q", c.amount, got, c.want)
		}
	}
}
