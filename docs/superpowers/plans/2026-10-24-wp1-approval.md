# WP1-APR Approvals Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. Run it under `docs/superpowers/plans/2026-10-25-execution-protocol.md` (roles, risk tier per task, review cadence; CD-96).

**Goal:** Fill `internal/kernel/approval` (rules, matching, eligibility, requests, append-only history, inbox, "My requests", inbox and rule-admin screens) so every APR test case in `docs/tests/core/04-approvals.md` passes, and DOC can plug it in behind the frozen `approval.Service` contract.

**Architecture:** One concrete type, `*approval.Approvals`, implements the §5 `approval.Service` plus admin and read-model methods. It owns three tables (rules, requests, history) in migrations 00400–00499 and reads everything else through §5 contracts passed in at construction: `auth.Service` (rights, roles, user names), `doctype.InfoSource` (document facts: creator, employee, projects, state, display label; CD-05), the `doctype` registry (labels and flags; CD-02), `auth.Directory` (employee ↔ user and manager chain, implemented by MST in Wave 2; CD-13) and an `audit.Writer` (CD-03). The party name for inbox cards comes from `doctype.Info.Party` (CD-55). The only APR-declared dependency interface is an optional one the contracts don't cover: `RoleDirectory` (role exists/active). Wave 1 tests drive the service through `approvaltest`, a harness on the shared fakes (`authfake` seeded with the fixture rights, `testkit.Directory` = `authfake.NewDirectory()`; CD-56) whose reference driver runs the §5 DOC ↔ APR call protocol (CD-23) in the CD-52 check order inside real `db.Write` transactions on a real SQLite file. After DOC and NAM merge, a second driver runs the same suite through the real `doc.Engine`.

**Tech Stack:** Go 1.27.3, `modernc.org/sqlite` v1.61.0, `sqlc` v1.31.1, `pressly/goose/v3` v3.29.0, `go-chi/chi/v5` v5.5.0, `a-h/templ` v0.3.1070, htmx 2.0.11, `google/go-cmp` v0.7.0, `golangci-lint` v2.15.0.

**Spec:** `docs/specs/2026-10-24-core-platform-spec.md` (§2 stack, §3.1 R1/R2/R4/R5, §3.2 state machine incl. `withdraw`, §4 layout, §5 contracts incl. the DOC ↔ APR call protocol, §9 parallel rules). Contract decisions: `docs/specs/contract-changes.md` (Rounds 1–3, CD-74 and Round 4's CD-86/CD-92; this plan applies CD-01, 02, 03, 05, 06, 11, 13, 23, 24, 26, 33, 40, 41, 44, 48, 49, 51, 52, 53, 54, 55, 56, 66, 72, 74, 86, 92). Requirements: `docs/tests/core/04-approvals.md`, fixtures `docs/tests/core/00-fixtures.md`. The spec overrides the test files where they differ.

## Global Constraints

- Module `github.com/vardhan/internaltools`; Go 1.27.3. Open-source only; latest stable versions; prefer well-maintained existing Go libraries over our own code.
- Versions: `modernc.org/sqlite` v1.61.0, `sqlc` v1.31.1, `pressly/goose/v3` v3.29.0, `go-chi/chi/v5` v5.5.0, `a-h/templ` v0.3.1070, htmx 2.0.11, `google/go-cmp` v0.7.0, `golangci-lint` v2.15.0.
- APR owns only `internal/kernel/approval/**` and `migrations/00400_*.sql`–`migrations/00499_*.sql`. Work in a git worktree on branch `wp/approval`.
- Contract changes (§5) are not made inside this WP: stop, record the change in `docs/specs/contract-changes.md`, wait for the lead.
- Imports from `approval` (§4.2 rule 3, CD-04): `auth`, `audit`, `doctype`, `types`, `platform/db`, `platform/clock`. Never `doc`, `list`, `naming`, `files` or `masters`. Paging uses `types.Page`/`types.Result[T]` (CD-01); user ids are `types.UserID` (`auth.UserID` is an alias).
- Every write goes through `db.Write` (`BEGIN IMMEDIATE`); modules never open their own transactions. Typed SQL: `internal/kernel/approval/sqlc.yaml`, queries in `internal/kernel/approval/queries/`, generated code in `internal/kernel/approval/dbq/`, built with `dbq.New(tx.SQL())`, where `tx.SQL()` returns `db.DBTX` (CD-06).
- Display formatting uses NUM's contracts (CD-33): `types.FormatINR` (`₹6,00,000.00`) and `types.FormatIST` (`24-10-2026 14:45`, the only timestamp format). APR has no private money or time display helper. NUM's `internal/types` merges to main first (Wave 1a, CD-53), so these are real from Task 1 on and no formatter-dependent case is deferred.
- Errors: every APR error that reaches a handler implements `doctype.HTTPError` (`Status()`, `FieldErrors()`; CD-44). Not-found is the one sentinel `doctype.ErrNotFound` (`auth.ErrNotFound` is the same value; CD-54). Actions and states use the `doctype.Action*` / `doctype.State*` constants (CD-54).
- Money is `INTEGER` paise. Timestamps are `TEXT` fixed-width UTC `2026-10-24T05:00:00.000Z`. Every business table has `version INTEGER NOT NULL DEFAULT 1`, `created_at`, `created_by`, `updated_at`, `updated_by`.
- Pending, rejected and revised documents have no number and show `Draft {id}` (R1). Numbers are assigned only inside the transaction that first enters `approved`.
- Approval is needed when amount ≥ from amount; the highest matching from amount wins; approvers of higher rules for the same type may also approve; no self-approval by default; no Administrator bypass; single level.
- Reject reason: trimmed, 1–1,000 characters. Withdraw (`pending_approval → draft`) closes the open request with outcome "withdrawn" (R2).
- Tests never use the wall clock (fixed clock 2026-10-24T05:00:00.000Z). Every test uses the canonical fixtures (R5). Go test names are exactly the **Go test** names in `04-approvals.md`.
- Screens: mobile-first (360 px first), light mode only, no horizontal scrolling from 320 px up, lists become stacked cards on narrow screens. Plain-English copy; buttons name the action.
- Done (Wave 1, CD-40 / §9 rule 3) = every P1 case that doesn't need another WP's real implementation passes, `make lint` is clean, and `go run ./scripts/checktestids APR --exclude-level e2e` reports nothing missing. The 12 P1 cases of Task 12 (need the real `doc.Engine` and NAM, "after DOC/NAM merge") and APR-097 (e2e, Go test `TestE2E_ApprovalWithdraw`, owned by WP3; CD-72, CD-74) are WP3's entry and exit criteria; their Go functions still exist in Wave 1 (Task 12 writes them, skipping under the reference driver) so the checker finds them.

## Review Focus

1. **Reject reasons in Hindi, Marathi or with emoji and line breaks**: the 1,000 limit counts characters, not bytes, and every Unicode space (including tabs and newlines) is trimmed. Test `TestApprovalReject_ReasonCountsCharactersNotBytes` in Task 7.
2. **Role names typed with stray spaces** ("Purchase Manager ") in the rule form: stored trimmed, so `HasRole` matches and documents don't get stuck. Test `TestApprovalRule_TrimsRoleNames` in Task 3.
3. **Two Administrators editing the same rule at once**: the second save is refused with "This rule was changed by someone else. Reload and try again." and doesn't overwrite. Test `TestApprovalRule_StaleEditRefused` in Task 3.
4. **An open request whose document can no longer be read** (data drift, deleted fixture row): the inbox skips that row and still shows the others. It doesn't fail the whole page. Test `TestApprovalInbox_SkipsUnresolvableDocument` in Task 8.
5. **User-typed text (reject reasons, party names, user names) on the inbox and "My requests" screens** is HTML-escaped. Test `TestWeb_InboxEscapesUserText` in Task 11.

---

## Before you start: decisions this plan fixes

### Persona and name mapping (R5)

`04-approvals.md` already uses the canonical users. Doc names follow R1: `Draft {id}` until approved; APR never builds them itself, it shows `doctype.Info.Display` (CD-05). Doc type names are the fixture DocType names. APR reads labels and flags from `*doctype.DocType` (CD-02): `Label`, `Approvable`, `Master != nil` (a master), `EmployeeField != ""` (employee-based):

| DocType name | Label | Approvable | Employee-based |
|---|---|---|---|
| `fixture.Order` | Purchase Order | yes | no |
| `fixture.Receipt` | Goods Receipt Note | yes | no |
| `fixture.Claim` (CD-25) | Expense Claim | yes | yes (`EmployeeField` set; claimant = `Info.EmployeeID`) |
| `Item` | Item | no (master) | no |

Seed rules (00-fixtures): **R1** `fixture.Order` from ₹50,000.00 → role Purchase Manager; **R2** `fixture.Order` from ₹5,00,000.00 → role Director; **E1** `fixture.Claim` from ₹0.00 → reporting manager, fallback Director. All with self-approval off.

The harness's `auth.Service` is the shared `authfake` (CD-56), seeded with `Grant(role, doctype, right, scope)` for every row of `testkit.Fixtures.Rights` (the Administrator row as `Grant("Administrator", "*", right, "all")` for every right, using `authfake`'s wildcard DocType, CD-86) and `AddRole(user, role)` for every fixture user, so it carries the seeded rights matrix from `00-fixtures.md` exactly, with scopes `own`, `team`, `all` (CD-11). No assumptions remain:

| Role | Rights |
|---|---|
| Administrator | every right on every DocType (all), seeded with the wildcard DocType `"*"` (CD-86) |
| Purchase Manager | `fixture.Order`: read, approve, cancel, amend, print, export (all) |
| Purchase User | `fixture.Order`: read, create, write, submit, print, export (all); delete (own) |
| Stores | `fixture.Receipt`: read, create, write, submit, print (all); `fixture.Order`: read (all) |
| Employee | `fixture.Claim`: read, create, write, submit, print (own); read, approve (team) |
| Director | `fixture.Order`, `fixture.Claim`: read, approve, print, export (all) |
| Masters Manager | every master: read, create, write, delete, export, import (all) |

`team` = own, plus records whose employee field's employee reports **directly** to the user's employee (active employees only), worked out through `auth.Directory.EmployeeForUser` and `DirectReports` (which skips employees who have left, CD-54). So Priya (EMP-004) reads and may approve Deepak's (EMP-007) claims; Ravi (EMP-003) sees Priya's and Suresh's claims but not Deepak's.

Anything else a case needs (e.g. Purchase Manager `approve` on GRN in APR-052/053, Director `cancel` in APR-086) is added in that test with `Auth.Grant`, and extra roles with `Auth.AddRole`, as the fixtures file says.

Restrictions: Priya → project `PRJ-METRO` (`Auth.Restrict(user, projects, sites)`, CD-56); Suresh → sites (not used by APR). Purchase Orders default to project `PRJ-METRO`.

### Call protocol (§5, CD-23) and order of checks (CD-52)

The protocol is part of §5; CD-52 fixes the order of checks for approve and reject. The reference driver implements exactly it:

| DOC action | APR calls, in this order, inside DOC's one `db.Write` |
|---|---|
| submit | `req, err := Evaluate(...)` (error aborts) → `Open(..., req)` **always** (records "submitted") → if `!req.Needed`, continue as approve **without** `CheckApprover`, calling `Record(..., doctype.ActionApprove, p, "")` |
| approve | DOC's visibility check (`Can(read)`; hidden → 404, `doctype.ErrNotFound`) → `CheckApprover(...)` (owns the approve-right and eligibility messages) **before** DOC's from-state guard → re-validate, BeforeSubmit, `naming.Assign`, ApplyEffects → `Record(..., doctype.ActionApprove, p, comment)` |
| reject | visibility (404) → `CheckApprover(...)` → DOC's from-state guard → reason check → `Record(..., doctype.ActionReject, p, reason)` |
| revise | `Record(..., doctype.ActionRevise, p, "")` |
| withdraw | DOC state guard and `submit` right first (DOC owns the APR-094 messages) → `Record(..., doctype.ActionWithdraw, p, "")` |
| delete (draft) | no APR call |

DOC does **not** run its generic `Can(approve)` for approve and reject (CD-52); `CheckApprover` is the only approve-right check, so a user who can see the record but may not approve it gets APR's "Only …" message, and a user who can't see it gets a 404 before APR is called.

`Record` takes `doctype.Action`; APR accepts `doctype.ActionApprove`, `ActionReject`, `ActionRevise`, `ActionWithdraw` (CD-54) and returns `ErrUnknownAction` for any other value. APR declares no action constants of its own.

### Messages (exact copy; built in `errors.go`)

| Error kind | `Status()` | Message |
|---|---|---|
| `ErrNotAdmin` | 403 | You don't have permission to change approval rules. Ask an Administrator. (CD-66: "Administrator", never "Admin") |
| `ErrNotAdmin` (stuck list) | 403 | You don't have permission to see stuck approvals. Ask an Administrator. (CD-152) |
| `ErrInvalidRule` | 422 | From amount must be ₹0.00 or more. · Choose an approver role for this rule. · Choose a fallback role for claims whose employee has no reporting manager. · Reporting manager approval is only available for {employee-based labels joined " or "}. Choose a role. · {Label} is a master and can't need approval. Choose a document type. · Choose a document type. (unknown name) · Role {r} is disabled. Choose an active role. |
| `ErrDuplicateRule` | 422 | A {Label} rule from {₹amount} already exists. Edit that rule instead. |
| `ErrRuleInUse` | 409 | This rule has been used by {n} documents. Disable it instead of deleting. (`1 document` when n = 1) |
| `ErrRuleConflict` | 409 | This rule was changed by someone else. Reload and try again. |
| `ErrNoApprover` | 409 | No one else can approve this {Label}. Give the {matched role} role to another active user, then submit again. |
| `ErrNotEligible` | 403 | Only a user with the {roles} role can approve {Title} ({₹amount}). · Only {Manager name} or a user with the {roles} role can approve {Title} ({₹amount}). (Reaches only users who can see the document: DOC answers 404 first, CD-52.) |
| `ErrSelfApproval` | 403 | You can't approve your own {Label}. Ask another {roles} to approve it. |
| `ErrNotPending` | 409 | CD-26: {Title} is no longer waiting for approval. It was {approved\|rejected\|withdrawn} by {Name}. (when a person's decision, i.e. the latest request's outcome, produced the current state) · otherwise {Title} is not waiting for approval. It is {State label}. |
| `ErrReasonRequired` | 422 | Enter a reason for rejecting. |
| `ErrReasonTooLong` | 422 | Reason must be 1,000 characters or fewer. |

Every `*UserError` implements `doctype.HTTPError` (CD-44) with the status above; `FieldErrors()` returns one `doctype.FieldError` on the rule-form field (`doctype`, `from_amount`, `role`, `fallback_role`) for `ErrInvalidRule`/`ErrDuplicateRule`, on `reason` for the two reason kinds, and nil otherwise. A record hidden from the user has no APR message: it is a 404 (`doctype.ErrNotFound`, CD-54) from DOC's visibility check (CD-52); the previous draft's `ErrNoAccess` ("You don't have access to {Title}.") is deleted.

`{Title}` is `doctype.Info.Display` and `{Label}` is `doctype.Info.DocLabel`. `{roles}` joins with " or " for two ("Purchase Manager or Director") and ", … or " for three or more. State labels: draft → Draft, pending_approval → Waiting for approval, approved → Approved, rejected → Rejected, cancelled → Cancelled, closed → Closed. `{₹amount}` is `types.FormatINR`: `₹6,00,000.00`.

---

## File structure

```
migrations/00400_approval_rules.sql        rules table + partial unique index (doctype, from_amount) WHERE enabled
migrations/00401_approval_requests.sql     requests table + one-open-request partial unique index
migrations/00402_approval_history.sql      append-only history table + no-update/no-delete triggers
internal/kernel/approval/
  contract.go        (WP0 stub file, CD-92) Requirement (§5 fields only), Service, InboxItem fields, compile-time assertion
  deps.go            Deps, New, Approvals, RoleDirectory (optional)
  model.go           Rule, RuleInput, ApproverType, Request, HistoryRow, Status, RequestItem, StuckItem, history/outcome constants
  errors.go          UserError (implements doctype.HTTPError, CD-44), Err* kinds, message constructors
  format.go          joinRoles(), stateLabel(), ts()/parseTS() (storage text only; every display of money and time is types.FormatINR / types.FormatIST)
  rules.go           ValidateRule, CreateRule, UpdateRule, SetRuleEnabled, DeleteRule, Rules, ApprovableDocTypes
  match.go           match() — pure rule matching
  eligible.go        own(), eligible(), EligibleApprovers
  evaluate.go        Evaluate, Open, LatestRequest
  decide.go          CheckApprover, Record (approve, auto-approve, reject, revise, withdraw)
  history.go         History, writeHistory
  inbox.go           Inbox, MyRequests, Status, Stuck
  sqlc.yaml, queries/approval.sql, dbq/ (generated)
  approvaltest/      env.go (Env, drivers, authfake seeding), doctypes.go (test DocType descriptors + lookup),
                     info.go (doctype.InfoSource over aprtest_docs, incl. Party), clock.go, assert.go
                     (auth: authfake.New(); directory: testkit.Directory(fx) = *authfake.Directory; audit: auditfake; CD-56)
  web/               routes.go, handlers.go, inbox.templ, mine.templ, rules.templ, stuck.templ, badge.templ
  *_test.go          one file per task (named below)
```

## Execution order and parallelism

| Wave | Tasks | Run in parallel? | Why |
|---|---|---|---|
| A | 1, 2 | yes | disjoint files; Task 2's harness talks to the schema only at test runtime |
| B | 3, 4, 5 | yes | `rules.go`, `match.go`, `eligible.go` + own test files |
| C | 6 | — | needs 3 (UpdateRule in APR-025), 4, 5 |
| D | 7 | — | needs 6 |
| E | 8, 9, 10, 12a | yes | `inbox.go`; test-only files; withdraw branch of `decide.go` is written in 7, Task 10 adds tests and the read-model fields only; 12a writes the engine-only test functions (skipping under the reference driver) |
| F | 11 | — | needs 3 and 8 (`types.ParseMoney` is on main from Wave 1a, CD-53) |
| P2 | 14, 15, 16, 17 | yes after their prerequisites (listed per task) | separate test files |
| Gated (WP3 criteria, CD-40) | 12b, 18 | after DOC + NAM merge | need the real `doc.Engine` and NAM |
| Hand-off (WP3 owns) | 13 | Wave 3 | e2e, lives under `e2e/` |
| Final | 19 | — | full verification |

