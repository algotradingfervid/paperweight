# Engineering lessons (paperweight stack)

What building core v1 taught us about this stack (Go + templ + htmx + Alpine CSP + SQLite/modernc + sqlc + daisyUI/Tailwind standalone + scs) and about running work through the crew harness. Every rule below cost at least one failed review, a stuck task or a bug that nearly reached main; the CD numbers point to the full decision in `docs/specs/contract-changes.md`.

**Who reads this:** whoever writes a feature plan (section 1), whoever briefs or reviews a task (section 2), and every implementer and test-writer (sections 3–10 for the areas they touch). `CLAUDE.md` keeps only the tool and environment gotchas that bite in every session.

**Keeping it current:** when a reviewer reports `REPEAT:` or the same mistake shows up twice, the director adds one rule here, in the right section, with its CD or task reference. Rules that a tool now enforces move to section 11 with the tool's name.

---

## 1. Writing a feature plan: checklist

Two prose reviews of the Wave 1 plans still left most of the defects below; they are mechanical, so check them mechanically.

1. **Compile the plan's names.** Every type, function, field and constant a task uses either exists on `main` (`go doc ./internal/...`) or is created by that task or an earlier one, never a later one (CD-107 `Check` clash, CD-119 `NumberFuncs` from a later task).
2. **Check every literal against the rules it must obey.**
   - Number templates render ≤ 14 characters (GRN/, EXP/, PUR/ broke it: CD-113, CD-115, CD-129).
   - Example values match the test docs (PAN `AAFCV2449G`, CD-131), fixture ids and names (`00-fixtures.md`), and the clock (`authtest.T0`, CD-125).
   - Select `Options` are the stored values, not the display labels (CD-175).
3. **Check the schema against the registry rules.**
   - Line tables have `id`, `parent_id` (ON DELETE CASCADE), `line_no` (CD-16, CD-161).
   - Field names are unique across header and lines; a line field sharing a header column is `line_<name>` with an explicit `Column` (CD-151).
   - Convention columns (`number`, `state`, `created_at`, …) are declared only as read-only fields mapped to themselves (CD-144).
   - Every table whose id appears in a URL, an audit entry or another table uses `INTEGER PRIMARY KEY AUTOINCREMENT` (CD-156).
4. **Check messages across work packages.** A refusal's text belongs to the package that produces it; every other package's test expects that exact text (CD-142, CD-155). Grep the other test docs for the same condition before inventing a message.
5. **Give every condition its own message.** For each refusal the plan adds, write its text, even when it reuses an error kind and status (CD-147, CD-152). Plans that left these out caused about a dozen mid-task escalations (CD-132, 166, 170, 176).
6. **List every write path for every invariant.** For a rule that links two records, list create, update, set-primary, enable/disable, delete and import on both sides, and plan one post-state check called by all of them (CD-180, CD-185).
7. **Say when each check runs.** Draft save vs submit (CD-178), route guard vs handler (CD-145), inside vs before the transaction (CD-94).
8. **Check the import table** (`TestContracts_KernelImportsFollowTable`) for every new package and test helper the plan creates (CD-138, CD-153, CD-168).
9. **Name the gates.** Which milestone on `main` each task needs (UI-M2 for screens, DOC-M1 for links, and so on), so nobody starts a task whose dependency is a stub.
10. **Run the plan check** (section 2) on each task before it is dispatched.

## 2. Before dispatch and during review: checklist

**Plan check before a task goes out** (director or AD, or a reviewer told to do it):
- Run section 1's items 1–5 on the task's section and its test files.
- Compare the task's test values with the latest CD entries on `main`; worktree copies lag until rebased.
- Confirm the gate milestone is on `main` and the branch is rebased onto it.

**Every review also checks:**
- Each behaviour the plan or a CD names has a test that fails when its call site is deleted, including code the implementer added beyond the plan (`go test -coverprofile` on the touched lines; `scripts/covertouched` once it lands, CD-185).
- Every write path of every invariant (section 1 item 6).
- Fakes refuse what the real service refuses (section 6).
- Checks that depend on other rows run inside the write (section 4).
- No test passes only because of a lenient fake: a restriction- or scope-dependent engine test has one case on the real `auth.Service` (CD-178 review).

## 3. SQLite, sqlc and migrations

