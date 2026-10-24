# Design System and Front-End Spec

Date: 24 October 2026 · Status: approved for planning · Owner: WP1-UI (`internal/ui/`)

Builds on: `docs/specs/2026-10-24-core-platform-spec.md` (§2 stack, §3.2 lifecycle, §6 screens). Evidence: `docs/research/ux-patterns.md` (interaction patterns, 30 cited recommendations) and `docs/research/frontend-tech.md` (CSS, scripts, tokens, overflow test; versions checked 24 Oct 2026). Where this spec says "see research §n", the research file holds the detail and the citations; this spec holds the decision.

## 1. Product rules (from the product owner, not negotiable)

1. **Responsive and mobile-first.** Base styles target a 320 px phone; larger layouts are added with `min-width` media or container queries only.
2. **Light mode only.** No dark theme, no `prefers-color-scheme` branch. `color-scheme: light` on `:root` and `<meta name="color-scheme" content="light">`, so form controls, scrollbars and browser auto-dark features stay light.
3. **No horizontal scrolling anywhere**, at any width from 320 px up, on any screen, dialog, wizard step, list, line grid or error state. Stricter than WCAG 1.4.10: no table exception. The only exempt surface is the paper print layout (`/print/*`), whose on-screen preview still must not scroll sideways.
4. **Complex forms are wizards with a clear progress bar.**
5. Open-source only, latest stable versions, prefer well-maintained libraries.

## 2. Who uses it and where

| Person (fixtures) | Main work | Device |
|---|---|---|
| Priya (Purchase User) | Raises POs daily | Desktop, sometimes phone |
| Ravi, Meena (approvers) | Approve POs and expenses | Phone first |
| Suresh (Stores) | Receives goods on site (GRN), maybe with gloves | Phone |
| Deepak and every employee | Expense claims with receipt photos | Phone |
| Anil, Kavya (admin, masters) | Users, rights, masters, imports | Desktop |

Design consequence: approval, GRN and expense flows are designed on a 360 px phone first; PO entry and admin screens must also be fast on desktop.

## 3. Front-end stack

| Concern | Decision | Version | Licence |
|---|---|---|---|
| Templates | templ components in `internal/ui`; **no templ `css` or `script` blocks** (they emit inline CSS/JS the CSP blocks) | a-h/templ v0.3.1070 | MIT |
| Interactivity | htmx, configured by `<meta name="htmx-config">` (research §2.2): `allowEval:false`, `allowScriptTags:false`, `includeIndicatorStyles:false`, `selfRequestsOnly:true`, `historyCacheSize:0`, `refreshOnHistoryMiss:true`, `defaultFocusScroll:true`, 422 swapped as non-error | htmx.org **2.0.11** (npm `latest`; 4.0.0 is on the `next` tag and has had no patch release, so it is deferred; config above already matches htmx 4 behaviour) | 0BSD |
| Local state | Alpine.js **CSP build**; every component registered with `Alpine.data` in `app.js`; no arrow functions, template literals or globals in attributes | @alpinejs/csp 3.17.5 | MIT |
| Own JS | One `app.js` (< 3 KB gzip target): event delegation for dialogs, sheets, toasts, upload progress, Alpine registrations | n/a | n/a |
| CSS | Hand-written CSS with tokens, `@layer`, container queries, a closed set of component classes; bundled and minified by esbuild's Go API from `go generate`; content-hashed, embedded, `Cache-Control: immutable` | evanw/esbuild v0.28.2 (build-time Go module) | MIT |
| Icons | Lucide, only the icons listed in `internal/ui/icons/icons.txt`, generated into templ components emitting inline `<svg aria-hidden="true">` | lucide-static 1.61.0 (vendored SVGs) | ISC |
| UI font | Inter variable, self-hosted subset (Latin-1, punctuation, ₹, arrows; features `kern liga calt tnum case zero`), 41.2 KB, all weights; preloaded | Inter 4.1 | OFL-1.1 |
| Devanagari | Noto Sans Devanagari variable subset, loaded only via `unicode-range` when such text is present | 2.007 | OFL-1.1 |
| Browser tests | chromedp generic API (`chromedp.Do`, `chromedp.Run`, `Evaluate[T]`, `Events`) | v0.21.0 | MIT |
| Accessibility engine (test only) | axe-core injected by chromedp | latest at execution | MPL-2.0 |

