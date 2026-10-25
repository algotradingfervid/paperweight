# Contract changes

Decision log for spec §5 (contracts), §4 (architecture) and §2 (stack). Round 1 settles every request raised by the WP plans written on 24 Oct 2026. The spec text has already been updated to match; this file records **why** and which requests each decision answers. Later rounds append below; a WP that needs a change stops, adds a "Requested" entry here, and waits for the lead.

Request IDs: AUTH `C1–C14` (wp1-auth), NAM `C1–C9` (wp1-naming), DOC (wp1-doc "contract problems" 1–8), APR `CC-1–CC-8` (wp1-approval), FILE `CC-FILE-1–5`, NUM `CC-NUM-1–3`, MST `CC-MST-01–04`, `G1–G10`.

## Round 1 (24 Oct 2026): all decided

### Structure: breaking the import cycles

| ID | Decision | Answers |
|---|---|---|
| CD-01 | **Shared identity and paging types move to `internal/types`:** `types.UserID`, `types.Page`, `types.Result[T]`. `auth.UserID`, `list.Page`, `list.Result` become aliases. `audit` therefore no longer imports `auth`, and `approval` no longer imports `list`. | DOC 1, DOC 2, APR CC-1, AUTH C3 |
| CD-02 | **New package `internal/kernel/doctype`** holds the registry and every type describing a DocType: `DocType`, `Field`, `FieldType`, `SortKey`, `LinkSpec`, `RollUp`, `Hooks`, `State`, `Action`, `FieldError`, `Info`, `InfoSource`, `MasterInfo`, `MasterLookup`, and `Register`, `Lookup`, `All`. It imports only `types`, `platform/db`, `templ` (none). `doc` re-exports them as aliases. `auth`, `naming`, `approval`, `files`, `print`, `list`, `csvio` import `doctype`, never `doc`. | DOC 2, AUTH C1, NUM CC-NUM-2, FILE CC-FILE-5 |
| CD-03 | **`audit` is importable by every kernel package** and gains a `Writer` interface plus `audit/auditfake` (a recorder). Packages take an `audit.Writer` in their constructor; production passes `audit.Default`. | NAM C4, APR CC-4, AUTH C3 |
| CD-04 | **Allowed kernel imports (§4.2 rule 3) are rewritten** as a table in the spec. Notable additions: `naming → auth, audit`; `files → auth, audit`; `print → auth, files`; `csvio → list, audit`; `auth → audit`. | NAM C3, FILE CC-FILE-3, NUM CC-NUM-3 |
| CD-05 | **Facts about a document come from `doctype.InfoSource`**, implemented by the DOC engine: `Info(ctx, tx, doctype, id) (Info, error)` with the record ref (creator, employee, projects, sites), state, number, display label (`PO/26-27/00001` or `Draft 42`) and DocType label. APR, FILE and PRINT take an `InfoSource` in their constructor; Wave 1 tests use `docfake`. | APR CC-2, FILE CC-FILE-2 |

### platform/db

