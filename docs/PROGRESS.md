# Progress

Status on 25 October 2026: **specs, tests and plans complete; crew harness built and verified (CD-101). App build paused at WP0** (Tasks 1–2 done, 3/7/7b partial on branch `wip/wp0-start`, unreviewed). Next: resume WP0 under the harness.

## Read in this order

1. `docs/specs/2026-10-24-core-platform-spec.md`: the core platform (stack, architecture, §5 Go contracts, parallel work packages).
2. `docs/specs/2026-10-24-design-system.md`: front-end spec (mobile-first, light only, no horizontal scroll, wizards). Visual preview: https://claude.ai/artifact/BmUKkSr1GtsLED2KAbyDtn
3. `docs/specs/contract-changes.md`: decision log CD-01 to CD-101; the spec already includes them.
4. `docs/tests/core/README.md`: 722 test cases (556 P1, 166 P2) across `01-` to `07-*.md`; fixtures in `00-fixtures.md`.
5. `docs/superpowers/plans/`: one plan per work package.
6. `docs/superpowers/plans/2026-10-25-execution-protocol.md`: how tasks are run (director, roles, risk tier per task, review cadence; CD-96), and the design stage every new screen goes through first (`docs/ux/TEMPLATE.md`, CD-97).

## Execution order

| Wave | Plans | Notes |
|---|---|---|
| 0 | `wp0-foundation` | Contract stubs and fakes for all of §5; tag `wp0-foundation` |
| 1a | `wp1-num` Tasks 1–5 (`internal/types`) | Merge first (CD-53) |
| 1 | `wp1-num` (rest), `wp1-auth`, `wp1-naming`, `wp1-doc`, `wp1-approval`, `wp1-files-print`, `wp1-ui` | Parallel, one git worktree each; screen tasks that need UI start after `wp1-ui` merges |
| 2 | `wp2-masters` | After Wave 1 |
| 3 | `wp3-integration` | Wiring, adapters, every `TestE2E_*`, overflow/axe/CSP sweep, release; tag `core-v1` |

Work-package done rule: spec §9 rule 3 (CD-40). Check with `go run ./scripts/checktestids <AREA> --exclude-level e2e`.

## Open before execution

