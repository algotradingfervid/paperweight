# Research: how ERPs build and test expense claims and employee advances (research-exp)

31 October 2026 · Researcher · For the director. Nothing here is decided, and no repo file was edited.

**Sources read (pinned):**
- Frappe HRMS `develop` @ `1d62bdbd` (28 Oct 2026), sparse clone at `scratchpad/research-src/hrms-exp/`. Paths below start at `hrms/`.
- ERPNext `develop` @ `a6f970eb` (31 Oct 2026), sparse clone at `scratchpad/research-src/erpnext-exp/`. Paths start at `erpnext/`.
- Odoo default branch `20.0` @ `1ab1ebbd` (31 Oct 2026), `addons/hr_expense` only, at `scratchpad/research-src/odoo-exp/`. Paths start at `hr_expense/`.
- Dolibarr `develop` @ `1f27c9d1`, three files fetched raw into `scratchpad/research-src/dolibarr-exp/`.
- CBIC `taxinformation.cbic.gov.in` HTML for CGST s.16, s.17 and Rules 36 and 46, fetched with curl into `scratchpad/research-src/cbic-exp/`. WebFetch failed on CBIC's TLS certificate, so curl was used.

Our side: Expense spec v0.5 (`docs/specs/2026-10-30-module-expense-spec.md`), `docs/tests/modules/exp.md` (EXP-001 to EXP-041), plan `docs/superpowers/plans/2026-10-31-mod-exp.md`, PO spec §1 to §3 and §14.2, `docs/tests/modules/po.md`, GRN spec §1 and §12.

**Caveat on Odoo.** In current Odoo the separate `hr.expense.sheet` model is gone: approval and state sit on `hr.expense` itself (`hr_expense/models/hr_expense.py:126-147`), and nothing in community `hr_expense` models employee advances (`grep -i advance models/*.py` finds nothing). Older articles about "expense sheets" describe earlier versions.

---

## 1. How each system models claims and advances

