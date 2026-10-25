# Document lifecycle & audit (DOC)

## Scope
DOC owns the generic engine every document type (PO, GRN, Expense, and later Delivery Challan, Sales Invoice, …) runs on: create, save, validation pipeline, child lines, the F5 state machine (submit, withdraw, approve, reject, revise, cancel, close, amend, delete), immutability after submit, linked-document checks, cross-document roll-ups ("effects"), optimistic concurrency, one-transaction-per-action atomicity, and the F6 append-only audit trail.
It defines the **hook interface** modules implement (see "Hooks contract" below). Core tests run against fixture DocTypes compiled only into the test binary: `fixture.Order` (header + lines with `qty`, roll-up `received_qty`), `fixture.Receipt` (lines that point at `fixture.Order` lines) and `fixture.Claim` (employee-based, approvable, labelled "Expense Claim", used by APR; CD-25). They mirror PO, GRN and Expense Claim. Examples below use PO/GRN wording for readability. The DocType registry lives in `internal/kernel/doctype` (CD-02); `doc` re-exports its names.
It relies on: **AUTH** for `auth.Service.Can(ctx, p, doctype, Right, rec)`, record rules, the restriction checks on save (`CheckValues`, `CheckLink`, CD-70) and user names (`auth.Service.User`); **NAM** for document numbers (assigned at first approval), including the amend suffix, display labels (`naming.Label`), the number-field guard and the amendment limit (CD-28); **APR** for deciding whether submit needs approval and who may approve or reject, following the §5 call protocol (CD-23); **MST** for master lookups (`not found`, `disabled`, `blocked`) through each master DocType's `MasterLookup` (CD-21); **FILE** for copying attachments on amend; **NUM** for money/qty precision, date rules and IST display.
Document numbers follow `00-fixtures.md` (`PO/26-27/00001`, `GR/26-27/00001`). NAM owns the format. A document gets its number only in the transaction that first enters `approved` (approve, or submit when no rule applies). Until then (draft, pending_approval, rejected) it has no number and displays as `Draft {id}`, e.g. `Draft 42` (spec R1).

### Hooks contract (what a module implements)
```go
// doctype.Hooks (re-exported as doc.Hooks), as frozen in spec §5
type Hooks interface {
    Validate(ctx context.Context, tx *db.Tx, id int64) []FieldError // every save, submit, approve
    BeforeSubmit(ctx context.Context, tx *db.Tx, id int64) error     // submit and approve, after Validate
    ApplyEffects(ctx context.Context, tx *db.Tx, id int64) error     // only on entering `approved`
    ReverseEffects(ctx context.Context, tx *db.Tx, id int64) error   // only on approved → cancelled
    Amount(ctx context.Context, tx *db.Tx, id int64) (types.Money, error) // passed to APR Evaluate
    Date(ctx context.Context, tx *db.Tx, id int64) (types.Date, error)    // passed to NAM Assign
}
// Declared on doctype.DocType (Links []LinkSpec, RollUps []RollUp) and registered once with doctype.Register (= doc.Register).
// Core uses Links to check source documents and to find blockers on cancel. Declared on the source DocType;
// with OnLine, Field is a line field holding a Target line id:
type LinkSpec struct{ Field, Target string; OnLine bool }
// Roll-ups are declared on the source DocType, not hand-coded (JoinField = source line field holding a target line id):
type RollUp struct{ TargetDocType, TargetField, SourceField, JoinField string }
// Masters register as DocTypes with Master: a doctype.MasterLookup (CD-21); Link fields name the target DocType.
// Every action runs through doc.Engine.Do(ctx, p, doctype, id, action, in). The Engine also implements
// doctype.InfoSource (CD-05), References (CD-22) and Load (CD-43: a doc.Record in the form value format).
// Every error DOC creates implements doctype.HTTPError (CD-44); not found wraps doctype.ErrNotFound (CD-54).
```
Core recomputes every declared `RollUp` as `SUM(source field) over source lines whose parent is approved or closed`, inside the same transaction, after `ApplyEffects` or `ReverseEffects`.

## Decisions
| ID | Question | ERPNext/Frappe | Odoo | Decision | Why |
|---|---|---|---|---|---|
| D-DOC-01 | How is state stored? | `docstatus` 0/1/2 plus a separate free `status` (Draft, To Receive, Closed, On Hold…) (`frappe/frappe/model/document.py:1721`, `erpnext/erpnext/controllers/status_updater.py:274`) | One `state` selection: draft, sent, to approve, purchase, cancel (`odoo/addons/purchase/models/purchase_order.py:108`) | One `state` column with exactly the F5 values. Progress such as "partly received" is a label computed for display, never a state. Tryton also uses one state with an explicit transition set (`tryton/modules/purchase/purchase.py:301`). | One machine for all modules. Two parallel status fields are the main source of ERPNext status bugs. |
| D-DOC-02 | When are effects (roll-ups) applied? | `on_submit` (`erpnext/erpnext/buying/doctype/purchase_order/purchase_order.py:435`) | On `button_approve`. `to approve` has no effects (`purchase_order.py:698-721`) | Only on entering `approved`, whether that comes from approval or from an auto-approved submit. `pending_approval` and `rejected` never have effects. | This is F5. It matches Odoo's two-step behaviour. |
| D-DOC-03 | Does approve re-run validation? | No separate approve step. Authorization runs inside `on_submit` | `button_confirm` checks before approving | Yes. Approve re-runs `Validate` and `BeforeSubmit`, then `ApplyEffects`, all in one transaction. | Masters, source documents and quantities may have changed while the document waited. |
| D-DOC-04 | Which later documents block cancelling a source document? | Cancel: only submitted (docstatus 1) linked documents block; drafts don't (`frappe/frappe/model/delete_doc.py:395`) | PO cancel is blocked by any non-draft, non-cancelled bill (`purchase_order.py:729-730`) | Later documents in `pending_approval`, `approved` or `closed` block. Documents in `draft`, `rejected` or `cancelled` don't. A draft that points at a cancelled source fails when it is submitted (DOC-038). **Flag:** F5 says "non-cancelled". We read that as "submitted and not cancelled", as both sources do. | Drafts are unfinished work. Blocking on them lets one forgotten draft freeze a PO. |
| D-DOC-05 | Is the link check done before or after reversing effects? | After `on_cancel`, which relies on rollback (`document.py:2209-2213`; `erpnext/erpnext/stock/doctype/purchase_receipt/test_purchase_receipt.py:7880`) | Before the write (`purchase_order.py:723-731`) | **Before** `ReverseEffects`, and the whole action is still one transaction. | It fails fast with a clear message, and rollback remains the safety net. |
| D-DOC-06 | What may be deleted? | Drafts and cancelled documents. A copy goes to "Deleted Document" (`delete_doc.py:307-325`, `:253`) | Only cancelled POs (`purchase_order.py:469-473`) | Only `draft` (F5). Rejected, pending, approved, closed and cancelled documents are never deleted. The audit trail of a deleted draft is kept, with a full snapshot. | Cancelled documents are evidence. Odoo deletes the history with the record (`odoo/addons/mail/models/mail_thread.py:427-432`), which we must not do (F6). |
| D-DOC-07 | Can a submitted document be edited? | Fields marked `allow_on_submit` can be, and ERPNext "Update Items" changes qty and rate after submit (`document.py:3830`; `test_purchase_order.py:339`) | Confirmed PO lines can be edited, with a chatter note (`odoo/addons/purchase/models/purchase_order_line.py:433-462`) | **No** in v1 (F5, kept by spec R7). Only system roll-up fields change, and only through effects. **Flag:** ERPNext and Odoo both allow changing qty and rate after approval. Without this, a user who wants to raise a PO's qty must cancel every GRN, then cancel, amend and re-approve the PO. "Update lines after approval" is listed as *later*. | F5 is fixed, and immutability keeps the audit simple. |
| D-DOC-08 | Close and reopen | `Closed` plus `Re-open` and `On Hold`. Only submitted POs (`purchase_order.py:657-664`; `test_purchase_order.py:2301`) | Lock and unlock (`test_purchase.py:1190`) | `approved → closed` by a user with the `submit` right, with a reason. `closed` is **terminal**: no reopen, no cancel, no edit (F5, kept by spec R6). **Flag:** every source supports reopening. Reopen (`closed → approved`, manager only, audited) is listed as *later* in spec §11. On Hold is *later*. | F5. |
| D-DOC-09 | Can a later document be cancelled when its source is closed? | No. GRN `on_cancel` refuses when the PO is Closed (`erpnext/erpnext/stock/doctype/purchase_receipt/purchase_receipt.py:454`, `erpnext/erpnext/controllers/stock_controller.py:615`) | — | **Yes.** Cancelling a GRN against a closed PO is allowed. The PO's roll-ups are recomputed and the PO stays `closed`. | ERPNext can block this because it can reopen. With a terminal `closed`, blocking would make a wrong GRN impossible to fix. |
| D-DOC-10 | How is optimistic concurrency checked? | `modified` timestamp compared on save (`document.py:1688-1719`). It needed a fix for saves within the same second (`frappe/frappe/tests/test_document.py:237`) | Not checked in the sparse clone | Integer `version` column. Every write runs `UPDATE … WHERE id=? AND version=?` and increments it. Zero rows updated means a stale copy. | No timestamp-resolution bugs. It is trivial with sqlc. |
| D-DOC-11 | What does the audit trail cover? | Opt-in `track_changes`. No entry on insert. Stores formatted values. Fields can be ignored (`document.py:2343-2366`; `frappe/frappe/core/doctype/version/version.py:104`; `test_version.py:229`) | Opt-in `tracking=True` fields. A creation message is logged. Tracking messages can't be edited (`mail_thread.py:350`, `:531`) | Every field of every document type, always on. One entry per successful action: `create` (all non-empty values), `save` (changed fields), each transition (from, to, reason), `delete` (snapshot), and roll-up changes (with the cause). Values are stored raw and formatted at display. Excluded: `version`, `updated_at`, `updated_by`. | F6. "Opt-in" is how fields go unaudited by mistake. |
| D-DOC-12 | Is the audit append-only? | Version rows can be deleted like other records | Tracking messages can't be modified (`mail_thread.py:531-532`) but are deleted with the record | SQLite `BEFORE UPDATE` and `BEFORE DELETE` triggers on `audit_log` call `RAISE(ABORT)`. No app code path updates or deletes audit rows. | F6. It is enforced at the lowest layer. |
| D-DOC-13 | How are roll-ups updated? | Recomputed as `SUM` over submitted source rows, not incremented (`status_updater.py:656-714`) | Computed fields | Recompute from approved and closed source lines on every apply or reverse. | Idempotent. It can't drift. |
| D-DOC-14 | Amend rules | Allowed only from cancelled (`document.py:1173-1178`). Copies attachments (`:1180`). Amendments may chain | Reset to draft after cancel (Tryton `cancelled → draft`, `purchase.py:301-312`) | Only from `cancelled`. Creates a new `draft` with `amended_from`. Copies header and lines (new line IDs). Resets roll-ups, approval and reason fields. The new draft has no number (it shows `Draft {id}`); NAM gives it `PO/26-27/00001-1` when it is first approved (changed by spec R1). At most **one** amendment per cancelled document that isn't deleted. | One clear successor per cancelled document. |
| D-DOC-15 | Lines required? Empty lines? | Items table mandatory: "Error: Data missing in table Items" (`document.py:3497`). Empty rows are removed client-side | — | A draft may be saved with no lines (changed by spec R8, so a wizard can save the header on step 1). **Submit** needs at least one line. Lines with every user field blank are dropped by the server before validation. A partly filled line is validated normally. Odoo and Tryton allow an empty draft (`tryton/modules/purchase/tests/scenario_purchase_empty.rst:22-39`). | Wizards save each step as a draft; a document with no lines is only a mistake once it is submitted. |
| D-DOC-16 | Line order and numbering | `idx` 1..n, renumbered on insert and remove (`document.py:2889-2962`) | `sequence` integer with gaps (`purchase_order_line.py:27`) | `line_no` 1..n, contiguous, renumbered on every save in the submitted order. Line `id` is stable across saves. | Users quote "line 3". IDs keep audit and links stable. |
| D-DOC-17 | Maximum lines | None found | None found | 500 lines per document. | Protects the HTMX form and PDF. Real POs here have under 100 lines. |
| D-DOC-18 | Links to disabled masters | Blocked on validate (save and submit), e.g. disabled supplier (`erpnext/erpnext/accounts/party.py:904-912`; `test_supplier.py:94`), disabled item (`erpnext/erpnext/stock/doctype/item/item.py:1509`). Cancel skips link validation (`document.py:1952`) | Not checked in clone | Blocked on save, submit and approve. Not checked on cancel, close, print or view. | Old documents stay valid, and new use is stopped. |
| D-DOC-19 | Reason on cancel and close | Not required | Not required | Required. 3 to 500 characters. Stored in the audit entry and on the document. | A 20-person company has no other control on who undoes what. |
| D-DOC-20 | How are validation errors reported? | Mandatory errors are collected. Others stop at the first error (`document.py:1802-1823`) | First error | `Validate` returns all field-level errors at once (header and rows). Cross-document checks (source state, over-limit) stop at the first failure. | One round-trip for form errors. |
| D-DOC-21 | Transaction boundary | Request-level transaction with savepoints (`frappe/frappe/tests/test_db.py:528`) | Request-level transaction. Tracking is written at precommit (`odoo/addons/mail/models/mail_track_mixin.py:76-89`) | Each action is one SQLite `BEGIN IMMEDIATE` transaction that covers the document, lines, effects on other documents, number allocation and audit. | Gives all-or-nothing behaviour, and the write lock serialises over-limit checks. |
| D-DOC-22 | Draft "discard" (draft → cancelled without delete) | Exists (`document.py:2110-2127`) | — | **Never.** Drafts are deleted, and the audit snapshot keeps them. | One way to get rid of a draft. |
| D-DOC-23 | When does a document get its number? | — | — | Inside the transaction that first enters `approved` (approve, or submit when no rule applies), via NAM `Assign`. Draft, pending_approval and rejected documents have no number and show `Draft {id}`. Withdraw and revise never assign one. An amendment gets `-1`…`-9` when it is approved (spec R1). | Gapless numbers: a number is never used by a document that is later withdrawn, revised or deleted. |
| D-DOC-24 | Can the requester pull back a pending document? | — | — | **Yes: `withdraw`, `pending_approval → draft`,** right `submit` (submitter or anyone with `submit` on the document). One transaction: APR closes the open request as withdrawn, audit transition `pending_approval → draft` with action `withdraw`, `version` + 1. No effects, no number, no reason required. A later submit opens a new approval request (spec R2). | Without it the only way to fix a pending document is to ask the approver to reject it. |

