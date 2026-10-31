# docscreen module hooks (HOOK)

## Scope
The optional per-DocType seams of `internal/ui/docscreen` (`Module`, `Deps.Modules`) from `docs/specs/2026-10-30-module-docscreen-hooks-spec.md` v0.3, whose §3 is authoritative for names. Cases are the spec's §6 table. A test-only `docscreentest.Hooked` DocType exercises every hook, on the real engine and real AUTH: no lenient double (CD-125). Built in MOD-0 tasks M0-1 to M0-5 of `docs/superpowers/plans/2026-10-31-mod-po.md`.

Fixtures are in `docs/tests/core/00-fixtures.md`.

## Test cases

### HOOK-001 · A DocType with no Module renders byte-identically to today
- **Priority:** P1
- **Level:** integration
- **Given** a DocType with no entry in `Deps.Modules` **When** its list, wizard steps, line pages, check page, document page and reason pages are rendered **Then** each is byte-identical to the output before the hooks were added.
- **Go test:** `TestModule_NoneUnchanged`
- **Sources:** Hooks spec §3, §6

### HOOK-002 · Custom Body on a step keeps Stepper, Bar, version and Back
- **Priority:** P1
- **Level:** integration
- **Given** `docscreentest.Hooked` with a `Step.Body` on one step **When** that step renders (`stepFull` and `stepFrame`) **Then** Body replaces the step's inputs inside the step's form, and the Stepper, Bar, version input, Back, `Save and finish later`, `return=check` and error summary stay docscreen's. On the Items step, Body also replaces `M.After`.
- **Go test:** `TestModule_StepBody`
- **Sources:** Hooks spec §4.1, §6

### HOOK-003 · Custom Save: Values and Lines reach Engine.Do with the posted version
- **Priority:** P1
- **Level:** integration
- **Given** a `Step.Save` returning `Values` and `Lines` **When** the step is posted **Then** docscreen calls `Engine.Do` with those values and lines, the page's posted `version` and the step's `Only`; a set `Save.Lines` makes `Only` include `lines`.
- **Go test:** `TestModule_StepSave`
- **Sources:** Hooks spec §4.1, §6

### HOOK-004 · First step creates a draft with lines, as a site-restricted user, on the real auth.Service
- **Priority:** P1
- **Level:** integration
- **Given** `Steps["details"]` with Body and Save, a site-restricted user (e.g. `suresh`) and the real `auth.Service` **When** the new-document step is posted (`newPost`, `View` nil) **Then** `Save.Values` and `Save.Lines` go to `Engine.Do` with id 0, and the draft is created with its lines in one call and one audit entry.
- **Go test:** `TestModule_FirstStepCreatesReal`
- **Sources:** Hooks spec §4.1, §6

### HOOK-005 · Save.Errors is a 422 with nothing written, and Body redraws typed values
- **Priority:** P1
- **Level:** integration
- **Given** a `Step.Save` returning `Save.Errors` **When** the step is posted **Then** the response is 422, nothing is written, and Body is redrawn with `StepState.Posted` true, showing the typed values and the errors.
- **Go test:** `TestModule_SaveErrors`
- **Sources:** Hooks spec §3, §4.1, §6

### HOOK-006 · A stale version is a 409 with typed values kept
- **Priority:** P1
- **Level:** integration
- **Given** a custom step opened at one version and changed elsewhere **When** it is posted with the old version **Then** the response is 409, nothing is written, and Body is redrawn with `Posted` true and the typed values kept.
- **Go test:** `TestModule_Conflict`
- **Sources:** Hooks spec §4.1, §6

### HOOK-007 · A 403 from Save shows AUTH's refusal, and a plain error is a 500
- **Priority:** P1
- **Level:** integration
- **Given** a `Step.Save` that returns AUTH's refusal (for example from `CheckLinkTx`) **When** the step is posted **Then** the error goes through `Classify` and the page shows AUTH's refusal. **Given** a `Step.Save` that returns a plain error **Then** the response is 500.
- **Go test:** `TestModule_SaveError`
- **Sources:** Hooks spec §4.1, §6

### HOOK-008 · Save.Lines nil keeps the stored lines
- **Priority:** P1
- **Level:** integration
- **Given** a draft with lines and a `Step.Save` returning `Lines` nil **When** the step is posted **Then** docscreen re-sends the loaded lines, as `stepPost` does, and the stored lines are unchanged.
- **Go test:** `TestModule_LinesKept`
- **Sources:** Hooks spec §3, §4.1, §6