| Topic | Frappe HRMS | Odoo `hr_expense` (20.0) | Dolibarr `expensereport` | Paperweight spec v0.5 |
|---|---|---|---|---|
| **Claim document** | Expense Claim with an `expenses` table (claimed `amount` and `sanctioned_amount` per row), a `taxes` table and an `advances` table (`hr/doctype/expense_claim/expense_claim.py:546-560, 573-596, 598-610`) | One record per expense; there is no header sheet (`models/hr_expense.py:126-147`) | Report header with a date range and lines (`expensereport.class.php:1986` `addline`) | Claim header with up to 500 lines (§3) |
| **States** | `docstatus` plus `approval_status` (Draft, Approved, Rejected) and a payment status: Draft, Unpaid, Partially Paid, Paid, Rejected, Cancelled (`expense_claim.py:127-161`) | draft, submitted, approved, posted, in_payment, paid, refused (`hr_expense.py:126-139`) | Draft 0, Validated 2, Cancelled 4, Approved 5, Closed (paid) 6, Refused 99 (`expensereport.class.php:273-298`) | Kernel states; Close by Director or Admin through `CloseRight: "cancel"` (§6) |
| **Approver** | `expense_approver` on the employee (enabled users only), else department approvers up the department tree (`hr/doctype/department_approver/department_approver.py:32-92`). The claim is shared with the approver (`hr/utils.py:785-810`) | expense_manager_id, then the department manager, then the team leader (`hr_expense.py:1856-1871`). **With no approver the expense is approved automatically on submit** (`hr_expense.py:1395-1416`; test `tests/test_expenses_states.py:196-234`) | Users with the "approve" right; the UI shows approve only to `fk_user_validator` (`card.php:158`) | Reporting manager, else holders of the Fallback Approver role; never automatic (§6; D-APR-07 in `04-approvals.md:17`) |
| **Self-approval** | Blocked only when HR Settings `prevent_self_expense_approval` is on (`expense_claim.py:207-217`; tests `test_expense_claim.py:960-1030`) | Refused for team approvers ("It is your own expense"), **but HR admins skip the check** (`hr_expense.py:1685-1694`) | Not found in the files read | Always excluded: requester, creator and claimant (`approval.own`, §3.1) |
| **Approver changes amounts** | Yes: `sanctioned_amount` up to the claimed amount (`expense_claim.py:660-665`). Rejected rows are zeroed (`:553-554`) | Yes: approvers can edit a submitted expense (`hr_expense.py:390-445`) | Not checked | No. Reject with a reason (§2, CD-234) |
| **Claim for others** | Only roles with write outside the self-service roles (`expense_claim.py:163-196`; tests `test_expense_claim.py:875-958`) | Refused with AccessError for a plain user (`tests/test_expenses_access_rights.py:15-30`) | Only for users in your hierarchy, unless `writeall_advance` is set (`card.php:130, 279-281`) | Administrator only, for back-entry (§3.1) |
| **Inactive or left employee** | Blocks only status `Inactive`; `Left` isn't blocked (`hr/utils.py:813-822`) | Not checked in the files read | Not checked | `AllowBlocked`: leavers stay usable (§3.1) |
| **Receipts** | Plain attachments; no receipt check per line (none in `expense_claim.py`) | Attachments on the expense. **Can't be deleted after submit, and can't be added or changed once posted** (`models/ir_attachment.py:10-38`) | One `fk_ecm_files` per line; optional "file required" setting (`card.php:144, 1281-1285`) | SHA-256 of the receipt on the line; receipt or a reason required at submit (§3.3, §5). The kernel lets files be added after approval (D-FILE-06, `05-files-print.md:15`) |
| **Duplicates** | None found | Soft. Same file checksum on any expense, of any employee (`hr_expense.py:868-890`). Same employee, product, date and amount (`:892-916`). Approve opens a confirmation wizard when the duplicate is submitted or later (`:1418-1426`); automatic approval skips the check (`:1405`) | **Hard by default**: a second report whose period overlaps the same user's is refused unless `EXPENSEREPORT_ALLOW_OVERLAPPING_PERIODS` is set (`card.php:356-363`; `expensereport.class.php:2454-2473`) | Soft warnings on the check and approval pages, only against **another** live claim (§5) |
| **Line date** | Not checked against the claim | — | Line outside the report period warns; set to block with a setting (`card.php:1254-1266`) | Not in the future and not after the claim date (§5) |
| **Zero amount** | Advance amount must be > 0 (`employee_advance.py:116-118`) | Only drafts may total 0 (`hr_expense.py:333-356`; test `test_expenses.py:630-677`) | Unit price 0 refused (`card.php:1269-1273`) | Line amount > 0 (§3.2) |
| **Tax** | Header taxes table computed by rate on the sanctioned total (`expense_claim.py:573-596`) | Purchase taxes per expense, price includes tax (`test_expenses.py:37-60`) | VAT rate per line (`addline`) | Optional "bill in company GSTIN" tick with supplier GSTIN, invoice no., tax code, HSN/SAC and the bill's GST figure; no split (§3.2, §8) |
| **Advance document** | Employee Advance: Draft, Unpaid, Partially Paid, Paid, Claimed, Returned, Partly Claimed and Returned, Cancelled (`hr/doctype/employee_advance/employee_advance.py:142-180`) | None in community | None found | Employee Advance with lines; `used_amount` recomputed; Close with a note (§4) |
| **Settlement** | A claim's `advances` rows: `allocated_amount` ≤ unclaimed − returned (`expense_claim.py:612-633`); total allocated ≤ sanctioned + taxes (`:649-658`). **Only paid advances can be picked** (`paid_amount > 0`, `:849-854`) and allocation is capped at what was paid (test `test_employee_advance.py:295-337`) | — | — | One advance per claim, `less_advance` ≤ what's left, checked at submit and again at approve (§4) |
| **Claimed total on the advance** | **Recomputed** as the sum over approved, submitted claims (`employee_advance.py:254-272`), called on claim submit and cancel (`expense_claim.py:246, 267, 271-273`) | — | — | Recomputed over approved and closed claims (§4, D-DOC-13). Same pattern |
| **Unspent balance** | A real Return entry (`make_return_entry`, `employee_advance.py:323`) or a salary deduction (`create_return_through_additional_salary`, `:304`). Return can't exceed unclaimed (`:243-244`). Cancelling the return reopens the advance for claims (test `test_employee_advance.py:152-161`) | — | — | Close with a text note; no returned-amount figure; a closed advance never reopens (§4, R6) |
| **Period lock** | Accounting Period refuses closed DocTypes by posting date; one exempt role (`accounts/doctype/accounting_period/accounting_period.py:105-149`). HRMS registers only Payroll Entry as a period-closing DocType (`hooks.py:293`) | Through accounting lock dates on the journal entry (not read in detail) | — | `locked_up_to` by `claim_date` and `advance_date`; refuses submit, approve and cancel; Close passes (PO spec §14.2) |
| **Audit** | Expense Claim is in `audit_trail_doctypes` (`hooks.py:305`) | Mail thread and activities (`tests/test_expenses_access_rights.py:108-140`) | — | Kernel audit; `effect` entries on the advance (§4) |

