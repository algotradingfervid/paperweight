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
- Workers run in parallel and share one scratchpad: every temp file name contains your task label (e.g. `wp1-num-1-handin.txt`), never a shared name such as `handin.txt` (hand-ins clobbered each other twice in Wave 1).
- A package's `sqlc.yaml` reads either the shared `migrations/` (its `dbq/models.go` changes when any WP adds a migration: after rebasing, run `make generate` and commit `dbq/` before `make ci`) or only its own migration files (CD-117: add every later migration that alters its tables, or sqlc generates against the old table).
- Test-writers can't lint before code exists. Lint-only fixes to committed test files (errcheck, an unused helper's `//nolint`) are made by the director as their own commit; never an assertion change.
- Before handing out a task, check its test files and plan examples against the latest CD entries on main (worktree copies lag; GRN/ and EXP/ against CD-113/115 stopped two implementers).
- Plan text was never compiled: before relying on an identifier a plan names, check it against the frozen §5 names (`go doc`) and that an earlier task (not a later one) creates it. DOC Task 1 hit two such clashes (CD-107 `Check`, and a fixture missing `OnLine`).
- Tailwind scans every `.go` and `.templ` file under `internal/ui` except `_test.go` and `testdata/` (`@source not` in `app.css`): any change there changes the hashed CSS, so run `make generate` before the hand-in or `TestAssets_UpToDate` fails. The Tailwind binary takes ~5 s to start.
- The Write tool turns a `\u00a0` escape in Go source into a literal NBSP. Put invisible characters behind a named constant written as an escape, and check with `grep -c $'\xc2\xa0' <files>` (`grep` here is ugrep; `-P "\xC2\xA0"` silently finds nothing).
- A check made on a row read before `db.Write` (password hash verified, enabled flag, last-admin count) is repeated inside the write or made its condition (`UPDATE … WHERE id=? AND password_hash=?`, 0 rows = refused); slow work such as argon2 stays outside (CD-94; three times in AUTH).
- `ON CONFLICT … DO UPDATE` in `queries/*.sql` needs a comment saying why (a silent upsert defeated AUTH's no-resurrection rule).
- Tailwind also picks up plain words in comments and strings ("list", "menu", "filter", "stack"), pulling daisyUI component CSS into the bundle: keep daisyUI words out of comments and exclude generated or test-only Go with `@source not`. It can't see concatenated class names (`"badge--"+state`): write each class in full.
- Compose the existing component (`components.Button`, Banner, Toast); never re-create its markup or classes in another file (three times in UI Tasks 4–8).
- Keep a lane worktree until its review has passed: removing it loses the worker's transcript, so the fix loop can't resume it. macOS has no `timeout` command.
- Template literals in tests and plans must render ≤ 14 characters (GRN/, EXP/ and PUR/ broke it: CD-113/115/129); check each against `parseTemplate` first.
- Fakes and reference drivers refuse everything the real component refuses; a lenient double lets tests pass that fail under the engine (APR's driver skipped DOC's read visibility and submit/write rights; CD-125).
- Test each behaviour a CD or plan names through the public path, so deleting its call site fails a test; a helper's unit test alone passed with the call removed three times in NUM.
- A catalogue user message covers only the condition it names; caller bugs get a plain internal error.
- Fixture ids follow name order, so sort-by-link-name tests insert rows out of name order.
- A `\ufeff` escape written through the Write/Bash tools becomes a real BOM byte (Go refuses it): use a named constant, as for NBSP. In AD sessions finished crew workers can't be resumed ("No transcript found"): fix loops dispatch a fresh worker with the findings and exact scope.
- scs saves the session at the first `Write`/`WriteHeader`: anything put into the session (CSRF token, flash, uid) must happen before the response starts, or it is silently lost (two AUTH Task 10 review rounds).
- The shell is zsh: an unquoted `$VAR` holding a list is never word-split (`for f in $FILES` runs once with the whole list). Read lists line by line: `while IFS= read -r f; do …; done < <(git diff --name-only --diff-filter=U)`. ugrep's `-Z` isn't NUL-separated either. This broke the director twice.
- Rebase conflicts in generated files (`**/dbq/`, `*_templ.go`, `internal/ui/static/*`, `assets_gen.go`): take either side with the loop above (`git checkout --ours -- "$f" && git add -- "$f"`, then `GIT_EDITOR=true git rebase --continue`), stop on any other file, then `make generate`, commit, `make ci`.
- Local Go is 1.25.5; the `toolchain go1.27.3` line in `go.mod` downloads the right one. Don't install Go by hand.

## How work runs here

This project uses the crew harness (`.claude/harness.json`): the main session is the director (`director` skill), workers are the global `crew-*` agents, and the rules are in `docs/superpowers/plans/2026-10-25-execution-protocol.md`. New or changed screens need an approved UX design first (`docs/ux/TEMPLATE.md`). Decisions go in `docs/specs/contract-changes.md`. Worktrees live at `../it-<wp>` on `wp/<wp>`; WP0 runs on `main` and ends with the tag `wp0-foundation`. The paused WP0 work is on branch `wip/wp0-start` (not reviewed; resume from it, don't merge it as is). Never push without the owner's say-so.