### HOOK-009 · Custom line page: new, edit, remove and back
- **Priority:** P1
- **Level:** integration
- **Given** a `Line` with Body and Save **When** a new line and an existing line are opened, saved and removed, and Back is used **Then** `Line.Body` and `Line.Save` replace `linePage` and `postedLine` for new and existing lines, the returned values are merged into the loaded lines as `linePost` does, and docscreen keeps the URLs, version, `backToItems` and remove.
- **Go test:** `TestModule_LinePage`
- **Sources:** Hooks spec §3, §4.2, §6

### HOOK-010 · Hidden field keeps its stored value through a step save and a line save; a crafted post of it is dropped
- **Priority:** P1
- **Level:** integration
- **Given** a header field and a line field with stored values, both hidden by `Hide` **When** a step and a line are saved without them, and when a crafted post includes them **Then** the stored values are kept (`postedStep` and `postedLine` skip hidden fields), and the crafted values are dropped.
- **Go test:** `TestModule_HideKeepsValue`
- **Sources:** Hooks spec §4.3, §6

### HOOK-011 · Check extras appear in order; a hook error is a 500
- **Priority:** P1
- **Level:** integration
- **Given** a `Check` returning `Notes`, `Warnings` and `Totals` **When** the check step renders **Then** `Notes` follow the approval preview, `Warnings` use `components.Warnings`, and `Totals` sit above the grand total. **Given** a `Check` that returns an error **Then** the response is 500.
- **Go test:** `TestModule_CheckExtra`
- **Sources:** Hooks spec §3, §4.4, §6

### HOOK-012 · Page cards, line notes and warnings appear, and only after the read check
- **Priority:** P1
- **Level:** integration
- **Given** a `Page` returning `Warnings`, `LineNotes`, `Pairs` and `Cards` **When** a user who can read the record opens the document page **Then** they appear (Cards after Lines, before History). **When** a user who can't read it opens the page **Then** `Page` is not called (it runs after docscreen's visibility check).
- **Go test:** `TestModule_PageExtra`
- **Sources:** Hooks spec §3, §4.4, §6

### HOOK-013 · Reason note and hint appear
- **Priority:** P1
- **Level:** integration
- **Given** a `Reason` returning `Note` and `Hint` **When** the reason page renders, and when `reasonPost` redraws it with a 422 **Then** `Note` shows above the reason field and `Hint` is the field's hint; button and field labels stay the kernel's.
- **Go test:** `TestModule_ReasonExtra`
- **Sources:** Hooks spec §3, §4.4, §6

### HOOK-014 · Return-to-check and Save and finish later work from a custom step
- **Priority:** P1
- **Level:** integration
- **Given** a custom step **When** it is posted with `return=check`, and with `Save and finish later` **Then** each navigates as from a generic step.
- **Go test:** `TestModule_Navigation`
- **Sources:** Hooks spec §4.1, §6

### HOOK-015 · A hook taking a second connection fails the nestcheck gate
- **Priority:** P1
- **Level:** integration
- **Given** a hook that opens a second connection inside a transaction **When** it runs under the nestcheck gate **Then** the gate fails. Hooks are called outside `db.Write` and `db.Read`.
- **Go test:** `TestModule_NoNestedTx`
- **Sources:** Hooks spec §5 rule 1, §6

### HOOK-016 · Save with line ids updates lines in place; only changed lines count under Only (CD-207)
- **Priority:** P1
- **Level:** integration
- **Given** a draft with lines and a `Save` returning lines with their ids (`LineView.ID`) **When** the step is posted **Then** `syncLines` updates the lines in place instead of deleting and reinserting them, and only the changed lines count under `Only` (CD-207).
- **Go test:** `TestModule_LineIDs`
- **Sources:** Hooks spec §3, §6; CD-207

### HOOK-017 · Empty non-nil Save.Lines removes all lines
- **Priority:** P1
- **Level:** integration
- **Given** a draft with lines **When** a `Save` returns a non-nil empty `Lines` (as `Clear all` does) **Then** every line is removed.
- **Go test:** `TestModule_ClearLines`
- **Sources:** Hooks spec §3, §4.1, §6