**GST facts (from CBIC, checked against secondary sources where noted):**
- Credit needs a tax invoice (s.16(2)(a)). The supplier must have reported it in GSTR-1 and it must reach the recipient (s.16(2)(aa)), and Rule 36(4) ties this to GSTR-2B. Source: `cbic-exp/section16_v1.00.html`, `rule36_v1.00.html`. Secondary sources say the same (taxguru, busy.in).
- A registered recipient's invoice must carry the recipient's "name, address and GSTIN or UIN" (Rule 46(d)). Rule 36(2) accepts an incomplete invoice only if it still has the GSTIN "of the supplier and recipient". This is the legal basis for "bill in company GSTIN". **Rule 46(b) caps an invoice number at 16 characters, unique per financial year.** Source: `cbic-exp/r46.html`.
- Time bar: no credit after 30 November following the end of the FY, or the annual return if filed earlier (s.16(4)).
- Blocked credit (s.17(5)(b)): food and beverages, outdoor catering, health services, renting of motor vehicles (with exceptions), club and fitness memberships, and leave or home-travel benefits. Source: `cbic-exp/section17_v1.00.html`. The spec §8 already defers these to the CA.
- 180-day payment proviso: credit is reversed if the supplier isn't paid within 180 days (s.16(2), second proviso). It doesn't affect employee-paid bills, because the employee paid at purchase.
- Not re-verified: IGST s.12(3) and Circular 170/02/2022 on hotel place of supply, and Income-tax Act 2025 s.36(4) on the cash limit. These are spec §8's citations; I didn't check them again.

## 2. Testing practice worth borrowing

| # | Pattern | Where | What to borrow for EXP and cross-module |
|---|---|---|---|
| T1 | **Domain builders that stop at draft when asked.** `make_expense_claim(..., do_not_submit=False, approval_status="Approved")` returns a draft or submits it; `make_employee_advance(emp, args, do_not_submit)`; `get_advances_for_claim(claim, advance, amount)` attaches an advance | HRMS `test_expense_claim.py:1418-1472`; `test_employee_advance.py:570-603` | `expensetest` builders: `DraftClaim(p, lines...)`, `ApprovedClaim`, `ApprovedAdvance(emp, ₹)` and `Against(claim, adv, ₹)`, like `fixturetest.H.ApprovedOrder` (`internal/fixture/fixturetest/harness.go:158`). Tests then read as business steps |
| T2 | **Default-merge factory.** `create_expenses(values)` merges each dict over defaults (employee, frozen date, company, product, ₹1,000) | Odoo `tests/common.py:109-131` | Builders take only the fields a test cares about, so most cases stay short and fixtures stay consistent |
| T3 | **Assert the derived figure on the other document after every step, reversal included.** PO → receipt → cancel: `ordered_qty` and `received_qty` checked after each step (ERPNext `buying/doctype/purchase_order/test_purchase_order.py:95-122`). Advance → claim → cancel: advance `claimed_amount` and `status` checked after submit and after cancel (HRMS `test_employee_advance.py:73-101`; `test_expense_claim.py:1340-1385`). Project total goes up with two claims and down on cancel (`test_expense_claim.py:43-71`) | as cited | EXP-025 and EXP-028 do this for one claim. Extend to a sequence: advance → claim 1 → claim 2 → cancel 1 → close. PO and GRN: request ordered_qty → PO → GRN part → GRN cancel → short close |
| T4 | **One test walks the whole state path** and asserts every derived field after each step (`assertRecordValues` lists) | Odoo `tests/test_expenses_states.py:31-70`; Dolibarr chains tests with `@depends` (`ExpenseReportTest.php:57-236`) | A "happy path ledger" table test per DocType: each row is an action plus the expected state, number, version, used and left. Don't chain with shared state as Dolibarr does: one failure hides every later step |
| T5 | **Per-test settings that restore themselves** (`change_settings` context manager and decorator) | ERPNext `tests/utils.py:3400-3425`; used at HRMS `test_employee_advance.py:393-396` | Matches "each test grants Fallback Approver in its own setup". Use a helper that restores, so ordering bugs can't leak |
| T6 | **Rollback per test**; `assert_raises_with_savepoint` keeps the transaction usable after an expected refusal | ERPNext `tests/utils.py:3379-3380`; `tests/assertions.py:10-24` | After every refusal, assert "nothing written": version, state and used unchanged. exp.md already does this in places (EXP-006, EXP-027); make it a helper so every refusal case does it |
| T7 | **Permission helpers that check the exact refusal and no leakage.** `as_user` context manager; `assert_refused` compares to the constant message; `assert_refused_without(values)` checks that no hidden value appears in any message | ERPNext `tests/permission_test_utils.py:58-94` | A helper `wantRefusedWithout(t, err, hiddenNames...)` for CD-240 and CD-248: duplicate warnings, the advance page and the print never name an unreadable claim |
| T8 | **Several actors in one test**: the same claim refused as the claimant, then approved by the approver | HRMS `test_expense_claim.py:988-1030`; Odoo mixed selection where the error names only the offending record (`test_expenses.py:1308-1346`) | EXP-002 and EXP-039 do this already. Add a case where a manager's own claim and a report's claim sit in one inbox |
| T9 | **Frozen time** at class level plus `freeze_time` around time-sensitive steps | Odoo `tests/common.py:4, 101, 148`; `test_expenses.py:602` | We have a fixed clock (2026-10-24). Add a step clock for advance age in days (EXP-034) and any s.16(4) warning |
| T10 | **Browser tour, then a database assertion** | Odoo `tests/test_expenses_access_rights.py:52-72` (`start_tour` then `assertRecordValues`); `tests/test_expenses_tour.py:17-20` | After each chromedp flow, assert the stored outcome through the service (state, number, used), not only what the screen shows |
| T11 | **Awkward paise.** Advance 1,000 minus a claim of 619.34 leaves exactly 380.66 | HRMS `test_employee_advance.py:339-374`; rounding `test_expense_claim.py:773-796` | Seed EXP-037's fuzz with paise amounts and add one fixed case |
| T12 | **"Can't be zero" in every state** | Odoo `test_expenses.py:630-677` | Table test: amount 0 or negative refused on a line at save, for a claim and an advance |
| T13 | **Mapper tests: "make from" refuses an unsubmitted source** | ERPNext `test_purchase_order.py:87-93` | GRN-001 and PO-051 cover this for purchasing. For claims, the advance radios list only approved advances with something left (see S-17) |

