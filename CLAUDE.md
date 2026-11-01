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

**Read `docs/engineering-lessons.md` before writing a plan, a brief, tests, code or a review** (sections 1–2 are the plan and review checklists; 3–11 are the rules by area). The lines below are the ones that bite in every session.


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
- Fakes and reference drivers refuse everything the real component refuses; a lenient double lets tests pass that fail under the engine (APR's driver skipped DOC's read visibility and submit/write rights; CD-125). Best: the fake calls the real validator (`audit.ValidateEntry`, `auth.DeniedError`) rather than copying it (authfake CheckLink, auditfake Op; CD-125, CD-163).
- Test each behaviour a CD or plan names through the public path, so deleting its call site fails a test; a helper's unit test alone passed with the call removed three times in NUM.
- A catalogue user message covers only the condition it names; caller bugs get a plain internal error. A new refusal gets its own message even when it reuses an error kind and status (CD-147, CD-152). Another WP's test expects the producing WP's text, never its own copy (CD-142, CD-155).
- Fixture ids follow name order, so sort-by-link-name tests insert rows out of name order.
- A `\ufeff` escape written through the Write/Bash tools becomes a real BOM byte (Go refuses it): use a named constant, as for NBSP. In AD sessions finished crew workers can't be resumed ("No transcript found"): fix loops dispatch a fresh worker with the findings and exact scope.
- scs saves the session at the first `Write`/`WriteHeader`: anything put into the session (CSRF token, flash, uid) must happen before the response starts, or it is silently lost (two AUTH Task 10 review rounds).
- The shell is zsh: an unquoted `$VAR` holding a list is never word-split (`for f in $FILES` runs once with the whole list). Read lists line by line: `while IFS= read -r f; do …; done < <(git diff --name-only --diff-filter=U)`. ugrep's `-Z` isn't NUL-separated either. This broke the director twice.
- Rebase conflicts in generated files (`**/dbq/`, `*_templ.go`, `internal/ui/static/*`, `assets_gen.go`): take either side with the loop above (`git checkout --ours -- "$f" && git add -- "$f"`, then `GIT_EDITOR=true git rebase --continue`), stop on any other file, then `make generate`, commit, `make ci`.
- Code a worker adds beyond the plan needs a test that fails when that code is deleted; reviewers check touched lines with `go test -coverprofile`. The same holds for every behaviour a CD names (missed twice in WP2: CD-166, CD-170 branches).
- Before extending a fake to pass a contract suite, list the real path's refusals; if covering them means copying the service, cut the fake's capability (refuse everything it isn't told to return) instead (CD-184, CD-186).
- A test-local copy of a registered descriptor or fake that accepts what the real one refuses is a lenient double: ask the owning WP to add the field (CD-162).
- An htmx fragment never redraws the input the user is typing in (a response arriving mid-typing overwrites keystrokes): update only the error or preview region.
- Every FK column fed from user input is validated in `Validate`/the Tx method first; the FK is a backstop, never the message (raw FK errors are 500s).
- A master whose `ProjectFields`/`SiteFields` is `{"id"}` puts its own id in the RecordRef's `ProjectIDs`/`SiteIDs`; with no ids a restricted user is refused on every record, even their own.
- Masters never declare `Links`, and `DefaultSort` names only declared fields (Address and Contact have no `name`; CD-167).
- Commit tests for a package that isn't built yet in its lane, never on the WP branch: tests ahead of code break the shared hand-in and `make ci`.
- A change to an auth check needs the same change in `authfake` and a parity row (real service vs fake, every DocType × `AllRights` + one unknown right); authfake drifted three times (CD-157, CD-162, CD-173). An engine test that depends on restrictions or scope also needs one case on the real `auth.Service`: a test that passes only on authfake hides the difference.
- An invariant between two records (or two masters) is checked on every write path of both sides: create, update, set-primary, enable, import. A rule on one path but missed on its siblings is the most common review finding in Wave 1. Best: one post-state check that reads the whole state after the change, inside the write tx, called last by every write, plus a property test over random write sequences (CD-180).
- Every browser-test (chromedp) wait has a short per-step deadline: a request that never answers (e.g. a fake returning "not implemented yet") then fails one test fast, not the whole run at the 10-minute timeout.
- The nested-transaction gate runs only in `make test` (`-tags nestcheck`): the fast check and hand-in are untagged, so a second connection inside a tx first fails there. Inside a tx use the Tx twins (`CanTx`, `HasRoleTx`, `ValueAllowedTx`) (CD-213, CD-224). Lint hand-ins for build-tagged files need `--build-tags`.
- `make ci` runs no browser tests. Before a MERGE-READY or a merge that touches internal/ui, docscreen, auth or internal/app, run `make e2e` on the merged head too: a prefill from one lane broke three browser flows from another, and `make ci` stayed green (CD-245, 31 Oct).
- A plan's "disjoint lanes" must not name the same functions: before splitting a package across lanes, grep each lane's named functions and give any shared seam its own serial task first (hooks spec M0-2/M0-3 named the same form.go helpers; CD-256); MOD-EXP repeated it with OpenAdvances and one shared stubs file). Every lane gets its own stub file.
- Derived lists, cards, warnings and history name nothing about a record the viewer can't read, not even its state or amount; show at most an aggregate the viewer can already see (CD-233, CD-240, CD-248, and again in the expense UX review).
- In docscreen every `stepViewOf(` caller passes errors through `stepErrors`/`refusalErrors` (or nil). newPost's 422/409 redraw skipped the hidden-field filter in M0-1b: a rule applied on most redraw paths but missed on one.
- Before a ruling changes a text pattern (a prefix or wording rule), search the CD table and the code for older texts it overrides; CD-264's `Line {n}:` rule missed CD-199's text (CD-278).
- golangci-lint takes a machine-wide lock. With several ADs linting at once, `make lint` failed with "parallel golangci-lint is running". The Makefile now passes `--allow-serial-runners` (wait, don't fail); add the same flag to hand-in lint commands.
- `scripts/covertouched` exempts untested plain error returns (CD-185) and skips generated `_templ.go`. Its summary now names the exempt blocks, and `-strict` reports them as uncovered. For template-only or error-path-heavy tasks, the reviewer runs a mutation pass (CD-288).
- A plan's stub returns nil or zero only where that means "no extra rule". Date and Amount hooks are real from the skeleton task, because a zero Date makes naming.Assign refuse every approve (MOD-PO plan check).
- A contract stated for one producer is written out in full for each sibling, never "as X does" (render error mapping, GET-state 403/404; MOD-PO plan check).
- Hand-in test lists that touch UI include `./internal/ui/...`; the assets and UI lint tests live there, and a list of only the changed packages misses them (AD-10).
- golangci-lint here doesn't run gofmt, so unformatted Go reached committed files three times. `make ci` now fails on `gofmt -l`; run gofmt on your files before a hand-in (AD-10). Use the toolchain's gofmt, `$(go env GOROOT)/bin/gofmt`: the shell's /usr/local/go gofmt is 1.25 and disagrees with 1.27's on some lines; make ci and make handin both use the toolchain's (AD-11).
- One `make ci` at a time in `.worktrees/ci-check`: checking out a new head while a run is going changes files under it, and that run's result is void (head director, 31 Oct).
- internal/kernel/auth/web needs about 6 minutes alone under -race. With several ADs' suites running it crossed go test's 10-minute default, so `make test` passes `-timeout 40m` (raised from 20m when load reached ~110); hand-ins that include it use at least `-timeout 20m`.
- A widened predicate (`x || field == v`) needs a test row for every other non-empty value of that field, not only for the new one (M0-24).
- A director edit to a protected file that a test compares against code (the research tokens block, test docs read by checkers) lands in the same commit as the code, never ahead of it: an early research token line broke TestTokens_MatchResearch on main (head director, 31 Oct).
- A rights test needs a user holding only the right the rule names. With only all-rights users, swapping read for submit in the code survived every test (G0a; same family as lessons §11).
- A test that numbers a TestOnly fixture DocType and a real module DocType sharing a prefix in one database sets the real series' counter first (SetCounter, e.g. 50000); `naming_issued.number` is unique across DocTypes (CD-295).
- Keep `queries/*.sql` comments ASCII: sqlc 1.31.1 generates broken SQL when a comment holds a non-ASCII character such as "…" (AD-11, E1).
- A module's first GET routes need e2e not-page entries or screens even while they answer 501: `make ci` doesn't run the route gate (TestE2E_EveryRouteCovered), only `make e2e` does (AD-11, E1).
- **High-tier reviews include a mutation pass** (`go test -overlay` with a mutated copy) on every guard and every `queries/*.sql` filter: ORDER BY, DISTINCT, state lists, refusal-switch arms, CSV BOM and escaping, and string-match guards (M0-26's exact-id match let weakened mutants survive). covertouched marks those lines covered while no test fails when they're deleted (four of five MOD-EXP reviews; CD-299).
- Re-check a task's tests and brief against main's CD log when the implementer is dispatched, not only at test-writer dispatch: M0-19 missed CD-292 (5), which landed in between.
- Write mixed `&&` / `||` with parentheses: staticcheck and gocritic don't flag them, and a gallery filter bug came from one (M0-5).
- A module's Validate writes each derived field as computed from current inputs or cleared, never left as it was: amend and step saves keep Validate's writes even when it returns errors, so "skip on error" leaves stale snapshots beside a changed input (CD-302; P2 failed review twice).
- `go tool templ generate -path <pkg>` rewrites the error filenames in every `_templ.go` of that package to short names (a noisy diff): use `-f <file.templ>` or plain `make generate` (AD-9, M0-9).
- Each skip or exception in a sweep or gate carries a test or check that fails when its stated reason stops being true: a stale "declares no filterable field" skip in e2e/routes.go hid a 422 on every module's filtered list until the expense sweep hit it (M0-34).
- A module's stub-route tests assert "mounted and guarded", never the 501 itself: each lane that builds a page otherwise breaks the shared test (AD-13, PO-M1).
- Parse posted values inside the service after load, right, version and state checks (CD-259): a handler-side parse let a bad date skip 403/409 (AD-13).
- After `make generate`, check `git status --porcelain -- ':(glob)**/dbq/**'` (diffs and untracked files; without `:(glob)` git doesn't expand `**` and the check is always empty, AD-12) before a hand-in: a stale generated file passed P7's hand-in.
- Date tests include a clock just after IST midnight (e.g. 18:31 UTC): a fixed clock on the same date in UTC and IST leaves the business-date boundary untested (AD-13).
- covertouched marks a returned value covered when the code runs: assert the value itself (e.g. a back-dated document's number for a Date hook). A wrong Amount/Date hook value passed G1's tests (AD-12).
- Test a guard in the setups where it changes the result, not only the common one: every DataList test had a state column, so the low-separator branch for lists without one had no test (M0-36).
- A change to `internal/ui/js/*` runs every browser test that loads or uploads through that script, not only the task's own: gallery `-run 'Attach|Upload'` and e2e `-run 'TestE2E_Attachment|TestCheckStep_UploadThenSubmit|TestDocPage_AddAfterApproval|TestE2E_CompanyLogo|TestE2E_ExpenseFromPhoneWithReceipt'` (M0-22 passed HOOK-041 and broke five others).
- A money input that feeds a derived amount is declared Positive or NonNegative: a negative `less_advance` raised net payable above the total and past the money limit (E10, REPEAT).
- Never `pkill -f "go test"` (or `make ci`/`make e2e`): several sessions share this machine, and the pattern kills all their runs (AD-10, 31 Oct). Kill by PID or by a pattern holding your own worktree path; a run that ends with "Terminated: 15" is void, not failed.
- A layout worst case covers every cell order and value combination (loop over the permutations), not one hand-picked row: one-order checks missed real overflows twice (money in track 4, then rate plus amount; M0-35, same family as M0-24).
- A page size or limit gets a test with one more row than the limit: G6's picker size 25 survived mutation to 26 and 1000 because the largest test list had 6 rows (AD-12, REPEAT).
- A head-lane kernel change that alters rendered output runs every module's tests on the current main before merging, not on the lane's older base: M0-43 (PartyBlock) was green on its base but broke a purchase test that merged in between.
- A module's scope helper returns a match-nothing predicate when the right is missing, never the empty "all rows" Predicate; and a module write that checks a version itself calls CanTx before the version and before parsing input, even though Engine.Do checks again (P13, P14; CD-259 REPEAT).
- The head merges the exact commit a MERGE-READY names (`git merge --no-ff <sha>`), never the branch name: an AD advanced wp/mod0-hooks after its MERGE-READY and an unverified task rode in (M0-46, 31 Oct). ADs don't move a WP branch between MERGE-READY and merge.
- A docscreen redraw status must be one the htmx config swaps (422, 409; layout.go): recorder tests never apply the swap table, so each new redraw status gets a browser test or a test that reads htmxConfig (M0-50, REPEAT).
- Hand in with `make handin BASE=<base> PKGS="<pkgs>"` (CD-328): it runs the gates make ci skips (route gate, generated files, ./internal/ui/... for UI changes) and fails on a gate that didn't run. High tasks also run `make mutate BASE=<base> PKGS=<pkgs>` and hand in its survivor list.
- A new document DocType or GET route fails TestToolingRedraw and TestTooling_VisibilitySweep until the same task adds its seeder (rsEnv.seeder, sweepSeeders) and its sweepStatuses row. Never skip there: the sweeps check Hide and hidden-record rules for every module (CD-317, CD-318 and CD-319 came from them).
- Before dispatching a task, run `make briefcds BASE=<lane base> FILES="<task files>"`; it reads main's CD log, not the lane's.
- Split a task estimated over ~90 worker-minutes (P2 took 299, E1 232): review rounds on a big diff cost more than two small reviews.
- Two lanes that are each green can fail together: a new DocType (fixture.Quote, not submittable) met the tooling sweeps, which probed submit on every DocType, and main went red (31 Oct). A sweep or gate derives what it probes from the DocType's own declarations, never assumes a right.
- A sweep or gate that needs a module's flags (NoLinePages, CardsOnly, Hide) reads them from the app's real Deps (`App.DocDeps()`), never from a test-local Modules map: a hand-written copy is a lenient double (M0-55, REPEAT).
- After a new gate or sweep lands on main, the head merges only MERGE-READYs whose ci ran on a head that includes it: PO batch 4 was green on a base without the tooling sweeps and turned main red (11 purchase.Request cells, 31 Oct).
- A module page extra (pairs, cards) that shows a stored field's value checks the registered Module.Hide, and so does anything derived from it (Left from Used); the redraw sweep's read-only probe fails otherwise (MOD-EXP S3, REPEAT).
- A docscreen `Step.Save` hook, like a handler, runs after the posted version, state and right checks, never before parsing or validating posted values; docscreen enforces it from M0-56 (CD-335). It reached P15c after the handler-side date parse (CD-259, AD-13, REPEAT).
- A CSS change in a fix round runs the full `make e2e`, not an e2e-quick subset: M0-53 fix1 passed its layout subset and broke TestE2E_AttachmentsAxe (target size) on main.
- Order tests insert a back-dated or out-of-order row: ids that follow the sort key let ORDER BY mutants survive (G9, REPEAT of the name-order lesson). A multi-filter parser that keeps one error gets a row with two bad filters (G9).
- A guard or open condition is tested in every state or input where it changes the result: one non-draft state let a narrowed state guard survive (S1d), and a condition joining several inputs needs one row per input with the others blank (S2) (AD-12, REPEAT).
- Fake a read failure in the service (a closed DB), never by cancelling the request context: a cancelled-ctx test couldn't fail (AD-12, S1b). The goodsreceipt package needs about 13 minutes under -race, so its hand-ins use -timeout 20m or more.
- A head lane branched from an older commit merges main before its hand-in, and checks every "no module uses X" claim against main: B1 (base 48c776c2) hid 22 purchase hints behind tips because P29 landed on main in between (REPEAT of the compare-with-main lesson). `pkill -f` without your own worktree path in the pattern happened again in B1.
- testkit.NoSecondConn's 3 s deadline can trip under heavy load (allmasters project chain at load 80, AD-11): rerun the one test alone before treating it as a real second connection.
- Never stop a run by walking up its process tree or killing a parent process: an M0-56 worker did that and killed the head director's Claude Code session (1 Nov, 05:59, six hours lost). Kill only the exact PID of a `go test` you started, found with `pgrep -f` on a pattern holding your own worktree path.
- Local Go is 1.25.5; the `toolchain go1.27.3` line in `go.mod` downloads the right one. Don't install Go by hand.

## How work runs here

This project uses the crew harness (`.claude/harness.json`): the main session is the director (`director` skill), workers are the global `crew-*` agents, and the rules are in `docs/superpowers/plans/2026-10-25-execution-protocol.md`. New or changed screens need an approved UX design first (`docs/ux/TEMPLATE.md`). Decisions go in `docs/specs/contract-changes.md`. Worktrees live at `.worktrees/<wp>` on `wp/<wp>`; WP0 runs on `main` and ends with the tag `wp0-foundation`. The paused WP0 work is on branch `wip/wp0-start` (not reviewed; resume from it, don't merge it as is). Never push without the owner's say-so.