### HOOK-018 · NoLinePages answers 404 on every line route; CardsOnly has no grid
- **Priority:** P1
- **Level:** integration
- **Given** a Module with `NoLinePages` **When** any line route is requested **Then** it answers 404. **Given** a Module with `CardsOnly` **When** the Items step renders at a desktop width **Then** it shows the add-to-a-list cards and no grid.
- **Go test:** `TestModule_LineFlags`
- **Sources:** Hooks spec §3, §4.2, §6

### HOOK-019 · A draft's reader without write gets the read-only document page
- **Priority:** P1
- **Level:** integration
- **Given** a draft and a user who can read it but lacks `write` on it **When** they open it **Then** they get the read-only document page (no step form, Change links or line actions); a user with `write` still lands on the check page.
- **Go test:** `TestDraft_ReaderGetsDocumentPage`
- **Sources:** CD-263 G1, design-system §8.3

### HOOK-020 · A reader's step and line URLs redirect to the document page; their POSTs are refused
- **Priority:** P1
- **Level:** integration
- **Given** a draft and a reader without `write` **When** they GET any step (check included, with or without `return=check`) or any line page **Then** each answers 303 to the document page; their step and line POSTs get AUTH's write refusal, before any version check or parsing.
- **Go test:** `TestDraft_ReaderStepAndLineURLsRedirect`
- **Sources:** CD-263 G1, CD-259

### HOOK-021 · Read-only drafts on the real auth.Service
- **Priority:** P1
- **Level:** integration
- **Given** `fixture.Order` on the real `auth.Service` **When** the owner, a Purchase Manager (read, no write) and a second Purchase User under own-scope write open the same draft **Then** the owner gets the check page and the other two the read-only page.
- **Go test:** `TestDraft_ReaderRealAuth`
- **Sources:** CD-263 G1, CD-246

### HOOK-022 · Check-page cards from the module
- **Priority:** P1
- **Level:** integration
- **Given** a Module whose `Check` returns `Cards` **When** the check page renders **Then** the cards appear after the Attachments card and before the grand total, outside the step form; a hook error is a 500.
- **Go test:** `TestModule_CheckCards`
- **Sources:** CD-263 G2

### HOOK-023 · Owner notice at the top of the document page
- **Priority:** P1
- **Level:** integration
- **Given** a Module whose `Page` returns a `Notice` **When** the document page renders **Then** the notice is the last of docscreen's banners, above the title; a blank kind renders as Info.
- **Go test:** `TestModule_PageNotice`
- **Sources:** CD-263 G3

### HOOK-024 · Module entries under More actions
- **Priority:** P1
- **Level:** integration
- **Given** a Module whose `Page` returns `More` entries **When** the document page renders **Then** they come before docscreen's own More actions entries, whose order is unchanged.
- **Go test:** `TestModule_PageMore`
- **Sources:** CD-263 G4

### HOOK-025 · A main button when docscreen has none
- **Priority:** P1
- **Level:** integration
- **Given** a Module whose `Page` returns a `Primary` **When** the bar has no primary and no note **Then** it shows as the bar's primary link; when docscreen has its own primary (Approve) or a note, the module's is not shown; a Primary without a link is a 500.
- **Go test:** `TestModule_PagePrimary`
- **Sources:** CD-263 G5

### HOOK-026 · A Body that returns no component falls back to the generic step
- **Priority:** P1
- **Level:** integration
- **Given** a `Step.Body` that returns a nil component **When** the step renders **Then** docscreen's generic body renders.
- **Go test:** `TestModule_StepBodyFallback`
- **Sources:** CD-263 G6

### HOOK-027 · A step that saves and stays
- **Priority:** P1
- **Level:** integration
- **Given** a step whose bar posts `nav=stay` **When** it saves **Then** the same step redraws at its bare URL (303, or an htmx push), and a 422 keeps the typed values.
- **Go test:** `TestModule_StepStay`
- **Sources:** CD-263 G6

### HOOK-028 · A module's step bar
- **Priority:** P1
- **Level:** integration
- **Given** a `Step.Bar` hook **When** the step renders **Then** its primary label, `nav=stay` and Back link apply, `Save and finish later` stays, and Enter submits the primary; a zero `StepBar` keeps docscreen's bar.
- **Go test:** `TestModule_StepBar`
- **Sources:** CD-263 G6

### HOOK-029 · Per-DocType state labels on badges, list rows and the submit banner
- **Priority:** P1
- **Level:** integration
- **Given** a DocType with `StateLabels` **When** its document page, list and submit banner render **Then** each shows the DocType's label; a DocType without labels is unchanged.
- **Go test:** `TestModule_StateLabels`
- **Sources:** CD-263 G7, CD-269

