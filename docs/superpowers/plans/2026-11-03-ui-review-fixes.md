# UI review fixes (3 Nov 2026)

Four reviews of main 0a90e96c: `docs/ux/design-audit/review-2026-11-03/{po,grn,exp,shell}.md`. Totals: 2 blockers, 25 high, 54 medium, 39 low (some repeat across reports). Screenshot paths in the reports are scratch files and may not outlive the session. Rulings are in CD-387.

Every blocker and high is fixed. Mediums go in the task that owns their code. Lows are fixed only when they sit in a file the task already touches; the rest stay in the reports as backlog for the owner.

## Wave 1 (parallel, disjoint)
- **R1 Failure states.** `#network` always visible and focused; message matches the failure; Try again keeps values; line pages and single-page master forms post through htmx; a plain POST on expiry or a stale CSRF gets a real page or sign-in with `next` and a reason. Sources: EXP blocker, upload banner, stacked upload errors; PO H2; GRN H2; shell H2, H3, M7. Files: `js/app.js`, `layout/page.templ`, `docscreen/lines*`, `mstkit/web/screens.templ`, `auth/web/guard.go`.
- **R3 User admin.** New user, roles, password reset, unlock, delete in the v2 form pattern, on the existing POST routes; desktop account menu reaches Change password and My sessions, closes on Esc and outside click; sessions name devices. Sources: shell B1, H7, M8. Files: `auth/web/views`, nav menu.
- **R6 Dates and numbers.** Digits format as typed (`05112026` → `05-11-2026`); master date chips fixed and DateInput refuses to draw empty chips; date defaults today (PO Order date, Advance date); `aria-pressed` on chips; a tap selects a prefilled quantity; GRN Rejected is blank when none; ₹ prefix readable. Sources: EXP H, PO M, GRN H1, M10; shell H1, M13, L2. Files: `js/date.js`, `js/field.js`, `components/inputs.templ`, `docscreen/inputs.go`, mstkit.
- **R8 Picker and QuickCreate.** A late search answer never closes or redraws an open sheet, and nothing typed is lost; rows show monogram, GSTIN, city; chosen value box; no "no match" with nothing typed; quick sheet focus, errors, state from GSTIN, three fields. Sources: PO H1, Ms, L; GRN M5, M6; shell L9. Files: `components/picker*`, `quickcreate*`, `js/alpine/combobox.js`, `mstkit/web/quick.go`.
- **R9 Document display.** Timeline never splits a word, no step that didn't happen; money never splits; success block on Approve and Mark as sent; actions order and footer; print viewport, targets and no overflow; amounts align. Sources: PO H6, Ms; GRN M7, M8; EXP Ms. Files: `components/timeline*`, `docheader*`, `success*`, `docscreen/actions.go`, `print`.

## Wave 2 (after wave 1; each merges main first)
- **R2 Landing.** Scroll 0 and the success block first after Submit; document title updates after every htmx move, with `Error:` on a refusal. Sources: EXP H, GRN H3, PO L, EXP M.
- **R4 Home and nav.** `/` is Home per CD-387; sidebar Home; links to expense reports, Import, stuck and overdue approvals; Search focus; drawer focus. Sources: EXP H, shell H4, H6, M10, M11; PO L.
- **R5 Lists.** Filter control and quick chips everywhere; plain chip wording; New and Export per §5.12, list top under the fold line; date column and sort; master rows show identifying data. Sources: PO H3, M; GRN M3, M4; EXP M; shell M5, M6.
- **R7 Wizard flow.** × and finish later per CD-387; blank lines refused; stars per CD-387; Check's Complete reflects the state, including module problems such as an unresolved tax code (a docscreen hook so the section shows `Needs attention`, from R13's review); line page in focus mode with the sticky footer; Check links reach the line; master wizards validate a step, `Continue`, Start over confirms, one exit. Sources: PO H4, H5, Ms; EXP Ms; GRN M1, M9; shell M1–M4.

## Wave 3 (after wave 2)
- **R10 Masters v2 forms.** Section cards, icon tiles, one-line descriptions, examples, hints; Branch State a Select derived from the GSTIN; address defaults; error text matches the screen. Sources: shell H5, H8, M12, M14, M15, L5, L6, L8. Owner: AD-13 or a head worker.
- **R11 GRN.** `Add bill details`; compact Quantities with `{n} of {m} lines entered`. Sources: GRN H4, H5, M2, M11. Owner: head worker (AD-12's session has ended).
- **R12 Expense.** Receipt types match the hint; KeyFacts; line card icons; advance line footer. Sources: EXP Ms. Owner: AD-11.
- **R13 Purchase.** Items empty state, Check and page totals repeat, 109-line `Show all`, rejection reason kept on revise. Sources: PO Ms. Owner: AD-13.
- **R7b Wizard flow, rest (head).** Line page in focus mode with the general sticky footer (CD-387 (6)), replacing R12's advance-line copy; Check links reach the line (line cards get ids; a line past the 20th on a collapsed Check page links through `?all=1`, and R14's `#line-reveal` card keeps one id); purchase reports an unresolved tax code and GRN its submit-only checks through `CheckExtra.Problems`. After R7 merges. Owner: head worker.
- **R14 Show all (head).** docscreen Check and document pages show a long line list in pages with `Show all`, left out of R13. Sources: PO M. Owner: head worker.

## Each task
Standard tier: tests that fail on today's code, `make handin`, `make mutate` plus hand mutants for templ, full `make e2e`. Layout checks loop over every supported width and measure what the change could break. Review by crew-reviewer, and the screen re-driven at the reviewed widths. A MERGE-READY names the APPROVE on its exact commit.

## After wave 3
Re-run the four reviews on the fixed screens (blockers and highs only), then the final gate, then the owner's push say-so.
