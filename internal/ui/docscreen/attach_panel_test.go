package docscreen_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// UX design 2026-10-31 (document attachments) §5.1–§5.4, ATT-002…ATT-007,
// ATT-013 and ATT-021's structure, on the sample DocType, which sets
// Attachable (docscreentest). Who may remove a file is FILE's rule
// (files.RemoveAllowed and the write right, D-FILE-06); these tests reach it
// through the page only.

const (
	attachEmpty  = "No files attached yet."
	attachLegend = "Add a file"
	attachHint   = "PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or CSV, up to 10 MB. One file at a time."
	attachAccept = "image/*,application/pdf,.xlsx,.docx,.csv"
	attachAction = "/attachments/docscreentest.Sample/42"
	sampleTotal  = "₹94,164.00"
)

// receiptJPG is receipt.jpg, id 7, as Extras gives it: no RemoveURL, which
// the page adds where FILE allows a removal.
func receiptJPG() components.Attachment {
	return components.Attachment{ID: 7, Name: "receipt.jpg", Size: "2.3 MB", UploadedBy: "Deepak Nair", UploadedAt: "24-10-2026 10:30", URL: "/files/7", Kind: "image"}
}

// attachPanel is the page's Card titled Attachments.
func attachPanel(sel *goquery.Selection) *goquery.Selection {
	return checkSection(sel, "Attachments")
}

// inOrder reports whether a's first node comes before b's in document order.
func inOrder(doc *goquery.Document, a, b *goquery.Selection) bool {
	ia, ib := -1, -1
	for i, n := range doc.Find("*").Nodes {
		if n == a.Get(0) {
			ia = i
		}
		if n == b.Get(0) {
			ib = i
		}
	}
	return ia >= 0 && ib >= 0 && ia < ib
}

// removeControls are every Remove opener, remove confirm and removal form.
func removeControls(sel *goquery.Selection) int {
	return sel.Find(`[data-open-dialog^="remove-file-"], dialog[id^="remove-file-"], form[action$="/remove"]`).Length() +
		buttonsNamed(sel, "Remove").Length()
}

// wantAdd checks the panel offers Add a file, posting to the sample's
// attachments route.
func wantAdd(t *testing.T, what string, panel *goquery.Selection) {
	t.Helper()
	if got := norm(panel.Find("legend").Text()); got != attachLegend {
		t.Errorf("%s: legend = %q, want %q", what, got, attachLegend)
	}
	forms := panel.Find(".upload__controls form")
	if forms.Length() != 2 {
		t.Errorf("%s: %d upload forms, want Take photo and Choose file", what, forms.Length())
	}
	forms.Each(func(_ int, f *goquery.Selection) {
		if got, _ := f.Attr("hx-post"); got != attachAction {
			t.Errorf("%s: upload hx-post = %q, want %q", what, got, attachAction)
		}
	})
	if panel.Find("#attachments").Length() != 1 {
		t.Errorf("%s: the panel's FileUpload is not #attachments", what)
	}
}

// wantRemove checks receipt.jpg's Remove opens its confirm, which posts to
// FILE's remove route and targets the FileUpload.
func wantRemove(t *testing.T, what string, panel *goquery.Selection) {
	t.Helper()
	rm := panel.Find(`button[aria-label="Remove receipt.jpg"]`)
	if rm.Length() != 1 {
		t.Errorf("%s: %d Remove receipt.jpg buttons, want 1", what, rm.Length())
		return
	}
	if got, _ := rm.Attr("type"); got != "button" {
		t.Errorf("%s: Remove type = %q, want button", what, got)
	}
	if got, _ := rm.Attr("data-open-dialog"); got != "remove-file-7" {
		t.Errorf("%s: Remove data-open-dialog = %q, want remove-file-7", what, got)
	}
	form := panel.Find(`dialog#remove-file-7 form`)
	if form.Length() != 1 {
		t.Errorf("%s: no form in dialog#remove-file-7", what)
		return
	}
	if got, _ := form.Attr("hx-post"); got != "/files/7/remove" {
		t.Errorf("%s: confirm hx-post = %q, want /files/7/remove", what, got)
	}
	if got, _ := form.Attr("hx-target"); got != "#attachments" {
		t.Errorf("%s: confirm hx-target = %q, want #attachments", what, got)
	}
}

