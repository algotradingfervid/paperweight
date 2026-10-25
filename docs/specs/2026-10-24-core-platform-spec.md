# Core Platform Specification

Version 0.1 · 24 October 2026 · Status: draft for review

This spec defines the core of the internal business app: the platform kernel and the master data that every module (Purchase Order, GRN, Expenses and later ones) is built on. Module specs come later and plug into the contracts defined here.

Companion documents:
- Architecture diagrams: https://claude.ai/artifact/K2f1KF2GpFEhUmickeYmvL
- Unified test cases: `docs/tests/core/01-auth.md` … `07-numbers-lists.md`
- Research brief and fixed decisions: `docs/research/BRIEF.md`
- Development plans: `docs/superpowers/plans/`

---

## 1. Goals and scope

**Goal.** A small, type-safe, single-binary platform that a 15 to 20 person Indian company can run on one server with almost no operations work. New business modules are added one at a time by defining two tables, a few rules and a print template.

**In scope (core v1)**
- Platform kernel: identity and permissions, naming series, document lifecycle and audit trail, approvals, files and printing, numbers/dates/list queries/CSV, shared screens.
- Masters: company and branches, party (vendors and customers), address and contact, item, unit of measure and conversion, tax code (GST), project, site/warehouse, employee, expense type, payment terms.

**Out of scope**
- Never: multi-tenancy (one company per install).
- Not in v1: accounting and GST filing (Zoho Books does this), multi-currency, multi-company, SSO, website or portal, translations, email inbox, live updates, adding fields from the browser.

**Success criteria for core v1**
1. Every P1 test case in `docs/tests/core/` passes in CI.
2. An administrator can install the binary, create the first user, load masters from CSV and manage them from the browser.
3. A sample "Test Document" DocType (used only in tests) goes through the full lifecycle (draft → approval → approved → cancel → amend) using only kernel code, proving a new module needs no kernel changes.
4. One command builds the binary; one command runs every test.

## 2. Technology stack

Rules: open-source only; latest stable versions; prefer well-maintained existing Go libraries over our own code. Versions below were checked on 24 October 2026 against the Go module proxy, npm and GitHub. Each implementation plan starts by confirming they are still the latest.

| Concern | Choice | Version | Licence | Why |
|---|---|---|---|---|
| Language | Go | 1.27.3 | BSD-3 | Static types, single binary, compatibility promise |
| Database | SQLite via `modernc.org/sqlite` (pure Go, no cgo) | v1.61.0 (bundles SQLite 3.54.1) | BSD-3 | One file, no server; pure Go keeps cross-compiling simple |
| Typed SQL | `sqlc` | v1.31.1 | MIT | Generates typed Go from plain SQL, checked at build time |
| Migrations | `pressly/goose/v3` | v3.29.0 | MIT | Plain SQL files, embedded in the binary |
| HTTP routing | `go-chi/chi/v5` | v5.5.0 | MIT | Route groups and middleware on top of `net/http` |
| Sessions | `alexedwards/scs/v2` with AUTH's own `CtxStore` over the `sessions` table | v2.9.0 | MIT | Server-side sessions; own store keeps the test clock and per-user session lists (CD-14). `scs/sqlite3store` is not used |
| CSRF | Go standard library `http.CrossOriginProtection` **plus** a per-session synchronizer token (CD-15) | Go 1.27 | BSD-3 | Origin check built in since Go 1.25; token needed for AUTH-046/047 |
| Unicode and terminal | `golang.org/x/text` (NFC passwords), `golang.org/x/term` (password prompt) | latest at execution | BSD-3 | Official Go sub-repositories |
| Common-password list | SecLists `Common-Credentials/10k-most-common.txt`, embedded | pinned commit | MIT | D-AUTH password policy |
| Password hashing | `golang.org/x/crypto` | v0.58.0 | BSD-3 | Official Go crypto (algorithm decided in AUTH) |
| HTML templates | `a-h/templ` | v0.3.1070 | MIT | Type-checked templates compiled to Go |
| Browser interactivity | htmx | 2.0.11 | 0BSD | Server-rendered interactivity; embedded in the binary |
| Small client behaviour | Alpine.js **CSP build** (`@alpinejs/csp`) | 3.17.5 | MIT | Only where htmx can't do it (e.g. line-grid keyboard handling); the standard build needs `unsafe-eval` |
| CSS bundling | `evanw/esbuild` Go API (build-time, `go generate`) | v0.28.2 | MIT | Bundles and minifies hand-written CSS; no Node |
| Icons | Lucide (`lucide-static`, vendored SVGs generated into templ) | 1.61.0 | ISC | Only the icons listed in `internal/ui/icons/icons.txt` |
| Fonts | Inter (subset) and Noto Sans Devanagari (subset, on demand) | 4.1 / 2.007 | OFL-1.1 | Self-hosted; ₹ and tabular figures |
| Decimal maths | Go standard library fixed-point `int64` types | Go 1.27 | BSD-3 | D-NUM-27 |
| Upload type detection | `gabriel-vasile/mimetype` | v1.4.16 | MIT | Detects real file type from content |
| PDF | Browser print from an A4 print view (v1) | n/a | n/a | D-FILE-10; pure-Go PDF later |
| Background jobs (later: Zoho sync) | `maragudk/goqite` | v0.4.0 | MIT | Job queue stored in SQLite |
| Backups | Litestream (separate process) | v0.5.17 | Apache-2.0 | Streams the database to S3-compatible storage |
| Logging | Go standard library `log/slog` | Go 1.27 | BSD-3 | Structured logs, no dependency |
| Test helpers | `google/go-cmp` | v0.7.0 | BSD-3 | Readable diffs in table tests |
| Browser end-to-end tests | `chromedp/chromedp` | v0.21.0 | MIT | Pure-Go control of headless Chrome; test-only |
| Lint | `golangci-lint` v2 | v2.15.0 | GPL-3.0 (tool only, not linked) | Standard Go linter suite |