## 3. Scenario catalogue

Format: **Given / When / Then**, then the source, then coverage. "Covered" means the named EXP case asserts it as written in `exp.md`. "Partial" means part of it is covered.

### Claim entry and validation
| ID | Scenario (Given / When / Then) | Source | Coverage |
|---|---|---|---|
| S-01 | Given an employee **When** they save a claim naming another employee **Then** refused; changing the employee on their own draft is refused too | HRMS `test_expense_claim.py:875-906`; Odoo `test_expenses_access_rights.py:15-30` | Covered: EXP-001 |
| S-02 | Given a role allowed to claim for others **When** it enters a claim for an employee **Then** it saves | HRMS `test_expense_claim.py:908-930` | Covered: EXP-002 (Administrator) |
| S-03 | Given a self-service user **When** they try to claim for another **Then** refused even with user permissions removed | HRMS `test_expense_claim.py:932-958` | Covered: EXP-001 |
| S-04 | Given a line of ₹0 or negative **When** saved or submitted **Then** refused; a draft may be incomplete but never ≤ 0 | Odoo `test_expenses.py:630-677`; HRMS `employee_advance.py:116-118`, test `test_employee_advance.py:497-504` | **GAP**: no EXP case asserts the amount > 0 rule (spec §3.2, §4) for a claim line or an advance line |
| S-05 | Given a line dated after the claim date or in the future **Then** refused | Dolibarr `card.php:1254-1266` (warns there, blocks by setting) | Covered: EXP-005 |
| S-06 | Given an employee's second claim whose line dates overlap an earlier claim **Then** Dolibarr refuses by default | Dolibarr `card.php:356-363` | Not adopted: the line-duplicate warning (EXP-014) is our answer. Note only |
| S-07 | Given an expense older than a year **Then** accepted (no maximum age) | Spec §16 | Owner accepted; see G8 for GST |
| S-08 | Given a Left employee's pending claim **Then** it approves and revises; back-entry for a leaver works | HRMS blocks only `Inactive` (`hr/utils.py:813-822`) | Covered: EXP-003 |
| S-09 | Given totals with paise (₹650.35 and ₹619.34) **Then** total, to pay and left are exact to the paisa | HRMS `test_employee_advance.py:339-374` | Partial: EXP-004 has paise; no fixed case for paise in left. Add to EXP-037 seeds |
| S-10 | Given a project tag at line level **When** the claim is approved, then cancelled **Then** the project's spend counts it, then drops it | HRMS `test_expense_claim.py:43-88` | **GAP**: EXP-035 reads columns and filters but doesn't assert that cancelled, rejected and draft lines are excluded from project spend, or which states count |

### Receipts and duplicates
| ID | Scenario | Source | Coverage |
|---|---|---|---|
| S-11 | Given no receipt and no reason **When** submitted **Then** refused; draft passes | Dolibarr `card.php:1281-1285` | Covered: EXP-006 |
| S-12 | Given a submitted claim **When** a receipt is removed **Then** refused | Odoo `models/ir_attachment.py:10-18`; FILE-039 | Covered: FILE-039 (kernel) and EXP-007 (draft and rejected) |
| S-13 | Given an approved claim **When** someone adds a new file **Then** Odoo refuses; our kernel allows it (D-FILE-06) | Odoo `ir_attachment.py:20-38` | Not a gap by decision; see G10 |
| S-14 | Given the same receipt file on two claims by the **same** employee **Then** soft warning; approve still possible | Odoo `hr_expense.py:868-890, 1418-1426` | Covered: EXP-014 |
| S-15 | Given the same receipt file on claims by **two different** employees (a shared taxi bill) **Then** warning on both | Odoo's checksum query isn't limited to one employee (`hr_expense.py:876-882`) | **GAP (test)**: EXP-014 uses one employee only. The spec text doesn't limit by employee, so add a cross-employee case, with the CD-240 "another claim" wording |
| S-16 | Given two identical lines (date, type, amount) on the **same** claim (a double tap on the phone) **Then** warn | Odoo's duplicate query groups every expense, siblings included (`hr_expense.py:897-916`) | **GAP (spec)**: §5 warns only against "another live claim". See G2 |
| S-17 | Given a company-GSTIN bill (supplier GSTIN + invoice no.) already on another live claim, by anyone **Then** warn | Rule 46(b): invoice no. is unique per supplier per FY | **GAP (spec)**: not among §5's warnings. See G3 |
| S-18 | Given one bill split over two lines (two projects) **Then** saves with "Same receipt as line {n}", and no duplicate warning between siblings | Odoo split wizard (`test_expenses.py:220-323`); same-receipt check skips split siblings (`hr_expense.py:872`) | Covered: EXP-007 (saves). Partial: "no sibling warning" isn't asserted |
| S-19 | Given a duplicate found while the expense would auto-approve **Then** Odoo skips the duplicate check | Odoo `hr_expense.py:1405` | N/A: we never auto-approve, except back-entry with no rule. Note: back-entry claims never see a warning on an approval page |