- Product owner reviews the specs and plans. Execution method: the execution protocol (CD-96); the director records each task's tier, times, attempts and findings in a task table here.
- Local Go is 1.25.5; the spec needs 1.27.3 (`go` downloads it through the toolchain directive).
- Git repository: https://github.com/algotradingfervid/paperweight (public). WP0 Task 1 skips `git init`.
- The Common Core Blueprint artifact (https://claude.ai/artifact/2LtWD6ytFvrZJYvW2786y7) still recommends Frappe; the decided stack is Go + templ + htmx + SQLite.

## Task table (execution protocol §7)

Times are local (IST). Findings are blocker / major / minor from the task review.

| WP | Task | Tier | Implementer | Start | End | Attempts | Findings | Commit |
|---|---|---|---|---|---|---|---|---|
| WP0 | 1 Toolchain | Standard | crew-implementer | — | 14:48 | 1 | 0/0/0 | c2af591 |
| WP0 | 2 Clock and errors | Standard | crew-implementer | — | 14:49 | 1 | 0/0/0 | 47846d1 |
| WP0 | 3 Database | High | crew-test-writer, crew-implementer | — | 14:55 | 1 | 0/0/2 (notes, no change) | 66e7538 tests, 5c0e570 |
| WP0 | 7 Test-ID checker | Standard | crew-implementer | — | 14:53 | 1 | 0/0/3 fixed | 63afa81 |
| WP0 | 7b Brand config | Standard | crew-implementer | — | 14:56 | 1 | 0/0/5 (4 fixed) | d7a995b |
| WP0 | 4 Web platform | High | crew-test-writer, crew-implementer | 14:54 | 15:04 | 1 | 0/1/3 (all fixed) | ac7158a tests, 9959bcb |
| WP0 | 5 Contracts and fakes | High | crew-test-writer, crew-implementer | 14:54 | 15:12 | 1 | 0/1/5 (4 fixed, 1 kept as CD-105) | e8a49e8 tests, 23bd4f1 |
| WP0 | 6 Test kit | High | crew-test-writer, crew-implementer | 14:55 | 15:14 | 1 | 0/0/3 (2 fixed, 1 doc fix CD-106) | 4e12eca tests, 7adeaaf |
| WP0 | 8 Command and CI gate | Standard | crew-implementer | 15:16 | 15:18 | 1 | 0/0/4 fixed | 4496ad2 |
| WP1-NUM | 1 types rounding and 128-bit | High | crew-test-writer | 15:24 | | | | |
| WP1-AUTH | 1 Package foundation | High | crew-test-writer | 15:24 | | | | |
| WP1-NAM | 1 Schema, skeleton, errors, FY seam | High | crew-test-writer | 15:24 | | | | |
| WP1-DOC | 1 DocType registry | High | crew-test-writer | 15:24 | | | | |
| WP1-APR | 1 Schema and queries | High | crew-test-writer | 15:24 | | | | |
| WP1-UI | 1 Foundation | Light | crew-implementer | 15:24 | | | | |

## Wave 1 carry-overs (put in the named task's brief)

- APR Tasks 2+: wait for NUM Tasks 1–5 on main (CD-53), then rebase wp/approval.
- AUTH Task 7: `users.updated_by` FK has no ON DELETE; the delete path must report `ReferencedUserError`, not a raw FK error.
- AUTH Task 17: a rights key outside the 11 known rights still reaches the DB CHECK (raw error); the form handler must reject it.
- DOC Task 6: the engine must return `normalized.err` (a failed MasterLookup) before using `checkFields`' errors, and test it with a failing lookup; `fixture`'s local `refused(msg)` must call `doc.Refused` once Task 6 creates it (Task 13 needs `KindRefused`); NAM `render` doesn't range-check the counter, so `Assign` must refuse overflow first.
- DOC Task 6: share one timestamp layout with `audit` (`timeLayout` is private there); `doc.NewEngine` calls `doctype.CheckRegistry()` (CD-107).
- NAM Task 6: reuse or replace `GetSeries` (one series lookup); drop `//nolint:unused` on names that become used (catalogue block, staticPrefix, fiscalYear).
- UI Task 2: lint allows `white-space: nowrap` and `1px` in `visually-hidden` too; allow the raw colours in the appended brand block in `static/` (CD-112).
- UI Task 6: no `data-theme` below `<html>` (CD-112); `.grid` shadows Tailwind's built-in `grid`.
- NUM plan: Task 1's fuzz seed comment (k=1 → unit 100, not 10) is wrong; fix the plan text when convenient.
- NAM Task 3: code done but uncommitted; run its hand-in and review after NAM Task 5's code lands (counter_test.go needs next/readLast to build).
- FILE Task 3: FILE-001's ListEntry time needs NUM's real `types.FormatIST`; green only after NUM merges and wp/files-print rebases.
- FILE Task 5: delete the placeholder Remove/Open/List/CopyAll/DeleteAll in store.go when adding read.go/remove.go.
- Every WP's worktree copy of the plans/test docs predates CD-107..114: name the CDs in briefs until each branch rebases onto main.
- Merges: both DOC and NAM edit `internal/contracts_test.go` (CD-111 rows); expect a small conflict.

## Wave 1 setup (25 Oct 2026)

Worktrees `../it-<wp>` on `wp/<wp>` from `main` 8b89c73 (num, auth, naming, doc, approval, files-print, ui); `bin/tools` is symlinked into each. Each WP runs its tasks in plan order; WPs run in parallel, at most 6 workers at once. NUM Tasks 1–5 merge to main first (CD-53); other WPs then rebase onto it.

## WP0 timing (crew report, 25 Oct 2026)

Tasks 3–8 ran 14:51–15:18 IST with up to three workers at once. Worker time 50.5 min across 16 workers. Medians: implementer 4.6 min, test-writer 3.5 min, reviewer 1.1 min. Slowest step: the Task 5 implementer (10.6 min, plus one fix round for a Review Focus 6 gap). Next to optimise: split large contract tasks by package so implementers run in parallel. Findings: 2 majors (Task 4 non-ASCII HX-Trigger, Task 5 approval protocol), both fixed in attempt 1; no task needed attempt 2.
