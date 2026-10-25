# WP0 Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Run it under `docs/superpowers/plans/2026-10-25-execution-protocol.md` (roles, risk tier per task, review cadence; CD-96).

**Goal:** Create the repository, toolchain, platform packages, test kit and every §5 contract stub and fake, so that the Wave 1 work packages can then be built in parallel without touching each other's files.

**Architecture:** One Go module with the layout in spec §4.1. Platform packages (`db`, `clock`, `web`) are implemented fully here. Kernel packages (including the new `internal/kernel/doctype`, CD-02) get only their exported contract types, stub bodies returning `errs.ErrNotImplemented`, and an in-memory fake in `<pkg>/<pkg>fake` (plus `audit/auditfake`, CD-03). A test kit gives every package a migrated temp SQLite file, its path, a fixed clock, the canonical fixtures and a filled `auth.Directory` fake.

**Tech Stack:** Go 1.27.3, `modernc.org/sqlite` v1.61.0, `pressly/goose/v3` v3.29.0, `sqlc` v1.31.1, `a-h/templ` v0.3.1070, `go-chi/chi/v5` v5.5.0, `alexedwards/scs/v2` v2.9.0 (core package only; **no** `scs/sqlite3store`, CD-14), htmx 2.0.11, Alpine.js CSP build 3.17.5, `google/go-cmp` v0.7.0, `golangci-lint` v2.15.0.

**Spec:** `docs/specs/2026-10-24-core-platform-spec.md` (read §2, §4, §5, §8, §9 before starting) and `docs/specs/contract-changes.md` (Rounds 1–4, CD-01 to CD-92; lead decision CD-74 on subtest names, Task 7; Round 4 CD-84 to CD-92 below). Fixture values: `docs/tests/core/00-fixtures.md`.

## Global Constraints

- Module path: `github.com/vardhan/internaltools`. Go directive: `go 1.27`. Toolchain: `go1.27.3`.
- Open-source dependencies only, each at the latest stable version listed in spec §2. Before adding one, run `go list -m -versions <module>` and use the newest non-pre-release; if it differs from §2, use the newer one and tell the lead (the lead updates §2).
- No floating point for business values anywhere (spec §3, D-NUM-01).
- Every write goes through `db.Write` (`BEGIN IMMEDIATE`). Nothing else opens a transaction. `(*db.Tx).SQL()` returns `db.DBTX`, which has no `Commit` or `Rollback` (CD-06).
- SQLite pragmas on every connection: `journal_mode=WAL`, `busy_timeout=5000` (overridable only through `db.WithBusyTimeout`, CD-08), `foreign_keys=ON`, `synchronous=NORMAL`.
- Migrations live in `migrations/` and use the number ranges in spec §4.5. WP0 uses 00001–00099. Declared column types are only `INTEGER`, `TEXT`, `BLOB` (CD-34), in production and test-only migrations alike. Every `goose_*` version table (`goose_db_version`, `goose_testkit`, …) is excluded from that rule, because goose creates them (CD-71).
- Every kernel error that reaches a handler implements `doctype.HTTPError` (CD-44). The sentinels WP0 creates (`doctype.ErrNotFound` → 404, `auth.ErrForbidden` → 403) implement it from the start. There is one not-found sentinel: `auth.ErrNotFound` is declared as `var ErrNotFound = doctype.ErrNotFound` (CD-54).
- Every static file is served under a content-hashed name with the immutable cache header; unhashed names are not served (CD-47).
- WP0 creates **no** `sessions` table and does not add `github.com/alexedwards/scs/sqlite3store` (CD-14). AUTH owns the sessions table (00100–00199) and the `scs.CtxStore`.
- §5 is copied exactly: package paths, names, signatures and field lists. WP0 may add only the extra names this plan lists (each marked "WP0 addition"); everything else is for the owning WP.
- Each package with SQL has its own `sqlc.yaml`; generated code goes in `<pkg>/dbq/` and is committed (§4.4).
- Tests never read the wall clock; they use `testkit.Clock()` (2026-10-24T05:00:00Z = 24-10-2026 10:30 IST).
- Copy: error messages are plain English and say how to fix the problem.

## Review Focus

1. **Two writers at once.** Two goroutines call `db.Write` together: the second waits (busy timeout) and both commit; neither gets `db.ErrBusy`. Pinned by `TestDB_ConcurrentWritesSerialise` (Task 3).
2. **A lock held past the busy timeout.** `db.Write` returns an error matching `errors.Is(err, db.ErrBusy)` with the text "The system is busy. Please try again in a moment." and writes nothing. Pinned by `TestDB_WriteBusyReturnsErrBusy` (Task 3).
3. **A hook trying to commit.** `tx.SQL()` must not expose `Commit`/`Rollback` even by type assertion. Pinned by `TestDB_SQLCannotCommit` (Task 3).
4. **A panic inside `db.Write`.** The transaction rolls back and the panic is re-raised; the next `db.Write` works. Pinned by `TestDB_WriteRollsBackOnPanic` (Task 3).
5. **A kernel package importing what §4.2 forbids** (the cycles CD-01/CD-02 broke). Pinned by `TestContracts_KernelImportsFollowTable` (Task 5).
6. **The approval fake accepting a call order DOC must never use** (CD-23). Pinned by `TestContracts_ApprovalFakeProtocol` (Task 5).
7. **A test that forgets to close the DB.** `testkit.NewDB` registers `t.Cleanup` to close it and delete the temp dir. Pinned by `TestTestkit_NewDBCleansUp` (Task 6).
8. **The test-ID checker** seeing a Go test name inside a comment or string (only `func TestX(` / `func FuzzX(` declarations in `_test.go` files count), and `--exclude-level e2e` hiding only e2e cases. Pinned by `TestCheckTestIDs_IgnoresComments` and `TestCheckTestIDs_ExcludeLevel` (Task 7).
9. **A request without the CSRF origin headers.** A cross-origin POST is rejected with 403 before AUTH's middleware runs; a same-origin POST passes. Pinned by `TestWeb_CrossOriginPostRejected` (Task 4).
10. **Two not-found sentinels drifting apart.** `auth.ErrNotFound == doctype.ErrNotFound` and both report status 404 through `doctype.HTTPError`. Pinned by `TestContracts_ErrorsImplementHTTPError` (Task 5).
11. **A stale front-end file cached for a year.** htmx and Alpine are served only under hashed names; the old unhashed path is 404. Pinned by `TestWeb_StaticHashedNames` (Task 4).

## Contract decisions applied (Round 1)

This plan raised no requests of its own; Round 1 changed what WP0 must produce. Each change and the decision behind it:

- `internal/types` gains `UserID`, `Page`, `Result[T]`, the `Parse*`/`Format*` signatures; `auth.UserID`, `list.Page`, `list.Result` become aliases (CD-01, CD-33). Previously WP0 declared `UserID` in `auth`.
- New package `internal/kernel/doctype` holds the registry and every DocType type; `doc` only re-exports aliases (CD-02, CD-09, CD-21, CD-35). Previously all of it was in `doc/contract.go`; `list` re-exported `SortKey` from `doc`, now it uses `doctype.SortKey` directly.
- `audit.Writer`, `audit.Default`, `audit/auditfake`; `Change` gains `LineNo`, `LineID`, `Op` (CD-03, CD-20).
- `db.DBTX`, `(*Tx).SQL() DBTX`, `(*Tx).Writable()`, `db.Open(path, opts...)` (no `ctx` parameter any more), `db.WithBusyTimeout`, `db.ErrBusy`, `testkit.DBPath` (CD-06, CD-07, CD-08).
- `auth`: scope `team`, `UserInfo`, `User`/`EnableUser`/`CheckValues`/`CheckLink`, `Directory` (every method takes `tx`), `WithPrincipal`/`PrincipalFrom` (CD-11, CD-12, CD-13).
- `approval`: the local `ManagerResolver` stand-in is deleted; APR uses `auth.Directory` (CD-13). `Requirement` gains `Amount`, `Requester`; `Record` takes `doctype.Action`; `approvalfake` enforces the CD-23 protocol (CD-23, CD-24).
- `naming.Admin`, `SeriesInfo` and helper functions (CD-28). `doc.Engine` gains `References` and embeds `doctype.InfoSource` (CD-05, CD-22).
- `files`: `Spooled`, `Attach(… f *Spooled)`, `List`; `CopyAll` records `to.CreatedBy` (CD-29). `print.Letterhead` (CD-30). `gstcalc.LineAmount` returns an error, `StateName` (CD-31). `csvio.RowFunc`/`RegisterImporter` (CD-32); the old WP0-invented `csvio.Import`/`Export` signatures are dropped (NUM declares its own).
- Sessions: no `sqlite3store`, no sessions table in WP0; `web.Options` accepts AUTH's session manager **and** AUTH's middleware (CD-14, CD-15, CD-17).
- `docs/specs/contract-changes.md` already exists (Round 1 log); WP0 no longer creates it (Task 1).
- Checker name and flags: `go run ./scripts/checktestids <AREA> --exclude-level e2e` (CD-40, CD-41). WP0 owns no test-case IDs, so CD-40's exit rule applies to WP0 only through the checker it builds.

## Contract decisions applied (Rounds 2 and 3)

