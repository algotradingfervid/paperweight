# Execution protocol: director, roles and process by risk

Applies to every plan in this folder (CD-96). Where a plan's "Execution handoff" or header says to review after every task, the review cadence below wins; everything else in the plan (tasks, tests, Review Focus, Global Constraints) is unchanged.

Sections marked **(general)** are not specific to paperweight and can be reused in other projects as written. Sections marked **(paperweight)** hold this project's tiers, commands and paths.

## 1. Why this protocol exists (general)

PO-tato v2 (`~/Documents/POCreator`, 16–25 Oct 2026) used the same director-and-workers idea at maximum ceremony: every task, small or risky, went through a test-writer, a test-reviewer, a coder and an Opus reviewer, each starting fresh. Measured from its git history and `.reviews/`:

- the median task took 2.0 h from acceptance tests to merge, a quarter took over 4.6 h, and the slowest took 9–16 h;
- 159 of 401 review reports asked for changes, and most tasks needed a second round;
- of the rejection reasons that could be classified, coverage and `v8 ignore` items (77), missing docs (31) and UI or a11y items (31) far outnumbered real logic bugs (13) and security issues (3);
- a "major" finding of any kind restarted the cycle with fresh agents, and the full suite with coverage ran three times per task (coder exit, reviewer, merge gate).

The fix is not fewer checks on risky code. It is: match the process to the risk, let tools catch what tools can catch, fix small findings in place, and run each check once.

## 2. Roles (general)

Only `haiku` and `opus` are used; never `fable`. Every dispatch sets `model` explicitly. The roles are the global crew-harness agents in `~/.claude/agents/`: `crew-implementer`, `crew-test-writer`, `crew-reviewer` (read-only), `crew-verifier` (`haiku`), `crew-researcher`, `crew-ux-designer` and `crew-ui-reviewer` (CD-101). Project settings for the harness are in `.claude/harness.json`.

| Role | Model | Does | Never |
|---|---|---|---|
| Director (the main session) | session model | Talks with the owner, breaks work down, assigns a tier, writes briefs, dispatches, reads status files and verdicts, runs commits and merges, logs decisions as CD entries, reports progress | Writes code or tests; reads whole diffs (it sends a reviewer); lets a worker change a CD, the spec or another WP's files |
| Implementer | `opus` (`haiku` for mechanical work: renames, copy, fixture data, moving files) | Builds one task test-first and hands in uncommitted changes | Commits; edits files outside the task's Files list |
| Test-writer (High tier only) | `opus` | Writes the task's named tests from the plan's values; they must fail before code exists | Sees or writes implementation code |
| Reviewer | `opus` | Reviews the change against the plan, spec, test cases and CD log; returns a verdict with findings by severity | Edits files; re-runs the whole suite (it reads the hand-in output, and may run one test to check a suspicion) |
| Verifier | `haiku` | Runs the hand-in commands and writes the exact output to the status file | Interprets or fixes failures |
| Researcher | `opus` | Answers an open question from primary sources (code, official docs, installed source) with citations | Decides; the director decides and logs it |

The director keeps its own context small: it works from the plan's task section, the status files and the verdicts, not from file contents. Anything that needs reading more than a couple of files goes to a worker.

## 3. Risk tiers (paperweight)

The director assigns a tier when it dispatches a task and records it in `docs/PROGRESS.md`. A task is **High** if any of these is true; otherwise it takes its WP's default from the table.

High triggers:
- money, quantity, rate or GST arithmetic, parsing or formatting (`internal/types`, `gstcalc`);
- the document state machine, approvals, numbering or amendment (`doc`, `approval`, `naming`);
- rights, scopes, restrictions, sessions, CSRF, sign-in, uploads (`auth`, `files`, `platform/web` security code);
- `db.Write`, transactions, `version` checks, CD-94 reads, anything with a concurrency test;
- a migration that changes an existing table;
- §5 contracts and their fakes;
- the task fixes or adds a test named in its plan's **Review Focus** list.

| WP | Default tier |
|---|---|
| WP0 Foundation | Standard (Tasks 3 `db` and 5 contracts are High) |
| WP1 NUM | High for `types` and `gstcalc`; Standard for `list` and `csvio` |
| WP1 AUTH, NAM, DOC, APR | High |
| WP1 FILE/PRINT | Standard (upload checks are High) |
| WP1 UI | Light (the line grid in Task 9 and the grid saves in Task 14, CD-95, are High) |
| WP2 Masters | Standard; screens that only call existing services are Light |
| WP3 Integration | Standard; Task 6 (release build, deployment and restore drill) is High |

When in doubt, the director picks the higher tier. After each wave it reviews the findings per tier (section 7) and may propose moving a WP's default up or down; the owner approves the change.

## 4. Flow per tier (general)