## 3. Requirements by area

Each area's full behaviour lives in its test-case file. Those files are the requirements. This section summarises and records cross-area resolutions.

| Area | Test file | Cases (P1 / P2) | Key decisions |
|---|---|---|---|
| Fixtures | `00-fixtures.md` | n/a | One shared seed for every test |
| AUTH | `01-auth.md` | 121 (92 / 29) | Email sign-in; argon2id; 5-failure lockout; 12 h idle / 7 day sessions; Administrator is a role, not a bypass; 11 rights × own/team/all scope; strict Project/Site restrictions; 403 vs 404 |
| NAM | `02-naming.md` | 65 (48 / 17) | Number assigned at first approval; gapless; `PO/{FY}/{#####}`; counter per rendered prefix; amendments `-1`…`-9`; immutable |
| DOC | `03-document.md` | 106 (76 / 30) | One state column; effects only on approved; roll-ups recomputed; one `BEGIN IMMEDIATE` transaction per action; integer `version`; always-on append-only audit; 1 to 500 lines |
| APR | `04-approvals.md` | 97 (72 / 25) | Rule = document type + from amount + approver; ≥ threshold; highest matching rule; no self-approval by default; reject needs reason; single level; in-app lists |
| FILE | `05-files-print.md` | 83 (67 / 16) | Files on disk by SHA-256; private, follow parent permission; 10 MB, 20 per document, allowlist; browser print A4; DRAFT/CANCELLED watermark; amount in words (lakh/crore) |
| MST | `06-masters.md` | 129 (96 / 33) | Upper-case unique codes; GSTIN on address with full India Compliance validation; snapshot on documents; delete only if unreferenced; per-item UoM conversion; flat sites; acyclic reporting managers |
| NUM | `07-numbers-lists.md` | 121 (105 / 16) | Money paise, Qty 3 dp, Rate 4 dp, Percent millipercent, Factor 9 dp; half-up; tax per line; CGST = SGST computed once; all-or-nothing CSV import; Indian grouping; dd-mm-yyyy |
| **Total** | | **722 (556 / 166)** | |

### 3.1 Cross-area resolutions (these override any area file that says otherwise)

| ID | Conflict | Resolution |
|---|---|---|
| R1 | DOC and APR assumed the number is assigned when a document first leaves draft; NAM assigns it at first approval | **NAM wins.** Numbers are assigned inside the transaction that first enters `approved`. Pending and rejected documents show `Draft {id}`. |
| R2 | F5 has no way for a requester to pull back a pending document (APR D-APR-12 deferred it) | **Add `withdraw`: `pending_approval → draft`,** allowed for the submitter (or anyone with `submit` right on the document) while it is still pending. The pending approval request is closed as "withdrawn" in approval history. P1. |
| R3 | AUTH requires a separate `print` right (D-AUTH-14); FILE says read is enough (D-FILE-15) | **AUTH wins.** Printing and the print view need `print` on the DocType plus visibility of the record. |
| R4 | Action names differ between DOC and AUTH | Actions map to rights: save → `create`/`write`; submit, withdraw, close → `submit`; approve, reject → `approve`; revise → `write`; cancel → `cancel`; amend → `amend`; delete → `delete`; print → `print`. |
| R5 | Example numbers and GSTINs differ between files, and some GSTINs were invalid | `00-fixtures.md` is canonical. Every file uses its company, branches, parties, users, roles, rules and number formats. |
| R6 | DOC treats `closed` as terminal; every source system can reopen | Kept terminal in v1. Reopen is listed as later (manager only, audited). |
| R7 | DOC forbids editing approved documents; ERPNext and Odoo allow changing qty/rate | Kept forbidden in v1. The fix is cancel → amend. "Update items after approval" is listed as later. |
| R8 | DOC required at least one line on every save; the wizard rule (each step saves the draft) saves the header before any line exists, and FILE-071 prints an empty draft | **Odoo/Tryton win.** A draft may have zero lines; **submit** requires at least one. DOC-022 updated. |

