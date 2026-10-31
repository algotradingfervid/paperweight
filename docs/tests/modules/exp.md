# Expense Claim and Employee Advance (EXP)

## Scope
The Expense module of `docs/specs/2026-10-30-module-expense-spec.md` v0.5: `expense.Claim` and `expense.Advance`, photo-first claims with one receipt per line, optional GST capture, one reporting-manager approval with the Fallback Approver role, advances set against claims, close, print and reports. Cases are spec §17 (EXP-001 to EXP-041), in one area, `EXP`. The plan is `docs/superpowers/plans/2026-10-31-mod-exp.md`.

Fixtures are in `docs/tests/core/00-fixtures.md`, including the `expense.Claim` and `expense.Advance` rights rows, the Fallback Approver role and the expense test seed. Expense types `TRAVEL`, `FOOD`, `FUEL`, `LODGING` and `SITE-MISC`. Clock 2026-10-24 10:30 IST, FY 2026-27. Both approval rules are seeded by the expense test environment: ₹0.00, reporting manager, fallback role Fallback Approver. No fixture user holds Fallback Approver; each test gives it (usually to Meena and Kavya) in its own setup. Meena also holds Employee in the expense seed (spec §11, §15 item 3).

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
- **Given** line 1 with `company_bill` ticked **When** the claim is submitted with no supplier GSTIN and no invoice no. **Then** it is refused with "Line 1: Supplier GSTIN is required for a bill in the company GSTIN." and "Line 1: Invoice no. is required for a bill in the company GSTIN." (both pass on a draft save). **When** the supplier GSTIN is `27AABCS1429B1Z5` **Then** the save is refused with "Line 1: GSTIN 27AABCS1429B1Z5 is not valid: the last character doesn't match. Check for typing mistakes." `27AABCS1429B1ZU` with invoice no. `INV-778` submits.
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
- **Given** Deepak's approved `EX/26-27/00001` with a line on 2026-10-20, `TRAVEL`, ₹1,250.00 and receipt hash H **When** his new claim has line 1 with the same hash and line 2 with the same date, type and amount **Then** the check page and the approval page warn "Line 1: This receipt is also on EX/26-27/00001." and "Line 2: This looks the same as a line on EX/26-27/00001.", and submit still succeeds. **When** `EX/26-27/00001` is cancelled **Then** neither warning appears. **When** the other claim is one the viewer can't read **Then** the warning doesn't name it ("another claim", proposed CD; CD-240, CD-248), on the real `auth.Service`.
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
- **Given** EXP-025's approved claim naming `EA/26-27/00001` **When** `meena` cancels the advance **Then** it is refused with DOC-052's text naming the claim, and the advance stays approved. After the claim is cancelled, the advance cancels.
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
- **Given** approved and closed claims and advances for Deepak and Kavya **Then** `/reports/expense/open` lists approved, not closed claims with the columns claim, employee, purpose, approved on, approved by, total, less advance, to pay, lines without a receipt; `/reports/expense/advances` lists approved, not closed advances with advance, employee, date, purpose, amount, used, left, age in days, and its employee filter. `priya` sees only her team's rows, `meena` all. The CSV needs `export` (AUTH's 403 otherwise).
- **Go test:** `TestExp_OpenReports`
- **Sources:** Expense spec §13, §17

### EXP-035 · Expense lines report: every column read back
- **Priority:** P1
- **Level:** integration
- **Given** claims whose lines set every field **When** `/reports/expense/lines` and its CSV are read **Then** each row has claim, employee, date, type, project, site, note, amount, GST, supplier GSTIN, invoice no., receipt yes or no and state, each asserted by value; the month, type, project and employee filters each narrow the rows.
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
- **Given** random sequences of save, submit, approve, reject, revise, cancel, close and amend over claims and advances **Then** after each step: a claim's total equals the sum of its lines; an advance's used equals the sum of `less_advance` over its approved and closed claims; left (`amount − used`) is never below 0.
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