### GST
| ID | Scenario | Source | Coverage |
|---|---|---|---|
| S-20 | Given "bill in company GSTIN" ticked **Then** supplier GSTIN and invoice no. are required at submit, and a bad GSTIN is refused | Rule 46(a), (b), (d); Rule 36(2) | Covered: EXP-011 |
| S-21 | Given GST above the line amount **Then** refused | — | Covered: EXP-012 |
| S-22 | Given a company with more than one GSTIN (branches) **When** a hotel bills one of them **Then** the keying staff need to know which GSTIN is on the bill | Rule 46(d), recipient GSTIN; PO header has `branch_id` and `snap_branch_gstin` (PO spec §3.1) | **GAP (spec)**: no "billed to GSTIN or branch" on the line. See G4 |
| S-23 | Given a company bill dated in FY 2025-26 **When** it is claimed after 30 Nov 2026 **Then** its credit is time-barred (s.16(4)) | CGST s.16(4) | **GAP (owner)**: no warning. See G8 |
| S-24 | Given an invoice no. longer than 16 characters **Then** probably mistyped (Rule 46(b)) | Rule 46(b) | Not tested: field max is 40. Low; note only |
| S-25 | Given a food bill ticked as a company bill **Then** credit is likely blocked (s.17(5)(b)(i)); the app doesn't decide | CGST s.17(5) | By design (§8). Note |
| S-26 | Given a bill date different from the travel date **Then** GSTR-2B matching uses the invoice date | Rule 46(c) | **GAP (minor)**: `expense_date` stands in for the invoice date. See G9 |