## Test cases

### DOC-001 · A new document starts as draft with owner, timestamps and version 1
- **Priority:** P1
- **Level:** integration
- **Given** user `priya@vardhan.test` creates a `fixture.Order` with supplier `SHREE` and one line (item `MS-ANGLE-50`, qty 1,200.000 kg) **When** it is saved at the fixture clock 2026-10-24T05:00:00Z **Then** `state = "draft"`, `version = 1`, `created_by = updated_by = priya`, `created_at = updated_at = 2026-10-24T05:00:00Z`, the document has no number and displays as `Draft {id}` (e.g. `Draft 42`), and the line has `line_no = 1` and a non-zero `id`.
- **Go test:** `TestDoc_Create_DraftDefaults`
- **Sources:** `frappe/frappe/tests/test_document.py:83` (`test_insert`), `frappe/frappe/model/document.py:1352` (`set_user_and_timestamp`), `odoo/addons/purchase/models/purchase_order.py:108` (state default draft)

### DOC-002 · Field defaults fill only missing values
- **Priority:** P2
- **Level:** unit
- **Given** `fixture.Order.priority` defaults to `"normal"` and the line field `uom` defaults to `"Nos"` **When** an order is created without `priority` and with one line that sets `uom = "Bag"`, plus a second line without `uom` **Then** `priority = "normal"`, line 1 `uom = "Bag"` and line 2 `uom = "Nos"`. An explicit `discount_pct = 0` stays 0 and is not replaced by a non-zero default.
- **Go test:** `TestDoc_Create_DefaultsOnlyFillMissing`
- **Sources:** `frappe/frappe/tests/test_document.py:83` (`test_insert`, default `send_reminder`), `frappe/frappe/tests/test_document.py:122` (`test_website_route_default`, child default)

### DOC-003 · State, version, owner and roll-up fields can't be set from user input
- **Priority:** P1
- **Level:** integration
- **Given** `priya` creates an order whose request includes `created_by=3` and `created_at=2026-04-01T00:00:00Z` **Then** `created_by = priya` and `created_at` is the server time (2026-10-24T05:00:00Z on the fixture clock). **Given** that draft at `version = 1` **When** a save request includes `state=approved`, `created_by=admin`, `created_at=2026-04-01T00:00:00Z`, `version=99` and line `received_qty=10` **Then** those inputs are ignored. Afterwards `state = "draft"`, `created_by` and `created_at` are unchanged, `version = 2` and `received_qty = 0`. An audit entry exists only for the fields the user may edit. **When** a save supplies a value for a `ReadOnly` computed field (CD-79), e.g. the claim's `total = 99999.00` **Then** it is refused with `total: "Total is calculated and can't be entered."` and nothing is written; the unchanged current value (as `Engine.Load` returns it) or an empty value is accepted. DOC owns these `created_by` assertions (CD-67; AUTH-083 is AUTH's form-level share).
- **Go test:** `TestDoc_Save_IgnoresSystemFields`
- **Sources:** `frappe/frappe/tests/test_document.py:708` (`test_non_submittable_doctype_docstatus_transition`), `frappe/frappe/tests/test_document.py:714` (`test_skip_docstatus_validation_flag`)

### DOC-004 · Every allowed transition succeeds (table-driven)
- **Priority:** P1
- **Level:** integration
- **Given** one document per starting state **When** each allowed action runs **Then** the resulting state is: draft+save→draft; draft+submit (no rule)→approved; draft+submit (rule)→pending_approval; pending_approval+approve→approved; pending_approval+reject→rejected; pending_approval+withdraw→draft; rejected+revise→draft; approved+cancel→cancelled; approved+close→closed; cancelled+amend→(original stays cancelled, new draft); draft+delete→(gone).
- **Go test:** `TestDoc_Transitions_Allowed`
- **Sources:** `frappe/frappe/model/document.py:1721-1774` (`check_docstatus_transition`), `tryton/modules/purchase/purchase.py:301-312` (`_transitions`), `frappe/frappe/tests/test_document.py:156` (`test_discard_transitions`)

### DOC-005 · Every other transition is refused with a fix-it message (table-driven)
- **Priority:** P1
- **Level:** integration
- **Given** one document per starting state **When** a disallowed action runs **Then** nothing changes (state, version, audit count) and the error is:
  - approve or reject in any state except pending_approval (CD-26; same as APR-055, APR-057 and APR-071): when a person's approve, reject or withdraw produced the current state → "<name> is no longer waiting for approval. It was <approved|rejected|withdrawn> by <Name>." (e.g. "PO/26-27/00001 is no longer waiting for approval. It was approved by Ravi Kulkarni."; DOC gets the name from `auth.Service.User`); otherwise → "<name> is not waiting for approval. It is <State>." (e.g. "Draft 42 is not waiting for approval. It is Draft.", an auto-approved "GR/26-27/00001 is not waiting for approval. It is Approved.")
  - draft+withdraw → "Draft 42 hasn't been submitted, so there is nothing to withdraw."
  - draft+cancel → "Only approved documents can be cancelled. Delete the draft instead."
  - draft+close, pending_approval+close, rejected+close, cancelled+close → "Only approved documents can be closed."
  - draft+amend, pending_approval+amend, rejected+amend → "Only cancelled documents can be amended."
  - approved+amend → "Only cancelled documents can be amended. Cancel PO/26-27/00001 first."
  - pending_approval+save/submit → "Draft 42 is waiting for approval and can't be changed. Withdraw it to make changes." (same as APR-027, APR-073)
  - pending_approval+cancel → "Draft 42 is waiting for approval. Withdraw it first if it needs changes."
  - approved+save/submit → "PO/26-27/00001 is approved and can't be edited. Cancel and amend it to make changes."
  - approved+withdraw (including a stale second click after an approver approved) → "PO/26-27/00001 has already been approved and can't be withdrawn. Cancel and amend it to make changes."
  - rejected+submit/cancel/withdraw → "Draft 42 was rejected. Revise it to move it back to draft."
  - closed+save/cancel/withdraw/amend → "PO/26-27/00001 is closed and can't be changed."
  - cancelled+save/submit/cancel/withdraw → "PO/26-27/00001 is cancelled and can't be changed. Amend it to make a corrected copy."
  - any state except draft + delete → see DOC-062.
- **Go test:** `TestDoc_Transitions_Refused`
- **Sources:** `frappe/frappe/model/document.py:1749-1774` ("Cannot change docstatus from 0 (Draft) to 2 (Cancelled)", "Cannot edit cancelled document"), `frappe/frappe/tests/test_document.py:156` (`test_discard_transitions`), `erpnext/erpnext/buying/doctype/purchase_order/test_purchase_order.py:2301` (`test_update_status_accepts_only_hold_close_and_reopen_on_submitted_po`)

### DOC-006 · Submit without an approval rule goes straight to approved and applies effects once
- **Priority:** P1
- **Level:** integration
- **Given** an approved order line with qty 10 and `received_qty = 0`, and a draft receipt for 4 against it, and APR returns "no rule applies" **When** the receipt is submitted **Then** receipt `state = "approved"`, it gets its number `GR/26-27/00001` in the same transaction (NAM `Assign` called once), APR is called `Evaluate`, `Open`, then `Record(approve)` without `CheckApprover` (CD-23), order line `received_qty = 4`, and exactly one transition audit entry `draft → approved` (with `auto_approved = true`) is written on the receipt.
- **Go test:** `TestDoc_Submit_NoRule_AutoApproves`
- **Sources:** `erpnext/erpnext/buying/doctype/purchase_order/test_purchase_order.py:94` (`test_ordered_qty`), `odoo/addons/purchase/models/purchase_order.py:708-721` (`button_confirm` → `button_approve` when allowed)

### DOC-007 · Submit with an approval rule locks the document without applying effects
- **Priority:** P1
- **Level:** integration
- **Given** a receipt for 4 against an order line with qty 10, and APR says approval is needed **When** it is submitted **Then** `state = "pending_approval"`, order line `received_qty` stays 0, `ApplyEffects` was not called, and the receipt has no number (it shows `Draft 42`; NAM `Assign` was not called).
- **Go test:** `TestDoc_Submit_WithRule_PendingNoEffects`
- **Sources:** `odoo/addons/purchase/models/purchase_order.py:718-720` (`state = 'to approve'`), `odoo/addons/purchase/models/purchase_order.py:1332` (`_approval_allowed`)

### DOC-008 · Approve re-validates, then applies effects
- **Priority:** P1
- **Level:** integration
- **Given** the pending receipt from DOC-007 **When** an approver approves it **Then** the call order is DOC's visibility check (`read`), APR `CheckApprover` (before DOC's from-state guard; DOC runs no `Can(approve)`, CD-23, CD-52), `Validate`, `BeforeSubmit`, NAM `Assign`, `ApplyEffects`, the roll-up recompute, then APR `Record(approve)`. `state = "approved"`, the receipt is numbered `GR/26-27/00001`, and order line `received_qty = 4`.
- **Go test:** `TestDoc_Approve_RevalidatesThenApplies`
- **Sources:** `odoo/addons/purchase/models/purchase_order.py:698-702` (`button_approve`), `frappe/frappe/model/document.py:2144-2174` (`run_before_save_methods` order)

### DOC-009 · Reject keeps the reason, and revise moves the document back to draft
- **Priority:** P1
- **Level:** integration
- **Given** a pending order **When** the approver rejects with reason "Rate for MS-ANGLE-50 is ₹62.5000, agreed rate is ₹60.0000." **Then** `state = "rejected"`, the reason is stored on the document and in the audit entry, no effects run, and no number is assigned (it shows `Draft 42`). **When** the owner then chooses Revise **Then** `state = "draft"`, it still has no number, the fields can be edited again, and the reject reason still shows in history.
- **Go test:** `TestDoc_Reject_ThenEditToDraft`
- **Sources:** BRIEF F5. No direct source test; Odoo `button_draft` resets to draft (`odoo/addons/purchase/models/purchase_order.py:704-706`)

### DOC-010 · Resubmitting after a reject runs the full submit path again
- **Priority:** P1
- **Level:** integration
- **Given** a rejected order revised back to draft and corrected to rate ₹60.0000 **When** it is submitted **Then** `Validate` and `BeforeSubmit` run, APR is asked again, and the state becomes `pending_approval` or `approved` according to the rule. A number is assigned only if it reaches `approved`.
- **Go test:** `TestDoc_Resubmit_AfterReject`
- **Sources:** BRIEF F5

