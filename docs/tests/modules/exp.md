# Expense Claim and Employee Advance (EXP)

## Scope
The Expense module of `docs/specs/2026-10-30-module-expense-spec.md` v0.6: `expense.Claim` and `expense.Advance`, photo-first claims with one receipt per line, optional GST capture, one reporting-manager approval with the Fallback Approver role, advances set against claims, close, print and reports. Cases are spec §17 (EXP-001 to EXP-067), in one area, `EXP`. EXP-042 onward come from the ERP research catalogue `docs/research/2026-10-31-expense-erp-practice.md` (CD-271); each cites its scenario (S-) or gap (G-) and the ERP source. The plan is `docs/superpowers/plans/2026-10-31-mod-exp.md`.

Fixtures are in `docs/tests/core/00-fixtures.md`, including the `expense.Claim` and `expense.Advance` rights rows, the Fallback Approver role and the expense test seed. Expense types `TRAVEL`, `FOOD`, `FUEL`, `LODGING` and `SITE-MISC`. Clock 2026-10-24 10:30 IST, FY 2026-27. Both approval rules are seeded by the expense test environment: ₹0.00, reporting manager, fallback role Fallback Approver. No fixture user holds Fallback Approver; each test gives it (usually to Meena and Kavya) in its own setup. Meena also holds Employee in the expense seed (spec §11, §15 item 3).

Every advance a claim sets off in these cases is marked as paid on its approval date (CD-271 G1), unless the case says otherwise. The company fixture has two branch GSTINs (PUNE, BLR), so `Billed to` is shown and required on company bills unless a case disables BLR.

Test helpers follow the research report's §2 patterns (CD-271): business-step builders that can stop at draft (`DraftClaim`, `ApprovedClaim`, `ApprovedAdvance`, `PaidAdvance`), the advance's used and left asserted after every step including cancel, a refusal helper that also asserts nothing was written (state, version, used, audit count), and a stored-state check after each browser flow.

Money is shown as the app formats it (`₹4,000.00`). Messages are exact; texts owned by another package (AUTH, DOC, APR, FILE, MST, the period lock) are expected by that package's text (CD-142).

Review focus (spec §17): EXP-016, EXP-017, EXP-018 and EXP-039 to EXP-041 run on the real `auth.Service`. EXP-017 proves why claims declare no restriction fields. EXP-020 must fail if `CloseRight` is removed. EXP-027 runs as non-admin users (CD-213).

## Test cases

### EXP-001 · Blank employee defaults to the creator's; naming another employee is refused, also on a draft an Administrator entered
- **Priority:** P1
- **Level:** integration
- **Given** `deepak` (EMP-007) **When** he saves a claim with `employee_id` blank **Then** it is stored as EMP-007 with `snap_employee_code` `EMP-007` and `snap_employee_name` `Deepak Nair`. **When** he saves one naming EMP-006 (Kavya) **Then** it is refused with "You can only raise claims for yourself." and nothing is written. **Given** a draft `admin` (Administrator) entered for EMP-007 **When** `deepak` edits it and names EMP-006 **Then** the same refusal; keeping EMP-007 saves. The same rules hold for an advance, with "You can only raise advances for yourself."
- **Go test:** `TestExp_EmployeeRules`
- **Sources:** Expense spec §3.1, §4, §5, §17