- Ids: `INTEGER PRIMARY KEY` reuses the deleted newest row's id (max rowid + 1). Use `AUTOINCREMENT` for any id that is referenced, linked or audited (CD-156). The audit log is the exception: its no-delete trigger already prevents reuse.
- Every FK fed from user input is validated in `Validate` or the Tx method first, with a user message; the FK constraint is the backstop. A raw FK or CHECK error is a 500 (CD-166, CD-170).
- `ON CONFLICT … DO UPDATE` needs a comment saying why; a silent upsert defeated AUTH's no-resurrection rule.
- sqlc 1.31.1 for SQLite can't bind a parameter in an upsert's `WHERE`; use `INSERT … SELECT … WHERE NOT EXISTS … ON CONFLICT … DO UPDATE … RETURNING` (CD-134).
- A package's `sqlc.yaml` reads either the shared `migrations/`, so its `dbq/models.go` changes whenever any package adds a migration (regenerate after every rebase), or only its own migration files, so it must list every later migration that alters its tables (CD-117).
- Unreleased migrations are edited in place; once a release exists, only new migrations (CD-156).
- `types.Date`/`Timestamp` `Scan` refuse NULL: nullable columns use `sql.Null[types.Date]`.

## 4. Transactions and concurrency (CD-94)

- Every write goes through `db.Write` (`BEGIN IMMEDIATE`). A check that depends on other rows reads inside that transaction.
- A check made on a row read before the write (password hash, enabled flag, last-admin count, role name for an id) is repeated inside the write or becomes its condition (`UPDATE … WHERE id=? AND password_hash=?`; 0 rows = refused). Slow work (argon2) stays outside. This happened three times in AUTH, and again with role ids (CD-158).
- Saves use the `version` the page posted, never a freshly loaded one. Every web write posts `version` (profile, roles, restrictions, delete, matrix, every `/doc/` POST); missing or non-numeric is 400, stale is 409 (CD-146, CD-158, CD-183).
- Resolve a record by id inside the write, never id → name before it (CD-158).
- A failure after a counter or number is taken relies on the caller's rollback; design it so the counter can't move without the row (CD-134).

## 5. Rights, restrictions and auth

- Fail closed: an unknown right, dimension or DocType is refused for everyone, Administrator included (CD-171, CD-174).
- A record with no project or site ids is outside every restriction. A master whose `ProjectFields`/`SiteFields` is `{"id"}` puts its own id in the RecordRef, or restricted users lose their own records. A Required project or site field stays required on a draft save for a restricted user (CD-178).
- A user restricted on a dimension can't create a master of that dimension (CD-172); use `auth.Restricted` and `auth.Forbidden(msg)` (CD-174).
- Export routes are guarded by `read`; the handler checks `export` (CD-145).
- Password hashes: parse PHC strings strictly and bound every parameter before argon2 runs (a stored `m=4294967295` would kill the process).
- Sessions (scs): the session is saved at the first `Write`/`WriteHeader`, so anything put into the session (CSRF token, flash, uid) happens before the response starts (CD-136). AUTH's middleware owns `LoadAndSave`; `web.Options.Sessions` stays nil with it, and `web.WithSessions` makes flash work (CD-150). Rotate the CSRF token with the session token at sign-in.
- Seeded roles are created once; a reset or restart never resurrects a deleted or renamed one (CD-158).

## 6. Fakes, doubles and contract suites