// ATT-003: a writer's draft with no files has the panel after the step
// summaries and before the total, outside the step form.
func TestCheckStep_AttachmentsPanel(t *testing.T) {
	e := newFormEnv(t, allRights...)
	e.seed(doctype.StateDraft)
	res := e.get(t, doc42+"/edit/check")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body %.300s", res.Code, res.Body)
	}
	page := uitest.Doc(t, res)
	panel := attachPanel(page.Selection)
	if panel.Length() != 1 {
		t.Fatalf("found %d Attachments panels on the check step, want 1", panel.Length())
	}
	for _, title := range []string{"Details", "Items"} {
		sec := checkSection(page.Selection, title)
		if sec.Length() != 1 || !inOrder(page, sec, panel) {
			t.Errorf("the panel is not after the %q summary", title)
		}
	}
	total := page.Find("p").FilterFunction(func(_ int, p *goquery.Selection) bool {
		return norm(p.Text()) == sampleTotal && p.Closest("section.card").Length() == 0
	})
	if total.Length() != 1 {
		t.Fatalf("found %d grand totals %q outside the cards, want 1", total.Length(), sampleTotal)
	}
	if !inOrder(page, panel, total) {
		t.Error("the panel is not before the grand total")
	}
	if panel.Closest("form").Length() != 0 {
		t.Error("the panel is inside a form: Enter in it would submit the wizard")
	}

	empty := panel.Find(".empty-state")
	if got := norm(empty.Text()); got != attachEmpty {
		t.Errorf("empty state = %q, want %q", got, attachEmpty)
	}
	wantAdd(t, "check step", panel)
	if !inOrder(page, empty, panel.Find("legend")) {
		t.Error("the empty text is not above Add a file")
	}
	hint := panel.Find(".field__hint")
	if got := norm(hint.Text()); got != attachHint {
		t.Errorf("hint = %q, want %q", got, attachHint)
	}
	inputs := panel.Find(`input[type=file]`)
	if inputs.Length() != 2 {
		t.Fatalf("found %d file inputs, want 2", inputs.Length())
	}
	inputs.Each(func(_ int, in *goquery.Selection) {
		if !inOrder(page, hint, in) {
			t.Error("the hint is not before a file input")
		}
	})
	photo := panel.Find(`label:contains("Take photo") input[type=file]`)
	if photo.AttrOr("accept", "") != "image/*" || photo.AttrOr("capture", "") != "environment" {
		t.Errorf("Take photo accept/capture = %q/%q, want image/* and environment", photo.AttrOr("accept", ""), photo.AttrOr("capture", ""))
	}
	choose := panel.Find(`label:contains("Choose file") input[type=file]`)
	if got := choose.AttrOr("accept", ""); got != attachAccept {
		t.Errorf("Choose file accept = %q, want %q", got, attachAccept)
	}
	if _, ok := choose.Attr("multiple"); ok {
		t.Error("Choose file takes multiple files")
	}
	panel.Find(".upload__controls form").Each(func(_ int, f *goquery.Selection) {
		if f.AttrOr("hx-encoding", "") != "multipart/form-data" || f.AttrOr("hx-trigger", "") != "change" {
			t.Errorf("upload form hx-encoding/hx-trigger = %q/%q", f.AttrOr("hx-encoding", ""), f.AttrOr("hx-trigger", ""))
		}
	})
	if removeControls(panel) != 0 {
		t.Error("a draft with no files shows a Remove")
	}
}

// ATT-004: what a writer's panel holds in each state. A draft's page is its
// check step; Remove only in draft and rejected (D-FILE-06); no panel on a
// cancelled document.
func TestDocPage_AttachmentsByState(t *testing.T) {
	for _, c := range []struct {
		state       doctype.State
		path        string
		add, remove bool
	}{
		{doctype.StateDraft, doc42 + "/edit/check", true, true},
		{doctype.StateRejected, doc42, true, true},
		{doctype.StatePending, doc42, true, false},
		{doctype.StateApproved, doc42, true, false},
		{doctype.StateClosed, doc42, true, false},
		{doctype.StateCancelled, doc42, false, false},
	} {
		t.Run(string(c.state), func(t *testing.T) {
			e := newFormEnv(t, allRights...)
			e.seed(c.state)
			e.extras.SetAttachments(docscreentest.Sample, 42, []components.Attachment{receiptJPG()})
			res := e.get(t, c.path)
			if res.Code != http.StatusOK {
				t.Fatalf("status = %d, body %.300s", res.Code, res.Body)
			}
			page := uitest.Doc(t, res)
			main := page.Find("main#main")
			panel := attachPanel(main)
			if c.state == doctype.StateCancelled {
				heads := main.Find("h2, h3, .card-title").FilterFunction(func(_ int, h *goquery.Selection) bool { return norm(h.Text()) == "Attachments" })
				if heads.Length() != 0 || main.Find(".attachments, input[type=file]").Length() != 0 {
					t.Errorf("cancelled: the page has an Attachments section")
				}
				return
			}
			if panel.Length() != 1 {
				t.Fatalf("found %d Attachments panels, want 1", panel.Length())
			}
			if panel.Find(`img[alt="receipt.jpg"]`).Length() != 1 {
				t.Error("the panel doesn't list receipt.jpg")
			}
			if strings.Contains(norm(panel.Text()), attachEmpty) {
				t.Error("the panel says there are no files")
			}
			wantAdd(t, string(c.state), panel)
			if panel.Find("#attachments .attachments").Length() != 1 {
				t.Error("the list is not under the FileUpload's controls")
			}
			if c.remove {
				wantRemove(t, string(c.state), panel)
			} else if n := removeControls(main); n != 0 {
				t.Errorf("%s: found %d Remove controls, want none after submit", c.state, n)
			}
		})
	}
}

