# UX design: Attachments on document pages

Date: 31-10-2026 · Status: in review (key points approved by the owner on 31-10-2026, CD-243; the rest needs the crew-reviewer check) · Execution protocol §9 (CD-97)

Sources: CD-241, CD-243, CD-133, CD-209 (`docs/specs/contract-changes.md`); D-FILE-02, -03, -04, -06, -07, -08 and FILE-001…046 (`docs/tests/core/05-files-print.md`); design-system §4, §6, §7, §8.3, §8.4, §9; GRN spec §3 (late photo), §11, GRN-028; Expense spec §3.3; hooks spec §4.2, §4.4. Code read at 34aed80: `internal/kernel/files/web/{routes,upload,remove,respond}.go`, `internal/kernel/files/{errors,attach,remove,read,policy,meta}.go`, `internal/ui/components/{upload,attachments,dialog,empty,card,form}.templ`, `internal/ui/js/{upload,toast,busy,dialog}.js`, `internal/ui/docscreen/{page.templ,page.go,check.templ,form.go,view.go,actions.go}`, `internal/ui/wizard/frame.templ`, `internal/ui/layout/page.templ`, `internal/app/uiadapters.go`, `internal/fixture/claim.go`.

**Fixed by the owner (CD-243), kept exactly here:**

1. The Attachments section is on the document page in every state except cancelled, and in the wizard's review step (Check and submit).
2. On a phone it has two buttons, `Take photo` (camera) and `Choose file`.
3. Remove follows D-FILE-06: write right, `draft` and `rejected` only. After submit, files can be added but not removed.
4. Each DocType opts in with a flag. PO and GRN do. `expense.Claim` doesn't (it keeps per-line receipts on its module route). The fixture DocType `fixture.Claim` does.

Everything else below is design detail within those points.

## 1. Goal

Today no document page can add a file. Once this ships, anyone with `write` on a document can attach a photo or file from the document page (draft, rejected, pending approval, approved, closed) or from the check step, with the phone camera in one tap. They can remove it again only while the document is a draft or rejected. Everyone who can read the document sees the files, and can open them. We will know it works when:

