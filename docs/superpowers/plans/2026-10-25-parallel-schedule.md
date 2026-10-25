# Parallel schedule for core v1 (Waves 1–3)

Owner request, 25 Oct 2026 (CD-123). Work starts when the packages it needs are on `main`, not when a whole wave ends. Each plan's own parallelism table still governs the order inside a WP. This file adds only the cross-WP gates, the milestone merges that open them, and how to run parallel tasks safely.

## 1. Rules

1. **Gates are packages, not waves.** A task may start when every package it consumes is on `main` at the needed milestone (section 2). The plans' "after Wave 1 merged" lines are replaced by the gates below; nothing else in the plans changes.
2. **Milestone merges.** An AD sends `MERGE-READY <milestone>` when the milestone's tasks are committed and `make ci` is green on its branch. The head director merges it on its own (one merge at a time, `make ci` on `main`), then tells every AD to rebase. Unfinished parts keep their WP0 stubs (CD-111), so a partial WP is safe on `main`.
3. **Lanes for tasks in the same Go package.** Two tasks the plan marks parallel but that sit in the same Go package must not share a worktree: one half-written file breaks the other's build and tests. The AD gives the second task a lane: `git worktree add ../it-<wp>-<task> -b lane/<wp>-<task> wp/<wp>`, plus `ln -s ../internaltools/bin ../it-<wp>-<task>/bin`. On approval it commits there and merges the lane into `wp/<wp>`, then removes the worktree. Tasks in different packages share the WP worktree.
4. **Generated files are regenerated, never merged by hand.** On any conflict in `*_templ.go`, `**/dbq/`, `internal/ui/static/*` or `internal/ui/assets_gen.go`, take either side, run `make generate`, and commit the result. Every UI lane changes the hashed CSS, so this happens on every UI lane merge.
5. **No UX design gate for core v1.** Protocol §9: the design-system spec is the approved design for core screens (UI, area screens and master screens), so no screen task waits for the owner.

## 2. Milestones and what they open

| Milestone | Tasks on `main` | Opens |
|---|---|---|
| NUM-1a | NUM 1–5 (`types`) | APR 2+, everyone's formatters. **Merged a9cb2f1.** |
| AUTH-M1 | AUTH 1–6 (`auth.Service` incl. `Scope`) | MST `msttest` (real AUTH), APR/DOC switching from `authfake` |
| DOC-M1 | DOC 1–12 (registry, audit, engine with links and `References`) | MST Stage B onward (delete checks need `References`), NAM 16 and APR 12b (with NAM merged) |
| UI-M1 | UI 1–8 (tokens, lint, icons, contrast, screen registry, shell, form and display components) | AUTH 19–21, NAM 18, APR 11 screens (CD-88: they import only `ui/layout` and `ui/components`). They follow UI 16's component map when it lands; the reviewer checks them against it. |
| UI-M2 | UI 1–17 (DataList, Picker, Wizard, docscreen, gallery) | MST 22 screens, WP3 Task 3 (area templates onto UI components) |
| NUM-M2 | NUM 8–12 (`list`, `csvio`) | MST 20–21 importers |
| FILE-P | FILE print track 10–14 | MST company letterhead wiring, WP3 Task 2 |
| WP done | each WP's done rule | WP3 Task 1's suite for that package |

## 3. Lanes per work package (remaining tasks)

`→` runs in order, `∥` runs at once (lane if same package), `[gate]` waits for a milestone. Tasks already committed are left out.

| WP | Lanes | Peak width |
|---|---|---|
| AUTH | 5 ∥ 7 ∥ 8 ∥ 9 → 6 → 10 → (11 ∥ 13 ∥ 14) → (12 ∥ 15) → (16 ∥ 17 ∥ 18) → [UI-M1] 19 ∥ 20 ∥ 21. MERGE-READY AUTH-M1 after 6. | 4 |
| UI | 4 ∥ 5 ∥ 6 ∥ 7 ∥ 8 → MERGE-READY UI-M1 → (9 ∥ 10 ∥ 11) → 12 → 13 → 14 → 15 → 16 → 17 → MERGE-READY UI-M2. Pay the owed light review of 1–3 in the first batch. | 5 |
| NUM | 6 → 7 (gstcalc) ∥ 8 → 9 (list) ∥ 10 → 11 (csvio) → 12; then 13 ∥ 14 ∥ 15. MERGE-READY NUM-M2 after 12. | 3 |
| NAM | 8 ∥ 9 ∥ 11 → 10 → (12 → 15) ∥ 13 ∥ 14 → [DOC-M1 + FILE done] 16 → 17 → [UI-M1] 18 | 3 |
| APR | 2 → (3 ∥ 4 ∥ 5) → 6 → 7 → (8 ∥ 9 ∥ 10 ∥ 12a) → 11 [UI-M1 for its screens] → 14 ∥ 15 ∥ 16 ∥ 17 → [DOC-M1 + NAM done] 12b ∥ 18 → 19 | 4 |
| DOC | 6 → (7 ∥ 8) → (9 ∥ 10) → (11 ∥ 12) → MERGE-READY DOC-M1 → 13 → 14 → 15 ∥ 16 ∥ 17 | 3 |
| FILE | files: 5 → 6 → 9, 7 (after 4) → 8 (after 5, 7) · print: 10 ∥ 11 → 12 → 13 → 14 → MERGE-READY FILE-P · then 15, 16 | 4 |
| MST (WP2) | **now:** 2 gstin ∥ 1 mstkit rules (fakes only; `msttest` waits) → [AUTH-M1 + DOC-M1] 1 `msttest` → 3 → Parties 4→5→6→7 ∥ Catalogue 8→9→10→11 ∥ Organisation 12→13→14 → 15 → 16 ∥ 17 ∥ 18 ∥ 19 → [NUM-M2] 20 ∥ 21 ∥ [UI-M2] 22 → [all Wave 1 done] 23 → 24 ∥ 25 ∥ 26 ∥ 27 → 28 | 4 |
| WP3 | **early:** Task 1's suite for each package as that WP is done (one lane per package); Task 6 Steps 1 and 3 (deploy files, `docs/operations.md` draft) now. **After everything:** 2 → 3 → 4 ∥ 5 → 6 Steps 2, 4, 5 (restore drill, final gate, tag `core-v1`) | 3 |

The longest chains are UI (about 10 steps to UI-M2) and AUTH (about 8 steps), so their ADs get first claim on spare workers.

## 4. Sessions and worker budgets (15 agents)

| Session | Runs | Workers |
|---|---|---|
| Head director | milestone and WP merges, CD log, `PROGRESS.md`, escalations | 2 |
| AD-1 | AUTH | 3 |
| AD-2 | UI | 3 |
| AD-3 | NUM, NAM, APR | 3 |
| AD-4 | DOC, FILE | 3 |
| AD-5 | MST (WP2) and early WP3 work, in `../it-masters` on `wp/masters` | 1 |

An AD with idle workers may lend them: it tells the head director (`ESCALATE <AD> - CROSS-WP: lend N`), and the head director gives the slots to the AD on the longest chain. WP3's late tasks (2–6) are run by the head director once AD-5's work and every WP are merged.

## 5. Gotchas

- Rebase after every milestone merge, then `make generate` and commit `dbq/` if it changed (project `CLAUDE.md`).
- `.golangci.yml` in each worktree lags `main`'s gosec test exclusions until rebased: rebase before the first hand-in lint.
- A lane branched before a rebase needs the same rebase before it merges back.
