VERDICT: REQUEST CHANGES. BLOCKER 0, HIGH 6, MEDIUM 15, LOW 9.

**`report.md` does not exist.** A hook refused the write ("Subagents should return findings as text"), so the full report is here instead of the one-line reply you asked for.

**How it was run:** 8081 has no company, branch, site or second user, and approval states need a second user. On 8081 I only signed in and read pages, including the empty-state shots. Everything else ran on a private copy of the same binary on port 8094, with its own data folder, now stopped. `app-local version` prints `dev`, so the commit is taken from your brief. Likely files come from reading the code and are not confirmed root causes.

## HIGH

- **Vendor picker, QuickCreate, 390:** pressing New while a search is in flight opens the sheet, then the late answer replaces it. It closes, focus drops to `body`, and the typed Name and City are lost. This is your "sheet left shut" case and it is worse than that: it stays shut for presses up to the moment the answer lands, and survives only for presses after it. §5.8 and §7 require nothing typed to be lost. Likely `internal/ui/components/picker.templ:127` (out-of-band swap on every answer), `combobox.js`. `<shots>/w4-A-typed-390.png`, `<shots>/w4-A-after-390.png`
- **Any wizard step scrolled down, 390:** when offline, the failure banner renders at `top: -354 px`, above the viewport, so Save appears to do nothing. Try again does work once reached. §3.3 and A-28 require a visible banner, so keep it in view and move focus to it. Likely `internal/ui/js/app.js`, `#network` in layout. `<shots>/e-network2-390.png`, `<shots>/e-network2-top-390.png`
- **PO and PR lists, all widths:** there is no Filter control or quick chips. The server filters (`?f=state:in:approved`), but only a hand-written URL reaches it. Purchase doc §5.3 and §5.14, UI v2 §5.12 and A-23 require them. Likely `internal/ui/docscreen/list.go:78` (no `FilterURL`); chips aren't built. It's shared code, so other modules are probably affected, but I checked purchase only. `<shots>/l-rows-390.png`
- **PO wizard, saved draft, 390:** × loses what was typed in the current step with no dialog, then lands on Check inside the wizard. Only a second × reaches the list. §5.6 and §7 require exit in one press with nothing lost. Likely `docscreen/form.go` (`Model.Close` redirects the owner to Check). `<shots>/w13-x-1-390.png`
- **Line page, 390:** Quantity alone saves a nameless line even though Item, UoM and Tax code are starred, and a fully blank save vanishes with no message. Errors appear only at Submit. §5.5 item 6 expects field errors at save. Likely `docscreen/lines.go`. `<shots>/w9-qtyonly-390.png`
- **Document page, all states, 320 and 360:** the timeline splits words ("Approve/d", "Rejecte/d", "Pendin/g approv/al"), and at 320 the amount breaks as `₹90,32,30,910` / `.00`. §5.13 requires a readable word on each dot, so show the current label only or stack vertically. Likely `components/timeline.templ`, `docheader.templ`. `<shots>/d-cancelled-360-fold.png`, `<shots>/d-long-320-fold.png`

## MEDIUM

- **PO step 1, 390:** Save and continue on an empty form creates a draft and marks Details done, despite three starred blanks. Validate at the step, or don't star fields required only at submit. Likely `docscreen/form.go`. `<shots>/w12-step1-422-390.png`
- **PO step 1, all widths:** Order date is empty, but §3.2 says it defaults to today. `<shots>/w1-step1-390-top.png`
- **PO and PR step 1, line page, 390:** Project, UoM, Tax code and Unit are starred and also hinted "Left blank…". A blank Project does save, and the item's unit and tax aren't shown after picking it. Drop the star and show the value. `internal/modules/purchase/fields.go:54,92,101,124,140`. `<shots>/w7-line-new-390-full.png`
- **Line page, 360 and 390:** it keeps the app header, and Save line is a small button at the end with no Cancel. §5.6 requires focus mode, `‹` and the footer. Likely `docscreen/lines.go:281`, `layout/layout.go:176-186`. Same screenshot as above.
- **Vendor and item QuickCreate, 390:** several problems:
  - The vendor sheet has 6 fields (5 required) against §5.8's 3.
  - State isn't taken from a valid GSTIN.
  - Add and choose is below the fold.
  - Focus falls to `body` after a 422, with no summary.
  - Stale errors stay until the next press.
  - Item HSN/SAC has no star but is refused for goods.
  - `<shots>/w5-quick-422-390.png`, `<shots>/w5-quick-gst-390.png`
