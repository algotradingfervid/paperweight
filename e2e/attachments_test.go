//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/screens"
	"github.com/vardhan/internaltools/internal/ui/uibrowser"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// The attachments panel against the real app (UX design 2026-10-31 §8,
// CD-241, CD-243). Each test runs on fixture.Claim, which sets Attachable,
// in its own harness. The gallery's attach_browser_test.go covers upload.js
// against a scripted server; these rows need the real server's answers.

const (
	anil = admin // Anil Kapoor, Admin

	takePhoto  = "#attachments input[type=file][capture]"
	chooseFile = "#attachments-file"

	msgTooLarge   = "File is larger than 10 MB. Choose a smaller file or compress it."
	msgGIF        = "File type .gif is not allowed. Use PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or CSV."
	msgDropped    = "The upload didn't finish. Check your connection and try again."
	msgSubmitted  = "Files can't be removed after the document is submitted. Cancel and amend it to change files."
	removeConfirm = "Remove this file?"
)

// attachCard finds the Card titled Attachments.
const attachCard = `[...document.querySelectorAll("h2.card-title")].find((e) => e.textContent.trim() === "Attachments")?.closest("section")`

// attachCardBox is the card's top and bottom and the viewport's height.
const attachCardBox = `(() => { const r = ` + attachCard + `.getBoundingClientRect(); return [r.top, r.bottom, innerHeight]; })()`

// attachCardInView: the card's box intersects the visible area.
const attachCardInView = `(() => { const c = ` + attachCard + `; if (!c) return false; const r = c.getBoundingClientRect(); return r.bottom > 0 && r.top < innerHeight; })()`

// thumbShown: the card holds name's thumbnail, loaded.
func thumbShown(name string) string {
	return `(() => { const i = ` + attachCard + `?.querySelector(` + strconv.Quote(`.attachments__thumb img[alt="`+name+`"]`) + `); return !!i && i.complete && i.naturalWidth > 0; })()`
}

// listed is the card's file names, newest first.
const listed = `[...(` + attachCard + `)?.querySelectorAll(".attachments > li") ?? []].map((li) => (li.querySelector(".attachments__name") ?? li.querySelector(".attachments__chip span")).textContent.trim())`

const removeButtons = `document.querySelectorAll('button[data-open-dialog^="remove-file-"]').length`

// Upload state, as in the gallery's browser test.
const (
	upInputs = `[...document.querySelectorAll("#attachments .upload__controls input[type=file]")]`
	upBar    = `document.querySelector("#attachments .upload__progress")`
	upStatus = `document.getElementById("attachments-status")`
	upMsg    = `document.getElementById("attachments-msg")`
	alerts   = `document.getElementById("alerts").children.length`

	upBusy    = `(` + upInputs + `.every((i) => i.disabled) && !` + upBar + `.hidden && ` + upStatus + `.textContent.trim() === "Uploading…")`
	upIdle    = `(` + upInputs + `.every((i) => !i.disabled) && ` + upBar + `.hidden && ` + upBar + `.value === 0 && ` + upStatus + `.textContent === "")`
	upInvalid = upInputs + `.filter((i) => i.getAttribute("aria-invalid") === "true").length`
	upMarked  = upInputs + `.filter((i) => i.hasAttribute("aria-invalid")).length`
	upState   = `JSON.stringify({inputs: ` + upInputs + `.map((i) => [i.disabled, i.getAttribute("aria-invalid")]), bar: ` + upBar + `.hidden, status: ` + upStatus + `.textContent, msg: ` + upMsg + `.textContent, active: document.activeElement.outerHTML.slice(0, 120)})`

	// noReload marks the page; a reload clears it.
	markPage = `window.attachE2E = 1`
	samePage = `window.attachE2E === 1`
)

// upShows: the message region holds text in the error style with its icon.
func upShows(text string) string {
	return `(` + upMsg + `.textContent.trim() === ` + strconv.Quote(text) + ` && ` + upMsg + `.classList.contains("field__msg--error") && ` + upMsg + `.querySelector("svg") !== null)`
}

func alertsHold(text string) string {
	return `document.getElementById("alerts").textContent.includes(` + strconv.Quote(text) + `)`
}

func focusOn(sel string) string {
	return `document.activeElement === document.querySelector(` + strconv.Quote(sel) + `)`
}

