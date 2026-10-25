# UX design: <feature name>

Date: <dd-mm-yyyy> · Status: draft | in review | approved by <owner> on <date> · Execution protocol §9 (CD-97)

Fill every section. Write "None" where a section truly does not apply, and say why. Copy is final wording, not placeholders.

## 1. Goal

One paragraph: what the user can do after this feature ships that they could not do before, and how we will know it works (a measurable outcome, for example "a GRN on a phone takes under a minute").

## 2. Who and where

| Person (from `docs/tests/core/00-fixtures.md` or new) | Job in this feature | Device and setting | How often |
|---|---|---|---|

Design for the hardest row first (usually a phone, one hand, poor network).

## 3. Jobs and flow

- The jobs, as "When … I want to … so I can …".
- The main flow as numbered steps from entry point to done, including where the user starts (menu, inbox, link in a banner).
- Alternative and failure paths: no rights, nothing to show, validation errors, a stale version (409), the record changed state underneath (APR stale message), server busy (503).

## 4. Screen inventory

| Screen | URL | Pattern (design-system §8) | States covered |
|---|---|---|---|

States to cover for every screen: normal, empty, loading (htmx indicator), validation error (422 with `ErrorSummary`), no rights / read-only, conflict (409), longest values (`uitest.Long*`), many rows (500 lines, 312 results).

## 5. Screens

One subsection per screen:

- **Phone layout (360 px)** first, as an ordered list of what appears top to bottom, or a simple ASCII sketch. Then what changes at 768 px and 1024 px.
- **Primary action** (exactly one) and secondary actions; what the action bar shows for each right and state.
- **Components** used, by design-system §7 name, with the props that matter.
- **Copy:** heading, labels, hints, button verbs, empty-state text, banners, every error message (design-system §9; kernel wording passed through unchanged).
- **Focus and keyboard:** where focus lands on load, after a 422 and after a step change; what Enter does.

## 6. New components or pattern changes

None, or each proposed change with the reason no existing component works. Approved here, then added to the design-system spec before any task uses it.

## 7. Checklist

Each line answered with how the design meets it (or "n/a" with a reason).

- [ ] One job and one primary button per phone screen (design-system §4.1).
- [ ] Renders fully without JavaScript; htmx and Alpine only enhance (§4.2).
- [ ] Business words: state badges, `₹94,164.00`, `dd-mm-yyyy`, "Draft 42", codes exact (§4.3, §9).
- [ ] Nothing wider than the screen at 320 px, including longest values and error states (§1.3, §4.4).
- [ ] Targets at least 44 × 44 px, 48 px in the phone action bar, 8 px gaps (§4.5).
- [ ] Never colour alone: every status, error and selection also has a word or icon (§4.6).
- [ ] Defaults for everything that can be defaulted; fewest taps for the daily user (§4.7).
- [ ] Complex forms are wizards: 3–5 fixed steps, last step "Check and submit", progress shown (§8.3).
- [ ] Labels always visible; hints before input; errors say what is wrong and how to fix it, next to the field and in the summary (§7 `Field`, `ErrorSummary`).
- [ ] Destructive actions confirm; the expected action (approve) does not (§8.5).
- [ ] Nothing typed is lost on Back, on a 422 or on a 409.
- [ ] Visibility of system status: saving, saved, submitted, who it went to (Nielsen 1).
- [ ] Recognition over recall: pickers show code, name and one distinguishing line (Nielsen 6, §7 `Picker`).
- [ ] WCAG 2.2 AA: contrast from tokens only, focus visible and not hidden under sticky bars, accessible names, error identification (§5, §10).
- [ ] Speed: every step answers in under 400 ms or says it is working and what happened (§4.8).
- [ ] The system works things out instead of asking (GST split, defaults, amounts in words) (§4.9, §4.12).
- [ ] The next step's choices only; the rest behind "More" or a disclosure; the frequent action is the biggest and nearest the thumb (§4.12).
- [ ] Consistent with existing screens: same pattern for the same job anywhere in the app. (§4.11)

## 8. Acceptance checks

Testable statements that become test cases (and later Go test names), for example "At 360 px, a GRN with 64 lines shows the first 20 cards and 'Show all 64 lines'."

## 9. Open questions

Each with who decides. The design is not approved while any question that changes a screen is open.
