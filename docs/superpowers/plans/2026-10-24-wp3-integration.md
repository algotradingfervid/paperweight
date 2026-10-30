# WP3 Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Run it under `docs/superpowers/plans/2026-10-25-execution-protocol.md` (roles, risk tier per task, review cadence; CD-96).

**Goal:** Join the Wave 1 kernel packages and Wave 2 masters into one working binary, prove fakes and real services behave the same, and pass the whole-core verification in spec §10.

**Architecture:** A single composition root (`internal/app`) builds every real service and hands them to HTTP handlers; nothing else constructs services. Each kernel interface gets a shared contract test suite that runs against both its fake and its real implementation, so Wave 1 tests written against fakes stay trustworthy. Browser tests (chromedp) drive the fixture lifecycle and check every screen for horizontal overflow and accessibility.

**Tech Stack:** as spec §2; plus `chromedp/chromedp` v0.21.0 and axe-core (MPL-2.0, test-only, injected by chromedp, latest version at execution time), Litestream v0.5.17 for the restore drill.

**Spec:** `docs/specs/2026-10-24-core-platform-spec.md` (§3.2, §5, §7, §9, §10), `docs/specs/2026-10-24-design-system.md` and `docs/specs/contract-changes.md` (Rounds 1–4; Round 4 items below).

## Contract decisions applied (Round 4)

- CD-99: WP3 mounts `brand.Static()` (`internal/platform/brand`) at `/static/brand/` beside UI's `/static/ui/`; the app shell, print pages and letterhead already read the brand config, so no other wiring is needed. Rebranding is a change to `internal/platform/brand/` plus a rebuild.
- CD-91: WP3 also switches `testkit` user seeding to AUTH's real tables (CD-67), writes UI's `Lister`/`Extras` adapters over DOC, APR, FILE and `list` (CD-78), moves Wave 1 area templates onto UI components (CD-49), wires `auth/cli` `user disable` into `cmd/app` (CD-18), asserts CD-17 for signed-out requests and asserts DOC-085's route clause (no PUT, PATCH or DELETE route). Tasks 2 and 3.
- CD-87: `/ui/validate/*` is mounted with AUTH's `Self` guard; `CheckedInHandler` is used only for `/doc/`. Task 2.
- CD-92: the withdraw e2e case is `TestE2E_ApprovalWithdraw` (APR-097); the P1 total is the current one in `docs/tests/core/README.md` (556 on 24-10-2026). Tasks 3 and 4.

## Global Constraints

- Starts only after every Wave 1 WP and WP2-MST are merged to `main` with `make ci` green.
- `internal/app` is the only package that imports more than one kernel implementation package.
- Fixture DocTypes (`internal/fixture`) are registered only in test builds and in the `e2e` build tag, never in the release binary.
- No new behaviour: if integration reveals a gap, record it in `docs/specs/contract-changes.md` and fix it in the owning package with its own test.
- WP3 owns `internal/app/` and every `TestE2E_*` under `e2e/`, plus the P1 cases each Wave 1/2 plan marks "after DOC/NAM merge" or "Wave 3" (CD-40). Its exit is `go run ./scripts/checktestids` with no exclusions reporting nothing missing.
- Viewport widths for overflow checks: 320, 360, 390, 768, 1024, 1440 px.

## Review Focus

1. **A fake that is more forgiving than the real service** (e.g. authfake allowing a right the real matrix denies). The contract suites in Task 1 run the same assertions on both. Pinned by `TestContract_*` for each interface.
2. **Startup with an empty database.** `app serve` on a fresh data dir shows `/setup`, and every other page redirects there until the first admin exists. Pinned by `TestApp_FreshInstallRedirectsToSetup` (Task 2).
3. **Two people approving the same document from two phones.** First wins, second sees "PO/26-27/00001 is no longer waiting for approval. It was approved by Ravi Kulkarni." Pinned by `TestE2E_ConcurrentApproveSecondSeesMessage` (Task 4).
4. **Long values on a 320 px screen** (GSTINs, emails, 40-character item names, ₹1,23,45,67,890.00). No horizontal scroll. Pinned by `TestE2E_NoHorizontalOverflow` with the long-value seed (Task 5).
5. **Restore from backup after a crash mid-write.** Restored database passes `PRAGMA integrity_check` and contains every committed approval. Pinned by `scripts/restore-drill.sh` (Task 6).

---

### Task 1: Contract test suites (fake vs real)

