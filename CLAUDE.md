# Paperweight

Internal business tools (purchase orders, goods receipts, expenses) for a small Indian company. Go 1.27.3 + templ + htmx 2.0.11 + Alpine (CSP build) + SQLite (modernc, WAL), styled with daisyUI 5 on Tailwind v4 standalone (no Node). Start with `docs/PROGRESS.md`.

## Commands (from WP0 Task 1)

| What | Command |
|---|---|
| Fast check for one package (use while working; cache on) | `go test -race ./internal/<pkg>/...` |
| Full tests (no cache) | `make test` |
| Lint (repo-local golangci-lint v2.15.0) | `make lint`; for one task's changes `bin/tools/golangci-lint run --new-from-rev=<base> <dirs>` |
| Generate (go generate, sqlc, templ, CSS) | `make generate` |
| Build | `make build` |
| Everything before a merge | `make ci` |
| Browser tests | `make e2e` |
| Test-ID check for an area | `make checkids AREA=<AREA>` |

## Who owns which files

- Generated and committed (regenerate, never hand-edit): `*_templ.go`, `**/dbq/`, `internal/ui/static/*`, `internal/ui/assets_gen.go`, `internal/ui/icons/icons_gen.go`, `internal/platform/brand/brand_gen.go`.
- Vendored: `internal/ui/vendor/` (daisyUI, Tailwind checksum). Re-copy from upstream at the pinned version.
- Director only (workers are blocked from editing): `docs/specs/`, `docs/research/`, `docs/superpowers/plans/`, `docs/tests/`, `CLAUDE.md`, `.claude/`.
- Branding: only `internal/platform/brand/` (CD-99).

## Things that will bite you

- Every write goes through `db.Write` (`BEGIN IMMEDIATE`); checks that depend on other rows read inside that transaction, and a save uses the `version` the page posted, never a freshly loaded one (CD-94).
- No `style=` attributes, inline scripts, `hx-on`, trigger filters `[...]` or templ `css`/`script` blocks: the CSP and the UI lint test reject them.
- Our component CSS must be Tailwind `@utility`, never `@layer components`: daisyUI's layers sit inside `utilities` and win over `components` (CD-98).
- daisyUI classes only inside `internal/ui`; no `*-soft` variants, `table`, `countdown`, `radial-progress` or popover dropdowns (research `ui-library.md` §4).
- `go test` without `-count=1` uses the cache (also with `-race`); `make test` forces a full run. Use the fast check while working.
- `scripts/checktestids` must accept flags after the area argument (`make checkids` passes `<AREA> --exclude-level e2e`).
- High-tier hand-in lint: use `--new-from-rev=<commit before the tests commit>`, not the tests commit itself, or the committed test file is never linted (a QF1011 in `contracts_test.go` reached `make ci` this way).
- Workers run in parallel: write temp files only to your own scratchpad or a path containing your task label, never a shared name such as `/tmp/handin.txt` (two hand-ins clobbered each other in Wave 1).
- Every package's `sqlc.yaml` reads the shared `migrations/`, so its `dbq/models.go` changes when any WP adds a migration. After rebasing onto main, run `make generate` and commit the regenerated `dbq/` before `make ci`.
- Test-writers can't lint before code exists. Lint-only fixes to committed test files (errcheck, an unused helper's `//nolint`) are made by the director as their own commit; never an assertion change.
- Local Go is 1.25.5; the `toolchain go1.27.3` line in `go.mod` downloads the right one. Don't install Go by hand.

## How work runs here

This project uses the crew harness (`.claude/harness.json`): the main session is the director (`director` skill), workers are the global `crew-*` agents, and the rules are in `docs/superpowers/plans/2026-10-25-execution-protocol.md`. New or changed screens need an approved UX design first (`docs/ux/TEMPLATE.md`). Decisions go in `docs/specs/contract-changes.md`. Worktrees live at `../it-<wp>` on `wp/<wp>`; WP0 runs on `main` and ends with the tag `wp0-foundation`. The paused WP0 work is on branch `wip/wp0-start` (not reviewed; resume from it, don't merge it as is). Never push without the owner's say-so.
