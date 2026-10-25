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
| WP1-AUTH | 4 authtest World | High | crew-test-writer, crew-implementer | | 16:12 | 1 | minors fixed | 330aff7 tests, 6de2f30 |
| WP1-NAM | 6 EnsureSeries and Assign | High | crew-test-writer, crew-implementer, crew-reviewer | | 16:21 | 1 | 0/0/1 (to Task 11) | af71b5e tests, a8b95a1 |
| WP1-NUM | 5 Dates, FY, IST time | High | crew-test-writer, crew-implementer, crew-reviewer | | 16:24 | 1 | 0/0/2 (carry-over) | 9a0dd85 tests, 7070525 |
| WP1-NUM | Tasks 1–5 merged to main (CD-53), `make ci` green | — | director | | 16:27 | — | — | a9cb2f1 |
| WP1-DOC | 4 Test doubles | High | crew-test-writer, crew-implementer, crew-reviewer | | 16:29 | 1 | 0/0/2 (1 fixed, 1 carry-over) | ced329d tests, 70898c8 |
| WP1-UI | 2 UI lint test | Light | crew-implementer | | 16:31 | 1 | review batched | 1131fe4 |
| WP1-UI | 3 Icons | Light | crew-implementer | | 16:44 | 1 | review batched | f99ad58 |
| WP1-NAM | 7 Numbers across the lifecycle | High | crew-test-writer, crew-implementer, crew-reviewer | | 16:45 | 1 | 0/0/3 (CD-122) | dda9db5 tests, 2390fe7 |
| WP1-AUTH | 2 Password hashing and policy | High | crew-test-writer, crew-implementer, crew-reviewer | | 16:50 | 2 | 0/2/3 (bounded strict PHC parsing; all fixed) | ef71d3a tests, 2d7795f |
| WP1-FILE | 4 Attach rules | High | crew-test-writer, crew-implementer, crew-reviewer | | 16:20 | 1 | 0/0/2 (no change; CD-120) | ca5c886 tests, 36f8cd9 |
| WP1-NAM | 8 Concurrent approvals, preview | High | crew-test-writer, crew-implementer, crew-reviewer | 16:28 | 16:31 | 1 | 0/0/2 (carry-over to Task 13) | 9f33ee4 tests, b385a8c |
| WP1-NAM | 9 Amendment numbers | High | crew-test-writer, crew-implementer, crew-reviewer | 16:31 | 16:36 | 1 | 0/1/1 (raw SQL to dbq fixed; minor to Task 13) | 0913f0a tests, 0419e3c |
| WP1-NAM | 11 Admin settings (lane/naming-11) | High | crew-test-writer, crew-implementer, crew-reviewer | 16:34 | 16:41 | 1 | TEST-DISPUTE (PUR/ over cap, DECISION escalated); 0/0/5 carried | ba8a4de, 48771bd tests, 624f532, merge e0411d1 |
| WP1-NAM | 10 Gapless invariant fuzz | High | crew-implementer (test-only), crew-reviewer | 16:36 | 16:44 | 1 | 0/0/2 (carried to Task 13) | 80883e9 |
| WP1-NAM | 12 P2 template, counter input (lane/naming-12) | High | crew-test-writer, crew-implementer, crew-reviewer | 16:43 | 16:45 | 1 | 0/0/2 (carried to Task 15) | fa638d2 tests, 5aa7187 |
| WP1-NAM | 13 P2 counter, assign, tx edges | High | crew-test-writer, crew-implementer, crew-reviewer | 16:45 | 16:53 | 1 | 0/0/4 (to follow-up) | 261161f tests, c7f9a8a |
| WP1-NAM | 14 P2 amendment edges (lane/naming-14) | High | crew-test-writer, crew-implementer, crew-reviewer | 16:45 | 16:49 | 1 | 0/0/2 (k<1 internal error fixed; harness move to Task 16) | dc3f3b7 tests, 0de3270 |
| WP1-NAM | 15 P2 admin edges (lane/naming-15) | High | crew-test-writer, crew-implementer, crew-reviewer | 16:46 | 16:53 | 1 | 0/0/3 (fixed) | 0f6c7e8 tests, 36cacdd |
| WP1-NAM | 17 WP completion check | Standard | crew-implementer (haiku) | 16:54 | 16:56 | 1 | checkids NAM 47/47 | a0ebe80 |
| WP1-UI | Rebase onto main d17fb3a | — | AD-2 | 16:28 | 16:28 | — | — | 0a0416e |
| WP1-UI | Batched review Tasks 1–3 | Light | crew-reviewer opus | 16:30 | 16:31 | 1 | 0/0/2 (fixes with Task 4) | — |
| WP1-UI | 6 App shell (lane/ui-6) | Light | crew-implementer opus | 16:30 | 16:38 | 1 | review batched (4,6,7) | 16e6701, merge b9dd19e |
| WP1-UI | 7 Form components (lane/ui-7) | Light | crew-implementer opus | 16:30 | 16:38 | 1 | review batched (4,6,7) | d44908d, merge 854f1ae |
| WP1-UI | 4 Contrast test + review 1–3 minors (wp/ui) | Light | crew-implementer opus | 16:31 | 16:34 | 1 | review batched (4–8) | e127e0d |
| WP1-UI | 8 Display components (lane/ui-8) | Light | crew-implementer opus | 16:34 | 16:42 | 1 | review batched (5,8) | 4d17538, merge 5c51bd8 |
| WP1-UI | 5 Screen registry, browser helpers (lane/ui-5) | Light | crew-implementer opus | 16:34 | 16:41 | 1 | review batched (5,8) | a223e63, merge bb8267e |
| WP1-UI | Batched review Tasks 4, 6, 7 | Light | crew-reviewer opus | 16:38 | 16:44 | 1 | 0/1/5 (Task 6 flash→Banner; fixes in 6-fix) | — |
| WP1-UI | gofmt contrast_test.go (lint-only, AD) | — | AD-2 | 16:41 | 16:41 | — | — | 4728966 |
| WP1-UI | 6/7 review fixes (wp/ui) | Light | crew-implementer opus | 16:44 | 16:48 | 1 | — (re-check pending) | 17a4eb8 |
| WP1-UI | 8-fix ActionBar via Button (lane/ui-8b) | Light | crew-implementer opus | 16:44 | 16:46 | 1 | — | 5a61281, merge 11eb207 |
| WP1-UI | 9 DataList/LineList tests (lane/ui-9) | High | crew-test-writer opus | 16:44 | 16:48 | 1 | — | fe232d9 |
| WP1-UI | 10 Picker, upload (lane/ui-10) | Light | crew-implementer opus | 16:44 | 16:52 | 1 | review batched (10,11) | 234a9a4 |
| WP1-UI | 11 Wizard framework (lane/ui-11) | Light | crew-implementer opus | 16:44 | 16:52 | 1 | review batched (10,11) | 96193b6 |
| WP1-UI | 9 DataList/LineList code (lane/ui-9) | High | crew-implementer opus | 16:48 | 17:01 | 1 | review pending | c8485f4 |
| WP1-UI | Batched review Tasks 5, 8 | Light | crew-reviewer opus | 16:47 | 16:52 | 1 | 0/1/6 (server Toast levels/dismiss) | — |
| WP1-UI | M1 precheck make ci + checkids (11eb207) | — | crew-verifier haiku | 16:50 | 16:52 | — | PASS | — |
| WP1-UI | 5–8 review fixes (lane/ui-fix) | Light | crew-implementer opus | 16:53 | 16:55 | 1 | — | cae040d |
| WP1-UI | Batched review Tasks 10, 11 | Light | crew-reviewer opus | 16:52 | 16:56 | 1 | 0/3/5 (Task 10 hand-written Button/Form, multiple; Task 11 approved) | — |
| WP1-UI | Re-check majors (Task 6 flash, Task 8 Toast) | Light | crew-reviewer opus | 16:55 | 16:57 | 1 | APPROVE; 2 minors left | — |
| WP1-UI | 10-fix (lane/ui-10) | Light | crew-implementer opus | 16:56 | 16:59 | 1 | — | 826c874 |
| WP1-UI | 11-fix conflict focus (lane/ui-11) | Light | crew-implementer opus | 16:56 | 16:58 | 1 | — | ad30a03 |
| WP1-UI | UI-M1 make ci + checkids UI (cae040d) | — | AD-2 | 16:57 | 16:57 | — | PASS; MERGE-READY sent | cae040d |
| WP1-UI | 8-minors (#alerts dismiss, MoreActions links) (lane/ui-fix) | Light | crew-implementer opus | 16:57 | 17:01 | 1 | — | 79ecb85, merge 97ece6a |
| WP1-UI | Re-check Task 10 majors | Light | crew-reviewer opus | 16:59 | 17:01 | 1 | APPROVE | merge cd4d22e |
| WP1-UI | Review Task 9 (High) | High | crew-reviewer opus | 17:01 | 17:05 | 1 | 0/3/6 (grid actions, cell inputs, Ctrl+Enter loses edit) | — |
| WP1-UI | 12 docscreen list (lane/ui-12) | Standard | crew-implementer opus | 17:01 | | | | |
| WP1-UI | 9-fix tests (lane/ui-9) | High | crew-test-writer opus | 17:05 | | | | |
| WP3 | 6 early (Steps 1, 3: deploy files, operations draft) | Standard | impl opus, fix haiku, rev opus ×2 | 16:57 | 17:13 | 1 | 0/1/5 (all fixed) | 1d9d1c4 |
| Merges | NAM done caad6e0; UI-M1 (Tasks 1–8) 7c3ef2f; WP3 early cb39476; `make ci` green after each | — | director | | 17:40 | — | — | — |

## Wave 1 handover to assistant directors (CD-121, 25 Oct 2026 16:55)

Nothing in flight; every worktree is clean. Next step per WP:

| WP | AD | Last committed | Next |
|---|---|---|---|
| AUTH | AD-1 | Task 2 (2d7795f); Tasks 1–4 done | Task 5 (Service: Can, UsersWithRight, …). Not rebased since the NUM merge; rebase first. Lint base `fe349d6~1`. |
| UI | AD-2 | Task 3 (f99ad58); Tasks 1–3 done | Task 4 (contrast). Light-tier batched review of Tasks 1–3 still owed. Not rebased since the NUM merge. |
| NUM | AD-3 | Tasks 1–5, merged to main (a9cb2f1) | Task 6 (gstcalc; removes its CD-111 stub row). |
| NAM | AD-3 | Task 7 (2390fe7) | Task 8 (concurrent approvals, preview). |
| APR | AD-3 | Task 1; Task 2 tests at cbe1141 | Task 2 implementer. Tests use `doctype.StatePending` (plan says `StatePendingApproval`); Anil's fixture email is `admin@vardhan.test`, not `anil@`. |
| DOC | AD-4 | Tasks 1–5 done (Task 4 at c61129b after rebase); Task 6 tests at e5de141 | Task 6 implementer: delete `fyShort`, add `doc.NumberFuncs` + `fixturetest.Numbers()/RefNumbers()` (CD-119), `fixture.go`/`receipt.go` per CD-118 (also commit them), go-cmp optional (CD-110). |
| FILE | AD-4 | Task 4 (68a9150); Task 5 tests at 12de7f6 | Task 5 implementer (FILE-029 then green); print track Tasks 10 ∥ 11 ready. |

## Wave 1 carry-overs (put in the named task's brief)

- APR Tasks 2+: wait for NUM Tasks 1–5 on main (CD-53), then rebase wp/approval.
- AUTH Task 7: `users.updated_by` FK has no ON DELETE; the delete path must report `ReferencedUserError`, not a raw FK error.
- AUTH Task 17: a rights key outside the 11 known rights still reaches the DB CHECK (raw error); the form handler must reject it.
- DOC Task 6: merge `normalize`'s and `checkFields`' errors by position (field, then line), not by appending one list to the other; the engine must return `normalized.err` (a failed MasterLookup) before using `checkFields`' errors, and test it with a failing lookup; `fixture`'s local `refused(msg)` must call `doc.Refused` once Task 6 creates it (Task 13 needs `KindRefused`); NAM `render` doesn't range-check the counter, so `Assign` must refuse overflow first.
- DOC Task 6: share one timestamp layout with `audit` (`timeLayout` is private there); `doc.NewEngine` calls `doctype.CheckRegistry()` (CD-107).
- NAM Task 6: reuse or replace `GetSeries` (one series lookup); drop `//nolint:unused` on names that become used (catalogue block, staticPrefix, fiscalYear).
- UI Task 2: lint allows `white-space: nowrap` and `1px` in `visually-hidden` too; allow the raw colours in the appended brand block in `static/` (CD-112).
- UI Task 6: no `data-theme` below `<html>` (CD-112); `.grid` shadows Tailwind's built-in `grid`.
- NUM plan: Task 1's fuzz seed comment (k=1 → unit 100, not 10) is wrong; fix the plan text when convenient.
- NAM Task 11: update `staticPrefix`'s `//nolint:unused` comment in template.go (it names Task 6; the user is Task 11).
- NAM Task 3: code done but uncommitted; run its hand-in and review after NAM Task 5's code lands (counter_test.go needs next/readLast to build).
- FILE Task 3: FILE-001's ListEntry time needs NUM's real `types.FormatIST`; green only after NUM merges and wp/files-print rebases.
- FILE Task 5: delete the placeholder Remove/Open/List/CopyAll/DeleteAll in store.go when adding read.go/remove.go.
- Every WP's worktree copy of the plans/test docs predates CD-107..114: name the CDs in briefs until each branch rebases onto main.
- DOC Task 4: `fyShort` in `fixturetest/doubles.go` is a stand-in for the `types.FiscalYear` stub; delete it when wp/doc rebases onto NUM.
- Worktree `.golangci.yml` predates the test-only gosec exclusions (G115, G404, G204, G702; main c8cad38 and after): copy main's file into a worktree when its tests hit them (done for wp/num).
- After the NUM merge, wp/approval, wp/naming, wp/files-print and wp/doc were rebased onto main (16:28); FILE-001 is green. wp/auth and wp/ui rebase between tasks.
- NUM-076 (committed test): an empty `ZONEINFO` still lets Go read `/usr/share/zoneinfo`, so part (b) doesn't prove the embedded tzdata; only the import check pins D-NUM-18. Strengthen it in a NUM P2 task.
- NUM: `types.Date`/`Timestamp` `Scan` refuse NULL; nullable columns need `sql.Null[types.Date]` at the call site.
- DOC Task 6+: the fixture double records `CheckValues` as `key=id1,id2 key2=id3` (keys sorted, ids in caller order); tests expecting several keys use this format.
- UI batch review: Task 2 stores templ fixtures as `.templ.txt`; `Allow` takes the enclosing selector; the fixed-width rule skips `@media`/`@container` (plan defect).
- AUTH Task 12 (account/password handlers): a corrupt stored hash returns plain `errBadHash` (500), not an HTTPError; map it there.
- AUTH Task 11: `go mod tidy` drops `golang.org/x/term` until `auth/cli` imports it; keep the hand-restored lines. Consider a `go mod tidy -diff` check in `make ci` after that.
- Merges: both DOC and NAM edit `internal/contracts_test.go` (CD-111 rows); expect a small conflict.

## Wave 1 setup (25 Oct 2026)

Worktrees `../it-<wp>` on `wp/<wp>` from `main` 8b89c73 (num, auth, naming, doc, approval, files-print, ui); `bin/tools` is symlinked into each. Each WP runs its tasks in plan order; WPs run in parallel, at most 6 workers at once. NUM Tasks 1–5 merge to main first (CD-53); other WPs then rebase onto it.

## WP0 timing (crew report, 25 Oct 2026)

Tasks 3–8 ran 14:51–15:18 IST with up to three workers at once. Worker time 50.5 min across 16 workers. Medians: implementer 4.6 min, test-writer 3.5 min, reviewer 1.1 min. Slowest step: the Task 5 implementer (10.6 min, plus one fix round for a Review Focus 6 gap). Next to optimise: split large contract tasks by package so implementers run in parallel. Findings: 2 majors (Task 4 non-ASCII HX-Trigger, Task 5 approval protocol), both fixed in attempt 1; no task needed attempt 2.