**Files:**
- Create: `internal/kernel/auth/authtest/contract.go`, `internal/kernel/naming/namingtest/contract.go`, `internal/kernel/approval/approvaltest/contract.go`, `internal/kernel/files/filestest/contract.go`, `internal/kernel/doc/doctest/contract.go`, and a `contract_test.go` in each real package and each fake package calling them.

**Interfaces:**
- Produces: `authtest.Run(t *testing.T, newSvc func(t *testing.T) (auth.Service, Setup))` (same shape for each package), where `Setup` lets the suite grant rights, roles and restrictions through the real admin API or the fake's helpers.

- [ ] **Step 1: Write the suites.** Each suite holds 10–20 behaviours copied from the area's P1 cases that other packages depend on (for auth: deny by default, own scope, strict project restriction, disabled user, 403 vs 404; for naming: first number, FY reset, gapless under rollback; for approval: threshold, self-approval blocked, withdraw; for files: permission follows parent, copy on amend; for doc: illegal transition rejected, version conflict).
- [ ] **Step 2: Run against fakes and real implementations.**
Run: `go test ./internal/kernel/... -run Contract`
Expected: PASS for both. Any fake failure is fixed in the fake; any real failure is a bug in the owning package.
- [ ] **Step 3: Commit.** `git commit -m "test: add fake-vs-real contract suites for kernel services"`

### Task 2: Composition root and commands

**Files:**
- Create: `internal/app/app.go`, `internal/app/routes.go`, `internal/app/uiadapters.go` (CD-78), `internal/app/app_test.go`, `internal/app/uiadapters_test.go`
- Modify: `cmd/app/main.go` (add `admin create`, `user disable` (CD-18), `files gc`; `serve` uses `internal/app`), `cmd/app/main_test.go`

**Interfaces:**
- Consumes: every real kernel service and master package, `web.NewRouter`.
- Wires the cross-package adapters (CD-05, CD-13, CD-28, CD-30): the DOC engine as `doctype.InfoSource` into APR, FILE and PRINT; MST's directory as `auth.Directory`; `company` as `print.Letterhead`; `naming.Admin.EnsureSeries` for every registered DocType with a `Series`; `auth/cli` (`AdminCreate`, `UserDisable`) into `cmd/app` as `app admin create` and `app user disable --data <dir> --email <email>` (CD-18, CD-91); AUTH's session manager and middleware into `platform/web` (CD-14, CD-15).
- Writes UI's consumer adapters (CD-78, CD-91) in `internal/app/uiadapters.go`: the `docscreen` `Lister` (`List(ctx, p, dt, q) (types.Result[ListRow], error)`: `list.Build` + `auth.Scope` inside `db.Read`) and `Extras` (`Approval`: `approval.Evaluate` plus APR history with its preview wording; `Origin`: DOC's `state_reason` and `amended_from_id`; `Audit`: the audit log with `FormatIST` timestamps; `Attachments`: `files.Service.List` with FILE's formatting), with the signatures in the WP1-UI plan. They hold no business logic; every value comes from the owning service. No new §5 contract.
- Route guards (CD-87): `/ui/validate/*` is mounted behind AUTH's `Self` guard; the generic `/doc/{doctype}/…` routes are the only ones behind `CheckedInHandler` (CD-46, CD-83).
- Produces: `app.New(ctx context.Context, cfg Config) (*App, error)` with `Config{DataDir, Addr string; Clock clock.Clock; TrustedOrigins []string}`, `(*App) Handler() http.Handler`, `(*App) Close() error`, background jobs (file GC hourly, expired-session sweep hourly) started by `(*App) Run(ctx)`.