| Step | High | Standard | Light |
|---|---|---|---|
| Tests | Fresh test-writer writes the task's named tests; they must fail. The director commits them as `test(<scope>): <task> tests` | Implementer, test-first in the same agent | Implementer, test-first in the same agent |
| Code | Fresh implementer makes them pass; it may not change any `_test.go` file committed in the previous step | Same implementer | Same implementer |
| Hand-in check | Verifier runs section 5's commands once | Same | Same |
| Review | Fresh reviewer, every task, tests and code together | Fresh reviewer, every task | One reviewer per batch: up to 5 Light tasks, or the plan's step group, whichever ends first |

High-tier test guard: after the code step, `git diff --name-only <tests commit> -- '*_test.go'` must print nothing. A changed test fails the hand-in. If the implementer believes a test is wrong, it stops with `TEST-DISPUTE` (section 6); the director sends the test, the case and the spec section to a fresh `opus` reviewer, which rules.

### Findings and the fix loop

Severity:
- **Blocker or major:** the behaviour differs from the plan, a test case or a CD entry; risk of data loss or a wrong number, amount or state; a security or concurrency gap; a test that would pass with an empty implementation or doesn't assert what its case requires; an import or §5 contract rule broken.
- **Minor:** anything else (naming, comments, small duplication, clarity).
- **Not a finding:** anything `make ci` already fails on (lint, format, generated code, race detector, test IDs). When a reviewer sees a class of problem a tool could catch, it says so in one line, and the director decides whether to add a lint rule (logged as a CD).

Fix loop:
- Minors: the same implementer fixes them in the same attempt; the verifier re-runs the hand-in check; no new review.
- Blockers and majors: the same implementer (resumed with its context) fixes them, then the same reviewer (resumed) re-checks only those findings. That is one attempt.
- If the re-check still finds a blocker or major, attempt 2 starts with a fresh implementer and the review report. After attempt 2 the director stops, re-diagnoses with a researcher if needed, and re-plans the task.

## 5. Checks: run each once (paperweight)

| When | Who | Command | Result goes to |
|---|---|---|---|
| While building | Implementer | `go test -race ./<package>/...` for the package it is working in | its own loop |
| Hand-in, every task | Verifier | `make lint` and `go test -race ./<the WP's owned dirs>/...` | the task's status file, verbatim |
| WP done | Verifier | `make checkids AREA=<AREA>` (spec §9 rule 3) | status file and `docs/PROGRESS.md` |
| Merge to main | Director | rebase onto main, then `make ci` | `docs/PROGRESS.md` |
| Wave end and WP3 | Director | `make e2e` | `docs/PROGRESS.md` |

The reviewer reads the hand-in output and does not repeat it. A failing hand-in goes back to the implementer in the same attempt and is not a review finding.

## 6. Briefs, status files and returns (general)

Every worker gets the same brief shape and nothing from the conversation:

```
TASK: paperweight <WP> <task> | ROLE: <role> | TIER: <tier>
Read <plan path> section "Task <n>" and follow it exactly. Also read: <spec sections>, <CD ids>, docs/superpowers/plans/2026-10-25-execution-protocol.md sections 4–6.
You may change only: <paths from the task's Files list>. Read nothing else unless the task names it.
Commands: <section 5 commands for this role>.
Status file: .work/status/<wp>-<task>.md. Append one line per step: time, step, result.
Do not commit. <role-specific rule, e.g. "Do not edit any _test.go file.">
Finish with exactly one of:
DONE: <one-line summary>
QUESTION: <question>: <what it blocks>
CONTRACT-CR: <change>: <reason>        (stop; §9 rule 2)
TEST-DISPUTE: <test>: <spec section>: <reason>
```

`.work/` is git-ignored (WP0 Task 1). The director reads status files to report progress; it does not poll on a timer, because it is told when a worker finishes.

Questions: work does not stop for the owner. The director sends an open question to a researcher, decides from the sources, and logs the decision as a CD entry with the sources. Only these wait for the owner: spending money, an irreversible action on the owner's systems, a new dependency, and a product decision the spec, test cases and CD log don't answer. Only the blocked task waits; others continue.

## 7. Records and tuning (general)

`docs/PROGRESS.md` keeps one row per task: task, tier, model per role, start, end, attempts, findings (blocker / major / minor), commit. After each wave the director reports, per tier: the median and slowest task time, the attempts, and what the majors were about. If a tier keeps producing majors of one kind, it proposes a lint rule, a test helper or a higher tier; if a High WP produces none over a wave, it may propose Standard. Changes to this protocol are CD entries.

## 8. Parallelism (paperweight)

- One worktree per WP on `wp/<name>` (spec §9 rule 1). Inside a WP, tasks run in parallel only as the plan's "Execution order and parallelism" table allows; two agents never edit the same file.
- At most 6 agents run at once across all WPs. Go tests use temp SQLite files, so there is no database-slot limit.
- Merges into main go one at a time, each with a green `make ci`.