// ATT-005: without write the panel lists the files, or says there are none,
// and offers neither Add a file nor Remove, in any state; the approver's
// primary stays Approve.
func TestDocPage_AttachmentsReadOnly(t *testing.T) {
	for _, c := range []struct {
		name  string
		state doctype.State
		files []components.Attachment
	}{
		{"pending with a file", doctype.StatePending, []components.Attachment{receiptJPG()}},
		{"pending with none", doctype.StatePending, nil},
		{"rejected with a file", doctype.StateRejected, []components.Attachment{receiptJPG()}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newFormEnv(t, "read", "approve")
			e.seed(c.state)
			e.extras.SetAttachments(docscreentest.Sample, 42, c.files)
			res := e.get(t, doc42)
			if res.Code != http.StatusOK {
				t.Fatalf("status = %d, body %.300s", res.Code, res.Body)
			}
			page := uitest.Doc(t, res)
			main := page.Find("main#main")
			panel := attachPanel(main)
			if panel.Length() != 1 {
				t.Fatalf("found %d Attachments panels, want 1", panel.Length())
			}
			if len(c.files) > 0 {
				if panel.Find(`img[alt="receipt.jpg"]`).Length() != 1 {
					t.Error("the panel doesn't list receipt.jpg")
				}
			} else if got := norm(panel.Find(".empty-state").Text()); got != attachEmpty {
				t.Errorf("empty state = %q, want %q", got, attachEmpty)
			}
			if main.Find("input[type=file]").Length() != 0 || strings.Contains(norm(main.Text()), attachLegend) {
				t.Error("a reader is offered Add a file")
			}
			if n := removeControls(main); n != 0 {
				t.Errorf("a reader has %d Remove controls", n)
			}
			if c.state == doctype.StatePending {
				if got := norm(main.Find(".actionbar .btn-primary").Text()); got != "Approve" {
					t.Errorf("primary = %q, want Approve", got)
				}
			}
		})
	}
}