### DOC-011 · A double submit (double-click) is refused without side effects
- **Priority:** P1
- **Level:** integration
- **Given** a draft order with version 3 **When** two submit requests with version 3 arrive one after the other **Then** the first succeeds. The second gets "Draft 42 has already been submitted." No number is allocated, no second approval request is opened, and no second audit entry or effect is written. For a receipt with no rule (auto-approved by the first submit) the message names its number, "GR/26-27/00001 has already been submitted.", and exactly one number was allocated.
- **Go test:** `TestDoc_Submit_Twice`
- **Sources:** `odoo/addons/purchase/models/purchase_order.py:709-711` (`button_confirm` skips non-draft), `frappe/frappe/tests/test_document.py:231` (`test_conflict_validation`)

### DOC-012 · Cancel reverses effects
- **Priority:** P1
- **Level:** integration
- **Given** order line qty 10 with approved receipts R1 (4) and R2 (8), so `received_qty = 12` (over-receipt allowed by the module in this fixture) **When** R2 is cancelled with reason "Wrong vehicle, goods returned at gate." **Then** R2 `state = "cancelled"` and order line `received_qty = 4`. **When** R1 is cancelled too **Then** `received_qty = 0`.
- **Go test:** `TestDoc_Cancel_ReversesEffects`
- **Sources:** `erpnext/erpnext/buying/doctype/purchase_order/test_purchase_order.py:94` (`test_ordered_qty`), `erpnext/erpnext/controllers/status_updater.py:633-714` (`update_qty` recompute)

### DOC-013 · Cancel needs a reason
- **Priority:** P1
- **Level:** integration
- **Given** an approved order **When** cancel is called with reason `""` or `"  "` **Then** it is refused with "Enter a reason for cancelling (at least 3 characters)." and the state stays `approved`. **When** the reason is longer than 500 characters **Then** the error is "Keep the reason under 500 characters."
- **Go test:** `TestDoc_Cancel_RequiresReason`
- **Sources:** Decision D-DOC-19 (no source requires it)

### DOC-014 · Close an approved document
- **Priority:** P1
- **Level:** integration
- **Given** an approved order with line qty 10 and `received_qty = 6`, and a user with submit rights **When** it is closed with reason "Supplier can't supply balance 4 bags." **Then** `state = "closed"`, the audit entry has the transition `approved → closed` with the reason, and the roll-up fields are unchanged (`received_qty = 6`).
- **Go test:** `TestDoc_Close_Approved`
- **Sources:** `erpnext/erpnext/buying/doctype/purchase_order/test_purchase_order.py:984` (`test_ordered_qty_for_closing_po`), `erpnext/erpnext/stock/doctype/purchase_receipt/test_purchase_receipt.py:735` (`test_closed_purchase_receipt`), `erpnext/erpnext/buying/doctype/purchase_order/purchase_order.py:657-664`