- [ ] **Step 1: Write failing tests:** `TestApp_FreshInstallRedirectsToSetup` (GET `/` on empty data dir → 303 to `/setup`), `TestApp_AdminCreateOnlyOnce` (second `admin create` fails with "Users already exist. Sign in as an administrator to add more users." (AUTH-049, CD-66)), `TestApp_UserDisableCommand` (CD-18, CD-91: after `admin create`, `run([]string{"user","disable","--data",dir,"--email","deepak@vardhan.test"})` → nil; Deepak's sign-in then fails with AUTH's disabled-user response (AUTH-007) and his existing sessions are gone; an unknown email returns AUTH's not-found error and exit is non-zero), `TestApp_AllRoutesRequireSignIn` (CD-17, CD-63, CD-91: with the first admin created (so nothing redirects to `/setup`), `chi.Walk` over the real router; for every route except `/setup`, `/signin`, `/static/*`, `/healthz`, with no session: a plain browser GET → 303 with `Location` `/signin?next=<escaped path>`; the same GET with `HX-Request: true` → 401 "Your session has expired. Sign in again." plus `HX-Redirect`; every non-GET route (sent with a valid same-origin header so CSRF origin checks pass) → 401 with that text; the test fails naming the method and path of any route that answers otherwise), `TestApp_NoPutPatchDeleteRoutes` (DOC-085 route clause, CD-91: `chi.Walk` finds no route registered for `PUT`, `PATCH` or `DELETE`, and, signed in as Administrator with a valid CSRF token, a `PUT`, `PATCH` and `DELETE` to every GET route's path (`{doctype}`/`{id}` filled with `fixture.Order` and an existing id) return 405 or 404, never 2xx, and add no `audit_log` row), `TestApp_RouteGuards` (CD-87: AUTH's route inventory shows every `/ui/validate/*` route with kind `KindSelf` and every `/doc/` route with kind `KindCheckedInHandler`, and no route outside `/doc/` uses `KindCheckedInHandler`), `TestApp_UIAdaptersMatchServices` (in `uiadapters_test.go`, CD-78: with the canonical seed, an approved `fixture.Order` with one attachment and an amended copy, the `Lister` rows for Priya equal the rows `list.Build` returns under her `auth.Scope` (a row she can't read is absent); `Extras.Approval` matches APR's history; `Extras.Audit` has one row per `audit` entry with `FormatIST` times; `Extras.Attachments` matches `files.Service.List`; `Extras.Origin` on the amendment gives the original's id and number), `TestApp_FixtureDocTypesNotInRelease` (`doc.Lookup("fixture.Order")` is false in a non-test build, checked via `go list -deps ./cmd/app` not containing `internal/fixture`).
- [ ] **Step 2: Run to see them fail.** `go test ./internal/app/ ./cmd/app/` → FAIL.
- [ ] **Step 3: Implement** the composition root; routes mounted per package (`auth/web`, `approval/web`, `files/web`, `print/web`, generic DocType list/form handlers from `internal/ui`, master screens from `internal/masters/allmasters`); `/ui/validate/*` behind `Self` and `/doc/` behind `CheckedInHandler` (CD-87); the `Lister`/`Extras` adapters (CD-78); `user disable` in `cmd/app` (CD-18).
- [ ] **Step 4: Run to see them pass.** `go test -race ./internal/app/ ./cmd/app/` → PASS.
- [ ] **Step 5: Commit.** `git commit -m "feat(app): wire real services, UI adapters, admin create, user disable and file GC"`

### Task 3: Switch all area tests to the real seed

