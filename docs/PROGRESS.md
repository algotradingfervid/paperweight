# Progress

Status on 24 October 2026: **spec, tests and plans complete; awaiting product-owner review. No code written yet.**

## Read in this order

1. `docs/specs/2026-10-24-core-platform-spec.md`: the core platform (stack, architecture, §5 Go contracts, parallel work packages).
2. `docs/specs/2026-10-24-design-system.md`: front-end spec (mobile-first, light only, no horizontal scroll, wizards). Visual preview: https://claude.ai/artifact/BmUKkSr1GtsLED2KAbyDtn
3. `docs/specs/contract-changes.md`: decision log CD-01 to CD-93; the spec already includes them.
4. `docs/tests/core/README.md`: 722 test cases (556 P1, 166 P2) across `01-` to `07-*.md`; fixtures in `00-fixtures.md`.
5. `docs/superpowers/plans/`: one plan per work package.

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

- Product owner reviews the specs and plans and chooses an execution method (subagent-driven recommended).
- Local Go is 1.25.5; the spec needs 1.27.3 (`go` downloads it through the toolchain directive).
- The repository is not yet a git repository; WP0 Task 1 runs `git init`.
- The Common Core Blueprint artifact (https://claude.ai/artifact/2LtWD6ytFvrZJYvW2786y7) still recommends Frappe; the decided stack is Go + templ + htmx + SQLite.