### 3.2 Document state machine (F5 + R2)

```
draft ──submit──▶ pending_approval ──approve──▶ approved ──close──▶ closed
  ▲  └─submit (no rule applies)──────────────────▲   │
  │                 │ withdraw / reject           │   └─cancel──▶ cancelled ──amend──▶ new draft
  └──revise── rejected ◀──reject──┘ (withdraw goes straight to draft)
```

| Action | From | To | Right | Kernel work in the same transaction |
|---|---|---|---|---|
| save | draft | draft | create / write | validate fields, version check, audit `create`/`save` |
| delete | draft | (deleted) | delete | delete lines and attachment rows, audit snapshot |
| submit | draft | pending_approval or approved | submit | validate, BeforeSubmit, APR evaluate; if no rule: same as approve |
| withdraw | pending_approval | draft | submit | close approval request as withdrawn, audit |
| approve | pending_approval | approved | approve | APR checks approver; re-validate; BeforeSubmit; NAM assign; ApplyEffects; roll-ups; audit |
| reject | pending_approval | rejected | approve | reason required (1–1,000 chars); approval history; audit |
| revise | rejected | draft | write | audit |
| close | approved | closed | submit | reason required (3–500 chars); audit |
| cancel | approved | cancelled | cancel | reason required; linked-document check first; ReverseEffects; roll-ups; audit |
| amend | cancelled | new draft | amend | copy header, lines, attachments; link `amended_from`; at most one live amendment |

## 4. Architecture

### 4.1 Repository layout

```
cmd/app/                      main: serve, migrate, admin create, files gc
internal/platform/db/         Open, Write (BEGIN IMMEDIATE), Read, migrations runner, sqlc config
internal/platform/clock/      Clock interface, real and fixed clocks
internal/platform/web/        chi router, middleware, CSRF, error pages, embedded static assets
internal/types/               Money, Qty, Rate, Percent, Factor, Date, Timestamp, FY      [NUM]
internal/kernel/auth/         users, sessions, roles, rights, scope predicates            [AUTH]
internal/kernel/naming/       series templates and counters                                [NAM]
internal/kernel/doctype/      DocType description types and registry (no kernel imports)  [DOC]
internal/kernel/doc/          state machine engine, hooks, links, roll-ups                 [DOC]
internal/kernel/audit/        append-only audit store                                      [DOC]
internal/kernel/approval/     rules, requests, history, inbox                              [APR]
internal/kernel/files/        attachment store and GC                                      [FILE]
internal/kernel/print/        print views, watermark, amount in words                      [FILE]
internal/kernel/list/         filters, sort, search, pagination, counts                    [NUM]
internal/kernel/csvio/        CSV import (dry run, all-or-nothing) and export              [NUM]
internal/kernel/gstcalc/      line amounts, tax split, totals, rounding                    [NUM]
internal/ui/                  templ layout, form, list, line grid, picker, toasts          [UI]
internal/masters/<name>/      one package per master                                       [MST]
internal/masters/gstin/       GSTIN, PAN, state code, pincode, HSN/SAC validation          [MST]
internal/fixture/             test-only DocTypes fixture.Order, fixture.Receipt, fixture.Claim [DOC]
internal/app/                 composition root: builds every real service, wires adapters  [WP3]
internal/testkit/             test DB, fixed clock, canonical seed from 00-fixtures.md     [Foundation]
migrations/                   numbered goose SQL files, one range per package (§4.4)
e2e/                          chromedp browser tests (build tag e2e)
```

### 4.2 Dependency rules

1. Imports point downward only: `cmd` → `masters` → `kernel` → `types`/`platform`. The kernel never imports `masters` or any module.
2. Kernel packages talk through the contracts in §5, never through each other's tables.
3. Within the kernel, a package may import only the packages listed for it (CD-04). `types`, `platform/*` and `doctype` are importable by every kernel package; `doctype` itself imports only `types` and `platform/db`.

   | Package | May also import |
   |---|---|
   | `audit` | (nothing else) |
   | `auth` | `audit` |
   | `naming` | `auth`, `audit` |
   | `approval` | `auth`, `audit` |
   | `files` | `auth`, `audit` |
   | `print` | `auth`, `files` |
   | `gstcalc` | (nothing else) |
   | `list` | `auth`, `naming` |
   | `csvio` | `auth`, `list`, `audit` |
   | `doc` | `auth`, `naming`, `approval`, `audit`, `files` |

   Where a package needs facts owned by a package it may not import, it declares a small interface and `internal/app` passes in the implementation (for example `doctype.InfoSource`, `auth.Directory`, `print.Letterhead`).
4. A module (later: PO, GRN, Expenses) talks to another module only through its exported functions, and depends only on earlier modules.
5. Screen subpackages (`internal/kernel/<area>/web/...`, `internal/masters/<name>/web`) may import `internal/ui` and `platform/web`; `<pkg>fake` and `<pkg>test` helpers may import `testkit` and any fake; `platform/web` imports no kernel package (CD-85, CD-89, CD-90). `internal/ui` imports kernel packages only for types and contracts, never an area's `web` package (CD-83).

