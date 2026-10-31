package components_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/components"
)

// m22Save is the page's Save that AlsoDisable names in HOOK-040.
const m22Save = "#m22-save"

// m22Render is FileUpload p's markup and its parsed document.
func m22Render(t *testing.T, p components.UploadProps) (string, *goquery.Document) {
	t.Helper()
	var buf bytes.Buffer
	if err := components.FileUpload(p).Render(t.Context(), &buf); err != nil {
		t.Fatal(err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatal(err)
	}
	return buf.String(), doc
}

// m22UploadForms are the forms holding a file input: the upload controls,
// never a Remove confirm.
func m22UploadForms(doc *goquery.Document) *goquery.Selection {
	return doc.Find("form").FilterFunction(func(_ int, f *goquery.Selection) bool {
		return f.Find(`input[type="file"]`).Length() > 0
	})
}

// m22Selectors is a hx-disabled-elt value split into its trimmed selectors.
func m22Selectors(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		out = append(out, strings.TrimSpace(s))
	}
	return out
}

// HOOK-040 (CD-275 UX-E5): with UploadProps.AlsoDisable every upload form's
// hx-disabled-elt is its own file inputs, then that selector; nothing else in
// the markup changes, and a zero AlsoDisable gives today's markup (the
// upload's own inputs only), with and without the camera and with files that
// have a Remove confirm.
func TestFileUpload_AlsoDisable(t *testing.T) {
	files := []components.Attachment{{ID: 7, Kind: "image", Name: "bill.jpg", Size: "1.2 MB", UploadedBy: "Deepak Nair", UploadedAt: "24-10-2026 10:30", URL: "/files/7", RemoveURL: "/files/7/remove"}}
	for _, c := range []struct {
		name  string
		p     components.UploadProps
		forms int
	}{
		{name: "camera and file", p: components.UploadProps{ID: "receipt", Label: "Receipt", Action: "/modules/expense/receipt"}, forms: 2},
		{name: "no camera", p: components.UploadProps{ID: "receipt", Label: "Receipt", Action: "/modules/expense/receipt", NoCamera: true}, forms: 1},
		{name: "with files and a target", p: components.UploadProps{ID: "receipt", Label: "Receipt", Action: "/modules/expense/receipt", Target: "#receipt-files", Files: files}, forms: 2},
	} {
		t.Run(c.name, func(t *testing.T) {
			own := "#" + c.p.ID + " input[type=file]"
			zeroHTML, zero := m22Render(t, c.p)
			withP := c.p
			withP.AlsoDisable = m22Save
			withHTML, with := m22Render(t, withP)

			if n := m22UploadForms(zero).Length(); n != c.forms {
				t.Fatalf("zero AlsoDisable: %d upload forms, want %d", n, c.forms)
			}
			m22UploadForms(zero).Each(func(i int, f *goquery.Selection) {
				if got := f.AttrOr("hx-disabled-elt", ""); got != own {
					t.Errorf("zero AlsoDisable: upload form %d hx-disabled-elt = %q, want today's %q", i, got, own)
				}
			})

			forms := m22UploadForms(with)
			if forms.Length() != c.forms {
				t.Fatalf("AlsoDisable %q: %d upload forms, want %d", m22Save, forms.Length(), c.forms)
			}
			var values []string
			forms.Each(func(i int, f *goquery.Selection) {
				got := f.AttrOr("hx-disabled-elt", "")
				values = append(values, got)
				sel := m22Selectors(got)
				if len(sel) != 2 || sel[0] != own || sel[1] != m22Save {
					t.Errorf("AlsoDisable %q: upload form %d hx-disabled-elt = %q, want %q then %q", m22Save, i, got, own, m22Save)
				}
			})

			// Only the upload forms' hx-disabled-elt changes.
			back := withHTML
			for _, v := range values {
				back = strings.Replace(back, `hx-disabled-elt="`+v+`"`, `hx-disabled-elt="`+own+`"`, 1)
			}
			if back != zeroHTML {
				i := 0
				for i < len(back) && i < len(zeroHTML) && back[i] == zeroHTML[i] {
					i++
				}
				t.Errorf("AlsoDisable changes more than the upload forms' hx-disabled-elt; first difference at byte %d:\n with: %.200s\n zero: %.200s", i, back[i:], zeroHTML[i:])
			}
		})
	}
}
