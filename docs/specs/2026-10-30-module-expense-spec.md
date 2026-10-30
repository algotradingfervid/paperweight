# Module spec: Expense Claim and Employee Advance

Version 0.5 · 30 October 2026 · Status: for owner review, with CD-234, CD-235 and CD-237 applied · Author: AD-6

Follows the Purchase Order spec (`docs/specs/2026-10-30-module-purchase-order-spec.md`), whose §14 shared decisions, §12 cutover plan and §17 sources apply here. Marks **(CD-234)**, **[Assumption]** and **CA** mean the same as there. This module depends on no other module, so it can run beside the PO.

---

## 1. Purpose and approach

Deepak takes an advance before a site trip, photographs each receipt, types the amount and submits at the end. His manager approves it, the advance is set against the claim, and the balance is settled.

**Three ways to tie a receipt to an expense line.** The file store attaches files to documents, not lines, and the print and approval page need a receipt beside each line.

| | Approach | For | Against |
|---|---|---|---|
| **A (recommended)** | A line stores the attachment's SHA-256; the file stays attached to the claim | `files.CopyAll` on amend gives new ids but the same hash, so the link survives (FILE-043). Also finds a receipt used twice | Reading attachments in a write needs one small tx method, and FILE-045 was written for ids |
| B | A line stores the attachment id, as FILE-045 and `files.CheckReceipt` assume | Already in FILE | Amend copies files under new ids, so every line would need remapping, and no hook runs on amend |
| C | Each line is its own attachment parent | Clean model | Lines have no rights or lifecycle of their own; a kernel change |

**Three ways to settle an advance against a claim.**

| | Approach | For | Against |
|---|---|---|---|
| **A (recommended)** | The claim names one advance and an amount; a claim's effects recompute the advance's used total | Simple on a phone; no second line table on a claim | One advance per claim |
| B | A settlement table on the claim, many advances per claim | Handles every case | The kernel has one lines table per DocType, so this needs custom saving, audit and amend |
| C | A separate Settlement document | Clear audit | A third document for a 20-person company |

## 2. Scope

**In scope.** Expense Claim: one employee, up to 500 lines, photo-first entry, one reporting-manager approval, project and site tag per line, optional GST capture, print, reports. Employee Advance: request, approval, settlement against claims, closing with the balance noted (CD-234).

**Non-goals.**
- Per-diems, per-type or per-day limits, a policy engine.
- Mileage calculation: mileage is a plain amount (CD-234).
- Approver changing an amount: reject with a reason (CD-234).
- Claims for someone else in normal use. Only an Administrator can, for back-entry (§3.1).
- Company-card expenses, travel requests.
- Payment through the app, payment status, payroll deduction: no accounts tracking (CD-234).
- Credit decisions: blocked credit (s.17(5)) and place-of-supply limits are decided in your accounting system.
- Cash limits: no check (§8).

## 3. Expense Claim

`internal/modules/expense`: `Name: "expense.Claim"`, `Label: "Expense Claim"`, `PluralLabel: "Expense Claims"`, tables `expense_claim` and `expense_claim_line` (AUTOINCREMENT, CD-156; `parent_id`, `line_no`, CD-161), `Series: "EX/{FY}/{#####}"` (CD-115), `Submittable`, `Approvable`, `Printable`, `OwnerField: "created_by"`, `EmployeeField: "employee_id"`, `CloseRight: "cancel"` (§6).

`Links: []doctype.LinkSpec{{Field: "advance_id", Target: AdvanceDocType}}`, so an advance can't be cancelled while a live claim uses it (DOC-052).

**No `ProjectFields` or `SiteFields`** [Assumption]. Claims are scoped by own and team, not by project. Otherwise a project-restricted manager like Priya couldn't see Deepak's claim with no project (AUTH-093) and couldn't approve it (D-APR-17). The project on a line is a tag for reports.

`HasRoundedTotal` is false: a reimbursement pays the exact amount [Assumption].

### 3.1 Header