### HOOK-030 · An approver sees why the document needs them
- **Priority:** P1
- **Level:** integration
- **Given** a pending document **When** an approver, a non-approver and a blocked approver open it **Then** only the approver sees `Needs your approval: {who}, {why}`.
- **Go test:** `TestPage_ApproverNeedsYourApproval`
- **Sources:** CD-263 G9(a), CD-266

### HOOK-031 · Approval status carries the viewer's roles and why
- **Priority:** P1
- **Level:** integration
- **Given** a pending approval request **When** its status is read for a viewer **Then** it carries the roles (or the reporting-manager form) the viewer is eligible through and the `WhyText`; both are empty unless pending.
- **Go test:** `TestApprovalStatus_RolesAndWhy`
- **Sources:** CD-263 G9(a), CD-266

### HOOK-032 · Approver line on the real services
- **Priority:** P1
- **Level:** integration
- **Given** `fixture.Claim` with a reporting-manager rule and a fallback role on the real services **When** the manager and a role holder open the pending claim **Then** the manager sees the reporting-manager form and the role holder sees their roles.
- **Go test:** `TestApp_ApproverLineReal`
- **Sources:** CD-263 G9(a), CD-266

### HOOK-049 · A field checked on blur keeps Save in place while its echo arrives
- **Priority:** P1
- **Level:** e2e
- **Given** a wizard step whose date field posts to `/ui/validate/` on blur **When** the user presses the mouse on Save, the field's echo arrives, and the user releases at the same point **Then** Save has not moved and step 2 loads: the empty message line was reserved (CD-287).
- **Go test:** `TestE2E_Field_EchoKeepsSaveInPlace`
- **Sources:** CD-287

### HOOK-033 · A line page Top region outside the line form
- **Priority:** P1
- **Level:** integration
- **Given** a Module with `Line` and `Line.Top` **When** the line page renders, on GET and on a 422 or 409 redraw **Then** Top's component renders after the title and outside the line form (`#line-form`), with `Posted` set on a redraw; an error is a 500; without `Module.Line` the form has no id.
- **Go test:** `TestModule_LineTop`
- **Sources:** CD-275 UX-E1

### HOOK-034 · A module names its lines
- **Priority:** P1
- **Level:** integration
- **Given** a Module with `LineNoun` **When** the Items step and line pages render **Then** the add button and new-line title use `Add`, existing line titles use `Title` and the number, and an empty Items step shows `Empty`; a zero `LineNoun` keeps today's words.
- **Go test:** `TestModule_LineNoun`
- **Sources:** CD-275 UX-E2

### HOOK-035 · Add button above the cards, repeated from four lines
- **Priority:** P1
- **Level:** integration
- **Given** a Module with `AddTop` **When** the Items step renders **Then** the add button sits above the cards, and from 4 lines a second one follows the cards and the totals.
- **Go test:** `TestModule_AddTop`
- **Sources:** CD-275 UX-E2, EUX-037

### HOOK-036 · Line card receipt thumbnail
- **Priority:** P1
- **Level:** unit
- **Given** a `LineCard` with `Receipt`, `ReceiptNote` or `ReceiptWarn` **When** it renders **Then** it shows the thumbnail or chip with its alt text, the note and the warning icon; a zero card is unchanged.
- **Go test:** `TestLineCard_Receipt`
- **Sources:** CD-275 UX-E3

### HOOK-037 · Receipts on line cards everywhere they show
- **Priority:** P1
- **Level:** integration
- **Given** a Module whose `LineCard` sets a receipt **When** the Items step, check page and document page render **Then** each card shows it.
- **Go test:** `TestModule_LineCardReceipt`
- **Sources:** CD-275 UX-E3

### HOOK-038 · Module control of the read-only Attachments card
- **Priority:** P1
- **Level:** integration
- **Given** a non-Attachable DocType and a Module whose `Page` returns `Files` **When** the document page renders **Then** nil keeps the card, an empty list removes it, items render without Remove, and an Attachable DocType is unchanged.
- **Go test:** `TestModule_PageFiles`
- **Sources:** CD-275 UX-E7

### HOOK-039 · The line page's Save moves to the sticky bar
- **Priority:** P1
- **Level:** integration
- **Given** a Module with `Line` **When** the line page renders **Then** Save is the ActionBar primary posting `form=line-form`, and Enter in a field submits it.
- **Go test:** `TestModule_LineBar`
- **Sources:** CD-275 UX-E4