**NUM's `types` merges first (Wave 1a, CD-53).** NUM Tasks 1–5 (`internal/types`, every Parse/Format function) are merged to main before any other Wave 1 WP finishes. Start `wp/approval` from (or rebase it onto) a main that has them. Every message with an amount or a time is built with `types.FormatINR` / `types.FormatIST` from Task 2 on, and every case that only needs formatters (APR-010, 030, 033, 042, 045, 051, 056, 066, 096 and the inbox card) asserts its exact text in Wave 1; none is deferred. If main doesn't have them yet when Task 2 starts, stop and tell the lead (§9 rule 2); never add a private formatter.

---

## Task 1: Schema and queries

**Files:**
- Create: `migrations/00400_approval_rules.sql`, `migrations/00401_approval_requests.sql`, `migrations/00402_approval_history.sql`
- Create: `internal/kernel/approval/sqlc.yaml`, `internal/kernel/approval/queries/approval.sql`, `internal/kernel/approval/dbq/*` (generated)
- Create: `internal/kernel/approval/schema_test.go`

**Interfaces:**
- Consumes: `testkit.NewDB(t) *db.DB`, `(*db.DB).Write`, `(*db.Tx).SQL() db.DBTX` (CD-06).
- Produces: package `dbq` with `dbq.New(tx.SQL())` and the queries below (sqlc names are the `-- name:` values).

- [ ] **Step 1: Confirm versions and the contract stubs**