- `doctype.Field` gains `ListPriority`, `MaxLen int`, `Options []string`, `Hint`, `Step string` (CD-42). `doctype.HTTPError` (CD-44). `doctype.Info.Party` (CD-55). `doctype.ErrNotFound` is now §5 (no longer a WP0 addition), with `Action*`/`State*` constants for every value (CD-54).
- `doc.Record`, `Engine.Load`, `Result.Warnings`; `docfake` gains `SetRecord` and `Load` (CD-43, CD-68). `docfake.SetInfo` is confirmed (CD-54).
- `audit.Ref` gains `Number` and `Action`; tests use keyed literals (CD-69).
- `auth.ErrNotFound` is the same value as `doctype.ErrNotFound` (CD-54). `authfake` gets the CD-56 seeding helpers `Grant`, `AddRole`, `Restrict(user, projects, sites)`, `Hide(doctype, id)`; these replace the earlier `SetRoles`, `Restrict(u, field, …)` and `Hide(u, target, id)`. `authfake.NewDirectory()` is the one shared Directory fake; `UserForEmployee` and `DirectReports` skip employees who have left (CD-54, CD-56).
- `naming.Admin.PreviewTemplate` (CD-57). `naming.Label` is implemented for real in WP0 (CD-53).
- `print.LetterheadInfo` has its CD-58 fields (no longer an empty struct).
- `list` may import `naming` (CD-59); the import-table test is updated.
- Makefile: `generate` runs `go generate ./...` first; `e2e` runs `go test -tags e2e ./...` (CD-45).
- Web: htmx errors go to a persistent `role="alert"` region, never a toast; 409 is swapped like 422; htmx and Alpine are served under content-hashed names (CD-47). Sign-in routes are `/signin` and `/signout` with 303 redirects (CD-63; AUTH implements them, WP0 only documents the hook).
- The column-type rule skips every `goose_*` table (CD-71). `testkit.Principal(t, email)` is confirmed (CD-72).
- Checker: a **Go test:** value may be `TestFunc/sub_name`, matched on the part before the first `/`; e2e cases must name a `TestE2E_*` function (CD-74, lead decision).

## Contract decisions applied (Round 4)

- Working bodies in `contract.go` (CD-84): the `contract.go` files hold the §5 declarations. The bodies WP0 ships working (the `doctype` registry, `auth.WithPrincipal`/`PrincipalFrom`, the scope constants, `naming.Label`, plus the error sentinels) are listed in Task 5. The owning WP may move such a body into its own file and lists `contract.go` under Modify. A name is never declared twice.
- Import-table test (CD-85): `TestContracts_KernelImportsFollowTable` checks each kernel package directory itself against the §4.2 table. Subpackages follow separate rules: `<pkg>fake` and `<pkg>test` may import `testkit` and any fake; `<area>/web` and its subpackages may import `internal/ui`, `platform/web` and the area's own test helpers. Task 5 adds table cases for these rules.
- Fakes (CD-86): `authfake.RemoveRole(u, role)` is added. `UsersWithRight` skips users added with `Enabled: false`. `Grant` accepts the wildcard DocType `"*"` (the Administrator row in `00-fixtures.md`). `auditfake.New()` returns `*auditfake.Recorder`. `docfake.SetResult(doctype, id, action, result, err)`.
- `platform/web` (CD-90) imports no kernel package. `web.ErrorFrom` matches a local `interface{ error; Status() int }` with `errors.As`, which `doctype.HTTPError` satisfies. `TestWeb_BusyIs503` declares its own HTTP-error type in the test file.
- Checker (CD-92): the test docs now name APR-097 `TestE2E_ApprovalWithdraw`, APR-079 `TestE2E_ApprovalInboxRowContents` and FILE-056 `TestE2E_PrintA4` (CD-74, CD-77). Task 7's first real run expects no `ERROR` lines.

---

### Task 1: Toolchain, repository and build commands

**Files:**
- Create: `go.mod`, `.gitignore`, `Makefile`, `.golangci.yml`, `README.md`
- Do not create or edit: `docs/specs/contract-changes.md` (exists; owned by the lead)

**Interfaces:**
- Produces: `make generate`, `make build`, `make test`, `make lint`, `make checkids AREA=<AREA>`, `make ci`, `make e2e` targets used by every WP.

- [ ] **Step 1: Install Go 1.27.3 and check it**

Run: `go version`
Expected: `go version go1.27.3 darwin/arm64`. If it shows an older version, install 1.27.3 from https://go.dev/dl/ and re-run.

- [ ] **Step 2: Initialise git and the module**

Run in the repository root:
```bash
git init -b main
go mod init github.com/vardhan/internaltools
go mod edit -go=1.27 -toolchain=go1.27.3
go get -tool github.com/a-h/templ/cmd/templ@v0.3.1070
go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
go get -tool github.com/pressly/goose/v3/cmd/goose@v3.29.0
```
Expected: `go.mod` lists exactly these three `tool` directives. Runtime dependencies are added by the task that first imports them (Task 3: `modernc.org/sqlite`, `goose/v3`; Task 4: `chi/v5`, `scs/v2`; Task 5: `templ` runtime; tests: `go-cmp`). `go list -m all | grep sqlite3store` prints nothing.

- [ ] **Step 3: Write `.gitignore`**

Ignore `reference/` (local research clones), `/bin/`, `/data/`, `*.db`, `*.db-wal`, `*.db-shm`, `/tmp/`, `/.work/` (agent status files, execution protocol §6).

- [ ] **Step 4: Write the `Makefile`**