No Node.js at build or run time. Font subsetting (`pyftsubset`) is a one-off developer step; the woff2 output is committed with its licence.

**CSP.** Start with the WP0 policy. Target `style-src 'self'` (dropping `'unsafe-inline'`) once the e2e CSP check (§10) passes; the lint rules in §9 make that possible.

**Supported browsers.** Last two years of Chrome/Edge, Safari 16.4+, Firefox 115+ (esbuild engines Chrome 111 / Safari 16.4 / Firefox 115). Everything used is Baseline widely available: `@layer`, container queries, `:has()`, nesting (lowered by esbuild), `dvh`, `inert`, `<dialog>`, `<details>`.

## 4. Principles

1. **One thing per screen on a phone.** A screen has one job and one primary button.
2. **Server is the truth.** Every state lives in the database; every screen is a URL that renders fully without JavaScript. htmx and Alpine only enhance.
3. **Show the business words.** State badges say the state ("Pending approval"), errors say what to do, amounts show ₹ with Indian grouping, codes are shown exactly.
4. **Nothing wider than the screen.** Content wraps, stacks or moves to a detail page. Never clip, never scroll sideways.
5. **Big, obvious targets.** At least 44 × 44 px with 8 px gaps; 48 px for the phone action bar.
6. **Never colour alone.** Every status, error and selection also has a word, an icon or a shape.
7. **Fast for daily users.** Defaults for everything that can be defaulted; Enter submits a step; the check page doubles as the whole-form view.

## 5. Tokens

`internal/ui/css/tokens.css` is created from `docs/research/frontend-tech.md` §9 **verbatim** and is the only file allowed raw colours or raw lengths. Summary:

| Group | Values |
|---|---|
| Surfaces | canvas `#F5F6F8` (page), surface `#FFFFFF` (cards, inputs), sunken `#EDEFF2` (wells, list header, read-only) |
| Text | text `#141A23` (17.5:1), secondary `#46505E` (8.2:1), muted `#5E6878` (5.6:1, lightest readable text), disabled `#8A93A1` (disabled only) |
| Lines | border `#7C8696` (input edges, 3.7:1), border-subtle `#DCE0E6` (decorative dividers only) |
| Primary | 50 `#EEF3FF`, 600 `#1F4FD1` (buttons, links, focus, 6.8:1), 700 `#1A3FA8` hover, 800 `#16348A` pressed / text on tint |
| Status | success / warning / danger / info, each with 50 tint, 600 icon/border (≥ 3:1), 700 text/solid fill (≥ 4.5:1), 800 text on tint (≥ 7.7:1) |
| Type | Inter; 13 / 14 / **16 (body and every input)** / 18 / 20 / 24 / 30 px; headings step up once at 48rem; line height 1.5 body, 1.25 headings |
| Space | 4 px grid, `--space-1` 4 px … `--space-16` 64 px; gutter 16 px, 24 px from 48rem; form max 40rem; content max 72rem |
| Size | `--target-min` 44 px, `--target-primary` 48 px, `--control-h` 44 px, header 56 px, action bar 72 px |
| Radius | 4 (badge), 8 (input, button), 12 (card), 16 (sheet, dialog), pill |
| Focus | 2 px solid `#1F4FD1` outline, 2 px offset (≥ 5.9:1 on every surface) |
| Motion | 120 / 200 / 300 ms; all near zero under `prefers-reduced-motion: reduce` |
| Breakpoints | constants 30rem (480), 48rem (768), 64rem (1024), 80rem (1280); components prefer container queries |
| Z-index | sticky 100, header 200, dropdown 300, overlay 400, toast 500; `<dialog>` uses the top layer |

Every contrast pair is computed in research §9 and must stay passing: text ≥ 4.5:1, UI edges, icons and focus ≥ 3:1 on all three surfaces.

**State badge mapping** (word + tint + icon, never colour alone):

| State | Tint | Icon (Lucide) |
|---|---|---|
| Draft | sunken / text-secondary | `pencil` |
| Pending approval | warning-50 / warning-800 | `clock` |
| Approved | success-50 / success-800 | `check-circle` |
| Rejected | danger-50 / danger-800 | `x-circle` |
| Cancelled | sunken / text-secondary, with "Cancelled" word | `ban` |
| Closed | primary-50 / primary-800 | `lock` |

Negative money shows a minus sign, never red alone.