- Deepak, on a 360 px phone, goes from tapping `Take photo` on his claim's check step to seeing the thumbnail in the list in **under 15 seconds**, camera time excluded, on the office Wi-Fi (`TestE2E_ExpenseFromPhoneWithReceipt` times it).
- Suresh adds a late bill photo to an approved GRN in **under 30 seconds** from opening the document (GRN-028's flow), with no other screen involved.
- No support question of the form "where do I attach the quote?" after the first month (owner check, not a test).

## 2. Who and where

| Person (from `docs/tests/core/00-fixtures.md`) | Job in this feature | Device and setting | How often |
|---|---|---|---|
| Suresh Patil (Stores) | Photographs the challan on the GRN check step; adds the vendor bill photo to the approved GRN when it arrives later | 360 px phone, at the gate, in gloves, truck waiting, mobile data that drops | Several times a day |
| Deepak Nair (Employee) | Attaches receipt photos to his claim on `fixture.Claim` (the stand-in for an expense-style document in the e2e tests) | 360 px phone, one hand, often on mobile data | Weekly |
| Priya Shah (Purchase User; Deepak's manager) | Attaches the vendor quote (PDF or XLSX) to a PO on desktop; as Deepak's approver she reads his receipts on her phone but can't add (no `write` on his claim) | 1440 px desktop for POs; 390 px phone for approvals | Daily |
| Ravi Kulkarni, Meena Iyer (approvers) | Open the attached files before approving | Phone first (390 px), sometimes 1440 px | Daily |

The hardest row is Suresh: one gloved thumb, a weak network, a 3–5 MB photo. Every decision below is checked against him first.

## 3. Jobs and flow

**Jobs**

- When I have the vendor's challan in my hand at the gate, I want to photograph it onto the receipt before I submit, so the office can type the bill details from it later.
- When a bill arrives after the receipt was approved, I want to add its photo to the approved document, so the record is complete without cancelling anything.
- When I submit a claim, I want my receipt photos on it, so my manager can check them before approving.
- When I approve, I want to open each attached file from the document, so I can check it without asking.
- When I attached the wrong file to a draft, I want to remove it, so the wrong evidence isn't submitted.

**Main flow: add a photo on the check step (draft)**

1. Deepak opens his draft from "My requests" or the list; a draft's document URL lands on its check step (`/doc/fixture.Claim/42/edit/check`, design-system §8.3).
2. He scrolls past the step summaries to the **Attachments** card. It says `No files attached yet.` and shows `Add a file` with the hint and the two buttons.
3. He taps **Take photo**. The phone opens the rear camera (`capture="environment"`). He takes the picture and confirms.
4. The upload starts at once (one file per request, CD-133). Both buttons are disabled, the progress bar fills and `Uploading…` shows under it.
5. The server attaches the file and answers `HX-Refresh: true`; the browser reloads the same URL. Deepak sees the check step again with `receipt.jpg` first in the list (newest first, FILE-030) as a thumbnail with `2.3 MB` and `Deepak Nair · 24-10-2026 10:30`.
6. He taps **Submit for approval** in the action bar as usual.

**Main flow: add a file to a submitted document**

1. Suresh opens approved `GR/26-27/00001` from the GRN list.
2. The **Attachments** card is below the lines on the phone (right column from 1280 px). He taps **Take photo** or **Choose file**; steps 4–5 above follow. The document's state, fields and `version` don't change (FILE-040); the History disclosure gains `attachment_added`.

**Main flow: remove a file (draft or rejected)**

1. Under each file is a **Remove** button (only for a user with `write`, only in `draft` and `rejected`).
2. Tapping it opens a short confirm dialog: `Remove this file?` / `receipt.jpg will be removed from this document. You can attach it again later.` with **Remove file** (danger) and **Keep file**.
3. **Remove file** posts; the server answers `HX-Refresh: true`; the page reloads without the file. **Keep file**, the close button, Escape or a tap on the backdrop close the dialog with no request, and focus goes back to the Remove button.

**Opening a file.** Tapping a thumbnail or a file chip opens `/files/{id}` in the same tab: JPG, PNG, WEBP and PDF open in the browser (full screen on a phone, design-system §8.4); HEIC, XLSX, DOCX and CSV download (FILE-032). The browser's Back returns to the page.

**Alternative and failure paths**

| Case | What the user sees |
|---|---|
| No `write` on the document (Priya on Deepak's pending claim; Meena) | The Attachments card with the list, or `No files attached yet.`; no `Add a file`, no Remove |
| DocType without the flag (`expense.Claim`, `fixture.Order`, `fixture.Receipt`) | Unchanged from today: a read-only Attachments card on the document page only when the document has files; nothing on the check step |
| Cancelled document (flagged DocType) | No Attachments section (CD-243 point 1). Note: a flagged DocType therefore loses today's read-only list on a cancelled document (`page.templ:88-92` shows it in any state); unflagged DocTypes keep it |
| A refused upload or remove (too big, wrong type, limit, duplicate, no right, state changed) | The FILE message, unchanged, under `Add a file` and in the alerts region at the top; the controls are enabled again; nothing else on the page changes (§5.1 Copy) |
| State changed underneath (the draft was submitted in another tab, then Remove is tapped) | `Files can't be removed after the document is submitted. Cancel and amend it to change files.` The page isn't reloaded; reloading shows the current state without Remove |
| Document cancelled underneath, then a file is added | `This document is cancelled. Attach files to its amended copy instead.` |
| Stale version (409) | Can't happen: attachments don't post or change the document's `version` (FILE-040), so a later Submit on the check step doesn't conflict (ATT-011) |
| Server busy (503) | `The system is busy. Please try again in a moment.` in the same two places |
| Session expired | 401 with `HX-Redirect` to sign in (FILE-027, unchanged) |
| Storage failure (500) | `Couldn't save the file. Try again or contact your administrator.` |
| Network drops mid-upload | htmx fires `htmx:sendError`; the progress bar and `Uploading…` hide, the controls are enabled again, focus returns to the input used, and `#attachments-msg` shows `The upload didn't finish. Check your connection and try again.` (§6 change 1 and 2). The same text is the fallback when a browser can't read an early 413 (see the 413 row in §5.1) |
| Typed but unsaved input elsewhere on the page | The check step has none: its form holds only the hidden `version` and `return` (wizard `Frame`, `After` sits outside the form), so the reload loses nothing. The document page has no inputs except a module's own cards: the GRN `Bill details` form (GRN spec §11), whose behaviour around a reload belongs to the GRN design |

## 4. Screen inventory

| Screen | URL | Pattern (design-system §8) | States covered |
|---|---|---|---|
| Check step of a draft (and of an amended draft) | `/doc/{doctype}/{id}/edit/check` | §8.3 Check and submit; Attachments card added | normal (files), empty, uploading, refused, read-only (no `write`), many rows (20 files), longest values, flag off |
| Document page | `/doc/{doctype}/{id}` | §8.4 Document page (any state) | per state: rejected (add + Remove), pending approval, approved, closed (add only), cancelled (no section); empty; read-only; refused; flag off; 20 files; longest values |
| Remove confirm | (dialog on either page above) | §7 `Dialog`, short confirm (§8.5 rule: destructive actions confirm) | open, removing, refused |
| File view | `/files/{id}` | Browser's own viewer or download (FILE-031, FILE-032) | not ours; 404 `File not found.`, missing blob message (FILE-036) |

Loading: the upload's progress bar and `Uploading…` (§6 change 1). 422 with `ErrorSummary`: not used here, because an upload isn't a form step; the refusal shows by the control and in the alerts region instead (§5.1). Conflict (409): n/a, see §3. 500 lines: the panel doesn't change with lines; at most 20 files (D-FILE-02).

## 5. Screens

### 5.1 The Attachments card (shared by both pages)

The same card is used on the check step and the document page, so the same job looks the same everywhere (§4.11). It is built only from existing components: `Card`, `EmptyState`, `FileUpload` (which renders `Attachments` for its files), `Attachments` alone for read-only, `Dialog`, `Button`, `Form`.

**What the card holds, by case** (flagged DocType; `canAdd` = the user's `write` on this record, from `docscreen.Rights`, and state ∈ {draft, rejected, pending_approval, approved, closed}; `canRemove` = `write` and state ∈ {draft, rejected}):

| Case | Card content, top to bottom |
|---|---|
| `canAdd`, no files | `EmptyState("No files attached yet.", nil)`, then `FileUpload` |
| `canAdd`, files | `FileUpload` with `Files` (the list sits under the controls); each file has Remove only when `canRemove` |
| not `canAdd`, no files | `EmptyState("No files attached yet.", nil)` |
| not `canAdd`, files | `Attachments(files)`, no Remove |
| state `cancelled` | no card |

Remove is never shown without `canAdd`, because `canRemove` implies it.

**Phone layout (360 px), top to bottom inside the card:**

```
┌ Attachments ─────────────────────────────┐   h2 (Card title)
│ No files attached yet.                   │   only when there are none
│ Add a file                               │   fieldset legend
│ PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or │   hint, before the controls
│ CSV, up to 10 MB. One file at a time.    │
│ [(camera) Take photo]  [(upload) Choose file]         │   two secondary buttons, ≥ 44 px tall, 8 px gap; wrap to two rows if narrower
│ (!) File is larger than 10 MB. Choose a…  │   #attachments-msg: inside the fieldset, right after the controls (field.templ:95); empty until a refusal
│ ▓▓▓▓▓▓▓▓░░░░░░░░░░                        │   progress bar, after the fieldset (upload.templ:40), hidden until an upload runs
│ Uploading…                               │   role="status" line under the bar (§6 change 1); text inserted only while an upload runs
│ ┌──────────┐                              │
│ │ thumbnail│  160 px max, square          │   image: thumbnail link
│ └──────────┘                              │
│ receipt.jpg                              │   name (images and HEIC)
│ 2.3 MB                                   │
│ Deepak Nair · 24-10-2026 10:30           │
│ [Remove]                                 │   draft and rejected only
│ ((file icon) quote-shree-steel.pdf)                │   PDF and other files: a chip with the name
│ 512 KB                                   │
│ Priya Shah · 24-10-2026 10:05            │
│ ((file icon) HEIC image, opens as download)        │   HEIC: chip, name below
│ challan.heic …                           │
└──────────────────────────────────────────┘
```

At 768 px: the same single column; the card is as wide as the content; `FileUpload` keeps its 40rem max width. At 1024 px: unchanged on the check step (the wizard uses the content width). From 1280 px (80rem) on the document page the card moves into the right side column (`doc-side`, 320 px), under the state badge and above Approval history; the thumbnails stay 160 px and the two buttons wrap to two rows if 320 px is too narrow for both.

**Primary action.** None in the card. Both upload buttons are secondary (`FileUpload` as built), so the page's one primary stays the state action in the action bar (`Submit for approval`, `Approve`, …). Remove is secondary; the danger button exists only inside the dialog.

**Components and the props that matter**

- `components.Card("Attachments", nil)`.
- `components.EmptyState("No files attached yet.", nil)`.
- `components.FileUpload(UploadProps{ID: "attachments", Label: "Add a file", Action: "/attachments/{doctype}/{id}", Hint: "PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or CSV, up to 10 MB. One file at a time.", Accept: "image/*,application/pdf,.xlsx,.docx,.csv", Files: files})`. `NoCamera` false, so `Take photo` (`accept="image/*" capture="environment"`) is present. `Target` default (`#attachments`); it is never swapped on success (the page reloads) or on a refusal (`HX-Reswap: none`, §6 change 3). The `Accept` keeps `image/*` so phones still offer the photo library, and adds the three office types of D-FILE-03; anything else chosen (GIF, SVG) is refused by the server with FILE's type message.
- `components.Attachments(files)` for read-only, each `components.Attachment` built by `extras.Attachments` as today (`Size` from `files.HumanSize`, `UploadedAt` from `types.FormatIST`, `Kind` "image" / "heic" / "file", `URL` `/files/{id}`), plus `RemoveURL: "/files/{id}/remove"` only when `canRemove`.
- `components.Dialog("remove-file-{id}", "Remove this file?")` per removable file (§6 change 2).

**Copy (final)**

| Where | Text |
|---|---|
| Card heading | `Attachments` |
| Empty | `No files attached yet.` |
| Legend | `Add a file` |
| Hint | `PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or CSV, up to 10 MB. One file at a time.` |
| Buttons | `Take photo`, `Choose file` (component text) |
| While uploading | `Uploading…` |
| File meta | `{size}` then `{uploader name} · {dd-mm-yyyy HH:MM}` IST, e.g. `2.3 MB` / `Deepak Nair · 24-10-2026 10:30`; a copy made by amend shows the amending user and time (FILE-043) |
| HEIC chip | `HEIC image, opens as download` (FILE-033) |
| Remove button | `Remove`, accessible name `Remove receipt.jpg` |
| Dialog title | `Remove this file?` |
| Dialog body | `receipt.jpg will be removed from this document. You can attach it again later.` |
| Dialog buttons | `Remove file` (danger, busy label `Removing…`), `Keep file` (secondary) |

**Refusal messages: FILE's text, passed through unchanged (design-system §9).** These are the kernel's strings (`internal/kernel/files/errors.go`, `attach.go`, `remove.go`, `read.go`, `files/web/respond.go`, `db.ErrBusy`); the screen never rewrites them.

| Refusal | Status | Message |
|---|---|---|
| Over 10 MB (declared or streamed) | 413 | `File is larger than 10 MB. Choose a smaller file or compress it.` A declared size over the limit is answered before the body is read (`upload.go:37-40`); a browser still sending may report that as a network error, in which case the dropped-connection text of §3 shows instead (ATT-015) |
| Empty file | 422 | `File is empty. Choose another file.` |
| Type not allowed | 422 | `File type .gif is not allowed. Use PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or CSV.` (`(none)` when there is no extension; the extension is the file's own, lower-cased, so `बिल.रसीद` gives `File type .रसीद is not allowed. …`, `sniff.go:28-35`) |
| Content doesn't match extension | 422 | `File content does not match its .pdf extension. Upload the original file.` |
| CSV not UTF-8 | 422 | `CSV files must be UTF-8. Save the file as CSV UTF-8 and try again.` |
| Same bytes already on this document | 422 | `This file is already attached to Draft 42.` (the document's display, e.g. `GR/26-27/00001`) |
| Count limit | 422 | `A document can have at most 20 attachments. Remove one first.` (a DocType policy with another count says its own number, Expense spec §3.3) |
| No `write` on add | 403 | `You don't have permission to add files to Draft 42.` |
| No `write` on remove | 403 | `You don't have permission to change Expense Claim. Ask your administrator for a role that allows it.` (the DocType label) |
| Add to cancelled | 422 | `This document is cancelled. Attach files to its amended copy instead.` |
| Remove after submit | 422 | `Files can't be removed after the document is submitted. Cancel and amend it to change files.` |
| Remove of a line's receipt | 422 | `This file is the receipt on line 2. Clear it there first.` (only DocTypes with a `RemoveGuard`; none of the flagged ones in v1) |
| Upload to a document that doesn't exist or the user can't see | 404 | `Save the document before attaching files.` (`errors.go:38`, `attach.go:116-121`, `parentErr` `attach.go:157-163`) |
| Upload URL with a bad id; remove or open of a file that is gone or whose document is hidden | 404 | `File not found.` (`upload.go:31-34`, `remove.go:31`, `read.go:162-167`) |
| No file part / two file parts (crafted requests only) | 422 | `Choose a file to upload.` / `Upload one file at a time.` |
| Busy | 503 | `The system is busy. Please try again in a moment.` (`db.ErrBusy`) |
| Storage failure | 500 | `Couldn't save the file. Try again or contact your administrator.` (`files.MsgStorage`) |
| Connection dropped (no answer) | none | `The upload didn't finish. Check your connection and try again.` (new copy, written by `upload.js`, §6 change 2) |

A refusal shows in two places, as a field error does with its summary: in the card's message region right after the controls (`#attachments-msg`, error style with the alert icon), and as a persistent danger notice in the page's `#alerts` region (`role="alert"`, so screen readers announce it). An upload refusal also sets `aria-invalid="true"` on both file inputs; a refused Remove doesn't (the inputs weren't wrong). The dropped-connection text shows only in `#attachments-msg` (there is no server notice). The message region and `aria-invalid` are cleared when the next upload starts.

**Focus and keyboard**

- On load: unchanged. The check step's heading takes focus (`autofocus`, wizard `Frame`); the document page has no autofocus.
- `Take photo` and `Choose file` are labels over visually hidden file inputs; Tab reaches each input, its label shows the focus ring (`.upload__button:has(:focus-visible)`), and Space or Enter opens the picker. After a file is chosen the input is disabled while uploading, which drops focus to `<body>`; so `upload.js` remembers the requesting input and, when the request ends without a refresh (a refusal or a dropped connection), re-enables it and moves focus back to it (only if it is still in the page; the company logo page redraws its area, and focus then follows that page's own rules). On success the page reloads.
- After a refusal: focus is back on the input the user used; the message is announced by `#alerts` and, through `aria-describedby`, read with the input.
- Remove: Enter or Space opens the dialog; focus goes to the dialog (native `showModal`, first focusable is its Close button); Tab moves to `Remove file` and `Keep file`; Escape closes; on close focus returns to the Remove button (`dialog.js`). On a refused remove the dialog closes first, then the message shows (§6 change 2).
- Enter inside the card never submits the wizard: the card sits in the check step's `After`, outside the step form.
- After a successful upload or remove, the page reloads at `#attachments` (the URL keeps the fragment): the browser scrolls to the card, and because the URL names an element it skips the heading's `autofocus`, so the next Tab reaches `Take photo`. Without the fragment the check step's heading autofocus scrolled the page back to the top (B4 build finding, 31 Oct). ATT-022 asserts the card is in view after the reload.

### 5.2 Check step (draft and amended draft)

**Phone layout (360 px), top to bottom:** title line, Stepper (`Step 3 of 3`), conflict banner / error summary / warnings when present, `h1` `Check and submit`, the step form (hidden version only), then `checkBody`: `Amended from …` link, approval preview banner, one summary section per earlier step, **the Attachments card**, grand total and amount in words, `More actions`, then the action bar (`Submit for approval` or `Submit`, `Save and finish later` is not on the check step).

The card goes after the step sections and before the total, so the total and the primary button stay together nearest the thumb (§4.12) and the files are reviewed with the rest of the content. At 768 and 1024 px: the same order in the wide wizard column.

**Primary action:** the state action in the action bar, unchanged. The card adds none.

**Copy:** §5.1. On an amended draft the copied files show with Remove (it is a draft) and the amending user as uploader (FILE-043); removing one leaves the original's file alone (FILE-044).

**Focus:** §5.1; the heading keeps its autofocus on an ordinary load, but not on the reload after an upload or remove, which opens at `#attachments`.

### 5.3 Document page (rejected, pending approval, approved, closed, cancelled)

A draft never shows this page (it redirects to the check step, `page.go`), so the page covers the other five states.

**Phone layout (360 px), top to bottom** (the `doc-layout` order, design-system §8.4): banners (rejected reason, refused action, success), state badge, party, amount and words, who it waits on, `Amended from …`, warnings, Details, Lines, module cards (`PageExtra.Cards`, e.g. GRN `Bill details`), **the Attachments card** (except cancelled), Approval history, History; then `More actions` and the action bar.

From 1280 px: content on the left; the right column holds the state badge, **the Attachments card** and Approval history.

| State | Card (writer) | Card (reader) | Action bar (unchanged) |
|---|---|---|---|
| Rejected | `FileUpload` + list with Remove | list or empty text | `Revise` for the submitter |
| Pending approval | `FileUpload` + list, no Remove | list or empty text | `Approve` / `Reject` for the approver; `Withdraw` for the submitter |
| Approved | `FileUpload` + list, no Remove | list or empty text | per rights (Cancel, Close in More actions) |
| Closed | `FileUpload` + list, no Remove | list or empty text | per rights |
| Cancelled | no card | no card | `Amend` per rights |

An approver who also has `write` (Ravi on a PO) sees `Add a file` while approving. That follows D-FILE-06 and the owner's point 3.

**Primary action, copy and focus:** §5.1.

### 5.4 Remove confirm dialog

**Phone layout (360 px):** the native `<dialog>` centred (`Dialog`, not `Sheet`): heading `Remove this file?` with the Close (×) button, body text, then `Remove file` (danger) and `Keep file` (secondary) stacked full width, 8 px apart. From 768 px the two buttons sit side by side. The dialog body is a `components.Form{Action: "/files/{id}/remove", HX: true}` holding `Remove file` as its submit; `Keep file` is `type="button"` with `data-close-dialog`.

**Primary action:** `Remove file` (danger is the only button style allowed for it, §7 `Button`). No primary-coloured button in the dialog.

**Copy:** §5.1. **Focus:** §5.1.

## 6. New components or pattern changes

No new component. Three changes to existing ones, each needed because the current behaviour can't meet a checklist line; each is to be added to design-system §7 (or, for the third, to the FILE plan) once approved.

1. **`FileUpload` says it is working, blocks a second upload and keeps focus.** Today its forms have no submit button, so `hx-disabled-elt="find button[type=submit]"` disables nothing, and the progress bar has no words (§4.8: "never a spinner with no words"). Change: each upload form sets `DisabledElt` to both file inputs of its `FileUpload` (`#{id} input[type=file]`); the component renders an empty `<p id="{id}-status" role="status"></p>` under the bar; on `htmx:beforeRequest` from a form inside `.upload__controls`, `upload.js` remembers the requesting input, shows the bar and inserts the text `Uploading…` into `#{id}-status` (inserting into a live region gets it announced; un-hiding pre-filled text often isn't). On **every** `htmx:afterRequest` from inside `.upload`, and on `htmx:sendError`, it hides the bar, resets it to 0 and empties `#{id}-status`: the app's htmx config treats 422 and 409 as non-errors (`layout.go:42`), so `detail.successful` is true on a 422 refusal and can't be the test (today's `upload.js` hides the bar only when `successful` is false, which misses every 422). When the request ended without `HX-Refresh`, it then moves focus back to the remembered input if that is still in the page. Proposed design-system §7 `FileUpload` text: "…progress via `htmx:xhr:progress`; while a file uploads both inputs are disabled and `Uploading…` is announced under the bar; when the request ends without a page reload the bar and text clear and focus returns to the input used…". The company logo page keeps working (it re-renders the upload area on every answer).
2. **Refusals show next to the control, and Remove confirms.** Today a non-2xx answer to the upload swaps nothing and shows nothing near the control, and `Attachments`' Remove posts at once with no confirm (§8.5 rule: destructive actions confirm). Change: (a) `upload.js` listens for the `notice` event that htmx dispatches on the requesting form; when that form is inside a `.upload`, it closes any open `<dialog>` around the form, and writes the text into that upload's `#{id}-msg` with the error style and icon (cloned from a `<template>` the component renders, text set as a text node). Only when the form is an upload form (inside `.upload__controls`) does it also set `aria-invalid="true"` on both inputs; a refused Remove doesn't. On `htmx:sendError` from an upload form it writes `The upload didn't finish. Check your connection and try again.` the same way (no notice exists then). The next `htmx:beforeRequest` clears the region and `aria-invalid`. `toast.js` still puts the same notice in `#alerts`. (b) `Attachments` renders, for an item with `RemoveURL`, a `Remove` button (`type="button"`, `data-open-dialog="remove-file-{id}"`, aria-label `Remove {name}`) and the `Dialog` of §5.4, whose form posts with `HX: true` and the `hx-target` of the `FileUpload` it sits in (`FileUpload` passes its `Target`, default `#{id}`, down to the list; `Attachments` rendered alone never has a `RemoveURL`). On a document the answer is `HX-Refresh`, so the target is never used; on the company logo page the remove handler's answer redraws `#upload-area` as it does today, now after the confirm (ATT-028).
3. **`files/web` answers htmx (CD-241, made exact).** For a request with `HX-Request: true`: a successful upload answers 201 and a successful remove 204, each with `HX-Refresh: true` (body may stay as today; htmx ignores it). A refusal keeps its status and text but sends an empty body, `HX-Reswap: none`, and `HX-Trigger: {"notice":{"level":"error","text":"<message>"}}`. The trigger is written by one exported helper in `internal/platform/web` (proposed `web.SetNotice(h http.Header, level, text string)`, the JSON plus `asciiJSON` that `web.Error` already uses at `errors.go:72-99`; `web.Error` calls it too), never a second copy in files/web. Without `HX-Request` nothing changes: 201 JSON, 204, plain-text errors. Needed because `http.Error`'s plain text, under the app's htmx config, would replace the whole upload with bare text on a 422 and show nothing at all on a 403, 413, 500 or 503.

**New declared flag (CD-241, CD-243 point 4).** `doctype.DocType` gains `Attachable bool`, next to `Submittable`, `Approvable` and `Printable`: "the document page (every state except cancelled) and the check step show the Attachments section, with `Add a file` for users with `write`". docscreen reads it; FILE's rules don't depend on it (an unflagged DocType's files are still governed by D-FILE-04/06). Set by: `fixture.Claim` (true), `po.Order` and `grn.Receipt` (true, in their module WPs). Not set: `expense.Claim`, `expense.Advance`, `fixture.Order`, `fixture.Receipt`, every master. With the flag off, the document page keeps today's read-only card when files exist.

**Gallery mock (protocol §9 step 2).** No code is changed by this stage. Director's ruling: the `/_gallery/attachments` mock pages below are the **first build task**, and get a crew-reviewer check before any screen task is released. The mock, at `/_gallery/attachments`, built from the real components with sample data: (a) writer, draft: `EmptyState` + `FileUpload`; (b) writer, draft, three files (`receipt.jpg` thumbnail, `challan.heic` chip, `quote-shree-steel.pdf` chip) with Remove; (c) writer, approved: same three files, no Remove; (d) reader: `Attachments` only; (e) refused: the message region holding `uitest.LongError`; (f) 20 files with a 120-character name and a Devanagari name (`बिल रसीद.jpg`); (g) a `screens.Screen` variant with the remove dialog open. The gallery's docscreen seed (`docscreentest.Sample`) sets `Attachable`, so `/doc/sample/42/edit/check` and an approved sample show the card in place at 360, 768 and 1440 px.

## 7. Checklist

- [x] One job and one primary button per phone screen (design-system §4.1). The card adds only secondary buttons; the page's single primary stays the state action; the dialog's only strong button is the danger `Remove file`.
- [x] Renders fully without JavaScript; htmx and Alpine only enhance (§4.2). The page, card, list, thumbnails and file links render and work without JavaScript. Adding and removing are enhancements by catalogue definition: design-system §7 lists `FileUpload` as "htmx + app.js" and `Dialog` as "app.js", CD-133(c) built FileUpload without a submit button, and CD-241 keeps JSON for non-htmx posts.
- [x] Business words (§4.3, §9): document displays `Draft 42` / `GR/26-27/00001` come from FILE's messages; sizes `2.3 MB`, `512 KB`; times `24-10-2026 10:30` IST; the state stays in the `StateBadge`.
- [x] Nothing wider than the screen at 320 px (§1.3, §4.4): names wrap (`overflow-wrap: anywhere`, already in `attachments`), thumbnails are `min(100%, 10rem)`, the buttons wrap, the dialog is a native modal box. ATT-018 checks a 120-character name, a Devanagari name, `LongError` in the message region and 20 files at 320–1440 px.
- [x] Targets (§4.5): `Take photo`, `Choose file`, the file chips and `Remove` are at least 44 × 44 px with 8 px gaps; dialog buttons full width on phones. The action bar is unchanged at 48 px. Gloves: the two upload buttons sit side by side with an 8 px gap from about 370 px; narrower (360 px phones, where the card leaves 278 px) they stack, each full width, 8 px apart, which gives a larger target for gloved hands (ATT-019 measures them; mock review, 31 Oct).
- [x] Never colour alone (§4.6): refusals carry the alert icon and words; HEIC says `HEIC image, opens as download`; the danger button says `Remove file`.
- [x] Defaults, fewest taps (§4.7): `Take photo` opens the rear camera in one tap and uploads on confirm, with no extra "Upload" button; the newest file shows first.
- [x] Wizards (§8.3): no new step; the card joins Check and submit, which keeps its 3–5 fixed steps.
- [x] Labels and errors (§7 `Field`, `ErrorSummary`): legend `Add a file` and the limits are always visible before the controls; a refusal says what's wrong and what to do (FILE's text), next to the control and in `#alerts` (§6 change 2).
- [x] Destructive actions confirm (§8.5): Remove opens a confirm dialog; adding a file needs no confirm.
- [x] Nothing typed is lost (Back, 422, 409): the check step has no unsaved input, and a refusal swaps nothing. A GRN `Bill details` form on the same page is the GRN design's to handle.
- [x] Visibility of system status (Nielsen 1): progress bar plus `Uploading…`, disabled controls, the new file first in the list after the reload, refusals in two places, and the dropped-connection message. After a successful reload, the new file first in the list is the "what happened" (files/web has no session for a flash, `routes.go:22-26`).
- [x] Recognition over recall (Nielsen 6): each file shows its thumbnail or type, name, size, who and when; no picker is involved.
- [x] WCAG 2.2 AA (§5, §10): tokens only; focus ring on the upload labels; the card's `h2`, fieldset legend and `aria-describedby` (hint and message) give accessible names; Remove buttons name their file; refusals are announced (`role="alert"`) and identified by `aria-invalid`; the sticky action bar's scroll padding keeps the focused control visible. axe zero violations: ATT-020.
- [x] Speed (§4.8): reload after a 3 MB upload is over 400 ms on mobile data, so the bar and `Uploading…` show for the whole wait.
- [x] The system works things out (§4.9, §4.12): type, size, uploader and time are read from the file and session, never asked; the server sniffs the real type.
- [x] Next step's choices only (§4.12): Remove only where allowed; nothing on cancelled; History keeps the audit entries behind its disclosure.
- [x] Consistent (§4.11): the same card on both pages and every flagged DocType; same `FileUpload` as the company logo page and the expense line page; same dialog pattern as other short confirms.

## 8. Acceptance checks

Ids are placeholders (`ATT-0nn`) until the director gives the area its prefix and test file. Fixtures: `00-fixtures.md`; `fixture.Claim` with `Attachable` set; `fixture.Order` unflagged; fixture clock 24-10-2026 10:30 IST. "Panel" means the Card titled `Attachments`. The flag on the real DocTypes is tested by their module WPs: PO and GRN each assert `Attachable` is true (`po.Order`, `grn.Receipt`), and Expense asserts it is false on `expense.Claim` (and `expense.Advance`); those rows live in the module test files, not here.

| Id | Behaviour | Given / When / Then | Level | Go test (proposed) |
|---|---|---|---|---|
| ATT-001 | The flag | **Given** the registry **When** `doctype.Lookup` is read **Then** `fixture.Claim.Attachable` is true and `fixture.Order.Attachable` and `fixture.Receipt.Attachable` are false | unit | `TestFixture_AttachableFlag` |
| ATT-002 | Flag off keeps today's page | **Given** approved `fixture.Order` `PO/26-27/00001` with `quote.pdf`, and a draft with none **When** Priya opens each **Then** the approved page shows the read-only panel with `quote.pdf` and no `Add a file`, no Remove; the draft's check step has no panel | integration | `TestDocPage_AttachmentsFlagOff` |
| ATT-003 | Check step panel, writer | **Given** Deepak's draft claim `Draft 42` with no files **When** he opens `/doc/fixture.Claim/42/edit/check` **Then** after the step sections and before the grand total there is a panel with `No files attached yet.`, legend `Add a file`, the hint `PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or CSV, up to 10 MB. One file at a time.` before both file inputs, a `Take photo` input with `accept="image/*"` and `capture="environment"`, a `Choose file` input with `accept="image/*,application/pdf,.xlsx,.docx,.csv"` and no `multiple`, and both forms `hx-post="/attachments/fixture.Claim/42"` with `hx-encoding="multipart/form-data"` and `hx-trigger="change"` | integration | `TestCheckStep_AttachmentsPanel` |
| ATT-004 | Panel by state (writer) | **Given** Deepak's claim with `receipt.jpg` in each of draft, rejected, pending_approval, approved, closed, cancelled **When** Deepak opens its page (check step for draft) **Then** draft and rejected show `Add a file` and `Remove`; pending_approval, approved and closed show `Add a file` and no `Remove`; cancelled has no element with the heading `Attachments` | integration (table) | `TestDocPage_AttachmentsByState` |
| ATT-005 | Reader sees, can't add | **Given** Deepak's pending claim with `receipt.jpg`, and one with no files **When** Priya (read and approve `team`, no `write`) and Meena (Director) open them **Then** the panel lists `receipt.jpg` (or shows `No files attached yet.`), with no file input and no `Remove`, and Priya's action bar still shows `Approve` as the primary | integration | `TestDocPage_AttachmentsReadOnly` |
| ATT-006 | List formatting | **Given** Deepak's draft with `receipt.jpg` (2,456,789 B, JPEG) uploaded at 04:30 UTC, `quote.pdf` (524,288 B) at 04:35 UTC and `challan.heic` at 04:40 UTC **When** the check step renders **Then** the list order is `challan.heic` (`Deepak Nair · 24-10-2026 10:10`), then `quote.pdf` (`Deepak Nair · 24-10-2026 10:05`), then `receipt.jpg` (`Deepak Nair · 24-10-2026 10:00`); `receipt.jpg` is an `<img loading="lazy" alt="receipt.jpg">` inside a link to `/files/{id}`, followed by `2.3 MB` and `Deepak Nair · 24-10-2026 10:00`; the PDF is a chip with its name and `512 KB`; the HEIC chip says `HEIC image, opens as download` and has `download` | integration | `TestAttachmentsPanel_Formatting` |
| ATT-007 | Remove opens a confirm | **Given** a draft with `receipt.jpg` **When** the page renders **Then** its `Remove` is `type="button"` with accessible name `Remove receipt.jpg` and `data-open-dialog="remove-file-{id}"`; the dialog has heading `Remove this file?`, the text `receipt.jpg will be removed from this document. You can attach it again later.`, a form posting `hx-post="/files/{id}/remove"` with submit `Remove file` (danger, busy `Removing…`) and a `Keep file` button with `data-close-dialog` | unit (components) | `TestAttachments_RemoveConfirm` |
| ATT-008 | Upload answers htmx with a refresh | **Given** Deepak's draft **When** `receipt.jpg` is posted with `HX-Request: true` **Then** the status is 201 and `HX-Refresh: true` is set; the same post without the header still answers 201 JSON `{id, file_name, size, content_type}` with no `HX-Refresh` | integration | `TestUpload_HTMXRefresh` |
| ATT-009 | Remove answers htmx with a refresh | **Given** a draft with `receipt.jpg` **When** it is removed with `HX-Request: true` **Then** 204 with `HX-Refresh: true`; without the header, 204 with no `HX-Refresh` | integration | `TestRemove_HTMXRefresh` |
| ATT-010 | Refusals reach htmx as notices | **Given** each server refusal in §5.1's table (not the dropped connection) **When** it is triggered with `HX-Request: true` **Then** the status is the table's, the body is empty, `HX-Reswap` is `none`, and `HX-Trigger` decodes to `{"notice":{"level":"error","text":<the table's message>}}` with the exact text. Cases include: an upload to `/attachments/fixture.Claim/999999` (no such record) and to a claim Deepak can't see, both 404 `Save the document before attaching files.`; an upload to `/attachments/fixture.Claim/abc` and a remove of `/files/999999/remove`, both 404 `File not found.`; `Draft 42` in the duplicate and add-permission messages; and the one real non-ASCII case, a file named `बिल.रसीद`, whose 422 text `File type .रसीद is not allowed. Use PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or CSV.` must decode exactly from the ASCII-escaped header. The line-receipt row needs a `RemoveGuard` registered by the test (no flagged DocType has one); the 503 and 500 rows need fault injection (a held write lock giving `db.ErrBusy`, a read-only `files/` directory as in FILE-037). Without the header the status and plain text are unchanged | integration (table) | `TestFilesWeb_HTMXRefusals` |
| ATT-011 | Attaching doesn't change the version | **Given** Deepak's check step showing version 3 **When** he uploads `receipt.jpg` and then taps `Submit for approval` from the reloaded page **Then** the submit succeeds (no 409), the record's version is unchanged by the upload, and the audit has `attachment_added` before the submit | integration | `TestCheckStep_UploadThenSubmit` |
| ATT-012 | Add after approval, no Remove | **Given** Deepak's approved `fixture.Claim` `EX/26-27/00001` **When** Deepak attaches `bill.pdf` **Then** it is listed, state and fields don't change, there is no `Remove` on any file, and a crafted remove post gets 422 `Files can't be removed after the document is submitted. Cancel and amend it to change files.` | integration | `TestDocPage_AddAfterApproval` |
| ATT-013 | Amended draft shows copied files | **Given** cancelled `EX/26-27/00001` with `receipt.jpg`, amended by Deepak **When** the new draft's check step opens **Then** `receipt.jpg` is listed with `Remove` and `Deepak Nair · {amend time}`; removing it leaves the cancelled original's file in place | integration | `TestCheckStep_AmendedFiles` |
| ATT-014 | Uploading state | **Given** the check step at 360 px with the upload request held by the harness **When** a file is set on `Take photo` **Then** both file inputs are disabled, the progress bar is visible and `#attachments-status` (`role="status"`, empty before the upload) now contains `Uploading…`; when the held request is answered with a 422 refusal, both inputs are enabled, the bar is hidden, `#attachments-status` is empty again and focus is on the input used | e2e | `TestE2E_AttachmentUploadingState` |
| ATT-015 | Refusal next to the control | **Given** the check step at 360 px in Chrome **When** a 10,485,761-byte JPEG (one byte over the limit, so it passes the early Content-Length check at `upload.go:37-40` and is refused while the body is read, a reliable answer) is set on `Choose file` **Then** `File is larger than 10 MB. Choose a smaller file or compress it.` (413) appears in `#attachments-msg` with the alert icon and in `#alerts`; `document.activeElement` is the `Choose file` input, enabled; both inputs have `aria-invalid="true"`; the list is unchanged and the page didn't reload. Choosing a valid file next clears the message and `aria-invalid`. A second case sets an 11 MB JPEG (refused before the body is read): `#attachments-msg` shows either the 413 text above or ATT-029's `The upload didn't finish. Check your connection and try again.`, and in both cases the inputs are enabled and focus is on `Choose file`. | e2e | `TestE2E_AttachmentRefused` |
| ATT-016 | Remove flow | **Given** a draft with `receipt.jpg` at 390 px **When** Deepak taps `Remove` **Then** the dialog opens; `Keep file` closes it with no request and focus is back on `Remove`; tapping `Remove` then `Remove file` reloads the page without `receipt.jpg`, and the audit log (read through `audit`, since the check step renders no History, `check.templ`) has an `attachment_removed` entry for `receipt.jpg` | e2e | `TestE2E_AttachmentRemove` |
| ATT-017 | Remove refused closes the dialog | **Given** a draft page with `receipt.jpg` open in one tab, and the draft submitted from another **When** `Remove file` is tapped **Then** the dialog closes, focus is on `Remove`, and `Files can't be removed after the document is submitted. Cancel and amend it to change files.` shows in `#attachments-msg` and `#alerts` | e2e | `TestE2E_AttachmentRemoveStale` |
| ATT-018 | No sideways scroll | **Given** the check step and the approved page with 20 files, one named with 120 characters, one `बिल रसीद.jpg`, and the refused state holding `uitest.LongError`, and the dialog open **When** rendered at 320, 360, 390, 768, 1024 and 1440 px **Then** the overflow check passes on each | e2e (sweep) | screen entries in `e2e/routes.go` |
| ATT-019 | Targets | **Given** the check step at 360 px with one removable file **When** measured **Then** `Take photo`, `Choose file`, the file link, `Remove`, `Remove file`, `Keep file` and the dialog's Close are each at least 44 × 44 px, with at least 8 px between the two upload buttons | e2e | sweep target check |
| ATT-020 | Accessibility | **Given** the check step and document page with files, the refused state and the open dialog **When** axe-core runs **Then** zero WCAG 2.2 AA violations | e2e | sweep axe check |
| ATT-021 | Desktop placement | **Given** an approved claim with files at 1440 px **When** the page renders **Then** the panel is in the right column below the state badge and above `Approval history` | e2e | `TestE2E_AttachmentsSidePanel` |
| ATT-022 | Expense from phone with receipt (WP3 plan) | **Given** Deepak at 360 px **When** he creates a `fixture.Claim` through the wizard, on the check step sets a JPEG on the `Take photo` input (the one with `capture`), sees its thumbnail after the reload with the Attachments card inside the viewport (its bounding box intersects the visible area), and taps `Submit for approval` **Then** the claim is pending; Priya at 390 px sees it in her inbox through the `team` scope, opens it, sees the thumbnail in the panel with no `Add a file`, taps `Approve`, and the claim is approved with the file still listed; the time from setting the file to the thumbnail showing is under 15 s | e2e | `TestE2E_ExpenseFromPhoneWithReceipt` |
| ATT-023 | Preview (FILE-033) | **Given** a `fixture.Claim` with `r.jpg` and `r.heic` **When** its page is viewed in Chrome **Then** `r.jpg` shows a lazy-loaded preview no larger than 160 px, and `r.heic` shows a file icon labelled `HEIC image, opens as download` | e2e | `TestE2E_ExpenseAttachmentPreview` |
| ATT-024 | Upright preview (FILE-035) | **Given** a JPEG with EXIF orientation 6 attached to a `fixture.Claim` **When** the panel renders **Then** the thumbnail is taller than wide | e2e | `TestE2E_ExifOrientation` |
| ATT-025 | Late photo on an approved document (GRN-028's flow, on the fixture) | **Given** approved `EX/26-27/00001` at 360 px **When** Deepak taps `Choose file` and picks `bill.pdf` **Then** after the reload the panel lists `bill.pdf` first, the state badge still says `Approved`, and there is no `Remove` | e2e | `TestE2E_AttachmentAfterApproval` |
| ATT-026 | Upload clears the busy state on a 422 | **Given** the check step at 360 px **When** a `.gif` is set on `Choose file` (422, which the htmx config swaps as a non-error) **Then** the progress bar is hidden, `#attachments-status` is empty, both inputs are enabled, focus is on `Choose file`, and the type message shows in `#attachments-msg` | e2e | `TestE2E_AttachmentRefused422ClearsBusy` |
| ATT-027 | Remove refusal doesn't mark the inputs | **Given** ATT-017's stale page **When** the refused remove's message shows **Then** neither file input has `aria-invalid` | e2e | part of `TestE2E_AttachmentRemoveStale` |
| ATT-028 | Company logo Remove now confirms | **Given** the company logo page with a logo, Anil at 1440 px **When** he taps `Remove` **Then** the `Remove this file?` dialog opens with no request sent; `Keep file` closes it and the logo stays; `Remove file` posts to the logo page's remove route and the upload area is redrawn without the logo, as before the change | e2e | `TestE2E_CompanyLogoRemoveConfirms` |
| ATT-029 | Dropped connection | **Given** the check step at 360 px **When** chromedp sets the network offline (`network.EmulateNetworkConditions` with `offline: true`) and a JPEG is set on `Take photo` **Then** `#attachments-msg` shows `The upload didn't finish. Check your connection and try again.`, `#alerts` has no new notice, the progress bar and `Uploading…` are gone, both inputs are enabled, focus is on `Take photo`, and the list is unchanged; back online, the same file uploads | e2e | `TestE2E_AttachmentNetworkDrop` |

## 9. Open questions

None. The review settled the earlier questions: no-JavaScript adding (catalogue and CD-133, CD-241; §7), cancelled documents (CD-243, noted in §3), the success announcement (the new file first in the list; §7), scroll after the reload (asserted by ATT-022), the dropped connection (designed, §3 and ATT-029), GRN pages with other inputs (CD-243 for step 2; the GRN design for `Bill details`), and `Take photo` on desktop (kept as built, design-system §7 and §4 rule 11).