### EXP-002 · Administrator enters a claim for another employee; the claimant still can't approve it
- **Priority:** P1
- **Level:** integration
- **Given** `admin` creates a claim for EMP-007 (Deepak) **When** it is submitted **Then** it is pending with `priya` (Deepak's manager) eligible. Neither `deepak` (the claimant) nor `admin` (the creator) is eligible to approve it, and an approve by either is refused with APR's not-eligible text.
- **Go test:** `TestExp_AdminEntersForOther`
- **Sources:** Expense spec §3.1, §6, §17

### EXP-003 · No linked employee is refused; a leaver's pending claim approves and revises; an Administrator back-enters for a leaver
- **Priority:** P1
- **Level:** integration
- **Given** a user with no linked employee **When** they save a claim **Then** it is refused with "Your user isn't linked to an employee. Ask your administrator." **Given** a pending claim for EMP-008 created while EMP-008 was Active, then EMP-008 set to Left **Then** its approver approves it (no MST-120 refusal, because `employee_id` sets `AllowBlocked`). A second such claim is rejected and revised to draft, and saves. **When** `admin` back-enters a claim for EMP-008 dated 2026-03-31 **Then** it saves and submits.
- **Go test:** `TestExp_EmployeeAndLeavers`
- **Sources:** Expense spec §3.1 (Leavers), §5, §15 item 7, §17; `06-masters.md` MST-120

### EXP-004 · Total, to pay and period are computed from the lines
- **Priority:** P1
- **Level:** unit
- **Given** lines of ₹1,250.00 (2026-10-20), ₹830.50 (2026-10-18) and ₹2,000.00 (2026-10-22), and less advance ₹1,000.00 **Then** `total` = ₹4,080.50, `net_payable` = ₹3,080.50, `period_from` = 2026-10-18 and `period_to` = 2026-10-22. With no advance, `net_payable` = `total`. An advance's `amount` is the sum of its lines.
- **Go test:** `TestExp_Totals`
- **Sources:** Expense spec §3.1, §4, §17

### EXP-005 · Date rules: future claim, future line, line after claim; past dates pass
- **Priority:** P1
- **Level:** unit
- **Given** the clock at 2026-10-24 **Then** claim date 2026-10-25 gives "Claim date can't be in the future."; a line dated 2026-10-25 gives "Line 1: Date can't be in the future."; a line dated 2026-10-23 on a claim dated 2026-10-22 gives "Line 1: Date can't be after the claim date."; a claim dated 2026-03-31 with a line of 2026-03-30 passes. An advance dated 2026-10-25 gives "Advance date can't be in the future."
- **Go test:** `TestExp_DateRules`
- **Sources:** Expense spec §3.1, §3.2, §5, §17

### EXP-006 · No receipt and no reason is refused at submit and passes on a draft
- **Priority:** P1
- **Level:** integration
- **Given** a claim whose line 1 has no `receipt_sha256` and no `no_receipt_reason` **When** it is saved as a draft **Then** it saves. **When** it is submitted **Then** it is refused with "Line 1: Add a receipt or say why there isn't one." on that line, with state and version unchanged. With reason `Paper file` it submits.
- **Go test:** `TestExp_ReceiptOrReason`
- **Sources:** Expense spec §3.2, §5, §14, §17; CD-178, CD-234

### EXP-007 · Receipt links: a hash not on this claim is refused; a used receipt can't be removed; one bill on two lines
- **Priority:** P1
- **Level:** integration
- **Given** claim A with `taxi.jpg` attached and claim B with `hotel.pdf` **When** A's line 2 is saved with `hotel.pdf`'s SHA-256 **Then** it is refused with FILE-045's text "The receipt on line 2 belongs to another document. Attach it to this expense first." **Given** A's line 2 holding `taxi.jpg`'s hash **When** `taxi.jpg` is removed, in draft and again in rejected **Then** it is refused with FILE-046's text "This file is the receipt on line 2. Clear it there first."; after the line's receipt is cleared, removal succeeds. **When** lines 1 and 2 hold the same hash (`Same receipt as line 1`) **Then** the claim saves.
- **Go test:** `TestExp_ReceiptLinks`
- **Sources:** Expense spec §3.3, §5, §17; `05-files-print.md` FILE-045, FILE-046

### EXP-008 · Amend keeps each line's receipt link after `CopyAll`
- **Priority:** P1
- **Level:** integration
- **Given** an approved claim with two receipts on two lines, cancelled with a reason **When** it is amended **Then** the new draft's attachments have new ids and the same SHA-256 (FILE-043), each line's `receipt_sha256` is unchanged, and the draft saves and submits with no receipt refusal.
- **Go test:** `TestExp_AmendKeepsReceipts`
- **Sources:** Expense spec §1 (approach A), §3.3, §17; `05-files-print.md` FILE-043

### EXP-009 · The receipt check reads in the caller's transaction (nestcheck gate)
- **Priority:** P1
- **Level:** integration
- **Given** a non-admin claimant and the nestcheck build (`-tags nestcheck`) **When** a claim with receipts is saved and submitted **Then** no second connection is opened inside the write: the receipt check uses `files.Store.ListTx`.
- **Go test:** `TestExp_ReceiptCheckSameTx`
- **Sources:** Expense spec §3.3, §17; CD-209, CD-213, CD-224

### EXP-010 · The claim policy accepts 21 receipts and refuses the 101st
- **Priority:** P1
- **Level:** integration
- **Given** the real `files.Store` with the `expense.Claim` policy (`MaxCount` 100) **When** 21 distinct files are attached to one draft claim **Then** all are accepted. **When** 100 are attached and a 101st is tried **Then** it is refused with "A document can have at most 100 attachments. Remove one first." Other DocTypes still refuse the 21st with "A document can have at most 20 attachments. Remove one first."
- **Go test:** `TestExp_ReceiptPolicy`
- **Sources:** Expense spec §3.3, §15 item 2, §16, §17

### EXP-011 · The GST tick requires GSTIN and invoice no.; a bad GSTIN uses MST-031's text
- **Priority:** P1
- **Level:** integration
- **Given** line 1 with `company_bill` ticked **When** the claim is submitted with no supplier GSTIN and no invoice no. **Then** it is refused with "Line 1: Supplier GSTIN is required for a bill in the company GSTIN." and "Line 1: Invoice no. is required for a bill in the company GSTIN." (both pass on a draft save). **When** the supplier GSTIN is `27AABCS1429B1Z5` **Then** the save is refused with "Line 1: GSTIN 27AABCS1429B1Z5 is not valid: the last character doesn't match. Check for typing mistakes." `27AABCS1429B1ZU` with invoice no. `INV-778` and Billed to PUNE submits.
- **Go test:** `TestExp_GSTFields`
- **Sources:** Expense spec §3.2, §5, §8, §17; `06-masters.md` MST-031; CD-38, CD-234

### EXP-012 · GST above the amount is refused
- **Priority:** P1
- **Level:** unit
- **Given** a line of ₹1,500.00 **Then** `gst_amount` ₹1,500.01 gives "Line 1: GST can't be more than the amount."; ₹1,500.00 and ₹160.71 pass.
- **Go test:** `TestExp_GSTAboveAmount`
- **Sources:** Expense spec §3.2, §5, §17

### EXP-013 · Type defaults are copied when blank; a disabled tax code gives none
- **Priority:** P1
- **Level:** integration
- **Given** expense type `LODGING` with default SAC `996311` and tax code GST18 (MST-123) **When** a line picks `LODGING` with `hsn_sac` and `tax_code_id` blank **Then** the saved line has `996311` and GST18. Values typed on the line are kept. **When** GST18 is disabled **Then** a new line gets no tax code (CD-216 item 4).
- **Go test:** `TestExp_TypeDefaults`
- **Sources:** Expense spec §3.2, §9, §17; `06-masters.md` MST-123

### EXP-014 · Duplicate receipt and duplicate line warn, never refuse; cancelled claims are ignored
- **Priority:** P1
- **Level:** integration
- **Given** Deepak's approved `EX/26-27/00001` with a line on 2026-10-20, `TRAVEL`, ₹1,250.00 and receipt hash H **When** his new claim has line 1 with the same hash and line 2 with the same date, type and amount **Then** the check page and the approval page warn "Line 1: This receipt is also on EX/26-27/00001." and "Line 2: This looks the same as a line on EX/26-27/00001.", and submit still succeeds. **When** `EX/26-27/00001` is cancelled **Then** neither warning appears. **When** the other claim is one the viewer can't read **Then** the warning doesn't name it ("another claim", proposed CD; CD-240, CD-248), on the real `auth.Service`. **Given** Suresh's live claim holding the same receipt bytes as Deepak's line 3 (a shared taxi bill) **Then** Deepak's check page warns "Line 3: This receipt is also on another claim." (he can't read Suresh's), and Meena (Director) sees Suresh's number (S-15, Odoo `hr_expense.py:876-882`).
- **Go test:** `TestExp_DuplicateWarnings`
- **Sources:** Expense spec §5 (Warnings), §17

### EXP-015 · A claim goes to the reporting manager and is numbered on approve
- **Priority:** P1
- **Level:** integration
- **Given** `deepak` (reports to EMP-004) **When** he submits a claim **Then** it is pending, `priya` is the eligible approver, and it has no number. **When** `priya` approves **Then** it is approved with number `EX/26-27/00001`.
- **Go test:** `TestExp_ManagerApproves`
- **Sources:** Expense spec §6, §7, §17

### EXP-016 · No manager falls back to the Fallback Approver role
- **Priority:** P1
- **Level:** integration
- **Given** Fallback Approver given to `meena` and `kavya`, and Deepak's reporting manager cleared **When** he submits **Then** the eligible approvers are `meena` and `kavya`, and `priya` is not. The same when his manager's employee has no linked user. On the real `auth.Service`.
- **Go test:** `TestExp_Fallback`
- **Sources:** Expense spec §6, §17; CD-236, CD-238

### EXP-017 · Project-restricted Priya sees and approves Deepak's claim with no project, on the real `auth.Service`
- **Priority:** P1
- **Level:** integration
- **Given** `priya` restricted to PRJ-METRO and Deepak's claim with no project on any line **When** she lists claims, opens it and approves it **Then** it is listed, opens (200) and approves. A line tagged PRJ-TECHPARK doesn't hide it either.
- **Go test:** `TestExp_RestrictedManagerReal`
- **Sources:** Expense spec §3 (no ProjectFields), §17; `01-auth.md` AUTH-093

### EXP-018 · Other teams' claims are 404; own and team scope on the list
- **Priority:** P1
- **Level:** integration
- **Given** claims by `deepak` and by `kavya` **Then** `deepak`'s list shows only his; `priya`'s shows hers and Deepak's (team); `kavya` opening Deepak's claim gets 404 with "This record doesn't exist or you don't have access to it." (CD-245); `meena` (Director) lists all and exports the CSV; `deepak` asking for the CSV gets AUTH's 403 for `export`. On the real `auth.Service`.
- **Go test:** `TestExp_TeamScopeReal`
- **Sources:** Expense spec §11, §13, §17; CD-11, CD-245

### EXP-019 · Reject needs a reason; revise and resubmit
- **Priority:** P1
- **Level:** integration
- **Given** Deepak's pending claim **When** `priya` rejects with a blank reason **Then** it is refused with APR's reason-required text. **When** she rejects with "Line 2 is a personal meal" **Then** it is rejected with that reason. **When** `deepak` revises **Then** it is a draft again; he edits line 2, submits, and it is pending with `priya`.
- **Go test:** `TestExp_RejectRevise`
- **Sources:** Expense spec §6, §17; D-APR-11

### EXP-020 · An employee can't close their own claim
- **Priority:** P1
- **Level:** integration
- **Given** Deepak's approved claim **When** `deepak` closes it **Then** it is refused with AUTH's 403 for `cancel` on Expense Claim ("You don't have permission to cancel Expense Claim. Ask your administrator for a role that allows it.") and nothing changes. Asserted on the real `auth.Service` and on `authfake`.
- **Go test:** `TestExp_CloseRight`
- **Sources:** Expense spec §6, §15 item 1, §17; CD-235, CD-255

### EXP-021 · The Director closes; a closed claim can't be cancelled or amended
- **Priority:** P1
- **Level:** integration
- **Given** Deepak's approved claim **When** `meena` closes it with "Paid by NEFT 1234 on 25-10-2026" **Then** it is closed with that reason. **When** anyone cancels or amends it **Then** it is refused with DOC's closed text and nothing changes (R6).
- **Go test:** `TestExp_ClosedIsFinal`
- **Sources:** Expense spec §6, §17; CD-235

### EXP-022 · CloseRight parity: the close right on the real service and the fake
- **Priority:** P1
- **Level:** integration
- **Given** `expense.Claim` and `expense.Advance` with `CloseRight` `cancel` **Then** for every canonical role, Fallback Approver included, and for both DocTypes, the real `auth.Service` and `authfake` agree on the close right, and the right the engine checks for close is `cancel`.
- **Go test:** `TestDocType_CloseRightParity`
- **Sources:** Expense spec §6, §15 item 1, §17; CD-255

### EXP-023 · Period lock: off by default; on, it refuses submit, approve and cancel by date, for a claim and an advance; close passes
- **Priority:** P1
- **Level:** integration
- **Given** no lock **Then** claims and advances dated 2026-03-31 submit, approve and cancel. **Given** the lock set to 2026-09-30 by `admin` **When** a claim dated 2026-09-15 is submitted, a pending one approved, or an approved one cancelled **Then** each is refused with the lock's text and nothing changes; the same for an advance dated 2026-09-15. An approved claim dated 2026-09-15 still closes. A claim dated 2026-10-01 submits.
- **Go test:** `TestExp_PeriodLock`
- **Sources:** Expense spec §6, §17; PO spec §14.2; CD-234

### EXP-024 · Advance lifecycle: request, approve, number
- **Priority:** P1
- **Level:** integration
- **Given** `deepak` **When** he requests an advance of ₹10,000.00 (one `TRAVEL` line, purpose "Pune Metro site visit") and submits **Then** it is pending with `priya`. **When** she approves **Then** it is approved with number `EA/26-27/00001`, `amount` ₹10,000.00 and `used_amount` ₹0.00.
- **Go test:** `TestAdv_Lifecycle`
- **Sources:** Expense spec §4, §7, §17

### EXP-025 · An advance of ₹10,000 against a claim of ₹6,000
- **Priority:** P1
- **Level:** integration
- **Given** EXP-024's approved advance **When** Deepak's claim totalling ₹6,000.00 names it with `less_advance` blank **Then** the draft shows less advance ₹6,000.00 and to pay ₹0.00 (stored blank, computed), and at submit ₹6,000.00 is written to `less_advance`. **When** the claim is approved **Then** the advance's used is ₹6,000.00 and left ₹4,000.00, its version went up by 1, and an `effect` audit entry by the claim's `updated_by` names the claim with action `approve`.
- **Go test:** `TestAdv_ClaimBelowAdvance`
- **Sources:** Expense spec §3.1, §4 (Settlement), §17

### EXP-026 · An advance of ₹10,000 against a claim of ₹12,000
- **Priority:** P1
- **Level:** integration
- **Given** EXP-024's approved advance **When** a claim totalling ₹12,000.00 names it with `less_advance` blank **Then** less advance defaults to ₹10,000.00 and to pay is ₹2,000.00. After approve, used is ₹10,000.00 and left ₹0.00. A typed `less_advance` of ₹12,000.01 saves as a draft and is refused at submit with "Less advance can't be more than the total." (proposed decision: a submit check, because the advance is picked before any line exists).
- **Go test:** `TestAdv_ClaimAboveAdvance`
- **Sources:** Expense spec §4 (Settlement), §5, §17

### EXP-027 · Two claims approved together can't overspend one advance
- **Priority:** P1
- **Level:** integration
- **Given** EXP-024's advance, and two pending claims by Deepak each setting ₹6,000.00 against it, with Fallback Approver given to `meena` **When** `priya` and `meena` (both non-admin) approve one each at the same time, on two connections **Then** exactly one is approved; the other is refused with "Less advance can't be more than the ₹4,000.00 left on EA/26-27/00001." and stays pending; used is ₹6,000.00.
- **Go test:** `TestAdv_ConcurrentClaims`
- **Sources:** Expense spec §4 (Settlement), §17; CD-94, CD-213

### EXP-028 · Cancelling a claim restores what's left; used is recomputed
- **Priority:** P1
- **Level:** integration
- **Given** EXP-025's approved claim (used ₹6,000.00) **When** `meena` cancels it with a reason **Then** used is ₹0.00 and left ₹10,000.00, recomputed from the remaining approved and closed claims, and an `effect` audit entry names the claim with action `cancel`.
- **Go test:** `TestAdv_CancelRestores`
- **Sources:** Expense spec §4 (Settlement), §17; D-DOC-13

### EXP-029 · An advance's cancel is blocked by a live claim
- **Priority:** P1
- **Level:** integration
- **Given** EXP-025's approved claim naming `EA/26-27/00001` **When** `meena` cancels the advance **Then** it is refused with DOC-052's text naming the claim, and the advance stays approved. After the claim is cancelled, the cancel is refused with "This advance is marked as paid. Undo paid first, or close it with the balance noted."; after `meena` undoes paid, the advance cancels.
- **Go test:** `TestAdv_CancelBlocked`
- **Sources:** Expense spec §3 (Links), §4, §17; `03-document.md` DOC-052

### EXP-030 · Advance sources: closed can't be picked; another employee's is refused
- **Priority:** P1
- **Level:** integration
- **Given** `EA/26-27/00001` closed by `meena` with "₹2,000 returned by NEFT 1234 on 30-11-2026" **When** a claim names it **Then** it is refused with DOC's closed-source text for the advance (DOC-017). **Given** Deepak's approved advance **When** `priya` names it on her own claim (she can read it: team) **Then** "EA/26-27/00001 belongs to another employee."; **when** `kavya` names it (she can't read it) **Then** AUTH's link refusal "You can't use this Employee Advance. It isn't available to you." (CD-240). A `less_advance` with no advance gives "Choose the advance to set this against."; an advance with `less_advance` 0 is refused at submit with "Enter the amount to set against EA/26-27/00001, or remove the advance."
- **Go test:** `TestAdv_Sources`
- **Sources:** Expense spec §3.1, §4, §5, §17; `03-document.md` DOC-017; CD-240

### EXP-031 · Advance rights match §11; close is refused for the employee
- **Priority:** P1
- **Level:** integration
- **Given** Deepak's approved advance **When** `deepak` closes it **Then** AUTH's 403 for `cancel` on Employee Advance. `meena` closes it. `priya` reads and approves her team's advances; `kavya`, without Fallback Approver, gets 404 on Deepak's. Each §11 right is exercised once for Employee, Director and Fallback Approver.
- **Go test:** `TestAdv_Rights`
- **Sources:** Expense spec §4, §11, §17

### EXP-032 · Print goldens: a claim with less advance and words on the amount to pay; an advance
- **Priority:** P1
- **Level:** integration (golden file)
- **Given** EXP-026's approved claim (total ₹12,000.00, less advance ₹10,000.00 against `EA/26-27/00001`, to pay ₹2,000.00, purpose "Pune Metro site visit", a line tagged PRJ-METRO) and EXP-024's approved advance **When** rendered **Then** the claim print shows the purpose, less advance, the advance number, to pay, each line's project and receipt marker, and "Rupees Two Thousand Only" in words; the advance print shows "Employee Advance", the employee, date, purpose, lines, total, words and approved by. Both match their golden files.
- **Go test:** `TestExp_PrintGolden`
- **Sources:** Expense spec §10, §17; `05-files-print.md` FILE-066, FILE-068

### EXP-033 · Print needs `print`; a team member can't print a colleague's claim
- **Priority:** P1
- **Level:** integration
- **Given** Priya's approved claim **Then** `priya` prints it (own) and `meena` prints it (all). `suresh`, a colleague who also reports to Ravi, gets the same 404 as a missing record (he can't read it). `ravi`, her manager (read on team, `print` own only), and `kavya` holding Fallback Approver (read, no `print`) get AUTH's 403 for `print`.
- **Go test:** `TestExp_PrintRights`
- **Sources:** Expense spec §10, §11, §17; `05-files-print.md` FILE-050

### EXP-034 · Open claims and open advances reports: columns, scope, `export`
- **Priority:** P1
- **Level:** integration
- **Given** approved and closed claims and advances for Deepak and Kavya **Then** `/reports/expense/open` lists approved, not closed claims with the columns claim, employee, purpose, approved on, approved by, total, less advance, to pay, lines without a receipt; `/reports/expense/advances` lists approved, not closed advances with advance, employee, date, purpose, amount, paid on (blank when unpaid), used, left, age in days, each asserted by value, and its employee filter. `priya` sees only her team's rows, `meena` all. The CSV needs `export` (AUTH's 403 otherwise).
- **Go test:** `TestExp_OpenReports`
- **Sources:** Expense spec §13, §17