// ATT-006: the list keeps FILE's order (newest first, FILE-030) and shows
// FILE's size and the IST time. The values are made by the formatters the
// app's Extras uses (files.HumanSize, types.FormatIST), so the copy here is
// theirs.
func TestAttachmentsPanel_Formatting(t *testing.T) {
	at := func(hh, mm int) string { return types.FormatIST(time.Date(2026, 10, 24, hh, mm, 0, 0, time.UTC)) }
	for got, want := range map[string]string{
		files.HumanSize(2_456_789): "2.3 MB",
		files.HumanSize(524_288):   "512 KB",
		at(4, 30):                  "24-10-2026 10:00",
		at(4, 35):                  "24-10-2026 10:05",
		at(4, 40):                  "24-10-2026 10:10",
	} {
		if got != want {
			t.Errorf("formatter gives %q, design says %q", got, want)
		}
	}
	e := newFormEnv(t, allRights...)
	e.seed(doctype.StateDraft)
	e.extras.SetAttachments(docscreentest.Sample, 42, []components.Attachment{
		{ID: 3, Name: "challan.heic", Size: files.HumanSize(3_145_728), UploadedBy: "Deepak Nair", UploadedAt: at(4, 40), URL: "/files/3", Kind: "heic"},
		{ID: 2, Name: "quote.pdf", Size: files.HumanSize(524_288), UploadedBy: "Deepak Nair", UploadedAt: at(4, 35), URL: "/files/2", Kind: "file"},
		{ID: 1, Name: "receipt.jpg", Size: files.HumanSize(2_456_789), UploadedBy: "Deepak Nair", UploadedAt: at(4, 30), URL: "/files/1", Kind: "image"},
	})
	res := e.get(t, doc42+"/edit/check")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body %.300s", res.Code, res.Body)
	}
	panel := attachPanel(uitest.Doc(t, res).Selection)
	if panel.Length() != 1 {
		t.Fatalf("found %d Attachments panels on the check step, want 1", panel.Length())
	}
	items := panel.Find(".attachments > li")
	want := []struct{ name, meta string }{
		{"challan.heic", "Deepak Nair · 24-10-2026 10:10"},
		{"quote.pdf", "Deepak Nair · 24-10-2026 10:05"},
		{"receipt.jpg", "Deepak Nair · 24-10-2026 10:00"},
	}
	if items.Length() != len(want) {
		t.Fatalf("found %d files, want %d", items.Length(), len(want))
	}
	items.Each(func(i int, li *goquery.Selection) {
		txt := norm(li.Text())
		if !strings.Contains(txt, want[i].name) || !strings.Contains(txt, want[i].meta) {
			t.Errorf("file %d = %q, want %s with %q", i+1, txt, want[i].name, want[i].meta)
		}
	})
	receipt := items.Eq(2)
	if receipt.Find(`a[href="/files/1"] img[loading="lazy"][alt="receipt.jpg"]`).Length() != 1 {
		t.Error("receipt.jpg is not a lazy thumbnail linking to /files/1")
	}
	if txt := norm(receipt.Text()); !strings.Contains(txt, "receipt.jpg 2.3 MB Deepak Nair · 24-10-2026 10:00") {
		t.Errorf("receipt.jpg shows %q, want its name, then 2.3 MB, then Deepak Nair · 24-10-2026 10:00", txt)
	}
	pdf := items.Eq(1).Find(`a[href="/files/2"]`)
	if pdf.Length() != 1 || norm(pdf.Text()) != "quote.pdf" || pdf.Find("img").Length() != 0 {
		t.Errorf("quote.pdf is not a chip with its name: %q", norm(pdf.Text()))
	}
	if !strings.Contains(norm(items.Eq(1).Text()), "512 KB") {
		t.Error("quote.pdf lacks 512 KB")
	}
	heic := items.Eq(0).Find(`a[href="/files/3"]`)
	if _, ok := heic.Attr("download"); !ok || norm(heic.Text()) != "HEIC image, opens as download" {
		t.Errorf("challan.heic chip = %q (download %v), want HEIC image, opens as download", norm(heic.Text()), ok)
	}
}

// ATT-013: an amended draft lists the files DOC copied, uploaded by the
// amending user, each with Remove (it is a draft). That removing one leaves
// the original's file (FILE-044) is FILE's test.
func TestCheckStep_AmendedFiles(t *testing.T) {
	e := newFormEnv(t, allRights...)
	e.seed(doctype.StateDraft)
	e.extras.SetOrigin(docscreentest.Sample, 42, docscreen.Origin{AmendedFrom: "SO/26-27/00001", AmendedFromID: 40})
	copied := receiptJPG()
	copied.UploadedAt = "24-10-2026 11:15"
	e.extras.SetAttachments(docscreentest.Sample, 42, []components.Attachment{copied})
	res := e.get(t, doc42+"/edit/check")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body %.300s", res.Code, res.Body)
	}
	page := uitest.Doc(t, res)
	if page.Find(`a[href="/doc/docscreentest.Sample/40"]`).Length() == 0 {
		t.Error("the check step lost its Amended from link")
	}
	panel := attachPanel(page.Selection)
	if panel.Length() != 1 {
		t.Fatalf("found %d Attachments panels, want 1", panel.Length())
	}
	if !strings.Contains(norm(panel.Text()), "receipt.jpg") || !strings.Contains(norm(panel.Text()), "Deepak Nair · 24-10-2026 11:15") {
		t.Errorf("panel = %q, want receipt.jpg by Deepak Nair at the amend time", norm(panel.Text()))
	}
	wantRemove(t, "amended draft", panel)
}