### 4.3 Data conventions

- Primary keys `INTEGER PRIMARY KEY`; masters have a unique upper-case `code`; documents have a nullable unique `number`.
- Every header and master table has `version INTEGER NOT NULL DEFAULT 1`, `created_at`, `created_by`, `updated_at`, `updated_by`. Line tables have `parent_id` and `line_no` and are versioned through their parent. Append-only tables (audit, attachments, approval history) have only `created_*`/`uploaded_*`.
- Declared column types are only `INTEGER`, `TEXT` and `BLOB` (CD-34).
- Money, Qty, Rate, Percent, Factor are `INTEGER` columns of the fixed scales in §3. Business dates are `TEXT` `YYYY-MM-DD`; timestamps are `TEXT` fixed-width UTC `2026-10-24T05:00:00.000Z`.
- Foreign keys on (`PRAGMA foreign_keys=ON`), `ON DELETE RESTRICT` to masters.
- Connection settings: WAL, `busy_timeout=5000`, `synchronous=NORMAL`, one writer.
- Every write goes through `db.Write`, which opens `BEGIN IMMEDIATE`. Modules never open their own transactions.
- Any check that depends on other rows (quantity limits, linked documents, approver eligibility, source state) reads inside the same `db.Write` transaction as the write it guards, never from data loaded before the transaction (CD-94).
- The `version` checked by a write is the one the user's page was rendered from and posted back. A handler never substitutes a version it has just loaded, because that would silently overwrite another user's change (CD-94).

### 4.4 Generated code and migrations without shared files

- All migrations live in the top-level `migrations/` folder; each WP adds files only in its number range.
- Each package that needs typed SQL has its own `sqlc.yaml`, query files in `<pkg>/queries/` and generated code in `<pkg>/dbq/`. `make generate` runs every `sqlc.yaml` it finds, so no WP edits a shared sqlc config.
- templ files sit next to the Go code that uses them; `make generate` runs `go tool templ generate` over the whole tree.

### 4.5 Migration number ranges (so parallel work never collides)

| Range | Owner |
|---|---|
| 00001–00099 | Foundation (platform, schema conventions) |
| 00100–00199 | AUTH |
| 00200–00299 | NAM |
| 00300–00399 | DOC + audit + fixture DocTypes |
| 00400–00499 | APR |
| 00500–00599 | FILE |
| 00600–00699 | NUM (list, csvio) |
| 01000–01999 | MST (blocks of 50 per master, CD-36) |
| 10000+ | modules (PO, GRN, Expenses, …) |

## 5. Contracts (frozen in the foundation step)

The foundation work package creates these packages with the exported types and signatures below, stub bodies that return `ErrNotImplemented`, and a fake implementation in `<pkg>/<pkg>fake` for each service interface (plus `audit/auditfake`). Parallel work packages then fill in their own package and test against the others' fakes. A contract changes only through `docs/specs/contract-changes.md` and then this section. Decisions CD-01 to CD-92 are included below. Packages may add exported names beyond these; they may not change these.

