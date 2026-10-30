# Module spec: docscreen module hooks

Version 0.1 · 30 October 2026 · Status: draft for the head's ruling · Author: AD-6

The design pass the head asked for on PO spec §15 item 11. It covers what `internal/ui/docscreen` must offer so the Purchase Order, GRN, Expense Claim and Employee Advance screens can be built without forking the generic document screens. It was read against `docscreen` on `main` at b1612b2. It changes no behaviour for a DocType with no hooks. Screen design stays with the UX documents (CD-97); this spec is about seams, not layouts.

---

## 1. The problem

`docscreen` renders every DocType from its Fields. That carries most of each module, and these needs don't fit:

| Need | Module | Why the generic screen can't do it |
|---|---|---|
| Choose a PO, then create the draft with site, project and lines | GRN | `newPost` sends Values only (`form.go`), and the first save must carry `site_id` (GRN spec §3.1) |
| Received quantities from PO lines, saved together | GRN | `stepPost` re-saves the loaded lines and ignores posted ones |
| Radios of this vendor's addresses; open advances | PO, Expense | `/pick/{Link}` takes only `q` |
| Photo-first line page, same-receipt choice | Expense | `linePage` renders Fields in order |
| Place reason, reverse-charge note, duplicate and over-delivery warnings | PO, GRN, Expense | The check page is fixed (`check.templ`) |
| Ordered, Received and Pending per line; linked GRNs; bill panel | PO, GRN | `docBody` has no extra region |
| Close warning; `Reason for closing` hint | PO, Expense | `reasonPages` is a static map |
| `already_received`, Employee, earlier-PO fields shown only sometimes | PO, Expense | Every editable field renders |
| `Items` relabelled `Expenses` or `Received quantities` | GRN, Expense | Label comes from the slug |

## 2. Three ways to do it

| | Approach | For | Against |
|---|---|---|---|
| **A (recommended)** | A struct of optional functions per DocType in `Deps.Modules`. docscreen keeps routes, frame, version, navigation and every call to `Engine.Do` | Matches how `Deps` already works (`Principal`, `PrintURL`, `LinkRead`: nil means default). One write path, so CD-94, 409, 422 and `Only` hold everywhere | docscreen gains about 15 small seams |
| B | A `Module` interface with an embeddable no-op base | Compile-time method names | A silent no-op when a method name drifts; nothing else here uses that style |
| C | Each module owns all its routes and composes `internal/ui/wizard` | No docscreen change | Every module re-creates version checks, 422 and 409 redraws, Back, return-to-check and warnings. That is the bug class of UI Tasks 4 to 8, and the overflow sweep loses them |

## 3. API

In `internal/ui/docscreen/module.go`:

```go
// Module is what one DocType adds to the generic screens. Every field is
// optional; nil keeps today's behaviour.
type Module struct {
	StepLabels map[string]string                  // step slug → label
	Steps      map[string]Step                    // step slug → custom step
	Line       *Line                              // custom line page
	LineCard   func(c Ctx, l LineView) components.LineCard // card for a line with no item_id
	Hide       func(c Ctx, f doctype.Field) bool  // field not rendered or summarised
	Check      func(c Ctx) (CheckExtra, error)    // check page additions
	Page       func(c Ctx) (PageExtra, error)     // document page additions
	Reason     func(c Ctx, a doctype.Action) (ReasonExtra, error)
}

// Ctx is what a hook reads. View is nil only on a new document's first step.
type Ctx struct {
	Req       *http.Request
	Principal auth.Principal
	DT        *doctype.DocType
	View      *DocView
	Deps      Deps                  // for FieldInput and the rest
}

// Step replaces a wizard step's body, its save, or both.
type Step struct {
	Body func(c Ctx, s StepState) (templ.Component, error) // nil → the generic form
	Save func(c Ctx) (Save, error)                        // nil → the generic post
}

// StepState is the redraw input: posted is true on a 422 or 409, when the
// body reads typed values from c.Req; Errors holds the step's field errors.
type StepState struct {
	Posted bool
	Errors []doctype.FieldError
}

// Save is what a custom post asks docscreen to do.
type Save struct {
	Values map[string]string
	Lines  []map[string]string // nil keeps the stored lines
	Errors []doctype.FieldError // parse errors: 422 with nothing written
}

// Line replaces the line page; same shape as Step, with Ctx.Line set.
type Line struct {
	Body func(c Ctx, no int, s StepState) (templ.Component, error)
	Save func(c Ctx, no int) (Save, error)
}

type CheckExtra struct {
	Notes    []string          // info banners under the approval preview
	Warnings []string          // components.Warnings
	Totals   []components.Pair // above the grand total
}

type PageExtra struct {
	Warnings  []string
	LineNotes map[int]string   // line no → a note on that card
	Cards     []templ.Component // after Lines, before History
}

type ReasonExtra struct {
	Note, Hint string // shown above the field; the field's hint
}
```