| ID | Decision | Answers |
|---|---|---|
| CD-06 | `(*Tx).SQL()` returns `db.DBTX` (sqlc's four-method interface), not `*sql.Tx`, so no hook can commit or roll back. | DOC 8 |
| CD-07 | Add `(*Tx).Writable() bool`. | NAM C5 |
| CD-08 | `Write` maps SQLITE_BUSY to `db.ErrBusy` ("The system is busy. Please try again in a moment."). `db.Open` takes options; `db.WithBusyTimeout(d)` exists for tests (DOC-075 uses 200 ms). `testkit.DBPath(t)` exposes the file path. | NAM C6, DOC 8 |

### auth

| ID | Decision | Answers |
|---|---|---|
| CD-09 | **Rights metadata travels on the DocType:** `doctype.DocType` gains `OwnerField`, `EmployeeField`, `ProjectFields`, `SiteFields` (header and line columns) and `DefaultRights map[string]map[string]string` (role → right → scope). `auth` reads the registry. No `auth.RegisterDocType`. | AUTH C1, AUTH C7 |
| CD-10 | **Production default rights** come from each DocType's `DefaultRights`. Any role that can create a document also gets `read` (all) on every master that document links to, so pickers work. The test seed stays exactly as `00-fixtures.md`. | AUTH C7 |
| CD-11 | **New scope `team`.** Scopes are `own`, `team`, `all`. `team` = own, plus records whose employee field's employee reports **directly** to me (active employees only). Seed: Employee role gets `read` and `approve` with scope `team` on Expense Claim (and `fixture.Claim`). This is Odoo's "Team Approver" model; ERPNext reaches the same result with the Expense Approver user permission. The rights screen shows None / Own / Team / All, with Team offered only on DocTypes that have an employee field. | APR CC-7 |
| CD-12 | `auth.Service` gains `User(ctx, tx, id) (UserInfo, bool, error)`, `EnableUser(ctx, tx, id)`, `CheckValues(...)`, `CheckLink(...)`. Package function `PrincipalFrom(ctx)` and `WithPrincipal(ctx, p)`. `authfake` implements all of them. | MST CC-MST-03, AUTH C4, AUTH C5, FILE CC-FILE-5 |
| CD-13 | `auth.Directory` (employee ↔ user, manager chain, project and site codes) is the single directory interface, implemented by MST in Wave 2, every method taking `tx *db.Tx`. APR uses the same interface for reporting managers instead of declaring its own. | AUTH C2, APR CC-8c |
| CD-14 | **Sessions:** drop `scs/sqlite3store` (wall-clock expiry, no `user_id`, cgo dependency in its go.mod). AUTH owns an `scs.CtxStore` over its own `sessions` table and builds the `*scs.SessionManager`; `platform/web` accepts the session manager and AUTH middleware. Session cookie has no Expires (browser session); the server enforces 12 h idle / 7 days. | AUTH C5, AUTH C11 |
| CD-15 | **CSRF:** keep `http.CrossOriginProtection` **and** add a per-session synchronizer token (`X-CSRF-Token` header for htmx, `csrf_token` form field). UI reads it with `authweb.CSRFToken(ctx)`. | AUTH C8 |
| CD-16 | `Predicate.SQL` columns are qualified with the DocType's table name; line tables are reached with `EXISTS (… WHERE <lines>.parent_id = <table>.id …)`; `SQL == ""` means no filter. `list.Build` never aliases the main table. Every lines table has `parent_id`. | AUTH C6 |
| CD-17 | Expired or revoked session: browser GET → 303 to `/signin?next=…`; htmx (`HX-Request`), `/api/*` and non-GET → 401 "Your session has expired. Sign in again." (htmx also gets `HX-Redirect`). | AUTH C10 |
| CD-18 | New dependencies: `golang.org/x/text` (NFC), `golang.org/x/term` (password prompt), SecLists `10k-most-common.txt` (MIT, embedded). AUTH ships `auth/cli` (`AdminCreate`, `UserDisable`); WP3 wires them into `cmd/app`. | AUTH C13, AUTH C14 |
| CD-19 | AUTH-055 copy is neutral: "{Name} has created or changed records, so they can't be deleted. Disable them instead." | AUTH C11 |

### doc, audit, naming, approval

| ID | Decision | Answers |
|---|---|---|
| CD-20 | `audit.Change` gains `LineNo int`, `LineID int64`, `Op string` (`set`, `add`, `remove`). | DOC 4 |
| CD-21 | **Masters register as DocTypes** with `Master: <lookup>`; `Submittable=false`. DOC validates every link to a master through `MasterLookup` (not found, disabled, blocked, warning). | MST CC-MST-01, MST G2, DOC 5 |
| CD-22 | `Engine.References(ctx, tx, table, id, limit) ([]Reference, error)` with DocType, field, id, display and state. `IsReferenced` stays as a convenience. | MST CC-MST-02 |
| CD-23 | **DOC ↔ APR call protocol** (APR owns approval history; written into §5): submit → `Evaluate` → `Open` always (records "submitted") → if not needed, continue as approve without `CheckApprover` and `Record("approve")`. approve/reject → `CheckApprover` **before** DOC's from-state guard → … → `Record("approve"/"reject")`. revise → `Record("revise")`. withdraw → DOC state guard and `submit` right first → `Record("withdraw")`. `Record` action strings equal `doctype.Action` values. Delete of a draft makes no APR call. | APR CC-5, DOC 3 |
| CD-24 | `approval.Requirement` gains exported `Amount types.Money` and `Requester types.UserID`. | APR CC-3 |
| CD-25 | DOC adds `fixture.Claim` (employee-based, approvable, labelled "Expense Claim", Amount = sum of lines). | APR CC-6 |
| CD-26 | **Stale approve/reject wording:** when a person's decision produced the current state, "{Label} is no longer waiting for approval. It was approved by Ravi Kulkarni."; otherwise "{Label} is not waiting for approval. It is {State}." DOC-005, APR-055, APR-057 and APR-071 follow this; DOC gets the name through `auth.Service.User`. | DOC 6, APR wording note |
| CD-27 | Second amendment wording: DOC-065's text wins; NAM uses it. | DOC 6 |
| CD-28 | `naming` gains `Admin` (list, change template, set counter, preview, `EnsureSeries`) and package functions `ValidateGSTDocNumber`, `Label`, `GuardNumberField`, `CheckAmendmentNo`. DOC calls `GuardNumberField` on save/import and `CheckAmendmentNo` on amend. `internal/app` calls `EnsureSeries` for every registered DocType with a `Series` at startup. | NAM C1, C2, C8 |

### files, print, num, masters

| ID | Decision | Answers |
|---|---|---|
| CD-29 | `files.Service` gains `List`. `Attach` takes an already-spooled file (`files.Spooled`, created by `Store.Spool` outside the transaction) instead of an `io.Reader`. `CopyAll` records copies as uploaded by `to.CreatedBy` at the amendment time. | FILE CC-FILE-1, CC-FILE-4, FILE design risk 6 |
| CD-30 | `print` declares `Letterhead` (company and branch details) as an interface; MST's `company` package implements it; `internal/app` wires it. | MST G10 |
| CD-31 | `gstcalc.LineAmount` returns `(types.Money, error)`. Inter-state is carried on each `gstcalc.Line`. `gstcalc.StateName(code)` owns the GST state table. | NUM CC-NUM-1, NUM gaps |
| CD-32 | `csvio.RegisterImporter(doctype, RowFunc)`: per-row master validation inside one `db.Write`, rolled back for the dry run. | MST CC-MST-04 |
| CD-33 | NUM formatters are contracts: `types.ParseMoney`, `FormatMoney`, `FormatINR`, `FormatQty`, `FormatQtyUoM`, `FormatRate`, `FormatPercent`, `FormatDate`, `FormatIST`. **One timestamp format everywhere: `24-10-2026 14:45`** (NUM-075); APR-056 changes to match. | APR CC-8, NUM gaps |
| CD-34 | Declared SQLite column types are only `INTEGER`, `TEXT`, `BLOB` (no `REAL`, `NUMERIC`, `BOOLEAN`, `DATETIME`). NUM-016 excludes goose's version table only (scs's table no longer exists, CD-14). | NUM gaps |
| CD-35 | `DocType.PluralLabel` ("Purchase Orders") for messages and list titles. | NUM gaps |
| CD-36 | MST migration blocks are 50 numbers each inside 01000–01999. | MST G1 |
| CD-37 | Item gains optional `standard_rate` (Rate). Item import template: `Code,Name,Type,UoM,HSN/SAC,Tax Code,Standard Rate,Disabled` (NUM-140 updated). | MST G6 |
| CD-38 | Message owners win: disabled-link wording is DOC-034's; GSTIN check-digit wording is MST-031's, prefixed "Row N: " in imports. | MST G7 |
| CD-39 | Registered Regular accepts the TCS/TDS GSTIN formats; UIN GSTINs get "This GSTIN category isn't supported yet." HRS maps to UQC `OTH`; seed adds `DRUM` (UQC `DRM`). `employee.user_id` is unique among active employees (partial index). | MST G8, G9, G4 |