```go
// ───────── internal/types  [NUM] ─────────
type Money int64   // paise
type Qty int64     // thousandths
type Rate int64    // ten-thousandths of a rupee
type Percent int64 // thousandths of a percent: 18% = 18000
type Factor int64  // 1e-9
type Date struct{ Year int; Month time.Month; Day int }
type Timestamp struct{ /* UTC instant */ }
type FY struct{ Start, End Date; Label, Short string } // "2026-27", "26-27"
func FiscalYear(d Date) FY
type UserID int64                                     // CD-01
type Page struct{ Number, Size int }                  // CD-01
type Result[T any] struct{ Rows []T; Total int }      // CD-01
func ParseMoney(s string) (Money, error)              // CD-33; also ParseQty, ParseRate, ParsePercent, ParseDate
func FormatMoney(m Money) string                      // "94,164.00"
func FormatINR(m Money) string                        // "₹94,164.00"
func FormatQty(q Qty) string                          // "1,200" (trailing zeros dropped)
func FormatQtyUoM(q Qty, uom string) string           // "1,200 kg"
func FormatRate(r Rate) string                        // "62.50" (2–4 decimals, CD-61)
func FormatPercent(p Percent) string                  // "18%"
func FormatDate(d Date) string                        // "24-10-2026"
func FormatIST(t time.Time) string                    // "24-10-2026 10:30", the only timestamp format

// ───────── internal/platform/db  [Foundation] ─────────
type DBTX interface { // CD-06: same method set sqlc generates
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	PrepareContext(context.Context, string) (*sql.Stmt, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type Tx struct{ /* wraps *sql.Tx */ }
func (tx *Tx) SQL() DBTX       // each package builds its own sqlc Queries: dbq.New(tx.SQL())
func (tx *Tx) Writable() bool  // CD-07
func Open(path string, opts ...Option) (*DB, error)
func WithBusyTimeout(d time.Duration) Option // CD-08; default 5 s
var ErrBusy error // "The system is busy. Please try again in a moment."
func (db *DB) Write(ctx context.Context, fn func(tx *Tx) error) error // BEGIN IMMEDIATE, all-or-nothing
func (db *DB) Read(ctx context.Context, fn func(tx *Tx) error) error

// ───────── internal/platform/clock  [Foundation] ─────────
type Clock interface{ Now() time.Time }

// ───────── internal/kernel/doctype  [DOC] (CD-02) ─────────
type State string  // draft pending_approval approved closed rejected cancelled ("" for masters)
type Action string // save submit withdraw approve reject revise close cancel amend delete
type FieldError struct{ Field string; Line int; Message string } // Line 0 = header
type FieldType int // Text Int Check Select Link Money Qty Rate Percent Factor Date Timestamp
type Field struct {
	Name, Label, Column string; Type FieldType; Link string // target DocType for Link
	Required, Filterable, Sortable, Searchable, Importable, OnLine bool
	ListPriority, MaxLen int; Options []string; Hint, Step string // CD-42
	ReadOnly bool // CD-79: computed fields; client values rejected
}
type SortKey struct{ Field string; Desc bool }
type LinkSpec struct{ Field, Target string; OnLine bool }
type RollUp struct{ TargetDocType, TargetField, SourceField, JoinField string }
type Hooks interface {
	Validate(ctx context.Context, tx *db.Tx, id int64) []FieldError
	BeforeSubmit(ctx context.Context, tx *db.Tx, id int64) error
	ApplyEffects(ctx context.Context, tx *db.Tx, id int64) error
	ReverseEffects(ctx context.Context, tx *db.Tx, id int64) error
	Amount(ctx context.Context, tx *db.Tx, id int64) (types.Money, error)
	Date(ctx context.Context, tx *db.Tx, id int64) (types.Date, error)
}
type MasterInfo struct{ Code, Name string; Disabled bool; Blocked, Warning string } // CD-21
type MasterLookup interface {
	Lookup(ctx context.Context, tx *db.Tx, id int64) (MasterInfo, bool, error)
}
type DocType struct {
	Name, Label, PluralLabel, Table, LinesTable string // CD-35
	Series string; Submittable, Approvable, Printable, Importable, HasRoundedTotal bool
	Fields []Field; SearchFields []string; DefaultSort []SortKey
	Links []LinkSpec; RollUps []RollUp
	OwnerField, EmployeeField string; ProjectFields, SiteFields []string // CD-09 (line columns as "lines.col")
	DefaultRights map[string]map[string]string // role → right → scope ("own" | "team" | "all"), CD-09/10/11
	Hooks  Hooks        // nil for masters
	Master MasterLookup // non-nil only for masters (CD-21)
}
func Register(dt DocType)                  // at init; panics on duplicates or invalid specs
func Lookup(name string) (*DocType, bool)
func All() []*DocType                      // sorted by Name
type Info struct { // CD-05
	DocType, DocLabel string; ID int64; State State; Number string
	Display string // "PO/26-27/00001" or "Draft 42"
	Party string // CD-55
	CreatedBy types.UserID; EmployeeID int64; ProjectIDs, SiteIDs []int64
}
var ErrNotFound error // CD-54: the one not-found sentinel; auth.ErrNotFound is the same value
// Action* and State* constants exist for every value above.
type HTTPError interface{ error; Status() int; FieldErrors() []FieldError } // CD-44: 403 404 409 422 503
type InfoSource interface {
	Info(ctx context.Context, tx *db.Tx, doctype string, id int64) (Info, error) // ErrNotFound if missing
}

// ───────── internal/kernel/audit  [DOC] (CD-03, CD-20) ─────────
type Ref struct{ DocType string; DocID int64; Number, Action string } // CD-69
type Change struct{ Field, Before, After string; LineNo int; LineID int64; Op string } // Op: set add remove
type Entry struct{ DocType string; DocID int64; Action string; From, To string; Reason string; Changes []Change; By types.UserID; At time.Time; CausedBy *Ref }
type Writer interface{ Append(ctx context.Context, tx *db.Tx, e Entry) error }
var Default Writer
func Append(ctx context.Context, tx *db.Tx, e Entry) error // = Default.Append

// ───────── internal/kernel/auth  [AUTH] ─────────
type UserID = types.UserID
type Right string // read create write submit cancel amend delete approve print export import
type Scope string // own team all (CD-11)
type Principal struct{ ID UserID; Name string; Roles []string }
type RecordRef struct{ DocType string; ID int64; CreatedBy UserID; EmployeeID int64; ProjectIDs, SiteIDs []int64 }
type Predicate struct{ SQL string; Args []any } // CD-16 naming; "" = no filter
type UserInfo struct{ ID UserID; Email, Name string; Enabled bool }
var ErrForbidden, ErrNotFound error // 403, 404; 403 text per 00-fixtures.md
type Service interface {
	Can(ctx context.Context, p Principal, doctype string, r Right, rec *RecordRef) error
	Scope(ctx context.Context, p Principal, doctype string, r Right) (Predicate, error)
	CheckValues(ctx context.Context, p Principal, doctype string, values map[string][]int64) error // proposed link ids vs restrictions (CD-12)
	CheckLink(ctx context.Context, p Principal, target string, id int64) error                     // linked record visible?
	UsersWithRight(ctx context.Context, doctype string, r Right, rec *RecordRef) ([]UserID, error)
	HasRole(ctx context.Context, u UserID, role string) (bool, error)
	User(ctx context.Context, tx *db.Tx, u UserID) (UserInfo, bool, error)
	DisableUser(ctx context.Context, tx *db.Tx, u UserID, reason string) error
	EnableUser(ctx context.Context, tx *db.Tx, u UserID) error
}
type Directory interface { // CD-13: implemented by MST, wired by internal/app
	EmployeeForUser(ctx context.Context, tx *db.Tx, u UserID) (employeeID int64, ok bool, err error)
	UserForEmployee(ctx context.Context, tx *db.Tx, employeeID int64) (UserID, bool, error)
	ManagerChain(ctx context.Context, tx *db.Tx, employeeID int64) ([]int64, error) // direct manager first
	DirectReports(ctx context.Context, tx *db.Tx, employeeID int64) ([]int64, error)  // active only
	ProjectCodes(ctx context.Context, tx *db.Tx, ids []int64) (map[int64]string, error)
	SiteCodes(ctx context.Context, tx *db.Tx, ids []int64) (map[int64]string, error)
}
func WithPrincipal(ctx context.Context, p Principal) context.Context
func PrincipalFrom(ctx context.Context) (Principal, bool)

// ───────── internal/kernel/naming  [NAM] (CD-28) ─────────
type Service interface {
	Assign(ctx context.Context, tx *db.Tx, doctype string, docID int64, d types.Date) (string, error)
	AssignAmendment(ctx context.Context, tx *db.Tx, doctype string, docID int64, root string, n int) (string, error)
	Preview(ctx context.Context, doctype string, d types.Date) (string, error)
	ValidateTemplate(tpl string) error
}
type SeriesInfo struct{ DocType, Label, Template string; Counters map[string]int64 } // rendered prefix → last number
type Admin interface {
	List(ctx context.Context) ([]SeriesInfo, error)
	SetTemplate(ctx context.Context, p auth.Principal, doctype, tpl string) error
	SetCounter(ctx context.Context, p auth.Principal, doctype, prefix string, last int64) error
	EnsureSeries(ctx context.Context, doctype, label, defaultTpl string) error
	PreviewTemplate(ctx context.Context, tpl string, d types.Date) (string, error) // CD-57
}
func Label(number string, id int64) string // "Draft 42" when number == ""
func ValidateGSTDocNumber(s string) error
func GuardNumberField(stored, supplied *string) error
func CheckAmendmentNo(root string, k int, docLabel string) error

// ───────── internal/kernel/doc  [DOC] ─────────
// Re-exports every doctype name as an alias (State, Action, DocType, Register, …).
type ActionInput struct{ Reason string; Version int; Values map[string]string; Lines []map[string]string }
type Result struct{ ID int64; State State; Number string; Version int; Warnings []string } // CD-68
type Reference struct{ DocType, Field string; ID int64; Display string; State State } // CD-22
type Record struct{ Info doctype.Info; Version int; Values map[string]string; Lines []map[string]string } // CD-43
type Engine interface {
	Do(ctx context.Context, p auth.Principal, doctype string, id int64, a Action, in ActionInput) (Result, error)
	IsReferenced(ctx context.Context, tx *db.Tx, table string, id int64) (firstRef string, ok bool, err error)
	References(ctx context.Context, tx *db.Tx, table string, id int64, limit int) ([]Reference, error)
	Load(ctx context.Context, tx *db.Tx, doctype string, id int64) (Record, error) // CD-43
	doctype.InfoSource
}

// ───────── internal/kernel/approval  [APR] ─────────
type Requirement struct{ Needed bool; RuleID int64; ApproverRoles []string; ManagerUser auth.UserID; Amount types.Money; Requester auth.UserID } // CD-24
type Service interface {
	Evaluate(ctx context.Context, tx *db.Tx, doctype string, id int64, amount types.Money, requester auth.Principal) (Requirement, error)
	Open(ctx context.Context, tx *db.Tx, doctype string, id int64, req Requirement) error // records "submitted"
	CheckApprover(ctx context.Context, tx *db.Tx, p auth.Principal, doctype string, id int64) error
	Record(ctx context.Context, tx *db.Tx, doctype string, id int64, action doctype.Action, p auth.Principal, reason string) error
	Inbox(ctx context.Context, p auth.Principal, page types.Page) (types.Result[InboxItem], error)
}
// DOC ↔ APR call protocol (CD-23), all inside DOC's one db.Write:
//   submit:   Evaluate → Open (always) → if !Needed: continue as approve WITHOUT CheckApprover, Record("approve")
//   approve:  CheckApprover BEFORE DOC's from-state guard → re-validate → BeforeSubmit → naming.Assign → ApplyEffects → Record("approve")
//   reject:   CheckApprover BEFORE DOC's from-state guard → reason check → Record("reject", reason)
//   revise:   Record("revise")
//   withdraw: DOC state guard and `submit` right first → Record("withdraw")
//   delete (draft): no APR call

// ───────── internal/kernel/files  [FILE] (CD-29) ─────────
type Meta struct{ ID int64; Name, MIME string; Size int64; SHA256 string; UploadedBy auth.UserID; UploadedAt time.Time }
type Spooled struct{ /* temp file on disk, hashed and type-checked outside the transaction */ }
type Service interface {
	Attach(ctx context.Context, tx *db.Tx, p auth.Principal, parent auth.RecordRef, name string, f *Spooled) (AttachmentID int64, err error)
	Remove(ctx context.Context, tx *db.Tx, p auth.Principal, id int64) error
	Open(ctx context.Context, p auth.Principal, id int64) (io.ReadCloser, Meta, error)
	List(ctx context.Context, p auth.Principal, parent auth.RecordRef) ([]Meta, error)
	CopyAll(ctx context.Context, tx *db.Tx, from, to auth.RecordRef) error // copies uploaded by to.CreatedBy
	DeleteAll(ctx context.Context, tx *db.Tx, parent auth.RecordRef) error
}

// ───────── internal/kernel/print  [FILE] ─────────
func Register(doctype string, view func(ctx context.Context, tx *db.Tx, id int64) (templ.Component, error))
func InWordsINR(m types.Money) (string, error)
type LetterheadInfo struct{ LegalName, LogoURL, BranchName string; Address []string; StateName, StateCode, GSTIN string } // CD-58
type Letterhead interface { // CD-30: implemented by masters/company
	ForBranch(ctx context.Context, tx *db.Tx, branchID int64) (LetterheadInfo, error)
}

// ───────── internal/kernel/gstcalc  [NUM] (CD-31) ─────────
func LineAmount(q types.Qty, r types.Rate) (types.Money, error)
func SplitGST(taxable types.Money, rate types.Percent, interState bool) (cgst, sgst, igst types.Money)
func IsInterState(supplierState, receiverState string) bool
func Totals(lines []Line, rounded bool) DocTotals // Line carries InterState
func StateName(code string) (string, bool)

// ───────── internal/kernel/list  [NUM] ─────────
type Page = types.Page
type Result[T any] = types.Result[T]
type Query struct{ Filters []Filter; Search string; Sort []doctype.SortKey; Page, PageSize int }
func Build(dt *doctype.DocType, q Query, scope auth.Predicate) (sql string, args []any, err error)

// ───────── internal/kernel/csvio  [NUM] (CD-32) ─────────
type RowFunc func(ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string) (id int64, errs []doctype.FieldError, err error)
func RegisterImporter(doctype string, fn RowFunc)

// ───────── internal/masters/gstin  [MST] ─────────
func ValidateGSTIN(s string) error
func StateCode(gstin string) string
func PANFromGSTIN(gstin string) string
```