## 8a. Speed rules (verified 25 Oct 2026)

Each rule is either checked on this machine or follows from the measurements in section 1.

| Rule | Why it is faster | Evidence |
|---|---|---|
| Hand-in tests run `go test -race ./<owned dirs>/...` **without** `-count=1`; only `make ci` at merge uses `-count=1` | Go caches passing package results, including `-race` runs, so unchanged packages finish instantly | `go help test` (cacheable flags; `-count=1` disables the cache); a test module showed `(cached)` on the second `-race` run |
| No per-worktree install step | Go's build and test cache is one per user (`go env GOCACHE` = `~/Library/Caches/go-build`) and is shared by every worktree; PO-tato ran `npm ci` in each | `go env GOCACHE` |
| Hand-in lint is `golangci-lint run --new-from-rev=<task base> ./<owned dirs>/...`; the whole repo is linted at merge | Lints only what the task changed | `golangci-lint run --help` (2.13.2 has `--new-from-rev`, `--whole-files`) |
| CSS builds take under 0.1 s | Tailwind standalone rebuilt the test stylesheet in 74 ms, so `make generate` is not a bottleneck | `docs/research/ui-library.md` §2 |
| Wave 1 runs its 7 WPs at once, each in its own worktree | Go tests use temp SQLite files, so there is no shared database or connection limit (PO-tato capped server-test tasks at 4) | spec §8, section 8 |
| Fixes resume the same implementer and reviewer instead of starting fresh agents | The agent keeps the files and reasoning it already loaded; a fresh agent re-reads the spec and plan | section 1: fresh agents per finding drove PO-tato's slowest tasks |
| Briefs point at one plan section and a file list, and say "read nothing else" | The plans already hold exact code-level steps, so an implementer needs little exploration | section 6 |
| Agent definitions (`~/.claude/agents/crew-*.md`) never change during a wave, and the task-specific part of each brief comes last | Anthropic prompt caching reuses an identical prompt prefix, so repeated dispatches of the same role are cheaper and start faster | Anthropic docs, prompt caching (prefix-based) |
| The director never polls; it waits for completion notices and reads status files only when reporting | No wasted turns | section 6 |
| `haiku` for the verifier and mechanical edits; `opus` only where judgement is needed | Faster and cheaper per call | section 2 |

After Wave 0 the director compares the PROGRESS timings with PO-tato's (median 2.0 h per task) and reports which step took longest in the slowest tasks; that step is the next thing to optimise.

## 9. UI and UX are designed before they are built (general)

Any feature that adds or changes a screen starts with a design stage, before its plan tasks are written or dispatched (CD-97). Code never decides a layout, a flow or a piece of wording on its own.

1. **Design.** A UX designer (`opus`) writes `docs/ux/<date>-<feature>.md` from `docs/ux/TEMPLATE.md`: who uses it and on what device, the jobs it serves, the flow, every screen in every state, phone layout first, the components used, the exact copy, and how each item on the template's checklist is met. It may only use components from the design-system catalogue (§7); a new component or pattern is a design-system change, proposed in the document and approved with it.
2. **Mock-up.** For a feature with more than one screen or any wizard, the designer also adds mock pages to the component gallery (`/_gallery`, WP1-UI) built from the real components with sample data, so the owner reviews the real look on a phone and a desktop, not a drawing. Before the gallery exists, a static HTML mock with the same tokens is used.
3. **UX review.** A fresh reviewer (`opus`) checks the document and mock against the template checklist and returns findings with the same severities as code review (section 4). Blockers and majors are fixed before the owner sees it.
4. **Owner approval.** The owner approves the design. Only then does the director write or release the plan tasks, and each screen task names the design document and the section it implements. A task that needs a screen not in an approved design stops with `QUESTION`.
5. **After build.** The task reviewer compares the screen with its design section. At each wave end and before a release, the director runs `/ui-review` (visual, contrast, sizes, accessibility on phone, tablet and desktop) and `/ux-review` (flows, findability) on the running app; critical and high findings become tasks.

For paperweight core v1, the design-system spec (§1–§10) and `docs/research/ux-patterns.md` are the approved design; the stage applies from the first feature after core (for example Expenses or Zoho Books sync) and to any core screen added later.

## 10. Reusing this in another project (general)

Copy sections 1, 2, 4, 6, 7 and 9 unchanged. Rewrite section 3 (what counts as High in that domain), section 5 (that project's lint, test and end-to-end commands) and section 8 (its branch layout and any resource limits, such as database connections). For non-code work (research, documents, data work), keep the director, briefs and status files, and replace tests with that work's check: sources cited and verified for research, a read-through by a fresh reviewer for documents.