// removeOf is file name's Remove button.
func removeOf(name string) string { return `button[aria-label="Remove ` + name + `"]` }

// pressSel clicks the element sel selects by position, as Press does.
func pressSel(s *Session, sel string) {
	s.t.Helper()
	find := `(() => { const e = document.querySelector(` + strconv.Quote(sel) + `); if (!e || !e.checkVisibility()) return [];
  e.scrollIntoView({block: "center", behavior: "instant"}); const r = e.getBoundingClientRect(); return [r.x + r.width / 2, r.y + r.height / 2]; })()`
	s.Until(sel, `(`+find+`).length === 2`)
	at := Eval[[]float64](s, find)
	s.Do("press "+sel, chromedp.MouseClickXY(at[0], at[1]))
}

// inDialog is the button labelled label in the open dialog.
func inDialog(label string) string {
	return `[...document.querySelectorAll("dialog[open] button")].find((b) => b.textContent.trim() === ` + strconv.Quote(label) + `)`
}

// pressInDialog clicks the open dialog's button labelled label.
func pressInDialog(s *Session, label string) {
	s.t.Helper()
	s.Until("the dialog's "+label, inDialog(label)+` !== undefined`)
	s.Do("press "+label, chromedp.Evaluate[chromedp.Void](inDialog(label)+`.click()`))
}

func checkStep(id int64) string { return DocPath(fixture.Claim, id) + "/edit/check" }

// claimDraft is Deepak's draft claim with one ₹1,250.00 line.
func claimDraft(h *Harness) doc.Result {
	h.t.Helper()
	var employee int64
	for _, e := range h.Fx.Employees {
		if e.Code == "EMP-007" {
			employee = e.ID
		}
	}
	return h.Do(deepak, fixture.Claim, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"employee_id": strconv.FormatInt(employee, 10), "claim_date": plainDate},
		Lines:  []map[string]string{{"expense_type": "TRAVEL", "amount": "1250.00"}},
	})
}

// approvedClaim is Deepak's claim, submitted and approved by Priya: EX/26-27/00001.
func approvedClaim(h *Harness) int64 {
	h.t.Helper()
	r := claimDraft(h)
	s := h.Do(deepak, fixture.Claim, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version})
	h.Do(priya, fixture.Claim, r.ID, doctype.ActionApprove, doc.ActionInput{Version: s.Version})
	if got := docState(h, "fixture_claim", r.ID); got != "approved" {
		h.t.Fatalf("claim state = %q, want approved", got)
	}
	return r.ID
}

// attachFile attaches data as name to claim id through FILE, as email.
func attachFile(h *Harness, email string, id int64, name string, data []byte) int64 {
	h.t.Helper()
	st := h.App.Files()
	sp, err := st.Spool(bytes.NewReader(data), files.MaxFileBytes)
	if err != nil {
		h.t.Fatal(err)
	}
	defer func() { _ = sp.Cleanup() }()
	p := h.Principal(email)
	ctx := auth.WithPrincipal(context.Background(), p)
	var fid int64
	if err := h.App.DB().Write(ctx, func(tx *db.Tx) error {
		fid, err = st.Attach(ctx, tx, p, auth.RecordRef{DocType: fixture.Claim, ID: id}, name, sp)
		return err
	}); err != nil {
		h.t.Fatalf("attach %s: %v", name, err)
	}
	return fid
}