`Deps` gains `Modules map[string]Module`, keyed by DocType name. `internal/app` fills it, and `internal/ui` never imports a module (spec §4.2). `components.LineCard` and `LineGridRow` gain an additive `Note string`.

## 4. Behaviour

### 4.1 Steps

- `FormSteps` is unchanged, so step order stays header steps, items, check (PO spec §3, GRN spec §11). `StepLabels` applies only to the label shown.
- **GET.** `Step.Body` replaces the step's inputs inside `stepFull` and `stepFrame`. The Stepper, Bar, version input, `Save and finish later`, `return=check` and error summary stay docscreen's.
- **POST.** docscreen does what it does today, with one change: where it would call `postedStep`, it calls `Step.Save`. It then calls `Engine.Do` itself, adding the page's posted `version` and the step's `Only`. `Save.Lines` nil means it re-sends the loaded lines, as `stepPost` does now. A set `Save.Lines` makes `Only` include `lines`.
- **First step.** `newGet` and `newPost` use the first step's hooks with `View` nil. `newPost` passes `Save.Values` and `Save.Lines` to `Engine.Do` with id 0. `create` already stores lines, so the GRN draft is created in one call and one audit entry.
- **Errors.** `Save.Errors` is a 422 with nothing written, like `postedStep`'s. A returned `error` goes through `Classify`, so a 403 from `CheckLinkTx` in the GRN's Save shows AUTH's refusal, and a plain error is a 500. A 422 or 409 from `Engine.Do` redraws `Body` with `Posted: true`, so typed values survive.
- **Empty step.** A step with a `Body` is never `Nothing needed here`, unless the Body says so.
- **Check sections.** A custom step's section still comes from its fields and `Hide`. The Items section uses `lineCards`, so a module shapes it with `Module.LineCard`, needed because GRN's and Expense's lines have no `item_id` for `lineView` to summarise.

### 4.2 Line page

`Line.Body` and `Line.Save` replace `linePage` and `postedLine` for new and existing lines. docscreen keeps the URLs, version, `backToItems`, grid and remove. The grid uses GridCells for the declared columns, and a module with its own line page sets `Steps["items"]` to a card-only body. Expense does that.

**Receipts.** The upload is a separate module route, because a file post isn't a line save. It attaches the file and returns a small fragment with the thumbnail and a hidden `receipt_sha256`. It never redraws the fields being typed in (CLAUDE.md).

### 4.3 Hide