### HOOK-040 · An upload disables the page's Save
- **Priority:** P1
- **Level:** unit
- **Given** `UploadProps.AlsoDisable` **When** the upload forms render **Then** both forms' `hx-disabled-elt` include that selector after their own inputs.
- **Go test:** `TestFileUpload_AlsoDisable`
- **Sources:** CD-275 UX-E5

### HOOK-041 · Save is disabled during an upload and focus stays put
- **Priority:** P1
- **Level:** e2e
- **Given** a line page with an upload and a Save **When** a file uploads **Then** Save is disabled until the upload ends, and focus returns to the input only if it was there or on the body.
- **Go test:** `TestUploadBrowser_SaveDisabledFocus`
- **Sources:** CD-275 UX-E5

### HOOK-042 · Check-page module totals after the grand total
- **Priority:** P1
- **Level:** integration
- **Given** a Module whose `Check` returns `Totals` **When** the check page renders **Then** they follow the grand total and its words.
- **Go test:** `TestModule_CheckTotalsAfter`
- **Sources:** CD-275 UX-E6

### HOOK-043 · A step's Top renders above the generic body
- **Priority:** P1
- **Level:** integration
- **Given** a `Step.Top` hook **When** the step renders with no Body, or a Body that returns no component **Then** Top's component renders first inside the step form; it does not render when the Body returns a component; an error is a 500.
- **Go test:** `TestModule_StepTop`
- **Sources:** CD-280 D5

### HOOK-047 · LargeQtyInput keeps QtyInput's attributes at the large size
- **Priority:** P1
- **Level:** unit
- **Given** a `LargeQtyInput` and a `QtyInput` with the same props **When** both render **Then** the large one carries the large control class, and its id, name, inputmode, `hx-post` and unit suffix equal the plain one's.
- **Go test:** `TestLargeQtyInput`
- **Sources:** CD-286 Q4, design-system §7

### HOOK-048 · LargeQtyInput is 56 px tall and never clips
- **Priority:** P1
- **Level:** e2e
- **Given** the gallery's gloved quantity sample **When** it is shown at 320 and 360 px **Then** the input is at least 56 px tall with 20 px figures and a 16 px unit, `9,99,999.9999` is not clipped, and nothing overflows or is too small to tap.
- **Go test:** `TestLargeQtyInputBrowser_Size`
- **Sources:** CD-286 Q4, CD-169

### HOOK-044 · A line error links to the Body's input only when the Body rendered it
- **Priority:** P1
- **Level:** integration
- **Given** an Items step with a `Step.Body` and a `Save` returning line errors **When** the step redraws (full page and htmx) **Then** an error with a line and field links to `#` + `LineFieldID(line, field)` only when the Body rendered an element with exactly that id; otherwise, and for errors with no field, it links to `#step-title`.
- **Go test:** `TestModule_BodyLineErrorLinks`
- **Sources:** CD-286 R1

### HOOK-045 · A step with a Body starts its form with the Enter copy of the primary
- **Priority:** P1
- **Level:** integration
- **Given** a step whose `Step.Body` renders and a Bar with a primary **When** the step renders **Then** the form's first submit is a hidden, 0×0, untabbable copy of the primary with its name and value (and nav=stay when the primary stays), and the first visible submit is the primary; a nil Body or no primary renders no copy.
- **Go test:** `TestModule_EnterPrimary`
- **Sources:** CD-286 R4, CD-300 (3)

### HOOK-046 · Enter in a Body's input presses the primary
- **Priority:** P1
- **Level:** e2e
- **Given** an Items step whose Body has its own submit button before the Bar **When** the user presses Enter in a quantity input **Then** the step saves and continues as the primary does, not as the Body's button.
- **Go test:** `TestModuleBrowser_EnterPressesPrimary`
- **Sources:** CD-286 R4

### HOOK-050 · Every Screens list URL is accepted by the list package
- **Priority:** P1
- **Level:** integration
- **Given** every registered document DocType with a filterable `state` field **When** each `/doc/<name>?…` URL that `Screens` generates is parsed and built by the real list path (`ParseListQuery`, `list.ParseSort`, `list.Build`) **Then** list accepts it; the "list filtered" screen's two-state filter uses Op `in`.
- **Go test:** `TestScreens_ListURLsAreAccepted`
- **Sources:** M0-34 (head, AD-11 S0 sweep)
