package components_test

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

func TestFileUpload_TwoControlsAndLimits(t *testing.T) {
	doc := uitest.Render(t, components.FileUpload(components.UploadProps{ID: "receipt", Label: "Receipt", Action: "/files/expense/4"}))
	inputs := doc.Find(`input[type="file"]`)
	if inputs.Length() != 2 {
		t.Fatalf("want two file inputs, got %d", inputs.Length())
	}
	photo := doc.Find(`label:contains("Take photo") input[type="file"]`)
	if got, _ := photo.Attr("accept"); got != "image/*" {
		t.Errorf("Take photo accept = %q", got)
	}
	if got, _ := photo.Attr("capture"); got != "environment" {
		t.Errorf("Take photo capture = %q", got)
	}
	file := doc.Find(`label:contains("Choose file") input[type="file"]`)
	if got, _ := file.Attr("accept"); got != "image/*,application/pdf" {
		t.Errorf("Choose file accept = %q", got)
	}
	if _, ok := file.Attr("multiple"); ok {
		t.Error("Choose file must not take multiple files")
	}
	if got := strings.TrimSpace(doc.Find("fieldset > legend").Text()); got != "Receipt" {
		t.Errorf("legend = %q", got)
	}
	if doc.Find(`input[capture]`).Length() != 1 {
		t.Error("capture must be on Take photo only")
	}
	hint := doc.Find(".field__hint")
	if got := strings.TrimSpace(hint.Text()); got != "JPG, PNG, HEIC or PDF, up to 10 MB" {
		t.Errorf("hint = %q", got)
	}
	if html, _ := doc.Html(); strings.Index(html, "up to 10 MB") > strings.Index(html, `type="file"`) {
		t.Error("the limits must come before the controls")
	}
	forms := doc.Find("form")
	if forms.Length() == 0 {
		t.Fatal("no form")
	}
	forms.Each(func(_ int, f *goquery.Selection) {
		if got, _ := f.Attr("hx-post"); got != "/files/expense/4" {
			t.Errorf("hx-post = %q", got)
		}
		if got, _ := f.Attr("hx-encoding"); got != "multipart/form-data" {
			t.Errorf("hx-encoding = %q", got)
		}
		if got, _ := f.Attr("hx-trigger"); got != "change" {
			t.Errorf("hx-trigger = %q", got)
		}
		if got, _ := f.Attr("hx-target"); got != "#receipt" {
			t.Errorf("hx-target = %q", got)
		}
	})
	pr := doc.Find("progress.upload__progress")
	if pr.Length() != 1 {
		t.Fatalf("want one progress.upload__progress, got %d", pr.Length())
	}
	if _, ok := pr.Attr("style"); ok {
		t.Error("progress has a style attribute")
	}
	if _, ok := pr.Attr("hidden"); !ok {
		t.Error("progress is not hidden")
	}
	if pr.AttrOr("max", "") != "100" || pr.AttrOr("value", "") != "0" {
		t.Errorf("progress max/value = %q/%q", pr.AttrOr("max", ""), pr.AttrOr("value", ""))
	}
}

// Accept, NoCamera and Target change only what they name, for a file that is
// neither a photo nor an attachment of the element it replaces.
func TestFileUpload_AcceptNoCameraTarget(t *testing.T) {
	doc := uitest.Render(t, components.FileUpload(components.UploadProps{
		ID: "sheet", Label: "CSV file", Action: "/import", Hint: "CSV", Accept: ".csv", NoCamera: true, Target: "#wizard",
	}))
	inputs := doc.Find(`input[type="file"]`)
	if inputs.Length() != 1 {
		t.Fatalf("want one file input, got %d", inputs.Length())
	}
	if got, _ := inputs.Attr("accept"); got != ".csv" {
		t.Errorf("accept = %q", got)
	}
	if doc.Find(`input[capture]`).Length() != 0 || strings.Contains(doc.Text(), "Take photo") {
		t.Error("NoCamera still offers the camera")
	}
	if got, _ := doc.Find("form").Attr("hx-target"); got != "#wizard" {
		t.Errorf("hx-target = %q", got)
	}
}

// An error marks the control and the summary can link to it.
func TestFileUpload_ErrorMarksTheInput(t *testing.T) {
	doc := uitest.Render(t, components.FileUpload(components.UploadProps{ID: "sheet", Label: "CSV file", Action: "/import", Error: "Too big."}))
	input := doc.Find("#" + components.UploadInputID("sheet"))
	if input.Length() != 1 || !strings.Contains(input.Closest("label").Text(), "Choose file") {
		t.Fatalf("UploadInputID doesn't name the Choose file input")
	}
	doc.Find(`input[type="file"]`).Each(func(_ int, s *goquery.Selection) {
		if got, _ := s.Attr("aria-invalid"); got != "true" {
			t.Errorf("aria-invalid = %q", got)
		}
	})
	if got := strings.TrimSpace(doc.Find("#sheet-msg").Text()); got != "Too big." {
		t.Errorf("message = %q", got)
	}

	clean := uitest.Render(t, components.FileUpload(components.UploadProps{ID: "sheet", Label: "CSV file", Action: "/import"}))
	if clean.Find(`input[aria-invalid]`).Length() != 0 {
		t.Error("aria-invalid without an error")
	}
}

func TestAttachments_PreviewAndHEIC(t *testing.T) {
	items := []components.Attachment{
		{ID: 1, Name: "bill.jpg", Size: "1.2 MB", UploadedBy: "Asha Rao", UploadedAt: "24-10-2026 14:45", URL: "/files/1", RemoveURL: "/files/1/remove", Kind: "image"},
		{ID: 2, Name: "IMG_0042.HEIC", Size: "2.4 MB", UploadedBy: "Asha Rao", UploadedAt: "24-10-2026 14:46", URL: "/files/2", Kind: "heic"},
		{ID: 3, Name: "quote.pdf", Size: "300 KB", UploadedBy: "Asha Rao", UploadedAt: "24-10-2026 14:47", URL: "/files/3", Kind: "pdf"},
	}
	doc := uitest.Render(t, components.Attachments(items))
	img := doc.Find(`img[loading="lazy"]`)
	if img.Length() != 1 {
		t.Fatalf("want one lazy img, got %d", img.Length())
	}
	if img.AttrOr("alt", "") != "bill.jpg" || img.AttrOr("src", "") != "/files/1" {
		t.Errorf("img alt/src = %q/%q", img.AttrOr("alt", ""), img.AttrOr("src", ""))
	}
	heic := doc.Find(`a[download]`)
	if heic.Length() != 1 {
		t.Fatalf("want one download link, got %d", heic.Length())
	}
	if got := strings.TrimSpace(heic.Text()); got != "HEIC image, opens as download" {
		t.Errorf("HEIC link = %q", got)
	}
	if heic.AttrOr("href", "") != "/files/2" {
		t.Errorf("HEIC href = %q", heic.AttrOr("href", ""))
	}
	text := doc.Text()
	for _, s := range []string{"quote.pdf", "300 KB", "Asha Rao · 24-10-2026 14:47"} {
		if !strings.Contains(text, s) {
			t.Errorf("attachments lack %q", s)
		}
	}
	rm := doc.Find(`form[method="post"]`)
	if rm.Length() != 1 || rm.AttrOr("action", "") != "/files/1/remove" {
		t.Errorf("want one Remove form for the item with RemoveURL, got %d", rm.Length())
	}
	if !strings.Contains(rm.Find("button").Text(), "Remove") {
		t.Error(`no "Remove" button`)
	}
}