Exact field lists, error values and helper types are settled by each work package's plan, within these signatures.

## 6. Screens (UI work package)

**Front-end rules (fixed by the product owner):**
- **Mobile-first and responsive.** Every screen is designed for a 360 px phone first, then enhanced for tablet and desktop.
- **Light mode only.** No dark theme, no theme switch.
- **No horizontal scrolling anywhere,** at any width from 320 px up, including lists, line items, the rights matrix and admin screens. Lists become stacked cards on narrow screens. An automated browser check enforces this on every screen (§10).
- **Complex forms are wizards with a clear progress bar.** Each step saves the draft, so nothing is lost. The last step is a review page before submit.
- The full design system and front-end spec is `docs/specs/2026-10-24-design-system.md`. It overrides anything in this section that it contradicts.

Kernel screens, all server-rendered with templ and htmx:
- Sign in, change password, first-admin setup (`/setup`), my sessions.
- Generic list screen per DocType: filters, search, sort, pagination, export, import (masters).
- Generic form screen per DocType: header fields, line grid (add, remove, reorder, keyboard entry), link pickers with search, action buttons shown by state and right, field errors inline, version-conflict message.
- Document side panel: state badge, approval history, audit history, attachments.
- Approvals: "Waiting for my approval" with count in the navigation; "My requests".
- Admin: users, roles and rights matrix, restrictions, approval rules, numbering settings, stuck approvals.
- Print view per printable DocType (A4).