Run: `go list -m -versions github.com/a-h/templ github.com/go-chi/chi/v5 github.com/pressly/goose/v3 modernc.org/sqlite github.com/google/go-cmp | tr ' ' '\n' | tail -n 3` and `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 version`.
Expected: the latest listed versions equal the Tech Stack line. If any is newer, stop and tell the lead (the spec pins versions).
Run: `go build ./internal/kernel/... && go list -deps ./internal/kernel/approval | grep internaltools/internal`
Expected: build succeeds; the approval stub depends only on `types`, `platform/db`, `kernel/doctype`, `kernel/auth`, `kernel/audit` (no `list`, no `doc`). If not, or if a §5 name used below (`doctype.InfoSource`, `doctype.Info.Party`, `doctype.ErrNotFound`, `doctype.HTTPError`, `doctype.Action*`/`State*`, `auth.Directory`, `audit.Writer`, `auditfake`, `authfake.Grant` (incl. the wildcard DocType `"*"`)/`AddRole`/`RemoveRole`/`Restrict` (CD-56, CD-86), `testkit.Directory`, `testkit.Principal(t, email)`, `types.Page`) is missing from the WP0 stubs, stop and tell the lead (§9 rule 2).
Run: `go test ./internal/types/ -run 'Format(INR|IST)' -v`
Expected: PASS (NUM's `types` is on main, CD-53). A stub failure means main is not ready: stop and tell the lead.

- [ ] **Step 2: Write the failing schema test**

```go
// internal/kernel/approval/schema_test.go
package approval_test

func TestSchema_HistoryIsAppendOnlyAndOneOpenRequest(t *testing.T) {
	d := testkit.NewDB(t)
	ctx := t.Context()
	err := d.Write(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		rid, err := q.InsertRule(ctx, dbq.InsertRuleParams{DocType: "fixture.Order", FromAmount: 5000000,
			ApproverType: "role", ApproverRole: "Purchase Manager", CreatedAt: "2026-10-24T05:00:00.000Z", CreatedBy: 1})
		if err != nil { return err }
		req := dbq.InsertRequestParams{DocType: "fixture.Order", DocID: 42, RuleID: rid, Amount: 6000000,
			ApproverRoles: `["Purchase Manager","Director"]`, Requester: 4, RequesterName: "Priya Shah", Creator: 4,
			SubmittedAt: "2026-10-24T05:00:00.000Z", CreatedBy: 4}
		if _, err := q.InsertRequest(ctx, req); err != nil { return err }
		if _, err := q.InsertRequest(ctx, req); err == nil { t.Error("second open request for the same document was accepted") }
		hid, err := q.InsertHistory(ctx, dbq.InsertHistoryParams{DocType: "fixture.Order", DocID: 42, Action: "submitted",
			UserID: 4, UserName: "Priya Shah", At: "2026-10-24T05:00:00.000Z"})
		if err != nil { return err }
		_, uerr := tx.SQL().ExecContext(ctx, `UPDATE approval_history SET reason='x' WHERE id=?`, hid)
		_, derr := tx.SQL().ExecContext(ctx, `DELETE FROM approval_history WHERE id=?`, hid)
		for _, e := range []error{uerr, derr} {
			if e == nil || !strings.Contains(e.Error(), "approval history is append-only") { t.Errorf("got %v", e) }
		}
		return nil
	})
	if err != nil { t.Fatal(err) }
}
```

- [ ] **Step 3: Run it to verify it fails**

Run: `go test ./internal/kernel/approval/ -run TestSchema_ -v`
Expected: FAIL to compile (`dbq` undefined).

- [ ] **Step 4: Write the migrations**

goose format (`-- +goose Up` / `-- +goose Down`; triggers inside `-- +goose StatementBegin/End`). Columns:

- `approval_rules`: `id INTEGER PRIMARY KEY`, `doctype TEXT NOT NULL`, `from_amount INTEGER NOT NULL CHECK (from_amount >= 0)`, `approver_type TEXT NOT NULL CHECK (approver_type IN ('role','reporting_manager'))`, `approver_role TEXT NOT NULL DEFAULT ''`, `fallback_role TEXT NOT NULL DEFAULT ''`, `allow_self_approval INTEGER NOT NULL DEFAULT 0`, `enabled INTEGER NOT NULL DEFAULT 1`, plus `version`, `created_at`, `created_by`, `updated_at`, `updated_by`. `CREATE UNIQUE INDEX approval_rules_enabled_uq ON approval_rules(doctype, from_amount) WHERE enabled = 1;`
- `approval_requests`: `id`, `doctype`, `doc_id`, `rule_id INTEGER NOT NULL REFERENCES approval_rules(id) ON DELETE RESTRICT`, `amount INTEGER NOT NULL`, `approver_roles TEXT NOT NULL` (JSON array), `manager_user INTEGER` (NULL = none), `manager_name TEXT NOT NULL DEFAULT ''`, `allow_self INTEGER NOT NULL`, `requester INTEGER NOT NULL`, `requester_name TEXT NOT NULL`, `creator INTEGER NOT NULL`, `claimant_user INTEGER`, `submitted_at TEXT NOT NULL`, `outcome TEXT CHECK (outcome IN ('approved','rejected','withdrawn'))` (NULL = open), `decided_by INTEGER`, `decided_by_name TEXT NOT NULL DEFAULT ''`, `decided_at TEXT`, `reason TEXT NOT NULL DEFAULT ''`, plus the five convention columns. `CREATE UNIQUE INDEX approval_requests_open_uq ON approval_requests(doctype, doc_id) WHERE outcome IS NULL;` and an index on `(outcome, submitted_at, id)`.
- `approval_history`: `id`, `doctype`, `doc_id`, `request_id INTEGER REFERENCES approval_requests(id)`, `action TEXT NOT NULL CHECK (action IN ('submitted','approved','approved_no_rule','rejected','revised','withdrawn'))`, `user_id INTEGER NOT NULL`, `user_name TEXT NOT NULL`, `at TEXT NOT NULL`, `amount INTEGER`, `rule_id INTEGER`, `reason TEXT NOT NULL DEFAULT ''`, `self_approved INTEGER NOT NULL DEFAULT 0`. Triggers `BEFORE UPDATE` and `BEFORE DELETE` doing `SELECT RAISE(ABORT, 'approval history is append-only');`. History is not a business table, so it has no `version`/`updated_*` columns.

No foreign keys to AUTH or DOC tables (§4.2 rule 2).

- [ ] **Step 5: Write `sqlc.yaml` and `queries/approval.sql`, then generate**

`sqlc.yaml`: `version: "2"`, engine `sqlite`, `schema: ../../../migrations`, `queries: queries`, `gen.go: {package: dbq, out: dbq}`. Queries (all later tasks use only these; adding one later means regenerating, so add here):

| name | kind | SQL shape |
|---|---|---|
| InsertRule | one (id) | INSERT … RETURNING id |
| UpdateRule | execrows | UPDATE … SET …, version = version + 1 WHERE id = ? AND version = ? |
| SetRuleEnabled | execrows | UPDATE … SET enabled = ?, version = version + 1, updated_* WHERE id = ? |
| DeleteRule | exec | DELETE FROM approval_rules WHERE id = ? |
| GetRule | one | SELECT * WHERE id = ? |
| ListRules | many | ORDER BY doctype, from_amount, id |
| ListEnabledRulesForDocType | many | WHERE doctype = ? AND enabled = 1 ORDER BY from_amount |
| FindEnabledDuplicate | one | WHERE doctype = ? AND from_amount = ? AND enabled = 1 AND id <> ? LIMIT 1 |
| CountRuleUsage | one | SELECT COUNT(DISTINCT doctype \|\| ':' \|\| doc_id) WHERE rule_id = ? |
| InsertRequest | one (id) | INSERT … RETURNING id |
| GetOpenRequest | one | WHERE doctype = ? AND doc_id = ? AND outcome IS NULL |
| GetLatestRequest | one | WHERE doctype = ? AND doc_id = ? ORDER BY id DESC LIMIT 1 |
| CloseRequest | execrows | UPDATE … SET outcome, decided_by, decided_by_name, decided_at, reason, version+1, updated_* WHERE id = ? AND outcome IS NULL |
| ListOpenRequests | many | WHERE outcome IS NULL ORDER BY submitted_at, id |
| ListLatestRequestsByRequester | many | latest request per (doctype, doc_id) WHERE requester = ? ORDER BY submitted_at DESC, id DESC |
| InsertHistory | one (id) | INSERT … RETURNING id |
| ListHistory | many | WHERE doctype = ? AND doc_id = ? ORDER BY id |
| LastHistory | one | … ORDER BY id DESC LIMIT 1 |

Run: `make generate`
Expected: `internal/kernel/approval/dbq/` exists; `go build ./internal/kernel/approval/...` succeeds.

- [ ] **Step 6: Run the test to verify it passes**

Run: `go test ./internal/kernel/approval/ -run TestSchema_ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add migrations/0040*.sql internal/kernel/approval/sqlc.yaml internal/kernel/approval/queries internal/kernel/approval/dbq internal/kernel/approval/schema_test.go
git commit -m "feat(approval): schema, append-only history and typed queries"
```

---

## Task 2: Core types, errors, formatting and the `approvaltest` harness

**Files:**
- Modify: `internal/kernel/approval/contract.go` (the WP0 stub file, CD-92: keep every §5 signature and the §5 `Requirement` fields exactly; fill `InboxItem`)
- Create: `internal/kernel/approval/deps.go`, `model.go`, `errors.go`, `format.go`
- Create: `internal/kernel/approval/approvaltest/{env.go,auth.go,directory.go,doctypes.go,info.go,clock.go,assert.go}`
- Test: `internal/kernel/approval/format_test.go`, `internal/kernel/approval/approvaltest/env_test.go`

**Interfaces:**
- Consumes: `testkit.NewDB`, `testkit.Seed`, `testkit.Principal(t, email)` (CD-72), `testkit.Directory(fx)` (`*authfake.Directory`), `authfake.New()` with `Grant` (wildcard DocType `"*"` accepted), `AddRole`, `RemoveRole`, `Restrict`, `AddUser` (CD-56, CD-86), `auth.Service`, `auth.Directory`, `auth.UserInfo`, `doctype.DocType`, `doctype.Info` (incl. `Party`, CD-55), `doctype.InfoSource`, `doctype.ErrNotFound`, `doctype.HTTPError`, `doctype.FieldError`, `doctype.Lookup`/`All`, `audit.Writer`, `audit.Default`, `auditfake.New()`, `types.Page`, `types.Result[T]`, `types.FormatINR`, `types.FormatIST`, `clock.Clock`.
- Produces (package `approval`):

```go
// contract.go (WP0's stub file, CD-92) — §5 exactly (CD-24); no unexported fields.
type Requirement struct{ Needed bool; RuleID int64; ApproverRoles []string; ManagerUser auth.UserID; Amount types.Money; Requester auth.UserID }
type InboxItem struct {
	RequestID       int64
	DocType, Label  string // Label = Info.DocLabel
	DocID           int64
	Title, Party    string // Title = Info.Display; Party = Info.Party (CD-55; "" when none)
	Amount          types.Money
	SubmittedBy     types.UserID
	SubmittedByName string
	SubmittedAt     time.Time
}
var _ Service = (*Approvals)(nil)

// deps.go
type RoleDirectory interface { RoleStatus(ctx context.Context, name string) (exists, active bool, err error) } // optional; AUTH in Wave 3
type Deps struct {
	DB *db.DB; Clock clock.Clock; Auth auth.Service
	Info      doctype.InfoSource                         // CD-05; DOC's Engine in production
	Directory auth.Directory                             // CD-13; MST in production
	Audit     audit.Writer                               // CD-03; nil → audit.Default
	Lookup    func(name string) (*doctype.DocType, bool) // nil → doctype.Lookup
	All       func() []*doctype.DocType                  // nil → doctype.All
	Roles     RoleDirectory                              // nil → role existence not checked
}
type Approvals struct{ /* unexported */ }
func New(d Deps) *Approvals

// model.go
type ApproverType string
const ( ApproverRole ApproverType = "role"; ApproverManager ApproverType = "reporting_manager" )
const ( HistSubmitted = "submitted"; HistApproved = "approved"; HistAutoApproved = "approved_no_rule"
	HistRejected = "rejected"; HistRevised = "revised"; HistWithdrawn = "withdrawn" )
const ( OutcomeApproved = "approved"; OutcomeRejected = "rejected"; OutcomeWithdrawn = "withdrawn" )
type RuleInput struct { DocType string; FromAmount types.Money; ApproverType ApproverType; Role, FallbackRole string; AllowSelfApproval bool }
type Rule struct { ID int64; RuleInput; Enabled bool; Version int64 }
type Request struct {
	ID, RuleID int64; DocType string; DocID int64; Amount types.Money; ApproverRoles []string
	ManagerUser types.UserID; ManagerName string; AllowSelf bool
	Requester types.UserID; RequesterName string; Creator, Claimant types.UserID; SubmittedAt time.Time
	Outcome string /* "" = open */; DecidedBy types.UserID; DecidedByName string; DecidedAt time.Time; Reason string
}
type HistoryRow struct { Action string; By types.UserID; ByName string; At time.Time; Amount types.Money; RuleID int64; Reason string; SelfApproved bool }
type Status struct { Line string; Pending bool }
type RequestItem struct { InboxItem; Outcome, DecidedByName, Reason string; DecidedAt time.Time; State string }
type StuckItem struct { InboxItem; Message string }

// errors.go
var ( ErrNotAdmin, ErrInvalidRule, ErrDuplicateRule, ErrRuleInUse, ErrRuleConflict, ErrNoApprover,
	ErrNotPending, ErrNotEligible, ErrSelfApproval, ErrReasonRequired, ErrReasonTooLong,
	ErrUnknownAction, ErrNotEvaluated error ) // the last two are programming errors (500), not UserErrors
type UserError struct { Kind error; Msg string; Field string } // Field "" = no field error
func (e *UserError) Error() string                        // Msg
func (e *UserError) Unwrap() []error                      // Kind, plus auth.ErrForbidden for NotAdmin/NotEligible/SelfApproval
func (e *UserError) Status() int                          // per the Messages table (CD-44)
func (e *UserError) FieldErrors() []doctype.FieldError    // [{Field, 0, Msg}] when Field != "", else nil
var _ doctype.HTTPError = (*UserError)(nil)

// format.go (unexported, used across the package). No money or time display here: that is types.FormatINR / types.FormatIST (CD-33, CD-53).
func joinRoles(roles []string) string        // ["A"]→"A"; ["A","B"]→"A or B"; ["A","B","C"]→"A, B or C"
func stateLabel(s doctype.State) string      // per the Messages section
func ts(t time.Time) string                  // storage only: UTC "2006-01-02T15:04:05.000Z" (the TEXT column format), never shown
func parseTS(s string) (time.Time, error)
```

The local `Records`/`Record`, `Catalog`/`DocTypeInfo`, `Directory`/`Employee`/`EmployeeID`, `PartySource`, `ErrNoAccess`, the `func` audit hook, `inr()` and `istStamp()` of earlier drafts are gone: they duplicated `doctype.InfoSource`, `doctype.Info.Party` (CD-55), the `doctype` registry, `auth.Directory`, `audit.Writer`, DOC's 404 (CD-52), `types.FormatINR` and `types.FormatIST`.

- Produces (package `approvaltest`, imported by every later test file as `at`):

```go
const ( Order = "fixture.Order"; Receipt = "fixture.Receipt"; Claim = "fixture.Claim"; ItemMaster = "Item" )
const ( ProjMetro int64 = 1; ProjTechPark int64 = 2 )
func Rs(rupees int64) types.Money // rupees × 100
// doctypes.go: test descriptors (Name, Label, Table, Approvable, EmployeeField "employee_id" on Claim, Master non-nil on Item).
// Never passed to doctype.Register: internal/fixture registers the real ones, and duplicates panic.
func DocTypes() []*doctype.DocType
func Lookup(name string) (*doctype.DocType, bool) // over DocTypes(); passed as Deps.Lookup / Deps.All in the harness
type Env struct {
	DB *db.DB; A *approval.Approvals; F testkit.Fixtures
	Auth *authfake.Fake; Dir *authfake.Directory // shared fakes (CD-56); Dir = testkit.Directory(F)
	Docs *Docs; Clock *Clock
	Audit interface{ audit.Writer; Entries() []audit.Entry } // auditfake.New()
	Anil, Meena, Ravi, Priya, Suresh, Kavya, Deepak auth.Principal
	R1, R2, E1 int64
}
func New(t testing.TB) *Env // testkit DB + seed, doubles, approval.New, seed rules R1 R2 E1, harness tables
type Doc struct{ Type string; ID int64 }
type DocOpt func(*docSpec)
func Project(id int64) DocOpt        // default ProjMetro for Order; no project for Receipt and Claim
func ForEmployee(code string) DocOpt // claimant, e.g. "EMP-007"; default: the creator's employee for Claim
func Party(name string) DocOpt
func InState(state doctype.State, number string) DocOpt // create directly in a state (APR-055/057)
func (e *Env) NewDoc(t testing.TB, doctype string, id int64, amount types.Money, creator auth.Principal, opts ...DocOpt) Doc
func (e *Env) SetAmount(t testing.TB, d Doc, m types.Money)
type ActOpt func(*actSpec)
func Comment(s string) ActOpt
func FailAfterRecord(err error) ActOpt // the driver returns err after Record, so the tx rolls back
func (e *Env) Submit(p auth.Principal, d Doc) error
func (e *Env) Approve(p auth.Principal, d Doc, opts ...ActOpt) error
func (e *Env) Reject(p auth.Principal, d Doc, reason string) error
func (e *Env) Revise(p auth.Principal, d Doc) error
func (e *Env) Withdraw(p auth.Principal, d Doc) error
func (e *Env) State(t testing.TB, d Doc) doctype.State
func (e *Env) Number(t testing.TB, d Doc) string // "" when none
func (e *Env) Title(t testing.TB, d Doc) string  // Info.Display
func (e *Env) Effects(t testing.TB, d Doc) int
func (e *Env) Latest(t testing.TB, d Doc) (approval.Request, bool)
func (e *Env) History(t testing.TB, d Doc) []approval.HistoryRow
func (e *Env) InboxTitles(t testing.TB, p auth.Principal) []string // titles in inbox order
func (e *Env) AddRule(t testing.TB, in approval.RuleInput) int64  // direct insert, bypasses the admin check
func (e *Env) RemoveRules(t testing.TB, doctype string)
func (e *Env) AddUser(t testing.TB, name, email string, roles ...string) auth.Principal // Auth.AddUser + AddRole
func (e *Env) Emp(code string) int64                          // employee id from F.Employees, e.g. "EMP-007"
func (e *Env) RemoveRole(t testing.TB, u types.UserID, role string) // Auth.RemoveRole(u, role) (CD-86)
func (e *Env) Disable(t testing.TB, u types.UserID)               // Auth.DisableUser(ctx, nil, u, "test")
func (e *Env) Unlink(t testing.TB, code string)                   // the employee's user link removed on Dir (see gap check)
func WantErr(t testing.TB, err error, kind error, msg string) // errors.Is(err, kind) && err.Error() == msg
func WantStatus(t testing.TB, err error, status int)          // errors.As(err, &doctype.HTTPError) && Status() == status (CD-44)
func WantNotFound(t testing.TB, err error)                    // errors.Is(err, doctype.ErrNotFound): the CD-52 404, no APR text asserted
func MustOK(t testing.TB, err error)
func (c *Clock) Set(t time.Time)
// Tests seed extra rights and roles on the shared fake directly (CD-56):
//   e.Auth.Grant(role, doctype, right, scope), e.Auth.AddRole(u, role), e.Auth.RemoveRole(u, role), e.Auth.Restrict(u, projects, sites),
//   e.Dir.SetManager(employeeID, managerID).
```

Driver behaviour (the reference driver runs the §5 call protocol, CD-23, in the CD-52 order). Harness tables, created with `CREATE TABLE IF NOT EXISTS` through `tx.SQL()` in `New`, never in `migrations/`: `aprtest_docs(doctype, id, state, number, amount, created_by, employee_id, project_id, party)`, `aprtest_effects(doctype, id, n)`, `aprtest_counters(prefix, n)`. All state, number, effect and counter changes happen inside the same `db.Write` as the APR calls, so rollbacks are real. Stand-in numbering on entering `approved`: `PO/26-27/%05d`, `GR/26-27/%05d`, `EX/26-27/%05d` from `aprtest_counters`.

- Approve and reject in the reference driver, as DOC does (CD-52): `Info` → `Auth.Can(p, doctype, "read", ref)`; a hidden record returns that error (`doctype.ErrNotFound`, 404) and APR is not called → `CheckApprover` → from-state guard → the rest of the protocol. The driver never calls `Can(approve)` for these two actions.
- `Docs` implements `doctype.InfoSource` over `aprtest_docs` through the passed `tx`: `Info{DocType, DocLabel (from DocTypes()), ID, State, Number, Display (number, else "Draft {id}"), Party (CD-55), CreatedBy, EmployeeID, ProjectIDs ([project_id] when set)}`; a missing row returns `doctype.ErrNotFound` (CD-54). This is APR's one own contract double, kept on purpose instead of `docfake.SetInfo`: its rows live in the same SQLite file and are read through the caller's `tx`, so state, number and effect changes roll back with the `db.Write` (APR-053) and serialise under `BEGIN IMMEDIATE` (APR-072); `docfake` keeps `Info` in memory, outside the transaction.
- `Auth` is `authfake.New()` (CD-56), seeded in `New` from `testkit.Fixtures`: `AddUser` (names, enabled flags; Former Staff with `Enabled: false`), `AddRole` per fixture role, `Grant(role, doctype, right, scope)` per rights row with the Administrator row as `Grant("Administrator", "*", r, "all")` for each right (CD-86), `Restrict(Priya, [ProjMetro], nil)`. APR relies on it for: `Can` returning `auth.ErrForbidden` with no such right and `doctype.ErrNotFound` when scope (`own`, `team` through the shared directory, CD-11) or a restriction hides the record (D-AUTH-16); `UsersWithRight` skipping users added with `Enabled: false` or disabled later (CD-86), sorted by ID, so APR never filters disabled users itself; live `HasRole`; `User`; `AddRole`, `RemoveRole` (CD-86) and `DisableUser` taking effect on the next call. APR uses no `SetRoles`.
- `Dir` is `testkit.Directory(fx)`, the shared `*authfake.Directory` (CD-56): EMP-001…EMP-008 with users and managers from `00-fixtures.md`, `ManagerChain` direct manager first, `UserForEmployee` and `DirectReports` skipping employees who have left (CD-54).
- **Gap check (Step 4, before writing any harness code):** confirm that the merged `authfake` does each item in the two bullets above (CD-86 makes `RemoveRole`, the disabled-user skip in `UsersWithRight` and `Grant` with `"*"` part of WP0's fake), plus removing an employee's user link (for `Env.Unlink`, APR-048). For anything missing, stop and ask the lead to add it to `authfake` (WP0 owns it; §9 rule 2). Only if the lead declines does `approvaltest` wrap `*authfake.Fake` / `*authfake.Directory` to add that one method. No second auth or directory double is written.

- [ ] **Step 1: Write the failing tests**

```go
// internal/kernel/approval/format_test.go
package approval

func TestFormat_RolesStatesAndStorage(t *testing.T) {
	if got := joinRoles([]string{"Purchase Manager", "Director"}); got != "Purchase Manager or Director" { t.Error(got) }
	if got := joinRoles([]string{"A", "B", "C"}); got != "A, B or C" { t.Error(got) }
	if got := stateLabel(doctype.StatePendingApproval); got != "Waiting for approval" { t.Error(got) }
	if got := ts(time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)); got != "2026-10-24T05:00:00.000Z" { t.Error(got) }
	// money and IST display are NUM's (types.FormatINR, types.FormatIST), real on main (CD-53) and asserted through the messages below
	err := notEligible("Draft 42", 6000000, "", []string{"Purchase Manager", "Director"})
	if err.Error() != "Only a user with the Purchase Manager or Director role can approve Draft 42 (₹60,000.00)." { t.Error(err) }
	var h doctype.HTTPError
	if !errors.As(err, &h) || h.Status() != 403 || h.FieldErrors() != nil { t.Errorf("HTTPError %v", h) } // CD-44
}

// internal/kernel/approval/approvaltest/env_test.go
package approvaltest_test

func TestEnv_SeedAndDoubles(t *testing.T) {
	e := at.New(t)
	ctx := t.Context()
	if e.Ravi.Name != "Ravi Kulkarni" || e.Meena.Name != "Meena Iyer" { t.Fatal("canonical users missing") }
	ok, _ := e.Auth.HasRole(ctx, e.Ravi.ID, "Purchase Manager")
	if !ok { t.Error("Ravi should hold Purchase Manager") }
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	if got := e.Title(t, d); got != "Draft 42" { t.Errorf("title %q", got) }
	if err := e.Auth.Can(ctx, e.Ravi, at.Order, "approve", &auth.RecordRef{DocType: at.Order, ID: 42}); err != nil { t.Error(err) }
	if err := e.Auth.Can(ctx, e.Priya, at.Order, "approve", &auth.RecordRef{DocType: at.Order, ID: 42}); !errors.Is(err, auth.ErrForbidden) { t.Error(err) }
	m, _ := e.Dir.ManagerChain(ctx, nil, e.Emp("EMP-007"))
	if len(m) == 0 || m[0] != e.Emp("EMP-004") { t.Errorf("Deepak's manager = %v, want Priya (EMP-004)", m) }
	claim := &auth.RecordRef{DocType: at.Claim, ID: 10, CreatedBy: e.Deepak.ID, EmployeeID: e.Emp("EMP-007")}
	if err := e.Auth.Can(ctx, e.Priya, at.Claim, "approve", claim); err != nil { t.Errorf("team scope: %v", err) } // CD-11
	if err := e.Auth.Can(ctx, e.Ravi, at.Claim, "read", claim); !errors.Is(err, doctype.ErrNotFound) { t.Errorf("Ravi is not Deepak's manager: %v", err) } // CD-54
	if p := testkit.Principal(t, "priya@vardhan.test"); p.ID != e.Priya.ID { t.Error("principal by email (CD-72)") }
	// CD-86: the Administrator row seeded with Grant("Administrator", "*", …) gives Anil every right
	if err := e.Auth.Can(ctx, e.Anil, at.Receipt, "approve", &auth.RecordRef{DocType: at.Receipt, ID: 7}); err != nil { t.Errorf("admin wildcard: %v", err) }
	// CD-86: UsersWithRight skips disabled users; RemoveRole takes effect on the next call
	ids, _ := e.Auth.UsersWithRight(ctx, at.Order, "approve", &auth.RecordRef{DocType: at.Order, ID: 42})
	if !slices.Contains(ids, e.Ravi.ID) { t.Errorf("approvers %v lack Ravi", ids) }
	e.Disable(t, e.Ravi.ID)
	ids, _ = e.Auth.UsersWithRight(ctx, at.Order, "approve", &auth.RecordRef{DocType: at.Order, ID: 42})
	if slices.Contains(ids, e.Ravi.ID) { t.Errorf("disabled Ravi still in %v", ids) }
	e.RemoveRole(t, e.Meena.ID, "Director")
	if ok, _ := e.Auth.HasRole(ctx, e.Meena.ID, "Director"); ok { t.Error("RemoveRole had no effect") }
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/kernel/approval/... -run 'TestFormat_|TestEnv_' -v`
Expected: FAIL to compile (`joinRoles`, `approvaltest.New` undefined).

- [ ] **Step 3: Implement `contract.go` fields, `deps.go`, `model.go`, `errors.go`, `format.go`**

`New` stores deps and fills the defaults (`audit.Default`, `doctype.Lookup`, `doctype.All`). All service methods other than those in this task keep returning `ErrNotImplemented` until their task. `UserError` constructors live next to the sentinels (e.g. `notEligible(title string, amount types.Money, manager string, roles []string) error`, which formats the amount with `types.FormatINR`), one per row of the Messages table; each sets the `Status()` and `Field` given there (CD-44). Implementation note: methods whose §5 signature names a parameter `doctype` (e.g. `Record`) shadow the `doctype` package in the body; name that parameter `dt` in the implementation.

- [ ] **Step 4: Implement the `approvaltest` package**

First run the gap check above. `New(t)` order: `testkit.NewDB`, `fx := testkit.Seed`, principals via `testkit.Principal(t, "<name>@vardhan.test")` (CD-72), shared fakes (`authfake.New()` seeded from `fx`, `testkit.Directory(fx)`; CD-56), `Docs`, `auditfake.New()`, `Clock`, harness tables, `approval.New(approval.Deps{…, Auth: e.Auth, Info: docs, Directory: e.Dir, Audit: rec, Lookup: Lookup, All: DocTypes})`, then insert R1, R2, E1 through `dbq.InsertRule` and store their ids. `AddUser` adds a user to the auth fake only (ids from 1001).

- [ ] **Step 5: Run to verify they pass** (NUM's formatters are on main, CD-53)

Run: `go test ./internal/kernel/approval/... -run 'TestFormat_|TestEnv_' -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/kernel/approval
git commit -m "feat(approval): core types, messages and test harness on the §5 contracts"
```

---

## Task 3: Rule administration

**Files:**
- Create: `internal/kernel/approval/rules.go`
- Test: `internal/kernel/approval/rules_test.go` (package `approval_test`), `internal/kernel/approval/rules_unit_test.go` (package `approval_test`, no DB)

**Interfaces:**
- Consumes: Task 1 `dbq`; Task 2 `Deps` (`Lookup`, `All`, `Audit`), `UserError`, `approvaltest`; `doctype.DocType`; `types.FormatINR` (duplicate message); `audit.Writer`.
- Produces:

```go
func ValidateRule(in RuleInput, lookup func(string) (*doctype.DocType, bool)) error // pure: validates NormalizeRule(in) — DocType, amount, approver fields; production passes doctype.Lookup
func NormalizeRule(in RuleInput) RuleInput         // TrimSpace on DocType, Role, FallbackRole
func (a *Approvals) CreateRule(ctx context.Context, p auth.Principal, in RuleInput) (Rule, error)
func (a *Approvals) UpdateRule(ctx context.Context, p auth.Principal, id, version int64, in RuleInput) (Rule, error)
func (a *Approvals) SetRuleEnabled(ctx context.Context, p auth.Principal, id int64, enabled bool) error
func (a *Approvals) DeleteRule(ctx context.Context, p auth.Principal, id int64) error
func (a *Approvals) Rules(ctx context.Context, p auth.Principal) ([]Rule, error)
func (a *Approvals) ApprovableDocTypes() []*doctype.DocType // Deps.All() filtered to Approvable && Master == nil, sorted by Label
```

Admin check: `a.auth.HasRole(ctx, p.ID, "Administrator")`, live. Every write is one `db.Write` that also writes one audit entry through `Deps.Audit` (`audit.Writer`, CD-03): `audit.Entry{DocType: "approval.Rule", DocID: id, Action: "create"|"save"|"enable"|"disable"|"delete", Changes: field-level before/after (from_amount as paise text), By: p.ID, At: clock.Now()}`. Duplicate check uses `FindEnabledDuplicate`; a unique-index violation from the partial index is mapped to the same `ErrDuplicateRule`. `UpdateRule` with zero affected rows → `ErrRuleConflict`. `DeleteRule` with `CountRuleUsage > 0` → `ErrRuleInUse`.

- [ ] **Step 1: Write the failing tests**

```go
// rules_unit_test.go
func TestApprovalRule_FromAmountNotNegative(t *testing.T) { // APR-003
	cat := at.Lookup
	in := approval.RuleInput{DocType: at.Order, FromAmount: -100, ApproverType: approval.ApproverRole, Role: "Purchase Manager"}
	at.WantErr(t, approval.ValidateRule(in, cat), approval.ErrInvalidRule, "From amount must be ₹0.00 or more.")
	in.FromAmount = 0
	at.MustOK(t, approval.ValidateRule(in, cat))
}
func TestApprovalRule_RoleRequired(t *testing.T) { // APR-005
	in := approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(50000), ApproverType: approval.ApproverRole, Role: "  "}
	at.WantErr(t, approval.ValidateRule(in, at.Lookup), approval.ErrInvalidRule, "Choose an approver role for this rule.")
}
func TestApprovalRule_ManagerNeedsFallbackRole(t *testing.T) { // APR-007
	in := approval.RuleInput{DocType: at.Claim, ApproverType: approval.ApproverManager}
	at.WantErr(t, approval.ValidateRule(in, at.Lookup), approval.ErrInvalidRule,
		"Choose a fallback role for claims whose employee has no reporting manager.")
}
func TestApprovalRule_OnlyApprovableDocTypes(t *testing.T) { // APR-009
	in := approval.RuleInput{DocType: at.ItemMaster, ApproverType: approval.ApproverRole, Role: "Director"}
	at.WantErr(t, approval.ValidateRule(in, at.Lookup), approval.ErrInvalidRule,
		"Item is a master and can't need approval. Choose a document type.")
	e := at.New(t)
	var labels []string
	for _, d := range e.A.ApprovableDocTypes() { labels = append(labels, d.Label) }
	if diff := cmp.Diff([]string{"Expense Claim", "Goods Receipt Note", "Purchase Order"}, labels); diff != "" { t.Error(diff) }
}

// rules_test.go
func TestApprovalRule_CreateAmountRule(t *testing.T) { // APR-001
	e := at.New(t); e.RemoveRules(t, at.Order)
	r, err := e.A.CreateRule(t.Context(), e.Anil, approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(50000),
		ApproverType: approval.ApproverRole, Role: "Purchase Manager"})
	at.MustOK(t, err)
	if !r.Enabled || r.FromAmount != 5000000 || r.AllowSelfApproval { t.Errorf("%+v", r) }
	got := e.Audit.Entries()
	if len(got) != 1 || got[0].Action != "create" || got[0].By != e.Anil.ID || got[0].DocType != "approval.Rule" { t.Fatalf("%+v", got) }
	if !slices.ContainsFunc(got[0].Changes, func(c audit.Change) bool { return c.Field == "from_amount" && c.After == "5000000" }) { t.Error("from_amount change missing") }
}
func TestApprovalRule_OnlyAdminCanEdit(t *testing.T) { // APR-002
	e := at.New(t)
	in := approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(200000), ApproverType: approval.ApproverRole, Role: "Director"}
	_, err := e.A.CreateRule(t.Context(), e.Ravi, in)
	at.WantErr(t, err, approval.ErrNotAdmin, "You don't have permission to change approval rules. Ask an Administrator.")
	_, err = e.A.UpdateRule(t.Context(), e.Ravi, e.R1, 1, in)
	at.WantErr(t, err, approval.ErrNotAdmin, "You don't have permission to change approval rules. Ask an Administrator.")
	at.WantErr(t, e.A.SetRuleEnabled(t.Context(), e.Ravi, e.R1, false), approval.ErrNotAdmin, "You don't have permission to change approval rules. Ask an Administrator.")
	rules, _ := e.A.Rules(t.Context(), e.Anil)
	if len(rules) != 3 || len(e.Audit.Entries()) != 0 { t.Errorf("something was saved: %d rules", len(rules)) }
}
func TestApprovalRule_DuplicateBlocked(t *testing.T) { // APR-010
	e := at.New(t)
	_, err := e.A.CreateRule(t.Context(), e.Anil, approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(50000), ApproverType: approval.ApproverRole, Role: "Director"})
	at.WantErr(t, err, approval.ErrDuplicateRule, "A Purchase Order rule from ₹50,000.00 already exists. Edit that rule instead.")
	_, err = e.A.CreateRule(t.Context(), e.Anil, approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(200000), ApproverType: approval.ApproverRole, Role: "Director"})
	at.MustOK(t, err)
}
func TestApprovalRule_UsedRuleDisableNotDelete(t *testing.T) { // APR-013
	e := at.New(t)
	for i, id := range []int64{42, 43, 44} {
		d := e.NewDoc(t, at.Order, id, at.Rs(60000+int64(i)), e.Priya)
		at.MustOK(t, e.Submit(e.Priya, d))
	}
	at.WantErr(t, e.A.DeleteRule(t.Context(), e.Anil, e.R1), approval.ErrRuleInUse, "This rule has been used by 3 documents. Disable it instead of deleting.")
	at.MustOK(t, e.A.SetRuleEnabled(t.Context(), e.Anil, e.R1, false))
	fresh, err := e.A.CreateRule(t.Context(), e.Anil, approval.RuleInput{DocType: at.Receipt, FromAmount: 0, ApproverType: approval.ApproverRole, Role: "Director"})
	at.MustOK(t, err)
	at.MustOK(t, e.A.DeleteRule(t.Context(), e.Anil, fresh.ID))
}
func TestApprovalRule_TrimsRoleNames(t *testing.T) { // Review Focus 2
	e := at.New(t)
	r, err := e.A.CreateRule(t.Context(), e.Anil, approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(200000), ApproverType: approval.ApproverRole, Role: " Purchase Manager "})
	at.MustOK(t, err)
	if r.Role != "Purchase Manager" { t.Errorf("role %q", r.Role) }
}
func TestApprovalRule_StaleEditRefused(t *testing.T) { // Review Focus 3
	e := at.New(t)
	in := approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(75000), ApproverType: approval.ApproverRole, Role: "Purchase Manager"}
	_, err := e.A.UpdateRule(t.Context(), e.Anil, e.R1, 1, in)
	at.MustOK(t, err)
	_, err = e.A.UpdateRule(t.Context(), e.Anil, e.R1, 1, in)
	at.WantErr(t, err, approval.ErrRuleConflict, "This rule was changed by someone else. Reload and try again.")
}
```

`at.Lookup` is the test DocType lookup (Task 2), so the unit tests need no database and no global registry. APR-013 uses `Submit`, which Task 6 implements: until then this one test fails with `not implemented`; it passes at the end of Task 6 (note it in the Task 6 run step).

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/kernel/approval/ -run 'TestApprovalRule_' -v`
Expected: FAIL (`ValidateRule`, `CreateRule` undefined).

- [ ] **Step 3: Implement `rules.go`** with the signatures and rules above. Validation order: DocType via `lookup` (unknown → "Choose a document type."; not approvable → master message), amount, approver type (manager on a DocType with `EmployeeField == ""` → manager-only message listing the labels of approvable DocTypes that have an `EmployeeField`), role / fallback role, role active (only when `Deps.Roles` is set).

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/kernel/approval/ -run 'TestApprovalRule_' -v`
Expected: PASS except `TestApprovalRule_UsedRuleDisableNotDelete` (needs Task 6).

- [ ] **Step 5: Commit**

```bash
git add internal/kernel/approval/rules.go internal/kernel/approval/rules_test.go internal/kernel/approval/rules_unit_test.go
git commit -m "feat(approval): rule administration with validation, duplicates and audit"
```

---

## Task 4: Rule matching (pure)

**Files:**
- Create: `internal/kernel/approval/match.go`
- Test: `internal/kernel/approval/match_test.go` (package `approval`, internal)

**Interfaces:**
- Consumes: `Rule` (Task 2).
- Produces: `func match(rules []Rule, amount types.Money) (matched Rule, higher []Rule, ok bool)`. Input is the enabled rules of one DocType in any order; `matched` is the rule with the highest `FromAmount <= amount`; `higher` are the enabled rules with `FromAmount > matched.FromAmount`, ascending. Also `func approverRoles(matched Rule, higher []Rule) []string`: matched role (or fallback role for a manager rule), then each higher rule's role (or fallback), de-duplicated, order kept.

- [ ] **Step 1: Write the failing tests**

```go
func po(id int64, from types.Money, role string) Rule {
	return Rule{ID: id, RuleInput: RuleInput{DocType: "fixture.Order", FromAmount: from, ApproverType: ApproverRole, Role: role}, Enabled: true}
}
var r1, r2 = po(1, 5000000, "Purchase Manager"), po(2, 50000000, "Director")

func TestApprovalMatch_EqualToThresholdNeedsApproval(t *testing.T) { // APR-017
	m, _, ok := match([]Rule{r1}, 5000000)
	if !ok || m.ID != 1 { t.Fatalf("₹50,000.00 must match R1, got ok=%v id=%d", ok, m.ID) }
}
func TestApprovalMatch_ThresholdTable(t *testing.T) { // APR-018
	for amt, want := range map[types.Money]bool{4999999: false, 5000000: true, 5000001: true, 0: false} {
		if _, _, ok := match([]Rule{r1}, amt); ok != want { t.Errorf("amount %d: needs=%v want %v", amt, ok, want) }
	}
	if _, _, ok := match([]Rule{po(3, 0, "Purchase Manager")}, 0); !ok { t.Error("₹0.00 rule must match ₹0.00") }
}
func TestApprovalMatch_HighestThresholdWins(t *testing.T) { // APR-021
	m, higher, _ := match([]Rule{r2, r1}, 6000000)
	if m.ID != 1 || len(higher) != 1 || higher[0].ID != 2 { t.Errorf("₹60,000: %d %v", m.ID, higher) }
	if diff := cmp.Diff([]string{"Purchase Manager", "Director"}, approverRoles(m, higher)); diff != "" { t.Error(diff) }
	m, higher, _ = match([]Rule{r1, r2}, 60000000)
	if m.ID != 2 || len(higher) != 0 { t.Errorf("₹6,00,000: %d %v", m.ID, higher) }
}
func TestApprovalMatch_OtherDocTypeIgnored(t *testing.T) { // APR-022 (unit half; Evaluate only passes rules of the document's type)
	if _, _, ok := match(nil, 100000000); ok { t.Error("no PO rules → no approval for ₹10,00,000.00") }
}
```

Also add to `evaluate_test.go` in Task 6 the integration half of APR-022 is not needed: the unit test above plus `ListEnabledRulesForDocType` filtering is the whole behaviour; Task 6's `TestApprovalSubmit_BelowThresholdApproved` covers the call path.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/kernel/approval/ -run 'TestApprovalMatch_(Equal|Threshold|Highest|OtherDocType)' -v`
Expected: FAIL (`match` undefined).

- [ ] **Step 3: Implement `match` and `approverRoles` in `match.go`.**

- [ ] **Step 4: Run to verify they pass** (same command). Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/kernel/approval/match.go internal/kernel/approval/match_test.go
git commit -m "feat(approval): threshold matching"
```

---

## Task 5: Eligibility

**Files:**
- Create: `internal/kernel/approval/eligible.go`
- Test: `internal/kernel/approval/eligible_test.go`

**Interfaces:**
- Consumes: `auth.Service.UsersWithRight`, `auth.Service.HasRole`, `auth.Directory.UserForEmployee`, `doctype.InfoSource`, `doctype.Info`, `dbq.GetOpenRequest`.
- Produces:

```go
// snapshot is what eligibility needs, built from a Requirement at submit or from a stored Request later.
type snapshot struct { doctype string; roles []string; manager types.UserID; allowSelf bool; own []types.UserID }
func recordRef(in doctype.Info) *auth.RecordRef // DocType, ID, CreatedBy, EmployeeID, ProjectIDs, SiteIDs copied from Info
func (a *Approvals) own(ctx context.Context, tx *db.Tx, requester types.UserID, in doctype.Info) ([]types.UserID, error) // requester, CreatedBy, Directory.UserForEmployee(EmployeeID) when ok; zeros dropped
func (a *Approvals) eligible(ctx context.Context, s snapshot, in doctype.Info) ([]types.UserID, error)
func (a *Approvals) holdsAny(ctx context.Context, u types.UserID, s snapshot) (bool, error) // u == manager or HasRole(u, any of roles)
func (a *Approvals) EligibleApprovers(ctx context.Context, tx *db.Tx, doctype string, id int64) ([]types.UserID, error) // for the open request; [] when none open
```

Algorithm of `eligible` (decided): candidates = `UsersWithRight(doctype, "approve", recordRef(in))` ∩ `UsersWithRight(doctype, "read", recordRef(in))` (both exclude disabled users and apply scope, including `team` (CD-11), and restrictions); keep `u` when `holdsAny`; drop `u` in `s.own` unless `s.allowSelf`; result sorted by user ID. A reporting manager is a candidate through the seeded Employee `team` scope on claims; no extra grant is needed.

- [ ] **Step 1: Write the failing test**

```go
func TestApprovalEligible_ExcludesDisabled(t *testing.T) { // APR-041
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	e.Disable(t, e.Ravi.ID)
	var got []types.UserID
	at.MustOK(t, e.DB.Read(t.Context(), func(tx *db.Tx) (err error) { got, err = e.A.EligibleApprovers(t.Context(), tx, at.Order, 42); return }))
	if diff := cmp.Diff([]types.UserID{e.Meena.ID}, got); diff != "" { t.Error(diff) }
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/kernel/approval/ -run TestApprovalEligible_ExcludesDisabled -v`
Expected: FAIL (`EligibleApprovers` undefined; after Step 3, fails on `Submit` not implemented until Task 6).

- [ ] **Step 3: Implement `eligible.go`.**

- [ ] **Step 4: Run** after Task 6 lands (`Submit` needs `Evaluate`/`Open`). Expected: PASS. Task 6 Step 4 runs it.

- [ ] **Step 5: Commit**

```bash
git add internal/kernel/approval/eligible.go internal/kernel/approval/eligible_test.go
git commit -m "feat(approval): live approver eligibility"
```

---

## Task 6: Evaluate and Open (submit)

**Files:**
- Create: `internal/kernel/approval/evaluate.go`
- Test: `internal/kernel/approval/evaluate_test.go`

**Interfaces:**
- Consumes: Task 3 `UpdateRule`; Task 4 `match`, `approverRoles`; Task 5 `eligible`, `own`, `snapshot`; `doctype.InfoSource`, `auth.Directory` (`ManagerChain`, `UserForEmployee`), `auth.Service.User`, `types.FormatINR`.
- Produces: `Evaluate`, `Open` (§5 signatures), and `func (a *Approvals) LatestRequest(ctx context.Context, tx *db.Tx, doctype string, id int64) (Request, bool, error)`.

`Evaluate` (decided):
1. `in := Info.Info(ctx, tx, doctype, id)`; rules := `ListEnabledRulesForDocType`; `match(rules, amount)`; no match → `Requirement{Needed: false, Amount: amount, Requester: requester.ID}`.
2. Manager rule: chain := `Directory.ManagerChain(ctx, tx, in.EmployeeID)`; candidate employee = `chain[0]` unless empty or equal to `in.EmployeeID`; `u, ok := Directory.UserForEmployee(ctx, tx, candidate)`; `u` is designated when `ok`, `u` is in the read ∩ approve candidate set (so enabled and, through `team` scope, able to see the claim), and `u` is not in `own`. `ManagerUser` = `u`, else 0.
3. `ApproverRoles = approverRoles(matched, higher)`; `Needed = true`; `RuleID = matched.ID`; `Amount = amount`; `Requester = requester.ID` (CD-24).
4. `eligible(...)` empty → `ErrNoApprover` naming `in.DocLabel` and the matched rule's role (fallback role for a manager rule).

`Open` (works from the exported §5 fields only, so a `Requirement` built by DOC's tests or `approvalfake` behaves the same): `req.Requester == 0`, or `Needed` with a `RuleID` that isn't a rule of this DocType → `ErrNotEvaluated`. Always inserts history `submitted` (amount; `rule_id` = RuleID when Needed, else NULL; user = requester, name from `Auth.User`). When `Needed`, inserts the request with the snapshot: `approver_roles` JSON, manager user and name (`Auth.User`), `allow_self` from the rule row (same transaction as `Evaluate`), requester name, creator and claimant user (re-read via `Info` and `Directory.UserForEmployee`), `submitted_at = clock.Now()`.

- [ ] **Step 1: Write the failing tests**

```go
func TestApprovalSubmit_BelowThresholdApproved(t *testing.T) { // APR-016
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 41, types.Money(4999999), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	if e.State(t, d) != "approved" || e.Number(t, d) != "PO/26-27/00001" { t.Errorf("%s %s", e.State(t, d), e.Number(t, d)) }
	if _, ok := e.Latest(t, d); ok { t.Error("no request expected") }
}
func TestApprovalMatch_UsesGrandTotalWithGST(t *testing.T) { // APR-019 (grand total is DOC's Hooks.Amount; APR stores what it is given)
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, types.Money(5003200), e.Priya, at.Party("Bharat Tools"))
	at.MustOK(t, e.Submit(e.Priya, d))
	r, ok := e.Latest(t, d)
	if e.State(t, d) != "pending_approval" || !ok || r.Amount != 5003200 || r.RuleID != e.R1 { t.Errorf("%+v", r) }
}
func TestApprovalMatch_ExpenseUsesClaimTotal(t *testing.T) { // APR-020
	e := at.New(t)
	e2 := e.AddRule(t, approval.RuleInput{DocType: at.Claim, FromAmount: at.Rs(25000), ApproverType: approval.ApproverRole, Role: "Director"})
	d := e.NewDoc(t, at.Claim, 10, at.Rs(18000)+at.Rs(7000), e.Deepak)
	at.MustOK(t, e.Submit(e.Deepak, d))
	r, _ := e.Latest(t, d)
	if e.State(t, d) != "pending_approval" || r.Amount != 2500000 || r.RuleID != e2 { t.Errorf("%+v", r) }
}
func TestApprovalMatch_ZeroThresholdAlwaysApplies(t *testing.T) { // APR-023
	e := at.New(t); e.RemoveRules(t, at.Order)
	e.AddRule(t, approval.RuleInput{DocType: at.Order, FromAmount: 0, ApproverType: approval.ApproverRole, Role: "Purchase Manager"})
	d := e.NewDoc(t, at.Order, 42, at.Rs(1500), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	if e.State(t, d) != "pending_approval" { t.Error(e.State(t, d)) }
}
func TestApprovalRequest_SnapshotsRule(t *testing.T) { // APR-025
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	_, err := e.A.UpdateRule(t.Context(), e.Anil, e.R1, 1, approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(75000), ApproverType: approval.ApproverRole, Role: "Director"})
	at.MustOK(t, err)
	r, _ := e.Latest(t, d)
	if diff := cmp.Diff([]string{"Purchase Manager", "Director"}, r.ApproverRoles); diff != "" { t.Error(diff) }
	if got := e.InboxTitles(t, e.Ravi); !slices.Contains(got, "Draft 42") { t.Errorf("Ravi's inbox %v", got) }
	later := e.NewDoc(t, at.Order, 43, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, later))
	if e.State(t, later) != "approved" { t.Error("a ₹60,000 PO submitted after the edit is below the new ₹75,000 rule") }
}
func TestApprovalSubmit_NoEligibleApproverRefused(t *testing.T) { // APR-040
	e := at.New(t)
	e.Auth.AddRole(e.Ravi.ID, "Purchase User"); e.RemoveRole(t, e.Meena.ID, "Director")
	d := e.NewDoc(t, at.Order, 43, at.Rs(70000), e.Ravi)
	at.WantErr(t, e.Submit(e.Ravi, d), approval.ErrNoApprover,
		"No one else can approve this Purchase Order. Give the Purchase Manager role to another active user, then submit again.")
	if e.State(t, d) != "draft" || len(e.History(t, d)) != 0 { t.Error("nothing may change") }
}
func TestApprovalExpense_ReportingManagerApproves(t *testing.T) { // APR-044
	e := at.New(t)
	d := e.NewDoc(t, at.Claim, 10, at.Rs(3450), e.Deepak)
	at.MustOK(t, e.Submit(e.Deepak, d))
	r, _ := e.Latest(t, d)
	if e.State(t, d) != "pending_approval" || r.ManagerUser != e.Priya.ID || r.ManagerName != "Priya Shah" { t.Fatalf("%+v", r) }
	at.MustOK(t, e.Approve(e.Priya, d))
}
func TestApprovalExpense_NoManagerUsesFallback(t *testing.T) { // APR-047
	e := at.New(t)
	d := e.NewDoc(t, at.Claim, 11, at.Rs(2000), e.Anil, at.ForEmployee("EMP-001"))
	at.MustOK(t, e.Submit(e.Anil, d))
	r, _ := e.Latest(t, d)
	if e.State(t, d) != "pending_approval" || r.ManagerUser != 0 || !slices.Equal(r.ApproverRoles, []string{"Director"}) { t.Errorf("%+v", r) }
}
func TestApprovalExpense_DisabledManagerUsesFallback(t *testing.T) { // APR-048
	for _, setup := range []func(e *at.Env){
		func(e *at.Env) { e.Disable(t, e.Priya.ID) },
		func(e *at.Env) { e.Unlink(t, "EMP-004") },
	} {
		e := at.New(t); setup(e)
		d := e.NewDoc(t, at.Claim, 10, at.Rs(3450), e.Deepak)
		at.MustOK(t, e.Submit(e.Deepak, d))
		r, _ := e.Latest(t, d)
		if r.ManagerUser != 0 || !slices.Equal(r.ApproverRoles, []string{"Director"}) { t.Errorf("%+v", r) }
	}
}
func TestApprovalExpense_HighAmountGoesToRole(t *testing.T) { // APR-051
	e := at.New(t)
	e2 := e.AddRule(t, approval.RuleInput{DocType: at.Claim, FromAmount: at.Rs(25000), ApproverType: approval.ApproverRole, Role: "Director"})
	d := e.NewDoc(t, at.Claim, 10, at.Rs(30000), e.Deepak)
	at.MustOK(t, e.Submit(e.Deepak, d))
	r, _ := e.Latest(t, d)
	if r.RuleID != e2 || r.ManagerUser != 0 { t.Errorf("%+v", r) }
	at.WantErr(t, e.Approve(e.Priya, d), approval.ErrNotEligible, "Only a user with the Director role can approve Draft 10 (₹30,000.00).")
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/kernel/approval/ -run 'TestApprovalSubmit_BelowThreshold|TestApprovalMatch_(UsesGrand|ExpenseUses|ZeroThreshold)|TestApprovalRequest_Snapshots|TestApprovalSubmit_NoEligible|TestApprovalExpense_(Reporting|NoManager|DisabledManager|HighAmount)' -v`
Expected: FAIL (`not implemented`).

- [ ] **Step 3: Implement `Evaluate`, `Open`, `LatestRequest` in `evaluate.go`** as decided above.

- [ ] **Step 4: Run to verify they pass**

Run the Step 2 command plus `-run 'TestApprovalEligible_ExcludesDisabled|TestApprovalRule_UsedRuleDisableNotDelete'`.
Expected: PASS, except tests that call `Approve` (`ReportingManagerApproves`, `HighAmountGoesToRole`) and `InboxTitles` (`SnapshotsRule`), which pass after Tasks 7 and 8.

- [ ] **Step 5: Commit**

```bash
git add internal/kernel/approval/evaluate.go internal/kernel/approval/evaluate_test.go
git commit -m "feat(approval): evaluate rules at submit and open requests"
```

---

## Task 7: CheckApprover and Record (approve, reject, revise, withdraw)

**Files:**
- Create: `internal/kernel/approval/decide.go`, `internal/kernel/approval/history.go`
- Test: `internal/kernel/approval/decide_test.go`

**Interfaces:**
- Consumes: Task 5 `eligible`, `holdsAny`, `own`, `recordRef`; Task 6 `LatestRequest`; `doctype.InfoSource`; `auth.Service.Can`; `dbq.CloseRequest`, `InsertHistory`, `LastHistory`, `ListHistory`.
- Produces: `CheckApprover`, `Record(ctx, tx, doctype string, id int64, action doctype.Action, p auth.Principal, reason string) error` (§5, CD-23), `func (a *Approvals) History(ctx context.Context, tx *db.Tx, doctype string, id int64) ([]HistoryRow, error)`.

`CheckApprover` order (decided; CD-52 puts DOC's visibility check before it and DOC's from-state guard after it, so this order gives each test its message). `in := Info.Info(...)` first:
1. Defensive repeat of DOC's visibility check: `auth.Can(p, doctype, "read", recordRef(in))` returns `doctype.ErrNotFound` (hidden by scope, including `team`, or a restriction) → return it unchanged (404, no APR text). Through DOC this never fires, because DOC already answered 404 (CD-52); it keeps direct callers from learning about hidden records. `auth.ErrForbidden` (no `read` right on the DocType at all) does not stop here; step 4 or 5 answers it.
2. No open request → `ErrNotPending` (CD-26: "no longer … It was {outcome} by {name}" when the latest request's outcome produced the current state — approved→`approved`, rejected→`rejected`, withdrawn→`draft` — else "is not waiting … It is {State}").
3. `p` in own, `!allowSelf`, and `holdsAny(p)` → `ErrSelfApproval`.
4. Not `holdsAny(p)` → `ErrNotEligible` (so the "Only {Manager} or a user with … role" message reaches only users who can see the document, CD-11).
5. `p` not in `eligible(...)` (disabled, no `approve` right) → `ErrNotEligible`. DOC runs no `Can(approve)` for approve or reject (CD-52), so steps 4–5 are the only approve-right check and a user without the right gets this message, not AUTH's 403 text.

Changed from the round-1 draft (CD-52): a hidden record is a 404 from DOC's visibility check before APR is called; `ErrNoAccess` and its "You don't have access to {Title}." text are gone. APR-034 (Ravi restricted to PRJ-TECHPARK) and Ravi in APR-045 (his `team` scope doesn't cover Deepak) expect `doctype.ErrNotFound` without asserting message text.

`Record` by action:
- `approve` with an open request: `CheckApprover`; `CloseRequest(outcome approved, decided_by p, name, now, reason = comment)`; 0 rows → `ErrNotPending`; history `approved` with `self_approved = p ∈ own`.
- `approve` without an open request: allowed only when `LastHistory` is a `submitted` row with NULL `rule_id` (the same submit) → history `approved_no_rule`; otherwise `ErrNotPending`.
- `reject`: trim with `strings.TrimSpace`; empty → `ErrReasonRequired`; `utf8.RuneCountInString > 1000` → `ErrReasonTooLong`; then as approve with outcome `rejected` and history `rejected` carrying the reason.
- `revise`: history `revised` with the latest request's amount and rule.
- `withdraw`: open request required (else `ErrNotPending`); close with outcome `withdrawn`, decided_by p; history `withdrawn`.
- any other `doctype.Action` → `ErrUnknownAction`.

Every history row carries the request's amount and rule id when a request exists.

- [ ] **Step 1: Write the failing tests**

```go
func pending42(t *testing.T) (*at.Env, at.Doc) {
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	return e, d
}
func TestApprovalApprove_RoleHolderCanApprove(t *testing.T) { // APR-029
	e, d := pending42(t)
	at.MustOK(t, e.Approve(e.Ravi, d))
	h := e.History(t, d)
	if e.State(t, d) != "approved" || e.Number(t, d) != "PO/26-27/00001" || h[len(h)-1].Action != approval.HistApproved || h[len(h)-1].By != e.Ravi.ID { t.Errorf("%+v", h) }
}
func TestApprovalApprove_NonApproverRefused(t *testing.T) { // APR-030
	e := at.New(t); e.Auth.AddRole(e.Ravi.ID, "Purchase User")
	d := e.NewDoc(t, at.Order, 43, at.Rs(60000), e.Ravi, at.Project(at.ProjMetro))
	at.MustOK(t, e.Submit(e.Ravi, d))
	err := e.Approve(e.Priya, d) // Priya can read it (Purchase User, PRJ-METRO) but holds no approve right: APR's text, not AUTH's 403 (CD-52)
	at.WantErr(t, err, approval.ErrNotEligible, "Only a user with the Purchase Manager or Director role can approve Draft 43 (₹60,000.00).")
	at.WantStatus(t, err, 403)
	if e.State(t, d) != "pending_approval" { t.Error(e.State(t, d)) }
}
func TestApprovalApprove_HigherTierCanApprove(t *testing.T) { // APR-032
	e, d := pending42(t)
	at.MustOK(t, e.Approve(e.Meena, d))
}
func TestApprovalApprove_LowerTierRefused(t *testing.T) { // APR-033
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 50, at.Rs(600000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	at.WantErr(t, e.Approve(e.Ravi, d), approval.ErrNotEligible, "Only a user with the Director role can approve Draft 50 (₹6,00,000.00).")
}
func TestApprovalApprove_NeedsReadAccess(t *testing.T) { // APR-034
	e, d := pending42(t)
	e.Auth.Restrict(e.Ravi.ID, []int64{at.ProjTechPark}, nil)
	at.WantNotFound(t, e.Approve(e.Ravi, d)) // CD-52: 404 before APR is called
	at.WantNotFound(t, e.Reject(e.Ravi, d, "No"))
	if e.State(t, d) != doctype.StatePendingApproval { t.Error(e.State(t, d)) }
	if slices.Contains(e.InboxTitles(t, e.Ravi), "Draft 42") { t.Error("must not be in Ravi's inbox") } // passes after Task 8
}
func TestApprovalSelf_RequesterBlocked(t *testing.T) { // APR-035
	e := at.New(t); e.Auth.AddRole(e.Ravi.ID, "Purchase User")
	d := e.NewDoc(t, at.Order, 43, at.Rs(70000), e.Ravi)
	at.MustOK(t, e.Submit(e.Ravi, d))
	at.WantErr(t, e.Approve(e.Ravi, d), approval.ErrSelfApproval, "You can't approve your own Purchase Order. Ask another Purchase Manager or Director to approve it.")
}
func TestApprovalSelf_CreatorOrSubmitter(t *testing.T) { // APR-036
	e := at.New(t); e.Auth.AddRole(e.Ravi.ID, "Purchase User")
	d44 := e.NewDoc(t, at.Order, 44, at.Rs(60000), e.Priya); at.MustOK(t, e.Submit(e.Ravi, d44))
	d45 := e.NewDoc(t, at.Order, 45, at.Rs(60000), e.Ravi); at.MustOK(t, e.Submit(e.Priya, d45))
	for _, d := range []at.Doc{d44, d45} {
		at.WantErr(t, e.Approve(e.Ravi, d), approval.ErrSelfApproval, "You can't approve your own Purchase Order. Ask another Purchase Manager or Director to approve it.")
		at.MustOK(t, e.Approve(e.Meena, d))
	}
}
func TestApprovalSelf_ClaimantBlocked(t *testing.T) { // APR-037
	e := at.New(t); e.Auth.AddRole(e.Ravi.ID, "Director")
	d := e.NewDoc(t, at.Claim, 12, at.Rs(3450), e.Anil, at.ForEmployee("EMP-003"))
	at.MustOK(t, e.Submit(e.Anil, d))
	at.WantErr(t, e.Approve(e.Ravi, d), approval.ErrSelfApproval, "You can't approve your own Expense Claim. Ask another Director to approve it.")
}
func TestApprovalSelf_AdminNotExempt(t *testing.T) { // APR-039
	e := at.New(t); e.Auth.AddRole(e.Anil.ID, "Purchase Manager")
	d := e.NewDoc(t, at.Order, 46, at.Rs(60000), e.Anil)
	at.MustOK(t, e.Submit(e.Anil, d))
	at.WantErr(t, e.Approve(e.Anil, d), approval.ErrSelfApproval, "You can't approve your own Purchase Order. Ask another Purchase Manager or Director to approve it.")
}
func TestApprovalEligible_LiveRoleCheck(t *testing.T) { // APR-042
	e, d := pending42(t)
	e.Auth.AddRole(e.Kavya.ID, "Purchase Manager")
	at.MustOK(t, e.Approve(e.Kavya, d))
	e2, d2 := pending42(t)
	e2.RemoveRole(t, e2.Ravi.ID, "Purchase Manager")
	at.WantErr(t, e2.Approve(e2.Ravi, d2), approval.ErrNotEligible, "Only a user with the Purchase Manager or Director role can approve Draft 42 (₹60,000.00).")
}
func TestApprovalExpense_OtherManagerRefused(t *testing.T) { // APR-045 (CD-11 team scope, CD-52 order)
	e := at.New(t)
	d := e.NewDoc(t, at.Claim, 10, at.Rs(3450), e.Deepak)
	at.MustOK(t, e.Submit(e.Deepak, d))
	at.WantNotFound(t, e.Approve(e.Ravi, d)) // Ravi's team is Priya and Suresh, not Deepak: he can't see Draft 10 → 404
	if slices.Contains(e.InboxTitles(t, e.Ravi), "Draft 10") { t.Error("must not be in Ravi's inbox") } // after Task 8
	at.WantErr(t, e.Approve(e.Anil, d), approval.ErrNotEligible, // sees every claim, holds approve, but is neither Priya nor a Director
		"Only Priya Shah or a user with the Director role can approve Draft 10 (₹3,450.00).")
	if e.State(t, d) != "pending_approval" { t.Error(e.State(t, d)) }
}
func TestApprovalExpense_ManagerDisabledWhilePending(t *testing.T) { // APR-049
	e := at.New(t)
	d := e.NewDoc(t, at.Claim, 10, at.Rs(3450), e.Deepak)
	at.MustOK(t, e.Submit(e.Deepak, d))
	e.Disable(t, e.Priya.ID)
	if slices.Contains(e.InboxTitles(t, e.Priya), "Draft 10") || !slices.Contains(e.InboxTitles(t, e.Meena), "Draft 10") { t.Error("claim must move to the Director list") } // after Task 8
	at.MustOK(t, e.Approve(e.Meena, d))
}
func TestApprovalApprove_OnlyFromPending(t *testing.T) { // APR-055
	e := at.New(t)
	cases := []struct{ id int64; state, number, msg string }{
		{60, "draft", "", "Draft 60 is not waiting for approval. It is Draft."},
		{61, "approved", "PO/26-27/00001", "PO/26-27/00001 is not waiting for approval. It is Approved."},
		{62, "rejected", "", "Draft 62 is not waiting for approval. It is Rejected."},
		{63, "cancelled", "PO/26-27/00002", "PO/26-27/00002 is not waiting for approval. It is Cancelled."},
		{64, "closed", "PO/26-27/00003", "PO/26-27/00003 is not waiting for approval. It is Closed."},
	}
	for _, c := range cases {
		d := e.NewDoc(t, at.Order, c.id, at.Rs(60000), e.Priya, at.InState(c.state, c.number))
		at.WantErr(t, e.Approve(e.Ravi, d), approval.ErrNotPending, c.msg)
	}
}
func TestApprovalReject_NotAfterApprove(t *testing.T) { // APR-057
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 61, at.Rs(60000), e.Priya, at.InState("approved", "PO/26-27/00001"))
	at.WantErr(t, e.Reject(e.Meena, d, "Too late"), approval.ErrNotPending, "PO/26-27/00001 is not waiting for approval. It is Approved.")
}
func TestApprovalReject_ReasonRequired(t *testing.T) { // APR-058
	e, d := pending42(t)
	for _, r := range []string{"", "   "} {
		at.WantErr(t, e.Reject(e.Ravi, d, r), approval.ErrReasonRequired, "Enter a reason for rejecting.")
	}
	if e.State(t, d) != "pending_approval" { t.Error(e.State(t, d)) }
}
func TestApprovalReject_StoresReason(t *testing.T) { // APR-060
	e, d := pending42(t)
	reason := "Rate is 12% above last PO. Renegotiate with vendor."
	at.MustOK(t, e.Reject(e.Ravi, d, reason))
	h := e.History(t, d); last := h[len(h)-1]
	if e.State(t, d) != "rejected" || e.Effects(t, d) != 0 || e.Title(t, d) != "Draft 42" { t.Error("state/effects/number") }
	if last.Action != approval.HistRejected || last.Reason != reason || last.By != e.Ravi.ID || !last.At.Equal(time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)) { t.Errorf("%+v", last) }
}
func TestApprovalReject_SameEligibility(t *testing.T) { // APR-061
	e, d := pending42(t)
	at.WantErr(t, e.Reject(e.Priya, d, "No"), approval.ErrNotEligible, "Only a user with the Purchase Manager or Director role can approve Draft 42 (₹60,000.00).") // reads it, no approve right: APR text (CD-52)
	e.Auth.AddRole(e.Ravi.ID, "Purchase User")
	d43 := e.NewDoc(t, at.Order, 43, at.Rs(60000), e.Ravi); at.MustOK(t, e.Submit(e.Ravi, d43))
	at.WantErr(t, e.Reject(e.Ravi, d43, "No"), approval.ErrSelfApproval, "You can't approve your own Purchase Order. Ask another Purchase Manager or Director to approve it.")
}
func TestApprovalConcurrency_FirstActionWins(t *testing.T) { // APR-071
	e, d := pending42(t)
	at.MustOK(t, e.Approve(e.Ravi, d))
	at.WantErr(t, e.Reject(e.Meena, d, "Too costly"), approval.ErrNotPending, "PO/26-27/00001 is no longer waiting for approval. It was approved by Ravi Kulkarni.")
	if n := len(e.History(t, d)); n != 2 { t.Errorf("history rows %d", n) }
}
func TestApprovalReject_ReasonCountsCharactersNotBytes(t *testing.T) { // Review Focus 1
	e, d := pending42(t)
	at.WantErr(t, e.Reject(e.Ravi, d, " \n\t "), approval.ErrReasonRequired, "Enter a reason for rejecting.")
	at.MustOK(t, e.Reject(e.Ravi, d, "\n"+strings.Repeat("दर", 500)+"\t")) // 1,000 characters, 3,000 bytes
}
```

Add `TestRecord_WithdrawAndRevise` (no ID), calling `Record` directly with `doctype.Action` values: withdraw closes the open request with outcome `withdrawn` and writes a `withdrawn` row; a second withdraw returns `ErrNotPending`; revise after reject writes `revised` with the request's amount and rule; `Record(..., doctype.ActionClose, ...)` returns `ErrUnknownAction`.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/kernel/approval/ -run 'TestApproval(Approve|Self|Eligible_Live|Expense_Other|Expense_ManagerDisabled|Reject|Concurrency_First)|TestRecord_' -v`
Expected: FAIL (`not implemented`).

- [ ] **Step 3: Implement `decide.go` and `history.go`** as decided above.

- [ ] **Step 4: Run to verify they pass** (Step 2 command, plus the Task 6 tests that call `Approve`).
Expected: PASS, except the inbox assertions in `NeedsReadAccess`, `OtherManagerRefused` and `ManagerDisabledWhilePending` (Task 8).

- [ ] **Step 5: Commit**

```bash
git add internal/kernel/approval/decide.go internal/kernel/approval/history.go internal/kernel/approval/decide_test.go
git commit -m "feat(approval): approver checks and approve/reject/revise/withdraw records"
```

---

## Task 8: Inbox, My requests and document status

**Files:**
- Create: `internal/kernel/approval/inbox.go`
- Test: `internal/kernel/approval/inbox_test.go`

**Interfaces:**
- Consumes: Task 7 `CheckApprover`-equivalent checks (`holdsAny`, `eligible`, `auth.Can` read), `ListOpenRequests`, `ListLatestRequestsByRequester`, `doctype.InfoSource` (incl. `Info.Party`, CD-55), `types.Page`, `types.Result[T]`, `types.FormatIST`.
- Produces:

```go
func (a *Approvals) Inbox(ctx context.Context, p auth.Principal, page types.Page) (types.Result[InboxItem], error) // §5 (CD-01)
func (a *Approvals) MyRequests(ctx context.Context, p auth.Principal, page types.Page) (types.Result[RequestItem], error)
func (a *Approvals) Status(ctx context.Context, tx *db.Tx, doctype string, id int64) (Status, error)
```

`Inbox`: one `db.Read`; for each open request oldest first (`submitted_at`, `id`): `Info.Info` (on error: `slog.Warn` and skip; party = `Info.Party`), include when `p` is in `eligible(...)` and `auth.Can(p, read)` passes. `Total` = all included rows; page defaults `Size 25`, `Number 1`; a page past the end returns no rows with the right total. `MyRequests`: latest request per document where `requester = p`, newest first, with the record's current title (`Info.Display`) and state. `Status.Line` by the last history row, times through `types.FormatIST` (CD-33): `approved` → "Approved by {name} on {FormatIST}"; `approved_no_rule` → "Approved on submit (no approval needed)"; `rejected` → "Rejected by {name} on {FormatIST}: {reason}"; `revised` after a rejected request → "Last rejected by {name}: {reason}"; `withdrawn` → "Withdrawn by {name} on {FormatIST}"; open request → "Waiting for approval from {Manager name or }{roles}" with `Pending: true`; none → "".

- [ ] **Step 1: Write the failing tests**

```go
func TestApprovalApprove_StampsApprover(t *testing.T) { // APR-056
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	e.Clock.Set(time.Date(2026, 10, 24, 9, 15, 0, 0, time.UTC))
	at.MustOK(t, e.Approve(e.Ravi, d))
	r, _ := e.Latest(t, d)
	if r.DecidedBy != e.Ravi.ID || !r.DecidedAt.Equal(time.Date(2026, 10, 24, 9, 15, 0, 0, time.UTC)) { t.Errorf("%+v", r) }
	var s approval.Status
	at.MustOK(t, e.DB.Read(t.Context(), func(tx *db.Tx) (err error) { s, err = e.A.Status(t.Context(), tx, at.Order, 42); return }))
	if s.Line != "Approved by Ravi Kulkarni on 24-10-2026 14:45" { t.Error(s.Line) } // types.FormatIST, CD-33
}
func TestApprovalRevise_ToDraftKeepsReason(t *testing.T) { // APR-063
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	at.MustOK(t, e.Reject(e.Ravi, d, "Rate is 12% above last PO. Renegotiate with vendor."))
	at.MustOK(t, e.Revise(e.Priya, d))
	var s approval.Status
	at.MustOK(t, e.DB.Read(t.Context(), func(tx *db.Tx) (err error) { s, err = e.A.Status(t.Context(), tx, at.Order, 42); return }))
	if e.State(t, d) != "draft" || e.Title(t, d) != "Draft 42" || s.Line != "Last rejected by Ravi Kulkarni: Rate is 12% above last PO. Renegotiate with vendor." { t.Errorf("%s %q", e.State(t, d), s.Line) }
}
func TestApprovalInbox_OnlyActionable(t *testing.T) { // APR-077
	e := at.New(t); e.Auth.AddRole(e.Ravi.ID, "Purchase User")
	steps := []struct{ d at.Doc; by auth.Principal }{
		{e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya), e.Priya},
		{e.NewDoc(t, at.Order, 50, at.Rs(600000), e.Priya), e.Priya},
		{e.NewDoc(t, at.Order, 43, at.Rs(60000), e.Ravi), e.Ravi},
		{e.NewDoc(t, at.Claim, 10, at.Rs(3450), e.Deepak), e.Deepak},
	}
	for i, s := range steps { e.Clock.Set(time.Date(2026, 10, 24, 5, i, 0, 0, time.UTC)); at.MustOK(t, e.Submit(s.by, s.d)) }
	for _, c := range []struct{ p auth.Principal; want []string }{
		{e.Ravi, []string{"Draft 42"}}, {e.Meena, []string{"Draft 42", "Draft 50", "Draft 43", "Draft 10"}}, {e.Priya, []string{"Draft 10"}},
	} {
		if diff := cmp.Diff(c.want, e.InboxTitles(t, c.p)); diff != "" { t.Errorf("%s: %s", c.p.Name, diff) }
	}
}
func TestApprovalInbox_ClosesAndReopens(t *testing.T) { // APR-080
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	at.MustOK(t, e.Approve(e.Meena, d))
	if len(e.InboxTitles(t, e.Ravi)) != 0 { t.Error("approved item must leave Ravi's list") }
	e2 := at.New(t)
	d2 := e2.NewDoc(t, at.Order, 42, at.Rs(60000), e2.Priya)
	at.MustOK(t, e2.Submit(e2.Priya, d2)); at.MustOK(t, e2.Reject(e2.Meena, d2, "Fix rate"))
	at.MustOK(t, e2.Revise(e2.Priya, d2)); at.MustOK(t, e2.Submit(e2.Priya, d2))
	if diff := cmp.Diff([]string{"Draft 42"}, e2.InboxTitles(t, e2.Ravi)); diff != "" { t.Error(diff) }
}
func TestApprovalInbox_MyRequests(t *testing.T) { // APR-081
	e := at.New(t)
	d42 := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya); at.MustOK(t, e.Submit(e.Priya, d42))
	d44 := e.NewDoc(t, at.Order, 44, at.Rs(60000), e.Priya); at.MustOK(t, e.Submit(e.Priya, d44)); at.MustOK(t, e.Approve(e.Ravi, d44))
	d45 := e.NewDoc(t, at.Order, 45, at.Rs(60000), e.Priya); at.MustOK(t, e.Submit(e.Priya, d45)); at.MustOK(t, e.Reject(e.Ravi, d45, "Wrong vendor"))
	res, err := e.A.MyRequests(t.Context(), e.Priya, types.Page{Number: 1, Size: 25})
	at.MustOK(t, err)
	got := map[string]approval.RequestItem{}
	for _, r := range res.Rows { got[r.Title] = r }
	if res.Total != 3 || got["Draft 42"].Outcome != "" || got["PO/26-27/00001"].Outcome != "approved" { t.Fatalf("%+v", res.Rows) }
	if r := got["Draft 45"]; r.Outcome != "rejected" || r.Reason != "Wrong vendor" || r.DecidedByName != "Ravi Kulkarni" { t.Errorf("%+v", r) }
}
func TestApprovalInbox_ExcludesOwnUnlessAllowed(t *testing.T) { // APR-082
	e := at.New(t); e.Auth.AddRole(e.Ravi.ID, "Purchase User")
	d := e.NewDoc(t, at.Order, 43, at.Rs(60000), e.Ravi); at.MustOK(t, e.Submit(e.Ravi, d))
	if slices.Contains(e.InboxTitles(t, e.Ravi), "Draft 43") { t.Error("own document listed") }
	e2 := at.New(t); e2.Auth.AddRole(e2.Ravi.ID, "Purchase User")
	e2.RemoveRules(t, at.Order)
	e2.AddRule(t, approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(50000), ApproverType: approval.ApproverRole, Role: "Purchase Manager", AllowSelfApproval: true})
	d2 := e2.NewDoc(t, at.Order, 43, at.Rs(60000), e2.Ravi); at.MustOK(t, e2.Submit(e2.Ravi, d2))
	if !slices.Contains(e2.InboxTitles(t, e2.Ravi), "Draft 43") { t.Error("allowed self-approval must list it") }
}
func TestApprovalInbox_SkipsUnresolvableDocument(t *testing.T) { // Review Focus 4
	e := at.New(t)
	for _, id := range []int64{42, 43} { d := e.NewDoc(t, at.Order, id, at.Rs(60000), e.Priya); at.MustOK(t, e.Submit(e.Priya, d)) }
	e.DropDoc(t, at.Doc{Type: at.Order, ID: 42}) // harness: deletes the aprtest_docs row only
	if diff := cmp.Diff([]string{"Draft 43"}, e.InboxTitles(t, e.Ravi)); diff != "" { t.Error(diff) }
}
```

`Env.DropDoc(t, d)` is added to the harness in this task (deletes the `aprtest_docs` row; test-only).

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/kernel/approval/ -run 'TestApprovalApprove_Stamps|TestApprovalRevise_ToDraft|TestApprovalInbox_' -v`
Expected: FAIL (`not implemented`).

- [ ] **Step 3: Implement `inbox.go`** (and `Env.DropDoc`).

- [ ] **Step 4: Run to verify they pass**, then `go test ./internal/kernel/approval/...`.
Expected: PASS, including the inbox assertions left from Tasks 6 and 7.

- [ ] **Step 5: Commit**

```bash
git add internal/kernel/approval/inbox.go internal/kernel/approval/inbox_test.go internal/kernel/approval/approvaltest
git commit -m "feat(approval): waiting-for-me inbox, my requests and status line"
```

---

## Task 9: Whole-sequence, rollback, concurrency and history tests

**Files:**
- Test: `internal/kernel/approval/sequence_test.go` (test-only task; any production fix it needs goes into the file that owns the behaviour)

**Interfaces:**
- Consumes: the harness and Tasks 6–8.
- Produces: nothing new.

- [ ] **Step 1: Write the tests**

```go
func TestApprovalSubmit_NoRuleApprovesDirectly(t *testing.T) { // APR-015
	e := at.New(t)
	d := e.NewDoc(t, at.Receipt, 43, at.Rs(120000), e.Suresh)
	at.MustOK(t, e.Submit(e.Suresh, d))
	h := e.History(t, d)
	if e.State(t, d) != "approved" || e.Number(t, d) != "GR/26-27/00001" || e.Effects(t, d) != 1 { t.Error("state/number/effects") }
	if _, ok := e.Latest(t, d); ok { t.Error("no request expected") }
	if len(h) != 2 || h[0].Action != approval.HistSubmitted || h[1].Action != approval.HistAutoApproved || h[1].By != e.Suresh.ID { t.Errorf("%+v", h) }
}
func TestApprovalApprove_AppliesEffectsAtomically(t *testing.T) { // APR-052 (APR half: Record and effects share the tx)
	e := at.New(t)
	e.Auth.Grant("Purchase Manager", at.Receipt, "read", "all"); e.Auth.Grant("Purchase Manager", at.Receipt, "approve", "all")
	e.AddRule(t, approval.RuleInput{DocType: at.Receipt, FromAmount: 0, ApproverType: approval.ApproverRole, Role: "Purchase Manager"})
	d := e.NewDoc(t, at.Receipt, 7, at.Rs(120000), e.Suresh)
	at.MustOK(t, e.Submit(e.Suresh, d))
	at.MustOK(t, e.Approve(e.Ravi, d))
	h := e.History(t, d); last := h[len(h)-1]
	if e.Number(t, d) != "GR/26-27/00001" || e.Effects(t, d) != 1 || last.By != e.Ravi.ID || last.Amount != 12000000 { t.Errorf("%+v", last) }
}
func TestApprovalApprove_RevalidatesAndRollsBack(t *testing.T) { // APR-053
	e := at.New(t)
	e.Auth.Grant("Purchase Manager", at.Receipt, "read", "all"); e.Auth.Grant("Purchase Manager", at.Receipt, "approve", "all")
	e.AddRule(t, approval.RuleInput{DocType: at.Receipt, FromAmount: 0, ApproverType: approval.ApproverRole, Role: "Purchase Manager"})
	d := e.NewDoc(t, at.Receipt, 7, at.Rs(120000), e.Suresh)
	at.MustOK(t, e.Submit(e.Suresh, d))
	overReceipt := errors.New("Line 1: received quantity is more than the quantity still pending on the order.")
	if err := e.Approve(e.Ravi, d, at.FailAfterRecord(overReceipt)); !errors.Is(err, overReceipt) { t.Fatal(err) }
	r, _ := e.Latest(t, d); h := e.History(t, d)
	if e.State(t, d) != "pending_approval" || r.Outcome != "" || e.Number(t, d) != "" || h[len(h)-1].Action != approval.HistSubmitted { t.Error("must roll back fully") }
}
func TestApprovalResubmit_LowerAmountSkipsApproval(t *testing.T) { // APR-065
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d)); at.MustOK(t, e.Reject(e.Ravi, d, "Too high")); at.MustOK(t, e.Revise(e.Priya, d))
	e.SetAmount(t, d, at.Rs(45000)); at.MustOK(t, e.Submit(e.Priya, d))
	var acts []string
	for _, r := range e.History(t, d) { acts = append(acts, r.Action) }
	if diff := cmp.Diff([]string{"submitted", "rejected", "revised", "submitted", "approved_no_rule"}, acts); diff != "" { t.Error(diff) }
	if e.Number(t, d) != "PO/26-27/00001" { t.Error(e.Number(t, d)) }
}
func TestApprovalResubmit_HigherAmountHigherRule(t *testing.T) { // APR-066
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d)); at.MustOK(t, e.Reject(e.Ravi, d, "Too high")); at.MustOK(t, e.Revise(e.Priya, d))
	e.SetAmount(t, d, at.Rs(550000)); at.MustOK(t, e.Submit(e.Priya, d))
	r, _ := e.Latest(t, d)
	if r.RuleID != e.R2 { t.Errorf("rule %d", r.RuleID) }
	at.WantErr(t, e.Approve(e.Ravi, d), approval.ErrNotEligible, "Only a user with the Director role can approve Draft 42 (₹5,50,000.00).")
}
func TestApprovalResubmit_FreshRequest(t *testing.T) { // APR-067
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	first, _ := e.Latest(t, d)
	at.MustOK(t, e.Reject(e.Ravi, d, "Too high")); at.MustOK(t, e.Revise(e.Priya, d)); at.MustOK(t, e.Submit(e.Priya, d))
	now, _ := e.Latest(t, d)
	old := e.RequestByID(t, first.ID) // harness helper added in this task
	if now.ID == first.ID || now.Outcome != "" || old.Outcome != "rejected" || e.State(t, d) != "pending_approval" || e.Title(t, d) != "Draft 42" { t.Error("fresh request expected") }
}
func TestApprovalConcurrency_ParallelApproveOnce(t *testing.T) { // APR-072
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	var wg sync.WaitGroup; errs := make([]error, 10)
	for i := range 10 {
		p := e.Ravi; if i%2 == 1 { p = e.Meena }
		wg.Go(func() { errs[i] = e.Approve(p, d) })
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		if err == nil { ok++; continue }
		if !errors.Is(err, approval.ErrNotPending) || !strings.HasPrefix(err.Error(), "PO/26-27/00001 is no longer waiting for approval. It was approved by ") { t.Error(err) }
	}
	approved := 0
	for _, r := range e.History(t, d) { if r.Action == approval.HistApproved { approved++ } }
	if ok != 1 || approved != 1 || e.Effects(t, d) != 1 || e.Number(t, d) != "PO/26-27/00001" || e.Counter(t, "PO/26-27/") != 1 { t.Errorf("ok=%d approved=%d", ok, approved) }
}
func TestApprovalHistory_AppendOnly(t *testing.T) { // APR-083
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d)); at.MustOK(t, e.Reject(e.Ravi, d, "Too high"))
	at.MustOK(t, e.Revise(e.Priya, d)); at.MustOK(t, e.Submit(e.Priya, d)); at.MustOK(t, e.Approve(e.Ravi, d))
	h := e.History(t, d)
	want := []string{"submitted", "rejected", "revised", "submitted", "approved"}
	for i, r := range h {
		if r.Action != want[i] || r.Amount != 6000000 || r.RuleID != e.R1 || r.At.IsZero() || r.By == 0 { t.Errorf("row %d %+v", i, r) }
	}
	err := e.DB.Write(t.Context(), func(tx *db.Tx) error { _, err := tx.SQL().ExecContext(t.Context(), `DELETE FROM approval_history`); return err })
	if err == nil || !strings.Contains(err.Error(), "append-only") { t.Errorf("delete allowed: %v", err) }
}
func TestApprovalAmend_NeedsFreshApproval(t *testing.T) { // APR-087 (APR half: a new id has no history and needs fresh approval)
	e := at.New(t)
	e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya, at.InState("cancelled", "PO/26-27/00042"))
	amended := e.NewDoc(t, at.Order, 61, at.Rs(60000), e.Priya)
	if len(e.History(t, amended)) != 0 { t.Error("amended copy starts with empty APR history") }
	at.MustOK(t, e.Submit(e.Priya, amended))
	r, _ := e.Latest(t, amended)
	if e.State(t, amended) != "pending_approval" || r.RuleID != e.R1 || e.Title(t, amended) != "Draft 61" { t.Error("fresh approval expected") }
}
func TestApprovalCopy_ClearsApprovalFields(t *testing.T) { // APR-088 (APR state is keyed by document id, so a copy has none)
	e := at.New(t)
	orig := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, orig)); at.MustOK(t, e.Approve(e.Ravi, orig))
	cp := e.NewDoc(t, at.Order, 70, at.Rs(60000), e.Priya)
	var s approval.Status
	at.MustOK(t, e.DB.Read(t.Context(), func(tx *db.Tx) (err error) { s, err = e.A.Status(t.Context(), tx, at.Order, 70); return }))
	if _, ok := e.Latest(t, cp); ok || s.Line != "" || e.Title(t, cp) != "Draft 70" { t.Errorf("copy carries approval state: %q", s.Line) }
}
```

Harness additions in this task: `Env.RequestByID(t, id) approval.Request` and `Env.Counter(t, prefix) int`. The DOC halves of APR-052 (PO line received quantity), APR-053 (DOC's real over-receipt message), APR-087 (link to the original) and APR-088 (copy clears DOC columns) run again under the engine driver in Task 12.

- [ ] **Step 2: Run them**

Run: `go test ./internal/kernel/approval/ -run 'TestApprovalSubmit_NoRule|TestApprovalApprove_(Applies|Revalidates)|TestApprovalResubmit_|TestApprovalConcurrency_Parallel|TestApprovalHistory_AppendOnly|TestApprovalAmend_|TestApprovalCopy_' -race -v`
Expected: PASS. If one fails, fix the owning production file (Tasks 6–8), not the test.

- [ ] **Step 3: Commit**

```bash
git add internal/kernel/approval/sequence_test.go internal/kernel/approval/approvaltest
git commit -m "test(approval): lifecycle sequences, rollback, race and append-only history"
```

---

## Task 10: Withdraw (R2) in APR

**Files:**
- Test: `internal/kernel/approval/withdraw_test.go`
- Modify (only if a test needs it): `internal/kernel/approval/inbox.go` (`MyRequests` outcome fields)

**Interfaces:**
- Consumes: `Record(..., "withdraw", ...)` (Task 7), `Inbox`, `MyRequests` (Task 8).
- Produces: nothing new.

- [ ] **Step 1: Write the tests**

```go
func TestApproval_Withdraw_LeavesInbox(t *testing.T) { // APR-093
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	if n := len(e.InboxTitles(t, e.Ravi)); n != 1 { t.Fatalf("count %d", n) }
	at.MustOK(t, e.Withdraw(e.Priya, d))
	if len(e.InboxTitles(t, e.Ravi)) != 0 || len(e.InboxTitles(t, e.Meena)) != 0 { t.Error("must leave every inbox") }
	res, _ := e.A.MyRequests(t.Context(), e.Priya, types.Page{Number: 1, Size: 25})
	r := res.Rows[0]
	if r.Title != "Draft 42" || r.Outcome != "withdrawn" || r.DecidedByName != "Priya Shah" || !r.DecidedAt.Equal(time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)) { t.Errorf("%+v", r) }
}
func TestApproval_Withdraw_ResubmitNewRequest(t *testing.T) { // APR-096
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	first, _ := e.Latest(t, d)
	at.MustOK(t, e.Withdraw(e.Priya, d))
	e.SetAmount(t, d, at.Rs(550000)); at.MustOK(t, e.Submit(e.Priya, d))
	now, _ := e.Latest(t, d)
	if now.ID == first.ID || now.RuleID != e.R2 || e.RequestByID(t, first.ID).Outcome != "withdrawn" { t.Error("new R2 request expected") }
	at.WantErr(t, e.Approve(e.Ravi, d), approval.ErrNotEligible, "Only a user with the Director role can approve Draft 42 (₹5,50,000.00).")
	at.MustOK(t, e.Approve(e.Meena, d))
}
```

- [ ] **Step 2: Run them**

Run: `go test ./internal/kernel/approval/ -run 'TestApproval_Withdraw_(LeavesInbox|ResubmitNewRequest)' -v`
Expected: PASS (fix `inbox.go` if the outcome fields are missing).

- [ ] **Step 3: Commit**

```bash
git add internal/kernel/approval/withdraw_test.go internal/kernel/approval/inbox.go
git commit -m "test(approval): withdraw leaves inboxes and re-submit re-evaluates"
```

---

## Task 11: Inbox, My requests, badge and rule-admin screens

Prerequisites: Tasks 3 and 8 merged into this branch (`types.ParseMoney`, `FormatINR`, `FormatIST` are on main from Wave 1a, CD-53).

**Screen building (CD-49).** The approval-rule screens (and the inbox, My requests and badge) are built from `internal/ui` components once WP1-UI has merged to main. If UI has merged when this task starts, rebase and build from its components directly. If not, write plain templ inside `Layout` now; moving these templates onto UI components after UI merges is a WP3 task (CD-49), so this task doesn't wait for UI. Either way APR owns only `internal/kernel/approval/web/` and adds no styles or components to `internal/ui/` (CD-46).

**Files:**
- Create: `internal/kernel/approval/web/routes.go`, `handlers.go`, `inbox.templ`, `mine.templ`, `badge.templ`, `rules.templ`
- Test: `internal/kernel/approval/web/web_test.go`

**Interfaces:**
- Consumes: `*approval.Approvals` (Inbox, MyRequests, Rules, CreateRule, UpdateRule, SetRuleEnabled, DeleteRule, ApprovableDocTypes), `auth.PrincipalFrom` (CD-12), `types.Page`, `types.ParseMoney`, `types.FormatINR`, `types.FormatIST`, chi.
- Produces:

```go
package web
type Deps struct {
	Approvals *approval.Approvals
	Layout    func(title string, body templ.Component) templ.Component  // internal/ui page shell, wired in Wave 3
	DocURL    func(doctype string, id int64) string                      // DOC form URL, wired in Wave 3
}
func Routes(d Deps) func(r chi.Router)
```

Routes: `GET /approvals` (Waiting for my approval, `?page=`), `GET /approvals/count` (htmx fragment: the number, empty when 0), `GET /approvals/mine` (My requests), `GET /admin/approval-rules`, `GET /admin/approval-rules/new`, `POST /admin/approval-rules`, `GET /admin/approval-rules/{id}`, `POST /admin/approval-rules/{id}`, `POST /admin/approval-rules/{id}/enable`, `.../disable`, `.../delete`. The principal comes from `auth.PrincipalFrom(r.Context())` (CD-12; no local principal hook). No principal → 401 is handled by the AUTH middleware, so handlers treat a missing principal as 403. Errors map only through `doctype.HTTPError` (CD-44): `Status()` is the response status, `Error()` the message, and `FieldErrors()` mark the rule-form fields; `doctype.ErrNotFound` (e.g. an unknown rule id) → 404. Errors render in the page's persistent `role="alert"` region, never as a toast (CD-47). On the rule form, `from_amount` is checked on blur by the server through UI's `/ui/validate/{kind}` with NUM's parser once UI is wired (CD-48); the POST handler parses it with `types.ParseMoney` regardless. Each row is a stacked card (title, label, party, `types.FormatINR` amount, submitted by, submitted on `types.FormatIST`, link via `DocURL`); no tables, no horizontal scroll. Approve/Reject/Withdraw buttons live on the DOC form screen (UI WP), not here.

- [ ] **Step 1: Write the failing tests**

```go
func TestWeb_InboxListsCardsOldestFirst(t *testing.T) {
	e := at.New(t)
	for i, id := range []int64{43, 42} {
		e.Clock.Set(time.Date(2026, 10, 24, 5, i, 0, 0, time.UTC))
		d := e.NewDoc(t, at.Order, id, at.Rs(60000), e.Priya); at.MustOK(t, e.Submit(e.Priya, d))
	}
	body := get(t, e, e.Ravi, "/approvals")
	if i, j := strings.Index(body, "Draft 43"), strings.Index(body, "Draft 42"); i < 0 || j < 0 || i > j { t.Error("oldest first") }
	if got := get(t, e, e.Ravi, "/approvals/count"); strings.TrimSpace(got) != "2" { t.Errorf("badge %q", got) }
}
func TestWeb_RuleFormRefusesNonAdmin(t *testing.T) {
	e := at.New(t)
	code, body := post(t, e, e.Ravi, "/admin/approval-rules", url.Values{"doctype": {at.Order}, "from_amount": {"200000.00"}, "approver_type": {"role"}, "role": {"Director"}})
	if code != 403 || !strings.Contains(pageText(t, body), "You don't have permission to change approval rules. Ask an Administrator.") { t.Error(code, body) } // pageText: the page text via goquery, not raw bytes (HTML escapes the apostrophe; CD-148)
}
func TestWeb_InboxEscapesUserText(t *testing.T) { // Review Focus 5
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya, at.Party(`<script>alert(1)</script>`))
	at.MustOK(t, e.Submit(e.Priya, d)); at.MustOK(t, e.Reject(e.Ravi, d, `<img src=x onerror=alert(1)>`))
	for _, path := range []string{"/approvals/mine"} {
		body := get(t, e, e.Priya, path)
		if strings.Contains(body, "<script>alert(1)") || strings.Contains(body, "<img src=x") { t.Errorf("%s not escaped", path) }
	}
}
```

`get`/`post` build `Routes(web.Deps{...})` on a chi router and put the given user on the request context with `auth.WithPrincipal` (CD-12), `Layout` returning the body unchanged, `DocURL` returning `/doc/{doctype}/{id}`.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/kernel/approval/web/ -v`
Expected: FAIL (`Routes` undefined).

