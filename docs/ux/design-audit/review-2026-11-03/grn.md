# GRN UI review (uiv2-review-grn)

VERDICT: REQUEST CHANGES

Counts: BLOCKER 0, HIGH 5, MEDIUM 11, LOW 10. Reviewed against main 0a90e96c. The flow works from end to end and no width scrolls sideways, but five issues would hurt a storekeeper or the office in normal use.

**How this was run.** I did not use http://localhost:8081 for the findings. Its data was empty: no company, party, item, site or PO. Finishing first-run setup there would have changed a setting shared with three other reviewers, and the combobox needs 50+ POs. So I copied its database and ran the same binary (`app-local`, vcs.revision 0a90e96c, not modified) on port 8092 from my own scratchpad, and stopped it afterwards. 8081 was only read, apart from the sign-in. All data was made through the app, with one exception: I set `default_tax_code_id` by SQL on the private copy's items, after my first seeding missed it. Browser: Chrome through Playwright, touch emulation below 1024 px, 664 px tall on phones.

## HIGH

### H1. Tapping a prefilled quantity and typing adds to the old value, so 10 becomes 100
- Screen: Quantities, `Rejected` and `Received now`. Width: 360 (tested); the field is the same at every phone width.
- Screenshot: <shots>/s3-reject-open-360.png (shows the field; the wrong value is a measured result, below).
- What's wrong: `Rejected` is prefilled with `0`, so its placeholder `e.g. 10` never shows. The numbers are right-aligned and tapping does not select them. Tapping the left third or the centre of the field put the caret at position 0, and typing `10` gave **100**. Only a tap on the digit itself gave 10. With 1,200 received, 100 is under the limit, so nothing refuses it and accepted drops by 90 with no message. `Received now` has the same caret behaviour (caret 0 after a tap on `8`). This is step 4 of the design's main flow ("types 10"), done with gloves.
- Expected: `Rejected` is blank when there is no reject (design §5.4 lists the placeholder `e.g. 10`), and tapping a prefilled quantity selects the whole value, so the first digit replaces it.
- Likely file: `internal/modules/goodsreceipt/web/module_s2.go` (rejected value), `internal/ui/js/field.js` (select on focus).
- Caveat: measured in emulated touch Chrome. iOS and Android place a tapped caret slightly differently, so confirm on a real phone. The blank field is needed either way.

### H2. A failed save on a long step shows nothing where the user is looking
- Screen: Quantities, with Save and continue failing on the network. Width: 360; the banner is in page flow at every width, but only long pages put it off screen.
- Screenshot: <shots>/s3-offline-scrolled-360.png
- What's wrong: with the user at line 5 (scrollY 1503) and the POST failing, nothing visible changes. The `#network` banner with `Try again` sits 1,864 px above the viewport, focus falls to `body`, and the button just returns to normal. The storekeeper on poor data sees a Save that did nothing and will tap again or walk off. A-28 is met only at the top of the page. Typed values do survive, and `Try again` worked.
- Expected: the design's own case, weak data at a gate. Make the banner visible wherever the user is (sticky under the wizard head, or directly above the footer buttons), or move focus to it.
- Likely file: `internal/ui/js/app.js` (network banner), `internal/ui/layout/page.templ`, `css/components/` for its position.

### H3. After Submit the page opens scrolled down, past the success block
- Screen: document page right after Submit. Widths: 390 (scrollY 360) and 1280 (scrollY 406). Also seen after PO Submit (PO 14, scrollY 360), so this is docscreen-wide.
- Screenshot: <shots>/s5-after-submit-vp-390.png
- What's wrong: the page lands with the h1 just under the header, and `Approved. GR/26-27/… has been numbered.` plus `New goods receipt` are above the fold. Opening the same URL later gives scrollY 0, so the flash visit is what moves it. A reload keeps the offset. A storekeeper who wants to start the next truck has to know to scroll up. The numbered result is the moment A-25 and §5.13 item 2 design for.
- Expected: scrollY 0 after Submit, with the success block first on screen (design §5.6: no autofocus, flash first in the content).
- Likely file: not found. My patched `focus`, `scrollIntoView` and `scrollTo` logged no call, so it is native; `autofocus` in the page is only on the closed New sheet's first tile. Start in `internal/ui/docscreen/page.templ` and `components/success.templ`. Needs a browser test.

