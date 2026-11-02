VERDICT: REQUEST CHANGES. 1 BLOCKER, 6 HIGH, 13 MEDIUM, 10 LOW.

`report.md` was not written: the harness guard refused my Write (`crew-ui-reviewer is read-only`), so the findings are here instead. I didn't route the report through Bash around that guard, so this reply is longer than the one line you asked for. The screenshots are saved in your review folder. Scripts ran from stdin, except one stray `s1.py` that I deleted. Likely files are from a code read, or inferred where no file is named.

**BLOCKER**
- Line page, 360. With the network down, Save line goes to the browser's own error page (`chrome-error://`, "ERR_INTERNET_DISCONNECTED"). The form is a plain POST; `id=line-form` has no `hx-post`, unlike the wizard steps, which show the banner and keep what was typed. In headless Chrome, Back landed on `/approvals`, so I saw no recovery of the amount, type or photo link. Required: §3.3 and A-28, a "Couldn't reach the server" banner with Try again and typed values kept. This is Deepak's main case. Likely file: `internal/ui/docscreen/lines.go`, shared with the other modules' line pages. <shots>/69-line-offline-save-360.png

**HIGH**
- All expense reports, all widths. Nothing links to `/reports/expense/*`: not the nav, Home, the lists, the document page or `/search`. Only `/reports/grn` and `/reports/purchase/pending` have links. The Director and accounts can reach these reports only by typing the URL. Required: §5.2 Reports entry. Likely file: `internal/app/routes.go`. <shots>/s-rep-open-1280.png
- After Submit, 360 and 390. The document page keeps the old scroll position. From a scrolled Check page I landed on Other files with the success block out of view, and focus was on BODY. The advance reproduced it too (scrollY 118 before, 174 after). Required: §5.13 success block visible and announced, §3.1 clear confirmation. <shots>/23-after-submit-360-view.png
- Upload dropped, 360. The banner reads "Your change may have been saved. Reload to check before trying again." and its Reload button discards the typed Type and Amount. The upload's POST carries `version` in the query string, so `app.js` treats it as unsafe. Required: §5.5 FILE text and try again, with typed fields untouched. Likely file: `internal/ui/js/app.js`. <shots>/09-upload-offline-360.png
- Advance Details, 390. Advance date is blank, though Claim date defaults to today. Nothing complains until Submit on Check, which bounces back to step 1 with "Advance date is required." Required: §5.10 "DateInput, default today". <shots>/36-adv-details-redraw-390.png
- Every date field, 360. The field uses `inputmode="numeric"`, but typing `05112026` gives no dashes and is refused as "not a valid date". `2-11-2026` and `02/11/2026` work; `02.11.26` and `02 11 2026` don't. A numeric keypad may have no dash (inferred, not tested on a real phone). Required: §5.7 and the owner's bar, digits should format as typed. Likely file: `internal/ui/js/date.js`. <shots>/11-line-422-360-full.png
- Line page, 360×640. There is no focus mode: the app header, Back link and h1 sit above the card, with no wizard head or save cue.
  - Measured: the footer top is at 568 px, Type starts at 616 (776 with a photo), and Amount at 843 (1003 with a photo).
  - The design has Food on screen and Amount at 650 to 700 px. Now Type needs a scroll first, about 490 px to bring Type and Amount into view.
  - It stays 7 taps and one long scroll, but the first thing you see after the camera is not the next task.
  - Required: §5.6, the mock and §5.5. <shots>/07-line-attached-360-view.png