**Files:**
- Modify: `internal/testkit/seed.go` and `internal/testkit/testkit_test.go` (users through AUTH's tables, CD-67), `internal/testkit/fixtures.go` (only if a value moves), each WP's `_test.go` files that construct fakes for cross-area behaviour where a real implementation now exists and the test is an integration test, and the Wave 1 area templates under `internal/kernel/*/web/` (CD-49).

- [ ] **Step 1: Write the failing test for real user seeding (CD-67, CD-91).** `TestTestkit_SeedUsesAuthTables`: after `testkit.Seed(t, d)`, AUTH's own read API (the real `auth.Service.User`, and its users/roles queries) returns all 8 fixture users with their `00-fixtures.md` emails, names, roles and enabled flags (the disabled user is `Enabled: false`), the rights rows in `Fixtures.Rights` are in AUTH's matrix tables (including the Administrator `"*"` row and Employee → `fixture.Claim` `read, approve` scope `team`), and `testkit.Principal(t, "priya@vardhan.test")` equals what AUTH loads for that user. Run `go test ./internal/testkit/ -run SeedUsesAuthTables` → FAIL.
- [ ] **Step 2: Switch `Seed` to AUTH's tables.** Insert users, role assignments and rights through AUTH's migrations' tables (or its admin API inside `db.Write`), keeping the IDs 1–8; drop any users stand-in. Run `go test -race ./internal/testkit/` → PASS, including the existing `TestTestkit_SeedMatchesFixturesDoc`.
- [ ] **Step 3: Move Wave 1 area templates onto UI components (CD-49, CD-91).** For each area screen package (`approval/web`, `files/web`, `print/web`, `naming/web`, and any other `internal/kernel/*/web` that still renders its own markup), replace hand-written layout, tables, buttons, form fields and alerts with the `internal/ui` components; behaviour and copy stay the same. Add `TestScreens_UseUIComponents` in `internal/app`: using `go/build`, every `internal/kernel/*/web` package (and subpackage) that has `.templ` files imports `internal/ui` or one of its subpackages, and a scan of those `.templ` files finds no raw `<table`, `<button`, `<input` or `role="alert"` markup (the UI components supply them). Each failure names the file and line. Run `go test ./internal/app/ -run UseUIComponents` → FAIL before, PASS after; `make generate` leaves no diff.
- [ ] **Step 4:** Run `go run ./scripts/checktestids` (strict mode).
Expected: every P1 in `docs/tests/core/README.md` (556) implemented, no `ERROR` lines, and exit 0. Any MISSING line is assigned back to its owning WP.
- [ ] **Step 5:** Run `make test` with `-count=3 -race`.
Expected: PASS three times (flushes order-dependent tests).
- [ ] **Step 6: Commit.** `git commit -m "test: seed users through AUTH, move area screens onto UI components, run integration tests against real services"`

### Task 4: End-to-end lifecycle in a browser

**Files:**
- Create: `e2e/e2e_test.go` (build tag `e2e`), `e2e/harness.go` (starts `internal/app` on a random port with the canonical seed and fixture DocTypes; chromedp contexts at a given viewport; sign-in helper)

- [ ] **Step 1: Write failing tests** for spec §10.2: `TestE2E_FixtureLifecycle` (Priya on a 390 px phone creates `fixture.Order` for SHREE with the reference PO lines through the wizard, sees the progress bar advance, reviews, submits; Ravi on 1440 px approves from the inbox; number `PO/26-27/00001` shown; Suresh on 360 px creates and submits `fixture.Receipt` against it; cancel of the order is blocked with the linked-document message; receipt cancelled with reason, then order; amend approved as `PO/26-27/00001-1`), `TestE2E_ConcurrentApproveSecondSeesMessage`, `TestE2E_WizardKeepsDataOnBack` (browser back from step 3 to step 2 shows saved values), `TestE2E_LineGridRapidEdits` (CD-95: Priya at 1440 px changes qty on rows 1, 2 and 3 of a draft with Tab between them, without waiting; no conflict banner appears, the draft's `version` rises by exactly 3, all three quantities are saved, there are three audit entries, and "Next" then saves without a 409; a second browser saving the same draft in between gives Priya the stale-record banner and does not overwrite the other user's change), `TestE2E_ExpenseFromPhoneWithReceipt` (Deepak at 360 px takes a receipt photo through "Take photo" (file input with `capture`) and submits; Priya, Deepak's reporting manager, sees it in the inbox through the `team` scope and approves). Also run every `TestE2E_*` named in `docs/tests/core/` (for example `TestE2E_PrintA4`, `TestE2E_NamingSettingsList`, `TestE2E_ApprovalWithdraw`); each area plan lists the helpers it provides.
- [ ] **Step 2: Run to see them fail.** `make e2e` → FAIL.
- [ ] **Step 3: Fix only wiring issues here;** behaviour bugs go back to the owning package with a unit test first.
- [ ] **Step 4: Run to see them pass.** `make e2e` → PASS.
- [ ] **Step 5: Commit.** `git commit -m "test(e2e): add fixture lifecycle, approvals and expense flows"`

### Task 5: Overflow and accessibility sweep

**Files:**
- Create: `e2e/sweep_test.go`, `e2e/routes.go` (list of every screen URL with the state needed to reach it, generated from the router plus fixture IDs), `internal/testkit/longvalues.go` (seed with maximum-length values), `e2e/axe/axe.min.js` (pinned, licence noted)

- [ ] **Step 1: Write failing tests:** `TestE2E_NoHorizontalOverflow` (for every route and Setup variant × widths 320, 360, 390, 768, 1024, 1440, mobile emulation below 768: `document.documentElement.scrollWidth <= document.documentElement.clientWidth` (not `innerWidth`, which grows with content under emulation; script in `docs/research/frontend-tech.md` §6.4) and no element's box extends past the viewport or holds content wider than itself; failure prints route, width and the widest element's selector), `TestE2E_AxeNoViolations` (axe-core with WCAG 2.2 AA rules, zero violations), `TestE2E_TargetSizeAtLeast44` (every interactive element at 360 px is at least 44×44 CSS px or has the spacing exception), `TestE2E_EveryRouteCovered` (every GET route from `chi.Walk`, except `/static`, `/print`, `/api`, has a screen entry), `TestE2E_NoCSPViolations` (e2e builds add `report-uri /csp-report`; zero reports across the sweep; when green, change `style-src` to `'self'` in `platform/web`).
- [ ] **Step 2: Run to see them fail,** then fix in `internal/ui` or the owning screen.
- [ ] **Step 3: Run to see them pass.** `make e2e` → PASS.
- [ ] **Step 4: Commit.** `git commit -m "test(e2e): enforce no horizontal scroll and WCAG 2.2 AA on every screen"`

### Task 6: Release build, deployment and restore drill

**Files:**
- Create: `deploy/app.service` (systemd), `deploy/Caddyfile` (reverse proxy with automatic TLS), `deploy/litestream.yml`, `scripts/restore-drill.sh`, `docs/operations.md`

- [ ] **Step 1:** `make build` for `linux/amd64` and `linux/arm64` with `CGO_ENABLED=0`; record binary sizes in `docs/operations.md`.
- [ ] **Step 2:** Write `scripts/restore-drill.sh`: start the app with Litestream replicating to a local MinIO (or a local file replica), run 200 approvals, kill the process with SIGKILL mid-write, restore from the replica into a new data dir, run `PRAGMA integrity_check` (expect `ok`) and compare approved-document counts (expect equal to committed count).
Run: `./scripts/restore-drill.sh`
Expected: prints `integrity ok` and `approvals 200/200`.
- [ ] **Step 3:** Write `docs/operations.md`: install, first admin, backup and restore, upgrade (stop, replace binary, start; migrations run on start), file GC, logs.
- [ ] **Step 4: Final gate.**
Run: `make ci && make e2e && go run ./scripts/checktestids`
Expected: all green; checker exits 0.
- [ ] **Step 5: Commit and tag.** `git commit -m "chore: release build, deployment files and restore drill"` then `git tag core-v1`.

## Added by CD-198 (30 Oct audit)

Task 2 (composition root) also does:
- `app.Config` gains `Secure` and `TrustProxy` and passes them to `authweb.Config`; a test proves a client IP behind the proxy and the Secure cookie flag.
- Runs `auth.SeedDefaults` before serving, with `Config.Seed` (CD-158, CD-171), and sets `PickerURL` (CD-194), `LogoURL` (CD-154) and `LogoPolicy`.
- Fills `ui.Shell` navigation and the inbox count, and sets Shell CSRF from `authweb.CSRFToken`.
- Owns the signed-in `/` route, where `SafeNext` and the header brand link land.
- Routes `/doc/{DocType}/export.csv`, which docscreen already links to.
- Mounts `web.BodyCeiling(limit, files.MsgTooLarge)` with `r.Use` right after `web.NewRouter(opts)`, before any route, and tests a 413 with CSP, a log line and FILE-003's exact text inside the alert div through `app.Handler()` (CD-211). Passes `allmasters.WebDeps` to `Routes`.
- Wires `files.New` Policies and Info, `Letterhead.LogoURL`, and merges `gstinweb.Parsers()` and `itemweb.Parsers()` into ValidateHandler; a nil `Deps.Parse` 404s every kind (CD-209).
- The /search Lister adds no default filters, and AUTH-106 compares rows, not the page body (CD-215).
- Mounts `GET /search` with Self, not CheckedInHandler (CD-87's prefix is /doc/), split out of docscreen.Routes at the mount, and wires `Deps.RecordURL` (CD-200).
- Mounts `/ui/validate/*` for real pages; today only the gallery mounts it.
- Calls `company.SetupTx` from first-run setup with CD-165's fields.
- Adds `app admin create` before the site opens, and an admin password reset command, so a sole admin who forgets a password can recover without SQL.
- `/healthz` reads the database. Logs are JSON to stdout (spec §7). HSTS is set.
- Deletes the empty `TestContracts_StubsReturnNotImplemented` (CD-154).

Task 6 (release) also does:
- Adds the hourly `files/` sync timer (CD-141) to `deploy/`.
- Sets Litestream retention explicitly; the value is the owner's recovery target.
- The restore drill covers `app.db` and `files/` and compares checksums (spec §10.4).
- The 200-approvals drill runs on an e2e-tag build, because the release binary has no fixture DocTypes.
- Adds a pre-upgrade backup and a rollback step to `docs/operations.md`.