### H4. `Add bill details` was never built
- Screen: document page, approved, no bill. Widths: all.
- Screenshot: <shots>/doc-approved-vp-1280.png, <shots>/doc-approved-360.png
- What's wrong: design §3.5, §5.6 and §5.7 give this state the bar primary `Add bill details` (G5), jumping to `#bill`. The text appears nowhere in the code except an error message, and `module_s3.go` sets no `Primary`. On a phone the Bill details card starts well over a screen down a page 4,400 px tall (5 lines) with no sticky bar at all. At 1280 the Actions card's only filled button is the red `Cancel`, so the strongest action on a healthy receipt is the destructive one. This is the "late bill" job.
- Expected: `PageExtra.Primary` = `Add bill details`, Href `#bill`, for `write` + approved + no bill.
- Likely file: `internal/modules/goodsreceipt/web/module_s3.go`.

### H5. Quantities is too tall for a gate: 1.5 lines per screen, no sense of progress
- Screen: Quantities. Widths: 360 and 390 at 664 px tall.
- Screenshot: <shots>/s3-qty-360.png, <shots>/s3-keyboard-open-360.png
- What's wrong: a closed card is 331 px; 610 px with `Some rejected` open. The usable area is about 536 px (head 56, footer 72), so one and a half lines show. Five lines run to about 2,200 px, three and a half screens. A 36-line order is 12,605 px, **19 screens**, with no count, no "line 12 of 36", no find-a-line. The reserved error line leaves a 40 px gap between each input and its `Some rejected` box, and the box is a full bordered 56 px control on every card although most lines have no reject. The design's stated aim is the lines he counts on one screen. Everything on a card is correct, it's the density. The sticky Save does stay reachable, and on a short viewport the focused input stays clear of the footer (360 × 350, field bottom 195, footer top 278).
- Expected: a compact card: item and hint as one block, input, and `Some rejected` as a quiet text button on the row or in the card corner, with no reserved line until there's an error. The sticky footer's summary row (§5.6) showing `{n} of {m} lines entered`. For long orders `Find a line`, as §5.11 gives the PO.
- Likely file: `internal/modules/goodsreceipt/web/s2.templ`, `internal/ui/css/components/field.css` (CD-287 reserved line). A head ruling is needed if the reserved line is to go here.

## MEDIUM

### M1. Check says `Complete` on a Delivery step with nothing filled in
- Screen: Check. Width: 360.
- Screenshot: <shots>/s3b-next-360.png, <shots>/s3-submit-fail-360.png
- What's wrong: with no document kind, no number and a reject with no reason, every section shows green `Complete` and no warning. Submit then throws the user back to Delivery. After fixing that, Submit throws them to Quantities for the reason: two bounces, each showing only its own step's errors, though both were known at the first Submit.
- Expected: `Needs attention` on a section whose submit requirement fails, before Submit (design system §8.3, UI v2 §5.6), ideally listing every missing item once.
- Likely file: `internal/ui/docscreen/form.go` `checkStep`.

### M2. Quantity cards touch each other
- Screen: Quantities. Widths: all.
- Screenshot: <shots>/s3-qty-360.png, <shots>/s3-focus-1280.png
- What's wrong: cards are measured at 621/621, 952/952: zero gap. The summary line sits on the buttons and the buttons sit on the first card. `itemsView` has no stack wrapper, which also makes this the one screen in the wizard without card spacing. Inside `Fully received lines` the same applies.
- Expected: the card stack gap used on Order (design §1 asks 8 px minimum).
- Likely file: `internal/modules/goodsreceipt/web/s2.templ` `itemsView`.

### M3. Desktop list and report columns are badly proportioned
- Screen: GRN list, Bills to enter, Received items. Widths: 768, 1280.
- Screenshot: <shots>/list-rows-1280.png, <shots>/rpt-bills-1280.png
- What's wrong: Vendor gets about 110 px and breaks inside words (`SHREESTEELT / RADERS`, `Venkatesw / ara`, 13 lines for a long name) while 200 px or more stays empty on the right. PO numbers break as `PO/26- / 27/00002`. Headers sit 8 to 15 px left of their cells. The `Receipt date, PO no. and challan no.` header takes five lines. Quick-create's generated codes (`BHARATTOOLSANDHARDWA`) make this common, not an edge case.
- Expected: flexible vendor column, numbers never broken, headers aligned to cells (§5.12).
- Likely file: `internal/ui/components/datalist.templ`, `css/components/` datalist rules. Check the PO list in the same pass.