- [ ] **Step 3: Write the templ files and handlers; run `make generate`.**

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/kernel/approval/web/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/kernel/approval/web
git commit -m "feat(approval): inbox, my requests, badge and approval-rule screens"
```

---

## Task 12: DOC-coupled P1 tests (12a in Wave 1, 12b after DOC + NAM merge; WP3 criteria per CD-40)

These 12 P1 cases need the real `doc.Engine` and NAM, so they are WP3's entry and exit criteria (CD-40). Their Go functions are written in Wave 1 (12a) so `go run ./scripts/checktestids APR --exclude-level e2e` finds them; they skip under the reference driver and run for real once 12b lands (by APR if still active, otherwise by WP3).

**Files:**
- 12a Create: `internal/kernel/approval/lifecycle_test.go`; Modify: `internal/kernel/approval/approvaltest/env.go` (`Save`, `Delete`, `Cancel`, `SetState`, `Offered`, `RequireEngine`)
- 12b Create: `internal/kernel/approval/approvaltest/engine.go` (engine driver)

**Interfaces:**
- 12a Produces: `func (e *Env) RequireEngine(t testing.TB)` (`t.Skip("needs APR_DRIVER=engine (real doc.Engine); WP3 exit criterion, CD-40")` under the reference driver); `Env.Save(p, d, changes map[string]string) error`, `Env.Delete(p, d) error`, `Env.SetState(p, d, state doctype.State) error` (save with a `state` value), `Env.Offered(p, d) []doctype.Action`. Under the reference driver these call `RequireEngine`. `Env.Cancel(p, d, reason) error` is added here with a reference stand-in (approved → `cancelled`, effects back to 0) that Task 17's fuzz test reuses; 12b routes it to the engine.
- 12a also Produces `func (e *Env) Version(t testing.TB, d Doc) int64` (APR-091/094). 12b notes (CD-142): `Env.Offered` has no DOC Go source (only WP3's `TestE2E_Doc_ButtonsPerState`), so the engine driver derives it from DOC's guard table for the principal; `Env.Effects` needs a test-only ApplyEffects table on `fixture.Order` defined by the engine driver for APR-091/095.
- 12b Consumes: DOC's Engine constructor (per the DOC plan), the real registry (`doctype.Lookup`/`All`, with `internal/fixture`'s `fixture.Order`/`fixture.Receipt`/`fixture.Claim`, CD-25), real `naming.Service`, `filesfake`, `audit.Default`, the same seeded `authfake` and `testkit.Directory` as the reference driver (CD-56). The engine is the `doctype.InfoSource` (`doc.Engine` embeds it, §5) and fills `Info.Party` (CD-55), so the engine driver passes `Info: engine`, `Lookup: nil`, `All: nil` (registry defaults). No doc-backed adapters are written: the contracts replaced them.
- 12b Produces: `approvaltest.New` selects the driver from `APR_DRIVER` (`engine` default once 12b lands, `ref` for the reference driver). `Env.Submit/Approve/Reject/Revise/Withdraw/Save/Delete/Cancel/SetState` call `doc.Engine.Do` with the matching `doctype.Action`.

- [ ] **Step 1 (12a): Write the tests** (each calls `e.RequireEngine(t)` first and asserts the exact copy below; DOC owns these messages)

```go
func TestApprovalMatch_OnlyAtSubmit(t *testing.T)            // APR-024: save ₹80,000 PO three times → no request, Title "Draft 42", state draft
func TestApprovalSubmit_LocksWithoutNumber(t *testing.T)      // APR-027: submit → pending, Number "", edit → "Draft 42 is waiting for approval and can't be changed. Withdraw it to make changes."
func TestApprovalSubmit_ValidationBeforeRules(t *testing.T)   // APR-028: qty 0 → "Quantity must be more than 0.", no request, no history
func TestApprovalRevise_RejectedIsLocked(t *testing.T)        // APR-062: edit rejected → "Draft 42 was rejected. Click Revise to edit it."
func TestApprovalPending_EditRefused(t *testing.T)            // APR-073: save grand total ₹40,000 → pending-edit message; request amount still 6000000
func TestApprovalPending_ApproverCannotEdit(t *testing.T)     // APR-074: Ravi edits rate → same message; Offered(Ravi) == [approve reject]
func TestApprovalPending_NoDeleteNoCancel(t *testing.T)       // APR-075: "Draft 42 is waiting for approval and can't be deleted. Withdraw it first, then delete it." / "Draft 42 is waiting for approval. Withdraw it first if it needs changes."
func TestApprovalTransition_NoDirectStateWrite(t *testing.T)  // APR-076: "State can only change through Submit, Withdraw, Approve, Reject or Revise."
func TestApproval_Withdraw_SubmitterBackToDraft(t *testing.T) // APR-091: version 3 → 4, request outcome withdrawn, history withdrawn by Priya, audit pending_approval→draft action withdraw, no number, no effects
func TestApproval_Withdraw_NeedsSubmitRight(t *testing.T)     // APR-092: Anil OK (history by Anil Kapoor); Deepak with read only → 403 "You don't have permission to submit Purchase Order. Ask your administrator for a role that allows it."; Kavya restricted to PRJ-TECHPARK → at.WantNotFound (doctype.ErrNotFound, CD-54); nothing changes
func TestApproval_Withdraw_OnlyFromPending(t *testing.T)      // APR-094: the five state messages; state, version, history, audit unchanged
func TestApproval_Withdraw_StaleAfterApprove(t *testing.T)    // APR-095: sequential and 2-goroutine race; "PO/26-27/00001 has already been approved and can't be withdrawn. Cancel and amend it to make changes."; exactly one succeeds
```

Write each body with the Given/When/Then of its case in `04-approvals.md`, asserting every listed observable (state, `version`, number, history rows, `audit` entries, request outcome, message).

- [ ] **Step 2 (12a): Run and commit**

Run: `go test ./internal/kernel/approval/ -run 'TestApprovalMatch_OnlyAtSubmit|TestApprovalSubmit_(Locks|Validation)|TestApprovalRevise_Rejected|TestApprovalPending_|TestApprovalTransition_|TestApproval_Withdraw_(Submitter|NeedsSubmit|OnlyFrom|Stale)' -v`
Expected: all 12 compile and report SKIP with the `RequireEngine` message.
Commit: `git add internal/kernel/approval/lifecycle_test.go internal/kernel/approval/approvaltest && git commit -m "test(approval): DOC-coupled P1 cases, skipped until the engine driver lands"`

- [ ] **Step 3 (12b): Prerequisites, then verify they fail.** DOC (`kernel/doctype`, `kernel/doc`, `kernel/audit`, `fixture/` incl. `fixture.Claim`) and NAM (`kernel/naming`) merged to main; rebase `wp/approval`. Read `docs/superpowers/plans/2026-10-24-wp1-doc.md` for the Engine constructor and how DOC's own tests build it.
Run: the Step 2 command with `APR_DRIVER=engine`.
Expected: FAIL (engine driver missing).

- [ ] **Step 4 (12b): Implement the engine driver.**

- [ ] **Step 5 (12b): Run the whole APR suite on both drivers**

Run: `APR_DRIVER=engine go test -race ./internal/kernel/approval/... && APR_DRIVER=ref go test -race ./internal/kernel/approval/...`
Expected: PASS on engine for every test; on ref, Task 12's and Task 18's tests skip via `RequireEngine` (the only skips in the suite) and everything else passes. A failure on engine that passes on ref means DOC deviates from the §5 protocol (CD-23): record it for the lead; don't change the test. Check first (CD-52): for approve and reject the engine must check `read` visibility (404), then call `CheckApprover`, then run its from-state guard, and must not run `Can(approve)`. If it still runs `Can(approve)` for these two actions, Priya in APR-030/061 gets AUTH's 403 text instead of APR's "Only …" message: that is a DOC deviation to record for the lead, not a test to change. APR-034 and Ravi in APR-045 expect the 404 on both drivers.

- [ ] **Step 6 (12b): Commit**

```bash
git add internal/kernel/approval/approvaltest
git commit -m "test(approval): run the APR suite through the real document engine"
```

---

## Task 13 (hand-off, owned by WP3): Browser test for withdraw

APR-097 is an e2e case. Per CD-40 every e2e case lives under `e2e/` and is owned and run by WP3; it is excluded from APR's checker run (`--exclude-level e2e`). APR provides this text as the spec of the test; WP3 writes it in its worktree. Prerequisites: Wave 3 wiring (`internal/app` serves DOC forms, UI layout and these APR routes).

**Files (WP3):**
- Create: `e2e/approvals_withdraw_test.go` (build tag `e2e`)

- [ ] **Step 1 (WP3): Write `TestE2E_ApprovalWithdraw`** (APR-097; Go name as in `04-approvals.md`, CD-72, CD-74) with chromedp v0.21.0: sign in as Priya (`priya@vardhan.test` / `Correct-Horse-42`), create the reference PO (₹94,164.00), click "Submit for approval", assert a "Withdraw" button; in a second browser context sign in as Ravi and assert the navigation count reads `1`; as Priya click "Withdraw", assert state "Draft" and editable fields; reload as Ravi, assert the count is empty/0 and "Draft 42" is absent from `/approvals`. The 320–1440 px no-horizontal-scroll check on `/approvals` and `/approvals/mine` is covered by WP3's `TestE2E_NoHorizontalOverflow` route sweep (§10.5).
- [ ] **Step 2 (WP3): Run** `make e2e` → Expected: PASS.
- [ ] **Step 3 (WP3): Commit** `git commit -m "test(e2e): withdraw from the browser"`.

---

## P2 task group

Each task: write the tests, run them (`go test ./internal/kernel/approval/... -run '<names>' -v`, expected PASS after any fix in the owning file), commit. They can run in parallel.

### Task 14: Rules P2 (after Task 3; APR-004 also after Task 11)

**Files:** Test: `internal/kernel/approval/rules_p2_test.go`, `internal/kernel/approval/web/rules_p2_test.go`

- [ ] **Step 1: Write the tests**
  - `TestApprovalRule_FromAmountPrecision` (APR-004, in `web`): POST `from_amount=50000.005` as Anil → 422 with "Amount can have at most 2 decimal places." and no rule saved.
  - `TestApprovalRule_RoleMustBeActive` (APR-006): `Deps.Roles` double with role `Site Auditor` exists but inactive → `ErrInvalidRule` "Role Site Auditor is disabled. Choose an active role."
  - `TestApprovalRule_ManagerOnlyForExpenses` (APR-008): PO rule with `ApproverManager` → "Reporting manager approval is only available for Expense Claim. Choose a role."
  - `TestApprovalRule_DisabledIgnored` (APR-011): disable R1; create PO ₹50,000 → Director (succeeds); submit ₹60,000 → request rule = new rule, roles `["Director"]`.
  - `TestApprovalRule_ReenableRechecksDuplicate` (APR-012): add disabled rule B (₹50,000 → Director); `SetRuleEnabled(B, true)` → `ErrDuplicateRule` "A Purchase Order rule from ₹50,000.00 already exists. Edit that rule instead."
  - `TestApprovalRule_ChangesAudited` (APR-014): Anil updates R1 to ₹75,000 → exactly one audit entry by Anil with change `{from_amount, 5000000, 7500000}`.
- [ ] **Step 2: Run** → PASS. **Step 3: Commit** `test(approval): rule P2 cases`.

### Task 15: Decision P2 (after Task 7)

**Files:** Test: `internal/kernel/approval/decide_p2_test.go`

- [ ] **Step 1: Write the tests**
  - `TestApprovalRequest_DisabledRuleKeepsPending` (APR-026): pending under R1; disable R1; state stays pending; Ravi approves OK (and, on a second env, rejects OK).
  - `TestApprovalApprove_RefusalIsAnError` (APR-031): Priya's approve returns a non-nil error and the request stays open.
  - `TestApprovalSelf_AllowedByRule` (APR-038): R2 with self-approval on; Meena (only Director) submits ₹6,00,000 → pending, no number; Meena approves → OK, number `PO/26-27/00001`, last history row `SelfApproved == true`.
  - `TestApprovalExpense_FallbackRoleCanApprove` (APR-046): Deepak's claim designated Priya; Meena approves → OK.
  - `TestApprovalExpense_SelfManagerFallsBack` (APR-050): `e.Dir.SetManager(e.Emp("EMP-006"), e.Emp("EMP-006"))`; Kavya submits a claim → `ManagerUser == 0`, roles `["Director"]`.
  - `TestApprovalApprove_OptionalComment` (APR-054): approve with `Comment("Rate checked against last PO.")` → last history row reason equals it; a second doc approved without comment → reason "".
  - `TestApprovalReject_ReasonLength` (APR-059): 1,001 `a` → `ErrReasonTooLong` "Reason must be 1,000 characters or fewer."; `"  " + 1,000 a + "  "` accepted and stored trimmed.
  - `TestApprovalResubmit_SameApproverMayApprove` (APR-068): Ravi rejects, Priya revises and re-submits, Ravi approves → OK.
- [ ] **Step 2: Run** → PASS. **Step 3: Commit** `test(approval): decision P2 cases`.

### Task 16: Inbox, stuck and history P2 (after Tasks 8 and 11)

**Files:** Create: `Stuck` in `internal/kernel/approval/inbox.go`, `stuck.templ` + route `GET /admin/approvals/stuck` in `web/`. Test: `internal/kernel/approval/inbox_p2_test.go`, `internal/kernel/approval/web/inbox_p2_test.go`

**Interfaces:** Produces `func (a *Approvals) Stuck(ctx context.Context, p auth.Principal) ([]StuckItem, error)` — Administrator only (`ErrNotAdmin` otherwise); open requests whose `eligible` set is empty, `Message` = "No active approver: needs {roles}." (manager name first when designated and still enabled is impossible here, so roles only).

- [ ] **Step 1: Write the tests**
  - `TestApprovalStuck_ListedForAdmin` (APR-043): pending 42; `e.Disable(t, e.Ravi.ID)`, `e.Disable(t, e.Meena.ID)` → Anil's `Stuck` has Draft 42 with "No active approver: needs Purchase Manager or Director."; `e.Auth.AddRole(e.Kavya.ID, "Purchase Manager")` → list empty.
  - `TestApprovalInbox_CountMatches` (APR-078, in `web`): Meena has 3 → `/approvals/count` body `3`; she approves one → `2`.
  - `TestWeb_InboxRowContents` (no ID; handler-level support for APR-079, which is an e2e case owned by WP3 under CD-40 with Go test `TestE2E_ApprovalInboxRowContents` in `e2e/`, CD-74): reference PO Draft 42, party "Shree Steel Traders" (`Info.Party` from the harness `Docs`, CD-55), ₹94,164.00, submitted by Priya at the fixture clock → Ravi's `/approvals` card contains "Purchase Order", "Draft 42", "Shree Steel Traders", "₹94,164.00", "Priya Shah", "24-10-2026 10:30" (`types.FormatIST`, CD-33, CD-51), and `href="/doc/fixture.Order/42"`; with two items, oldest first.
  - `TestApprovalHistory_KeepsRuleAndAmount` (APR-084): submit ₹60,000 (R1), reject, revise, ₹5,50,000, submit (R2), then `UpdateRule(R1 → ₹75,000)` → history submitted rows show (6000000, R1) and (55000000, R2).
- [ ] **Step 2: Run** → PASS. **Step 3: Commit** `feat(approval): stuck approvals list; inbox and history P2 cases`.

### Task 17: Fuzz invariants (after Tasks 7, 10 and 12a)

**Files:** Test: `internal/kernel/approval/fuzz_test.go` (`FuzzApprovalMatch_Monotonic` in package `approval`), `internal/kernel/approval/fuzz_lifecycle_test.go` (package `approval_test`)

- [ ] **Step 1: Write the fuzz tests**
  - `FuzzApprovalMatch_Monotonic` (APR-090): inputs `(seed int64, a, b int64)`; build up to 6 enabled rules with distinct non-negative from amounts from `seed`; with `a ≤ b` (swap if needed, both clamped ≥ 0): matched(b).FromAmount ≥ matched(a).FromAmount when both match, and `ok(a) ⇒ ok(b)`. Seeds: `(1, 4999999, 5000000)`, `(2, 0, 0)`, `(3, 60000000, 6000000)`.
  - `FuzzApproval_StateInvariants` (APR-089): input `[]byte` decoded into up to 30 steps of {submit, withdraw, approve, reject, revise, edit amount, cancel} by one of {Priya, Ravi, Meena, Kavya} over two POs and amounts from {₹1,500, ₹49,999.99, ₹60,000, ₹6,00,000}; errors are allowed, panics are not. After every step: `approved` ⇒ last submit had no matching rule or the last `approved` row's user was eligible and not own; `pending_approval` ⇔ exactly one open request; `draft` ⇒ no open request; `rejected` ⇒ last request reason non-empty; number set ⇒ state in {approved, closed, cancelled}; `Effects > 0` ⇒ state in {approved, closed} (cancel in the reference driver reverses effects to 0 and sets `cancelled`). It uses the reference `Env.Cancel` from Task 12a (pure harness stand-in).
- [ ] **Step 2: Run** `go test ./internal/kernel/approval/ -run 'Fuzz' -v` (seed corpus) and `go test ./internal/kernel/approval/ -fuzz FuzzApprovalMatch_Monotonic -fuzztime 30s` → PASS. **Step 3: Commit** `test(approval): fuzz invariants`.

### Task 18 (gated with Task 12b): DOC-coupled P2 cases

**Files:** Test: `internal/kernel/approval/lifecycle_p2_test.go` (engine driver only; each test starts with `e.RequireEngine(t)`, so it can be written in Wave 1 next to 12a)

- [ ] **Step 1: Write the tests**
  - `TestApprovalRevise_NeedsWriteRight` (APR-064): Suresh with read but no `write` on `fixture.Order` revises rejected Draft 42 → "You don't have permission to change Purchase Order. Ask your administrator for a role that allows it."
  - `TestApprovalRevise_DraftDeletable` (APR-069): revised Draft 42 deleted by Priya → history rows still readable; no open request; no `withdrawn` row.
  - `TestApprovalReject_NotExportable` (APR-070): Deepak's claim Draft 10 rejected → a `list` query on `fixture.Claim` filtered `state = approved` (the core stand-in for "approved, not yet exported") does not return it.
  - `TestApprovalHistory_InDocAudit` (APR-085): after Ravi approves Draft 42, the audit trail has `From pending_approval`, `To approved`, `By Ravi`, a change `number: "" → "PO/26-27/00001"`.
  - `TestApprovalCancel_NoApprovalNeeded` (APR-086): `PO/26-27/00001` approved under R1; `e.Auth.Grant("Director", at.Order, "cancel", "all")` (not in the seeded matrix); Meena cancels with a reason → `cancelled`, no new request row.
- [ ] **Step 2: Run** `APR_DRIVER=engine go test ./internal/kernel/approval/ -run '<names>' -v` → PASS. **Step 3: Commit** `test(approval): DOC-coupled P2 cases`.

---

## Task 19: Final verification

- [ ] **Step 1:** Run `make generate && git diff --exit-code` → no diff (generated code committed).
- [ ] **Step 2:** Run `make test` → PASS (with `APR_DRIVER` unset: engine after Task 12b, ref before, where Tasks 12 and 18 skip).
- [ ] **Step 3:** Run `go test -race ./internal/kernel/approval/...` → PASS.
- [ ] **Step 4:** Run `make lint` → no findings.
- [ ] **Step 5:** Run `go run ./scripts/checktestids APR --exclude-level e2e` (CD-40, CD-41) → nothing missing (Task 12a's functions exist; APR-079 and APR-097 are e2e and excluded). WP3 later runs the checker without exclusions; it also enforces the `TestE2E_` prefix on e2e cases (CD-74), which `04-approvals.md` already uses.
- [ ] **Step 6:** Self-review the diff: only `internal/kernel/approval/**` and `migrations/0040*.sql` changed (`e2e/` is WP3's); no local stand-in for a §5 contract remains (`grep -n 'type Records\|type Catalog\|type Directory\|func inr\|istStamp' internal/kernel/approval/*.go` finds nothing; `grep -rn "PartySource\|ErrNoAccess" internal/kernel/approval` finds nothing; `approvaltest` has no auth or directory double, only the `Docs` InfoSource kept for transactional rollback, CD-56); no test weakened or skipped except the documented `RequireEngine` skips under the reference driver.
- [ ] **Step 7:** Wave 1 exit (CD-40): every P1 case except Task 12's 12 and APR-097 passes under the reference driver. Hand Task 12b, Task 13 and the list of known engine-vs-ref message differences to WP3.

---

## P1 coverage map

| Task | Test cases |
|---|---|
| 3 | APR-001, 002, 003, 005, 007, 009, 010, 013 |
| 4 | APR-017, 018, 021, 022 |
| 5 | APR-041 |
| 6 | APR-016, 019, 020, 023, 025, 040, 044, 047, 048, 051 |
| 7 | APR-029, 030, 032, 033, 034, 035, 036, 037, 039, 042, 045, 049, 055, 057, 058, 060, 061, 071 |
| 8 | APR-056, 063, 077, 080, 081, 082 |
| 9 | APR-015, 052, 053, 065, 066, 067, 072, 083, 087, 088 |
| 10 | APR-093, 096 |
| 12 (12a written in Wave 1, skipping; 12b after DOC + NAM merge; WP3 criteria, CD-40) | APR-024, 027, 028, 062, 073, 074, 075, 076, 091, 092, 094, 095 |
| 13 (e2e, owned by WP3) | APR-097 (`TestE2E_ApprovalWithdraw`) |
| P2: 14 / 15 / 16 / 17 / 18 | 004, 006, 008, 011, 012, 014 / 026, 031, 038, 046, 050, 054, 059, 068 / 043, 078, 084 / 089, 090 / 064, 069, 070, 085, 086 |
| P2 e2e, owned by WP3 | 079 (`TestE2E_ApprovalInboxRowContents`; APR supplies `TestWeb_InboxRowContents` in Task 16 as handler-level support) |

72 P1 cases (APR-001…090 P1 plus 091–097) and 25 P2 cases, each mapped once. Wave 1 exit (CD-40): the 59 P1 cases in Tasks 3–10 pass; Task 12's 12 and APR-097 are WP3's entry and exit criteria.

## Resolved by (Rounds 1–3, CD-74 and Round 4 in `docs/specs/contract-changes.md`)

This plan's contract requests CC-1…CC-8, its two notes and its alignment-pass points (APR 1, 2, 3, 5, 6, 8 in Round 3) are all settled. Nothing is pending from APR; a new need goes to `contract-changes.md` as a "Requested" entry through the lead (§9 rule 2), never edited from this WP.

| Request | Settled by | Outcome and what the plan now does |
|---|---|---|
| CC-1 import cycle | CD-01 | As proposed. `Inbox`/`MyRequests` use `types.Page`/`types.Result[T]`; `approval` doesn't import `list`. |
| CC-2 document facts | CD-05 (+ CD-02) | **Different:** instead of APR-declared `Records`/`Catalog`, a shared `doctype.InfoSource` (DOC's Engine implements it) and the `doctype` registry. Both local interfaces and their harness doubles are deleted. The party name now comes from `Info.Party` (CD-55, see below), so `PartySource` is gone too. |
| CC-3 Requirement fields | CD-24 | The "cleaner alternative" was adopted: exported `Amount` and `Requester`. All unexported `Requirement` fields are gone; `Open` re-reads creator, claimant, names and `allow_self` in the same transaction. |
| CC-4 audit fake | CD-03 | As suggested: `Deps.Audit` is an `audit.Writer` (nil → `audit.Default`); tests use `audit/auditfake`. |
| CC-5 call protocol | CD-23 | As proposed, now in §5. `Record` takes `doctype.Action`; APR's own action constants are deleted. |
| CC-6 claim fixture | CD-25 | As proposed: DOC adds `fixture.Claim` (label "Expense Claim", employee-based, Amount = sum of lines). |
| CC-7 manager rights | CD-11 | **Different in form:** a new AUTH scope `team` (not "reports"); the seed gives Employee `read` + `approve` with scope `team` on Expense Claim / `fixture.Claim`, so the plan has no rights assumption left. (b) In-test grants for APR-052/053/086 stay. (c) Visibility is checked first, so the "Only {Manager} or …" message reaches only users who can see the claim; APR-045: Ravi → 404 (CD-52, below), Anil → the "Only Priya Shah or …" message. |
| CC-8 (a) formatters | CD-33 | `types.FormatINR`/`FormatIST`/`ParseMoney` are §5 contracts; private `inr()`/`istStamp()` deleted. |
| CC-8 (b) timestamp format | CD-33 | **Different from the plan,** which followed APR-056: the one format is `24-10-2026 14:45`. APR-056 now expects "Approved by Ravi Kulkarni on 24-10-2026 14:45" and the inbox card shows `24-10-2026 10:30`. |
| CC-8 (c) directory | CD-13 | As proposed in substance: `auth.Directory`, every method taking `tx`, implemented by MST. APR's own `Directory`/`Employee`/`EmployeeID` are deleted; APR uses `ManagerChain`, `UserForEmployee`; `team` scope uses `EmployeeForUser`/`DirectReports`. In tests the directory is the shared `testkit.Directory` (CD-56, below). |
| Process note (exit criteria) | CD-40, CD-41 | As proposed: Wave 1 exit excludes Task 12 (WP3 criteria) and e2e cases; checker is `go run ./scripts/checktestids APR --exclude-level e2e`. **Addition:** Task 12's functions are written in Wave 1 and skip under the reference driver so the checker finds them. |
| Wording note (APR-055/057 vs 071) | CD-26 | As proposed: "It was {outcome} by {Name}" only when a person's decision produced the current state, else "It is {State}". The APR-002 "Ask an Administrator." note is settled by CD-66 (below). |
| APR 1 (order of checks) | CD-52 | DOC checks `read` visibility first (hidden → 404), then `CheckApprover`, then its from-state guard; no `Can(approve)` for approve/reject. The reference driver follows that order; `ErrNoAccess` is deleted; APR-034 and Ravi in APR-045 expect `doctype.ErrNotFound` (404) with no APR text; APR-030/061 keep APR's "Only …" message on both drivers. |
| APR 2 (formatters) | CD-53 | NUM's `types` merges first (Wave 1a). No formatter case is deferred; the "rebase on NUM's formatters later" step is gone; APR uses `types.FormatINR`/`FormatIST` and has no private display helper (`ts`/`parseTS` are storage text only). |
| APR 3 (party on inbox cards) | CD-55 | `doctype.Info.Party`. `PartySource` and `Deps.Parties` are deleted; the harness `Docs` fills `Party`. |
| APR 5 (principal signature, e2e name) | CD-72, CD-74 | `testkit.Principal(t, email)`. APR-097 → `TestE2E_ApprovalWithdraw`, APR-079 → `TestE2E_ApprovalInboxRowContents`, both owned by WP3. |
| APR 6 (not-found sentinel, constants, fakes) | CD-54, CD-56 | `doctype.ErrNotFound`, `doctype.Action*`/`State*`. `approvaltest` uses `authfake` (seeded with `Grant`/`AddRole`/`Restrict`) and `testkit.Directory` (`authfake.NewDirectory()`) instead of its own doubles. It keeps its own `Docs` InfoSource over `aprtest_docs` rather than `docfake.SetInfo`, because those rows must roll back and serialise with the `db.Write` (APR-053, APR-072). |
| APR 8 (wording) | CD-66 | "Ask an Administrator." everywhere; `ErrNotAdmin` already uses it. |
| Round 4: stale stub name | CD-92 | WP0's stub file is `internal/kernel/approval/contract.go`, not `approval.go`; the file map and Task 2 modify it. |
| Round 4: `authfake` gaps | CD-86 | `authfake` gains `RemoveRole(u, role)` (replaces every former `SetRoles` use; `Env.RemoveRole` calls it), `UsersWithRight` skips users added with `Enabled: false` (APR relies on it and filters nothing itself), and `Grant` accepts the wildcard DocType `"*"` (the harness seeds the Administrator row with it). `TestEnv_SeedAndDoubles` pins all three. |
| UI requests | CD-44, CD-47, CD-48, CD-49, CD-51 | `UserError` implements `doctype.HTTPError` and the screens map errors only through it; errors show in the `role="alert"` region; `from_amount` is checked on blur by the server; the approval-rule and inbox screens are built from `internal/ui` components once WP1-UI merges (otherwise moved later by WP3); APR-079 shows `24-10-2026 10:30` in the CD-33 format. |

## Open issues for the lead

1. **The DOC plan's `Do` pipeline doesn't follow CD-52 yet.** `docs/superpowers/plans/2026-10-24-wp1-doc.md` step 5 still runs `Auth.Can(ctx, p, doctype, right(a), …)` for every action, including approve and reject, before `CheckApprover` (step 6). Under CD-52, approve and reject need a `read` visibility check (404) instead. Until the DOC plan changes, APR-030 and APR-061 will fail under the engine driver (Task 12b) with AUTH's 403 text.
2. **`authfake` coverage (CD-56, CD-86).** CD-86 settles `RemoveRole`, the disabled-user skip in `UsersWithRight` and `Grant("*", …)`. Still unconfirmed in WP0's plan: whether `Can`/`UsersWithRight` apply role grants with `own`/`team` scope through the directory, and whether a `Directory` user link can be removed (APR-048). Task 2 runs the gap check and asks before adding any wrapper.
