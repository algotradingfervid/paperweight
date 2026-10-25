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
| WP1-NUM | 6 gstcalc line, split, totals | High | crew-test-writer, crew-implementer, crew-reviewer | 16:28 | 16:33 | 1 | 0/0/4 (no change; plan names gstcalc.go, stub is contract.go) | 1e9fff3 tests, 62ab10c |
| WP1-NUM | 8 list WHERE clause | Standard | crew-implementer, crew-reviewer | 16:33 | 16:41 | 1 | 0/0/4 (to Task 9 brief) | 5e91d77 |
| WP1-NUM | 7 gstcalc place of supply, recompute | High | crew-test-writer, crew-implementer, crew-reviewer | 16:34 | 16:39 | 1 | 0/0/3 (carried) | 96f0fb1 tests, 4ac9b41 |
| WP1-NUM | 13 types P2 | High | crew-test-writer, crew-implementer, crew-reviewer | 16:34 | 16:38 | 1 | 0/0/4 (comment minors carried; FromMonthEnd reading to researcher) | 1c5bf2f tests, 4a9af3e |
| WP1-NUM | 13b DueDate month-end follows ERPNext (DECISION escalated) | High | crew-researcher, crew-implementer | 16:39 | 16:41 | 1 | director diff check | 97a7112 |
| WP1-NUM | 9 list sort, pages, service | Standard | crew-implementer, crew-reviewer | 16:43 | 16:50 | 1 | 0/1/4 (link-sort test proved by mutation; all fixed) | b393471 |
| WP1-NUM | 10 csvio read, headers | Standard | crew-implementer, crew-reviewer | 16:43 | 16:48 | 1 | 0/1/2 (row cap before full read, fixed; ParseError msg to Task 11) | 721871c |
| WP1-NUM | 11 csvio import | Standard | crew-implementer, crew-reviewer | 16:49 | 17:00 | 1 | 0/2/9 (unescape and savepoint untested; fixed, mutation-proven) | a8dde7b |
| WP1-NUM | 14 gstcalc P2 | High | crew-test-writer, crew-implementer, crew-reviewer | 16:51 | 16:54 | 1 | 0/0/3 (96 question escalated; IGST msg test carried) | 9513932 tests, 2d9688f |
| WP1-NUM | 12 csvio export | Standard | crew-implementer, crew-reviewer | 17:00 | 17:05 | 1 | 0/0/2 (OnLine header, missing link: note for plan) | 99e1377 |
| WP1-NUM | NUM-M2 merge check (make ci green, checkids 105/105) | — | crew-verifier | 17:08 | 17:07 | — | — | ec921b5 |
| WP1-NUM | 15 list and csvio P2 | Standard | crew-implementer | 17:07 | | | | |
| WP1-AUTH | rebase onto main d17fb3a | — | AD-1 | | 16:25 | — | — | dfef97b |
| WP1-AUTH | 9 Sessions | High | crew-test-writer, crew-implementer, crew-reviewer | 16:27 | 16:37 | 1 | 0/1/3 (upsert removed; uid error; ORDER BY; Sweep/AllCtx/absolute untested → Task 16) | 377e12b tests, 1af9ceb lint, ff3c0be |
| WP1-AUTH | 5 Service (Can …) | High | crew-test-writer, crew-implementer, crew-reviewer | 16:27 | 16:38 | 1 | 0/0/5 (3 code minors → Task 6 brief; Can-order/strict/create + ReadableDocTypes test gaps → Task 15/18) | ced63f7 tests, 51cef81 (incl. CD-125) |
| WP1-AUTH | 7 Users | High | crew-test-writer, crew-implementer, crew-reviewer | 16:27 | 16:43 | 1 | 0/1/9 (CD-94 hash re-check in own change; argon2 out of reset write; stored role names; ActionDelete; test found-flag; reset re-checks policy; rest → Tasks 13/17) | b32bf00 tests, f3157d7 |
| WP1-AUTH | 8 Sign-in and lockout | High | crew-test-writer, crew-implementer, crew-reviewer | 16:40 | 16:46 | 1 | 0/1/6 (CD-94 re-check in success write; conditional rehash; expired-window test; byte precheck; 3 test minors → Task 16) | a0c6676 tests, cb7a4af |
| WP1-AUTH | 6 Scope | High | crew-test-writer, crew-implementer, crew-reviewer | 16:39 | 16:49 | 1 | 0/1/5 (major: tests missed NULL-header line case → agreement test added; Applies parity; dedupe clause; lazy Directory; AUTH-069 Scope assert; site master shape) | ef6e5a1 tests, f563ae1 |
| WP1-AUTH | lanes 7, 8, 9 merged into wp/auth | — | AD-1 | | 16:49 | — | — | ae11dbd, a20bbd3 |
| WP1-AUTH | CD-137 db test fix; make ci green (AUTH-M1) | — | crew-implementer (haiku), crew-verifier | | 17:06 | 1 | — | a8f94a1 |
| WP1-AUTH | 10 Web core | High | crew-test-writer, crew-implementer, crew-reviewer | 16:50 | 17:10 | 2 | 0/4/9 (attempt 1: owner-field strip on parse error, client CSRF GET, guest session per request → CD-136 lazy CSRF; attempt 2: late token never saved → up-front for sessions with values + harness check; minors fixed) | bd37afa tests, 1cba802 lint, 082a7ec |
| Merges | NUM-M2 1ddda63 (contracts_test.go stub rows: both removed); AUTH-M1 71d58e9 + dbq regenerate; `make ci` green after each | — | director | | 17:55 | — | — | — |
| WP1-FILE | 5 Open, List, Remove, CopyAll, DeleteAll | High | crew-implementer opus ×2, crew-reviewer opus ×2 | 16:29 | 16:36 | 1 | 0/1/2 (CD-126 wording fixed; order fixed; CopyAll order untested, carry-over) | 3e33acc tests, a47287c |
| WP1-FILE | 10 Amount in words | High | crew-test-writer opus, crew-implementer opus, crew-reviewer opus | 16:29 | 16:38 | 1 | 0/0/1 (optional, skipped) | 474e282 tests, a714c27, merge 208df79 |
| WP1-FILE | 11 Print registry, page shell, status, watermark, title | Standard | crew-implementer opus, crew-reviewer opus, crew-implementer haiku (minors) | 16:32 | 16:38 | 1 | 0/0/2 (both fixed) | 8df07d3 |
| WP1-FILE | 7 Upload and remove handlers | High | crew-test-writer opus, crew-implementer opus ×2, crew-reviewer opus ×2 | 16:34 | 16:47 | 1 | 0/1/3 (all fixed: 201 body read in-tx via Store.Meta) | 6da6169 tests, b32ab99 lint, 913d44c |
| WP1-FILE | 6 Garbage collection | High | crew-test-writer opus, crew-implementer opus, crew-reviewer opus, crew-implementer haiku (minors) | 16:36 | 16:44 | 1 | 0/0/3 (2 fixed; FILE-020 race test is probabilistic, carry-over; skip path untested) | 371f511 tests, 88960cb |
| WP1-FILE | 12 Letterhead, trade template, golden prints | Standard | crew-implementer opus, crew-reviewer opus, crew-implementer haiku (minors) | 16:38 | 16:46 | 1 | 0/0/5 (3 fixed; Amends line kept; logo source escalated CD-58 vs CD-99) | 5ef2709 |
| WP1-FILE | 13 Print handler | High | crew-test-writer opus, crew-implementer opus, crew-reviewer opus, crew-implementer haiku (minors) | 16:47 | 16:55 | 1 | 0/0/4 (2 fixed; no-template 404 text and shared signedOut helper are carry-overs) | 87b89bd tests, 3479b79 |
| WP1-FILE | 8 Download handler | High | crew-test-writer opus, crew-implementer opus ×2, crew-reviewer opus | 16:48 | 16:55 | 1 | 0/0/3 (Cache-Control on 416/412 fixed; size check optional, skipped; mount point note for WP3) | 272c207 tests, 3494f44 |
| WP1-FILE | 9 Company logo policy | High | crew-test-writer opus, crew-implementer opus ×2, crew-reviewer opus | 16:55 | 17:01 | 1 | 0/0/2 (empty principal refused; missing-company test added) | 47d72ef tests, 7bc0955 |
| WP1-FILE | 14 Helpers for WP3 e2e tests | Standard | crew-implementer opus ×2, crew-reviewer opus | 16:55 | 17:05 | 1 | 0/0/3 (all fixed) + CONTRACT-CR CD-138 (import rule) | 057a4f3 CD-138, cc5764e |
| WP1-FILE | 15 P2 files cases (lane) | High | crew-test-writer opus, crew-implementer opus ×2, crew-reviewer opus | 17:02 | 17:17 | 1 | 0/0/4 (KindReceiptMismatch added; Check orphan grace skipped — run when idle, WP3 note) | 5d2a285 tests, 929868a (lane, not yet merged) |
| WP1-FILE | CD-140 letterhead logo fallback | Standard | crew-implementer opus ×2, crew-reviewer opus | 17:15 | 17:18 | 1 | TEST-DISPUTE: FILE-064 logo check follows CD-140 | bc21c33 |
| WP1-FILE | FILE-P rebase fixes | — | crew-verifier haiku, crew-implementer haiku | 17:10 | 17:17 | — | UI lint daisy-outside-ui: badge → doc_badge | b462e5a, ae36419 |
| WP1-FILE | 16 P2 print cases (lane) | Standard | crew-implementer opus | 17:18 | | | | |
| Merges | FILE-P (FILE 5–14) 3d66864; `make ci` green | — | director | | 18:05 | — | — | — |
| WP1-NUM | 15 list and csvio P2 | Standard | crew-implementer, crew-reviewer | 17:07 | 17:17 | 1 | 0/1/4 (error-rows header escaping fixed) | 9da0907 |
| Merges | NUM done (Tasks 1–15) 765248f; `make ci` green | — | director | | 18:15 | — | — | — |
| WP1-FILE | 15 P2 files cases (lane) | High | crew-test-writer opus, crew-implementer opus ×2, crew-reviewer opus | 17:02 | 17:17 | 1 | 0/0/4 (KindReceiptMismatch added; Check orphan grace skipped — run when idle, WP3 note) | 5d2a285 tests, ec0759f (cherry-picked onto rebased branch) |
| WP1-FILE | 16 P2 print cases | Standard | crew-implementer opus ×2, crew-reviewer opus, crew-implementer haiku (minors) | 17:18 | 17:28 | 1 | 0/0/4 (CSS form feed escape and page.css template check fixed) + TEST-DISPUTE upheld (linked print.js vs <script bans) | d4c4508 |
| Merges | FILE done (Tasks 1–16) d9a4c39; `make ci` green | — | director | | 18:40 | — | — | — |
| WP1-DOC | 6 Engine skeleton, create, Load, harness | High | crew-implementer opus, crew-reviewer opus, crew-implementer haiku (shared layout) | 16:28 | 16:42 | 1 | 0/0/2 (accepted; Task 7 must merge Validate errors like create) | 1cd911f tests, 4e1a4b9 |
| WP1-DOC | 7 Save an existing draft | High | crew-test-writer opus, crew-implementer opus, crew-reviewer opus, crew-implementer haiku (comments) | 16:42 | 16:54 | 1 | 0/0/5 (2 comment fixes; save rules sent as DECISION; reorder test owed) | f5c854f tests, 103bad4, merge bc5b7c2 |
| WP1-DOC | 8 Transition guard and refusal messages (lane) | High | crew-test-writer opus, crew-implementer opus, crew-reviewer opus | 16:42 | 16:52 | 1 | 0/0/3 + TEST-DISPUTE upheld (Sprintf on fixed texts; reopen wording pinned) | d4c00dd tests, 7c21489 test fix, a0e57f5 |
| WP1-DOC | 9 Submit, approve, reject, revise, withdraw | High | crew-test-writer opus, crew-implementer opus, crew-reviewer opus | 16:55 | 17:05 | 1 | 0/0/3 (msg consts move to messages.go after the 10 merge; number marker plain text → DECISION) | f736267 tests, 386c17a |
| WP1-DOC | 10 Close, cancel, delete, amend (lane) | High | crew-test-writer opus, crew-implementer opus ×2, crew-reviewer opus ×2 | 16:55 | 17:08 | 1 | 0/1/5 (helper clash with Task 9 fixed; msgs moved; shadowed `reason` var in reject left, carry-over) | ebeda78 tests, merge 2999620 |
| WP1-DOC | 11 Links, blockers, References | High | crew-test-writer opus, crew-implementer opus ×2, crew-reviewer opus | 17:07 | 17:26 | 1 | 0/0/5 (4 fixed: errors returned not panicked; 2+ and 11-blocker messages and header links untested → P2) | d07dad2 tests, e2f6d58 lint, 8805843 |
| WP1-DOC | 12 Roll-ups, effect audit, rollback (lane) | High | crew-test-writer opus, crew-implementer opus ×2, crew-reviewer opus ×2 | 17:07 | 17:34 | 1 | 0/1/2 (targets keyed by DocType+id; cause pinned) | c0dbe87 tests, 1535c02 (after rebase) |
| WP1-AUTH | 11 Sign-in, setup, CLI | High | crew-test-writer, crew-implementer, crew-reviewer | 17:10 | 17:23 | 1 | 0/2/6 (TEST-DISPUTE: AUTH-004 harness header, ruled by AD; setup-done before validation; CSRF rotation test; destroy-then-log; sqlc query; placement; CLI disable test) | 754b65b tests, eed43a1 ruling, 980bc1e |
| WP1-AUTH | 14 Probe and rights per action | High | crew-test-writer, crew-implementer, crew-reviewer | 17:10 | 17:23 | 1 | 0/1/6 (export guard read → CD-145; nosniff on files; addLine keys; CheckRoutes probe test) | a59725c tests, efa8ffa |
| WP1-AUTH | 12 Account and passwords | High | crew-test-writer, crew-implementer, crew-reviewer | 17:20 | 17:30 | 1 | 0/0/2 (password-echo test → Task 16; duplicate csrf input → Task 21) | 4d5759c tests, 5815cee |
| WP1-AUTH | 15 Own/team scope, restrictions, channels (tests only) | High | crew-test-writer, crew-reviewer | 17:20 | 17:32 | 1 | 0/1/3 (API list positive control; perm_version assert; AUTH-099 sorted → CD-149) | ef3b8d7, 2e9960f, 9159fd0 |
| WP1-AUTH | 13 User admin handlers | High | crew-test-writer, crew-implementer, crew-reviewer | 17:10 | 17:36 | 2 | 0/4/8 (plan gap → CD-143 service methods; CD-146 versioned web writes + restriction version bump + body parse before roles; flash harness gap → CD-150 web.WithSessions (prod double-save bug); no-roles form → empty list) | ce2aa68 tests, e1de3f0 ruling, 0e5f8c3 |
| Merges | CD-151 fix d32d5b5; DOC-M1 (DOC 1–12) 58465c3 (director finished the wp/doc rebase); AUTH done (Tasks 1–15) f75c813; `make ci` green after each | — | director | | 18:55 | — | — | — |
| Budgets | after DOC-M1: head 2, AD-1 5, AD-2 3, AD-3 7, AD-4 4, AD-5 4 (CD-124: 3 slots to AD-5 from finished NUM and FILE) | — | director | | 18:55 | — | — | — |
| WP1-APR | 2 Core types, errors, format, harness | High | crew-implementer, crew-reviewer | 16:28 | 16:39 | 1 | 0/2/2 (driver read leniency, missing submit/write checks; all fixed) | 4ec4115 tests, 267beeb |
| WP1-APR | 3 Rule administration | High | crew-test-writer, crew-implementer, crew-reviewer | 16:40 | 16:47 | 1 | 0/0/5 (carried); APR-013 green after Task 6 | 311c119 tests, af0f585 |
| WP1-APR | 4 Rule matching (lane/approval-4) | High | crew-test-writer, crew-implementer, crew-reviewer | 16:40 | 16:44 | 1 | 0/0/2 (dedup test carried) | 7eef4dc tests, dd00b12 |
| WP1-APR | 5 Eligibility (lane/approval-5) | High | crew-test-writer, crew-implementer, crew-reviewer | 16:40 | 16:45 | 1 | 0/0/2 (no change); test green after Task 6 | cb1f61d tests, 3b19718 |
| WP1-APR | 6 Evaluate and Open | High | crew-test-writer, crew-implementer, crew-reviewer | 16:48 | 16:54 | 1 | 0/0/4 (carried) | 44bde43 tests, ba87e5e |
| WP1-APR | 7 CheckApprover and Record | High | crew-test-writer, crew-implementer, crew-reviewer | 16:54 | 17:02 | 1 | 0/0/6 (to follow-up) | afcf8f8 tests, b18a82e |
| WP1-APR | 8 Inbox, My requests, status | High | crew-test-writer, crew-implementer, crew-reviewer | 17:02 | 17:11 | 1 | 0/1/6 (page overflow panic fixed; 2 minors fixed, rest carried) | 4d576f5 tests, def668b |
| WP1-APR | 9 Sequence, rollback, concurrency tests (lane/approval-9) | High | crew-implementer (test-only), crew-reviewer | 17:02 | 17:12 | 1 | 0/0/5 (carried to Task 16) | eaaebd4 |
| WP1-APR | 12a DOC-coupled tests, driver methods (lane/approval-12) | High | crew-implementer, crew-reviewer | 17:02 | 17:13 | 1 | 0/1/6 (APR-074 refusal exact; fixed); CD-142 cross-WP | e114be5 |
| WP1-APR | 10 Withdraw (R2) + CD-127/CD-142 test follow-ups | High | crew-implementer, crew-reviewer | 17:14 | 17:17 | 1 | 0/0/1 (count via Total, to Task 16) | 7861a25, f57e6eb |
| WP1-APR | 11 Inbox, mine, badge, rule screens | High | crew-test-writer, crew-implementer, crew-reviewer | 17:14 | 17:28 | 1 | 0/1/3 (forked error path removed; test reads page text by ruling) | 354d192, 96dcfd1 tests, 9d3de78 |
| WP1-APR | 15 Decision P2 (lane/approval-15) | High | crew-implementer, crew-reviewer | 17:14 | 17:19 | 1 | 0/0/2 (approverRole dedupe carried) | 783bbf8 |
| WP1-APR | 17 Fuzz invariants | High | crew-implementer (test-only), crew-reviewer | 17:18 | 17:32 | 1 | 0/0/3 (seed replay test to follow-up) | 1bf6e93 |
| WP1-APR | 14 Rules P2 (root part; APR-004 after Task 11) | High | crew-implementer, crew-reviewer | 17:19 | 17:23 | 1 | 0/0/2 (ApproverType 500→422 bug fixed) | 87b19c4 |
| WP1-APR | CD-147 missing-role message | High | crew-test-writer, crew-implementer (haiku) | 17:25 | 17:25 | 1 | ruling-driven | 4214c66 test, 08d1617 |
| WP1-APR | 14 Rules P2 web part (APR-004, version 400) | High | crew-implementer, crew-reviewer (with Task 17) | 17:29 | 17:30 | 1 | mutation-proven | 75f9993 |
| WP1-APR | 16 Stuck, inbox and history P2 (lane/approval-16) | High | crew-test-writer, crew-implementer, crew-reviewer | 17:29 | 17:39 | 1 | 0/1/3 (refusal text → CD-152, fixed) | 4c1962e, c17da31 tests, c6675ba |
| WP1-APR | 12b Engine driver | High | crew-implementer | 17:48 | | | | |
| WP1-APR | 18 DOC-coupled P2 (lane/approval-18) | High | crew-test-writer | 17:48 | | | | |
| Merges | APR done (P1 71/71) 8102873; `make ci` green; 12b and 18 follow | — | director | | 19:05 | — | — | — |
| WP1-NAM | 16 Cross-package P2 vs real DOC and FILE (+ harness amend carry-over) | High | crew-implementer (test-only), crew-reviewer | 17:45 | 17:52 | 1 | 0/0/4 (NAM-047 text → CD-155) | cf8828a |
| Merges | NAM Task 16 e2dd7d5; `make ci` green | — | director | | 19:20 | — | — | — |
| WP1-AUTH | CD-156 ids never reused | High | crew-implementer, crew-reviewer | | | 1 | 0/0/0 | 2faefcc |
| Merges | AUTH CD-156 5aecfe2; `make ci` green | — | director | | 19:35 | — | — | — |
| WP1-AUTH | CD-157 C authfake denial text | High | crew-implementer, crew-reviewer | | | 1 | 0/0/2 | b843525 |
| Merges | AUTH CD-157 C 7e2bf9b; `make ci` green | — | director | | 19:55 | — | — | — |
| Merges | UI CD-160 buffered Render 026383e; `make ci` green | — | director | | 20:05 | — | — | — |
| WP1-NAM | 18 Numbering screen | High | crew-test-writer, crew-implementer, crew-reviewer | 17:50 | 18:10 | 2 | 0/0/7 (flash bug → CD-160; 2 minors fixed, 1 reverted) | 3004445 b49667a aadb877 aa39db2 |
| Merges | NAM Task 18 95db286 (NAM complete); `make ci` green | — | director | | 20:15 | — | — | — |
| WP1-AUTH | CD-162 authfake CheckValues parity | High | crew-implementer, crew-test-writer, crew-reviewer | | | 1 | 0/1/2 (project row missing → added) | 81e51ee, 61111ae |
| Merges | AUTH CD-162 (merge below); `make ci` green | — | director | | 20:25 | — | — | — |
| WP1-DOC | 14 Lifecycle table and fuzz (lane 13) | High | crew-test-writer opus, crew-reviewer opus, crew-implementer opus (model fix) | 17:38 | 17:53 | 1 | 0/0/4 + major for product → CD-156 (ids reused) | 8fe78a3, 7f2f192 |
| WP1-DOC | 17 P2 audit values and history | High | crew-test-writer opus, crew-implementer opus, crew-reviewer opus, crew-implementer haiku (minors) | 17:40 | 18:03 | 1 | 0/0/3 (2 fixed; ListDeleted index deferred) + blocked on CD-156 | 99ed8d4 tests, 18d73a9 |
| WP1-DOC | CD-156 ids never reused (fixture + attachment) | High | crew-implementer opus, crew-reviewer opus | 17:52 | 18:00 | 1 | 0/0/0; make ci green on lane/doc-cd156 | 275f46b |
| WP1-DOC | CD-157 (A) reject reason to APR, (F) Receipt Amount | High | crew-implementer opus, crew-reviewer opus | 18:00 | 18:06 | 1 | 0/0/2 (double's Fail-before-reason order; plan text :319 :545 :572 stale → head) | fe22fbe (lane cd156) |
| WP1-DOC | CD-162 fixture read-only state field | High | crew-implementer opus, crew-reviewer opus | 18:06 | 18:13 | 1 | | 0d07e62 |
| WP1-DOC | CD-163 audit.ValidateEntry parity + files Op fix | High | crew-implementer opus, crew-implementer haiku, crew-reviewer opus | 18:08 | 18:13 | 1 | found files audit changes without Op (fixed) | d9454e7, 0599efa |
| Merges | DOC batch (CD-156, CD-157 A/F, CD-162, CD-163) ee93bd7; `make ci` green | — | director | | 20:35 | — | — | — |
| WP1-DOC | 13 Concurrency (lane) | High | crew-test-writer opus, crew-reviewer opus, crew-implementer opus (test tightening) | 17:38 | 17:49 | 1 | 0/0/3 (DOC-073 loser errors pinned; case text update proposed) | 2e4d607, bc699e6 |
| WP1-DOC | 15 P2 validation and line extras (lane) | High | crew-test-writer opus, crew-implementer opus, crew-reviewer opus | 17:40 | 17:52 | 1 | 0/0/4 (DOC-002 explicit-0 check can't fail: fixture has no non-zero default, carry-over) | 85e335b tests, 7d37ebc (lane 15) |
| WP1-DOC | 16 P2 lifecycle, links, tx extras (lane) | High | crew-test-writer opus, crew-reviewer opus, crew-implementer opus (test minors) | 17:40 | 17:55 | 1 | 0/0/4 (all tests passed on existing engine; no code needed; 4 test fixes) | 8fad957, ad555e4, merge 4171714 |
| WP1-DOC | Fixture harness template-DB speedup | Standard | crew-implementer opus | 17:55 | 17:58 | — | dropped: baseline already 6.6 s doc tests, ~85 fuzz execs/s; needs testkit change | — |
| Merges | DOC done (Tasks 1–17) 6d505af; `make ci` green | — | director | | 20:45 | — | — | — |
| Budgets | AD-4 finished (DOC, FILE): its 4 slots go to AD-5 (masters, now the longest chain): head 2, AD-1 5, AD-2 3, AD-3 7, AD-4 0, AD-5 8 | — | director | | 20:45 | — | — | — |

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
- WP3 (from FILE): mount files/web under a prefix (its NotFound says "File not found."); run `files check` when idle (in-flight uploads look like orphans); CopyAll order is untested; the `signedOut` helper is duplicated in files/web and print/web.
- NUM follow-ups (it-num/.work/carry-over.md): IGST tolerance-message test; inter-state CGST message wording; NUM-076 tzdata check.
- APR carry-overs: it-approval/.work/carry-over.md (read before WP3 Task 1's APR suite).
- WP3 Task 2: set `authweb.Config.Seed` to `auth.SeedOptions{LinkedMasterRead: true}` (CD-158, CD-10).
- DOC carry-overs: `audit` ListDeleted has no partial index on action='delete' (add in a new audit migration if the admin page slows; CD-117 sqlc list); DOC-002's explicit-0 check can't fail until a fixture field has a non-zero default; `audit_log.id` stays plain INTEGER PRIMARY KEY (its no-delete trigger prevents reuse).
- Merges: both DOC and NAM edit `internal/contracts_test.go` (CD-111 rows); expect a small conflict.

## Wave 1 setup (25 Oct 2026)

Worktrees `../it-<wp>` on `wp/<wp>` from `main` 8b89c73 (num, auth, naming, doc, approval, files-print, ui); `bin/tools` is symlinked into each. Each WP runs its tasks in plan order; WPs run in parallel, at most 6 workers at once. NUM Tasks 1–5 merge to main first (CD-53); other WPs then rebase onto it.

## WP0 timing (crew report, 25 Oct 2026)

Tasks 3–8 ran 14:51–15:18 IST with up to three workers at once. Worker time 50.5 min across 16 workers. Medians: implementer 4.6 min, test-writer 3.5 min, reviewer 1.1 min. Slowest step: the Task 5 implementer (10.6 min, plus one fix round for a Review Focus 6 gap). Next to optimise: split large contract tasks by package so implementers run in parallel. Findings: 2 majors (Task 4 non-ASCII HX-Trigger, Task 5 approval protocol), both fixed in attempt 1; no task needed attempt 2.