### M4. GRN list misses the v2 list pattern
- Screen: GRN list. Widths: 320 to 390.
- Screenshot: <shots>/list-rows-360.png
- What's wrong: no search icon or placeholder, no quick-filter chips (`Drafts`, `Approved`), no sort select although design §5.1 lists one, and the PO number is missing from cards and rows. A second blue `New goods receipt` sits above the bottom nav's `New`, and `Export` sits in the page on phones rather than in the Filters sheet.
- Expected: UI v2 §5.12 and `v2-mocks/list.html`; GRN design §5.1 row 3 `date · PO · state`.
- Likely file: `internal/modules/goodsreceipt/fields.go` (list and filter declarations), `internal/ui/docscreen/list.go`.

### M5. The 7-to-49 Select hides what identifies the order
- Screen: Order step with 7+ POs. Widths: 320, 360.
- Screenshot: <shots>/s1-select-chosen-320.png, <shots>/s1-widened-360.png
- What's wrong: the chosen option shows `PO/26-27/00002 · Bharat Too`. Site and pending are gone and the vendor is cut, on a screen whose acceptance check is PO number and vendor on screen when choosing (§1). The empty option is blank, so the unopened field looks broken. This is beyond the known chevron overlap: the problem is the lost information and the blank prompt.
- Expected: a `Choose an order` prompt, and the chosen order's vendor, site and pending echoed under the select.
- Likely file: `internal/modules/goodsreceipt/web/s1.templ`.

### M6. Combobox chosen state is bare
- Screen: Order step with 50+ POs. Widths: 320, 360, 1280.
- Screenshot: <shots>/s1-combo-chosen-360.png, <shots>/s1-combo-q-360.png
- What's wrong: the choice collapses to plain `PO/26-27/00020 Shree Steel Traders` with `Change`. The site and pending shown in the results vanish, with no bordered box and no tile (§5.8 Chosen). Two orders for one vendor at different sites can't be told apart at the moment of saving. The results have no count line. On phones the list stays anchored under the field, as a dropdown 576 px tall.
- Expected: §5.8: chosen summary with secondary line; `{n} orders match`.
- Likely file: `internal/ui/components/picker.templ`, `internal/modules/goodsreceipt/web/search_s1b.go`.

### M7. Status timeline shows a step that never happened
- Screen: document page. Widths: all.
- Screenshot: <shots>/doc-approved-vp-1280.png, <shots>/doc-cancelled-360.png
- What's wrong: `Draft → Pending approval → Approved`, with `Pending approval` ticked and dated, on a receipt that no rule ever sent for approval. §5.13 item 4 gives GRN `Draft → Submitted`.
- Expected: the GRN's own steps. Also state the choice of word: the kernel says Approved, the design says Submitted.
- Likely file: `internal/ui/docscreen` timeline builder, `internal/modules/goodsreceipt/web/module.go`.

### M8. Large amounts split in the middle of the number
- Screen: document page header. Widths: 320, 360, 390.
- Screenshot: <shots>/doc-long-320-crop1.png
- What's wrong: `₹1,24,43,14,3 / 36.88`: the 32 px amount wraps at a digit, at all three phone widths (height 70 px). Shared with every module's header.
- Expected: amount stays on one line; step the size down for long values (lessons: every supported width with a crore amount).
- Likely file: `internal/ui/components/docheader.templ`, its CSS.

### M9. `Save and finish later` doesn't finish
- Screen: Order step on a saved draft. Width: 390.
- Screenshot: none needed; reproduced as draft 10 at `/edit/details`.
- What's wrong: it lands on `/edit/check`, step 4 of the same wizard, with no "saved" message. The user asked to leave and is deeper in. The × does go to the list.
- Expected: back to the list or home with `Saved. Draft 10 is in your drafts.` (§5.14 toast).
- Likely file: `internal/ui/docscreen/form.go`, `internal/ui/wizard/frame.templ`.