## 6. Layout

**App shell.**
- **Phone (< 48rem):** top header (56 px: menu button, page title, inbox icon with count), content, and on screens with actions a sticky bottom **action bar** (primary + at most one secondary). Navigation opens as a full-height sheet from the menu button. No bottom tab bar: the module list will grow.
- **Desktop (≥ 64rem):** left sidebar navigation (collapsible), header with search and user menu, content up to 72rem. Document pages get a right side panel (state, approval history, attachments, audit) from 80rem.
- `html { scroll-padding-block: header + 8px, action bar + 8px }` so the focused field is never under a sticky bar (WCAG 2.4.11).
- Viewport meta: `width=device-width, initial-scale=1, viewport-fit=cover, interactive-widget=resizes-content`. Never `maximum-scale` or `user-scalable=no`. Safe-area padding on header, action bar and sheets.

**Layout primitives** (classes in `layout.css`): `.stack` (vertical rhythm), `.cluster` (wrapping row), `.grid` (`repeat(auto-fit, minmax(min(100%, 16rem), 1fr))`), `.sidebar-layout`, `.actionbar`. All children get `min-inline-size: 0`.

**Forms.** Single column, max 40rem. Two fields per row only for tight pairs (Qty + UoM, Rate + Tax code) and only when the container is ≥ 30rem.

## 7. Components

All components are templ functions in `internal/ui/components/`. Screens compose these; modules never write their own CSS or raw markup for these concerns. JS column: None = HTML+CSS; htmx; app.js; Alpine (CSP build).

| Component | Purpose and rules | JS |
|---|---|---|
| `Button` (primary, secondary, ghost, danger) | Min 44 px; one primary per screen; danger only for destructive actions, which also need a confirm step; `hx-disabled-elt` on submit to stop double submits; label changes to "Saving…" | None |
| `Field` (label, hint, error, input) | Label always visible above the input, no placeholder-as-label; "(optional)" marks optional fields; error text under the field with icon, `aria-invalid`, `aria-describedby`, 2 px danger border | None |
| `MoneyInput`, `QtyInput`, `RateInput`, `PercentInput` | `type="text" inputmode="decimal"` (`numeric` for whole-number UoM); never `type="number"`; Indian grouping on blur; UoM / % as a suffix outside the input; more decimals than allowed is an error, never silent rounding (NUM rules) | app.js (blur formatting) |
| `DateInput` | Text input `dd-mm-yyyy` with `inputmode="numeric"`, default today, "Today" and "Yesterday" buttons, parsed date echoed with weekday ("Friday, 27 March 2026") | app.js |
| `CodeInput` (GSTIN, PAN, HSN, pincode) | `autocapitalize="characters"`, `spellcheck="false"`; validates on reaching length; on a valid GSTIN shows derived state and PAN under the field | htmx (`/validate/gstin`) |
| `Picker` (party, item, project, site, employee, PO) | ARIA 1.2 combobox; server search on code + name + GSTIN, 250 ms debounce, max 20 rows; each row shows code, name and one distinguishing line; chosen value shows as a summary with "Change"; full-height sheet on phones; respects rights scope and restrictions; preselects when exactly one value is allowed; `<select>` fallback for lists under 50 | htmx + Alpine |
| `Radios`, `Checkboxes` | Option sets up to about 6 are radios, not dropdowns; whole row is the target | None |
| `FileUpload` | Two buttons: "Take photo" (`accept="image/*" capture="environment"`) and "Choose file" (`accept="image/*,application/pdf" multiple`); limits stated before upload ("JPG, PNG, HEIC or PDF, up to 10 MB"); progress via `htmx:xhr:progress`; thumbnail or file chip with "Remove" | htmx + app.js |
| `Stepper` (wizard progress) | §8.3 | None |
| `ActionBar` | Sticky bottom bar on phones; inline footer on desktop; only actions the user's rights and the document state allow (spec R4) | None |
| `StateBadge` | §5 mapping | None |
| `Card`, `SummaryList` | Label/value pairs (`<dl>`); "Change" link per section on check pages | None |
| `DataList` | §8.1: one markup, cards on phones, aligned rows from 48rem via container query; never a `<table>` | None (htmx for paging, filters) |
| `LineList` | §8.2 | None / Alpine on desktop grid |
| `ErrorSummary` | "There is a problem" box at the top of a failed step, each error a link to its field, focused on render; page title prefixed "Error: " | None |
| `Banner` (success, info, warning) | Success banner above the `h1` of the next page after an action, gone on next navigation; errors are never toasts | None |
| `Toast` | Only for low-stakes background confirmations ("Saved"); the server sends an `HX-Trigger` `notice` event (CD-75) or a flash (CD-80); `app.js` shows success/info in a `role="status"` region and auto-dismisses after 5 s (paused on hover/focus); errors and warnings go to the persistent `#alerts` region | htmx + app.js |
| `Dialog`, `Sheet` | Native `<dialog>` with `showModal()`; sheet is the same element styled to slide from the bottom on phones, centred dialog from 48rem; explicit close button; used for pickers and short confirms only, never for reject reasons or line editing | app.js |
| `Disclosure` | `<details><summary>` for history, audit, low-priority fields | None |
| `Pagination` | "Previous" / "Next" plus "1–25 of 312"; page size 25 (D-NUM-21); no infinite scroll | None |
| `FilterBar` | Search box; "Filter" opens a filter page or sheet on phones, side panel on desktop; active filters as removable chips with "Clear all"; filters are a GET form with `hx-push-url`, so filtered URLs are shareable | htmx |
| `AmountInWords` | Under large totals on check and approval pages ("Rupees Ninety Four Thousand One Hundred Sixty Four Only") using `print.InWordsINR` | None |
| `EmptyState` | Message plus the one useful action ("No purchase orders yet. New purchase order") | None |
| `Icon` | Generated Lucide components; icon-only buttons need `aria-label` | None |