### EXP-035 · Expense lines report: every column read back
- **Priority:** P1
- **Level:** integration
- **Given** claims whose lines set every field **When** `/reports/expense/lines` and its CSV are read **Then** each row has claim, employee, date, type, project, site, note, amount, GST, supplier GSTIN, invoice no., billed-to GSTIN, receipt yes or no and state, each asserted by value; the month, type, project and employee filters each narrow the rows.
- **Go test:** `TestExp_LinesReport`
- **Sources:** Expense spec §13, §17

### EXP-036 · Default rights match §11
- **Priority:** P1
- **Level:** unit
- **Then** the `DefaultRights` of `expense.Claim` and `expense.Advance` equal the `00-fixtures.md` rows for every role: Employee, Director, Fallback Approver and Administrator.
- **Go test:** `TestExp_DefaultRights`
- **Sources:** Expense spec §11, §15 item 3, §17

### EXP-037 · Random claim and advance sequences keep the invariants
- **Priority:** P1
- **Level:** unit (fuzz)
- **Given** random sequences of save, submit, approve, reject, revise, cancel, close, amend, mark as paid and undo paid over claims and advances, with paise amounts in the seeds, where each refusal (including the paid-cancel and not-paid refusals) is an expected outcome that changes nothing **Then** after each step: a claim's total equals the sum of its lines; an advance's used equals the sum of `less_advance` over its approved and closed claims; left (`amount − used`) is never below 0.
- **Go test:** `FuzzExp_Invariants`
- **Sources:** Expense spec §4, §17; lessons §8, §10

