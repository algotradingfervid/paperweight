package components_test

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// UX design 2026-10-31 (document attachments) §6 changes 1 and 2(b), §5.4,
// ATT-007. The behaviour upload.js adds is in the gallery's browser tests.

const (
	removeTitle = "Remove this file?"
	removeBody  = "receipt.jpg will be removed from this document. You can attach it again later."
)

// confirmFiles are a removable receipt.jpg (id 7) and a quote.pdf (id 8)
// that can't be removed.
func confirmFiles() []components.Attachment {
	return []components.Attachment{
		{ID: 7, Name: "receipt.jpg", Size: "2.3 MB", UploadedBy: "Deepak Nair", UploadedAt: "24-10-2026 10:30", URL: "/files/7", RemoveURL: "/files/7/remove", Kind: "image"},
		{ID: 8, Name: "quote.pdf", Size: "512 KB", UploadedBy: "Priya Shah", UploadedAt: "24-10-2026 10:05", URL: "/files/8", Kind: "file"},
	}
}

// §6 change 1: each upload form disables both file inputs of its FileUpload
// while it posts, and an empty live region sits under the progress bar.
func TestFileUpload_BusyTargetsAndStatus(t *testing.T) {
	for _, c := range []struct {
		name   string
		p      components.UploadProps
		forms  int
		status string
	}{
		{"camera", components.UploadProps{ID: "attachments", Label: "Add a file", Action: "/attachments/fixture.Claim/42", Files: confirmFiles()}, 2, "attachments-status"},
		{"no camera", components.UploadProps{ID: "company-logo", Label: "Company logo", Action: "/m/company/1/logo", NoCamera: true, Target: "#company-logo-area"}, 1, "company-logo-status"},
	} {
		t.Run(c.name, func(t *testing.T) {
			doc := uitest.Render(t, components.FileUpload(c.p))
			forms := doc.Find(".upload__controls form")
			if forms.Length() != c.forms {
				t.Fatalf("want %d upload forms, got %d", c.forms, forms.Length())
			}
			want := "#" + c.p.ID + " input[type=file]"
			forms.Each(func(_ int, f *goquery.Selection) {
				if got, _ := f.Attr("hx-disabled-elt"); got != want {
					t.Errorf("upload form hx-disabled-elt = %q, want %q", got, want)
				}
			})
			// The selector names exactly this upload's inputs.
			if n := doc.Find(want).Length(); n != c.forms {
				t.Errorf("%q finds %d inputs, want %d", want, n, c.forms)
			}

			st := doc.Find("#" + c.status)
			if st.Length() != 1 {
				t.Fatalf("want one #%s, got %d", c.status, st.Length())
			}
			if goquery.NodeName(st) != "p" {
				t.Errorf("#%s is a <%s>, want <p>", c.status, goquery.NodeName(st))
			}
			if got, _ := st.Attr("role"); got != "status" {
				t.Errorf("#%s role = %q, want status", c.status, got)
			}
			if st.Contents().Length() != 0 {
				t.Errorf("#%s is not empty: %q (the text is inserted while uploading, so it is announced)", c.status, st.Text())
			}
			if st.Closest("#"+c.p.ID).Length() != 1 {
				t.Errorf("#%s is outside the upload", c.status)
			}
			bar := doc.Find("progress.upload__progress")
			if !before(doc, bar, st) {
				t.Errorf("#%s is not under the progress bar", c.status)
			}
			if list := doc.Find(".attachments"); list.Length() > 0 && !before(doc, st, list) {
				t.Errorf("#%s comes after the file list, want it right under the bar", c.status)
			}
		})
	}
}

// §6 change 2(a): the message region's error markup is a <template> inside
// the upload, which upload.js clones for a notice: the error icon with room
// for the text. Rendered with no error, nothing shows.
func TestFileUpload_MessageTemplate(t *testing.T) {
	doc := uitest.Render(t, components.FileUpload(components.UploadProps{ID: "attachments", Label: "Add a file", Action: "/attachments/fixture.Claim/42"}))
	tpl := doc.Find("#attachments template")
	if tpl.Length() != 1 {
		t.Fatalf("want one <template> inside the upload for upload.js, got %d", tpl.Length())
	}
	if tpl.Find("svg").Length() != 1 {
		t.Errorf("the message template has %d icons, want the alert icon", tpl.Find("svg").Length())
	}
	if tpl.Closest("form").Length() != 0 {
		t.Error("the message template is inside an upload form")
	}
	msg := doc.Find("#attachments-msg")
	if msg.Length() != 1 || strings.TrimSpace(msg.Text()) != "" || msg.HasClass("field__msg--error") {
		t.Errorf("#attachments-msg = %d element(s) %q, want one empty region without the error style", msg.Length(), msg.Text())
	}
	if doc.Find("#attachments-msg svg").Length() != 0 {
		t.Error("#attachments-msg shows an icon with no error")
	}
}