## 8. Screen patterns

### 8.1 Lists (every DocType)

- Each DocType declares list fields with a priority: 1 title (number or "Draft 42"), 2 right-aligned figure (amount), 3 state, 4+ metadata.
- **Phone card:** row 1 title + amount (tabular figures, right); row 2 party or employee; row 3 date · project/site · state badge. The whole card is one link.
- **From 48rem (container):** the same `<li>`s become aligned rows with a header row (`aria-hidden`); every column track is `minmax(0, …)`; low-priority fields (`data-priority="low"`) hidden below 64rem. If declared columns can't fit, it stays as cards.
- Top: search, Filter, sort as one select ("Newest first", "Amount high to low"), result count ("312 purchase orders"), "New …" for users with `create`, Export for users with `export`. Bulk actions only from 64rem, later.

### 8.2 Line items (PO, GRN, expense lines; up to 500)

- **Phone: add-to-a-list.** Each line is a card: line number, item name and code, "1,200 kg × ₹62.5000", amount on the right, tax code; actions "Change" and "Remove". Below: running taxable total and "Add an item". Add/Change opens a **line page** (its own URL: item picker, qty + UoM, rate, tax code, optional project/site) with "Save line" returning to the list. Remove asks on its own small confirm ("Remove line 3 (MS Angle 50x50x5)?").
- **From 64rem: fitting grid.** Columns #, Item, Qty, UoM, Rate, Tax, Amount, actions, all shrinking; description wraps; detail (description, HSN, project, site) in an expandable row or the line page. Alpine handles keyboard entry (Enter/Tab between cells, add/remove row); each changed row posts with htmx and the server returns recalculated totals and GST split.
- More than 20 lines on a phone: first 20 cards, "Show all 64 lines", and "Find a line".
- Reorder with "Move up" / "Move down", never drag.

### 8.3 Wizards

**Which forms are wizards:** any form with more than one logical section or any line items: PO, GRN, expense claim, party (with addresses), employee, CSV import. **Single-page forms:** sign in, change password, simple masters (UoM, tax code, expense type, project, site, payment terms), approval rule, numbering settings, reject reason, cancel/close reason.

**Shape.** 3 to 5 **fixed** steps (no steps that appear conditionally; a step with nothing to do says "Nothing needed here"). The last step is always **Check and submit**.

**Progress bar (`Stepper`).**
- "Step 2 of 4" text and the step name as the page heading, plus a segmented bar, always visible at every width.
- Markup is an `<ol>` of steps with `aria-current="step"`, hidden status text ("Completed: "), and a `<progress max=N value=n>`; no inline style.
- Done steps are links; the current step is bold-outlined; future steps (beyond the furthest reached) are not links.
- On phones only the current step's label shows; others become segments. Labels under all segments from 30rem.
- Never a bare percentage.