### EXP-038 · Phone flow with a photo at 360 px, with overflow check
- **Priority:** P1
- **Level:** e2e
- **Given** a 360 px browser **When** `deepak` starts a claim, adds one expense with a photo, submits, and `priya` approves it **Then** each screen fits without horizontal overflow, the expense with its photo takes no more taps than the UX design's count, and the claim ends approved with `EX/26-27/00001`.
- **Go test:** `TestE2E_ExpenseFlow`
- **Sources:** Expense spec §12, §17; `docs/ux/2026-10-31-expense.md`

### EXP-039 · The Director's claim goes to the other Fallback Approver, not to the Director
- **Priority:** P1
- **Level:** integration
- **Given** Fallback Approver given to `meena` and `kavya` **When** `meena` (Director, no manager) submits a claim **Then** it is pending with `kavya` as the only eligible approver; `meena` can't approve her own claim. On the real `auth.Service`.
- **Go test:** `TestExp_DirectorClaim`
- **Sources:** Expense spec §6, §17; CD-236, CD-238

### EXP-040 · A claim from the only active Fallback Approver is refused; a second holder lets it submit
- **Priority:** P1
- **Level:** integration
- **Given** Fallback Approver given only to `meena` **When** she submits her claim **Then** it is refused with "No one else can approve this Expense Claim. Give the Fallback Approver role to another active user, then submit again." and the draft is unchanged. **When** `kavya` is given the role **Then** the same draft submits. On the real `auth.Service`.
- **Go test:** `TestExp_OnlyFallback`
- **Sources:** Expense spec §6, §17; D-APR-08

