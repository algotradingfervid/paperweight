# UI re-review after the review fixes (main 49343403, 3 Nov 2026)

Four crew-ui-reviewer passes, one per area, each on its own copy of the data, at 320, 360, 390, 768 and 1280 px. They covered blockers and highs only, plus spot-checks of the mediums the fix plan addressed.

| Area | Verdict | Blockers | Highs | Mediums spot-checked | New blockers or highs |
|---|---|---|---|---|---|
| Purchase (PO, PR) | PASS | none | 6 of 6 closed | 15: 13 closed, 2 partly | 0 |
| Goods receipt | PASS | none | 5 of 5 closed | 5: 4 closed, 1 partly (M3) | 0 |
| Expense | FAIL | 1 of 1 closed | 5 closed, 1 partly (line page with a photo) | 15: 14 closed, 1 open | 0 |
| App shell and masters | PASS | 1 of 1 closed | 8 of 8 closed (H2 with a leftover) | 11 of 11 closed | 0 |

## Left open, going to the final fix round (RR1a, RR1b)

- **Expense, high, partly fixed.** On the line page with a photo attached, at 360 × 640, Type and Amount sit under the sticky Save line footer (design §5.5 places the form at about 340–410 px).
- **Expense, medium.** Check says Expenses is Complete while a line has no receipt. The submit refusal links to `#step-title` instead of the line.
- **Purchase, medium, partly fixed.** After saving a step with blank required fields, the step bar still marks the step Completed.
- **Purchase, medium, partly fixed.** On the print page at 360, the lines table scrolls inside itself and the Amount column is off-screen, with nothing to show it's there.
- **GRN M3, medium, partly fixed.**
  - On the list at 1280, the state badge breaks inside the word.
  - In both reports at 1280, the vendor column is narrow and breaks inside words, and headers run to 4–5 lines.
- **Lows:**
  - Search shows an empty "Disabled" badge on records that aren't disabled.
  - New user shows the browser's own required-field bubble.
  - The phone drawer's close button scrolls off the screen.
  - At 320, the sort select's text runs under its arrow.
  - After "Add bill details", focus doesn't move and the button stays in the sticky bar.

## Backlog (not blocking)

- **Shell H2 leftover.** After the session expires mid-entry and the user signs in again, master forms and wizard steps that never posted lose what was typed. The finding's stated minimum (a real sign-in page with the reason, returning to the form) is met. Keeping the typed values would need a draft kept in the browser across sign-in. That is a design change, so it wasn't done.
- **Not checked by the reviewers:**
  - real phones, on-screen keyboards and screen readers;
  - order pickers with 7 or more orders;
  - approver and reader views of expense.