| Field | Label | Type | Rule |
|---|---|---|---|
| `number` | Claim number | Text, read-only | List priority 1 |
| `employee_id` | Employee | Link Employee, `AllowBlocked` | Blank defaults in `Validate` to the creator's employee (`auth.Directory.EmployeeForUser`). Shown only to an Administrator, through `Hide`, who types an employee code (`employee.ByCode`), because the picker hides leavers. Anyone else naming another employee is refused |
| `claim_date` | Claim date | Date | Required, default today. Not in the future. Sets the FY and is the lock date |
| `purpose` | Purpose | Text, max 140 | Required at submit. "Pune Metro site visit" |
| `advance_id` | Advance | Link `expense.Advance` | Optional. This employee's approved advance with something left. Shown as radios through a custom step, because `/pick/{Link}` can't filter. The claimant must be able to read it, or AUTH's `LinkError` answers first (CD-70) |
| `less_advance` | Less advance | Money | Default 0. Requires `advance_id`. Blank with an advance chosen defaults to the smaller of what's left and the total |
| `total` | Total | Money, read-only | Sum of line amounts. List priority 2 |
| `net_payable` | To pay | Money, read-only | Total − less advance |
| `period_from`, `period_to` | Period | Date, read-only | Earliest and latest line date, for the print |
| `state` | State | Select, read-only | List priority 3 |
| `remarks` | Remarks | Text, max 500 | Optional |

`Hooks.Amount` is `total`, not `net_payable`: the approver approves the spending. `Hooks.Date` is `claim_date`.

Hooks get no principal, so while the state is `draft`, `Validate` compares `employee_id` with the employee of `updated_by`, which is set before it runs (`internal/kernel/doc/save.go`). `created_by` wouldn't do: Deepak can edit a draft an Administrator entered for him. Only an updater holding the Administrator role (`auth.HasRoleTx`, CD-221) may name another employee. That supports back-entry. Pending and later states skip the check, so a role change can't stop an approval. Self-approval still holds, because eligibility excludes requester, creator and claimant (`approval.own`).

**Leavers.** The master blocks a Left employee on save, submit and approve (CD-179). That would strand a leaver's pending claim, stop its revise, and stop back-entry for staff who left during the year. So `employee_id` sets a new additive `Field.AllowBlocked`, which skips `Blocked` but not `Disabled` (§15 item 7). New claims by Left employees can't happen in normal use, since leaving disables the user.

Snapshots: `snap_employee_name`, `snap_employee_code`. `doc.partyOf` follows `EmployeeField`, so inbox cards show the employee.

### 3.2 Lines

| Field | Label | Type | Rule |
|---|---|---|---|
| `expense_date` | Date | Date | Required. Not in the future, not after the claim date |
| `expense_type_id` | Type | Link Expense Type | Required |
| `description` | Note | Text, max 200 | Optional; printed |
| `amount` | Amount | Money | Required, more than 0. Includes any GST. Mileage is an ordinary line with an amount (CD-234) |
| `project_id` | Project | Link Project | Optional |
| `site_id` | Site | Link Site | Optional |
| `receipt_sha256` | Receipt | Text, max 64 | Set by the line page after upload |
| `no_receipt_reason` | Why no receipt | Text, max 200 | Required at submit when there's no receipt (CD-234) |
| `company_bill` | Bill in company GSTIN | Check | Default off (CD-234) |
| `supplier_gstin` | Supplier GSTIN | Text | Required when ticked; `gstin.ValidateGSTIN` |
| `invoice_no` | Invoice no. | Text, max 40 | Required when ticked |
| `tax_code_id` | Tax code | Link Tax Code | Default from the expense type (MST-123) |
| `hsn_sac` | HSN/SAC | Text | Default from the expense type |
| `gst_amount` | GST on the bill | Money | Optional. Part of `amount`, not added to it. Not above it |

Line snapshots: `snap_type_code`, `snap_type_name`, `snap_project_code`, `snap_site_code`.

No `uom.RegisterQtyUse`: the table has no UoM column.

### 3.3 Receipts