**Saving and navigation.**
- **Documents** save at every step. **Master create wizards** (party, employee) keep step values in the server-side session and save once at "Check and save" (CD-50).
- Step 1 "Save and continue" on a new document creates the `draft` ("Draft 42") — allowed with zero lines (spec R8).
- Each step is its own URL (`/{doctype}/{id}/edit/{step}`), rendered fully without `HX-Request` and as `#wizard` with it (`Vary: HX-Request`).
- Each step form POSTs with the document `version`. Success: 200 with the next step and `HX-Push-Url` **response header** (no-JS: 303 redirect). Validation failure: **422** re-rendering the step from the posted values with `ErrorSummary` focused. Version conflict: 409 with DOC-069's message and a reload link, keeping the typed values visible.
- **Back** is a submit button (`name="nav" value="back"`) that saves without validating, so nothing typed is lost; the browser Back button does a full GET (`historyCacheSize:0`, `refreshOnHistoryMiss:true`) and shows the saved draft.
- "Save and finish later" text link under the action bar on every step of a document wizard (hidden in master create wizards, CD-81).
- Focus moves to the step heading (`autofocus`, `tabindex="-1"`) after each step change, or to the error summary after a 422.
- Enter in a field submits "Save and continue".

**Check and submit.**
- Summary card per step with a "Change" link that opens that step; "Save and continue" there returns straight to the check page.
- Shows totals, CGST/SGST or IGST (with the computed reason "Intra-state: supplier and branch are both in Maharashtra"), rounded total, `AmountInWords`.
- **Approval preview** from `approval.Evaluate` (read-only): "This order needs approval from a Purchase Manager because it is ₹50,000 or more." or "No approval needed. It will be approved and numbered when you submit."
- Primary button is the state action: "Submit for approval" or "Submit".
- Server re-validates everything on submit; any failure sends the user to the first failing step with its errors.
- **Reopening a draft** from a list lands on the check page with each section marked "Complete" or "Needs attention", not on step 1.

**Lifecycle mapping (spec §3.2).**

| Event | Screen |
|---|---|
| Submit | Document page with banner "Submitted for approval to Ravi Kulkarni (Purchase Manager)." or "Approved. PO/26-27/00001 has been numbered." |
| Pending | Read-only document page; Withdraw (R2) for the submitter returns it to draft and the check page |
| Rejected | Document page with the reason in a danger banner at the top; "Revise" → draft → check page |
| Cancelled → Amend | New draft opened on the check page with banner "Amending PO/26-27/00042" |

### 8.4 Document page (any state)

Phone order: state badge + label ("PO/26-27/00001" or "Draft 42"); amount and in-words; who/why if pending; key header facts; lines as compact cards with totals and GST split; attachments as thumbnails (full screen on tap); approval history and audit in `Disclosure`s. The action bar shows only allowed actions; secondary actions (Print, Cancel, Delete draft, Close) go in a "More actions" list on the page, not the bar. Desktop: content plus side panel from 80rem.

### 8.5 Approvals

- **Inbox ("Waiting for my approval")**, count badge in the header: card list, oldest first, each card shows type and label, party/employee, amount (large, right), "Submitted by Priya Shah · 2 days ago", and why it is with me ("₹50,000 or more"). Filter by type. No approve on the card: the approver opens the document.
- **Approval page:** order on a phone: amount + words; "Needs your approval: Purchase Manager, ₹50,000 or more"; header facts; lines; attachments (expense receipts next to each expense line); history collapsed.
- **Action bar:** "Approve" (primary) and "Reject" (secondary, not red) always visible. **Approve is one tap, no confirm dialog** (it is the expected action; reject is reversible via revise). Next page: success banner plus "Next request" link.
- **Reject** opens its own page: summary line, required "Reason for rejecting" textarea (1 to 1,000 characters, live counter), button "Reject and send back".
- If self-approval is blocked, the bar shows one line explaining why instead of the buttons.
- Stale action (withdrawn or already decided): "PO/26-27/00001 is no longer waiting for approval. It was approved by Ravi Kulkarni." (APR wording).
- **My requests:** the same card list filtered to my documents.

### 8.6 Admin: rights