### EXP-041 · The second holder is disabled while the Director's claim is pending
- **Priority:** P1
- **Level:** integration
- **Given** Meena's claim pending with `kavya`, and Deepak's pending with `priya` **When** `kavya` is disabled **Then** Meena's claim is on Stuck approvals (APR-043) and Deepak's is not. **When** `ravi` is given Fallback Approver **Then** Meena's claim leaves Stuck approvals and `ravi` approves it. On the real `auth.Service`.
- **Go test:** `TestExp_FallbackLeaves`
- **Sources:** Expense spec §6, §17; `04-approvals.md` APR-043; D-APR-18

### EXP-042 · A line amount of 0 or less is refused, on a claim and an advance
- **Priority:** P1
- **Level:** integration
- **Given** a claim line and an advance line **When** saved (draft) with amount ₹0.00 and again with -₹1.00 **Then** each save is refused with DOC's "Line 1: Amount must be more than 0." and nothing is written; ₹0.01 saves.
- **Go test:** `TestExp_AmountPositive`
- **Sources:** Expense spec §3.2, §4, §5; research S-04, S-50, T12 (Odoo `test_expenses.py:630-677`; HRMS `employee_advance.py:116-118`, `test_employee_advance.py:497-504`)

### EXP-043 · A fully used advance isn't offered and is refused at submit
- **Priority:** P1
- **Level:** integration
- **Given** Deepak's paid advance of ₹10,000.00 with ₹10,000.00 used by an approved claim **When** he opens a new claim's details step **Then** the advance isn't among the radios. **When** a claim names it anyway with less advance ₹100.00 and is submitted **Then** it is refused with "Less advance can't be more than the ₹0.00 left on EA/26-27/00001." and nothing is written.
- **Go test:** `TestAdv_FullyUsedNotOffered`
- **Sources:** Expense spec §4, §5; research S-38 (HRMS `expense_claim.py:849-854`)

### EXP-044 · An advance that isn't approved is refused
- **Priority:** P1
- **Level:** integration
- **Given** Deepak's advances in draft, pending, rejected and cancelled **When** a claim names each one **Then** it is refused with DOC's source text for that state (e.g. "EA/26-27/00002 is waiting for approval. Pick an approved employee advance."; a draft or rejected one by its `Draft {id}` label), and none is offered on the details step.
- **Go test:** `TestAdv_NotApprovedRefused`
- **Sources:** Expense spec §4; research S-39 (HRMS `expense_claim.py:851`); `03-document.md` DOC-017 family

### EXP-045 · One claim setting more than what's left is refused at submit
- **Priority:** P1
- **Level:** integration
- **Given** Deepak's paid advance of ₹10,000.00 with ₹6,000.00 used **When** a claim totalling ₹8,000.00 types less advance ₹5,000.00 and is submitted **Then** it is refused with "Less advance can't be more than the ₹4,000.00 left on EA/26-27/00001.", with state, version and used unchanged; ₹4,000.00 submits.
- **Go test:** `TestAdv_LeftForOneClaim`
- **Sources:** Expense spec §4, §5; research S-40 (HRMS `expense_claim.py:626-633`)

### EXP-046 · An advance closed while a claim on it is pending
- **Priority:** P1
- **Level:** integration
- **Given** Deepak's claim pending with ₹3,000.00 against `EA/26-27/00001` **When** `meena` closes the advance and `priya` then approves the claim **Then** the approve is refused with DOC's closed-source text for the advance (DOC-017) and the claim stays pending; `priya` rejects it with a reason; used stays as it was.
- **Go test:** `TestAdv_ClosedWhileClaimPending`
- **Sources:** Expense spec §4 (Close row); research S-45

### EXP-047 · A draft claim doesn't block the advance's cancel
- **Priority:** P1
- **Level:** integration
- **Given** Deepak's approved advance `EA/26-27/00001` not marked as paid, and a draft claim naming it **When** `meena` cancels the advance with a reason **Then** the cancel succeeds (DOC-052 counts pending, approved and closed documents only). **When** the draft is then submitted **Then** it is refused with DOC's cancelled-source text for the advance, and the draft is unchanged.
- **Go test:** `TestAdv_DraftClaimDoesNotBlockCancel`
- **Sources:** Expense spec §3 (Links), §4; research S-46; `03-document.md` DOC-052

### EXP-048 · A rejected claim on an advance leaves used unchanged; resubmit re-checks
- **Priority:** P1
- **Level:** integration
- **Given** Deepak's claim pending with ₹6,000.00 against a ₹10,000.00 advance **When** `priya` rejects it **Then** used is ₹0.00. **Given** a second claim then approved for ₹6,000.00 **When** the first is revised and resubmitted unchanged **Then** it is refused with "Less advance can't be more than the ₹4,000.00 left on EA/26-27/00001."
- **Go test:** `TestAdv_RejectedClaimRechecks`
- **Sources:** Expense spec §4; research S-47, T3