### Approval
| ID | Scenario | Source | Coverage |
|---|---|---|---|
| S-27 | Given an employee with a manager **When** they submit **Then** the manager is the approver; number on approve | Odoo `hr_expense.py:1856-1871`; HRMS `department_approver.py:71-76` | Covered: EXP-015 |
| S-28 | Given no manager **Then** Odoo auto-approves; we go to the fallback role | Odoo `test_expenses_states.py:196-204` | Covered: EXP-016 (our rule is the opposite of Odoo's) |
| S-29 | Given an approver **When** they approve their own claim **Then** refused | Odoo `test_expenses_access_rights.py:74-84`; HRMS `test_expense_claim.py:988-1030` | Covered: EXP-002, EXP-039 |
| S-30 | Given a manager who files their own claim and also has a report's claim pending **Then** they can approve the report's but not their own; their own goes to their manager | Odoo `test_expenses.py:1308-1346` | Partial: EXP-039 covers the Director only. A middle manager (Priya → Ravi) isn't covered |
| S-31 | Given a team approver **When** they approve a claim of someone they don't manage **Then** refused (404 for us) | Odoo `test_expenses_access_rights.py:86-106` | Partial: EXP-018 (open is 404); EXP-031 (advance). No claim *approve* attempt by a non-manager |
| S-32 | Given a pending claim **When** the approver is changed **Then** the old approver loses access | HRMS `test_expense_claim.py:637-664`; `hr/utils.py:799-810` | **GAP**: Deepak's manager changes from Priya to Ravi while his claim is pending. Who may approve? Spec §6 covers the manager *leaving* (fallback still works) but has no test. See G6 |
| S-33 | Given the manager's user is disabled while a claim is pending **Then** a Fallback Approver can approve it | Spec §6 (last paragraph) | **GAP (test)**: EXP-041 disables the second fallback holder, not the reporting manager |
| S-34 | Given a rejected claim **Then** its amounts don't count, and revise and resubmit re-evaluate | HRMS `test_expense_claim.py:622-635`; D-APR-11 | Covered: EXP-019 |
| S-35 | Given a Director's **advance** **Then** it goes to the other Fallback Approver | Spec §4 rule = §6 | **GAP (test)**: EXP-039 to EXP-041 test claims only; the advance rule is separate data |

### Advances and settlement
| ID | Scenario | Source | Coverage |
|---|---|---|---|
| S-36 | Given an approved advance **When** a claim sets the full amount **Then** used equals the advance; it drops out of the picker; cancelling the claim restores it | HRMS `test_employee_advance.py:73-101` | Partial: EXP-025 and EXP-028 cover used and left. **The picker excluding a fully used advance isn't asserted.** See S-38 |
| S-37 | Given an advance of another employee **Then** refused | HRMS `test_expense_claim.py:215-241`; `expense_claim.py:619-620` | Covered: EXP-030 |
| S-38 | Given an advance with ₹0 left (fully used) **When** a claim names it **Then** not offered, and refused with the left text | HRMS `get_advances` status filter (`expense_claim.py:849-854`) | **GAP (test)** |
| S-39 | Given an advance in draft, pending, rejected or cancelled **When** a claim names it **Then** refused | HRMS needs `docstatus == 1` (`expense_claim.py:851`) | **GAP (test)**: EXP-030 covers closed and other-employee only |
| S-40 | Given an advance with ₹4,000 left **When** a single claim types ₹5,000 less advance **Then** refused at submit with the left text | HRMS `expense_claim.py:626-633` | Partial: the text is asserted only in EXP-027's concurrency case |
| S-41 | Given total allocated above the claim total **Then** refused | HRMS `expense_claim.py:649-658` | Covered: EXP-026 |
| S-42 | Given an advance approved but **never paid out** **When** a claim sets it off **Then** HRMS can't offer it (only paid advances); we would net it off and underpay the employee | HRMS `expense_claim.py:849-854`; test `test_employee_advance.py:295-337` (allocation capped at the ₹700 paid of ₹1,000) | **GAP (owner)**. See G1 |
| S-43 | Given a partly used advance **When** the balance is returned **Then** the advance is closed with the returned figure; return can't exceed the unclaimed amount | HRMS `test_employee_advance.py:103-161`; `employee_advance.py:243-244` | Partial: EXP-030 closes with a text note. No figure, so nothing can check it (CD-234) |
| S-44 | Given an advance closed by mistake **Then** HRMS can undo the return; we have no reopen | HRMS `test_employee_advance.py:152-161` | **GAP (owner)**. See G5 |
| S-45 | Given a claim **pending** on an advance **When** the advance is closed **Then** approve fails with DOC-017's text and the approver rejects | Spec §4 (Close row) | **GAP (test)**: described in the spec, not in exp.md |
| S-46 | Given a **draft** claim naming an advance **When** the advance is cancelled **Then** allowed (is a draft "live" for DOC-052?), and the draft's submit then fails | Spec §3 Links; DOC-052 | **GAP (test and clarification)** |
| S-47 | Given a claim with an advance that is rejected **Then** used is unchanged; revise and resubmit re-checks left | Spec §4 (pending doesn't count) | Partial: only the EXP-037 fuzz covers it implicitly |
| S-48 | Given an approved claim with an advance, cancelled then **amended** **Then** the amended draft keeps the advance and re-checks left at submit; if the advance was closed meanwhile, submit fails | Spec §4; HRMS cancel recomputes (`expense_claim.py:267`) | **GAP (test)**: EXP-008 amends a claim with no advance |
| S-49 | Given an employee holding an unsettled advance **When** they request another **Then** HRMS shows the pending total on the new advance | HRMS `employee_advance.py:274-290`; test `test_employee_advance.py:376-391` | **GAP (owner)**: no warning. See G7 |
| S-50 | Given a ₹0 advance line **Then** refused | HRMS `test_employee_advance.py:497-504` | **GAP (test)**: same as S-04 |
| S-51 | Given two claims approved together on one advance **Then** only one passes | — (ours) | Covered: EXP-027 |
| S-52 | Given an open advance **Then** the report shows its age in days | HRMS report `hr/report/unpaid_expense_claim/` (unpaid claims; ageing not read) | Covered: EXP-034 |

### Close, cancel, lock
| ID | Scenario | Source | Coverage |
|---|---|---|---|
| S-53 | Given an approved claim **When** the employee closes it **Then** refused | Spec §6 | Covered: EXP-020 |
| S-54 | Given a closed claim **Then** it can't be cancelled or amended | — | Covered: EXP-021 |
| S-55 | Given a period lock **Then** submit, approve and cancel are refused by document date; close passes | ERPNext `accounting_period.py:105-149` | Covered: EXP-023 |
| S-56 | Given a claim dated 5 Oct with lines from September (locked to 30 Sep) **Then** it submits, because the lock follows `claim_date` | PO spec §14.2 | Not tested; see G11 (low) |
| S-57 | Given a draft claim with attachments **When** deleted **Then** its attachments go too | Odoo `test_expenses.py:1182-1194`; FILE-042 | Covered: FILE-042 (kernel) |

That's 57 scenarios: 22 covered, 12 partly covered, 23 gaps or notes.

## 4. Cross-module end-to-end scenarios for `make e2e`

Today's e2e covers PO-045 (PO alone), PO-068 (request → PO → approve → sent) and EXP-038 (one claim). None crosses PO and GRN, and none runs advance → claim → close. ERPNext's chain tests (T3) and Odoo's tour-then-assert pattern (T10) suggest these:

| # | Flow (actors) | Steps and assertions | Why |
|---|---|---|---|
| X1 | **Request → PO → approval → partial GRN → short close** (suresh, priya, ravi, store user) | Suresh requests 10. Priya creates the PO; Ravi approves; it's marked sent. A GRN accepts 6 and the PO shows `part_received` with 4 pending. The PO is closed with a reason: pending shows 0, and a new GRN against it is refused (DOC-017). Suresh's request shows the ordered status. After the browser steps, assert `received_qty`, `receipt_status` and the request's ordered quantity through services | Covers what PO-033, PO-055 and GRN-011 assert separately, as ERPNext asserts `ordered_qty` after each step (`test_purchase_order.py:95-122`) |
| X2 | **GRN cancel restores pending** (inside X1 or separate) | Cancel the 6-unit GRN before the short close: PO pending goes back to 10, status `not_received` | The reversal step of T3 (ERPNext `pr.cancel()` at `test_purchase_order.py:118-122`) |
| X3 | **Advance → two claims → close** (deepak, priya, meena) at 360 px | Deepak requests ₹10,000 and Priya approves (`EA/26-27/00001`). Claim 1: ₹6,000 against it, approved; the advance page shows used 6,000 and left 4,000. Claim 2: ₹2,500 against it, approved; left 1,500. Meena closes the advance with "₹1,500 returned by NEFT …". Claim 3's radios no longer offer it. Assert the stored used amount and the audit `effect` entries | The HRMS advance lifecycle (`test_employee_advance.py:73-161`) as one flow. Also catches S-36 and S-38 in the UI |
| X4 | **Claim above the advance pays the balance** (deepak, priya) | Advance ₹10,000; claim ₹12,000; check page shows less advance ₹10,000 and to pay ₹2,000; print shows "Rupees Two Thousand Only" | EXP-026 and EXP-032 through the screens |
| X5 | **A buyer's own claim for a local cash buy** (priya buys site materials with cash; ravi approves) | Priya files a claim with a SITE-MISC line tagged PRJ-METRO and site, ticks "bill in company GSTIN" with supplier GSTIN and invoice no. Ravi (her manager) approves; Priya can't approve her own. The expense lines report shows the line under PRJ-METRO with GSTIN and invoice no.; it doesn't appear in PO pending or any GRN | GRN spec §1 and §12 send small local cash buys to expense claims (`2026-10-30-module-grn-spec.md:26, 240`). This proves the hand-off and S-30 |
| X6 | **Director's own claim through the fallback** (meena, kavya) | Meena files; only Kavya can approve; Meena sees no Approve button | EXP-039 in the browser: the most likely go-live surprise |
| X7 | **Reject → revise → resubmit with a receipt fixed** (deepak, priya) | Priya rejects "Line 2 has no receipt". Deepak revises, adds a photo to line 2 and resubmits; Priya approves | EXP-019 plus a receipt edit after rejection (FILE-039 allows removal in rejected) |

Every e2e step needs a short deadline (CLAUDE.md), and the flows assert stored state after the browser part (T10).

## 5. Gaps against our spec: risk and recommendation

Owner decisions are marked **[OWNER]**; AD or test-doc fixes are marked **[AD]**.

| # | Gap | Risk | Recommendation | Source |
|---|---|---|---|---|
| G1 | **An advance can be set off before it is paid out.** HRMS offers only paid advances and caps allocation at the amount paid. We track no payments (CD-234), so an approved-but-unpaid advance nets off a claim and the employee is underpaid | **Medium-High**: real money, and it's silent | **[OWNER]** Options: (a) a "Paid out on" date set by Director or Admin, and only paid advances offered; adds one field and one action. (b) Keep it as is, with a hint on the radios: "Only pick an advance you received". (c) Treat approval as payment and say so in the UX and the print. The trade-off is CD-234's no-payment rule against correctness | `expense_claim.py:849-854`; `test_employee_advance.py:295-337` |
| G2 | **Duplicate lines within one claim aren't warned** (a double tap or a re-upload on the phone) | **Medium**: the most common real duplicate | **[AD]** Extend §5's line-duplicate warning to the same claim, skipping lines that share one receipt hash on purpose (the split case) | Odoo `hr_expense.py:892-916, 872` |
| G3 | **The same company-GSTIN invoice on two claims** (two staff claim one hotel bill, or a re-shot photo with different bytes) isn't warned. The hash check misses a re-photographed bill | **Medium**: duplicate input credit keyed into accounts | **[AD]** Add a warning keyed on supplier GSTIN + invoice no. across live claims, under the CD-240 wording. Add a cross-employee receipt-hash case to EXP-014 | Rule 46(b); Odoo `hr_expense.py:876-882` |
| G4 | **Which company GSTIN the bill is in isn't captured.** The PO has `branch_id` and `snap_branch_gstin`; a claim line doesn't | **Medium** if the company has more than one GSTIN: credit goes to the GSTIN on the bill (Rule 46(d)), and place of supply depends on it (§8) | **[OWNER/CA]** Ask whether the company has more than one GSTIN. If yes, add "Billed to" (branch) on company-bill lines, defaulted from the site's branch; if no, record that one GSTIN is assumed | Rule 46(d), Rule 36(2); PO spec §3.1 |
| G5 | **A closed advance never reopens**, and nothing records the returned amount. HRMS can cancel a return and reopen | **Medium**: a mistaken Close strands the money left on the advance | **[OWNER]** Options: (a) accept, and document the fix: a new back-dated advance for the balance, as the cutover note does (§14). (b) Let Admin reverse a close; a kernel change against R6. (c) Store a "returned amount" number with the Close (≤ left), so the open-advances report and audit carry figures. Lowest cost: (a) plus a confirmation on Close that shows left | `test_employee_advance.py:103-161`; `employee_advance.py:243-244` |
| G6 | **Missing EXP tests for rules the spec states:** S-04 and S-50 (amount > 0); S-38 (fully used advance); S-39 (advance not approved); S-40 (left text for one claim); S-45 (close while a claim is pending); S-46 (draft claim against an advance that is cancelled); S-47 (rejected claim with advance); S-48 (amend with advance); S-33 (manager disabled while pending); S-35 (Director's advance); S-15 (cross-employee receipt); S-10 (which states count in reports); S-30 (middle manager's own claim) | **Medium**: each is a call site that could be deleted with no test failing (CLAUDE.md lesson) | **[AD]** Fold into existing cases where the setup matches (EXP-025/026/030 for the advance sources, EXP-008 for amend, EXP-016 for the manager disabled, EXP-035 for states), else add EXP-042 onward. S-32 (manager *changed* while pending) needs APR's answer first: designated approver at submit or at act time. `docs/tests/core/04-approvals.md:346` records "designated approver Priya" at submit. Couldn't verify how a later change is handled |
| G7 | **No warning when an employee with an unsettled advance asks for another** | **Low-Medium**: advances pile up | **[OWNER]** Soft warning on the advance check page ("You have ₹x left on EA/…") and on the approval page. No refusal | HRMS `employee_advance.py:274-290` |
| G8 | **s.16(4) time bar**: company bills from a past FY claimed after 30 November lose credit, and nothing flags them | **Low**: the CA decides credit; the expense lines CSV carries the date | **[OWNER]** Optional warning on company-bill lines dated in a FY whose 30 November has passed. Otherwise note it in the CA hand-off | CGST s.16(4) |
| G9 | **Invoice date** isn't separate from the expense date | **Low** | **[AD]** Label hint on company-bill lines: "Use the bill date". No new field | Rule 46(c) |
| G10 | **Files can be added to an approved claim** (kernel D-FILE-06); Odoo forbids it after posting | **Low**: line hashes are frozen after submit, so an added file can't become a receipt | Accept. Optionally note it in the UX | Odoo `ir_attachment.py:20-38`; `05-files-print.md:15` |
| G11 | **The lock follows `claim_date`, not line dates**: September expenses can go on an October claim after September is locked | **Low**: credit can still be taken until 30 Nov; this is normal practice | Note in the spec so nobody "fixes" it later | PO spec §14.2 |
| G12 | **Back-entered claims with no rule auto-approve and never show duplicate warnings on an approval page** (as Odoo's automatic approval skips its duplicate check) | **Low**: cutover only | **[AD]** Show the same warnings on the check page (the spec already does) and keep one test asserting they appear there for back-entry | Odoo `hr_expense.py:1405` |
| G13 | **No receipts in the print** (auditors often want the claim PDF with its bills) | **Low** | **[OWNER]** Out of scope unless asked; the approval page already shows receipts beside each line | Odoo attaches receipts to the journal entry (`test_expenses.py:473-575`) |

**Confirmed matches (no action):**
- The advance's used total is recomputed, never incremented, in HRMS too (`employee_advance.py:254-272`), which supports D-DOC-13.
- Duplicate checks are soft in Odoo (`hr_expense.py:1418-1426`), as the spec says.
- Reporting-manager rules are allowed for Employee Advance in the kernel: `approval/rules.go:55-58` accepts any DocType with `EmployeeField`, and the "only available for Expense Claim" text in APR-008 lists those DocTypes' labels (`internal/kernel/approval/errors.go:97-100`).

**Not verified:**
- How APR treats a reporting-manager change while a request is pending (S-32).
- Odoo's lock-date behaviour for expenses.
- The HRMS unpaid-claim report's ageing.
- IGST s.12(3), Circular 170/02/2022 and Income-tax Act 2025 s.36(4), which are spec §8's citations.