// ATT-007, §6 change 2(b), §5.4: Remove opens a confirm; the confirm's form
// is what posts.
func TestAttachments_RemoveConfirm(t *testing.T) {
	doc := uitest.Render(t, components.Attachments(confirmFiles()))

	rm := doc.Find(`[data-open-dialog]`)
	if rm.Length() != 1 {
		t.Fatalf("want one Remove opener (receipt.jpg only), got %d", rm.Length())
	}
	if goquery.NodeName(rm) != "button" {
		t.Fatalf("Remove is a <%s>, want a button", goquery.NodeName(rm))
	}
	if got, _ := rm.Attr("type"); got != "button" {
		t.Errorf("Remove type = %q, want button (it must not post)", got)
	}
	if got, _ := rm.Attr("data-open-dialog"); got != "remove-file-7" {
		t.Errorf("Remove data-open-dialog = %q, want remove-file-7", got)
	}
	if got, _ := rm.Attr("aria-label"); got != "Remove receipt.jpg" {
		t.Errorf("Remove aria-label = %q, want Remove receipt.jpg", got)
	}
	if got := text(rm); got != "Remove" {
		t.Errorf("Remove text = %q", got)
	}
	if rm.Closest("li").Find(`img[alt="receipt.jpg"]`).Length() != 1 {
		t.Error("Remove is not in receipt.jpg's item")
	}

	dlg := doc.Find("dialog#remove-file-7")
	if dlg.Length() != 1 {
		t.Fatalf("want one dialog#remove-file-7, got %d", dlg.Length())
	}
	if got := text(dlg.Find("h2")); got != removeTitle {
		t.Errorf("dialog heading = %q, want %q", got, removeTitle)
	}
	if !strings.Contains(text(dlg), removeBody) {
		t.Errorf("dialog text = %q, want %q", text(dlg), removeBody)
	}
	form := dlg.Find("form")
	if form.Length() != 1 {
		t.Fatalf("want one form in the dialog, got %d", form.Length())
	}
	if got, _ := form.Attr("action"); got != "/files/7/remove" {
		t.Errorf("dialog form action = %q", got)
	}
	if got, _ := form.Attr("method"); got != "post" {
		t.Errorf("dialog form method = %q", got)
	}
	sub := form.Find(`button[type=submit]`)
	if sub.Length() != 1 || text(sub) != "Remove file" {
		t.Fatalf("dialog form submits = %d %q, want one Remove file", sub.Length(), text(sub))
	}
	if !sub.HasClass("btn-error") || !sub.HasClass("btn--danger") {
		t.Errorf("Remove file class = %q, want the danger button", sub.AttrOr("class", ""))
	}
	if got, _ := sub.Attr("data-busy-label"); got != "Removing…" {
		t.Errorf("Remove file busy label = %q, want Removing…", got)
	}
	keep := dlg.Find(`button[data-close-dialog]`).FilterFunction(func(_ int, b *goquery.Selection) bool { return text(b) == "Keep file" })
	if keep.Length() != 1 {
		t.Fatalf("want one Keep file close button, got %d", keep.Length())
	}
	if got, _ := keep.Attr("type"); got != "button" {
		t.Errorf("Keep file type = %q, want button", got)
	}
	if keep.HasClass("btn-primary") || keep.HasClass("btn-error") {
		t.Errorf("Keep file class = %q, want secondary", keep.AttrOr("class", ""))
	}
	if dlg.Find(".btn-primary").Length() != 0 {
		t.Error("the confirm has a primary button")
	}

	// The only way to post the removal is the confirm's form.
	doc.Find(`form[action="/files/7/remove"]`).Each(func(_ int, f *goquery.Selection) {
		if f.Closest("dialog").Length() == 0 {
			t.Error("a form outside the confirm posts the removal")
		}
	})
	if n := doc.Find("dialog").Length(); n != 1 {
		t.Errorf("found %d dialogs, want one for receipt.jpg only", n)
	}
	if doc.Find("#remove-file-8").Length() != 0 || doc.Find(`[aria-label="Remove quote.pdf"]`).Length() != 0 {
		t.Error("quote.pdf, which has no RemoveURL, has a Remove or a confirm")
	}
}

// §6 change 2(b): inside a FileUpload the confirm posts with htmx and swaps
// the FileUpload's own target, so the company logo page redraws its area.
func TestFileUpload_RemoveConfirmTarget(t *testing.T) {
	for _, c := range []struct {
		name, target, want string
	}{
		{"default", "", "#attachments"},
		{"given", "#company-logo-area", "#company-logo-area"},
	} {
		t.Run(c.name, func(t *testing.T) {
			doc := uitest.Render(t, components.FileUpload(components.UploadProps{
				ID: "attachments", Label: "Add a file", Action: "/attachments/fixture.Claim/42", Target: c.target, Files: confirmFiles(),
			}))
			form := doc.Find("dialog#remove-file-7 form")
			if form.Length() != 1 {
				t.Fatalf("want one form in dialog#remove-file-7, got %d", form.Length())
			}
			if got, _ := form.Attr("hx-post"); got != "/files/7/remove" {
				t.Errorf("hx-post = %q, want /files/7/remove", got)
			}
			if got, _ := form.Attr("hx-target"); got != c.want {
				t.Errorf("hx-target = %q, want %q", got, c.want)
			}
			if form.Closest(".upload").Length() != 1 {
				t.Error("the confirm is outside the upload, so upload.js can't show its refusal")
			}
			if form.Closest(".upload__controls").Length() != 0 {
				t.Error("the confirm is inside the upload controls: its refusal would mark the inputs")
			}
		})
	}
}