### EXP-049 · Amend keeps the advance and re-checks it
- **Priority:** P1
- **Level:** integration
- **Given** an approved claim with ₹6,000.00 against `EA/26-27/00001`, then cancelled (used back to ₹0.00) **When** it is amended **Then** the new draft names the same advance and less advance, and submits. **Given** the advance closed before the amended draft is submitted **Then** submit is refused with DOC's closed-source text.
- **Go test:** `TestAdv_AmendKeepsAdvance`
- **Sources:** Expense spec §4; research S-48 (HRMS `expense_claim.py:267`)

### EXP-050 · The manager's user is disabled while a claim is pending
- **Priority:** P1
- **Level:** integration
- **Given** Deepak's claim pending with `priya`, and Fallback Approver given to `meena` and `kavya` **When** `priya`'s user is disabled **Then** `meena` approves it. Before the disable, both `priya` and the Fallback Approver holders were eligible. On the real `auth.Service`.
- **Go test:** `TestExp_ManagerDisabledFallback`
- **Sources:** Expense spec §6; research S-33; D-APR-18

### EXP-051 · The Director's advance goes to the other Fallback Approver
- **Priority:** P1
- **Level:** integration
- **Given** Fallback Approver given to `meena` and `kavya` **When** `meena` submits an advance **Then** it is pending with `kavya` as the only eligible approver; with only `meena` holding the role, submit is refused with "No one else can approve this Employee Advance. Give the Fallback Approver role to another active user, then submit again." On the real `auth.Service`.
- **Go test:** `TestAdv_DirectorAdvance`
- **Sources:** Expense spec §4 (Rule), §6; research S-35

### EXP-052 · The expense lines report counts approved and closed claims only
- **Priority:** P1
- **Level:** integration
- **Given** claims tagged PRJ-METRO in draft, pending, rejected, approved, closed and cancelled **Then** `/reports/expense/lines` filtered to PRJ-METRO lists only the approved and closed claims' lines, and their total. **When** the approved claim is cancelled **Then** its lines leave the report.
- **Go test:** `TestExp_LinesReportStates`
- **Sources:** Expense spec §13; research S-10, T3 (HRMS `test_expense_claim.py:43-88`)