- A fake refuses everything the real component refuses, with the same error and text. Best: the fake calls the real validator or error constructor (`audit.ValidateEntry`, `auth.DeniedError`, `auth.ValueError`) rather than copying it (CD-125, CD-157, CD-162, CD-163, CD-173).
- Every fake has a contract suite run against both it and the real service, including refusal rows (CD-182). A fake that can't be made faithful without reimplementing the service is deleted, and callers use the real service over a test DB (namingfake, approvalfake: CD-184).
- A change to an auth check needs the same change in authfake and a parity row: every DocType × `AllRights`, plus one unknown right and records with empty ids.
- A test-local copy of a registered descriptor or fake that accepts what the real one refuses is a lenient double: ask the owning package to add the field (CD-162).
- Reference drivers (APR's) refuse what the engine refuses; run the suite on the real engine by default (CD-157).
- Before extending a fake to pass a contract suite, list the real path's refusals. If covering them means copying the service, cut the fake's capability instead (it refuses everything it isn't told to return): namingfake deleted (CD-184), docfake.Do reduced (CD-186). Fifth lenient-double finding.
- Contract-suite mutation checks run in an rsync scratch copy of the worktree: the crew guard blocks `git checkout -- <file>`, so say so in review briefs.
- A fake change is checked against every package that imports the fake: run `make test`, not a hand-picked list (a hand-picked AUTH hand-in missed 5 failures in FILES, PRINT and contracts). A contract change to one double reaches every double of that contract (authfake and fixturetest.Auth; CD-189). Prove "fails without the fix" with `go test -overlay` on scratch copies, never by editing the worktree.

## 7. Validation and messages

- Every user message is in a catalogue, covers exactly the condition it names, and is tested by exact text. Caller bugs get a plain internal error (CD-147).
- Patterns: `{Label} is required.`; `{Label} not found. Pick one from the list.`; disabled → the case's text or `{Label} {code} is disabled. Pick another {label}.`; Select → `{Label} must be A, B … or Z.`; line fields prefixed `Row N:` (CD-166, CD-170).
- Required is checked on submit, not on draft saves; drafts still check types, formats, lengths, options, links and line shape (CD-178).
- Lists of allowed codes in messages are sorted (CD-149).
- A CSV parse error is a whole-file 422 with the spreadsheet row, never a raw `csv.ParseError` (CD-132).
- Refusal text belongs to the producing package (CD-142, CD-155).

## 8. Invariants across records

- An invariant between two records is enforced by one post-state check that reads the whole affected state inside the write and runs last in every write path of both sides, plus a property test over random write sequences. Per-path preconditions alone missed sibling paths in four tasks; WP2 Task 6 needed three attempts (CD-180).
- When a state needs history to be checked ("re-enable only the user this leave disabled"), store that history in a column; don't infer it from the current link (CD-185), and when the history is another package's state, store its version (CD-189).
- A disabled record is never primary or default; disabling one clears the flag and promotes nothing (CD-180).
- Master-to-master references are checked by the master's delete, since `doc.References` covers documents only (CD-167).
- A rule on both owner kinds (party/branch) or both types (billing/shipping) needs a test for each half; a post-state backstop that skips one owner kind doesn't cover it (WP2 Task 6).
- Don't store another package's state as a flag ("we disabled this user"): it goes stale when the owner changes it. Store the owner's version at that moment and compare it inside the write (CD-189).

## 9. UI: templ, htmx, Alpine, Tailwind and daisyUI

- CSP: no `style=` attributes, inline scripts, `hx-on`, trigger filters `[...]`, or templ `css`/`script` blocks.
- Component CSS is Tailwind `@utility`, never `@layer components` (CD-98). daisyUI classes only inside `internal/ui`; no `*-soft`, `table`, `countdown`, `radial-progress` or popover dropdowns.
- Tailwind scans every `.go` and `.templ` under `internal/ui` except `_test.go` and `testdata/` (`@source not` in `app.css`). It picks up plain words in comments and strings ("list", "menu", "stack") and pulls daisyUI component CSS into the bundle: keep those words out of comments, and exclude generated or test-only Go. It can't see concatenated class names: write each class in full. Our utility names must not clash with daisyUI or Tailwind built-ins (`.grid` → `auto-grid`).
- Compose the existing component (`components.Button`, Banner, Notice, ErrorSummary); never re-create its markup or classes (three times in UI Tasks 4–8). The shared API additions are in CD-183.
- `ui.Render` renders into a buffer, then writes the header, so flash removal is saved and render errors become 500 (CD-160).
- An htmx fragment never redraws the input the user is typing in; update only the error or preview region (NAM 18). On a 422 the page keeps the typed values and the error summary (CD-178).
- `components.ErrorSummary` announces itself by taking focus; don't add `role="alert"` around it (CD-159).
- Number inputs never clip their worst-case value; text inputs end in an ellipsis; a radio's or checkbox's 44 × 44 target is its label row (CD-169).
- File upload takes one file per request (CD-133).
- Run `make generate` before every UI hand-in: any change under `internal/ui` changes the hashed CSS, and `TestAssets_UpToDate` fails otherwise.

## 10. Testing

- Test-first; each behaviour a CD or plan names has a test through the public path that fails when its call site is deleted. Unit tests of a helper alone passed with the call removed three times in NUM and twice in WP2.
- Fuzz and property tests find what example tests don't (id reuse, CD-156; the M5 holes, CD-180). Give every invariant one.
- Sort tests insert rows out of name order (fixture ids follow name order).
- Every browser-test (chromedp) step has a short deadline, so a request that never answers fails one test fast.
- Page tests compare the page text (goquery), not raw bytes (CD-148).
- `go test` without `-count=1` uses the cache; hand-ins use `-count=1`.
- Invisible characters (NBSP, BOM) go behind a named constant written as an escape; the Write and Bash tools turn ` ` and `﻿` escapes into real bytes.
- A guard put on a shared helper also goes on every duplicated call path; a test-local copy of `visit()` skipped the browser step deadlines (UI Task 17).
- When a fake becomes faithful, tests in other packages that asserted the old fake's output fail on merge; run `make ci` after each contract-suite merge (CD-188).

## 11. Harness, git and process

- Shell is zsh: an unquoted `$VAR` holding a list is not word-split. Iterate line by line: `while IFS= read -r f; do …; done < <(cmd)`. `grep` is ugrep (`-Z` isn't NUL-separated; `-P "\xC2\xA0"` finds nothing).
- Generated-file rebase conflicts (`**/dbq/`, `*_templ.go`, `internal/ui/static/*`, `assets_gen.go`): take either side, `make generate`, commit, `make ci`. Any other conflicting file: stop and look.
- Lanes: parallel tasks in one Go package get their own worktree; keep it until its review passes (removing it loses the worker's transcript). Tests for a package that doesn't exist yet are committed in the lane, never on the WP branch.
- Every worktree lives in `.worktrees/<name>` inside the repo (gitignored), never beside it: `git worktree add .worktrees/<wp> -b wp/<wp> main`, then `ln -s ../../bin .worktrees/<wp>/bin` for the shared tools. Move one with `git worktree move`; a relative `bin` link then needs repointing.
- A decision that gives WP2 or WP3 work is copied into that plan the same day: CD-141, 154, 165, 171 and 178 stayed only in the log and the work had no owner until the 30 Oct audit.
- A hand-in's covertouched base is the task's first commit~1: a fix-round base hid 4 of 5 coverage gaps in AUTH Task 20.
- A hand-in that changes a helper's callers runs full lint: `--new-from-rev` missed gosec G710 at an unchanged line caused by a new caller.
- Start an AD session with its working directory inside a worktree: subagents of a background session started in the main checkout can write only to the worktree that session entered, so lanes don't work from it.
- A check a test executes but never asserts passes covertouched (Update's CheckVersion, CheckCodeFree, CheckCodeChange): reviewers run a mutation pass over touched `Check*` calls.
- Test helpers use the faithful constructor (`filesfake.NewWith`), never the lenient legacy `filesfake.New`.
- Inside `db.Write` or an open read tx, never call anything that takes its own connection: a `files.Service.List` inside `db.Write` stalled every writer for about 5 s. Read with the caller's tx.
- Concurrency tests run as a non-admin user: `Can` returns early for an Administrator, which hid a second connection in every record-level write (CD-213).
- A test that identifies a code path by its text stops proving anything once two paths share that text: assert something only that path does.
- Moving data out of a loop moves it out of every check that loop fed: `Check` over `dt.Fields` lost ImportOnly fields until the rule read `ImportFields()`.
- Check every test literal that enters a validated API (page size, template, code) against that validator first; a forwarding wrapper's test asserts every argument arrives, not only the result.
- `chromedp.Poll`, and so `waitFor`, can't span a navigation: wait with a Query on a selector found only on the target page.
- A no-op test that takes the Unchanged early return never reaches the behaviour it names. Lock and rule checks that read the current row go after CheckVersion, or a stale page gets a 422.
- Run `covertouched` with a profile path named after the task: the shared `.work/covertouched.out` gave one false "0 blocks" when two runs collided.
- Don't settle an open design question in a brief: reviewers found a major in each round of P2, P5, P6 and P7b from tests that proved nothing or decisions the design contradicted. Full-repo lint before review on security paths: `--new-from-rev` misses taint in unchanged files.
- An import test reads every column back: one that only asserted a row was imported passed with ten columns dropped. covertouched counts lines, not operands, so mapping code needs a read-back test per column.
- When a Service method gains a Tx twin, every double that embeds a Service and overrides the plain method overrides the twin too: listtest.Auth skipped Deny until a test caught it.
- Test harnesses use production session settings: `scs.New()` has IdleTimeout 0 while production had 12 h, which hid draft-overwrite races from every test.
- A conversion task also greps comments for the old name: three comments describing the old state survived CD-213.
- After every merge to `main`, run `make generate` and commit regenerated `dbq/` before `make ci`; then every AD rebases.
- High-tier lint base is the commit before the tests commit, or the committed test file is never linted.
- Temp files carry the task label (the scratchpad is shared).
- Record times from commits or the clock, never estimates.
- After a network outage, check each worktree for recent file changes; a worker that died mid-task leaves uncommitted changes, and a fresh worker resumes from them with the same brief.
- Hand idle workers to whatever is unblocked: contract suites, plan checks, tooling. Don't hold slots for a session whose lanes run one task at a time (CD-182).
- An AD session can't resume finished crew workers ("No transcript found"); fix loops brief a fresh worker with the findings and exact scope.
- macOS has no `timeout` command.
- AD session transcripts in the worktree folders did not survive the overnight pause (only their tool-results folders remained), so `claude --resume` can't bring an AD back. Before any pause, each AD writes its full resume steps to `.work/ad-tasks.md`; a restart is a fresh AD session briefed from that file and PROGRESS "RESUME HERE" (26 Oct).
- Scripts that parse git output pin its format (`--src-prefix=a/ --dst-prefix=b/ --no-color --no-ext-diff`): a user's `diff.noprefix` made covertouched pass vacuously.
- `make covertouched BASE=<commit before the task> PKGS="<patterns>"` lists every uncovered touched statement; use it in hand-ins and reviews (CD-185 tooling).