**MEDIUM**
- Line page, 360. Refused uploads stack in `#alerts`, and they stay even after a later success. Four banners plus the offline one fill the screen. Required: one current message, cleared on success. <shots>/09-upload-offline-360.png
- Check, 360. Expenses shows "Complete" while lines say "No receipt yet", then Submit bounces. The summary links go to `#step-title`, not the line. Required: §1 and §5.6 name those lines before submit, Needs attention badge. <shots>/20-submit-refused-360.png
- Line page, 360. Save line on an empty form saves a card with no type and no amount, despite the asterisks. Draft saving is by design, but the blank card isn't usable. Required: a card that says what is missing, or a refusal. <shots>/74-blank-line-saved-360.png
- Wizard, 360. × on Details and Expenses goes to the draft, which redirects to Check, so leaving takes two taps. "Save and finish later" also lands on Check, with no message. Required: §5.6, return to where the wizard was opened. <shots>/72-draft-open-360.png
- Claims list, 360 and 1280. No claim date at any width, and no sort. On phones the page keeps Export and New, pushing cards to about y=320. Required: §5.1 date on the card and columns from 1024 px, §5.12 New in the bottom nav and Export in Filters. <shots>/s-claims-360.png
- Document page, 360. Close, Cancel and Print sit at the very end of a page about 2,650 px long, and red filled Cancel is the loudest control. `KeyFacts` isn't set for expense, so the employee floats outside any card. Required: §5.13 sticky footer with ellipsis sheet, key facts in the header. <shots>/23-after-submit-360-full.png
- Totals and Details, 360 and 390. Money rows break into a label, then a right-aligned value on the next line; other Details rows are left-aligned. At 1280 Total and To pay sit far right of the other values. On the document page at 360, line amounts also drop to a second row. Required: aligned `kv` rows. <shots>/35-adv-after-submit-390-full.png
- Expenses cards, 360. Change and Remove have no icons, Remove has no danger text, and Move up/down are text on their own row. Each card is about 230 px tall. Required: §5.11 pencil, trash in danger, icon-only move. Likely file: `internal/ui/components/lines.templ`. <shots>/16-expenses-4lines-360-full.png
- Advance line and Mark as paid, 390. The primary is a small inline button touching the field above, not the sticky full-width footer the claim line has. Required: §5.6 and consistency. <shots>/32-adv-line-390.png
- Receipt, 390. `sheet.csv` is accepted and shown as "Attached to this expense", and it counts in "4 photos". The refusal text offers XLSX, DOCX and CSV, against the hint "Photo or PDF". Required: §5.5. <scratch>
- Browser title, all widths. The title stays stale after htmx moves: "Check · Draft 1" on the approved document, "Cancel EX/26-27/00002" on the amended Check, and no `Error:` prefix on bounces. Required: §5.14 and screen-reader orientation. <shots>/20-submit-refused-360.png
- Claim Details, 360. No cards and no "Fields marked *" line. The Purpose hint repeats its placeholder, and the date shows "For example, 27-03-2026" above a filled field. Advance Details has no intro line and no Purpose hint. Required: §5.5 items 1, 2 and 5, §5.6 intro. <shots>/02-claim-details-360-full.png
- Print, 360. No viewport meta, so a phone gets a 980 px layout, and the buttons are unstyled at 21 px tall. Required: 44 px targets; readable on a phone. <shots>/73-print-360.png

**LOW**
- Wizard head, 360: the title truncates to "New expense…" beside "Not saved yet". <shots>/02-claim-details-360-view.png
- Date chips, all widths: `aria-pressed` is null and the chosen day has no shown state (§5.7).
- Focus, all widths: BODY on line page load and on reason pages; the design asks for the h1 and the reason field.
- Save cue, all widths: absent after a line save and on the line page.
- Check, 360: banners touch each other and the first card. <shots>/19-check-360-view.png
- Footer, 1280: a full-bleed white band spills past the form column, not the §5.6 footer card. <shots>/s-line1-1280.png
- Receipt card, 320 and 360: buttons stack at unequal widths with reserved blank space below, and the refusal text sits under the footer. <shots>/08-upload-refused-txt-360.png
- Purpose, 360: no visible "0 of 140" counter, though the reason pages have one.
- Wording: "Expense Claims" against "Expense claims" in the nav, "Employee Advances", "Advances" and "Advance request" for one thing, and "Ask your administrator" shown to the administrator. Timeline shows "Pending approval (done)" with no approval rule, and Paid is never in the advance timeline.
- Console, 360: CSP "Applying inline style" errors after 422 redraws and uploads; cause not traced, no visible effect.

**Passed**
- No overflow, and no target under 44 px on the 12 swept routes at 320, 360, 390, 768 and 1280.
- Live commas, caret position and decimal cap; 409 keeps values; Tab order and visible focus at 1280.
- GST errors and the Billed to rule with one and two GSTINs; More details open rules; Tiles order, and Select at 7 types.
- E7b left nothing broken in the receipt flow: tile, disabled state during upload, focus kept while typing Amount, same-file note, PDF and HEIC tiles. The only gaps in it are the MEDIUM CSV item and the LOW card spacing above.

**Could not reach or verify**
- Pending approval, Rejected and the approver and reader views: there are no approval rules and I may not change settings.
- Non-admin Deepak: no linked user. His flow without the Employee code field is untested.
- Real camera, iOS and Android keypads, true 3G timing. The progress bar stayed at 24% under CDP throttling, so smooth movement is unconfirmed.
- 20+ lines "Show all", the period lock, the 503 banner, and axe or measured contrast. Contrast was judged by eye only.
- DOC-052 cancel-blocked: my 1-character reason hit the length check first.
- CSV export contents.

**Shared data I changed; other reviewers may notice**
- Completed first-run company setup, with PUNE as the head office, because an employee needs a branch.
- Added branch BLR, which gives two GSTINs.
- Added employee EMP-007.
- Added 7 expense types. Seven types turn Type from Tiles into a Select on every line page, so anyone checking Tiles needs to disable one.
- Added claims 1 to 3 and advances 1 and 2.