- **Picker, 390 and 768:** rows lack the monogram, GSTIN and city, and the chosen value lacks the box and tile (§5.8), so similar vendors can't be told apart. `<shots>/t-picker-768.png`, `<shots>/m3-step1-long-390.png`
- **Line page and step 1, 390:** errors surface one at a time, and "Percentage must be…" doesn't name Discount. §5.14 expects all errors listed. `<shots>/w11-line-422-390.png`
- **Empty Items, 390:** there's no `No items yet.`, six ₹0.00 rows repeat the footer total, and Add an item isn't the 56 px dashed tile (§5.11). `<shots>/w6-items-empty-390.png`
- **Check and document page, 109 lines, 390:** no `Show all`, so the pages are about 22,000 px tall and the cards sit below every line. §5.11 and DS §8.2. I took no screenshot; the heights are measured values (22,172 and 22,626 px).
- **Check and document page, 390:** Details repeats the totals, including `IGST ₹0.00`, and the document page repeats the header's key facts. §5.6 gives totals their own card. `<shots>/m1-check-390.png`
- **Revise, 390:** the rejection reason is gone on all four wizard steps. Keep it as a banner until resubmitted. `<shots>/d-revise-390.png`
- **List top, 320 to 390:** about 400 px passes before the first row, the search has no icon or placeholder, and New and Export sit on the page. §5.12 and CD-366 put New in the bottom nav on phones. `<shots>/l-rows-390.png`
- **Success block, 390:** Approve and Mark as sent show a plain banner, and Submit's block has one line. §5.13 requires the block on all three, with a heading plus who it went to. `<shots>/m2-approved-success-390.png`
- **Print, 360:** 110 px of sideways scroll, 21 px unstyled buttons, and a blue bar where a logo would be. `<shots>/p-print-360.png`
- **Actions, 360 and 1280:** at 1280, red Cancel sits above the primary with a stray divider. At 360, an administrator-owner's Withdraw is at the bottom of the page, though a Purchase User gets it in the footer. Likely `docscreen/actions.go:130`. `<shots>/d-sent-1280.png`, `<shots>/d-pending-360-fold.png`

## LOW

- **Home, 360:** no greeting, no second caught-up line, no `Receive goods` quick action, and a dangling " ·" before badges (§5.12). `<shots>/h-ravi-360.png`
- **Picker, 390:** "No vendor matches" shows with nothing typed. `<shots>/w1-picker-empty-390.png`
- **Wizard, all widths:** the page title goes stale after a step change (`Terms · Draft 1` on Items). No screenshot.
- **List, 320:** the chip reads `State: in Approved`, which is developer wording. `<shots>/l-chip-320.png`
- **Wizard, all widths:** console CSP "inline style" errors during step swaps; the source isn't traced. No screenshot.
- **Line card, 390:** the discount isn't shown, so `40 BAG × ₹350.00` next to `₹6,300.00` doesn't add up. It also shows the code `BAG`, not `Bag`. `<shots>/f-items-link-390.png`
- **Wizard head, 390 and 768:** a 24 px strip sits above the head, and at 768 the white bars stop about 80 px short of the right edge. `<shots>/t-picker-768.png`
- **Close page, 390:** the pending warning is an info banner (§5.11 says warning), and focus isn't in the reason field. `<shots>/m2-close-390.png`
- **Copy, 360 and 390:** the reverse-charge hint says "Tick" on a switch, and the PR wizard has no step intro. `<shots>/w6-terms-390.png`, `<shots>/r-new-360.png`

## Checked and fine

- **Layout:** every document state at 320, 360, 390, 768 and 1280 had no overflow and no control under 44 px. Print was the only overflow I found.
- **Accessibility:** axe reported 0 violations at 390 on 12 pages.
- **Input:** live commas and caret are right, and the Today chip and weekday echo work. Real Tab at 390×664 and 320×480 left no field under the footer, and the keyboard picker works.
- **Errors and redraws:** the 409 redraw keeps typing, and focus goes to the summary after a 422 on steps and reason pages.
- **Navigation:** the leave dialog before first save and Back both work.
- **Long values:** a ₹90 crore total and a 100-character vendor wrap cleanly in cards and footers; only the timeline and amount at 320 break (see HIGH).

## Could not reach

- **Devices:** real phones and their keyboards, the native date picker sheet (the calendar button was not opened), and screen-reader listening.
- **Motion and live states:** motion and reduced-motion (A-25, A-32), the 300 ms loading bar, and the `Saving…` state of the save cue.
- **Error paths:** a 409 on the line page, the duplicate request-line 422, and hand-over refusals.
- **Data states:** `Over by` notes, a restricted user, and PR Closed and Cancelled with its held-rows warning.
- **Scope of the walk:** a full walk on 8081, because it has no company.

All 176 screenshots, including those not cited, are in `<shots>/`.