// ATT-021, structure only (the 1440 px layout is the e2e test's): on the
// document page the panel is in the side column, under the state badge and
// above Approval history.
func TestDocPage_AttachmentsInSidePanel(t *testing.T) {
	e := newFormEnv(t, allRights...)
	e.seed(doctype.StateApproved)
	e.extras.SetApproval(docscreentest.Sample, 42, docscreen.ApprovalView{
		Needed: true, History: []docscreen.HistoryRow{{At: "24-10-2026 10:30", By: "Ravi Kulkarni", Action: "Approved"}},
	})
	e.extras.SetAttachments(docscreentest.Sample, 42, []components.Attachment{receiptJPG()})
	res := e.get(t, doc42)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body %.300s", res.Code, res.Body)
	}
	page := uitest.Doc(t, res)
	side := page.Find("main#main .doc-side")
	if side.Length() != 1 {
		t.Fatalf("found %d .doc-side, want 1", side.Length())
	}
	panel := attachPanel(side)
	if panel.Length() != 1 {
		t.Fatalf("found %d Attachments panels in .doc-side, want 1", panel.Length())
	}
	if attachPanel(page.Find(".doc-main")).Length() != 0 {
		t.Error("an Attachments panel is also in the main column")
	}
	wantAdd(t, "side panel", panel)
	if !inOrder(page, side.Find(".doc-state"), panel) {
		t.Error("the panel is not under the state badge")
	}
	hist := side.Find("details > summary").FilterFunction(func(_ int, s *goquery.Selection) bool { return norm(s.Text()) == "Approval history" })
	if hist.Length() != 1 {
		t.Fatalf("found %d Approval history disclosures in .doc-side, want 1", hist.Length())
	}
	if !inOrder(page, panel, hist) {
		t.Error("the panel is not above Approval history")
	}
}

// ATT-002: a DocType without the flag keeps today's page: a read-only card
// when the document has files (in any state but draft), and nothing on the
// check step, even for a writer. A guard: today's code passes it, and an
// implementation that ignores the flag fails it.
func TestDocPage_AttachmentsFlagOff(t *testing.T) {
	e := newFormEnv(t, allRights...)
	if dt, _ := doctype.Lookup(claimType); dt.Attachable {
		t.Fatalf("%s sets Attachable; this test needs a DocType without it", claimType)
	}
	quote := components.Attachment{ID: 9, Name: "quote.pdf", Size: "512 KB", UploadedBy: "Priya Shah", UploadedAt: "24-10-2026 10:05", URL: "/files/9", Kind: "file"}
	claim := func(id int64, s doctype.State) doctype.Info {
		info := draftInfo(claimType, "Expense Claim", id)
		info.State = s
		if s != doctype.StateDraft {
			info.Number, info.Display = "EX/26-27/0000"+itoa(id-50), "EX/26-27/0000"+itoa(id-50)
		}
		return info
	}
	rec := doc.Record{Version: 3, Values: map[string]string{"claim_date": "2026-10-24", "total": "1200.00"}}
	for _, c := range []struct {
		name  string
		id    int64
		state doctype.State
		path  string
		files []components.Attachment
		card  bool
	}{
		{"approved with a file", 51, doctype.StateApproved, "", []components.Attachment{quote}, true},
		{"cancelled with a file", 52, doctype.StateCancelled, "", []components.Attachment{quote}, true},
		{"approved with none", 53, doctype.StateApproved, "", nil, false},
		{"draft with none", 54, doctype.StateDraft, "/edit/check", nil, false},
		{"draft with a file", 55, doctype.StateDraft, "/edit/check", []components.Attachment{quote}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			docscreentest.Seed(e.docs, claim(c.id, c.state), rec)
			e.extras.SetAttachments(claimType, c.id, c.files)
			res := e.get(t, "/doc/"+claimType+"/"+itoa(c.id)+c.path)
			if res.Code != http.StatusOK {
				t.Fatalf("status = %d, body %.300s", res.Code, res.Body)
			}
			main := uitest.Doc(t, res).Find("main#main")
			panel := attachPanel(main)
			if c.card {
				if panel.Length() != 1 || !strings.Contains(norm(panel.Find(".attachments").Text()), "quote.pdf") {
					t.Errorf("want the read-only card listing quote.pdf, got %d panels", panel.Length())
				}
			} else if panel.Length() != 0 {
				t.Errorf("found %d Attachments panels, want none", panel.Length())
			}
			if main.Find("input[type=file]").Length() != 0 || strings.Contains(norm(main.Text()), attachLegend) || strings.Contains(norm(main.Text()), attachEmpty) {
				t.Error("an unflagged DocType offers Add a file or the empty text")
			}
			if n := removeControls(main); n != 0 {
				t.Errorf("an unflagged DocType has %d Remove controls", n)
			}
		})
	}
}