`Hide` runs in `stepViewOf`, on `lineView`'s inputs and on check sections. A hidden field posts nothing, so its stored value stays (CD-135 item 2). It's presentation only: the rule is enforced in `Validate` (Expense's employee check), never by hiding.

### 4.4 Extras

- `Check` runs in `checkStep`. `Notes` follow the approval preview. `Warnings` use `components.Warnings`. `Totals` sit above the grand total, for To pay and Less advance.
- `Page` runs in `pageViewOf`. `Cards` are arbitrary components, so the bill panel is the module's own form posting to the module's route.
- `Reason` runs in `reasonGet`. `Note` shows above the reason field. Button and field labels stay the kernel's.
- All three run on GET, after docscreen's visibility check. An error is a 500: extras never fail silently.

### 4.5 Module routes

Bill details and receipt upload are outside `/doc/`. They live under `/modules/<module>/…`, each with a route guard as usual (AUTH-107, CD-46 keeps `CheckedInHandler` to `/doc/`), posting the page's `version` (CD-183). Reports the same.

## 5. Rules for hook authors

1. **No hook runs inside a transaction.** They're called outside `db.Write` and `db.Read`, so they open their own reads, and `Save` may use `Auth.CheckLinkTx` inside its own `db.Read`. The nestcheck gate catches a mistake.
2. **Hooks never call `Engine.Do`.** Writes go back through `Save`, except a module route's own write through `db.Write`.
3. **Body composes components.** `FieldInput`, `components.*`, Tailwind layout only (design-system §7.1). No daisyUI classes outside `internal/ui`, and no inline scripts or styles. The UI lint walks `**/*.templ`. `app.css` already sources `internal/modules/**/*.templ`.
4. **A hook's data uses scope.** A list of POs or advances in a Body goes through `Auth.Scope` or `list.Build`, never a bare query (AUTH-110). A hook must not read another record's label without `LinkRead` (CD-233).
5. **A Body renders without JavaScript** (design rule 2).
6. **Refusal text is the producer's** (CD-142). Hooks show messages from the module's service package.

## 6. Tests and gates

| ID | Behaviour | Go test | P | Level |
|---|---|---|---|---|
| HOOK-001 | A DocType with no Module renders byte-identically to today | `TestModule_NoneUnchanged` | P1 | integration |
| HOOK-002 | Custom Body on a step keeps Stepper, Bar, version and Back | `TestModule_StepBody` | P1 | integration |
| HOOK-003 | Custom Save: Values and Lines reach `Engine.Do` with the posted version | `TestModule_StepSave` | P1 | integration |
| HOOK-004 | First step creates a draft with lines, as a site-restricted user, on the real `auth.Service` | `TestModule_FirstStepCreatesReal` | P1 | integration |
| HOOK-005 | `Save.Errors` is a 422 with nothing written, and Body redraws typed values | `TestModule_SaveErrors` | P1 | integration |
| HOOK-006 | A stale version is a 409 with typed values kept | `TestModule_Conflict` | P1 | integration |
| HOOK-007 | A 403 from Save shows AUTH's refusal, and a plain error is a 500 | `TestModule_SaveError` | P1 | integration |
| HOOK-008 | `Save.Lines` nil keeps the stored lines | `TestModule_LinesKept` | P1 | integration |
| HOOK-009 | Custom line page: new, edit, remove and back | `TestModule_LinePage` | P1 | integration |
| HOOK-010 | Hidden field keeps its stored value, and a posted value is still validated | `TestModule_HideKeepsValue` | P1 | integration |
| HOOK-011 | Check extras appear in order; a hook error is a 500 | `TestModule_CheckExtra` | P1 | integration |
| HOOK-012 | Page cards, line notes and warnings appear, and only after the read check | `TestModule_PageExtra` | P1 | integration |
| HOOK-013 | Reason note and hint appear | `TestModule_ReasonExtra` | P1 | integration |
| HOOK-014 | Return-to-check and Save and finish later work from a custom step | `TestModule_Navigation` | P1 | integration |
| HOOK-015 | A hook taking a second connection fails the nestcheck gate | `TestModule_NoNestedTx` | P1 | integration |

A test-only `docscreentest.Hooked` DocType exercises every hook, on the real engine and real AUTH: no lenient double (CD-125). The overflow sweep needs nothing new for steps, because `Screens` visits every `FormSteps` slug and `screens.Uncovered` fails an unlisted route. Each module adds its `/modules/…` screens, and the gallery gets one hooked sample.

## 7. Questions for the head

1. **Who builds it.** `internal/ui` is AD-2's area. I recommend one MOD-0 task, High tier, after WP3 Task 2 has settled `Deps` wiring, with AD-2 or a fresh AD.
2. **Route prefix.** `/modules/<module>/…` is my default. Any other prefix is fine as long as it stays off `/doc/`.

## 8. Work outline (MOD-0)

| # | Task | Tier | Lane |
|---|---|---|---|
| 1 | `module.go`, `Deps.Modules`, `docscreentest.Hooked`, HOOK-001 | High | Serial first |
| 2 | Step Body and Save, first step, labels, Hide | High | A |
| 3 | Line page and `LineCard` | High | B |
| 4 | Check, Page and Reason extras, `LineCard.Note` | Standard | C |
| 5 | Gallery sample; `make generate`; overflow on hooked pages | Light | Last |

`CloseRight` touches `actions.go` and `reasons.go`, so sequence it with task 4. Tasks 2 to 4 share a package, so each runs in a lane worktree. Run the plan check in `docs/engineering-lessons.md` §1 first: `main` moves quickly here, and CD-233 changed `Deps` and `form.go` today.