// shadedPhoto is a real w × h JPEG; shade makes each one's bytes different.
func shadedPhoto(t *testing.T, w, h int, shade uint8) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			img.Set(x, y, color.RGBA{R: shade, G: uint8(x), B: uint8(y), A: 255}) //nolint:gosec // test images are small
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fileOf writes data to a temp file called name.
func fileOf(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func gif() []byte { return []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00;") }

// auditActions is the claim's audit actions, oldest first, read through audit.
func auditActions(h *Harness, id int64) []audit.Entry {
	h.t.Helper()
	var es []audit.Entry
	if err := h.App.DB().Read(context.Background(), func(tx *db.Tx) error {
		var err error
		es, err = audit.ForDoc(context.Background(), tx, fixture.Claim, id)
		return err
	}); err != nil {
		h.t.Fatal(err)
	}
	return es
}

// posts counts the server's POSTs to path so far.
func posts(h *Harness, path string) int {
	h.logs.mu.Lock()
	defer h.logs.mu.Unlock()
	return strings.Count(h.logs.b.String(), `"method":"POST","path":"`+path+`"`)
}

func claimVersion(h *Harness, id int64) int {
	h.t.Helper()
	return Scalar[int](h, `SELECT version FROM fixture_claim WHERE id = ?`, id)
}

func attachCount(h *Harness, id int64) int {
	h.t.Helper()
	return Scalar[int](h, `SELECT count(*) FROM attachment WHERE parent_doctype = ? AND parent_id = ?`, fixture.Claim, id)
}

// TestCheckStep_UploadThenSubmit: ATT-011. Attaching doesn't bump the
// version, so the reloaded check step submits without a conflict.
func TestCheckStep_UploadThenSubmit(t *testing.T) {
	h := Start(t)
	id := claimDraft(h).ID
	d := h.SignIn(deepak, phone360)
	d.Open(checkStep(id))
	version := claimVersion(h, id)
	if got := Eval[string](d, `document.getElementById("doc-version").value`); got != strconv.Itoa(version) {
		t.Fatalf("the check step posts version %s, want %d", got, version)
	}

	d.SetFile(takePhoto, fileOf(t, "receipt.jpg", shadedPhoto(t, 8, 8, 1)))
	d.Until("the receipt's thumbnail", thumbShown("receipt.jpg"))
	if got := claimVersion(h, id); got != version {
		t.Errorf("version after the upload = %d, want %d", got, version)
	}
	d.Press(sendApproval)
	d.WaitText("Pending approval")
	if d.Shows("Someone else changed") {
		t.Error("the submit after the upload was refused as stale")
	}
	if got := docState(h, "fixture_claim", id); got != "pending_approval" {
		t.Errorf("state = %q, want pending_approval", got)
	}
	var seq []string
	for _, e := range auditActions(h, id) {
		seq = append(seq, e.Action)
	}
	if i, j := slices.Index(seq, "attachment_added"), slices.Index(seq, "submit"); i < 0 || j < 0 || i > j {
		t.Errorf("audit = %q, want attachment_added before submit", seq)
	}
}

// TestDocPage_AddAfterApproval: ATT-012. A file can be added to an approved
// claim; nothing else changes and nothing can be removed.
func TestDocPage_AddAfterApproval(t *testing.T) {
	h := Start(t)
	id := approvedClaim(h)
	before := Scalar[string](h, `SELECT state || number || claim_date || total || version FROM fixture_claim WHERE id = ?`, id)
	d := h.SignIn(deepak, phone390)
	d.Open(DocPath(fixture.Claim, id))
	d.WaitText(firstClaim)
	d.SetFile(chooseFile, fileOf(t, "bill.pdf", filestest.PDF(2048)))
	d.Until("bill.pdf listed", listed+`.includes("bill.pdf")`)
	if got := Scalar[string](h, `SELECT state || number || claim_date || total || version FROM fixture_claim WHERE id = ?`, id); got != before {
		t.Errorf("the claim changed with the upload: %q, was %q", got, before)
	}
	if n := Eval[int](d, removeButtons); n != 0 {
		t.Errorf("%d Remove buttons on an approved claim, want none", n)
	}

	fid := Scalar[int64](h, `SELECT id FROM attachment WHERE parent_doctype = ? AND parent_id = ?`, fixture.Claim, id)
	got := Eval[[]string](d, `(async () => { const r = await fetch("/files/`+strconv.FormatInt(fid, 10)+`/remove", {method: "POST",
  body: new URLSearchParams({csrf_token: document.querySelector("input[name=csrf_token]").value})}); return [String(r.status), (await r.text()).trim()]; })()`, chromedp.EvalAwaitPromise)
	if want := []string{"422", msgSubmitted}; !slices.Equal(got, want) {
		t.Errorf("crafted remove = %q, want %q", got, want)
	}
	if n := attachCount(h, id); n != 1 {
		t.Errorf("attachments after the crafted remove = %d, want 1", n)
	}
}

// TestE2E_AttachmentUploadingState: ATT-014. While the upload is held both
// inputs are disabled and Uploading… shows; the 422 answer clears it and
// returns focus to the input used.
func TestE2E_AttachmentUploadingState(t *testing.T) {
	h := Start(t)
	id := claimDraft(h).ID
	d := h.SignIn(deepak, phone360)
	d.Open(checkStep(id))
	if got := Eval[string](d, upStatus+`.getAttribute("role") + "|" + `+upStatus+`.textContent`); got != "status|" {
		t.Errorf("status line before the upload = %q, want an empty role=status", got)
	}
	h.DelayPosts("/attachments/", 2*time.Second)
	d.SetFile(takePhoto, fileOf(t, "receipt.gif", gif()))
	d.Until("busy while the upload is held", upBusy)
	d.Until("idle after the 422", upIdle)
	h.DelayPosts("/attachments/", 0)
	d.Until("focus back on Take photo", focusOn(takePhoto))
	d.Until("the type refusal", upShows(msgGIF))
}

// TestE2E_AttachmentRefused: ATT-015. One byte over 10 MB is refused while
// the body is read: 413's text by the control and in #alerts, focus back on
// Choose file, both inputs marked, no reload; the next file clears it. 11 MB
// is refused before the body is read, which the browser may see as a drop.
func TestE2E_AttachmentRefused(t *testing.T) {
	h := Start(t)
	id := claimDraft(h).ID
	d := h.SignIn(deepak, phone360)
	d.Open(checkStep(id))
	d.Do("mark the page", chromedp.Evaluate[chromedp.Void](markPage))
	before := Eval[int](d, alerts)

	d.SetFile(chooseFile, fileOf(t, "big.jpg", filestest.JPEG(int(files.MaxFileBytes)+1)))
	d.Until("the 413 refusal by the control", upShows(msgTooLarge))
	d.Until("idle after the refusal", upIdle)
	d.Until("focus back on Choose file", focusOn(chooseFile))
	d.Until("the refusal in #alerts", alertsHold(msgTooLarge))
	if n := Eval[int](d, alerts); n != before+1 {
		t.Errorf("#alerts gained %d notices, want 1", n-before)
	}
	if n := Eval[int](d, upInvalid); n != 2 {
		t.Errorf("%d inputs have aria-invalid=true, want 2: %s", n, Eval[string](d, upState))
	}
	if !Eval[bool](d, samePage) {
		t.Error("the page reloaded on a refusal")
	}
	if n := attachCount(h, id); n != 0 {
		t.Errorf("attachments = %d, want 0", n)
	}

	// The next file clears the message and the marks when it starts.
	h.DelayPosts("/attachments/", time.Second)
	d.SetFile(chooseFile, fileOf(t, "receipt.jpg", shadedPhoto(t, 8, 8, 2)))
	d.Until("busy with the message cleared", upBusy+` && `+upMsg+`.textContent.trim() === "" && `+upMarked+` === 0`)
	d.Until("the receipt's thumbnail", thumbShown("receipt.jpg"))
	h.DelayPosts("/attachments/", 0)

	d.Do("mark the page", chromedp.Evaluate[chromedp.Void](markPage))
	const elevenMB = 11 << 20
	d.SetFile(chooseFile, fileOf(t, "huge.jpg", filestest.JPEG(elevenMB)))
	d.Until("a refusal by the control", upShows(msgTooLarge)+` || `+upShows(msgDropped))
	d.Until("idle after the refusal", upIdle)
	d.Until("focus back on Choose file", focusOn(chooseFile))
	if !Eval[bool](d, samePage) {
		t.Error("the page reloaded on the 11 MB refusal")
	}
	if n := attachCount(h, id); n != 1 {
		t.Errorf("attachments = %d, want 1", n)
	}
}

// TestE2E_AttachmentRemove: ATT-016.
func TestE2E_AttachmentRemove(t *testing.T) {
	h := Start(t)
	id := claimDraft(h).ID
	fid := attachFile(h, deepak, id, "receipt.jpg", shadedPhoto(t, 8, 8, 3))
	removePath := "/files/" + strconv.FormatInt(fid, 10) + "/remove"
	dialog := `document.getElementById("remove-file-` + strconv.FormatInt(fid, 10) + `")`
	d := h.SignIn(deepak, phone390)
	d.Open(checkStep(id))
	d.Until("the receipt's thumbnail", thumbShown("receipt.jpg"))

	pressSel(d, removeOf("receipt.jpg"))
	d.Until("the confirm opens", dialog+`.open`)
	if got := Eval[string](d, dialog+`.querySelector("h2").textContent.trim()`); got != removeConfirm {
		t.Errorf("confirm heading = %q", got)
	}
	pressInDialog(d, "Keep file")
	d.Until("Keep file closes the confirm", `!`+dialog+`.open`)
	d.Until("focus back on Remove", focusOn(removeOf("receipt.jpg")))
	if n := posts(h, removePath); n != 0 {
		t.Errorf("Remove or Keep file posted %d times", n)
	}

	pressSel(d, removeOf("receipt.jpg"))
	d.Until("the confirm opens again", dialog+`.open`)
	pressInDialog(d, "Remove file")
	d.WaitText("No files attached yet.")
	if n := attachCount(h, id); n != 0 {
		t.Errorf("attachments = %d, want 0", n)
	}
	found := false
	for _, e := range auditActions(h, id) {
		if e.Action == "attachment_removed" && len(e.Changes) > 0 && e.Changes[0].Field == "file_name" && e.Changes[0].Before == "receipt.jpg" {
			found = true
		}
	}
	if !found {
		t.Errorf("no attachment_removed entry for receipt.jpg in %+v", auditActions(h, id))
	}
}

// TestE2E_AttachmentRemoveStale: ATT-017 and ATT-027. The draft is submitted
// from elsewhere while its check step is open: Remove file is refused, the
// confirm closes, focus is on Remove and the inputs aren't marked.
func TestE2E_AttachmentRemoveStale(t *testing.T) {
	h := Start(t)
	r := claimDraft(h)
	fid := attachFile(h, deepak, r.ID, "receipt.jpg", shadedPhoto(t, 8, 8, 4))
	dialog := `document.getElementById("remove-file-` + strconv.FormatInt(fid, 10) + `")`
	d := h.SignIn(deepak, phone390)
	d.Open(checkStep(r.ID))
	d.Until("the receipt's thumbnail", thumbShown("receipt.jpg"))
	h.Do(deepak, fixture.Claim, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: claimVersion(h, r.ID)})

	pressSel(d, removeOf("receipt.jpg"))
	d.Until("the confirm opens", dialog+`.open`)
	pressInDialog(d, "Remove file")
	d.Until("the refused remove closes the confirm", `!`+dialog+`.open`)
	d.Until("the refusal by the control", upShows(msgSubmitted))
	d.Until("the refusal in #alerts", alertsHold(msgSubmitted))
	d.Until("focus back on Remove", focusOn(removeOf("receipt.jpg")))
	if n := Eval[int](d, upMarked); n != 0 {
		t.Errorf("%d file inputs have aria-invalid after a refused remove, want none", n)
	}
	if n := attachCount(h, r.ID); n != 1 {
		t.Errorf("attachments = %d, want 1", n)
	}
}

// claimWithFiles is an approved claim with three files.
func claimWithFiles(t *testing.T, h *Harness) int64 {
	t.Helper()
	id := approvedClaim(h)
	attachFile(h, deepak, id, "receipt.jpg", shadedPhoto(t, 8, 8, 5))
	attachFile(h, deepak, id, "challan.heic", filestest.HEIC("heic"))
	attachFile(h, deepak, id, "quote-shree-steel.pdf", filestest.PDF(4096))
	return id
}

// TestE2E_AttachmentsSidePanel: ATT-021. At 1440 px the panel is in the
// right column, under the state badge and above Approval history.
func TestE2E_AttachmentsSidePanel(t *testing.T) {
	h := Start(t)
	id := claimWithFiles(t, h)
	d := h.SignIn(deepak, desktop)
	d.Open(DocPath(fixture.Claim, id))
	d.Until("the receipt's thumbnail", thumbShown("receipt.jpg"))
	got := Eval[string](d, `(() => {
  const card = `+attachCard+`, side = document.querySelector(".doc-side");
  if (!side || !side.contains(card)) return "the panel is not in .doc-side";
  const main = side.previousElementSibling.getBoundingClientRect(), s = side.getBoundingClientRect();
  if (s.left < main.right) return "the side column is not right of the content: " + s.left + " < " + main.right;
  const badge = side.querySelector(".doc-state").getBoundingClientRect(), c = card.getBoundingClientRect();
  const hist = [...side.querySelectorAll("summary")].find((e) => e.textContent.trim().startsWith("Approval history"));
  if (!hist) return "no Approval history in the side column";
  const a = hist.getBoundingClientRect();
  if (badge.bottom > c.top) return "the panel is above the state badge";
  if (c.bottom > a.top) return "the panel is below Approval history";
  return "";
})()`)
	if got != "" {
		t.Error(got)
	}
}

// TestE2E_AttachmentAfterApproval: ATT-025, GRN-028's flow on the fixture.
// A late file on an approved claim at 360 px, under the design's 30 s.
func TestE2E_AttachmentAfterApproval(t *testing.T) {
	h := Start(t)
	id := approvedClaim(h)
	attachFile(h, deepak, id, "receipt.jpg", shadedPhoto(t, 8, 8, 6))
	d := h.SignIn(deepak, phone360)
	const goal = 30 * time.Second
	start := time.Now()
	d.Open(DocPath(fixture.Claim, id))
	d.SetFile(chooseFile, fileOf(t, "bill.pdf", filestest.PDF(3000)))
	d.Until("bill.pdf listed first", listed+`[0] === "bill.pdf"`)
	if took := time.Since(start); took >= goal {
		t.Errorf("opening to listed took %v, want under %v", took, goal)
	}
	if got := stateBadge(d, ".doc-state .badge"); got != "Approved" {
		t.Errorf("state badge = %q, want Approved", got)
	}
	if n := Eval[int](d, removeButtons); n != 0 {
		t.Errorf("%d Remove buttons, want none", n)
	}
}

// TestE2E_AttachmentRefused422ClearsBusy: ATT-026. A .gif's 422, which htmx
// treats as a success here, still clears the busy state.
func TestE2E_AttachmentRefused422ClearsBusy(t *testing.T) {
	h := Start(t)
	id := claimDraft(h).ID
	d := h.SignIn(deepak, phone360)
	d.Open(checkStep(id))
	d.SetFile(chooseFile, fileOf(t, "receipt.gif", gif()))
	d.Until("the type refusal by the control", upShows(msgGIF))
	d.Until("idle after the 422", upIdle)
	d.Until("focus back on Choose file", focusOn(chooseFile))
}

// offline turns the browser's network off or on (network.EmulateNetworkConditions,
// now ByRule; an empty pattern matches every request).
func offline(s *Session, off bool) {
	s.t.Helper()
	s.Do("network offline "+strconv.FormatBool(off), chromedp.Func(func(ctx context.Context, _ *chromedp.Target) error {
		if _, err := chromedp.Call(ctx, network.Enable, network.EnableParams{}); err != nil {
			return err
		}
		_, err := chromedp.Call(ctx, network.EmulateNetworkConditionsByRule, network.EmulateNetworkConditionsByRuleParams{
			MatchedNetworkConditions: []*network.Conditions{{Offline: off, DownloadThroughput: -1, UploadThroughput: -1}},
		})
		return err
	}))
}

// TestE2E_AttachmentNetworkDrop: ATT-029.
func TestE2E_AttachmentNetworkDrop(t *testing.T) {
	h := Start(t)
	id := claimDraft(h).ID
	d := h.SignIn(deepak, phone360)
	d.Open(checkStep(id))
	before := Eval[int](d, alerts)
	file := fileOf(t, "receipt.jpg", shadedPhoto(t, 8, 8, 7))
	offline(d, true)
	d.SetFile(takePhoto, file)
	d.Until("the dropped-connection text", upShows(msgDropped))
	d.Until("idle after the drop", upIdle)
	d.Until("focus back on Take photo", focusOn(takePhoto))
	if n := Eval[int](d, alerts); n != before {
		t.Errorf("#alerts gained %d notices on a dropped connection, want none", n-before)
	}
	if n := Eval[int](d, `(`+listed+`).length`); n != 0 {
		t.Errorf("the list changed: %d files", n)
	}
	offline(d, false)
	d.SetFile(takePhoto, file) // the same file again: Chrome sends no change event for an unchanged pick
	d.Until("the receipt's thumbnail once back online", thumbShown("receipt.jpg"))
}

// manyFiles fills claim id with 20 files (the count limit): a 120-character
// name and a Devanagari one among them.
func manyFiles(t *testing.T, h *Harness, id int64) {
	t.Helper()
	attachFile(h, deepak, id, strings.Repeat("quotation-shree-steel-", 6)[:116]+".pdf", filestest.PDF(1000))
	attachFile(h, deepak, id, "बिल रसीद.jpg", shadedPhoto(t, 8, 8, 8))
	for i := range 18 {
		attachFile(h, deepak, id, fmt.Sprintf("receipt-%02d.jpg", i), shadedPhoto(t, 9+i, 8, 20)) // each size is its own bytes
	}
}

// refusedLong shows uitest.LongError as a refusal, as a server notice does.
const refusedLong = `document.querySelector("#attachments .upload__controls form").dispatchEvent(new CustomEvent("notice", {bubbles: true, detail: {level: "error", text: %s}}))`

// TestE2E_AttachmentsOverflow: ATT-018. No sideways scroll on the check step
// and the approved page with 20 files, the refused state and the open
// confirm, at every sweep width.
func TestE2E_AttachmentsOverflow(t *testing.T) {
	h := Start(t)
	draftID := claimDraft(h).ID
	manyFiles(t, h, draftID)
	approved := approvedClaim(h)
	manyFiles(t, h, approved)
	d := h.SignIn(deepak, desktop)
	firstRemove := `document.querySelector('button[data-open-dialog^="remove-file-"]').click()`
	for _, c := range []struct {
		name, path, setup string
	}{
		{"check step", checkStep(draftID), ""},
		{"approved page", DocPath(fixture.Claim, approved), ""},
		{"refused", checkStep(draftID), fmt.Sprintf(refusedLong, strconv.Quote(uitest.LongError))},
		{"confirm open", checkStep(draftID), firstRemove},
	} {
		for _, w := range screens.Widths {
			if err := uibrowser.At(d.ctx, w, h.URL+c.path); err != nil {
				t.Fatal(err)
			}
			d.Until("the page to load", `document.readyState === "complete"`)
			if c.setup != "" {
				d.Do(c.name, chromedp.Evaluate[chromedp.Void](c.setup))
			}
			if c.name == "refused" {
				d.Until("the long refusal", upMsg+`.textContent.trim() !== ""`)
			}
			if c.name == "confirm open" {
				d.Until("the confirm", `document.querySelector("dialog[open]") !== null`)
			}
			bad, err := uibrowser.Overflow(d.ctx)
			if err != nil {
				t.Fatal(err)
			}
			if len(bad) > 0 {
				t.Errorf("%s at %d px: %v", c.name, w, bad)
			}
		}
	}
}

// TestE2E_AttachmentsTargets: ATT-019. At 360 px every control of the panel
// and the confirm is at least 44 × 44 px, with 8 px between the two upload
// buttons.
func TestE2E_AttachmentsTargets(t *testing.T) {
	h := Start(t)
	id := claimDraft(h).ID
	fid := attachFile(h, deepak, id, "receipt.jpg", shadedPhoto(t, 8, 8, 9))
	d := h.SignIn(deepak, phone360)
	d.Open(checkStep(id))
	d.Until("the receipt's thumbnail", thumbShown("receipt.jpg"))
	pressSel(d, removeOf("receipt.jpg"))
	d.Until("the confirm opens", `document.getElementById("remove-file-`+strconv.FormatInt(fid, 10)+`").open`)
	got := Eval[[]string](d, `(() => {
  const out = [], card = `+attachCard+`, dlg = document.querySelector("dialog[open]");
  const btn = (root, label) => [...root.querySelectorAll("button, a")].find((b) => b.textContent.trim() === label || b.getAttribute("aria-label") === label);
  const labels = [...card.querySelectorAll(".upload__button")];
  const items = {"Take photo": labels[0], "Choose file": labels[1], "file link": card.querySelector(".attachments__thumb"),
    "Remove": card.querySelector('`+removeOf("receipt.jpg")+`'), "Remove file": btn(dlg, "Remove file"), "Keep file": btn(dlg, "Keep file"),
    "Close": [...dlg.querySelectorAll("button")].find((b) => !["Remove file", "Keep file"].includes(b.textContent.trim()))};
  for (const [k, e] of Object.entries(items)) {
    if (!e) { out.push(k + " missing"); continue; }
    const r = e.getBoundingClientRect();
    if (r.width < 44 || r.height < 44) out.push(k + " " + Math.round(r.width) + "x" + Math.round(r.height));
  }
  if (labels.length === 2) {
    const a = labels[0].getBoundingClientRect(), b = labels[1].getBoundingClientRect();
    const gap = Math.max(b.left - a.right, b.top - a.bottom);
    if (gap < 8) out.push("upload buttons " + gap + " px apart");
  }
  return out;
})()`)
	if len(got) > 0 {
		t.Errorf("too small or too close at 360 px: %v", got)
	}
}

// TestE2E_AttachmentsAxe: ATT-020. axe finds no WCAG 2.2 AA violation on the
// check step and the document page with files, the refused state or the
// open confirm.
func TestE2E_AttachmentsAxe(t *testing.T) {
	h := Start(t)
	draftID := claimDraft(h).ID
	attachFile(h, deepak, draftID, "receipt.jpg", shadedPhoto(t, 8, 8, 10))
	attachFile(h, deepak, draftID, "quote.pdf", filestest.PDF(1000))
	approved := claimWithFiles(t, h)
	d := h.SignIn(deepak, phone360)
	gifFile := fileOf(t, "receipt.gif", gif())
	for _, c := range []struct {
		name, path, setup, upload, ready string
	}{
		{"check step", checkStep(draftID), "", "", thumbShown("receipt.jpg")},
		{"document page", DocPath(fixture.Claim, approved), "", "", thumbShown("receipt.jpg")},
		{"refused", checkStep(draftID), "", gifFile, upShows(msgGIF) + " && " + upIdle},
		{"confirm open", checkStep(draftID), `document.querySelector('button[data-open-dialog^="remove-file-"]').click()`, "", `document.querySelector("dialog[open]") !== null`},
	} {
		d.Open(c.path)
		d.Until("the page to load", `document.readyState === "complete"`)
		if c.setup != "" {
			d.Do(c.name, chromedp.Evaluate[chromedp.Void](c.setup))
		}
		if c.upload != "" {
			d.SetFile(chooseFile, c.upload) // a real refusal from the server (ATT-026)
		}
		// A dialog fading in fails color-contrast mid-way: wait for its
		// transitions (daisyUI's scroll-driven animation never ends).
		d.Until(c.name, c.ready+` && !document.getAnimations().some((a) => a instanceof CSSTransition)`)
		ctx, cancel := context.WithTimeout(d.ctx, uibrowser.AxeTimeout)
		v, err := uibrowser.Axe(ctx)
		cancel()
		if err != nil {
			t.Fatalf("%s: axe: %v", c.name, err)
		}
		for _, x := range v {
			t.Errorf("%s: %s (%s): %v", c.name, x.ID, x.Help, x.Nodes)
		}
	}
}

// TestE2E_CompanyLogoRemoveConfirms: ATT-028. The logo's Remove opens the
// confirm with no request; Keep file keeps the logo; Remove file posts the
// logo page's remove and the upload area is redrawn without it.
func TestE2E_CompanyLogoRemoveConfirms(t *testing.T) {
	h := Start(t)
	const logoPage = "/m/company/1/logo"
	a := h.SignIn(anil, desktop)
	a.Open(logoPage)
	a.SetFile("#company-logo-file", fileOf(t, "logo.png", filestest.PNG(1024)))
	a.Until("the logo's Remove", `document.querySelector('`+removeOf("logo.png")+`') !== null`)

	pressSel(a, removeOf("logo.png"))
	a.Until("the confirm opens", `document.querySelector("dialog[open]")?.querySelector("h2")?.textContent.trim() === `+strconv.Quote(removeConfirm))
	if n := posts(h, logoPage+"/remove"); n != 0 {
		t.Errorf("Remove posted %d times before the confirm", n)
	}
	pressInDialog(a, "Keep file")
	a.Until("Keep file closes the confirm", `document.querySelector("dialog[open]") === null`)
	if n := posts(h, logoPage+"/remove"); n != 0 {
		t.Errorf("Keep file posted %d times", n)
	}
	if !Eval[bool](a, `document.querySelector('`+removeOf("logo.png")+`') !== null`) {
		t.Fatal("the logo went away after Keep file")
	}

	pressSel(a, removeOf("logo.png"))
	a.Until("the confirm opens again", `document.querySelector("dialog[open]") !== null`)
	pressInDialog(a, "Remove file")
	a.Until("the area redrawn without the logo", `document.querySelector("#company-logo-area") !== null && document.querySelector('`+removeOf("logo.png")+`') === null`)
	if n := posts(h, logoPage+"/remove"); n != 1 {
		t.Errorf("the remove posted %d times, want 1", n)
	}
}