Copy rules: plain English; buttons name the action ("Submit for approval", "Approve", "Reject"); errors say what went wrong and how to fix it.

## 7. Security and operations

- Sessions, CSRF, lockout and password rules as in `01-auth.md`. All forms POST; every response sets `X-Content-Type-Options: nosniff`, a strict `Content-Security-Policy`, `Referrer-Policy: same-origin`; cookies are `HttpOnly`, `Secure` (behind TLS), `SameSite=Lax`.
- Deployment: one binary plus `<data_dir>/app.db` and `<data_dir>/files/`. TLS by a reverse proxy (for example Caddy, Apache-2.0) or the binary's own `autocert` later.
- Backups: Litestream streams `app.db`; `files/` is synced to the same bucket; restore is tested in CI with a script.
- Scheduled jobs inside the binary: file GC hourly, expired-session sweep hourly.
- Logs: `log/slog` JSON to stdout.

## 8. Testing strategy

- Every test case ID in `docs/tests/core/` maps to one Go test function named in its **Go test** line. A CI script (`go run ./scripts/checktestids`) fails if any P1 case has no matching test function.
- Levels: unit (pure functions), integration (real SQLite file in a temp dir via `testkit`), e2e (chromedp, build tag `e2e`).
- Fuzz tests for NUM parsing, GST split, the lifecycle invariants (`FuzzDoc_LifecycleInvariants`) and naming templates.
- Each test that crosses areas uses the canonical seed. Tests never depend on the wall clock; they use `clock.Fixed`.
- Commands: `make test` (unit + integration), `make e2e`, `make lint`, `make build`.