Never a role × DocType × right grid. **Roles list → role page** (DocTypes as cards, each summarised "8 of 11 rights · own documents") → **DocType rights page**: 11 rows (right name, one-line meaning, None / Own / Team / All segmented control with the label above it so it fits at 320 px; Team appears only on DocTypes with an employee field), "Save rights". Rights invalid for the DocType (D-AUTH-11) are not shown, with a note. Saving any right grants read with a notice. "Copy rights from another role" and a read-only **"Who can…"** view (pick DocType + right → roles and users). Desktop: two panes. Every save is audited.

### 8.7 Other screens

| Screen | Pattern |
|---|---|
| Sign in | Single page; `autocomplete="username"` / `"current-password"`; paste allowed; "Show password" toggle; generic error in `ErrorSummary` |
| First admin setup, change password | Single page; password rules listed before typing |
| GRN | Wizard: Choose PO (approved, pending qty, my sites) → Received quantities (one card per PO line, "Received now" prefilled with pending, "Receive all pending" / "Clear all") → Delivery details (challan no., date, vehicle, photo of challan) → Check and submit |
| Expense claim | Wizard: Claim details → Expenses (add-to-a-list; each expense page photo first, then type radios, date, amount, note) → Check and submit (thumbnail per expense, total, "Goes to Ravi Kulkarni (your manager)") |
| Party | Create: wizard (Party details → Addresses → Contacts → Check and save). Edit: summary page with Change per section |
| CSV import | Wizard: Choose master + template download → Upload → Check results (dry run; problems as cards "Row 14 · GSTIN · check digit is wrong") → Import ("Import 214 parties"); step 4 unreachable while errors exist |
| Print view | A4 sheet (`/print/*`, may use `<table>`); on screen scaled to the viewport width (CSS `zoom`/transform inside a container, no sideways scroll) with a sticky "Print or save as PDF" button |
| My sessions | Card list with "End session"; "End all other sessions" |

## 9. Writing rules (copy)

Sentence case everywhere. Buttons are verbs ("Save and continue", "Submit for approval", "Approve", "Reject and send back"). Errors say what is wrong and how to fix it with the field name ("Rate can have at most 4 decimal places."). Wording owned by kernel areas (403 message in `00-fixtures.md`, APR state messages, NUM parse errors) is used exactly; screens never invent their own variant. Dates `dd-mm-yyyy`; times IST `24-10-2026 10:30`; money `₹94,164.00`; documents without a number "Draft 42".

## 10. Enforcement

**Lint test** (`internal/ui/lint_test.go`, Go, no Node) walks `**/*.templ` and `internal/ui/css/*.css` and fails with `file:line` on: `style=` attributes; inline `<script>` (except `type="application/json"`); `hx-on`, `js:` in `hx-vals`/`hx-headers`, trigger filters `[...]`; templ `css`/`script` blocks; fixed `width`/`min-width`/`inline-size` in px/rem/em outside `tokens.css` (except inside `min(100%, …)`); `100vw`; `overflow-x: auto|scroll`; `overflow-x: hidden` on `html`/`body`; `white-space: nowrap` outside `.truncate`; raw colours outside `tokens.css`; `<table` outside `print/`. Full rule table: research §6.3.

**Overflow test** (`e2e`, chromedp): every GET route from `chi.Walk` (except `/static`, `/print`, `/api`) plus `Setup` variants (each wizard step, each dialog/sheet, a 422 state, an empty list) × widths 320, 360, 390, 768, 1024, 1440; mobile emulation below 768. Assert `document.documentElement.scrollWidth <= document.documentElement.clientWidth` (**not** `innerWidth`, which grows with content under mobile emulation) and that no element's box extends past the viewport or has content wider than itself. Script verbatim in research §6.4. A route with no screen entry fails the test.

**Other e2e gates:** axe-core WCAG 2.2 AA zero violations on every screen; every interactive element ≥ 44 × 44 at 360 px; zero CSP violations (via `report-uri` handler in e2e builds), which then permits `style-src 'self'`.

**Long-value seed** (`internal/testkit/longvalues.go`, used by the overflow test): 100-character party and item names, 60-character unbroken codes, the longest valid emails, ₹99,99,99,999.99 amounts, 500-line documents, error messages at their maximum length.

## 11. Out of scope for v1

Dark mode (never), offline/PWA install, bulk approve, drag-and-drop, swipe actions, user-customisable columns, charts and dashboards, push or email notifications, Hindi UI translation (Devanagari is supported in data only).