Targets: `generate` (in this order, CD-45: `go generate ./...` (UI's CSS bundle and icons), then `go tool sqlc generate -f <file>` for every `sqlc.yaml` found by `find internal -name sqlc.yaml`, then `go tool templ generate`; generated files are committed), `build` (`CGO_ENABLED=0 go build -trimpath -o bin/app ./cmd/app`), `test` (`go test -race -count=1 ./...`), `e2e` (`go test -tags e2e -count=1 ./...`, CD-45), `lint` (`golangci-lint run`), `checkids` (`go run ./scripts/checktestids $(AREA) --exclude-level e2e`; the WP done-check, spec §9 rule 3), `ci` (`generate`, then `git diff --exit-code` to prove generated code is committed, then `lint test build`, then `go run ./scripts/checktestids --report`).

- [ ] **Step 5: Install golangci-lint v2.15.0 and write `.golangci.yml`**

Install the binary with the official install script pinned to `v2.15.0`. Enable `govet`, `staticcheck`, `errcheck`, `ineffassign`, `unused`, `gosec`, `forbidigo` with a rule forbidding `float32|float64` in non-test files under `internal/`. Disable staticcheck's `ST1005` (error strings capitalised/punctuated): user-facing sentences such as `db.ErrBusy` are error values by design.

- [ ] **Step 6: Confirm the decision log is present**

Run: `grep -c '^| CD-' docs/specs/contract-changes.md`
Expected: `92` or more (CD-01 to CD-92). If the file is missing or the count is lower, stop and ask the lead; don't recreate it.

- [ ] **Step 7: Verify and commit**

Run: `make lint && go build ./...`
Expected: no errors (empty module builds).
```bash
git add -A && git commit -m "chore: initialise repository, toolchain and build commands"
```

### Task 2: Clock and errors

**Files:**
- Create: `internal/platform/clock/clock.go`, `internal/platform/clock/clock_test.go`, `internal/platform/errs/errs.go`

**Interfaces:**
- Produces: `clock.Clock` (`Now() time.Time`), `clock.Real{}`, `clock.Fixed` with `NewFixed(t time.Time) *Fixed`, `(*Fixed).Advance(d time.Duration)`, `(*Fixed).Set(t time.Time)`; `clock.IST *time.Location` (from embedded `time/tzdata`); `errs.ErrNotImplemented` (text "not implemented yet").

- [ ] **Step 1: Write the failing tests**

```go
func TestClock_FixedAdvance(t *testing.T) {
	c := clock.NewFixed(time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC))
	c.Advance(15 * time.Minute)
	if got := c.Now(); !got.Equal(time.Date(2026, 10, 24, 5, 15, 0, 0, time.UTC)) {
		t.Fatalf("Now() = %v", got)
	}
}
func TestClock_ISTWithoutZoneinfo(t *testing.T) {
	t.Setenv("ZONEINFO", "/nonexistent")
	got := time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC).In(clock.IST).Format("15:04")
	if got != "10:30" { t.Fatalf("IST = %s", got) }
}
```

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/platform/clock/`
Expected: FAIL, package does not exist.

- [ ] **Step 3: Implement** the types above; `IST` via `time.LoadLocation("Asia/Kolkata")` with `import _ "time/tzdata"`; `Fixed` is safe for concurrent use (mutex).

- [ ] **Step 4: Run the tests to see them pass**

Run: `go test ./internal/platform/clock/`
Expected: PASS.

- [ ] **Step 5: Commit**

`git add internal/platform && git commit -m "feat(platform): add clock and shared errors"`

### Task 3: Database

**Files:**
- Create: `internal/platform/db/db.go`, `internal/platform/db/options.go`, `internal/platform/db/migrate.go`, `internal/platform/db/db_test.go`, `migrations/embed.go`, `migrations/00001_schema_conventions.sql`

**Interfaces:**
- Consumes: nothing.
- Produces (§5 exactly, plus the WP0 additions marked):
  - `type DBTX interface{ ExecContext; PrepareContext; QueryContext; QueryRowContext }` with the four signatures in §5 (sqlc's method set, so `dbq.New(tx.SQL())` compiles).
  - `type Option` (opaque, WP0 detail) and `func WithBusyTimeout(d time.Duration) Option`; default 5 s.
  - `func Open(path string, opts ...Option) (*DB, error)` — applies the Global Constraints pragmas to every pooled connection through DSN `_pragma` parameters (`busy_timeout` in milliseconds from the option), `SetMaxOpenConns(8)`.
  - `var ErrBusy error` — text exactly "The system is busy. Please try again in a moment."
  - `(*DB) Write(ctx context.Context, fn func(tx *Tx) error) error` — `BEGIN IMMEDIATE`, commit on nil, roll back on error or panic (re-panic). Any error whose SQLite primary result code is `SQLITE_BUSY` (from `BEGIN IMMEDIATE`, a statement inside `fn`, or `COMMIT`) is returned wrapped so `errors.Is(err, ErrBusy)` holds and `err.Error()` is the `ErrBusy` text.
  - `(*DB) Read(ctx context.Context, fn func(tx *Tx) error) error` — read-only deferred transaction.
  - `(*Tx) SQL() DBTX` — returns an unexported wrapper around the `*sql.Tx` that has only the four `DBTX` methods (CD-06).
  - `(*Tx) Writable() bool` — true inside `Write`, false inside `Read` (CD-07).
  - WP0 additions: `(*DB) Close() error`, `(*DB) SQL() *sql.DB` (goose and tests only), `(*DB) Path() string`.
  - `db.Migrate(ctx context.Context, d *DB, fsys fs.FS, table string) error` — runs a goose `Provider` over `fsys` with version table `table`.
  - `migrations.FS embed.FS` (all `*.sql` in `migrations/`).
  - `00001_schema_conventions.sql` creates `app_meta(key TEXT PRIMARY KEY, value TEXT NOT NULL)` and inserts `('schema_conventions','1')`. No `sessions` table (CD-14).

- [ ] **Step 1: Write the failing tests**

- `TestDB_OpenSetsPragmas`: on two separate pooled connections, `PRAGMA journal_mode` = `wal`, `busy_timeout` = `5000`, `foreign_keys` = `1`, `synchronous` = `1`.
- `TestDB_WithBusyTimeout`: `Open(path, WithBusyTimeout(200*time.Millisecond))` → `PRAGMA busy_timeout` = `200` on two connections.
- `TestDB_WriteCommits`, `TestDB_WriteRollsBackOnError`.
- `TestDB_WriteRollsBackOnPanic`: panic propagates (caught with `recover` in the test); a following `Write` succeeds.
- `TestDB_ConcurrentWritesSerialise`: two goroutines each insert 200 rows in separate `Write` calls that sleep 1 ms inside; both return nil; 400 rows exist.
- `TestDB_WriteBusyReturnsErrBusy`: a second raw `sql.Open("sqlite", path)` connection runs `BEGIN IMMEDIATE` and holds it; `Open(path, WithBusyTimeout(200*time.Millisecond))` then `Write` inserting one row → `errors.Is(err, db.ErrBusy)`, `err.Error()` = "The system is busy. Please try again in a moment.", elapsed ≥ 200 ms and < 2 s; after the holder rolls back, the row count is 0 and a new `Write` succeeds.
- `TestDB_TxWritable`: `tx.Writable()` is true inside `Write`, false inside `Read`.
- `TestDB_SQLCannotCommit`: inside `Write`, `tx.SQL().(interface{ Commit() error })` and `tx.SQL().(interface{ Rollback() error })` both fail the type assertion; `var _ db.DBTX = (*sql.Tx)(nil)` and `(*sql.DB)(nil)` compile.
- `TestDB_MigrateAppliesEmbedded`: `app_meta` has `schema_conventions=1`; no table named `sessions` exists.
- `TestDB_SQLiteVersionAtLeast`: `select sqlite_version()` ≥ `3.51.3`.

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/platform/db/`
Expected: FAIL, package does not exist.

- [ ] **Step 3: Implement** with `modernc.org/sqlite` v1.61.0 (driver name `sqlite`). `BEGIN IMMEDIATE` is issued by taking a dedicated `*sql.Conn` and executing it, then wrapping that connection's transaction; `Read` uses `BeginTx` with `ReadOnly: true`. Detect BUSY through the driver's error type and result-code constants (`modernc.org/sqlite` `*sqlite.Error`.`Code()` masked to the primary code, compared with `sqlite3.SQLITE_BUSY` from `modernc.org/sqlite/lib`); confirm those names against the installed v1.61.0 source before use.

- [ ] **Step 4: Run the tests to see them pass**

Run: `go test -race ./internal/platform/db/`
Expected: PASS.

- [ ] **Step 5: Commit**

`git add internal/platform/db migrations go.mod go.sum && git commit -m "feat(platform): add SQLite access with single-writer transactions, busy mapping and migrations"`

### Task 4: Web platform

**Files:**
- Create: `internal/platform/web/router.go`, `internal/platform/web/middleware.go`, `internal/platform/web/errors.go`, `internal/platform/web/static.go`, `internal/platform/web/static/htmx.min.js`, `internal/platform/web/static/alpine.min.js`, `internal/platform/web/static/SOURCES.md`, `internal/platform/web/web_test.go`

**Interfaces:**
- Produces:
  - `web.NewRouter(opts Options) chi.Router` with middleware in this order: request ID, `slog` access log, recover (500 page), security headers, `http.CrossOriginProtection`, session load (`opts.Sessions.LoadAndSave`, skipped when nil), then `opts.Auth` (skipped when nil).
  - `web.Options{Logger *slog.Logger; Sessions *scs.SessionManager; Auth func(http.Handler) http.Handler; TrustedOrigins []string}`. AUTH builds the `*scs.SessionManager` over its own `CtxStore` and supplies `Auth` (principal loading, synchronizer CSRF token CD-15, expired or missing session → 303 to `/signin?next=…` or 401, CD-17/CD-63). AUTH owns the `/signin` and `/signout` routes; their redirects are 303 (CD-63). WP3 wires both. `platform/web` imports no kernel package.
  - `web.ErrorFrom(w http.ResponseWriter, r *http.Request, err error)` (CD-76, CD-90): an error matching the unexported local interface `statusError interface{ error; Status() int }` through `errors.As` → its `Status()` and message (`doctype.HTTPError` satisfies it, so kernel errors map without `platform/web` importing `doctype`); `errors.Is(err, db.ErrBusy)` → 503 with the `db.ErrBusy` text; anything else → 500 page. It then calls `web.Error`.
  - `web.Notice{Level, Text string}`, `web.Flash(ctx context.Context, level, text string)` (appends) and `web.TakeFlash(ctx context.Context) []web.Notice` (CD-80): messages stored in the scs session in order, read and cleared by the next full page. Test `TestWeb_FlashSurvivesRedirect` (two flashes, 303, next GET reads both in order; a second GET sees none).
  - `web.Error(w http.ResponseWriter, r *http.Request, status int, message string)`. Errors are never toasts (CD-47):
    - Full-page request: a plain error page with the message (HTML-escaped) in `<div role="alert">`.
    - htmx request with status 409 or 422: the body is the fragment `<div class="error" role="alert">message</div>` and there is no `HX-Trigger`. The htmx config in UI's layout swaps these two codes (CD-47; the 409 swap shows the version-conflict content).
    - Any other htmx error status: the body is empty, and the header `HX-Trigger: {"notice":{"level":"error","text":"…"}}` is set. UI's `app.js` (CD-75) shows `level: "error"` in the persistent `<div id="alerts" role="alert">` region, which stays until closed.
  - `web.Static() http.Handler` serving `/static/*` from `embed`. Every file is served only under its content-hashed name `<base>.<first 8 hex of SHA-256>.<ext>`, with `Cache-Control: public, max-age=31536000, immutable`. An unhashed or unknown name returns 404 (CD-47). The files on disk keep their plain names.
  - WP0 addition: `var HTMXURL, AlpineURL string`, computed at init from the embedded bytes (`/static/htmx.<hash>.js`, `/static/alpine-csp.<hash>.js`). The shell references these and never hard-codes a path. UI hashes its own `app.js`, CSS and fonts under `/static/ui/` the same way.
  - Security headers exactly: `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`, `X-Frame-Options: DENY`, `Content-Security-Policy: default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; frame-ancestors 'none'`.

- [ ] **Step 1: Download the pinned front-end files**

htmx 2.0.11 `dist/htmx.min.js` and the Alpine.js **CSP build** `@alpinejs/csp` 3.17.5 `dist/cdn.min.js` (save as `alpine.min.js`; the standard build needs `unsafe-eval`, spec §2) from the npm registry tarballs. Record each version, package name, source URL, licence (htmx 0BSD, Alpine MIT) and SHA-256 in `SOURCES.md`.

- [ ] **Step 2: Write the failing tests**

`TestWeb_SecurityHeaders` (GET `web.HTMXURL` has every header above and `Cache-Control: public, max-age=31536000, immutable`), `TestWeb_StaticHashedNames` (`HTMXURL` matches `^/static/htmx\.[0-9a-f]{8}\.js$` and `AlpineURL` matches `^/static/alpine-csp\.[0-9a-f]{8}\.js$`; each hash is the first 8 hex digits of the SHA-256 of the embedded file; both return 200 with the immutable header; `/static/htmx.min.js`, `/static/alpine.min.js` and `/static/htmx.00000000.js` return 404), `TestWeb_CrossOriginPostRejected` (POST with `Origin: https://evil.example` and `Sec-Fetch-Site: cross-site` → 403 and the `Auth` middleware is never called; same-origin POST → 200), `TestWeb_MiddlewareOrder` (with a real `scs.New()` manager and a recording `Auth` middleware: `Auth` runs after session load, so `Sessions.Put` inside it does not panic, and the handler sees the value; with `Sessions` and `Auth` nil the router still serves), `TestWeb_BusyIs503` (CD-76, CD-90: a handler returning an error wrapping `db.ErrBusy` through `web.ErrorFrom(w, r, err)` → 503 with "The system is busy. Please try again in a moment."; an error of a type declared in `web_test.go` itself (`type teapotErr struct{}` with `Error() string` returning "Short and stout." and `Status() int` returning 418), wrapped with `fmt.Errorf("x: %w", …)` → 418 with that text; the test file imports no kernel package), `TestWeb_PanicGives500Page` (handler panics → 500 with body containing "Something went wrong. Try again, and if it keeps happening tell the administrator."), `TestWeb_HTMXErrorToAlertRegion`: `HX-Request: true` and `web.Error(w, r, 403, "You don't have permission to read fixture.Order. Ask your administrator for a role that allows it.")` → status 403 and `HX-Trigger` `{"notice":{"level":"error","text":"You don't have permission to read fixture.Order. Ask your administrator for a role that allows it."}}`.
`TestWeb_HTMXSwappedErrorsAreFragments`: with `HX-Request: true`, `web.Error(w, r, 422, "Quantity must be more than 0.")` → status 422 and a body with one `role="alert"` element holding that text; the same with status 409 and message `<b>x</b>` → status 409 and the text escaped as `&lt;b&gt;x&lt;/b&gt;`; neither response has `HX-Trigger`.
`TestWeb_FullPageErrorHasAlert`: the same 422 call without `HX-Request` → an HTML page whose `role="alert"` element holds the message, `TestWeb_StaticFilesMatchSources` (SHA-256 of embedded files equals `SOURCES.md`).

- [ ] **Step 3: Run them to see them fail**

Run: `go test ./internal/platform/web/`
Expected: FAIL.

- [ ] **Step 4: Implement** using `go-chi/chi/v5` v5.5.0, `alexedwards/scs/v2` v2.9.0 (the `scs` package only) and the standard library's `http.NewCrossOriginProtection()` (add `TrustedOrigins` with `AddTrustedOrigin`). `static.go` builds a map from hashed name to embedded file once at init.

- [ ] **Step 5: Run the tests to see them pass**

Run: `go test -race ./internal/platform/web/ && go list -m all | grep -c sqlite3store && go list -deps ./internal/platform/web/ | grep -c internal/kernel`
Expected: PASS, then `0`, then `0` (CD-90: `platform/web` imports no kernel package).

- [ ] **Step 6: Commit**

`git add internal/platform/web go.mod go.sum && git commit -m "feat(platform): add router, security middleware, alert-region errors and hashed htmx/alpine"`

### Task 5: Contract stubs and fakes for every kernel package

**Files:**
- Create one `contract.go` per package with exactly the exported declarations in spec §5, plus the fakes:
  - `internal/types/types.go`: `Money`, `Qty`, `Rate`, `Percent`, `Factor`, `Date`, `Timestamp`, `FY`, `FiscalYear`, `UserID`, `Page`, `Result[T]`, `ParseMoney`, `ParseQty`, `ParseRate`, `ParsePercent`, `ParseDate`, `FormatMoney`, `FormatINR`, `FormatQty`, `FormatQtyUoM`, `FormatRate`, `FormatPercent`, `FormatDate`, `FormatIST(t time.Time) string` (signatures only; NUM implements; examples in §5, e.g. `FormatIST` → "24-10-2026 10:30").
  - `internal/kernel/doctype/contract.go`: `State` (consts `StateDraft="draft"`, `StatePending="pending_approval"`, `StateApproved="approved"`, `StateClosed="closed"`, `StateRejected="rejected"`, `StateCancelled="cancelled"`), `Action` (consts `ActionSave` … `ActionDelete` for `save submit withdraw approve reject revise close cancel amend delete`), `FieldError`, `FieldType` (consts `Text Int Check Select Link Money Qty Rate Percent Factor Date Timestamp`), `Field{Name, Label, Column string; Type FieldType; Link string; Required, Filterable, Sortable, Searchable, Importable, OnLine bool; ListPriority, MaxLen int; Options []string; Hint, Step string; ReadOnly bool}` (CD-42, CD-79), `SortKey`, `LinkSpec`, `RollUp`, `Hooks`, `MasterInfo`, `MasterLookup`, `DocType` (with `PluralLabel`, `OwnerField`, `EmployeeField`, `ProjectFields`, `SiteFields`, `DefaultRights`, `Hooks`, `Master`), `Register`, `Lookup`, `All`, `Info` (with `Party string`, CD-55), `InfoSource`, `var ErrNotFound error` (CD-54; returned by `InfoSource.Info` for a missing document), `HTTPError interface{ error; Status() int; FieldErrors() []FieldError }` (CD-44)
  - `internal/kernel/audit/contract.go` (`Ref{DocType string; DocID int64; Number, Action string}` (CD-69), `Change{Field, Before, After string; LineNo int; LineID int64; Op string}`, `Entry`, `Writer`, `Default`, `Append`), `internal/kernel/audit/auditfake/fake.go`
  - `internal/kernel/auth/contract.go` (`UserID = types.UserID`, `Right`, `Scope` with consts `ScopeOwn`, `ScopeTeam`, `ScopeAll`, `Principal`, `RecordRef`, `Predicate`, `UserInfo`, `ErrForbidden`, `var ErrNotFound = doctype.ErrNotFound` (CD-54), `Service`, `Directory`, `WithPrincipal`, `PrincipalFrom`), `internal/kernel/auth/authfake/fake.go`, `internal/kernel/auth/authfake/directory.go`
  - `internal/kernel/naming/contract.go` (`Service`, `SeriesInfo`, `Admin` with `PreviewTemplate(ctx, tpl string, d types.Date) (string, error)` (CD-57), `Label`, `ValidateGSTDocNumber`, `GuardNumberField`, `CheckAmendmentNo`), `internal/kernel/naming/namingfake/fake.go`
  - `internal/kernel/doc/contract.go` (aliases for every `doctype` name: types as `type X = doctype.X`, the State/Action/FieldType constants, `Register`/`Lookup`/`All` as package-level funcs calling `doctype`; plus `ActionInput`, `Result{ID int64; State State; Number string; Version int; Warnings []string}` (CD-68), `Reference`, `Record{Info doctype.Info; Version int; Values map[string]string; Lines []map[string]string}` (CD-43), `Engine` with `Load(ctx, tx, doctype, id) (Record, error)` (CD-43)), `internal/kernel/doc/docfake/fake.go`
  - `internal/kernel/approval/contract.go` (`Requirement` with `Amount types.Money` and `Requester auth.UserID`, `Service`, `InboxItem`; the CD-23 protocol copied as a comment; **no** `ManagerResolver`), `internal/kernel/approval/approvalfake/fake.go`
  - `internal/kernel/files/contract.go` (`Meta`, `Spooled` (opaque), `Service`; WP0 addition: `type Store struct{ /* FILE */ }` and `func (s *Store) Spool(r io.Reader, limit int64) (*Spooled, error)` stub, the creator CD-29 names), `internal/kernel/files/filesfake/fake.go`
  - `internal/kernel/print/contract.go` (`Register`, `InWordsINR`, `Letterhead`, `LetterheadInfo{LegalName, LogoURL, BranchName string; Address []string; StateName, StateCode, GSTIN string}`, CD-58)
  - `internal/kernel/gstcalc/contract.go` (`LineAmount(q types.Qty, r types.Rate) (types.Money, error)`, `SplitGST`, `IsInterState`, `Totals`, `StateName`, `Line{Amount types.Money; GSTRate types.Percent; InterState bool}`, `LineTax{Taxable, CGST, SGST, IGST types.Money}`, `DocTotals{Lines []LineTax; Net, CGST, SGST, IGST, Grand, Rounded, Adjustment, Payable types.Money}`)
  - `internal/kernel/list/contract.go` (`Page = types.Page`, `Result[T any] = types.Result[T]`, `Query` with `Sort []doctype.SortKey`, `Filter{Field string; Op Op; Values []string}`, `Op string` type with no constants (NUM adds them), `Build`; no `DisplayNumber`, because `list` uses `naming.Label`, CD-59)
  - `internal/kernel/csvio/contract.go` (`RowFunc`, `RegisterImporter`; nothing else)
  - `internal/masters/gstin/contract.go` (`ValidateGSTIN`, `StateCode`, `PANFromGSTIN`)
  - `internal/contracts_test.go` (package `internal_test`)
- Delete from the earlier draft: `approval.ManagerResolver`, `list.SortKey` re-export, `csvio.Import`/`Export`, `doc`-owned DocType type definitions (now aliases), a separate `auth.ErrNotFound` value (CD-54), `authfake.SetRoles` and the old `Restrict`/`Hide` signatures (CD-56).

**Interfaces:**
- Consumes: `db.Tx`, `db.DBTX`, `clock.Clock`, `errs.ErrNotImplemented`.
- Produces: every symbol in spec §5, compiled, with exactly the §5 signatures and field lists. Imports follow the §4.2 table (CD-04, CD-59): `doctype` imports only `types` and `platform/db`; `types` imports no internal package; `audit` imports no kernel package; `list` may import `auth` and `naming`.
- Stub rules: functions and methods with an `error` result return zero values and `errs.ErrNotImplemented`; functions without one return the zero value.
- Working implementations in `contract.go` (CD-84): the bodies named below as "implemented for real" (the `doctype` registry, `auth.WithPrincipal`/`PrincipalFrom`, the `auth` scope constants, `naming.Label`, and the error sentinels) are working code, not stubs. Each sits in its package's `contract.go` beside the §5 declarations. The owning WP (DOC for `doctype`, AUTH for `auth`, NAM for `naming`) may move a body into a file of its own; it then lists `contract.go` under **Modify** and deletes the body there, so no name is declared twice. Each such body carries a comment `// Working WP0 body; the owning WP may move it out of contract.go (CD-84).` `audit.Default` is a stub `Writer` whose `Append` returns `errs.ErrNotImplemented`, and `audit.Append` calls `Default.Append`. Implemented for real by WP0 because they have no error path and every Wave 1 WP needs them: `auth.WithPrincipal`/`PrincipalFrom` (unexported context key); `naming.Label` (returns `number`, or `"Draft " + strconv.FormatInt(id, 10)` when `number == ""`, CD-53); the error sentinels (below); and the `doctype` registry (`Register` panics on an empty `Name` or a duplicate; `Lookup`; `All` sorted by `Name`; mutex-guarded). DOC adds full spec validation to `Register` later without changing the signatures.
- Error sentinels (CD-44, CD-54): `doctype.ErrNotFound` is a value of an unexported type implementing `doctype.HTTPError` with `Status() == 404`, `FieldErrors() == nil` and text "This record doesn't exist or you don't have access to it." (the AUTH plan's 404 text). `auth.ErrNotFound` is declared as `var ErrNotFound = doctype.ErrNotFound`, so there is one value. `auth.ErrForbidden` is a WP0 placeholder implementing `doctype.HTTPError` with `Status() == 403`; AUTH may replace its value but must keep the interface. `doctype` declares `State*` constants (`StateDraft="draft"`, `StatePending="pending_approval"`, `StateApproved="approved"`, `StateClosed="closed"`, `StateRejected="rejected"`, `StateCancelled="cancelled"`) and `Action*` constants (`ActionSave`, `ActionSubmit`, `ActionWithdraw`, `ActionApprove`, `ActionReject`, `ActionRevise`, `ActionClose`, `ActionCancel`, `ActionAmend`, `ActionDelete`) for every §5 value.
- Fakes (each guarded by a mutex; each has a compile-time assertion in its own file):
  - `authfake.New() *Fake` implements `auth.Service`. Seeding helpers (CD-56, CD-86): `Grant(role, doctype string, r auth.Right, s auth.Scope)` (doctype `"*"` grants the right on every DocType, the Administrator row in `00-fixtures.md`), `AddRole(u auth.UserID, role string)`, `RemoveRole(u auth.UserID, role string)` (no-op when the user doesn't hold it), `Restrict(u auth.UserID, projects, sites []int64)`, `Hide(doctype string, id int64)`. Other helpers: `Allow(u auth.UserID, doctype string, rights ...auth.Right)` (scope `all`), `Deny(u, doctype, rights...)`, `SetScope(doctype string, p auth.Predicate)`, `SetUsers(doctype string, r auth.Right, users []auth.UserID)`, `UseDirectory(d auth.Directory)`, `AddUser(info auth.UserInfo)`.
    - `Can`: a user's roles are `p.Roles` plus those added by `AddRole`. With no `Allow` and no granted role, it denies, wrapping `auth.ErrForbidden` with the message "You don't have permission to {right} {doctype}. Ask your administrator for a role that allows it." When the right is granted and `rec` is non-nil, it returns `auth.ErrNotFound` in three cases: the record was hidden with `Hide`; the record's `ProjectIDs`/`SiteIDs` fall outside a `Restrict` list; or the scope fails. Scope `own` means `rec.CreatedBy == p.ID`. Scope `team` means own, or `rec.EmployeeID` is one of the user's direct reports through `UseDirectory`; without a directory, team works like own.
    - `CheckValues`: ids in the DocType's `ProjectFields`/`SiteFields` (from `doctype.Lookup`) that are outside a `Restrict` list → `auth.ErrForbidden`.
    - `CheckLink` → `auth.ErrNotFound` for a hidden `(target, id)`.
    - `HasRole` uses the `AddRole` roles minus those removed with `RemoveRole`. `UsersWithRight` returns the `SetUsers` list, or otherwise every user holding a granted role (a `"*"` grant counts for every DocType), sorted; either way it skips users added with `AddUser` and `Enabled: false` or disabled with `DisableUser` (CD-86).
    - `Scope` with the right allowed and no `SetScope` returns `Predicate{SQL: ""}`.
    - `User` returns users added by `AddUser`. `DisableUser`/`EnableUser` flip `Enabled` and return `auth.ErrNotFound` for unknown users.
  - `authfake.NewDirectory() *Directory` implements `auth.Directory` (CD-13). It is the one shared Directory fake: AUTH and APR use it and keep no fake of their own (CD-56). `Link(u auth.UserID, employeeID int64)`, `Unlink(employeeID int64)` (APR-048), `SetManager(employeeID, managerID int64)`, `SetActive(employeeID int64, active bool)` (default active), `SetProjectCode(id int64, code string)`, `SetSiteCode(id int64, code string)`. `ManagerChain` lists the direct manager first. `DirectReports` returns active employees only, sorted by id, and `UserForEmployee` returns `(0, false, nil)` for an employee who has left (CD-54). The `tx` argument may be nil.
  - `namingfake.New() *Fake` implements `naming.Service` and `naming.Admin`. `Assign` returns `"TEST/00001"`, `"TEST/00002"`, … per doctype; `AssignAmendment` returns `root + "-" + n`; `Preview` returns the next number without consuming it; `ValidateTemplate` returns nil unless `RejectTemplate(tpl string, err error)` was called. `PreviewTemplate` returns that same error for a rejected `tpl`, otherwise the value set with `SetPreview(tpl, out string)`, otherwise `tpl` unchanged (CD-57); `EnsureSeries`/`SetTemplate`/`SetCounter` store state that `List` returns; `Calls() []Call` records method, doctype, doc id and result in order.
  - `auditfake.New() *auditfake.Recorder` (CD-86; the exported type is `Recorder`, not `Fake`) implements `audit.Writer`. `Entries() []audit.Entry` (in order), `For(doctype string, id int64) []audit.Entry`, `FailNext(err error)`.
  - `approvalfake.New() *Fake` implements `approval.Service` and checks the CD-23 call protocol per (doctype, id). `SetRequirement(doctype string, r approval.Requirement)` (default `Needed: false`); `Evaluate` returns it with `Amount` and `Requester` filled from its arguments; `AllowApprover(u auth.UserID)` (otherwise `CheckApprover` wraps `auth.ErrForbidden`); `AddInboxItem(u auth.UserID, it approval.InboxItem)` (paged by `Inbox`); `Calls() []Call{Method string; DocType string; ID int64; Action doctype.Action; By auth.UserID; Reason string}` in order. Protocol errors wrap the WP0 addition `approvalfake.ErrProtocol`: `Open` while a request is already open; `Record` with `approve`, `reject` or `withdraw` when no request is open (each closes it); `Record("revise")` unless the last closing action was `reject`; `Record` with any other action.
  - `filesfake.New(c clock.Clock) *Fake` implements `files.Service`. `Spool(name string, b []byte) *files.Spooled` returns a fresh opaque pointer the fake maps to its bytes (no `files` internals needed). `Attach` stores a `Meta` with `Size`, `SHA256`, `UploadedBy = p.ID`, `UploadedAt = c.Now()`; `List` returns a parent's `Meta` in upload order; `Open` returns the bytes; `CopyAll` copies with `UploadedBy = to.CreatedBy` and `UploadedAt = c.Now()` (CD-29); `Remove`/`DeleteAll` drop entries.
  - `docfake.New() *Fake` implements `doc.Engine`, including `doctype.InfoSource`. `SetInfo(info doctype.Info)`, `SetState(doctype string, id int64, s doctype.State)` (updates the stored `Info`), `AddReference(table string, id int64, ref doc.Reference)`, `SetReferenced(table string, id int64, firstRef string)` (adds a reference whose `Display` is `firstRef`), `SetResult(doctype string, id int64, a doctype.Action, r doc.Result, err error)` (CD-86: exactly these five parameters in this order), `SetRecord(r doc.Record)` (keyed by `r.Info.DocType` and `r.Info.ID`; also stores `r.Info` as with `SetInfo`). `Info` returns an error wrapping `doctype.ErrNotFound` when unset (CD-54). `Load` returns a copy of the stored `Record` (with `Info` taken from the latest `SetInfo`/`SetState`), or an error wrapping `doctype.ErrNotFound` (CD-43). `References` returns at most `limit` in insertion order; `IsReferenced` returns the first one's `Display`; `Do` records the call and returns the configured result (including `Warnings`, CD-68), otherwise `errs.ErrNotImplemented`.

- [ ] **Step 1: Write the failing tests** in `internal/contracts_test.go`

- `TestContracts_FakesSatisfyInterfaces`: compile-time assertions `auth.Service = authfake.New()`, `auth.Directory = authfake.NewDirectory()`, `naming.Service` and `naming.Admin = namingfake.New()`, `audit.Writer = auditfake.New()` and `var _ *auditfake.Recorder = auditfake.New()` (CD-86), `approval.Service = approvalfake.New()`, `files.Service = filesfake.New(clock.NewFixed(...))`, `doc.Engine` and `doctype.InfoSource = docfake.New()`, `db.DBTX = (*sql.Tx)(nil)`; and alias identity `reflect.TypeOf(auth.UserID(0)) == reflect.TypeOf(types.UserID(0))`, same for `list.Page`/`types.Page`, `doc.DocType`/`doctype.DocType` and `doc.Field`/`doctype.Field`.
- `TestContracts_FieldListsMatchSpec`: keyed literals compile for `doctype.Field{ListPriority: 1, MaxLen: 140, Options: []string{"Cash", "Bank"}, Hint: "h", Step: "lines"}` (CD-42), `doctype.Info{Party: "Shree Cement Traders"}` (CD-55), `audit.Ref{DocType: "fixture.Order", DocID: 1, Number: "PO/26-27/00001", Action: "approve"}` (CD-69), `doc.Result{Warnings: []string{"w"}}` (CD-68), `doc.Record{Info: doctype.Info{ID: 1}, Version: 2, Values: map[string]string{"qty": "1200.000"}, Lines: []map[string]string{{"rate": "62.50"}}}` (CD-43) and `print.LetterheadInfo{LegalName: "L", LogoURL: "u", BranchName: "B", Address: []string{"a"}, StateName: "Maharashtra", StateCode: "27", GSTIN: "27AAFCV2449G1Z7"}` (CD-58); `doctype.ActionApprove == "approve"` and `doctype.StatePending == "pending_approval"`.
- `TestContracts_ErrorsImplementHTTPError` (CD-44, CD-54): `auth.ErrNotFound == doctype.ErrNotFound`; `errors.As(fmt.Errorf("x: %w", doctype.ErrNotFound), &he)` with `var he doctype.HTTPError` succeeds and `he.Status() == 404`; the same for `auth.ErrForbidden` gives 403; `FieldErrors()` is nil for both.
- `TestContracts_StubsReturnNotImplemented`: `gstin.ValidateGSTIN("27AAFCV2449G1Z7")`, `gstcalc.LineAmount(1_200_000, 625_000)`, `naming.ValidateGSTDocNumber("PO/26-27/00001")` and `audit.Append(ctx, nil, audit.Entry{})` each return an error with `errors.Is(err, errs.ErrNotImplemented)`.
- `TestContracts_AuthFakeDeniesByDefault`: `Can(ctx, auth.Principal{ID: 4}, "fixture.Order", "read", nil)` → `errors.Is(err, auth.ErrForbidden)` and text "You don't have permission to read fixture.Order. Ask your administrator for a role that allows it."; after `Allow(4, "fixture.Order", "read")` → nil, and `Scope` returns `Predicate{SQL: ""}`.
- `TestContracts_AuthFakeSeedingHelpers` (CD-56): `Grant("Purchase User", "fixture.Order", "read", auth.ScopeOwn)` and `AddRole(4, "Purchase User")`. Then `Can` for Priya (`ID: 4`) on `RecordRef{CreatedBy: 4}` → nil, and on `RecordRef{CreatedBy: 3}` → `auth.ErrNotFound`. `HasRole(4, "Purchase User")` is true. After `Restrict(4, []int64{10}, nil)`, `Can` on `RecordRef{CreatedBy: 4, ProjectIDs: []int64{11}}` → `auth.ErrNotFound`. After `Hide("fixture.Order", 9)`, `CheckLink(ctx, p, "fixture.Order", 9)` → `auth.ErrNotFound`.
- `TestContracts_AuthFakeRound4Helpers` (CD-86): `RemoveRole(4, "Purchase User")` after `AddRole` → `HasRole(ctx, 4, "Purchase User")` = (false, nil) and `Can` for `read` on `fixture.Order` → `auth.ErrForbidden`; `RemoveRole` of a role never added → no panic. `Grant("Administrator", "*", "delete", auth.ScopeAll)` and `AddRole(1, "Administrator")` → `Can(ctx, auth.Principal{ID: 1}, "fixture.Receipt", "delete", nil)` → nil. `AddUser(auth.UserInfo{ID: 8, Enabled: false})` plus `AddUser(auth.UserInfo{ID: 4, Enabled: true})`, both holding `Purchase User` with `read` on `fixture.Order` → `UsersWithRight(ctx, "fixture.Order", "read", nil)` = `[4]`; after `DisableUser(4)` it is `[]`. `docfake.New().SetResult("fixture.Order", 1, doctype.ActionSubmit, doc.Result{ID: 1, State: doctype.StatePending}, nil)` compiles and `Do` for that action returns that result.
- `TestContracts_AuthDirectoryFake`: employees 3→2, 4→3, 7→4, with user 4 linked to employee 4 and user 7 to employee 7. `EmployeeForUser(4)` = (4, true); `ManagerChain(7)` = `[4, 3, 2]`; `DirectReports(3)` = `[4]`. After `SetActive(4, false)`: `DirectReports(3)` = `[]` and `UserForEmployee(4)` = (0, false) (CD-54).
- `TestContracts_NamingLabel` (CD-53, real implementation): `naming.Label("", 42)` = `"Draft 42"`; `naming.Label("PO/26-27/00001", 42)` = `"PO/26-27/00001"`.
- `TestContracts_PrincipalContextRoundTrip`: `PrincipalFrom(WithPrincipal(ctx, p))` = (p, true); `PrincipalFrom(context.Background())` = (zero, false).
- `TestContracts_AuditFakeRecords`: two appends → `Entries()` in order with `Changes[0].Op == "set"` preserved; `FailNext(e)` makes the next append return `e` and record nothing.
- `TestContracts_ApprovalFakeProtocol`: (a) `Evaluate` with amount ₹94,164.00 and requester Priya returns `Amount == 9416400`, `Requester == 4`; (b) `Open` → `Record(ActionApprove)` → nil (the not-needed submit path, no `CheckApprover` in `Calls()`); (c) `Record(ActionApprove)` with nothing open → `ErrProtocol`; (d) `Open` twice → `ErrProtocol`; (e) `Open` → `Record(ActionReject, "Wrong rate")` → `Record(ActionRevise)` → nil, then `Record(ActionRevise)` again → `ErrProtocol`; (f) `Open` → `Record(ActionWithdraw)` → nil; (g) `CheckApprover` for a user not allowed → `auth.ErrForbidden`.
- `TestContracts_DocFakeInfoAndReferences`: `SetInfo` with `Display: "PO/26-27/00001"` → `Info` returns it; unknown id → `errors.Is(err, doctype.ErrNotFound)`; three references with `limit` 2 → two; `IsReferenced` → ("PO/26-27/00001", true).
- `TestContracts_DocFakeLoad` (CD-43): `SetRecord(doc.Record{Info: doctype.Info{DocType: "fixture.Order", ID: 1}, Version: 3, Values: map[string]string{"qty": "1200.000", "date": "2026-10-24"}})` → `Load(ctx, nil, "fixture.Order", 1)` returns it; changing the returned map does not change the next `Load`; after `SetState(…, doctype.StateApproved)`, `Load(…).Info.State` is `approved`; an unknown id → `errors.Is(err, doctype.ErrNotFound)`.
- `TestContracts_FilesFakeSpoolAttachList`: `Spool("receipt.jpg", 2.3 MB of bytes)` → `Attach` as Priya → `List` has one `Meta` with that `Size`, `UploadedBy == 4`, `UploadedAt` = the fixed clock; `Open` returns the same bytes; `CopyAll` to a record with `CreatedBy: 3` → the copy has `UploadedBy == 3`.
- `TestContracts_NamingFakeSequence`: `Assign` twice for `fixture.Order` → `TEST/00001`, `TEST/00002`; first `Assign` for `fixture.Receipt` → `TEST/00001`; `Preview` doesn't advance; `AssignAmendment(…, "PO/26-27/00001", 1)` → `PO/26-27/00001-1`; after `EnsureSeries("fixture.Claim", "Expense Claim", "EXP/{FY}/{#####}")`, `List` contains that template; `PreviewTemplate` returns the `SetPreview` value, and the `RejectTemplate` error for a rejected template (CD-57).
- `TestContracts_DoctypeRegistry`: `Register` two DocTypes out of order → `All()` sorted by `Name`; `Lookup` finds one; registering a duplicate panics; `doc.Lookup` returns the same pointer as `doctype.Lookup`. Uses names prefixed `wp0test.` so it never collides with DOC's fixtures.
- `TestContracts_KernelImportsFollowTable` (CD-85): the rule lives in a test helper `importAllowed(pkg, imp string) bool` (both module-relative paths, e.g. `internal/kernel/list`, `internal/kernel/naming`), and the test has two parts.
  - **Real tree:** using `go/build` (`build.Import`, non-test files, imports under the module path only), it walks every directory under `internal/kernel/` and checks each import with `importAllowed`. A kernel package directory itself (`internal/kernel/<pkg>`) may import nothing beyond `internal/types`, `internal/platform/...`, `internal/kernel/doctype` and its row in the §4.2 table (`audit` —; `auth` audit; `naming` auth, audit; `approval` auth, audit; `files` auth, audit; `print` auth, files; `gstcalc` —; `list` auth, naming (CD-59); `csvio` auth, list, audit; `doc` auth, naming, approval, audit, files); `doctype` imports only `types` and `platform/db`; `types` imports no `internal/` package. Subpackages follow these rules instead of the table: `internal/kernel/<pkg>/<pkg>fake` and `internal/kernel/<pkg>/<pkg>test` may import everything their parent may, plus the parent itself, `internal/testkit` and any `internal/kernel/*/*fake` package; `internal/kernel/<pkg>/web` and its subpackages may import everything their parent may, plus the parent, `internal/ui`, `internal/platform/web` and the parent's own `<pkg>fake`/`<pkg>test` helpers. A failure names the package, the import and the rule it breaks.
  - **Table cases** (subtests calling `importAllowed` directly, so they pass before any area writes a `web` or `test` package): `approval/web` importing `internal/ui` → allowed; `approval/web/views` importing `internal/ui` → allowed; `auth/authtest` importing `internal/testkit` → allowed; `naming/namingfake` importing `internal/kernel/auth/authfake` → allowed; `audit` importing `internal/kernel/auth` → rejected; `list` importing `internal/kernel/doc` → rejected; `doctype` importing `internal/kernel/auth` → rejected; `auth` (the kernel package itself) importing `internal/ui` → rejected; `approval/web` importing `internal/kernel/files/filesfake` → rejected (another area's helper).

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/`
Expected: FAIL, packages missing.

- [ ] **Step 3: Write the contract files** copying spec §5 signatures exactly (check each one against §5 line by line), then the fakes above.

- [ ] **Step 4: Run them to see them pass**

Run: `go vet ./... && go test -race ./internal/...`
Expected: PASS.

- [ ] **Step 5: Commit**

`git add internal && git commit -m "feat(contracts): add kernel contract stubs and fakes from spec §5 (Rounds 1-4)"`

### Task 6: Test kit and canonical seed

**Files:**
- Create: `internal/testkit/testkit.go`, `internal/testkit/fixtures.go`, `internal/testkit/seed.go`, `internal/testkit/directory.go`, `internal/testkit/migrations/09001_testkit_minimal_masters.sql`, `internal/testkit/testkit_test.go`

**Interfaces:**
- Consumes: `db.Open`, `db.Migrate`, `migrations.FS`, `clock.NewFixed`, `authfake.NewDirectory`.
- Produces:
  - `testkit.NewDB(t testing.TB) *db.DB` — temp dir, `app.db`, `db.Open(path)` (default 5 s busy timeout), applies `migrations.FS` (table `goose_db_version`) then the test-only `internal/testkit/migrations` (table `goose_testkit`), registers cleanup.
  - `testkit.DBPath(t testing.TB) string` — the file path of the database `NewDB` created for this `t` (CD-08), so a test can open a second connection or reopen with `db.WithBusyTimeout(200*time.Millisecond)` (DOC-075). Calls `t.Fatalf` if `NewDB(t)` was not called first.
  - `testkit.DataDir(t testing.TB) string` — temp directory for files.
  - `testkit.Clock() *clock.Fixed` — 2026-10-24T05:00:00Z.
  - `testkit.Fixtures` struct with every value from `docs/tests/core/00-fixtures.md`: `Users []FixtureUser{ID types.UserID; Email, Name string; Roles []string; EmployeeCode string; Disabled bool}` with IDs 1–8 in table order, `Employees []FixtureEmployee{ID int64; Code, ManagerCode, Status string; UserID types.UserID}`, `Branches`, `Parties`, `Items`, `Projects`, `Sites`, `ExpenseTypes`, `Rights []FixtureRight{Role, DocType string; Rights []string; Scope string}` (scopes `own`, `team`, `all`, CD-11, including Employee → Expense Claim and `fixture.Claim`: `read`, `approve` with scope `team`), `ApprovalRules` (including Expense Claim / `fixture.Claim` from ₹0.00, reporting manager, fallback Director), `Numbering` (including `fixture.Claim` → `EXP/{FY}/{#####}`), `ReferencePO`.
  - `testkit.Seed(t testing.TB, d *db.DB) Fixtures` — inserts the Wave 1 minimal rows into `testkit_employee(id INTEGER, code TEXT, user_id INTEGER, manager_id INTEGER, status TEXT)`, `testkit_project(id INTEGER, code TEXT, name TEXT, enabled INTEGER)`, `testkit_site(id INTEGER, code TEXT, name TEXT, branch_code TEXT, project_id INTEGER, enabled INTEGER)` (only `INTEGER`/`TEXT`, CD-34) and returns `Fixtures` with database IDs filled in. No users, rights or sessions tables (AUTH owns them). WP2 replaces these tables with real masters.
  - `testkit.Directory(fx Fixtures) *authfake.Directory` — WP0 addition: links users to employees, sets managers, marks EMP-008 (status Left) inactive, and sets project and site codes, all from `fx`, so AUTH and APR Wave 1 tests share one directory.
  - `testkit.Principal(t testing.TB, email string) auth.Principal` (CD-72 signature) — from `Fixtures.Users` (no database read in Wave 1).

- [ ] **Step 1: Write the failing tests**

`TestTestkit_NewDBMigrated` (both version tables exist), `TestTestkit_NewDBCleansUp` (after the subtest ends, the temp dir no longer exists), `TestTestkit_DBPathMatchesNewDB` (a raw `sql.Open("sqlite", DBPath(t))` sees `app_meta`; `DBPath` on a `t` without `NewDB` calls `Fatalf`, checked with a fake `testing.TB`), `TestTestkit_SeedMatchesFixturesDoc` (Ravi's employee `EMP-003` has manager `EMP-002`; Deepak's `EMP-007` has manager `EMP-004`; `PRJ-METRO` exists; 8 users; Priya's roles are `[Purchase User, Employee]`; the Employee → `fixture.Claim` rights row is `read, approve` with scope `team`; every rights scope is one of `own`, `team`, `all`), `TestTestkit_DirectoryFromFixtures` (with `fx := Seed(...)`: `DirectReports(Priya's employee)` = `[Deepak's employee]`; `ManagerChain(Deepak's employee)` = `[EMP-004, EMP-003, EMP-002]` ids; `UserForEmployee(EMP-004)` = Priya's id), `TestTestkit_StandInColumnTypes` (`PRAGMA table_info` of the three `testkit_*` tables shows only `INTEGER` and `TEXT`), `TestTestkit_ColumnTypesRuleSkipsGoose` (CD-34, CD-71: every table in `sqlite_schema` declares only `INTEGER`, `TEXT` or `BLOB`, except names matching `sqlite_%` or `goose_%`; the skip list covers both `goose_db_version` and `goose_testkit`, and the test fails if a non-goose table declares `TIMESTAMP`), `TestTestkit_PrincipalUnknownEmailFails` (unknown email calls `t.Fatalf`, checked with a fake `testing.TB`), `TestTestkit_ClockIsIST1030` (`Clock().Now().In(clock.IST).Format("02-01-2006 15:04")` = `24-10-2026 10:30`, the `FormatIST` format, CD-33).

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./internal/testkit/`
Expected: FAIL.

- [ ] **Step 3: Implement** the test kit; copy values from `00-fixtures.md` verbatim into `fixtures.go` with a comment naming that file as the source. `DBPath` keeps a mutex-guarded map from `t` to path, removed in the cleanup.

- [ ] **Step 4: Run them to see them pass**

Run: `go test -race ./internal/testkit/`
Expected: PASS.

- [ ] **Step 5: Commit**

`git add internal/testkit && git commit -m "feat(testkit): add temp database, path, fixed clock, canonical fixtures and directory fake"`

### Task 7: Test-ID checker

**Files:**
- Create: `scripts/checktestids/main.go`, `scripts/checktestids/main_test.go`, `scripts/checktestids/testdata/`

**Interfaces:**
- Produces: `go run ./scripts/checktestids [--report] [--all] [--exclude-level <level>]... [AREA...]` (CD-41). Parses `docs/tests/core/0*.md` for blocks starting `### <AREA>-<NNN>` and reads their `**Priority:**`, `**Level:**` and `**Go test:**` lines; scans `*_test.go` files (excluding `reference/`, including `e2e/` whatever its build tags) with `go/parser` for `func Test…`/`func Fuzz…` declarations. `--exclude-level e2e` skips cases whose level's first word is `e2e` (so `unit (fuzz)` counts as `unit`); the flag may repeat. Prints `MISSING <ID> <GoTest> <P>` for each P1 (and P2 with `--all`) without a function. Exit 1 when any P1 is missing unless `--report`. Prints a summary line such as `P1 implemented: 12/535 (excluded 15 e2e)`. A case without a `**Level:**` line is an error naming the ID. A `**Go test:**` value may name a subtest as `TestFunc/sub_name` (e.g. NAM-009 `TestTemplate_Validate/missing_number`); it is matched on the part before the first `/`, so several cases may share one function, but only through subtest names (CD-74). Two cases naming the same bare function, with no `/`, are reported as an error. A case whose level's first word is `e2e` but whose Go test does not start with `TestE2E_` is reported as an error naming the ID (CD-74). These name errors print as `ERROR <ID> <reason>` lines and, like MISSING lines, cause exit 1 unless `--report` is given.

- [ ] **Step 1: Write the failing tests**

`TestCheckTestIDs_FindsMissing` (testdata markdown with `NUM-001` P1 unit `TestMoney_Add` and no test file → one MISSING line, exit 1), `TestCheckTestIDs_PassesWhenPresent`, `TestCheckTestIDs_IgnoresComments` (a `// TestMoney_Add` comment and a string literal don't count), `TestCheckTestIDs_PrefixFilter` (`NAM` only reports NAM cases), `TestCheckTestIDs_ReportModeExitsZero`, `TestCheckTestIDs_ExcludeLevel` (testdata with `DOC-090` P1 e2e `TestE2E_FixtureLifecycle` and `DOC-001` P1 unit (fuzz) `FuzzDoc_X`, neither implemented: with `--exclude-level e2e` only `DOC-001` is reported and the summary says `excluded 1 e2e`; without the flag both are reported), `TestCheckTestIDs_MissingLevelIsError`, `TestCheckTestIDs_SubtestNameMatchesFunc` (testdata: `NAM-009` P1 unit `TestTemplate_Validate/missing_number` and `NAM-010` P1 unit `TestTemplate_Validate/bad_token`, plus a test file declaring only `func TestTemplate_Validate(`; neither is reported missing; with no such function, both are reported missing; two cases naming the bare `TestX` → error), `TestCheckTestIDs_E2ENamePrefix` (`DOC-091` P1 e2e `TestDoc_Lifecycle` → error naming `DOC-091`; `TestE2E_FixtureLifecycle` → no error).

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./scripts/checktestids/`
Expected: FAIL.

- [ ] **Step 3: Implement** with the standard library only (`go/parser`, `go/ast`, `bufio`, `regexp`, `flag`).

- [ ] **Step 4: Run them to see them pass, then run it for real**

Run: `go test ./scripts/checktestids/ && go run ./scripts/checktestids --report && go run ./scripts/checktestids DOC --exclude-level e2e --report`
Expected: tests PASS. The first report prints `P1 implemented: 0/556` (the P1 total in `docs/tests/core/README.md`) and **no** `ERROR` lines: APR-097 (`TestE2E_ApprovalWithdraw`), APR-079 (`TestE2E_ApprovalInboxRowContents`) and FILE-056 (`TestE2E_PrintA4`) are fixed in the test docs (CD-74, CD-77, CD-92). If any `ERROR` line appears, pass it to the lead and don't edit the test docs. The second report covers only non-e2e DOC cases. No crash.

- [ ] **Step 5: Commit**

`git add scripts && git commit -m "feat(scripts): add test-ID coverage checker with level exclusion"`

### Task 7b: Brand config (`internal/platform/brand`, CD-99)

Runs after Task 7 and before Task 8, so Task 8's gate and tag cover it. Standard tier.

**Files:**
- Create: `internal/platform/brand/{brand.go,colour.go,static.go,brand_test.go,colour_test.go}`, `internal/platform/brand/brand.json`, `internal/platform/brand/assets/{logo.svg,logo-print.png,favicon.png}`, `internal/platform/brand/gen/main.go`, `internal/platform/brand/brand_gen.go` (generated), `internal/platform/brand/README.md`

**Interfaces:**
- Produces (`package brand`, standard library only, imports no internal package, so every package may import it under spec §4.2 rule 3):
  - `type Config struct{ AppName string `json:"app_name"`; Primary string `json:"primary"`; Primary700 string `json:"primary_700"`; Primary800 string `json:"primary_800"`; Primary50 string `json:"primary_50"`; DocumentAccent string `json:"document_accent"`; AppLogo string `json:"app_logo"`; DocumentLogo string `json:"document_logo"`; Favicon string `json:"favicon"` }`
  - `func Parse(b []byte, files fs.FS) (Config, error)`: unknown keys are an error; colours are `#RRGGBB` (upper-cased on output); missing `primary_700`, `primary_800`, `primary_50` are derived (`Mix(primary, "#000000", 0.15)`, `Mix(primary, "#000000", 0.30)`, `Mix(primary, "#FFFFFF", 0.93)`); missing `document_accent` = `primary`; image files must exist in `files` with the extensions and size limits of design-system §5.1; then every contrast pair of design-system §5.1 is checked against the fixed token neutrals (`#FFFFFF` surface and text-on-fill, `#F5F6F8` canvas, `#EDEFF2` sunken).
  - `func Load() Config`: parses the embedded `brand.json` and `assets/`; panics on error (the tests and `go generate` fail first, so a bad config never builds).
  - `func Contrast(a, b string) (float64, error)`: WCAG 2.x ratio (the formula in WP1-UI Task 4). `func Mix(a, b string, t float64) (string, error)`: interpolation in OKLab, `t=0` gives `a`, `t=1` gives `b`.
  - `func CSS(c Config) string`: one unlayered `:root{…}` block setting `--color-primary-600`, `--color-primary-700`, `--color-primary-800`, `--color-primary-50`, `--color-focus`, daisyUI's `--color-primary` and `--color-accent`, and `--color-doc-accent`. `func PrintCSS(c Config) string`: `:root{--doc-accent:#RRGGBB}`.
  - `brand_gen.go` (from `//go:generate go run ./gen`): constants `AppLogoURL`, `DocumentLogoURL`, `FaviconURL` = `"/static/brand/<name>.<sha256[:8]>.<ext>"`. `func Static() http.Handler` serves those names with `Cache-Control: public, max-age=31536000, immutable`; unknown name → 404. WP3 mounts it at `/static/brand/`.
- Default `brand.json`: `{"app_name":"Paperweight","primary":"#1F4FD1","primary_700":"#1A3FA8","primary_800":"#16348A","primary_50":"#EEF3FF","document_accent":"#1F4FD1","app_logo":"logo.svg","document_logo":"logo-print.png","favicon":"favicon.png"}` (the token values, so the default look is unchanged). Default images are placeholders (a wordmark SVG and plain PNGs drawn with `image/png`) until the owner supplies real logos.

- [ ] **Step 1: Write the failing tests.**
  - `TestContrast_KnownPairs`: `#141A23` on `#FFFFFF` rounds to 17.47; `#FFFFFF` on `#1F4FD1` is at least 4.5; an invalid colour returns an error.
  - `TestMix_Endpoints`: `Mix(a,b,0) == a`, `Mix(a,b,1) == b` for three pairs; results are `#RRGGBB`.
  - `TestParse_Defaults`: the default `brand.json` parses; `CSS` contains `--color-primary-600:#1F4FD1` and `--color-doc-accent:#1F4FD1`.
  - `TestParse_DerivesShades`: `{"primary":"#0F6E56", …}` without shades gives a 700 and 800 darker than the primary and a 50 lighter than it, and all §5.1 pairs pass.
  - `TestParse_TooLightPrimary`: `"primary":"#8AB4F8"` fails; the error starts with `brand.json: "primary" #8AB4F8 is too light:` and contains `needs at least 4.5:1`.
  - `TestParse_RejectsUnknownKeyAndMissingFile`: an unknown key and a missing logo file each fail with an error naming the key or file.
  - Logo sizes (owner, 25 Oct 2026): `Parse` reads each image's intrinsic size (PNG/JPEG via `image.DecodeConfig`; SVG from the root `viewBox`) and rejects a missing `viewBox`, a logo outside 1:2 to 8:1, and a favicon that is not square or is under 32 × 32, each with an error naming the key, file and size. The generator also emits `AppLogoWidth`, `AppLogoHeight`, `DocumentLogoWidth`, `DocumentLogoHeight`. Tests: `TestParse_LogoSizes`, `TestParse_RejectsTallLogo`, `TestParse_RejectsSVGWithoutViewBox`, `TestParse_FaviconMustBeSquare`.
  - `TestBrand_EmbeddedIsValid`: `Load()` does not panic. `TestBrand_GenUpToDate`: running the generator in memory equals the committed `brand_gen.go`. `TestStatic_ServesLogo`: `GET` of the base name of `AppLogoURL` → 200 with the immutable header; unknown → 404.
- [ ] **Step 2: Run them to see them fail.** `go test ./internal/platform/brand/...` → FAIL.
- [ ] **Step 3: Implement** the package, the generator and the default assets. `README.md` explains how to rebrand: replace the files, edit `brand.json`, run `make generate && make build`, deploy.
- [ ] **Step 4: Run them to see them pass.** Same command → PASS; `make lint` clean.
- [ ] **Step 5: Commit.** `git add internal/platform/brand && git commit -m "feat(platform): brand config with build-time contrast checks"`

### Task 8: Command skeleton and CI gate

**Files:**
- Create: `cmd/app/main.go`, `cmd/app/main_test.go`, `e2e/doc.go` (`//go:build e2e`, `package e2e`, comment: every `TestE2E_*` lives here and is owned by WP3, CD-40)

**Interfaces:**
- Produces: `app serve --data <dir> --addr :8080`, `app migrate --data <dir>`, `app version`. `serve` calls `db.Open(<dir>/app.db)`, migrates, builds `web.NewRouter(web.Options{Logger: …})` with `Sessions` and `Auth` nil, serves `/healthz` → `200 ok`. Sub-commands parsed with the standard `flag` package. WP3 adds `admin create` and `user disable` (from `auth/cli`, CD-18), `files gc`, and passes AUTH's session manager and middleware into `web.Options`.

- [ ] **Step 1: Write the failing tests**

`TestApp_MigrateCreatesDatabase` (run `run([]string{"migrate","--data",dir})` → `app.db` exists with `app_meta`), `TestApp_HealthzOK` (start `serve` on `127.0.0.1:0` in a goroutine with a context; GET `/healthz` → 200 body `ok`; cancel stops it within 2 s), `TestApp_UnknownCommand` (returns error "unknown command \"frobnicate\". Run `app help` to see the commands.").

- [ ] **Step 2: Run them to see them fail**

Run: `go test ./cmd/app/`
Expected: FAIL.

- [ ] **Step 3: Implement** `func run(args []string) error` called by `main`, with graceful shutdown on context cancel or SIGINT/SIGTERM.

- [ ] **Step 4: Run the whole gate**

Run: `make ci && make e2e`
Expected: generate leaves no diff, lint clean, all tests PASS, `bin/app` built, test-ID report prints; `make e2e` exits 0 with no tests yet.

- [ ] **Step 5: Commit and tag**

```bash
git add -A && git commit -m "feat(app): add serve, migrate and version commands"
git tag wp0-foundation
```

---

## Exit criteria

WP0 owns no test-case IDs. It is done when `make ci` is green, `make e2e` runs, every §5 name compiles with the exact §5 signature, and every fake listed in Task 5 passes its `TestContracts_*` test.

## Hand-off to Wave 1

After `wp0-foundation` is tagged, the Wave 1 plans run in parallel, each in its own worktree (`git worktree add ../it-<wp> -b wp/<wp> wp0-foundation`): `wp1-num`, `wp1-auth`, `wp1-naming`, `wp1-doc`, `wp1-approval`, `wp1-files-print`, `wp1-ui`. Each is done (spec §9 rule 3, CD-40) when every P1 case that does not need another WP's real implementation passes, lint is clean, and `go run ./scripts/checktestids <AREA> --exclude-level e2e` (`make checkids AREA=<AREA>`) reports nothing missing. Cases a plan marks "after DOC/NAM merge" or "Wave 3", and every `TestE2E_*` case (under `e2e/`, owned by WP3), are WP3's entry and exit criteria.