- The line page uploads through `files` (one file per request, CD-133). `Spool` then `Attach` give a `Meta` with the SHA-256, which goes in `receipt_sha256`.
- `Validate` checks the hash is among this claim's attachments, through a new `(*files.Store).ListTx(ctx, tx, parent)` that reads in the caller's transaction (CD-209's rule). A hash that isn't there gets FILE-045's text with this line's number (`The receipt on line {n} belongs to another document. Attach it to this expense first.`). `files.CheckReceipt` isn't used.
- One bill covering two lines: the store refuses identical bytes on one parent (`AttachLimits`), so the line page offers `Same receipt as line {n}`, and both lines hold the same hash.
- A `files.RemoveGuard` for `expense.Claim` finds a line holding the attachment's hash and refuses with FILE-046's text, in draft and rejected states alike. The user clears the receipt on the line, then removes the file.
- Attachment policy for `expense.Claim`: default types, `MaxCount: 100` instead of 20, because a claim may hold a receipt per line [Assumption]. A claim needing more goes in two claims. The limit message becomes `A document can have at most {n} attachments. Remove one first.`, the same text at 20 (§15 item 2).
- Print: `HasReceipt` is true when the hash is found.

## 4. Employee Advance (CD-234)

`Name: "expense.Advance"`, `Label: "Employee Advance"`, `PluralLabel: "Employee Advances"`, tables `employee_advance` and `employee_advance_line`, `Series: "EA/{FY}/{#####}"` (14 characters), `Submittable`, `Approvable`, `Printable`, `OwnerField: "created_by"`, `EmployeeField: "employee_id"`, `CloseRight: "cancel"`.

It has a lines table because `checkFields` refuses a submit with no lines on every DocType (`internal/kernel/doc/validate.go`), and because people ask for an advance by purpose. HRMS keeps it as a separate document too.

**Header.** `number`; `employee_id` (same rules as a claim); `advance_date` (Date, required, not in the future; sets the FY and is the lock date); `purpose` (required at submit); `amount` (Money, read-only, sum of lines, list priority 2); `used_amount` (Money, `System`, `ReadOnly`, `EditableAfterSubmit`: total set against claims); `left_amount` is never stored: `amount − used_amount`.

**Lines.** `expense_type_id` (required), `description`, `amount` (more than 0), `project_id`, `site_id`. No receipts: nothing has been spent yet.

**Rule.** Entered at go-live: Employee Advance, ₹0, reporting manager, fallback role Fallback Approver, like claims (§6).

**Settlement.**

| Case | What happens |
|---|---|
| Claim uses an advance | At submit, `BeforeSubmit` reads inside the transaction (CD-94): the advance belongs to this employee, it's approved, and `less_advance` is no more than what's left, counting other approved and closed claims |
| Claim approved | `ApplyEffects` recomputes `used_amount` as the sum over approved and closed claims naming this advance and bumps the advance's `version`. It writes an audit entry with action `effect`, `By` the claim's `updated_by`, and `CausedBy` the claim with action `approve`, since a hook can't tell submit from approve. Recompute, never increment (D-DOC-13) |
| Claim cancelled | `ReverseEffects` recomputes the same way, with cause `cancel` |
| Claim spends more than the advance | `net_payable` is the balance owed to the employee |
| Claim spends less | `left_amount` stays on the advance for the next claim |
| Nothing more to come | **Close** the advance with a reason, for example `₹2,000 returned by NEFT 1234 on 30-11-2026`. A closed advance can't be picked again (DOC-017). The app holds the note, not a payment record. Close has no hook, so a claim pending on the advance isn't checked: its approve then fails with DOC-017's text, and the approver rejects it |
| Advance not paid out | Cancel it with a reason |

A `less_advance` of 0 with an advance named is refused, so a claim can't block an advance's cancel for nothing.

Pending claims don't count against what's left until approved, but `BeforeSubmit` runs again on approve (D-APR-15), so two claims can't overspend one advance. The second gets a refusal at approval and is rejected or fixed.

## 5. Validation

Required at submit (CD-178). `Validate` reads inside the transaction, computes totals and period, and copies type defaults when blank, the same way as PO defaults.

| Condition | Message | When |
|---|---|---|
| Creator has no employee | `Your user isn't linked to an employee. Ask your administrator.` | Save |
| Another employee, creator not Administrator | `You can only raise claims for yourself.` | Save |
| Claim date in the future | `Claim date can't be in the future.` | Save |
| Line date in the future | `Row {n}: Date can't be in the future.` | Save |
| Line date after claim date | `Row {n}: Date can't be after the claim date.` | Save |
| Less advance with no advance | `Choose the advance to set this against.` | Save |
| Advance named, less advance 0 | `Enter the amount to set against {advance}, or remove the advance.` | Submit |
| Less advance above total | `Less advance can't be more than the total.` | Save |
| Advance of another employee, readable by this user (a manager's team, a Director) | `{advance} belongs to another employee.` An unreadable advance gets AUTH's link refusal first | Save |
| Less advance above what's left | `Less advance can't be more than the ₹{x} left on {advance}.` | Submit, Approve |
| No receipt, no reason | `Row {n}: Add a receipt or say why there isn't one.` | Submit |
| Receipt hash not on this claim | FILE-045's text (§3.3) | Save |
| Ticked with no GSTIN | `Row {n}: Supplier GSTIN is required for a bill in the company GSTIN.` | Submit |
| Ticked with no invoice no. | `Row {n}: Invoice no. is required for a bill in the company GSTIN.` | Submit |
| Bad GSTIN | MST-031's text with `Row {n}: ` (CD-38) | Save |
| GST above amount | `Row {n}: GST can't be more than the amount.` | Save |

An advance's employee and date rules use the same text, with `Advance date` as the label and `advances` for `claims`.

**Warnings, never refusals.** Shown on the check and approval pages through `Module.Check`:
- Same receipt hash on another live claim: `Row {n}: This receipt is also on {claim}.`
- Same employee, date, type and amount on another live claim: `Row {n}: This looks the same as a line on {claim}.`

Odoo's duplicate checks work the same way and are soft there too. A cancelled claim doesn't count.

## 6. States, close and approvals

| Action | Who | Module work |
|---|---|---|
| Save, submit, withdraw, revise | Employee, own | §5 |
| Approve, reject | Reporting manager, else a Fallback Approver (§6) | Eligibility excludes the claimant |
| Close | Director, Administrator (CD-235) | Claim: reason says how it was settled. Advance: §4 |
| Cancel | Director, Administrator (CD-235) | Reason required; lock check; recompute the advance |
| Amend | Employee, own | New draft; the amender becomes `created_by` |

**Close needs a new owner.** There's no Accounts role (CD-234), and the kernel gives `close` to the `submit` right (R4), so every employee could close their own claim. This spec proposes `doctype.DocType.CloseRight string`: `""` means `submit` as today, `"none"` means no close, otherwise a right name. Claims and Advances set `"cancel"`. Cancel is held by Director and Administrator, so one role does both. GRN sets `"none"`.

**Close is optional.** It's the app's only "done" state for a claim, and CD-234 tracks no payments. A closed claim can't be cancelled or amended (R6). A claim left approved forever is harmless. The page shows the kernel's words: button `Close`, field `Reason for closing`, hint `For example how it was paid`.

**Fallback approver (owner's answer, confirmed 30 October, CD-236, CD-238).** The Director keeps the Employee role and files claims in the app. Someone else must approve them, so:

- A rule has one fallback role (`approval.Rule.FallbackRole`). A new canonical role, **Fallback Approver**, holds read and approve (all) on Expense Claim and Employee Advance, and nothing else. A second Director also works, but the Director role carries PO approval and cancel rights, which this person may not need.
- The owner gives Fallback Approver to the Director and to at least one other person. That person may be the Administrator, who still needs the role, because eligibility goes by role (`holdsAny`), not by right alone.
- Both rules (Expense Claim and Employee Advance, ₹0, reporting manager) name Fallback Approver. No code holds a name: both are data in the approval rules screen.

| Claim from | Who approves |
|---|---|
| Employee with a manager | The manager. A Fallback Approver can also act (below) |
| Employee with no manager, or a manager with no user | A Fallback Approver |
| The Director | The other Fallback Approver |
| The other Fallback Approver | Their manager, usually the Director; with no manager, the Director |
| The only active Fallback Approver | Submit is refused with APR's text: `No one else can approve this Expense Claim. Give the Fallback Approver role to another active user, then submit again.` The draft stays intact (D-APR-08) |

The fallback role is an approver role on the rule, so its holders can act on any claim and read them all. Accepted: it is the same reach the Director already has.

**When the second person leaves.** A disabled user is never eligible (D-APR-18). The Director's own pending claim then has nobody, so it shows on Stuck approvals (APR-043) until another person gets the role. Withdrawing also works. Other claims are unaffected, because the Director still holds the role. The go-live check confirms two active holders.

**One edge.** An Administrator who is the second holder can't approve a Director's claim they entered themselves, because the creator is excluded. The Director files their own.

The approver can't change an amount. A claim with one wrong line goes back with a reason, and revise puts it in draft, then through approval again (D-APR-11). A claim pending when the manager leaves stays approvable, because the fallback role is on the request (`approverRoles`).

## 7. Numbering

`EX/{FY}/{#####}` and `EA/{FY}/{#####}`, 14 characters each, at first approval. Two different prefixes, so D-NAM-13 holds. FY from `claim_date` and `advance_date`.

## 8. GST and the audit's open items

| Item | Decision | CA |
|---|---|---|
| GST on claims | Optional tick per line (CD-234). Captures supplier GSTIN, invoice no., tax code and GST amount for keying. No split, no credit decision. `gstcalc.CheckEnteredGST` isn't used, because the app takes the bill's figure as a note | CA: credit needs the company's GSTIN on the bill (Rule 46(d), Rule 36 proviso) |
| Place of supply | Not computed on claims. A hotel charges by its own location | CA: a Karnataka hotel charging CGST and SGST to a Maharashtra GSTIN may give no credit (IGST s.12(3), Circular 170/02/2022) |
| Blocked credit | Not decided. The expense type's tax code is a default only | CA: food, personal vehicles, health insurance and club fees are commonly blocked (CGST s.17(5)). The 57th Council's recommended changes aren't law yet |
| Fuel | No GST field needed; the tick stays off | CA: petrol and diesel are outside GST at present |
| Cash limit | No check. The app doesn't know how a bill was paid | CA: Income-tax Act 2025 s.36(4), ₹10,000 per day to a person. How it applies to reimbursements couldn't be verified |
| TDS, RCM, IRN, e-way | Not relevant to the claim screen | CA |
| Retention | Receipts stay as attachments; backups per CD-226, CD-228 | CA: GST 72 months from the annual return due date (CGST s.36). Rule 56(8) wants changes logged, which the audit does. Income-tax rule period unconfirmed |

## 9. Links to masters

| Link | Rule |
|---|---|
| `employee_id` | Leavers stay usable through `AllowBlocked` (§3.1); only an Administrator can name one |
| `expense_type_id` | Enabled. `expensetype.Defaults` gives HSN and tax code, 0 for a disabled tax code (CD-216 item 4) |
| `project_id`, `site_id` | Enabled. Pickers respect restrictions, so Priya tags only her project. Scope on the claim itself isn't affected |
| `tax_code_id` | Enabled when set |

Snapshots keep old claims stable (D-MST-03). Amend needs PO spec §15 item 8 to refill them.

## 10. Print

`print.ExpenseDocument` through `print.Register` (FILE-068), filled from snapshots. Additive fields, omitted when empty: `Purpose`, `LessAdvance`, `AdvanceNumber`, `NetPayable` (when set, the words use it), and `Project` on `ExpenseLine`. Golden `expense_approved` updates.

Employee Advance: a new small `AdvanceDocument` in `print`, on the same pattern: letterhead, title `Employee Advance`, employee, date, purpose, lines, total, words, approved by, signatory. A golden file is added, and FILE-066 then covers it.

## 11. Rights and restrictions

Both DocTypes:

| Role | Rights (scope) |
|---|---|
| Employee | read (team); create, write, submit, amend, print (own); delete (own); approve (team) |
| Director | read, approve, cancel, print, export (all) |
| Fallback Approver | read, approve (all) |
| Administrator | every applicable right |

- Team is direct reports only (CD-11), which is why Priya sees and approves Deepak's claims.
- The Director also holds Employee, so they can file their own claims (`00-fixtures.md` changes for Meena).
- Fixture `Employee` has no `amend` or `delete`; both join here so a person can correct their own work. An employee can't self-approve, so a claimant amending is safe.
- No one outside the employee's line sees a claim except Director and Administrator, since there's no Accounts role.

## 12. Screens

UX design and approval first (CD-97). Everyone files these on a phone, so 360 px comes first. Steps from `docscreen.FormSteps`: Claim details → Expenses → Check and submit, as in design-system §8.7. The `Items` step is relabelled `Expenses` through a custom step.

| Screen | Notes |
|---|---|
| Claim details | Purpose, claim date. Disclosure `Set against an advance`: radios of open advances, each with what's left; less advance. Employee for an Administrator |
| Expenses | Add-to-a-list. Card: thumbnail, type, date, amount |
| Expense line page | Own URL. Photo first (`Take photo`, `Choose file`), then type radios, date (`Today`, `Yesterday`), amount, note. `No receipt` reveals the reason. Disclosures `Bill in company GSTIN` and `Project or site` |
| Check and submit | Thumbnail per expense, total, less advance, to pay, in words, duplicate warnings, `Goes to Ravi Kulkarni (your manager)`, `Submit for approval` |
| Approval page | §8.5, receipts next to each line, full screen on tap |
| Advance wizard | Details → Items → Check and submit |
| Advance document page | Amount, used, left, claims set against it, read through `doc.LinkedBlockers` |

**Measurable goal for the UX document:** one expense with a photo in under 30 seconds on a phone.

## 13. Reports and exports

The generic list and CSV cover both registers. Each report is guarded by `read` with `export` checked in the handler, and scope applied, so a manager sees the team only.

**Open claims** (`/reports/expense/open`). Approved, not closed. Columns: claim, employee, purpose, approved on, approved by, total, less advance, to pay, lines without a receipt. Mirrors HRMS's Unpaid Expense Claim, without payment figures.

**Expense lines** (`/reports/expense/lines`). One row per line: claim, employee, date, type, project, site, note, amount, GST, supplier GSTIN, invoice no., receipt yes or no, state. Filters: month, type, project, employee. Staff key your accounting system from this CSV, and project spend comes from it.

**Open advances** (`/reports/expense/advances`). Approved, not closed. Columns: advance, employee, date, purpose, amount, used, left, age in days. Filter: employee.

## 14. Cutover

PO spec §12 applies.
- Back-entered claims and advances use their real dates. An Administrator enters them for employees (§3.1). With no approval rule they approve on submit.
- No photo: type `Paper file` as the reason. That's one short entry per line, and there's no bulk route [Assumption].
- Set `EX/` and `EA/` series as in the PO plan; the owner has no earlier advance numbers on record that I know of.
- Advances open on go-live day go in as back-dated advances, then their claims set against them, or as an advance for the balance. The Administrator picks one.
- Each employee needs a linked user and, normally, a reporting manager before rules go in.
- Go-live day: give Fallback Approver to the Director and a second person, confirm both are active, then enter the rules for Expense Claim and Employee Advance: ₹0, reporting manager, fallback role Fallback Approver.

## 15. Proposed changes for the head

1. **`doctype.DocType.CloseRight`** (§6). Additive across DOC, `auth` and `authfake` with parity rows, and `docscreen`'s button and reason page. Core spec R4 gains a line.
2. **FILE:** in MOD-0, `(*files.Store).ListTx` per CD-209, `msgLimit` takes `pol.MaxCount`, and FILE-045 and FILE-046 gain hash wording. In MOD-EXP, the claim's policy and `RemoveGuard` go in `files.New`'s wiring. `filesfake` stays on DefaultPolicy, so EXP-010 runs on the real store.
3. **`00-fixtures.md`:** the `expense.Claim` and `expense.Advance` rows only: Employee gains `amend` and `delete` (own); Director gains `cancel`; new canonical role Fallback Approver with no fixture user, since EXP tests give it to Meena and Kavya in setup; Employee Advance rows. `fixture.Claim`'s Director fallback stays, so existing approval tests don't move. Meena gains Employee in EXP test setup, for the same reason.
4. **`print.ExpenseDoc`** additive fields, and a new `AdvanceDocument`.
5. **`docscreen` module hooks:** this spec uses `Steps`, `Line`, `LineCard`, `CardsOnly`, `Hide`, `Check` and `Page` from the hooks spec, whose §3 is authoritative.
6. **Series `EA/{FY}/{#####}`** joins `00-fixtures.md`'s numbering table.
7. **`doctype.Field.AllowBlocked bool`** (§3.1): DOC's master check ignores `MasterInfo.Blocked` for that field.

## 16. Open points for owner review

The owner has answered both batches (CD-234, CD-235). CD-236 still marks the Fallback Approver role as proposed, so §6 stands until the owner confirms it.

**My other assumptions, for review.**
- Claims aren't restricted by project or site.
- No limits, and no maximum age for an expense.
- A claim pays the exact amount, with no rounding.
- One employee per claim, and one advance per claim.
- Up to 100 receipts on a claim.
- Advances need no receipts, and use the same approval rule as claims.

## 17. Test plan

Fixtures: `00-fixtures.md`, `TRAVEL`, `FOOD`, `FUEL`, `LODGING`, `SITE-MISC`. Advance rule as in §4. EXP-016 and EXP-039 to EXP-041 run on the real `auth.Service`.

| ID | Behaviour | Go test | P | Level |
|---|---|---|---|---|
| EXP-001 | Blank employee defaults to the creator's; own-scope user naming another is refused, also when editing a draft an Administrator entered | `TestExp_EmployeeRules` | P1 | integration |
| EXP-002 | Administrator enters for another employee; claimant still can't approve it | `TestExp_AdminEntersForOther` | P1 | integration |
| EXP-003 | No linked employee refused; a leaver's pending claim approves and revises; an Administrator back-enters for a leaver | `TestExp_EmployeeAndLeavers` | P1 | integration |
| EXP-004 | Total, to pay and period computed from lines | `TestExp_Totals` | P1 | unit |
| EXP-005 | Date rules: future claim, future line, line after claim; past dates pass | `TestExp_DateRules` | P1 | unit |
| EXP-006 | No receipt and no reason refused at submit, passes on a draft | `TestExp_ReceiptOrReason` | P1 | integration |
| EXP-007 | Hash not on this claim refused with FILE-045's text; used receipt can't be removed (FILE-046), on draft and rejected; one bill on two lines | `TestExp_ReceiptLinks` | P1 | integration |
| EXP-008 | Amend keeps each line's receipt link after `CopyAll` | `TestExp_AmendKeepsReceipts` | P1 | integration |
| EXP-009 | `ListTx` takes no second connection (nestcheck gate) | `TestExp_ReceiptCheckSameTx` | P1 | integration |
| EXP-010 | 21 receipts accepted under the claim policy; 101 refused | `TestExp_ReceiptPolicy` | P1 | integration |
| EXP-011 | GST tick requires GSTIN and invoice no.; bad GSTIN uses MST-031's text | `TestExp_GSTFields` | P1 | integration |
| EXP-012 | GST above the amount refused | `TestExp_GSTAboveAmount` | P1 | unit |
| EXP-013 | Type defaults copied when blank; disabled tax code gives none | `TestExp_TypeDefaults` | P1 | integration |
| EXP-014 | Duplicate receipt and duplicate line warn, don't refuse; cancelled claims ignored | `TestExp_DuplicateWarnings` | P1 | integration |
| EXP-015 | Goes to the reporting manager; number on approve | `TestExp_ManagerApproves` | P1 | integration |
| EXP-016 | No manager falls back to Fallback Approver | `TestExp_Fallback` | P1 | integration |
| EXP-039 | Director's claim goes to the other Fallback Approver and not to the Director | `TestExp_DirectorClaim` | P1 | integration |
| EXP-040 | Claim from the only active Fallback Approver is refused with APR's text; the draft stays; a second holder lets it submit | `TestExp_OnlyFallback` | P1 | integration |
| EXP-041 | Second holder disabled while the Director's claim is pending: on Stuck approvals, moves when the role is given; another employee's claim is unaffected | `TestExp_FallbackLeaves` | P1 | integration |
| EXP-017 | Project-restricted Priya sees and approves Deepak's no-project claim, on the real `auth.Service` | `TestExp_RestrictedManagerReal` | P1 | integration |
| EXP-018 | Other teams' claims are 404; own scope on list and export | `TestExp_TeamScopeReal` | P1 | integration |
| EXP-019 | Reject needs a reason; revise and resubmit | `TestExp_RejectRevise` | P1 | integration |
| EXP-020 | Employee can't close their own claim: 403, on the real service and `authfake` | `TestExp_CloseRight` | P1 | integration |
| EXP-021 | Director closes; a closed claim can't be cancelled or amended | `TestExp_ClosedIsFinal` | P1 | integration |
| EXP-022 | `CloseRight` parity: every value on real service and fake | `TestDocType_CloseRightParity` | P1 | integration |
| EXP-023 | Lock off by default; on, it refuses submit, approve and cancel by date, for a claim and for an advance; close passes | `TestExp_PeriodLock` | P1 | integration |
| EXP-024 | Advance lifecycle: request, approve, number `EA/26-27/00001` | `TestAdv_Lifecycle` | P1 | integration |
| EXP-025 | Advance of ₹10,000, claim ₹6,000 sets ₹6,000: used 6,000, left 4,000, to pay 0 | `TestAdv_ClaimBelowAdvance` | P1 | integration |
| EXP-026 | Advance of ₹10,000, claim ₹12,000 sets ₹10,000: to pay ₹2,000 | `TestAdv_ClaimAboveAdvance` | P1 | integration |
| EXP-027 | Two claims approved together can't overspend one advance (real engine, non-admin) | `TestAdv_ConcurrentClaims` | P1 | integration |
| EXP-028 | Cancelling a claim restores what's left; used is recomputed | `TestAdv_CancelRestores` | P1 | integration |
| EXP-029 | Advance cancel blocked by a live claim | `TestAdv_CancelBlocked` | P1 | integration |
| EXP-030 | Closed advance can't be picked; another employee's is refused, with its text if readable and AUTH's if not | `TestAdv_Sources` | P1 | integration |
| EXP-031 | Advance rights match §11; close refused for the employee | `TestAdv_Rights` | P1 | integration |
| EXP-032 | Print goldens: claim with less advance and words on to-pay; advance | `TestExp_PrintGolden` | P1 | integration (golden file) |
| EXP-033 | Print needs `print`; a team member can't print a colleague's | `TestExp_PrintRights` | P1 | integration |
| EXP-034 | Open claims and open advances: columns, scope, `export` | `TestExp_OpenReports` | P1 | integration |
| EXP-035 | Expense lines report: every column read back | `TestExp_LinesReport` | P1 | integration |
| EXP-036 | Default rights match §11 | `TestExp_DefaultRights` | P1 | unit |
| EXP-037 | Random claim and advance sequences: total equals the lines, used equals the sum, left never below 0 | `FuzzExp_Invariants` | P1 | unit (fuzz) |
| EXP-038 | Phone flow with a photo at 360 px, with overflow check | `TestE2E_ExpenseFlow` | P1 | e2e |

Review focus: EXP-017 proves why claims declare no restriction fields. EXP-020 must fail if `CloseRight` is removed. EXP-027 runs as a non-admin (CD-213).

## 18. Work-package outline

MOD-EXP starts after MOD-0 and runs beside MOD-PO. `CloseRight` is in MOD-0.

| # | Task | Tier | Lane |
|---|---|---|---|
| 1 | Migrations, both DocTypes, rights | High | Serial first |
| 2 | Validate: employee, totals, dates, defaults, GST | High | A |
| 3 | Receipts: hash, `ListTx`, policy, amend | High | B |
| 4 | Advance settlement: checks, effects, concurrency | High | C |
| 5 | Lock and duplicate warnings | Standard | D |
| 6 | Prints and goldens | Standard | E |
| 7 | Reports and CSV | Standard | F |
| 8 | Wizards, line page, check page, claim policy and `RemoveGuard` wiring, after UX approval | Standard | G |
| 9 | End-to-end and overflow | Standard | Last |