## 9. Parallel development model

Work is split into work packages (WPs) that own disjoint directories and migration ranges. Agents working in parallel never edit the same files.

| Wave | WP | Owns | Depends on | Plan |
|---|---|---|---|---|
| 0 | Foundation | repo, `cmd/`, `internal/platform/`, `internal/testkit/`, all §5 contract stubs and fakes, Makefile, CI, test-ID checker | none | `plans/2026-10-24-wp0-foundation.md` |
| 1 | NUM | `types/`, `gstcalc/`, `list/`, `csvio/` | 0 | `…-wp1-num.md` |
| 1 | AUTH | `kernel/auth/` | 0 | `…-wp1-auth.md` |
| 1 | NAM | `kernel/naming/` | 0 | `…-wp1-naming.md` |
| 1 | DOC | `kernel/doctype/`, `kernel/doc/`, `kernel/audit/`, `fixture/` | 0 (fakes for AUTH, NAM, APR, FILE) | `…-wp1-doc.md` |
| 1 | APR | `kernel/approval/` | 0 (fakes for AUTH, DOC) | `…-wp1-approval.md` |
| 1 | FILE | `kernel/files/`, `kernel/print/` | 0 | `…-wp1-files-print.md` |
| 1 | UI | `internal/ui/` | 0 | `…-wp1-ui.md` |
| 2 | MST | `internal/masters/` | 1 | `…-wp2-masters.md` |
| 3 | Integration | `internal/app/`, wiring in `cmd/app`, replacing fakes, `e2e/` (every `TestE2E_*`), release build | 1, 2 | `…-wp3-integration.md` |

Rules for parallel agents:
1. Each agent works in its own git worktree on branch `wp/<name>` and touches only the directories it owns.
2. Contract changes (§5) are not made inside a WP. The agent stops, records the needed change in `docs/specs/contract-changes.md`, and the lead updates §5 and the fakes first.
3. Each Wave 1/2 WP is done when every P1 case that does not need another WP's real implementation passes, lint is clean, and `go run ./scripts/checktestids <AREA> --exclude-level e2e` reports nothing missing. Cases the plan marks "after DOC/NAM merge" or "Wave 3", and every e2e case, are WP3's entry and exit criteria (CD-40).
4. Merge order within a wave doesn't matter, except that NUM's `internal/types` (NUM Tasks 1–5) merges first (CD-53); each merge must keep `make test` green on main.
5. The current round of decisions is in `docs/specs/contract-changes.md`; §5 above already includes them.

## 10. Verification of the whole core

1. `make test` passes with every P1 case in `docs/tests/core/` implemented (checked by `go run ./scripts/checktestids`).
2. `make e2e` runs the fixture lifecycle in a browser: sign in as Priya, create `fixture.Order` for ₹94,164.00, submit; sign in as Ravi, approve; number `PO/26-27/00001` appears; create and approve a `fixture.Receipt` against it; cancel blocked with the linked-document message; cancel the receipt, then the order; amend to `PO/26-27/00001-1` on approval.
3. A fresh install: `app migrate`, `app admin create`, import masters from the sample CSVs in `testdata/masters/`, all through the binary only.
4. A restore drill from a Litestream replica gives an identical database checksum.
5. The overflow check loads every screen at 320, 360, 390, 768, 1024 and 1440 px wide and finds no horizontal scroll; the accessibility check finds no WCAG 2.2 AA contrast or target-size failures.

## 11. Later (not in core v1)

Reopen closed documents; edit approved documents; multi-level approval; email notifications; server-side PDF; self-service password reset; CSV update/upsert and document import; user-defined print templates; HRMS; Zoho Books sync (first module after Expenses).