### DOC-015 · Close needs a reason, and closing a non-approved document is refused
- **Priority:** P1
- **Level:** integration
- **Given** an approved order **When** it is closed with an empty reason **Then** the error is "Enter a reason for closing (at least 3 characters)." **Given** a draft order **When** it is closed **Then** the error is "Only approved documents can be closed."
- **Go test:** `TestDoc_Close_Guards`
- **Sources:** `erpnext/erpnext/stock/doctype/purchase_receipt/test_purchase_receipt.py:8305` (`test_closed_receipt_cannot_be_invoiced`, draft can't be closed), `erpnext/erpnext/buying/doctype/purchase_order/purchase_order.py:662-663` ("Only a submitted Purchase Order can be held, closed or re-opened.")

### DOC-016 · Closed is terminal
- **Priority:** P1
- **Level:** integration
- **Given** a closed order **When** cancel, save, approve, withdraw, amend or a "reopen" request is tried **Then** each is refused with "PO/26-27/00001 is closed and can't be changed." (approve gets the DOC-005 message "PO/26-27/00001 is not waiting for approval. It is Closed."), and there is no reopen route (reopen is later, spec R6).
- **Go test:** `TestDoc_Closed_IsTerminal`
- **Sources:** BRIEF F5. Disagrees with `erpnext/erpnext/buying/doctype/purchase_order/purchase_order.py:657` (Re-open). See D-DOC-08

### DOC-017 · New later documents can't draw from a closed source
- **Priority:** P1
- **Level:** integration
- **Given** a closed order **When** a receipt line pointing at it is saved, or a receipt saved before the close is submitted or approved **Then** it is refused with "Line 1: PO/26-27/00001 is closed. Remove the line or pick another purchase order."
- **Go test:** `TestDoc_Link_ClosedSourceRefused`
- **Sources:** `erpnext/erpnext/stock/doctype/purchase_receipt/test_purchase_receipt.py:747` (`test_purchase_return_against_closed_purchase_order`, "a regular receipt against the closed order must still be blocked"), `erpnext/erpnext/controllers/status_updater.py:241-272` (`validate_closed_source_items`), `erpnext/erpnext/buying/utils.py:126`

### DOC-018 · A later document can be cancelled when its source is closed
- **Priority:** P2
- **Level:** integration
- **Given** an order closed with `received_qty = 6` from approved receipt R1 (6) **When** R1 is cancelled with a reason **Then** R1 becomes `cancelled`, order line `received_qty = 0`, the order stays `closed`, and the order gets an audit entry for the roll-up change with cause R1.
- **Go test:** `TestDoc_Cancel_LaterDocOfClosedSource`
- **Sources:** Decision D-DOC-09. Differs from `erpnext/erpnext/stock/doctype/purchase_receipt/purchase_receipt.py:454` and `erpnext/erpnext/controllers/stock_controller.py:615`

### DOC-019 · All missing required fields are reported together
- **Priority:** P1
- **Level:** unit
- **Given** an order with `supplier` and `order_date` empty **When** it is saved **Then** the save fails with field errors `supplier: "Supplier is required."` and `order_date: "Order date is required."` in one response, and nothing is written.
- **Go test:** `TestDoc_Validate_MandatoryCollected`
- **Sources:** `frappe/frappe/tests/test_document.py:209` (`test_mandatory`), `frappe/frappe/model/document.py:1802-1823` (`_validate_mandatory` collects all)

### DOC-020 · Whitespace counts as empty; zero and false count as filled
- **Priority:** P1
- **Level:** unit
- **Given** a required text field `supplier_ref = "   "`, a required number `qty = 0` and a required boolean `is_urgent = false` **When** the document is validated **Then** `supplier_ref` is reported missing. `qty` and `is_urgent` don't fail the required check (qty > 0 is a separate rule, DOC-033).
- **Go test:** `TestDoc_Validate_MandatoryBlankRules`
- **Sources:** `frappe/frappe/model/document.py:3509-3530` (`has_content`, Check never mandatory), `frappe/frappe/tests/test_document.py:225` (`test_text_editor_field`)

### DOC-021 · Required line fields report the line number
- **Priority:** P1
- **Level:** unit
- **Given** an order with three lines where line 2 has `item` set and `qty` empty **When** it is saved **Then** the error is `lines[2].qty: "Line 2: Quantity is required."`
- **Go test:** `TestDoc_Validate_LineMandatoryRowNo`
- **Sources:** `frappe/frappe/model/document.py:3500-3505` ("Row #{1}: Value missing for"), `frappe/frappe/tests/test_document.py:136` (`test_insert_with_child`)

### DOC-022 · At least one line is required to submit (an empty draft may be saved)
- **Priority:** P1
- **Level:** unit
- **Given** an order header with no lines, or only blank lines **When** it is saved as a draft **Then** it saves with zero lines (blank lines dropped). **When** it is submitted **Then** the error is "Add at least one line." and the state stays `draft`.
- **Go test:** `TestDoc_Validate_AtLeastOneLine`
- **Sources:** `frappe/frappe/model/document.py:2459-2465` (`validate_table_has_rows`), `frappe/frappe/model/document.py:3497` ("Data missing in table"). Contrast: `tryton/modules/purchase/tests/scenario_purchase_empty.rst:22-39`

### DOC-023 · Non-negative and min/max field rules come from the DocType definition
- **Priority:** P1
- **Level:** unit
- **Given** line `rate` is marked non-negative and header `credit_days` has min 0 and max 365 **When** a document is saved with rate −10 **Then** the error is "Line 1: Rate can't be negative." **When** `credit_days` is 400 **Then** the error is "Credit days must be between 0 and 365." **When** `credit_days` is 30 **Then** it saves, and an empty optional `credit_days` also saves.
- **Go test:** `TestDoc_Validate_NonNegativeMinMax`
- **Sources:** `frappe/frappe/tests/test_document.py:430` (`test_non_negative_check`), `frappe/frappe/tests/test_document.py:445` (`test_min_max_value_check`), `erpnext/erpnext/buying/doctype/purchase_order/test_purchase_order.py:34` (`test_purchase_order_qty`)

### DOC-024 · Text length limits are enforced, and measured in characters, not bytes
- **Priority:** P2
- **Level:** unit
- **Given** `supplier_ref` has max length 140 **When** a 141-character value is saved **Then** the error is "Supplier ref can be at most 140 characters." **When** a 140-character Devanagari value (more than 140 bytes) is saved **Then** it saves.
- **Go test:** `TestDoc_Validate_MaxLength`
- **Sources:** `frappe/frappe/tests/test_document.py:333` (`test_varchar_length`), `frappe/frappe/tests/test_document.py:338` (`test_varchar_length_after_sanitization`)

### DOC-025 · From/to date pairs are checked by a shared helper
- **Priority:** P2
- **Level:** unit
- **Given** a document with `valid_from` and `valid_to` **When** both are empty, or only one is set, or from 2026-04-01 and to 2027-03-31 **Then** it validates. **When** from is 2027-03-31 and to is 2026-04-01 **Then** the error is "Valid to can't be before Valid from."
- **Go test:** `TestDoc_ValidateFromToDates`
- **Sources:** `frappe/frappe/tests/test_document.py:677` (`test_validate_from_to_dates`), `frappe/frappe/tests/test_document.py:302` (`test_validate`)

### DOC-026 · Field errors from the module's Validate hook are returned together with the core errors
- **Priority:** P1
- **Level:** unit
- **Given** a fixture `Validate` that adds `lines[1].rate: "Line 1: Rate must be more than 0."`, and the header also misses `supplier` **When** the document is saved **Then** both errors come back in one `ValidationErrors` value, keyed by field path, and nothing is written.
- **Go test:** `TestDoc_Validate_HookErrorsMerged`
- **Sources:** Decision D-DOC-20. `frappe/frappe/tests/test_document.py:302` (`test_validate`)

### DOC-027 · Which hooks run for which action
- **Priority:** P1
- **Level:** unit
- **Given** a fixture DocType whose hooks record their calls **When** each action runs **Then** the calls are: save → `[Validate]`; submit (rule) → `[Validate, BeforeSubmit]`; submit (no rule) → `[Validate, BeforeSubmit, ApplyEffects]`; approve → `[Validate, BeforeSubmit, ApplyEffects]`; reject → `[]`; withdraw → `[]`; revise → `[]`; cancel → `[linked check, ReverseEffects]` (no `Validate`); close → `[]`; delete → `[]`.
- **Go test:** `TestDoc_Hooks_CallOrder`
- **Sources:** `frappe/frappe/model/document.py:2144-2174` (`run_before_save_methods`), `frappe/frappe/model/document.py:2198-2213` (`run_post_save_methods`), `frappe/frappe/model/document.py:1149-1150` (no `_validate` on cancel), `frappe/frappe/tests/test_document.py:640` (`test_doc_events`)

### DOC-028 · A failed validation writes nothing
- **Priority:** P1
- **Level:** integration
- **Given** a saved draft (version 2) with supplier `SHREE` **When** a save that changes supplier to `BHARAT` and blanks `order_date` fails **Then** the database still shows `SHREE`, version 2, and the same audit count.
- **Go test:** `TestDoc_Validate_FailureWritesNothing`
- **Sources:** `frappe/frappe/tests/test_document.py:209` (`test_mandatory`), `frappe/frappe/tests/test_db.py:549` (`test_savepoints_wrapper`)

### DOC-029 · A save that changes nothing doesn't bump the version or write an audit entry
- **Priority:** P2
- **Level:** integration
- **Given** a draft at version 4 **When** it is saved with identical values **Then** the response is success, `version = 4`, `updated_at` is unchanged, and no audit entry is written.
- **Go test:** `TestDoc_Save_NoOp`
- **Sources:** `frappe/frappe/core/doctype/version/version.py:248-252` (no diff → no Version), `frappe/frappe/tests/test_document.py:187` (`test_value_changed`)

### DOC-030 · created_* never changes; updated_* follows the last writer
- **Priority:** P1
- **Level:** integration
- **Given** an order created by `priya` **When** `admin` (Anil Kapoor) saves a change **Then** `created_by = priya`, `created_at` is unchanged, `updated_by = admin` and `updated_at` is the save time in UTC.
- **Go test:** `TestDoc_Save_OwnerAndModifier`
- **Sources:** `frappe/frappe/model/document.py:1352-1373` (`set_user_and_timestamp`), `frappe/frappe/tests/test_document.py:237` (`test_save_after_insert_on_a_whole_second`)

### DOC-031 · Saving a document that no longer exists gives a clear error
- **Priority:** P2
- **Level:** integration
- **Given** draft id 812 was deleted in another tab **When** a save for id 812 arrives **Then** the response is 404 "This document no longer exists. It may have been deleted." and nothing is created.
- **Go test:** `TestDoc_Save_MissingDoc`
- **Sources:** `frappe/frappe/tests/test_document.py:664` (`test_error_on_saving_new_doc_with_name`)

### DOC-032 · Links must point at existing masters
- **Priority:** P1
- **Level:** unit
- **Given** supplier id 9999 doesn't exist **When** an order with `supplier_id = 9999` is saved **Then** the error is "Supplier not found. Pick one from the list." **When** line 2 has a missing item id **Then** the error is "Line 2: Item not found. Pick one from the list."
- **Go test:** `TestDoc_Link_MissingMaster`
- **Sources:** `frappe/frappe/tests/test_document.py:280` (`test_link_validation`), `frappe/frappe/model/document.py:1951-1968` (`_validate_links`, "Could not find")

### DOC-033 · Line quantity must be more than 0
- **Priority:** P1
- **Level:** unit
- **Given** a line with qty 0, and another with qty −1 **When** the document is saved **Then** the errors are "Line 1: Quantity must be more than 0." and "Line 2: Quantity must be more than 0."
- **Go test:** `TestDoc_Line_QtyPositive`
- **Sources:** `erpnext/erpnext/buying/doctype/purchase_order/test_purchase_order.py:34` (`test_purchase_order_qty`), `erpnext/erpnext/buying/doctype/purchase_order/test_purchase_order.py:73` (`test_purchase_order_zero_qty_cannot_be_forced_by_the_client`), `erpnext/erpnext/controllers/accounts_controller.py:1119-1133`

### DOC-034 · Disabled masters are refused on save, submit and approve
- **Priority:** P1
- **Level:** integration
- **Given** supplier `RAJKOT` Rajkot Castings is disabled **When** a new order with that supplier is saved **Then** the error is "Supplier RAJKOT Rajkot Castings is disabled. Pick an active supplier, or ask an admin to enable it." **When** line 1's item `CEMENT-OPC` (OPC Cement 53 Grade) is disabled **Then** the error is "Line 1: Item CEMENT-OPC OPC Cement 53 Grade is disabled. Pick an active item, or ask an admin to enable it." A master is always shown as "{Code} {Name}" (CD-68).
- **Go test:** `TestDoc_Link_DisabledMasterRefused`
- **Sources:** `erpnext/erpnext/buying/doctype/supplier/test_supplier.py:94` (`test_supplier_disabled`), `erpnext/erpnext/accounts/party.py:904-912`, `erpnext/erpnext/stock/doctype/item/item.py:1500-1510`

### DOC-035 · A master disabled after use blocks only forward actions
- **Priority:** P1
- **Level:** integration
- **Given** draft order D and approved order A both use supplier `RAJKOT`, which is then disabled **When** D is submitted **Then** it fails with the DOC-034 message. **When** A is viewed, printed, closed or cancelled **Then** each succeeds.
- **Go test:** `TestDoc_Link_DisabledAfterUse`
- **Sources:** `frappe/frappe/model/document.py:1952` (links not validated on cancel), `erpnext/erpnext/buying/doctype/supplier/test_supplier.py:94`

### DOC-036 · Approve fails when a master was disabled while the document was pending
- **Priority:** P2
- **Level:** integration
- **Given** a pending order with item `CEMENT-OPC`, which is then disabled **When** the approver approves **Then** the error is the DOC-034 line message, the state stays `pending_approval` and no effects run. **When** the approver rejects instead **Then** that succeeds.
- **Go test:** `TestDoc_Approve_DisabledSinceSubmit`
- **Sources:** Decision D-DOC-03

### DOC-037 · Source documents must be approved (table-driven)
- **Priority:** P1
- **Level:** integration
- **Given** source orders in each state **When** a receipt line points at one and is saved **Then**: approved → OK; draft or rejected → "Line 1: Draft 42 isn't approved yet. Pick an approved purchase order."; pending_approval → "Line 1: Draft 42 is waiting for approval. Pick an approved purchase order."; cancelled → "Line 1: PO/26-27/00001 is cancelled. Remove the line or pick another purchase order."; closed → DOC-017 message.
- **Go test:** `TestDoc_Link_SourceStateRules`
- **Sources:** `erpnext/erpnext/buying/doctype/purchase_order/test_purchase_order.py:86` (`test_make_purchase_receipt`, can't make from unsubmitted PO), `frappe/frappe/model/document.py:1970-1972` (`CancelledLinkError`)

### DOC-038 · A source cancelled after a later draft was saved blocks that draft's submit
- **Priority:** P1
- **Level:** integration
- **Given** draft receipt R points at approved order O, and O is then cancelled (R is a draft, so it doesn't block) **When** R is submitted **Then** the error is "Line 1: PO/26-27/00001 is cancelled. Remove the line or pick another purchase order." and R stays a draft.
- **Go test:** `TestDoc_Link_SourceCancelledBeforeSubmit`
- **Sources:** `erpnext/erpnext/selling/doctype/sales_order/test_sales_order.py:1800` (`test_block_delivery_note_against_cancelled_sales_order`)

### DOC-039 · Lines are numbered 1..n in the order given
- **Priority:** P1
- **Level:** integration
- **Given** an order saved with lines for items A, B, C in that order **When** it is reloaded **Then** the lines come back as A(1), B(2), C(3), sorted by `line_no`.
- **Go test:** `TestDoc_Lines_NumberedInOrder`
- **Sources:** `frappe/frappe/model/document.py:2910-2914` (`append` sets idx)

### DOC-040 · Removing a line renumbers the rest without gaps
- **Priority:** P1
- **Level:** integration
- **Given** lines A(1), B(2), C(3), D(4) **When** B is removed and the order saved **Then** the lines are A(1), C(2), D(3), the B row is gone from the database, and the audit records "line removed: 2 (item B)".
- **Go test:** `TestDoc_Lines_RemoveRenumbers`
- **Sources:** `frappe/frappe/model/document.py:2953-2962` (`remove` re-numbers idx), `frappe/frappe/model/document.py:1210-1236` (`update_child_table` deletes missing rows)

### DOC-041 · Inserting or moving a line renumbers and persists the new order
- **Priority:** P2
- **Level:** integration
- **Given** lines A(1), B(2), C(3) **When** X is inserted at position 2 and C is moved to the top **Then** after save the order is C(1), A(2), X(3), B(4), and line ids of A, B and C are unchanged.
- **Go test:** `TestDoc_Lines_InsertAndMove`
- **Sources:** `frappe/frappe/model/document.py:2915-2921` (`append` with position re-numbers), `odoo/addons/purchase/models/purchase_order_line.py:27` (`sequence`)

### DOC-042 · Line ids are stable, and new lines get new ids
- **Priority:** P1
- **Level:** integration
- **Given** lines with ids 501 and 502 **When** line 502's qty changes from 10 to 12 and a new line is added **Then** 501 and 502 keep their ids, the new line gets a fresh id, and the audit shows `line 2 (id 502): qty 10 → 12` plus "line added: 3".
- **Go test:** `TestDoc_Lines_StableIDs`
- **Sources:** `frappe/frappe/model/document.py:1782-1786` (`set_name_in_children`), `frappe/frappe/core/doctype/version/version.py:158-183` (rows matched by name)

### DOC-043 · Fully blank lines are dropped; a partly filled line is validated
- **Priority:** P1
- **Level:** unit
- **Given** three lines: line 1 complete; line 2 with item, qty, rate and description all blank; line 3 with only description "loading charges" **When** the document is saved **Then** line 2 is dropped silently, line 3 becomes line 2, and the error is "Line 2: Item is required."
- **Go test:** `TestDoc_Lines_BlankDropped`
- **Sources:** Decision D-DOC-15. `frappe/frappe/model/document.py:3492-3536` (mandatory per row)

### DOC-044 · No more than 500 lines
- **Priority:** P2
- **Level:** unit
- **Given** an order with 501 non-blank lines **When** it is saved **Then** the error is "A document can have at most 500 lines. Split it into two documents." With exactly 500 lines it saves.
- **Go test:** `TestDoc_Lines_Max500`
- **Sources:** Decision D-DOC-17 (no source limit)

### DOC-045 · The same item may appear on two lines
- **Priority:** P2
- **Level:** unit
- **Given** two lines both with item `CEMENT-OPC` (qty 10 at ₹395, and qty 5 at ₹380) **When** the order is saved **Then** it saves with two separate lines.
- **Go test:** `TestDoc_Lines_DuplicateItemAllowed`
- **Sources:** `erpnext/erpnext/buying/doctype/purchase_order/test_purchase_order.py:1006` (`test_group_same_items`)

### DOC-046 · A line id belonging to another document is refused
- **Priority:** P1
- **Level:** integration
- **Given** order A has line id 501 and order B has line id 777 **When** a save of A submits a line with id 777 **Then** the error is "Line 1 doesn't belong to this document. Reload and try again.", and neither A nor B changes.
- **Go test:** `TestDoc_Lines_ForeignLineIDRefused`
- **Sources:** `frappe/frappe/model/document.py:1776-1780` (`set_parent_in_children` forces parent)

### DOC-047 · Deleting a document deletes its lines
- **Priority:** P1
- **Level:** integration
- **Given** a draft with 3 lines **When** it is deleted **Then** no rows remain in the line table for that parent id (foreign key `ON DELETE CASCADE` and `PRAGMA foreign_keys=ON`).
- **Go test:** `TestDoc_Delete_CascadesLines`
- **Sources:** `frappe/frappe/model/delete_doc.py:274` (`delete_from_table`)

### DOC-048 · Lines are frozen once the document leaves draft
- **Priority:** P1
- **Level:** integration
- **Given** an order in each of pending_approval, approved, rejected, closed and cancelled **When** a request adds, removes or edits a line **Then** it is refused with the DOC-005 message for that state, and the lines are unchanged.
- **Go test:** `TestDoc_Lines_FrozenAfterDraft`
- **Sources:** `tryton/modules/purchase/purchase.py:2183-2196` (lines only created in draft; deleted in draft or cancelled), `odoo/addons/purchase/models/purchase_order_line.py:466-471`, `frappe/frappe/tests/test_document.py:320` (`test_update_after_submit`)

### DOC-049 · Header fields of a submitted document can't be edited
- **Priority:** P1
- **Level:** integration
- **Given** an approved order with supplier `SHREE` **When** a save changes supplier, date or remarks **Then** the error is "PO/26-27/00001 is approved and can't be edited. Cancel and amend it to make changes." (editing approved documents is later, spec R7).
- **Go test:** `TestDoc_Submitted_HeaderImmutable`
- **Sources:** `frappe/frappe/model/document.py:3830-3864` ("Not allowed to change … after submission"), `tryton/modules/purchase/purchase.py:2444-2448` ("To modify the header … it must be in draft state."), `tryton/modules/purchase/tests/scenario_purchase_modify_header.rst:82-92`

### DOC-050 · Roll-up fields of an approved document change only through effects
- **Priority:** P1
- **Level:** integration
- **Given** an approved order line with `received_qty = 4` **When** a receipt for 3 is approved against it **Then** `received_qty = 7`, `version` goes up by 1, and the order gets an audit entry `line 1 received_qty 4 → 7` with `cause = GR/26-27/00002 approve`. No user-facing route can set `received_qty`.
- **Go test:** `TestDoc_RollUp_OnlyViaEffects`
- **Sources:** `erpnext/erpnext/controllers/status_updater.py:633-714`, `frappe/frappe/core/doctype/version/version.py:61-75` (`updater_reference`)

### DOC-051 · No DocType declares a field editable after submit (schema guard)
- **Priority:** P2
- **Level:** unit
- **Given** the generated DocType registry **When** the test walks all fields **Then** no field has `EditableAfterSubmit = true`, except fields marked `RollUp` or `System`.
- **Go test:** `TestDoc_Schema_NoAllowOnSubmit`
- **Sources:** Decision D-DOC-07. Contrast `frappe/frappe/tests/test_document.py:320` (`test_update_after_submit`, allow_on_submit)

### DOC-052 · Cancel is blocked by a later approved document, and the blocker is named
- **Priority:** P1
- **Level:** integration
- **Given** approved order PO/26-27/00001 with approved receipt GR/26-27/00001 **When** the order is cancelled with a reason **Then** the error is "Can't cancel PO/26-27/00001 because GR/26-27/00001 (approved) is linked to it. Cancel GR/26-27/00001 first." and the order stays approved, unchanged.
- **Go test:** `TestDoc_Cancel_BlockedByLaterDoc`
- **Sources:** `frappe/frappe/model/delete_doc.py:527-542` (`raise_link_exists_exception`), `frappe/frappe/model/document.py:2335-2341` (`check_no_back_links_exist`), `erpnext/erpnext/stock/doctype/purchase_receipt/test_purchase_receipt.py:7880` (`test_cancel_blocked_by_submitted_invoice_rolls_back`), `odoo/addons/purchase/models/purchase_order.py:728-730`

### DOC-053 · Which later-document states block a cancel (table-driven)
- **Priority:** P1
- **Level:** integration
- **Given** one approved order and one linked receipt in each state **When** the order is cancelled **Then**: receipt pending_approval → blocked; approved → blocked; closed → blocked; draft → not blocked; rejected → not blocked; cancelled → not blocked.
- **Go test:** `TestDoc_Cancel_BlockingStates`
- **Sources:** `frappe/frappe/model/delete_doc.py:395-398` (only submitted docs block a Cancel), `odoo/addons/purchase/models/purchase_order.py:729` (bills not in cancel/draft block). Decision D-DOC-04

### DOC-054 · Once the blockers are cancelled, the source can be cancelled
- **Priority:** P1
- **Level:** integration
- **Given** the blocked order from DOC-052 **When** GR/26-27/00001 is cancelled and then the order is cancelled **Then** both succeed, and the order line `received_qty = 0` before the order's own reverse runs.
- **Go test:** `TestDoc_Cancel_AfterBlockersCancelled`
- **Sources:** `erpnext/erpnext/buying/doctype/purchase_order/test_purchase_order.py:808` (`test_purchase_order_invoice_receipt_workflow`, cancel PR, then PI, then PO)

### DOC-055 · Line-level links are found by the linked-document query
- **Priority:** P1
- **Level:** unit
- **Given** `fixture.Receipt` declares `LinkSpec{Field: "order_line_id", Target: "fixture.Order", OnLine: true}` and an approved receipt whose line 3 points at order line 2 **When** `LinkedBlockers(order)` runs **Then** it returns that receipt once, even when several of its lines point at the same order.
- **Go test:** `TestDoc_LinkedBlockers_LineLevelDedup`
- **Sources:** `frappe/frappe/model/delete_doc.py:332-412` (`get_linked_docs`, child tables mapped to parent), `frappe/frappe/tests/test_linked_with.py:221` (`test_get_submitted_linked_docs_has_no_duplicates`)

### DOC-056 · The link check runs before effects are reversed, and a blocked cancel leaves no trace
- **Priority:** P1
- **Level:** integration
- **Given** the blocked order from DOC-052 and a fixture `ReverseEffects` that records calls **When** cancel is tried **Then** `ReverseEffects` was never called. Order state, version, roll-ups and audit count are all unchanged.
- **Go test:** `TestDoc_Cancel_LinkCheckFirst`
- **Sources:** `erpnext/erpnext/stock/doctype/purchase_receipt/test_purchase_receipt.py:7880` (rollback after late check), `odoo/addons/purchase/models/purchase_order.py:723-731` (check before write). Decision D-DOC-05

### DOC-057 · The amended-from link never blocks anything
- **Priority:** P2
- **Level:** unit
- **Given** cancelled A and its approved amendment A-1 (`amended_from = A`) **When** `LinkedBlockers(A)` runs **Then** it returns nothing.
- **Go test:** `TestDoc_LinkedBlockers_IgnoreAmendedFrom`
- **Sources:** `frappe/frappe/model/delete_doc.py:349` (`link_field == "amended_from" and method == "Cancel"`)

### DOC-058 · Many blockers are listed with a cap
- **Priority:** P2
- **Level:** integration
- **Given** an order with 13 approved receipts **When** cancel is tried **Then** the message lists the first 10 numbers in number order and ends "…and 3 more."
- **Go test:** `TestDoc_Cancel_ManyBlockersCapped`
- **Sources:** `frappe/frappe/tests/test_linked_with.py:468` (`test_get_submitted_linked_docs_truncates_large_graphs`)

### DOC-059 · Roll-ups are recomputed from source lines, never incremented
- **Priority:** P1
- **Level:** integration
- **Given** order line qty 10 and approved receipts R1 (4) and R2 (3) **When** the line's `received_qty` is corrupted to 99 directly in the database and R3 (2) is approved **Then** `received_qty = 9` (4+3+2), not 101. Cancelling R1 then gives 5.
- **Go test:** `TestDoc_RollUp_Recompute`
- **Sources:** `erpnext/erpnext/controllers/status_updater.py:686-711` (SUM over submitted source rows), `erpnext/erpnext/buying/doctype/purchase_order/test_purchase_order.py:735` (`test_update_qty`)

### DOC-060 · Closed source documents still count toward roll-ups
- **Priority:** P2
- **Level:** unit
- **Given** receipts R1 (4, approved) and R2 (2, closed) on one order line **When** the roll-up is recomputed **Then** `received_qty = 6`.
- **Go test:** `TestDoc_RollUp_IncludesClosed`
- **Sources:** Decision D-DOC-13 (closed = approved with nothing pending)

### DOC-061 · Delete a draft and keep its history
- **Priority:** P1
- **Level:** integration
- **Given** a draft with 2 lines and 3 audit entries **When** its owner deletes it **Then** the document and lines are gone. A `delete` audit entry with a JSON snapshot of the header and lines is written, and the earlier 3 entries remain.
- **Go test:** `TestDoc_Delete_DraftKeepsAudit`
- **Sources:** `frappe/frappe/model/delete_doc.py:253-262` (`add_to_deleted_document`). Contrast `odoo/addons/mail/models/mail_thread.py:427-432` (messages deleted)

### DOC-062 · Only drafts can be deleted (table-driven)
- **Priority:** P1
- **Level:** integration
- **Given** documents in each non-draft state **When** delete is tried **Then**: pending_approval → "Draft 42 is waiting for approval and can't be deleted. Withdraw it first, then delete it."; rejected → "Rejected documents can't be deleted. Revise it to move it back to draft, then delete it."; approved or closed → "Approved documents can't be deleted. Cancel it instead."; cancelled → "Cancelled documents are kept for the record and can't be deleted."
- **Go test:** `TestDoc_Delete_OnlyDraft`
- **Sources:** `frappe/frappe/model/delete_doc.py:307-325` ("Submitted Record cannot be deleted. You must Cancel it first."), `tryton/modules/purchase/purchase.py:938-946` ("To delete purchase … you must cancel it."), `odoo/addons/purchase/models/purchase_order.py:469-473`. Decision D-DOC-06

### DOC-063 · Deleting a draft amendment frees the original for a new amendment
- **Priority:** P2
- **Level:** integration
- **Given** cancelled A and its draft amendment A-1 **When** A-1 is deleted and A is amended again **Then** a new draft is created with `amended_from = A`.
- **Go test:** `TestDoc_Delete_AmendmentDraftFreesOriginal`
- **Sources:** `frappe/frappe/tests/test_linked_with.py:1001` (`test_check_delete_integrity`)

### DOC-064 · Amend copies a cancelled document into a new draft
- **Priority:** P1
- **Level:** integration
- **Given** cancelled order A `PO/26-27/00001` (the reference PO: supplier `SHREE`; lines MS-ANGLE-50 1,200.000 kg at ₹62.5000 with `received_qty = 0` and WIRE-18G 50.000 kg at ₹96.0000; cancel reason "Wrong supplier GSTIN"; created by `priya`) **When** `ravi` (who has the `amend` right) amends it **Then** a new draft A-1 exists with `amended_from = A.id`, `created_by = updated_by = ravi` and `created_at` = the amend time (the amender becomes `created_by`, CD-67; attachments are copied as uploaded by ravi, CD-29), the same header values and lines (new line ids, `line_no` 1 and 2), `received_qty = 0`, no approval, reject or cancel reason, no number yet (it shows `Draft {id}`; NAM gives it `PO/26-27/00001-1` when it is approved, spec R1), and `state = "draft"`. A stays cancelled.
- **Go test:** `TestDoc_Amend_CopiesToDraft`
- **Sources:** `frappe/frappe/model/document.py:1061-1063`, `frappe/frappe/model/document.py:1173-1199`, `frappe/frappe/tests/test_naming.py:311` (`test_naming_for_cancelled_and_amended_doc`), `erpnext/erpnext/controllers/status_updater.py:275-277` (amended doc status reset to Draft)

### DOC-065 · Only one amendment per cancelled document
- **Priority:** P1
- **Level:** integration
- **Given** cancelled A with amendment A-1 (in any state except deleted) **When** A is amended again **Then** the error is "PO/26-27/00001 has already been amended as Draft 43. Open that document instead." when A-1 is not yet approved, and "…amended as PO/26-27/00001-1…" once A-1 has been approved.
- **Go test:** `TestDoc_Amend_OnlyOnce`
- **Sources:** Decision D-DOC-14

### DOC-066 · Amendments can chain
- **Priority:** P2
- **Level:** integration
- **Given** A (cancelled) → A-1 (approved, then cancelled) **When** A-1 is amended **Then** A-2 has `amended_from = A-1`, and the history view can walk A-2 → A-1 → A.
- **Go test:** `TestDoc_Amend_Chain`
- **Sources:** `frappe/frappe/tests/test_naming.py:311`

### DOC-067 · An amendment is fully validated when submitted
- **Priority:** P2
- **Level:** integration
- **Given** cancelled A (from DOC-064) whose item `WIRE-18G` has since been disabled **When** A is amended **Then** the draft A-1 is created. **When** A-1 is submitted unchanged **Then** it fails with "Line 2: Item WIRE-18G Binding Wire 18G is disabled. Pick an active item, or ask an admin to enable it." (CD-68)
- **Go test:** `TestDoc_Amend_ValidatedOnSubmit`
- **Sources:** `frappe/frappe/model/document.py:1032` (`_validate_links` on insert). Decision D-DOC-18

### DOC-068 · Amend writes audit entries on both documents
- **Priority:** P2
- **Level:** integration
- **Given** cancelled A **When** it is amended into A-1 **Then** A-1 has a `create` entry with `amended_from = A`, and A has an `amended` entry pointing at A-1. A's state and version are unchanged.
- **Go test:** `TestDoc_Amend_AuditBothSides`
- **Sources:** `frappe/frappe/model/document.py:2357-2358` (version compares with amended_from)

### DOC-069 · A stale copy can't be saved
- **Priority:** P1
- **Level:** integration
- **Given** users U1 and U2 both open draft D at version 5 **When** U1 saves (version becomes 6) and then U2 saves from version 5 **Then** U2 gets HTTP 409 "Someone else changed Draft 42 after you opened it. Reload it and make your changes again." and the database keeps U1's values.
- **Go test:** `TestDoc_Concurrency_StaleSave`
- **Sources:** `frappe/frappe/tests/test_document.py:231` (`test_conflict_validation`), `frappe/frappe/tests/test_document.py:259` (`test_conflict_validation_single`), `frappe/frappe/model/document.py:1688-1719`

### DOC-070 · Every transition checks the version
- **Priority:** P1
- **Level:** integration
- **Given** a document loaded at version 5 and then changed elsewhere to version 6 **When** submit, withdraw, approve, reject, revise, cancel, close, amend or delete is sent with version 5 **Then** each returns the DOC-069 message and nothing changes.
- **Go test:** `TestDoc_Concurrency_StaleTransition`
- **Sources:** `frappe/frappe/model/document.py:2116-2118` (`discard` also calls `check_if_latest`), `frappe/frappe/model/document.py:1688`

### DOC-071 · Parallel saves: exactly one wins
- **Priority:** P1
- **Level:** integration
- **Given** draft D at version 5 **When** 20 goroutines save D concurrently from version 5 **Then** exactly 1 succeeds, 19 get the stale error, `version = 6`, and there is exactly 1 new audit entry.
- **Go test:** `TestDoc_Concurrency_ParallelSaves`
- **Sources:** `frappe/frappe/tests/test_document.py:231`. Decision D-DOC-10

### DOC-072 · Concurrent approvals can't over-fill a source line
- **Priority:** P1
- **Level:** integration
- **Given** order line qty 10 with nothing received, a module rule "received may not exceed ordered", and pending receipts R1 (6) and R2 (6) **When** both are approved at the same time **Then** exactly one succeeds. The other fails with "Line 1: receiving 6 would take PO/26-27/00001 line 1 to 12 of 10. Reduce the quantity." and the final `received_qty = 6`.
- **Go test:** `TestDoc_Concurrency_NoOverfill`
- **Sources:** `erpnext/erpnext/controllers/status_updater.py:567-613` (`limits_crossed_error`), `erpnext/erpnext/buying/doctype/purchase_order/test_purchase_order.py:123` (`test_make_purchase_receipt_respects_over_receipt_allowance`). Decision D-DOC-21

### DOC-073 · A cancel and a dependent approve can't both succeed
- **Priority:** P1
- **Level:** integration
- **Given** approved order O and receipt R against it, either pending (approve R) or draft with no approval rule (submit R auto-approves), since a pending R always blocks the cancel (CD-156) **When** "cancel O" and "approve R" run at the same time (repeated 50 times) **Then** in every run the end state is never "O cancelled and R approved". Either O is cancelled and R's approve failed, or R is approved and O's cancel failed.
- **Go test:** `TestDoc_Concurrency_CancelVsApprove`
- **Sources:** `frappe/frappe/model/delete_doc.py:168-177` (row lock before delete), `frappe/frappe/tests/test_linked_with.py:421` (`test_deadlocks_are_not_deferred`)

### DOC-074 · Version goes up by exactly 1 per write, including roll-up writes
- **Priority:** P2
- **Level:** integration
- **Given** an approved order at version 3 **When** two receipts against it are approved one after the other **Then** the order's version is 5.
- **Go test:** `TestDoc_Concurrency_VersionOnRollUp`
- **Sources:** `erpnext/erpnext/controllers/status_updater.py:834` (`_update_modified` on target)

### DOC-075 · A busy database is retried, then reported plainly
- **Priority:** P2
- **Level:** integration
- **Given** another connection holds the write lock for longer than `busy_timeout` (5 s by default; the test opens the database with `db.WithBusyTimeout(200 ms)`, CD-08) **When** a save runs **Then** it returns 503 with the `db.ErrBusy` text "The system is busy. Please try again in a moment." (the one busy text everywhere, CD-66) and nothing is written. When the lock is released inside the timeout, the save succeeds.
- **Go test:** `TestDoc_Tx_BusyTimeout`
- **Sources:** `frappe/frappe/model/delete_doc.py:168-177` ("can not be deleted right now as it's being modified by another user")

### DOC-076 · A failure while applying effects rolls back everything
- **Priority:** P1
- **Level:** integration
- **Given** a pending receipt with lines against order lines L1 and L2, and a fixture `ApplyEffects` that updates L1 and then returns an error **When** it is approved **Then** the receipt stays `pending_approval` (same version), L1 and L2 `received_qty` are unchanged, no audit entry is written on either document, the receipt still has no number (`Draft 42`), and the NAM counter is unchanged, so the next receipt approved still gets `GR/26-27/00001`.
- **Go test:** `TestDoc_Tx_ApplyEffectsFailureRollsBack`
- **Sources:** `erpnext/erpnext/stock/doctype/purchase_receipt/test_purchase_receipt.py:7880`, `frappe/frappe/tests/test_db.py:528` (`test_savepoints`), `frappe/frappe/tests/test_linked_with.py:540` (`test_background_cancel_is_all_or_nothing`)

### DOC-077 · A failure while reversing effects rolls back the cancel
- **Priority:** P1
- **Level:** integration
- **Given** an approved receipt and a fixture `ReverseEffects` that errors after its first write **When** it is cancelled **Then** the receipt is still `approved`, the roll-ups are unchanged and no cancel audit entry exists.
- **Go test:** `TestDoc_Tx_ReverseEffectsFailureRollsBack`
- **Sources:** `frappe/frappe/tests/test_linked_with.py:540` (`test_background_cancel_is_all_or_nothing`), `frappe/frappe/tests/test_linked_with.py:561` (`test_background_delete_is_all_or_nothing`)

### DOC-078 · Hooks run inside the action's transaction and can't commit it
- **Priority:** P2
- **Level:** unit
- **Given** a fixture hook that writes a row through the `Tx` it was given **When** a later step fails **Then** that row is gone after rollback. The `Tx` passed to hooks exposes no `Commit` or `Rollback` method (compile-time check).
- **Go test:** `TestDoc_Hooks_ShareTx`
- **Sources:** `frappe/frappe/tests/test_db.py:549` (`test_savepoints_wrapper`), `odoo/addons/mail/models/mail_track_mixin.py:76-89` (tracking held until precommit)

### DOC-079 · Lifecycle invariants hold under random action sequences (fuzz)
- **Priority:** P1
- **Level:** unit (Go fuzz)
- **Given** 2 orders × 3 lines and up to 6 receipts, with random sequences of create, save, submit, withdraw, approve, reject, revise, cancel, close, amend and delete, and random failures injected into hooks **When** the sequence runs **Then** after every step: (a) each order line `received_qty` equals the sum of qty over receipt lines in approved or closed state; (b) no approved, pending or closed receipt points at a cancelled order; (c) no document outside draft has a different header or lines than when it left draft (except roll-ups); (d) the audit row count equals successful actions plus roll-up updates; (e) no audit row was ever changed; (f) a document has a number exactly when it is approved, closed or cancelled (draft, pending_approval and rejected documents have none), and numbers within a series are gapless.
- **Go test:** `FuzzDoc_LifecycleInvariants`
- **Sources:** BRIEF F5/F6, `erpnext/erpnext/controllers/status_updater.py:633-714`

### DOC-080 · Create writes an audit entry with every initial value
- **Priority:** P1
- **Level:** integration
- **Given** `priya` creates an order with supplier `SHREE` (id 7), date 2026-10-24 and one line **When** it is saved **Then** one audit row exists: `doc_type = "fixture.Order"`, `doc_id`, `user_id = priya`, `at` in UTC, `action = "create"`, `from_state = ""`, `to_state = "draft"`, and changes `[{field: "supplier_id", op: "set", before: null, after: {id: 7, code: "SHREE"}}, {field: "order_date", op: "set", before: null, after: "2026-10-24"}, {line_no: 1, line_id: …, op: "add", after: {…}}]` (`audit.Change` with `LineNo`, `LineID`, `Op`, CD-20).
- **Go test:** `TestAudit_Create`
- **Sources:** `odoo/addons/mail/models/mail_thread.py:350` (creation message). Contrast `frappe/frappe/core/doctype/version/test_version.py:229` (`test_no_version_on_new_doc`). Decision D-DOC-11

### DOC-081 · Save records only the changed fields, with before and after values
- **Priority:** P1
- **Level:** integration
- **Given** a draft with `remarks = ""` and `credit_days = 30` **When** `meena` saves `remarks = "Deliver to Pune site"` and leaves `credit_days` at 30 **Then** one audit row, `action = "save"`, with exactly `[{field: "remarks", before: "", after: "Deliver to Pune site"}]`.
- **Go test:** `TestAudit_SaveChangedFieldsOnly`
- **Sources:** `frappe/frappe/core/doctype/version/test_version.py:137` (`test_get_diff`), `frappe/frappe/model/document.py:2343-2366` (`save_version`)

### DOC-082 · Line changes are recorded as added, removed or changed
- **Priority:** P1
- **Level:** integration
- **Given** draft lines 1 (id 501, qty 10) and 2 (id 502, qty 5) **When** line 1's qty becomes 12, line 2 is removed and a line for `BOLT-M12` qty 3 is added **Then** the audit row holds `{field: "qty", line_no: 1, line_id: 501, op: "set", before: 10, after: 12}`, `{line_no: 2, line_id: 502, op: "remove", before: {…}}` and `{line_no: 2, line_id: <new>, op: "add", after: {item: BOLT-M12, qty: 3, …}}` (raw values are stored, e.g. qty 10 as `10000`; CD-20).
- **Go test:** `TestAudit_LineDiff`
- **Sources:** `frappe/frappe/core/doctype/version/version.py:104-196` (`get_diff` added/removed/row_changed), `frappe/frappe/core/doctype/version/test_version.py:194` (`test_get_diff_skips_ignore_versioning_field_in_child_rows`)

### DOC-083 · Every transition is recorded with from-state, to-state and reason
- **Priority:** P1
- **Level:** integration
- **Given** an order taken through submit → withdraw → submit → reject ("Rate too high") → revise → submit → approve → close ("Balance not needed") **When** the history is read **Then** it has transition rows `draft→pending_approval`, `pending_approval→draft` (action `withdraw`), `draft→pending_approval`, `pending_approval→rejected` (reason "Rate too high"), `rejected→draft` (action `revise`), `draft→pending_approval`, `pending_approval→approved` (the row where the number `PO/26-27/00001` first appears), `approved→closed` (reason "Balance not needed"), each with its user and UTC time.
- **Go test:** `TestAudit_Transitions`
- **Sources:** `frappe/frappe/core/doctype/version/version.py:239-245` (docstatus changes in diff), `odoo/addons/purchase/models/purchase_order.py:114` (`state` has `tracking=True`)

### DOC-084 · Roll-up changes on another document are recorded with their cause
- **Priority:** P1
- **Level:** integration
- **Given** approved order O, line 1 `received_qty = 0` **When** receipt GR/26-27/00001 (qty 4) is approved by `suresh` **Then** O gets an audit row `action = "effect"`, `user = suresh`, `cause_doc = GR/26-27/00001`, `cause_action = "approve"`, changes `[{field: "received_qty", line_no: 1, line_id: …, op: "set", before: 0, after: 4}]`.
- **Go test:** `TestAudit_EffectWithCause`
- **Sources:** `frappe/frappe/core/doctype/version/version.py:61-75` (`updater_reference`), `odoo/addons/purchase/models/purchase_order_line.py:460-462` (`_track_qty_received`)

### DOC-085 · The audit table can't be updated or deleted, even with raw SQL
- **Priority:** P1
- **Level:** integration
- **Given** an audit row **When** `UPDATE audit_log SET user_id = 1 WHERE id = ?` or `DELETE FROM audit_log WHERE id = ?` runs on the app's own connection **Then** SQLite aborts with "audit log is append-only" and the row is unchanged. No HTTP route accepts PUT, PATCH or DELETE on audit entries.
- **Go test:** `TestAudit_AppendOnlyTriggers`
- **Sources:** `odoo/addons/mail/models/mail_thread.py:531-532` ("Messages with tracking values cannot be modified"). Decision D-DOC-12

### DOC-086 · The audit is written in the same transaction as the action
- **Priority:** P1
- **Level:** integration
- **Given** an action that writes its audit row and then fails (fixture error after the audit insert) **When** it rolls back **Then** no audit row remains. Refused actions (validation, permission, stale version) leave no audit row.
- **Go test:** `TestAudit_SameTransaction`
- **Sources:** `odoo/addons/mail/models/mail_track_mixin.py:133-170` (`_track_finalize` at precommit), Decision D-DOC-21

### DOC-087 · Audit values are stored raw; links store the id and the code as of that moment
- **Priority:** P2
- **Level:** unit
- **Given** a change of `amount` from ₹1,25,000.50 to ₹1,30,000.00, `order_date` from 2026-10-01 to 2026-10-24, and `supplier_id` from 7 (`SHREE`) to 9 (`BHARAT`) **When** it is audited **Then** stored values are `12500050 → 13000000` (paise), `"2026-10-01" → "2026-10-24"`, and `{id: 7, code: "SHREE"} → {id: 9, code: "BHARAT"}`. Renaming the supplier code later doesn't change the stored history.
- **Go test:** `TestAudit_RawValues`
- **Sources:** `frappe/frappe/core/doctype/version/version.py:200-236` (stores formatted values and resolves link titles). Decision D-DOC-11 (differs: raw)

### DOC-088 · System fields are left out of the diff
- **Priority:** P2
- **Level:** unit
- **Given** a save that changes `remarks` (which also bumps `version`, `updated_at` and `updated_by`) **When** it is audited **Then** only `remarks` appears in the changes.
- **Go test:** `TestAudit_ExcludesSystemFields`
- **Sources:** `frappe/frappe/core/doctype/version/test_version.py:169` (`test_get_diff_skips_ignore_versioning_field`)

### DOC-089 · History shows newest first, in IST, with names
- **Priority:** P2
- **Level:** e2e
- **Given** an order with entries at the fixture clock 2026-10-24T05:00:00Z (create, Priya Shah) and 2026-10-24T09:15:00Z (save, Anil Kapoor) **When** the History tab is opened **Then** the first row reads "24-10-2026 14:45 · Anil Kapoor · Saved · Remarks: (empty) → Deliver to Pune site" and the second reads "24-10-2026 10:30 · Priya Shah · Created" (`types.FormatIST`, the only timestamp format, CD-33, CD-51). Money shows as ₹1,30,000.00. Owned and run by WP3 in `e2e/` (CD-40).
- **Go test:** `TestE2E_Audit_HistoryView`
- **Sources:** `frappe/frappe/core/doctype/version/test_version.py:17` (`test_generate_html_diff_produces_table`), `frappe/frappe/core/doctype/version/test_version.py:25` (`test_generate_html_diff_escapes_html`)

### DOC-090 · History is escaped and visible only to users who can read the document
- **Priority:** P2
- **Level:** integration
- **Given** a remarks change to `<script>alert(1)</script>`, and a user without read access to the order **When** the history is rendered for an authorised user **Then** the value appears as text, not as markup. **When** the unauthorised user asks for the history **Then** they get 403, the same as for the document itself.
- **Go test:** `TestAudit_EscapedAndPermissioned`
- **Sources:** `frappe/frappe/core/doctype/version/test_version.py:25` (`test_generate_html_diff_escapes_html`), `frappe/frappe/tests/test_document.py:269` (`test_permission`)

### DOC-091 · History of a deleted draft stays readable by admins
- **Priority:** P2
- **Level:** integration
- **Given** a draft deleted per DOC-061 **When** an admin opens "Deleted documents" and selects it **Then** the full history plus the snapshot is shown. Non-admins get 403.
- **Go test:** `TestAudit_DeletedDraftViewable`
- **Sources:** `frappe/frappe/model/delete_doc.py:253-262` (`Deleted Document`)

### DOC-092 · Full purchase lifecycle in the browser
- **Priority:** P1
- **Level:** e2e
- **Given** buyer `priya`, approver `ravi` (Purchase Manager) and storekeeper `suresh` **When** priya creates the reference PO (2 lines, project PRJ-METRO) and submits it, ravi approves it, suresh creates and submits a GRN for line 1 qty 400.000 kg, ravi tries to cancel the PO, then the GRN is cancelled, the PO is cancelled with a reason, and the PO is amended **Then** in turn: the PO shows "Draft 42" while pending, then "PO/26-27/00001" and "Approved", then received 400 of 1,200 kg; the cancel attempt shows the DOC-052 message with a link to the GRN; after the GRN cancel, the PO shows received 0; the PO shows "Cancelled"; a new draft (shown as `Draft {id}`) opens with a link "Amended from PO/26-27/00001", and the history of both documents shows every step. Owned and run by WP3 in `e2e/` (CD-40).
- **Go test:** `TestE2E_Doc_PurchaseLifecycle`
- **Sources:** `erpnext/erpnext/buying/doctype/purchase_order/test_purchase_order.py:808` (`test_purchase_order_invoice_receipt_workflow`), `odoo/addons/purchase/tests/test_purchase_flow.py:14` (`test_basic_purchase_flow_with_minimal_access_rights`)

### DOC-093 · An edit in two tabs shows the conflict message and keeps the user's input on screen
- **Priority:** P2
- **Level:** e2e
- **Given** the same draft open in two tabs **When** tab 1 saves and then tab 2 saves **Then** tab 2 shows the DOC-069 message with a "Reload" button. The typed values stay in the form until the user reloads. Owned and run by WP3 in `e2e/` (CD-40).
- **Go test:** `TestE2E_Doc_StaleTabConflict`
- **Sources:** `frappe/frappe/tests/test_document.py:231` (`test_conflict_validation`)

### DOC-094 · Buttons shown match the allowed transitions
- **Priority:** P2
- **Level:** e2e
- **Given** documents in each state, viewed by a user with all rights **When** each is opened **Then** draft shows Save, Submit, Delete; pending_approval shows Withdraw (users with the `submit` right) and Approve and Reject (approver only); rejected shows Revise and never an "Edit" button (CD-51); approved shows Cancel and Close; closed and cancelled show no action except (cancelled) Amend; and every form field is read-only outside draft. Owned and run by WP3 in `e2e/` (CD-40).
- **Go test:** `TestE2E_Doc_ButtonsPerState`
- **Sources:** `tryton/modules/purchase/purchase.py:314-320` (button visibility by state), `tryton/modules/purchase/purchase.py:82-166` (fields read-only unless draft)

### DOC-095 · Withdraw moves a pending document back to an editable draft
- **Priority:** P1
- **Level:** integration
- **Given** `priya` submitted the reference PO (project PRJ-METRO), so it is `pending_approval` at version 3 and shows `Draft 42` **When** priya withdraws it at the fixture clock 2026-10-24T05:00:00Z **Then** `state = "draft"`, `version = 4`, and exactly one audit transition entry `pending_approval → draft` with action `withdraw`, by priya, at 2026-10-24T05:00:00Z, with no reason. DOC calls APR (fake) `Record(withdraw)` once, inside the same transaction, after its own state guard and the `submit` right check (CD-23); APR closes the open request as withdrawn. `ApplyEffects` and `ReverseEffects` are not called, NAM `Assign` is not called and the PO still shows `Draft 42`. **When** priya then saves a changed line **Then** it saves (version 5). **When** she submits again **Then** APR `Evaluate` and `Open` run again and the state is `pending_approval`.
- **Go test:** `TestDoc_Withdraw_PendingToDraft`
- **Sources:** Spec §3.1 R2 and §3.2 (withdraw). Approval-request and inbox side: `04-approvals.md` (APR-091+)

### DOC-096 · Withdraw is one transaction
- **Priority:** P1
- **Level:** integration
- **Given** a pending order (`Draft 42`, version 3) and an APR fake whose `Record(withdraw)` returns an error **When** priya withdraws it **Then** the action fails, `state = "pending_approval"`, `version = 3`, no audit row is written, and the order is still read-only.
- **Go test:** `TestDoc_Withdraw_Atomic`
- **Sources:** Spec §3.2 (one transaction per action). Decision D-DOC-21

### DOC-097 · Info describes a document for other kernel packages
- **Priority:** P1
- **Level:** integration
- **Given** the reference PO created by `priya` on project `PRJ-METRO`, and an expense claim (`fixture.Claim`) created by `deepak` for employee EMP-007 **When** another package calls `Engine.Info(ctx, tx, doctype, id)` (`doctype.InfoSource`, CD-05) **Then** the order gives `DocType = "fixture.Order"`, `DocLabel = "Purchase Order"`, `State = draft`, `Number = ""`, `Display = "Draft {id}"`, `Party = "SHREE Shree Steel Traders"` (CD-55, master display "{Code} {Name}"), `CreatedBy = priya` and `ProjectIDs = [PRJ-METRO]`, taken from the DocType's `OwnerField` and `ProjectFields`; the claim gives `DocLabel = "Expense Claim"` and `EmployeeID` = EMP-007 from its `EmployeeField` (its `Party` is empty in Wave 1, because the fixture claim's employee field is not a master link). A missing id, or a master DocType, gives `doctype.ErrNotFound`.
- **Go test:** `TestDoc_Info_FromRegistryFields`
- **Sources:** Spec §5 `doctype.Info` and `InfoSource`; contract decisions CD-05, CD-09

### DOC-098 · References lists the documents that use a master record
- **Priority:** P1
- **Level:** integration
- **Given** supplier `RAJKOT` used by draft order D (created first) and approved order A, and item `WIRE-18G` used on a line of a cancelled order **When** `Engine.References(ctx, tx, "fixture_party", RAJKOT, 10)` runs **Then** it returns `[{fixture.Order, "supplier_id", D, "Draft {D}", draft}, {fixture.Order, "supplier_id", A, "PO/26-27/00001", approved}]`, ordered by DocType, then id; with limit 1 it returns only D, and `IsReferenced` returns `("Draft {D}", true)`. The item gives one reference with field `"lines.item_id"` and state `cancelled` (drafts and cancelled documents count). An unused item (`SVC-CRANE`) gives none.
- **Go test:** `TestDoc_References_ListsDocuments`
- **Sources:** Spec §5 `doc.Reference`; contract decision CD-22. `frappe/frappe/model/delete_doc.py:332-412` (`get_linked_docs`)

### DOC-099 · fixture.Claim is an employee-based approvable document whose amount is the sum of its lines
- **Priority:** P1
- **Level:** integration
- **Given** `deepak` creates a `fixture.Claim` (label "Expense Claim", series `EX/{FY}/{#####}`) for employee EMP-007 with lines TRAVEL ₹1,250.00 and FOOD ₹480.50 **When** he submits it **Then** APR `Evaluate` gets amount ₹1,730.50 (`Hooks.Amount` = sum of line amounts, also stored in the claim's `ReadOnly` `total` field, CD-79) and requester Deepak, `Open` is called, and the claim is `pending_approval`. **When** `priya` approves it **Then** it is `approved` with number `EX/26-27/00001`.
- **Go test:** `TestDoc_Claim_SubmitAndApprove`
- **Sources:** Contract decision CD-25 (needed by APR-020, APR-037, APR-044–051, APR-070); `00-fixtures.md` numbering and approval rules

### DOC-100 · A document number can't be supplied through save
- **Priority:** P1
- **Level:** integration
- **Given** a new order, and an existing draft **When** a save request includes `number = "PO/26-27/00099"` **Then** it is refused with NAM's number-guard error (`naming.GuardNumberField`, CD-28) on field `number`, nothing is created or changed and no audit entry is written. An empty `number` value is ignored. The exact wording is NAM's (NAM-048, NAM-049).
- **Go test:** `TestDoc_Save_NumberGuarded`
- **Sources:** Contract decision CD-28; `02-naming.md` NAM-048, NAM-049

### DOC-101 · A blocked master is refused with the master's own reason
- **Priority:** P2
- **Level:** integration
- **Given** supplier `SHREE`'s lookup reports blocked "Shree Steel Traders is on hold. Ask accounts before ordering." and item `WIRE-18G`'s reports "Binding Wire 18G is blocked for purchase." **When** a draft using both is saved **Then** the errors are `supplier_id: "Shree Steel Traders is on hold. Ask accounts before ordering."` and `lines[2].item_id: "Line 2: Binding Wire 18G is blocked for purchase."`. An approved order using `SHREE` can still be closed or cancelled (as DOC-035).
- **Go test:** `TestDoc_Link_BlockedMaster`
- **Sources:** Contract decision CD-21 (`MasterInfo.Blocked`); `erpnext/erpnext/accounts/party.py:904-912` (supplier on hold)

### DOC-102 · Load returns a document's values in the form value format
- **Priority:** P1
- **Level:** integration
- **Given** the reference PO created by `priya` (supplier `SHREE`, project `PRJ-METRO`, lines MS-ANGLE-50 1,200.000 kg at ₹62.5000 and WIRE-18G 50.000 kg at ₹96.0000, amount ₹94,164.00) **When** `Engine.Load(ctx, tx, "fixture.Order", id)` runs (CD-43) **Then** the `doc.Record` has `Info` equal to `Engine.Info` for the same id, `Version = 1`, `Values` in the plain form value format (`order_date = "2026-10-24"`, `amount = "94164.00"`, `supplier_id` = SHREE's id, `priority = "normal"`, empty text as `""`), and `Lines` in `line_no` order with `"id"` = the line id, `qty = "1200.000"`, `rate = "62.5000"` and the read-only roll-up `received_qty = "0.000"`. Sending the record back as a save (`Values`, `Lines`, `Version` unchanged) changes nothing: no version bump and no audit entry. A missing id or a master DocType gives a 404 error wrapping `doctype.ErrNotFound`.
- **Go test:** `TestDoc_Load_PlainValues`
- **Sources:** Contract decision CD-43 (requested by the UI work package, CC-UI-3); spec §5 `doc.Record`

### DOC-103 · Every engine error carries its HTTP status
- **Priority:** P1
- **Level:** integration
- **Given** actions that fail in each way the engine can refuse **When** the error is inspected as `doctype.HTTPError` (CD-44) **Then** a validation failure is 422 with its field errors (`FieldErrors()`, e.g. path `supplier_id`); a state refusal (DOC-005), a stale version (DOC-069) and a blocked cancel (DOC-052) are 409 with no field errors; a missing document (`Do` or `Load`) is 404 and `errors.Is(err, doctype.ErrNotFound)`; a busy database is 503 (DOC-075). A permission refusal is AUTH's own error (403), passed through unchanged, not re-wrapped by DOC. Each error's text is exactly the message the other cases state.
- **Go test:** `TestDoc_Errors_HTTPStatus`
- **Sources:** Contract decisions CD-44, CD-54

### DOC-104 · Approve and reject check visibility, then the approver, then the state
- **Priority:** P1
- **Level:** integration
- **Given** a pending order **When** a user who can't see it (outside their read scope) approves or rejects it **Then** the error is AUTH's not-found error (404, `doctype.ErrNotFound`), APR `CheckApprover` is never called and nothing changes. **When** a user who can see it but whose roles lack the `approve` right in AUTH approves it **Then** DOC does not refuse it itself (it runs no `Can(approve)`); whether the user may approve is APR `CheckApprover`'s decision, and with the fake allowing it the order is approved as `PO/26-27/00001`. **When** `CheckApprover` refuses **Then** its error is returned unchanged, even when the document is not pending or the version is stale, because it runs before DOC's from-state guard and version check (CD-52).
- **Go test:** `TestDoc_ApproveReject_CheckOrder`
- **Sources:** Contract decisions CD-23, CD-52; `04-approvals.md` (APR cases where the user can't see the record expect 404)

### DOC-105 · Saving checks Project/Site restrictions and the visibility of linked documents
- **Priority:** P1
- **Level:** integration
- **Given** an AUTH fake that refuses project `PRJ-TECHPARK` for the user **When** `priya` creates an order on `PRJ-TECHPARK`, or saves a draft with that project **Then** DOC has called `auth.Service.CheckValues` with `{"project_id": [PRJ-TECHPARK]}` (every `ProjectFields`/`SiteFields` entry, CD-70), AUTH's 403 error is returned unchanged, and nothing is created, changed or audited. With `PRJ-METRO` the save succeeds. **Given** an approved order the user may not see **When** `suresh` saves a receipt with two lines on that order **Then** DOC has called `auth.Service.CheckLink(…, "fixture.Order", order id)` once, AUTH's error is returned unchanged and nothing is written. Submit, approve and the other transitions make neither call.
- **Go test:** `TestDoc_Save_ChecksRestrictions`
- **Sources:** Contract decision CD-70; `01-auth.md` AUTH-099, AUTH-101

### DOC-106 · A master warning is shown but doesn't refuse the save
- **Priority:** P2
- **Level:** integration
- **Given** supplier `BHARAT`'s lookup reports the warning "Bharat Tools asks for advance payment on new orders." and item `BOLT-M12`'s reports "Hex Bolt M12x60 is low in stock." **When** an order with `BHARAT` and `BOLT-M12` on line 2 is saved, and then submitted **Then** both succeed and each `doc.Result.Warnings` holds "Bharat Tools asks for advance payment on new orders." and "Line 2: Hex Bolt M12x60 is low in stock." (CD-68). A document with no warnings returns none. Warnings are not written to the audit. (Showing them after the redirect is the UI's job, through `web.Flash`, CD-80.)
- **Go test:** `TestDoc_Link_MasterWarning`
- **Sources:** Contract decisions CD-21 (`MasterInfo.Warning`), CD-68

## Deferred and excluded
| Scenario | Source | later/never | Reason |
|---|---|---|---|
| Update qty and rate of lines after approval ("Update Items") | `erpnext/erpnext/buying/doctype/purchase_order/test_purchase_order.py:339`, `:509`, `:590`; `odoo/addons/purchase/models/purchase_order_line.py:438-458` | later | F5 forbids editing submitted documents (kept by spec R7; listed as later in spec §11). See flag in D-DOC-07. If added, it must refuse to reduce below received qty (`test_purchase_order.py:509`) and must be refused on closed documents (`test_purchase_order.py:2413`). |
| Reopen a closed document | `erpnext/erpnext/buying/doctype/purchase_order/purchase_order.py:657` | later | F5 makes closed terminal (kept by spec R6). Later: reopen by a manager only, audited (spec §11). Flagged in D-DOC-08. |
| On Hold state | `erpnext/erpnext/buying/doctype/purchase_order/test_purchase_order.py:848` (`test_purchase_order_on_hold`) | later | Not in F5. Close covers the common need. |
| Fields editable after submit (`allow_on_submit`) | `frappe/frappe/tests/test_document.py:320` | never (v1 schema guard DOC-051) | F5: submitted documents are never edited. |
| Draft discard (draft → cancelled) | `frappe/frappe/tests/test_document.py:156`, `:173` | never | Delete the draft instead, and the audit keeps a snapshot. |
| Delete cancelled documents | `frappe/frappe/model/delete_doc.py:307-325`; `odoo/addons/purchase/models/purchase_order.py:469` | never | F5 and audit evidence. |
| Background document locks for queued actions | `frappe/frappe/tests/test_document_locks.py:9`, `:21`, `:41` | never | There is no background job queue for document actions. SQLite transactions and `version` cover concurrency. |
| Odoo lock/unlock of confirmed PO | `odoo/addons/purchase/tests/test_purchase.py:1190`, `:1346` | never | Approved documents are already immutable. |
| "Cancel all linked documents" in one click (cascade) | `frappe/frappe/tests/test_linked_with.py:236`-`:660` | later | Risky for users. Cancelling one document at a time is clearer for 20 people. |
| Bulk actions over many documents (submit or delete selected) | `frappe/frappe/tests/test_linked_with.py:521` | later | Each document would be its own transaction. Not needed for v1 volumes. |
| Over-receipt or over-billing allowance percentages | `erpnext/erpnext/buying/doctype/purchase_order/test_purchase_order.py:123` | module (GRN) | Tolerance is a module rule. DOC only guarantees the check runs inside the write transaction (DOC-072). |
| Returns against closed sources | `erpnext/erpnext/stock/doctype/purchase_receipt/test_purchase_receipt.py:747` | later | No return documents in v1. |
| Section, subsection and note lines (`display_type`, Tryton comment lines) | `odoo/addons/purchase/models/purchase_order_line.py:435`; `tryton/modules/purchase/tests/scenario_purchase_modify_header.rst:77` | later | Not needed by PO, GRN or Expense v1. |
| Line exception handling (cancelled moves → "exception" state, ignore) | `tryton/modules/purchase/tests/scenario_purchase_line_cancelled.rst:76-102` | never | Tryton-specific shipment model. Our cancel and recompute covers it. |
| Modify header in draft via a wizard (re-applies taxes on party change) | `tryton/modules/purchase/tests/scenario_purchase_modify_header.rst:82-92` | never | Drafts are freely editable. Recalculation on save is a module concern (NUM/MST for tax). |
| `set_only_once` fields | `frappe/frappe/model/document.py:1498-1525` | later | No v1 field needs it. Drafts are fully editable, then frozen. |
| Field-level permissions (perm levels) and masking | `frappe/frappe/model/document.py:1557-1645` | later (AUTH) | v1 permissions are per DocType and record. |
| Impersonation in audit (`impersonated_by`) | `frappe/frappe/core/doctype/version/version.py:40-47` | never | No impersonation feature. |
| HTML diff for long text in history | `frappe/frappe/core/doctype/version/test_version.py:40`-`:120` | later | Plain before → after is enough for v1 fields. |
| Real-time "document changed" push to open tabs | `frappe/frappe/tests/test_document.py:651` (`test_realtime_notify`) | never (v1) | Out of scope (no live updates). The stale check catches it on save. |
| Virtual or lazy documents, extension classes, pickling | `frappe/frappe/tests/test_document.py:504`, `:838`-`:941`, `:1165`-`:1282` | never | Frappe runtime metadata internals. Our DocTypes are generated Go code. |
| Single (settings) documents' concurrency | `frappe/frappe/tests/test_document.py:248`, `:259` | later | Settings pages are not documents. If they get a `version`, they reuse DOC-069. |
| Schema migration tests (column type changes) | `frappe/frappe/tests/test_db_update.py:20`-`:644` | never | goose migrations are hand-written SQL, not runtime schema sync. |
| XSS sanitising on save | `frappe/frappe/tests/test_document.py:379` (`test_xss_filter`) | never | Values are stored raw and templ escapes on output (DOC-090). |

## Interfaces this area needs from others
- **AUTH** provides `auth.Service.Can(ctx, p, doctype, Right, rec) error`. DOC maps each action to a right (spec R4): save → `create` (new) / `write` (existing); submit, withdraw, close → `submit`; revise → `write`; cancel → `cancel`; amend → `amend`; delete → `delete`; viewing and history → `read`. Approve and reject need `approve`, but that right is checked by APR's `CheckApprover`; DOC checks only that the record is visible (`Can(read)`, a hidden record gives 404) and runs no `Can(approve)` (CD-52). There is no `reject` or `close` right. DOC calls it at the start of every action, inside the transaction, before the version check. On create and save DOC also calls `CheckValues` for every `ProjectFields`/`SiteFields` entry and `CheckLink` for every link to a document DocType, and returns AUTH's errors unchanged (CD-70, DOC-105). DOC builds the `auth.RecordRef` from the DocType's `OwnerField`, `EmployeeField`, `ProjectFields` and `SiteFields` (CD-09). AUTH also provides user display names through `auth.Service.User(ctx, tx, id)` for the history view and the "approved by" wording (CD-12, CD-26).
- **NAM** provides `naming.Service.Assign(ctx, tx, doctype, docID, d)`, called by DOC inside the transaction that first enters `approved` (approve, or submit when no rule applies), with the date from `Hooks.Date` (spec R1). It also provides `AssignAmendment(ctx, tx, doctype, docID, root, n)`, called when an amendment is approved. Both must roll back with the transaction (DOC-076). Draft, pending_approval and rejected documents have no number and display `Draft {id}` via `naming.Label` (CD-28; implemented for real in WP0, CD-53). DOC calls `naming.GuardNumberField` on save (DOC-100) and `naming.CheckAmendmentNo` on amend; the second-amendment refusal text is DOC-065's (CD-27). Withdraw and revise never assign a number, so deleting a draft never leaves a gap.
- **APR** provides `approval.Service` and DOC follows the §5 call protocol (CD-23), all inside DOC's one transaction: submit → `Evaluate` → `Open` always (APR records "submitted") → if no approval is needed, continue as approve without `CheckApprover` and `Record(approve)`; approve and reject → DOC's visibility check, then `CheckApprover` (which owns the approve right and eligibility messages), then DOC's from-state guard (CD-52), then `Record(approve)` / `Record(reject, reason)`; revise → `Record(revise)`; withdraw → DOC's state guard and the `submit` right first, then `Record(withdraw)`; delete of a draft makes no APR call. `Record`'s action strings are the `doctype.Action` values. DOC owns the state change (including `withdraw`, `pending_approval → draft`, spec R2), the reason storage and the audit row. APR owns who and when, the approval history and the inbox (`04-approvals.md`, APR-091+ for withdraw).
- **MST** registers each master as a DocType with `Master: doctype.MasterLookup` (CD-21). DOC validates every Link to a master through `Lookup(ctx, tx, id) (MasterInfo, found, error)`: not found, `Disabled` (DOC-034 wording, CD-38), `Blocked` (DOC-101, shown as given) and `Warning` (returned in `doc.Result.Warnings`, never a refusal, DOC-106; the UI shows it after the redirect through `web.Flash`, CD-80). Masters are shown as "{Code} {Name}" everywhere (CD-68). MST keeps referenced masters from being deleted (F4) using `Engine.References` / `IsReferenced` (CD-22).
- **FILE** provides `files.Service.CopyAll(ctx, tx, from, to)` for amend (copies recorded as uploaded by `to.CreatedBy`, CD-29) and `DeleteAll` for delete, and (through `print.Register`) must allow printing documents in any state, including cancelled (watermarked) and closed, for users who have the `print` right on the DocType and can see the record (spec R3). FILE and PRINT read document facts through `doctype.InfoSource`, which DOC's engine implements (CD-05; `Info.Party` added by CD-55).
- **NUM** provides money in paise (int64), quantity precision, `types.FormatIST(time)` ("24-10-2026 14:45", the only timestamp format) and `types.FormatINR(paise)` for the history view (CD-33). It also provides the plain-date type used in audit values.
- **platform/db** provides `tx.SQL()` as `db.DBTX` (no `Commit` or `Rollback`, CD-06), `db.ErrBusy` and `db.WithBusyTimeout` (CD-08).
- **Every module** (PO, GRN, Expense, …) implements `doctype.Hooks`, registers its `doctype.DocType` (with `LinkSpec` and `RollUp` declarations and its rights fields) through `doctype.Register` (= `doc.Register`), and runs every action through `doc.Engine.Do`. It must not open its own transaction or write documents outside the `Tx` it is given.