### M10. Date field misses three v2 details
- Screen: Order, Delivery and bill dates. Widths: all.
- Screenshot: <shots>/s1-radios-360.png, <shots>/mock-grn-choose.png
- What's wrong: `Today` has no `aria-pressed` or chosen look when it matches the value. The echo has no check icon and no `· the day the goods arrived`. The hidden date input has no `max`, so the calendar offers future dates that are then refused with `Receipt date can't be in the future.`
- Expected: UI v2 §5.7 and the mock.
- Likely file: `internal/ui/components/inputs.templ`, `internal/ui/js/date.js`, `s1.templ` (bounds).

### M11. Line cards read messily on phones
- Screen: Check and document Lines. Widths: 320 to 390.
- Screenshot: <shots>/s4-check-360.png, <shots>/doc-long-320.png
- What's wrong: `Received 1,200 KGS · Accepted 1,200 KGS` is right-aligned and wraps with the unit alone on the next line, while `Rejected 5 KGS: Rusted coils` is left-aligned under it. Long item names are squeezed into a narrow column by the amount. The two numbers that matter are the hardest thing to read on the card.
- Expected: Received and Accepted left-aligned on their own lines or in a two-column row, same edge as Rejected.
- Likely file: `internal/ui/components/lines.go`, line card CSS; `LineCard` in the GRN module.

## LOW

- L1. Quantities 422, 360: a line over twice ordered shows the error and a warning under the same field saying almost the same thing. Show only the error. <shots>/s3-errors-360.png, `s2.templ`.
- L2. Lists and reports, all widths: a dangling `·` after the last item (`03-11-2026 ·`, `Rejected 0 KGS ·`, `DC-77 ·`). <shots>/list-rows-360.png, <shots>/rpt-items-320.png, DataList separator.
- L3. Wizard head, 320 and 360: `New goods…` is cut because `Not saved yet` takes the room. The mock's title is `Receive goods`. <shots>/s1-radios-320.png, `internal/ui/wizard/frame.templ`.
- L4. Quantities: `Receive all pending` still shows on an order with nothing pending. <shots>/s3-pending-zero-360.png, `module_s2.go`.
- L5. Quantities: every card marks `Received now*` as required, yet blank is allowed and means "didn't come". Per CD-341 as written, but it misleads; raise with the head. <shots>/s3-cleared-360.png.
- L6. Copy: `As printed on the document. Type Not recorded if it's lost.` reads as a typo without quotes. Bill date hint `For example, 27-03-2026` differs from the challan date's hint. `Vendor invoice no.` has no placeholder. <shots>/s2-err-360.png.
- L7. Details card: `Accepted value` alone is right-aligned among left-aligned values. <shots>/doc-long-320-crop1.png.
- L8. Order radios against the mock: bars are blue rather than the mock's amber, `Show fully received orders` is black with no eye icon, and bar plus text wrap at 360. <shots>/s1-radios-360.png, <shots>/mock-grn-choose.png.
- L9. Print: on-screen `Print / Save as PDF` and `Close` are 21 px tall with no page margin, and the logo prints as a plain blue bar (the `logo-print.png` asset). The A4 output itself is clean, one page, and a cancelled receipt prints `CANCELLED`. <shots>/print-360.png, <shots>/print-grn-1-1.png.
- L10. Console: each htmx swap logs a CSP `Applying inline style` refusal from `htmx.*.js`. No visible effect seen; worth clearing so real errors stand out.

## Checked and fine

No horizontal overflow on any screen at 320, 360, 390, 768, 1280. No control under 44 px apart from L9. Contrast sampled on Check at 360: nothing under 4.5:1. Live Indian commas and caret (`12,34,567.555`, `19,50,000`), letters and minus stripped. Whole units use the numeric keypad; a typed point is dropped, so a pasted `10.5` becomes `105`. Enter presses Save and continue, never `Receive all pending`. Tab order and focus rings on Quantities. Focus to the summary after 422, and summary links reach the field, and a line error redraws its disclosure open. Over-delivery, twice-ordered, rejected-over-received, reason and no-lines texts match the design. 409 keeps typed values and shows `Reload Draft 9`. Part receipts, `Fully received lines (3)`, `Clear all`, service-only order, tick, tax invoice prefill, bill 422, save, duplicate warning, cancel with bill warning, amend with bill copied, reports with chip, filter page, upload with rear camera and refused file. Radios, Select and combobox thresholds all work.

## Could not reach

- Selected radio row against the mock: seeding passed 6 POs before I took that shot. The CSS shows no selected-row rule beyond the radio itself, but that isn't seen on screen.
- One visible PO preselected, for the same reason.
- Read-only draft, and any user without full rights: the brief excludes changing users.
- Pending state, period lock, closed-PO race, stale bill 409, 20 photos, 500 lines (36 was the longest), CSV contents.
- PO `To close` queue after the tick (the tick saved; the purchase side was not opened).
- Real phone, real on-screen keyboard, real camera and screen reader. The keyboard was approximated with a 350 px viewport.
- Time to complete on a phone against the 43 s budget.