### EXP-053 · A middle manager approves a report's claim, not her own
- **Priority:** P1
- **Level:** integration
- **Given** Deepak's claim and Priya's own claim, both pending **Then** `priya` approves Deepak's; her own isn't hers to approve (APR's not-eligible text) and `ravi`, her manager, approves it. Her approvals inbox lists Deepak's only.
- **Go test:** `TestExp_MiddleManagerOwnClaim`
- **Sources:** Expense spec §6; research S-30, T8 (Odoo `test_expenses.py:1308-1346`)

### EXP-054 · Mark as paid: rights, states, dates, undo
- **Priority:** P1
- **Level:** integration
- **Given** Deepak's approved advance dated 2026-10-20 **Then**:
  - `deepak` and `priya` posting Mark as paid get AUTH's 403 for `cancel`; `meena` sets `paid_on` 2026-10-21; version goes up by 1 and an audit entry with action `paid` is written;
  - on a pending advance: "Only an approved advance can be marked as paid.";
  - 2026-10-25: "Paid on can't be in the future."; 2026-10-19: "Paid on can't be before the advance date.";
  - a second Mark as paid: "This advance is already marked as paid.";
  - a stale `version`: DOC's stale text with 409;
  - Undo paid clears `paid_on` (audit `unpaid`) while only draft or rejected claims name the advance; with a pending, approved or closed claim on it: "A claim already uses this advance, so it stays paid."; on an advance not marked as paid: "This advance isn't marked as paid.";
  - Mark as paid on a closed or cancelled advance: "Only an approved advance can be marked as paid."; `kavya`, who can't read the advance, gets the same 404 as a missing one (CD-245);
  - cancelling the paid advance: "This advance is marked as paid. Undo paid first, or close it with the balance noted."; after Undo paid it cancels.
  Every refusal leaves state, version and audit count unchanged.
- **Go test:** `TestAdv_MarkAsPaid`
- **Sources:** Expense spec §4 (Mark as paid); research G1, S-42 (HRMS `expense_claim.py:849-854`, `test_employee_advance.py:295-337`); CD-271

### EXP-055 · Only paid advances can be set off
- **Priority:** P1
- **Level:** integration
- **Given** Deepak's approved advance not marked as paid **Then** it isn't offered on the details step. **When** a claim names it and is submitted **Then** it is refused with "EA/26-27/00001 hasn't been marked as paid yet." and the draft is unchanged. **When** `meena` marks it as paid **Then** it is offered, the same draft submits, and `priya` approves it (approve re-checks the paid mark, which can't be undone while the claim is pending, EXP-054).
- **Go test:** `TestAdv_OnlyPaidSetOff`
- **Sources:** Expense spec §4, §5; research G1; CD-271

### EXP-056 · Duplicate lines within one claim warn
- **Priority:** P1
- **Level:** integration
- **Given** a claim whose lines 1 and 2 have the same date, type and amount and different receipts **Then** the check page warns "Line 2: This looks the same as line 1." and submit still succeeds. **When** the two lines share one receipt on purpose (`Same receipt as line 1`, a split bill) **Then** no warning.
- **Go test:** `TestExp_DuplicateWithinClaim`
- **Sources:** Expense spec §5; research G2, S-16, S-18 (Odoo `hr_expense.py:872, 892-916`)

### EXP-057 · The same supplier invoice on another live claim warns
- **Priority:** P1
- **Level:** integration
- **Given** Suresh's approved claim with a company bill from `27AABCS1429B1ZU`, invoice `INV-778` **When** Deepak's claim has a company-bill line (Billed to PUNE) with the same GSTIN and invoice no. (a re-shot photo, different bytes) **Then** his check page warns "Line 1: Invoice INV-778 from 27AABCS1429B1ZU is also on another claim.", and Meena's view of the claim page names Suresh's claim number. Different invoice no., or Suresh's claim cancelled: no warning. Submit still succeeds.
- **Go test:** `TestExp_DuplicateInvoice`
- **Sources:** Expense spec §5; research G3, S-17 (Rule 46(b))

### EXP-058 · Billed to on company bills
- **Priority:** P1
- **Level:** integration
- **Given** the fixture company (PUNE `27AAFCV2449G1Z7`, BLR `29AAFCV2449G1Z3`) **When** a company-bill line has no Billed to **Then** submit is refused with "Line 1: Billed to is required for a bill in the company GSTIN."; with BLR it submits and `snap_billed_gstin` is `29AAFCV2449G1Z3`, unchanged after BLR's GSTIN is edited. With a branch `SITE-OFF` added in setup (enabled, blank GSTIN), a crafted post naming it is refused with "Line 1: Choose a branch with a GSTIN." **Given** BLR disabled (one branch GSTIN) **Then** the field is hidden, the line saves with Billed to set to PUNE, and submit needs nothing more. A line without the tick has no Billed to.
- **Go test:** `TestExp_BilledTo`
- **Sources:** Expense spec §3.2, §8; research G4, S-22 (Rule 46(d), Rule 36(2))

### EXP-059 · The advance close page shows what's left
- **Priority:** P1
- **Level:** integration
- **Given** Deepak's paid advance of ₹10,000.00 with ₹8,500.00 used **When** `meena` opens its close page **Then** it shows "₹1,500.00 is left on this advance. Closing treats it as returned." above the reason; after the close, the advance is closed and isn't offered to claims.
- **Go test:** `TestAdv_ClosePageShowsLeft`
- **Sources:** Expense spec §4 (Close row); research G5, S-43, S-44

### EXP-060 · Money left on another advance warns
- **Priority:** P1
- **Level:** integration
- **Given** Deepak's paid advance with ₹4,000.00 left **When** he checks a new advance **Then** the check page warns "Deepak Nair still has ₹4,000.00 left on EA/26-27/00001.", and `priya`'s approval page shows the same; submit and approve still succeed. An advance the viewer can't read gives no warning at all, neither number nor amount (CD-305) An approved advance not marked as paid doesn't warn.
- **Go test:** `TestAdv_OtherAdvanceWarning`
- **Sources:** Expense spec §5; research G7, S-49 (HRMS `employee_advance.py:274-290`)

### EXP-061 · A company bill past the s.16(4) date warns
- **Priority:** P1
- **Level:** integration
- **Given** the clock at 2026-10-24 and an Administrator back-entering a claim **When** a company-bill line (Billed to PUNE) is dated 2025-03-31 **Then** the check page warns "Line 1: This bill is dated in FY 24-25. Input tax credit on it closed on 30-11-2025." A company bill dated 2026-03-31 (FY 25-26, closes 30-11-2026) and a non-company bill dated 2025-03-31 don't warn. The claim still submits.
- **Go test:** `TestExp_TimeBarWarning`
- **Sources:** Expense spec §5, §8; research G8, S-23 (CGST s.16(4))

### EXP-062 · A back-entered claim shows the warnings on its check page
- **Priority:** P1
- **Level:** integration
- **Given** no approval rule for `expense.Claim` and an Administrator back-entering a claim for EMP-007 whose line duplicates one on Deepak's approved claim **Then** the check page shows the duplicate warning before submit, and submit approves and numbers it at once.
- **Go test:** `TestExp_BackEntryWarnings`
- **Sources:** Expense spec §5, §14; research G12, S-19 (Odoo `hr_expense.py:1405`)

### EXP-063 · Advance → two claims → close, at 360 px
- **Priority:** P1
- **Level:** e2e
- **Given** a 360 px browser **When** `deepak` requests ₹10,000.00 and `priya` approves (`EA/26-27/00001`), `meena` marks it paid; claim 1 sets ₹6,000.00 against it and is approved (the advance page shows used ₹6,000.00, left ₹4,000.00); claim 2 sets ₹2,500.00 and is approved (left ₹1,500.00); `meena` closes the advance with "₹1,500 returned by NEFT 1234" **Then** a third claim's details step doesn't offer it. After the browser steps, the service shows used ₹8,500.00, state closed and two `effect` audit entries.
- **Go test:** `TestE2E_AdvanceTwoClaimsClose`
- **Sources:** research X3, T3, T10 (HRMS `test_employee_advance.py:73-161`)

### EXP-064 · A claim above the advance pays the balance
- **Priority:** P1
- **Level:** e2e
- **Given** Deepak's paid advance of ₹10,000.00 **When** he files a claim of ₹12,000.00 against it at 360 px **Then** the check page shows less advance ₹10,000.00 and to pay ₹2,000.00; after `priya` approves, the print shows "Rupees Two Thousand Only"; the service shows used ₹10,000.00.
- **Go test:** `TestE2E_ClaimAboveAdvance`
- **Sources:** research X4; EXP-026, EXP-032

### EXP-065 · The Director's own claim through the fallback
- **Priority:** P1
- **Level:** e2e
- **Given** Fallback Approver held by `meena` and `kavya` **When** `meena` files and submits a claim **Then** she sees no Approve button on it, `kavya` approves it, and the service shows it approved by `kavya`.
- **Go test:** `TestE2E_DirectorClaimFallback`
- **Sources:** research X6; EXP-039

### EXP-066 · Reject, revise, fix a receipt, resubmit
- **Priority:** P1
- **Level:** e2e
- **Given** Deepak's pending claim whose line 2 has a reason instead of a receipt **When** `priya` rejects with "Line 2 has no receipt", `deepak` revises, adds a photo to line 2 and resubmits, and `priya` approves **Then** the service shows line 2's `receipt_sha256` set, no reason, and the claim approved.
- **Go test:** `TestE2E_RejectReviseReceipt`
- **Sources:** research X7; EXP-019; `05-files-print.md` FILE-039

### EXP-067 · A buyer's own cash claim for a local buy
- **Priority:** P1
- **Level:** e2e
- **Given** `priya` buys site materials for cash **When** she files a claim with a `SITE-MISC` line tagged PRJ-METRO and METRO-SITE, ticks `Bill in company GSTIN` with `27AABCS1429B1ZU`, `INV-901` and Billed to PUNE, and `ravi` approves (Priya has no Approve button on her own) **Then** the expense lines report lists the line under PRJ-METRO with the GSTIN and invoice no., and it appears in no PO pending figure or GRN. Built by MOD-EXP or MOD-GRN, whichever finishes last (CD-271).
- **Go test:** `TestE2E_BuyerCashClaim`
- **Sources:** research X5; GRN spec §1, §12

### EXP-068 · A claim line's project comes from its site
- **Priority:** P1
- **Level:** unit
- **Given** a draft claim line with site METRO-SITE (project PRJ-METRO) **When** the line is saved with no project, or with another project **Then** the project fills as PRJ-METRO, and another project is refused with "Line 1: Site METRO-SITE is on project PRJ-METRO. Pick that project or another site." (CD-337 (5)).
- **Go test:** `TestExpE13_LineProjectFromSite`
- **Sources:** CD-337 (5); form audit P2

### EXP-069 · An advance line's project comes from its site
- **Priority:** P1
- **Level:** unit
- **Given** a draft advance line with a site **When** it is saved with no project or another project **Then** the same default and refusal as EXP-068 apply.
- **Go test:** `TestExpE13_AdvanceLineProjectFromSite`
- **Sources:** CD-337 (5)

### EXP-070 · The site-project rule runs only on drafts
- **Priority:** P1
- **Level:** unit
- **Given** a submitted claim whose line's site later moves to another project **When** it is approved **Then** approval isn't blocked and the line keeps its project.
- **Go test:** `TestExpE13_SiteProjectDraftOnly`
- **Sources:** CD-337 (5); AD-11 ruling

### EXP-071 · A claim line's HSN/SAC is checked
- **Priority:** P1
- **Level:** unit
- **Given** a claim line **When** its HSN/SAC (after the expense type's default) fails the goods or SAC rule **Then** it is refused with "Line {n}: " plus the gstin text.
- **Go test:** `TestExpE13_LineHSN`
- **Sources:** CD-337 (5)

### EXP-072 · A supplier GSTIN equal to our own branch's warns
- **Priority:** P2
- **Level:** unit
- **Given** a company-bill line whose supplier GSTIN is an enabled branch's GSTIN **When** the claim is checked **Then** it warns "Line {n}: Supplier GSTIN {gstin} is our own branch's GSTIN."
- **Go test:** `TestExpE13_OwnBranchGSTIN`
- **Sources:** CD-337 (5)

### EXP-073 · The employee is hidden only as an input
- **Priority:** P1
- **Level:** unit
- **Given** a non-admin's claim **When** it is shown on the list, search and CSV, and in the wizard **Then** the employee is hidden as a wizard input only, carried on lists and the CSV, and a sort or filter on employee_id is refused for everyone (CD-327 (2)).
- **Go test:** `TestExp_EmployeeHiddenOnlyAsInput`
- **Sources:** CD-327 (2)

### EXP-074 · A receipt clears the no-receipt reason
- **Priority:** P1
- **Level:** unit
- **Given** a draft claim line with a no-receipt reason **When** a receipt is attached and the claim saved **Then** the reason is cleared (CD-357).
- **Go test:** `TestExp_ReceiptClearsReason`
- **Sources:** CD-357, CD-302

### EXP-075 · Mark as paid shows its flash after an htmx post
- **Priority:** P1
- **Level:** e2e
- **Given** an approved advance **When** the user marks it paid from the page **Then** the page moves to the advance and shows "Marked as paid." (EUX-040, CD-80, CD-357).
- **Go test:** `TestE2E_PaidFlashAfterHTMXPost`
- **Sources:** CD-357

### EXP-076 · The paid route answers htmx with a redirect
- **Priority:** P1
- **Level:** unit
- **Given** an htmx POST to mark paid or undo paid **When** it succeeds **Then** the answer is 200 with HX-Redirect and the flash; a plain POST gets 303.
- **Go test:** `TestAdv_PaidHTMXRedirects`
- **Sources:** CD-357

### EXP-077 · GST on the bill can't be negative
- **Priority:** P1
- **Level:** unit
- **Given** a claim line **When** its GST amount is -1.00 or -0.01, on create or edit **Then** it is refused with "Line 2: GST on the bill can't be negative." and nothing is written; 0.00 and positive amounts save (the money-input lesson, E10).
- **Go test:** `TestExp_GSTNegative`
- **Sources:** UI v2 B3 review; CLAUDE.md money inputs

### EXP-078 · Paid pages hide the phone bottom nav
- **Priority:** P1
- **Level:** integration
- **Given** an approved claim **When** a user opens Mark as paid or Undo paid **Then** the page has no bottom nav (Focus mode), while the advances report and its filter still show it.
- **Go test:** `TestExpFocus_PaidPagesHaveNoBottomNav`
- **Sources:** CD-352 (4)

### EXP-079 · Line amounts are unsigned number inputs
- **Priority:** P1
- **Level:** unit
- **Given** a claim line page **When** it renders **Then** the gst_amount and amount inputs carry no data-signed, so a minus can't be typed (B3 live input rules).
- **Go test:** `TestExpLine_NumberInputsUnsigned`
- **Sources:** CD-355; EXP-077

### EXP-080 · Claim remarks sit in More details
- **Priority:** P2
- **Level:** integration
- **Given** a claim's details step **When** Remarks is empty **Then** it sits in a closed More details group at the end of the step, named in its summary; a stored value, a typed value redrawn on 422 or an error on Remarks opens it; when Remarks is hidden the group is not drawn (CD-359 (7)).
- **Go test:** `TestMoreExp_ClaimRemarks`, `TestMoreExp_ClaimRemarksHidden`
- **Sources:** CD-359 (7); UI v2 §5.5 item 3

### EXP-081 · A claim line's note sits in More details after Date
- **Priority:** P2
- **Level:** integration
- **Given** a claim line page **When** Note is empty **Then** it sits in a closed More details right after Date, and opens on a value or an error; a hidden Note is never inside it.
- **Go test:** `TestMoreExp_ClaimLineNote`, `TestMoreExp_ClaimLineNoteHidden`
- **Sources:** CD-359 (7); expense UX §5.5

### EXP-082 · Advance line description, project and site sit in More details
- **Priority:** P2
- **Level:** integration
- **Given** an advance line **When** description, project and site are empty **Then** they sit in docscreen's More details group, which opens on a value or an error and never holds a hidden field.
- **Go test:** `TestMoreExp_AdvanceLine`, `TestMoreExp_AdvanceLineHidden`
- **Sources:** CD-359 (7)

### EXP-083 · The expense More helper composes MoreDetails
- **Priority:** P2
- **Level:** unit
- **Given** a field **When** the helper draws it **Then** a hidden field draws nothing, a field without More draws the bare input, and a More field is wrapped in components.MoreDetails, open on a value or an error.
- **Go test:** `TestMoreExp_Helper`
- **Sources:** CD-359 (7)
