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

### HOOK-049 · A field checked on blur keeps Save in place while its echo arrives
- **Priority:** P1
- **Level:** e2e
- **Given** a wizard step whose date field posts to `/ui/validate/` on blur **When** the user presses the mouse on Save, the field's echo arrives, and the user releases at the same point **Then** Save has not moved and step 2 loads: the empty message line was reserved (CD-287).
- **Go test:** `TestE2E_Field_EchoKeepsSaveInPlace`
- **Sources:** CD-287