### Process

| ID | Decision | Answers |
|---|---|---|
| CD-40 | **WP exit criteria (§9 rule 3):** a Wave 1 WP is done when all its P1 cases that do **not** need another WP's real implementation pass. Cases marked in the plan as "after DOC/NAM merge" or "Wave 3" become WP3 entry and exit criteria. Every `TestE2E_*` lives under `e2e/` and is owned and run by WP3; area plans may provide helpers. `checktestids` accepts `--exclude-level e2e`. | APR process note, DOC 7, NAM C7, FILE e2e cases |
| CD-41 | The test-ID checker is `go run ./scripts/checktestids <AREA>` (WP0 name). | DOC 8 |

## Round 2 (24 Oct 2026): requests from the WP1-UI plan, all decided

| ID | Decision | Answers |
|---|---|---|
| CD-42 | `doctype.Field` gains `ListPriority int` (0 = not in list; 1 title, 2 figure, 3 state, 4+ meta), `Options []string` (Select), `MaxLen int`, `Hint string`, `Step string` (wizard step slug; empty = first step). UI's `RegisterList`/`RegisterOptions`/`RegisterForm` workarounds are dropped. | CC-UI-1, CC-UI-2 |
| CD-43 | `doc.Engine` gains `Load(ctx, tx, doctype, id) (Record, error)`; `doc.Record{ Info doctype.Info; Version int; Values map[string]string; Lines []map[string]string }` with values in the plain format DOC already uses for `ActionInput` (`"1200.000"`, `"2026-10-24"`). Computed totals are ordinary read-only fields. `docfake` implements it. | CC-UI-3 |
| CD-44 | Every kernel error that reaches a handler implements `doctype.HTTPError{ Status() int; FieldErrors() []FieldError }` (403, 404, 409 version conflict, 422 validation, 503 busy). UI maps by this interface only. | CC-UI-4 |
| CD-45 | `make generate` runs `go generate ./...` (CSS bundle, icons) before sqlc and templ; `make e2e` runs `go test -tags e2e ./...`. Generated files are committed. | CC-UI-6 |
| CD-46 | §9: UI owns `internal/ui/` only. Generic `/doc/{doctype}/…` routes check rights inside the handler through `auth.Service.Can`; AUTH's route guard supports a "checked in handler" mode for them. | CC-UI-7, CC-UI-8 |
| CD-47 | htmx `responseHandling` also swaps 409 (version conflict page). Errors are never toasts: WP0's error toast becomes a persistent `role="alert"` region. Every static asset, including htmx and Alpine, is served under a content-hashed name with the immutable cache header. | SG-3, SG-5, SG-10 |
| CD-48 | Number fields are checked on blur by the server (`/ui/validate/{kind}` using NUM's parsers), not reformatted in JavaScript. | SG-4 |
| CD-49 | Screen owners: numbering settings (NAM-051) → NAM; user restrictions editor and the "Who can…" view → AUTH; each built from UI components. Wave 1 area templates move onto UI components after UI merges (a WP3 task). | SG-7, SG-8 |
| CD-50 | "Every step saves a draft" applies to documents. Master create wizards (party, employee) hold step values in the server-side session and save once at "Check and save"; edits of existing masters save per section. Design system §8.3 updated. | SG-11 |
| CD-51 | Timestamps in DOC-089 and APR-079 follow CD-33 (`24-10-2026 14:45`). Rejected documents show "Revise", never "Edit". | SG-1, SG-2 |

## Round 3 (24 Oct 2026): open points from the alignment pass, all decided

| ID | Decision | Raised by |
|---|---|---|
| CD-52 | **Order of checks for approve/reject:** DOC first checks visibility (`read` scope; hidden → 404), then calls APR `CheckApprover` (which owns the approve-right and eligibility messages), then its from-state guard. DOC does **not** run its generic `Can(approve)` for these two actions. For every other action DOC runs `Can(right)` as before. APR cases where the user cannot see the record expect 404. | APR 1 |
| CD-53 | **NUM's `types` package merges first (Wave 1a).** NUM Tasks 1–5 (`internal/types`, all Parse/Format functions) are merged to main before any other Wave 1 WP finishes; other WPs rebase onto it. Cases that only need formatters are **not** deferred. WP0 implements `naming.Label` for real (one line). | APR 2, NAM 7, WP0 5, DOC 7 |
| CD-54 | One not-found sentinel: `doctype.ErrNotFound`; `auth.ErrNotFound` is the same value (`var ErrNotFound = doctype.ErrNotFound`). `doctype` has `Action*` and `State*` constants. `docfake` has `SetInfo(Info)`. `auth.Directory.UserForEmployee` and `DirectReports` skip employees who have left. | APR 6, NAM 5, DOC 6, WP0 3 |
| CD-55 | `doctype.Info` gains `Party string` (party or employee display name) for inbox cards and lists. | APR 3 |
| CD-56 | `authfake` has seeding helpers: `Grant(role, doctype, right, scope)`, `AddRole(user, role)`, `Restrict(user, projects, sites)`, `Hide(doctype, id)`. `authfake.NewDirectory()` is the shared Directory fake; AUTH and APR use it instead of their own. | NAM 4, AUTH 6, APR 6 |
| CD-57 | `naming.Admin` gains `PreviewTemplate(ctx, tpl string, d types.Date) (string, error)` (NAM-062). | NAM 1 |
| CD-58 | `print.LetterheadInfo{ LegalName, LogoURL, BranchName string; Address []string; StateName, StateCode, GSTIN string }` (from the FILE plan) goes into §5 and WP0's stub. FILE provides `printtest` print views for `fixture.Order`, `fixture.Receipt`, `fixture.Claim` so FILE-066 passes in Wave 1. | WP0 4, NUM/MST 6, NAM 6, FILE 8 |
| CD-59 | `list` may import `naming` (for `naming.Label`); `list.DisplayNumber` is dropped. | NUM 3 |
| CD-60 | csvio writes the "created by import" audit entry; MST's `RowFunc` skips its own create entry (context flag). `RowFunc`'s `row` is keyed by field `Name`, cells trimmed and formula-unescaped, links already resolved by code. | NUM 4, 5 |
| CD-61 | `FormatRate` shows 2 to 4 decimals (NUM-054): 625000 → "62.50". §5 comment corrected. | NUM 2 |
| CD-62 | Unit code for Drum is `DRUM` (UQC `DRM`); fixtures updated. | MST 1 |
| CD-63 | Sign-in routes are `/signin` and `/signout`; redirects are 303. Requests with no session follow CD-17 like expired ones. | AUTH 2, FILE 10 |
| CD-64 | Project and Site master DocTypes declare `ProjectFields: ["id"]` / `SiteFields: ["id"]` so restrictions apply to the masters themselves. | AUTH 5 |
| CD-65 | Test-only DocTypes registered outside `internal/fixture` use their package prefix (`authtest.Order`, `listtest.Item`), never `fixture.*`. | AUTH 7 |
| CD-66 | Copy owners: AUTH-049 wording for a second `admin create` wins ("Users already exist. Sign in as an administrator to add more users."); "Ask an Administrator." everywhere (not "Admin"); `db.ErrBusy` text everywhere (DOC-075 updated). | AUTH 9, APR 8, WP0 1 |
| CD-67 | AUTH C9: DOC owns the `created_by` assertions (amender becomes `created_by`; clients can't set it). AUTH C12: WP3 switches `testkit` user seeding to the real AUTH tables. | AUTH 11 |
| CD-68 | Master display everywhere is "{Code} {Name}" (DOC-034 expects "CEMENT-OPC OPC Cement 53 Grade"). `doc.Result` gains `Warnings []string` for `MasterInfo.Warning`; `Blocked` text is shown as given, prefixed "Line N: " on lines. | DOC 3, 4 |
| CD-69 | `audit.Ref` gains `Number` and `Action` (DOC-084); always use keyed literals. | DOC 5 |
| CD-70 | DOC calls `auth.CheckValues` on save for every field in `ProjectFields`/`SiteFields` (AUTH-099) and `auth.CheckLink` for every link to a document DocType (AUTH-101). | DOC 9 |
| CD-71 | NUM-016 and the CD-34 rule exclude every `goose_*` table. | WP0 8 |
| CD-72 | `testkit.Principal(t, email)` is the signature; APR adjusts. APR-097's Go test is renamed `TestE2E_ApprovalWithdraw`. | WP0 7, APR 5 |
| CD-73 | FILE test text follows AUTH: FILE-050 uses the standard 403 message, FILE-026 expects 403 (AUTH-109), FILE-015/037 use one message "Couldn't save the file. Try again or contact your administrator." | FILE 9 |
| CD-74 | A case's **Go test:** may name a subtest (`TestTemplate_Validate/missing_number`); `checktestids` matches the function part, and only subtest names may share a function. Every e2e-level case's Go test starts with `TestE2E_` and the checker enforces it (APR-079 → `TestE2E_ApprovalInboxRowContents`, APR-097 → `TestE2E_ApprovalWithdraw`). | index check |
| CD-75 | The htmx event for server notices is `notice` (`{"level":…,"text":…}`), not `toast`. `app.js` shows success and info as toasts and errors and warnings in the persistent `#alerts` `role="alert"` region. UI uses `web.HTMXURL` / `web.AlpineURL` (hashed names) from WP0. | WP0 2, 4 |
| CD-76 | `doctype.ErrNotFound`'s text is AUTH's: "This record doesn't exist or you don't have access to it." AUTH owns the wording; WP0 declares it. `platform/web` maps `db.ErrBusy` to 503 with its own text, so `db` needn't implement `HTTPError`. | WP0 5, 6 |
| CD-77 | FILE-056's Go test is `TestE2E_PrintA4` only; `TestPrintCSS_A4` stays as a FILE unit test without its own case ID. | WP0 1 |
| CD-78 | UI's `Lister` (list rows) and `Extras` (approval preview and history, audit history, attachments, `state_reason`, `amended_from`) are consumer interfaces in `internal/ui`; WP3 writes the adapters over DOC, APR, FILE and `list`. No new §5 contract. | UI 1 |
| CD-79 | `doctype.Field` gains `ReadOnly bool` for computed fields (totals, GST split); DOC rejects client values for them; UI shows them read-only on every step. | UI 2 |
| CD-80 | Flash messages: `web.Flash(ctx, level, text string)` appends a message to the session list and `web.TakeFlash(ctx) []web.Notice` returns and clears them all; the next full page shows it (success banner above `h1`; warnings and errors in `#alerts`). Used for `Result.Warnings` and success banners after a 303. WP0 provides it. | UI 3 |
| CD-81 | Master-mode wizards hide "Save and finish later". Version-conflict wording on screens is DOC-069's (kernel wording wins). WP3 changes `style-src` to `'self'` once `TestE2E_NoCSPViolations` passes. Error mapping goes through `web.ErrorFrom` (CD-76). | UI 4, 5, 8 |
| CD-82 | Accepted as named by the plans: `csvio.IsImport(ctx)`; employee create wizard steps details → reporting → Check and save. Prints show Qty and UoM in separate cells (FILE-065 updated). Masters without a code (address, contact) display their name only; reference lists keep "{Label} {code}" (MST-014). `fixture.Receipt` is Printable. List query errors are 422. | FILE/NUM/MST open issues |
| CD-83 | NAM-062: `PreviewTemplate` returns the next numbers joined by ", " (looked up by rendered prefix). CD-66 covers only "Ask an admin" variants; AUTH's other sentences stay. AUTH's new screen copy is accepted. The generic-route guard mode is `auth.CheckedInHandler`. Area screen subpackages (`<area>/web`) may import `internal/ui` (§4.2 rule 5). | AUTH/NAM open issues |

## Round 4 (24 Oct 2026): from the cross-plan consistency review

| ID | Decision | Finding |
|---|---|---|
| CD-84 | WP0's `contract.go` files hold the §5 declarations. Where WP0 also ships a working body (doctype registry, `auth.WithPrincipal`/`PrincipalFrom`, scope constants, `naming.Label`), the owning WP may **move** that body into its own file and must list `contract.go` under Modify. Nothing is ever declared twice. | 1 |
| CD-85 | `TestContracts_KernelImportsFollowTable` checks each kernel package directory itself against the §4.2 table. Subpackages follow these rules instead: `<pkg>fake` and `<pkg>test` helpers may import `testkit` and any fake; `<area>/web` (and its subpackages) may import `internal/ui`, `platform/web` and the area's own test helpers. | 2 |
| CD-86 | `authfake` gains `RemoveRole(u, role)`; `UsersWithRight` skips users added with `Enabled: false`. `Grant` accepts the wildcard DocType `"*"` (the Administrator row in `00-fixtures.md`). `auditfake.New()` returns `*auditfake.Recorder`. `docfake.SetResult` takes `(doctype, id, action, result, err)`. | 3, 11 |
| CD-87 | `/ui/validate/*` uses the `Self` guard. `CheckedInHandler` stays limited to `/doc/`. | 4 |
| CD-88 | The styled sign-in, password and session pages live in `internal/kernel/auth/web/views/` (AUTH owns them, built from UI components after WP1-UI merges). UI's Task 16 becomes a component hand-off; `internal/ui` never imports `authweb`. | 5 |
| CD-89 | Master screens live in `internal/masters/<pkg>/web/` subpackages; `allmasters/routes.go` imports those. Master service packages never import `internal/ui`. | 6 |
| CD-90 | `platform/web` imports no kernel package. `web.ErrorFrom` matches a local `interface{ error; Status() int }` with `errors.As`; `doctype.HTTPError` satisfies it. | 7 |
| CD-91 | WP3 also does: switch `testkit` user seeding to AUTH's tables (CD-67), write the `Lister`/`Extras` adapters (CD-78), move Wave 1 area templates onto UI components (CD-49), wire `auth/cli` `user disable` (CD-18), assert CD-17 (signed-out GET → 303; htmx and non-GET → 401) and DOC-085 (no PUT/PATCH/DELETE routes). | 9 |
| CD-92 | Stale text fixed: APR stub file is `contract.go`; WP3 uses `TestE2E_ApprovalWithdraw` and the current P1 total from the README; spec §3 AUTH row says own/team/all; design-system toasts use the `notice` event. | 8, 10 |
| CD-93 | "Area" in CD-85 means the work package: FILE owns both `files` and `print`, so `print/web/webtest` may import `filestest`. | UI/MST/FILE review 1 |

## Round 5 (25 Oct 2026): concurrency review

| ID | Decision | Finding |
|---|---|---|
| CD-94 | **Checks read inside the write transaction, and writes use the posted version.** Any check that depends on other rows (quantity limits, linked documents, approver eligibility, source state) reads inside the `db.Write` transaction it guards. A handler always passes the `version` the user's page posted, never one it just loaded. Spec §4.3. | Checking before the transaction lets two users both pass a limit (DOC-072 catches this for roll-ups; the rule makes it general for later modules). Using the freshly loaded version turns a stale save into a silent overwrite. |
| CD-95 | **Desktop line grid saves one row at a time with the latest version.** One `#doc-version` input, rendered by the wizard frame. Row forms use `hx-include="#doc-version"`, `hx-sync="#line-editor:queue all"` and swap only their own row; the response updates totals and `#doc-version` out-of-band. Design system §8.2, WP1-UI Tasks 9 and 14, WP3 `TestE2E_LineGridRapidEdits`. | Each row save increases `version`, but the plan didn't say how the next row's post got it, so quick edits to two rows gave the user a 409 against their own edit. Two other problems found in htmx 2.0.11: requests from elements removed from the page are dropped (`htmx.js:4335`), so swapping all of `#lines` would lose queued row saves; and the default `queue last` discards earlier queued requests (`htmx.js:4440`). |

## Round 6 (25 Oct 2026): execution process

| ID | Decision | Finding |
|---|---|---|
| CD-96 | **Tasks run under the execution protocol** (`docs/superpowers/plans/2026-10-25-execution-protocol.md`, spec §9 rule 6). A director dispatches workers using one brief format and reads their status files. Each task gets a risk tier: High tasks use a separate test-writer whose tests the implementer may not change, and a fresh reviewer; Standard tasks use one test-first implementer and a fresh reviewer; Light tasks are reviewed in batches of up to 5. Only `haiku` and `opus` are used. Minor findings are fixed in place with no new review; anything `make ci` catches is not a review finding; each check runs once, and the reviewer reads the hand-in output instead of re-running it. At most 2 attempts per task, then a re-plan. Plan headers point to the protocol; WP0 git-ignores `/.work/`. | PO-tato v2 ran every task through four fresh agents. Its median task took 2.0 h and the slowest 9–16 h, and 159 of 401 reviews asked for changes, mostly for coverage, docs and UI items rather than logic or security. Majors restarted the cycle and the full suite ran three times per task. |
| CD-97 | **UI and UX are designed before they are built** (execution protocol §9). A feature that adds or changes a screen first gets a design document from `docs/ux/TEMPLATE.md` (users and devices, flow, every screen in every state, phone first, components from the §7 catalogue, final copy, a checklist from design-system §1, §4, §9 and WCAG 2.2 AA), a mock-up in the component gallery for multi-screen features, a fresh UX review and owner approval. Only then are plan tasks written. `/ui-review` and `/ux-review` run on the built app at each wave end and before a release. Core v1's approved design is the design-system spec and `docs/research/ux-patterns.md`. | Owner requirement (25 Oct 2026): plan the UI and UX before developing any feature, using best practices, for a consistent experience. |
| CD-98 | **UI library: daisyUI 5 on Tailwind CSS v4** (owner choice, 25 Oct 2026; `docs/research/ui-library.md`). Replaces hand-written CSS in design-system §3. Tokens become the Tailwind theme (`@theme static`, defaults cleared); one custom light daisyUI theme copied from the tokens and checked by a test; daisyUI classes only inside `internal/ui` components (design-system §7.1); our own component CSS as Tailwind `@utility`; banned parts and new lint rules in research §4 and design-system §10. Tailwind v4.3.3 standalone (sha256-pinned, downloaded to `bin/tools/`) and vendored `daisyui.mjs` v5.7.50; no Node. WP1-UI Tasks 1, 2, 4 and 6 updated. | The owner wants a very popular library for consistent, polished UI. Verified in a test build: light-only theme, 44 px controls via `--size-field: 0.275rem`, bottom-sheet modal, 9.1 KB gzip CSS, and daisyUI's layers nest inside `utilities`, so `@layer components` rules would lose to it. |
| CD-99 | **Branding is one config folder applied at build time** (design-system §5.1). `internal/platform/brand/brand.json` sets the app name, primary colour (shades derived when absent), document accent and three image files; `go generate` and the tests reject colours that fail the contrast rules. The app theme, print stylesheet (`var(--doc-accent)`), app header, favicon and the letterhead logo (`brand.DocumentLogoURL`) all read it. Status colours are not brandable. WP0 Task 7b builds the package; WP1-UI Task 6, WP1-FILE print page, WP2 M11 and WP3 Task 2 consume it. | Owner (25 Oct 2026): changing the logo and colour scheme for the app and outgoing documents must be a config change and a rebuild, not development. Runtime branding is not needed. |
| CD-100 | **UX philosophy and principles** (design-system §4): calm, fast and obvious; rules 1–7 binding, rules 8–12 (speed as design, prevent then undo, recognise not recall, familiar beats clever, fewer choices and bigger targets) for decisions the rules don't settle, each tied to its established source (Nielsen's heuristics, Doherty threshold, Jakob's, Hick's, Fitts's and Tesler's laws). The UX template checklist (`docs/ux/TEMPLATE.md`) checks them. | Owner asked for the UX principles and philosophy behind the design. |

## Round 7 (25 Oct 2026): crew harness

| ID | Decision | Finding |
|---|---|---|
| CD-101 | **Work runs on the global crew harness** (`~/.claude/harness/README.md`). The project-local `pw-*` agents are replaced by the global `crew-*` roles (implementer, test-writer, reviewer, verifier, researcher, ux-designer, ui-reviewer); `.claude/harness.json` holds this project's paths, protected files and commands; briefs start with `TASK: <label> \| ROLE: <role> \| TIER: <tier>`. Global hooks enforce the agent rules (explicit model, never `fable`, workers never change git history or protected files, read-only roles never write) and log every worker's duration, model, tokens and result to `.work/timing.jsonl` (`/crew-report`). Project `CLAUDE.md` follows Anthropic's own repos: commands, who owns which files, things that will bite you. Code-change rules (simplicity first, surgical changes) added to the owner's global rules. The earlier security-, UI- and UX-review harnesses are archived. | Owner (25 Oct 2026): build the harness before the app, reusable for future projects, with timing logs and guard hooks; inspired by public CLAUDE.md files from Anthropic repositories, Ghostty and the Karpathy-derived guidelines. Live checks passed: a dispatch without `model` was blocked, a crew worker's `git commit` was blocked while its other commands ran, and its timing row was written. |

## Round 8 (25 Oct 2026): WP0 build

| ID | Decision | Finding |
|---|---|---|
| CD-102 | **`db.Write` gets `BEGIN IMMEDIATE` from the driver option `_txlock=immediate`**, not from a hand-run statement on a dedicated connection (WP0 Task 3 Step 3). `Write` uses `BeginTx`, so `Tx` wraps a real `*sql.Tx`; `Read` stays a deferred `begin`; goose's migration transactions also become immediate. `Read` does not refuse writes at the driver level; CD-07's `Writable()` remains the guard. | Implementer departure, verified in `modernc.org/sqlite` v1.61.0 `sqlite.go:426-431` and `tx.go:22-25` by the Task 3 reviewer; same behaviour, less code. |
| CD-103 | **WP0 addition: `brand.Generate(c Config, files fs.FS) ([]byte, error)`.** `brand/gen` is `package main` and only calls `Parse` then `Generate`, so `TestBrand_GenUpToDate` can run the generator in memory. Colour maths in `brand` uses `float64` with a per-line `//nolint:forbidigo // colour maths, not a business value`; the float ban for business values stays repo-wide. | Task 7b implementer note, accepted by the reviewer. |
| CD-104 | **`types` stubs return their own "not implemented yet" error**, not `errs.ErrNotImplemented`: `types` imports no `internal/` package (§4.2), and the import rule wins over the WP0 stub rule. `errors.Is(err, errs.ErrNotImplemented)` is false for `types` stubs; NUM's tests must not assert it. | Task 5 implementer departure, confirmed by the Task 5 reviewer. |
| CD-105 | **WP0 addition: `docfake.Call` and `(*docfake.Fake).Calls() []Call`**, so tests can read back the `Do` calls the plan says the fake records, like the other fakes' `Calls()`. | Task 5 review minor; kept. |
| CD-106 | **Director's fixture rights include `fixture.Order` and `fixture.Claim`** (`00-fixtures.md` rights table), matching the approval rules that name Director as the `fixture.Order` approver from ₹5,00,000.00 and the `fixture.Claim` fallback. The row was the only one without the fixture twins. | Task 6 review minor; doc consistency fix by the director. |
