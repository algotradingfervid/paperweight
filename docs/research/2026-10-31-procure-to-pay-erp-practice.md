# Research: how established ERPs build and test PR → PO → goods receipt

Task research-p2p · researcher · 31 October 2026. Read-only; no repo file edited.

**Our documents compared:** PO spec v0.5 (`docs/specs/2026-10-30-module-purchase-order-spec.md`), GRN spec v0.4 (`docs/specs/2026-10-30-module-grn-spec.md`, §16 table), PO test doc (`docs/tests/modules/po.md`, PO-001 to PO-070). No GRN test doc exists yet (`docs/tests/modules/` has README, exp, hook, po), so GRN coverage below points at GRN spec §16 IDs.

**Sources read (shallow sparse clones, read only, nothing executed):**

| Tag | Repo | Commit | Dirs |
|---|---|---|---|
| EN | github.com/frappe/erpnext (develop) | `a6f970eb` (31 Oct 2026) | `erpnext/buying`, `erpnext/controllers`, `erpnext/stock/doctype/{purchase_receipt,material_request,stock_settings}` |
| OD | github.com/odoo/odoo (20.0) | `1ab1ebbd` (31 Oct 2026) | `addons/purchase`, `addons/purchase_stock`, `addons/purchase_requisition` |
| TR | github.com/tryton/tryton | `bc4f1f40` (28 Oct 2026) | `modules/purchase`, `purchase_request`, `purchase_requisition`, `purchase_amendment`, `stock/tests` |

Paths below are relative to each repo root. ERPNext test files are long: `EN-PO` = `erpnext/buying/doctype/purchase_order/test_purchase_order.py`, `EN-PR` = `erpnext/stock/doctype/purchase_receipt/test_purchase_receipt.py`, `EN-MR` = `erpnext/stock/doctype/material_request/test_material_request.py`, `EN-SU` = `erpnext/controllers/status_updater.py`, `EN-IC` = `erpnext/controllers/tests/test_item_close.py`. `OD-PSO` = `addons/purchase_stock/tests/test_purchase_order.py`, `OD-PROC` = `addons/purchase_stock/tests/test_purchase_order_process.py`.

Law: CBIC CGST Rules PDF "amended upto 24th September 2021" (https://cbic-gst.gov.in/pdf/24092021-CGST-Rules-2017-Part-A-Rules.pdf), and CBIC tax repository section pages for CGST Act s.9, s.16, s.34 (https://taxinformation.cbic.gov.in/content/html/tax_repository/gst/acts/2017_CGST_act/active/chapter5/section16_v1.00.html and siblings). The Rules PDF is five years old. Later amendments to Rules 55 and 138 **couldn't be verified** from a newer CBIC copy. All tax points are "CA to confirm", as in our specs.

---

## 1. How each ERP models PR → PO → receipt

| Aspect | ERPNext | Odoo 20 community | Tryton | Paperweight (ours) |
|---|---|---|---|---|
| Request document | Material Request. States Draft, Pending, Partially Ordered, Ordered, Partially Received, Received, Stopped, Cancelled (EN-SU:135-169). `per_ordered` and `per_received` per MR | **None** in `purchase`, `purchase_stock` or `purchase_requisition`. `purchase.requisition` is a vendor agreement (blanket order or tender), types at `addons/purchase_requisition/models/purchase_requisition.py:21`, states draft/confirmed/done(Closed)/cancel (:34-39) | Two: `purchase.requisition` (employee request with approval: draft, waiting, rejected, approved, processing, done, cancelled; `modules/purchase_requisition/purchase.py:120-126`) creates `purchase.request` rows (draft, purchased, done, cancelled, exception; `modules/purchase_request/purchase_request.py:100-105`) | Purchase Request, no approval, Submitted then Closed or Cancelled; `order_status` not/part/ordered (PO spec §20.2-§20.3) |
| Request → PO quantity rule | PO qty above MR qty refused unless `over_order_allowance` % (Buying Settings, `buying_settings.json:331`). A stock role doesn't bypass it (EN-PO:242-293) | n/a | Request is one line; a cancelled PO puts the request in `exception`, and a user picks reset or cancel (`modules/purchase_request/tests/scenario_purchase_request.rst:108-144`) | Over-ordering allowed with a check-page warning (§20.5). PO cancel frees rows automatically (§20.2 `lineOrdered`) |
| PO states | Draft, To Receive and Bill, To Receive, To Bill, Completed, On Hold, Closed, Cancelled; derived from `per_received` / `per_billed` (EN-SU:88-111) | draft (RFQ), sent, to approve, purchase, cancel (`addons/purchase/models/purchase_order.py:108-114`), plus a `locked` flag (:115-119) | draft, quotation, confirmed, processing, done, cancelled (`modules/purchase/purchase.py:261-267`); transitions incl. done→processing (:302-312) | Kernel: draft, pending, approved, rejected, closed, cancelled; `receipt_status` not/part/fully received (PO spec §6.3) |
| Receipt status | `per_received` % on PO (EN-SU:88-111) | `receipt_status` pending/partial/full from picking states (`addons/purchase_stock/models/purchase_order.py:62-71`) | `shipment_state` none, waiting, partially shipped, received, exception (`modules/purchase/purchase.py:236-242`) | `receipt_status`, computed by `RefreshReceiptStatus` (PO spec §6.3) |
| Quantities per PO line | `qty`, `received_qty`, `returned_qty`, `billed_amt` (EN-PO:2513-2523 asserts `returned_qty`) | `product_qty`, `qty_received`, `qty_invoiced`; returns subtract when `to_refund` (`addons/purchase_stock/models/purchase_order_line.py:61-86`) | Moves per line; cancelled moves raise `exception` (`modules/purchase/tests/scenario_purchase_line_cancelled.rst:64-82`) | `qty`, `already_received` (cutover), `received_qty` roll-up of **accepted**; pending never stored (PO spec §3.2) |
| Does rejected count as received? | **Yes**. PO `received_qty` sums receipt `received_qty`, which is accepted + rejected (`erpnext/stock/doctype/purchase_receipt/purchase_receipt.py:156-174`; `erpnext/controllers/buying_controller.py:1021-1034`). Pending only reopens when a return is made | No rejected quantity at receipt: you receive less, or receive and return (`purchase_order_line.py:61-86`) | Same as Odoo (moves) | **No**: accepted counts and rejected stays pending (GRN spec §4, CD-234) |
| Over-receipt tolerance | Item or global `over_delivery_receipt_allowance` % (`stock_settings.json:128`); above it, `OverAllowanceError` (EN-SU:524-621). A named role may bypass it with a warning (EN-SU:557, :623-638; `stock_settings.json:251`). The default is 0, so it refuses | Not enforced in core: you can validate more than the demand. **Couldn't verify** a core over-receipt check in the files read | No purchase tolerance module: the repo has `sale_shipment_tolerance` but no purchase one (`git ls-tree modules/`) | Warn only, no limit (GRN spec §4) |
| Short close | Whole PO "Closed" releases ordered qty (EN-PO:1027-1047), and Re-open is allowed (EN-PO:1327-1350). **Row-level close** too (`erpnext/controllers/item_close.py:47-100`, tests EN-IC:49-190) | Receive with "no backorder" leaves a warning activity on the PO (OD-PSO:877-985). A cancelled backorder no longer counts as late (OD-PSO:1100-1118) | Cancel the remaining move, then "handle shipment exception → ignore", and the PO goes to done (`scenario_purchase_line_cancelled_on_shipment.rst:62-91`) | Whole-PO close with a reason; no reopen (PO spec §6.4, R6) |
| Hold | "On Hold" blocks receipts and invoices (EN-PO:861-868) | `locked` blocks edits and cancel (`purchase_order.py:723-726`) | none found | none |
| Returns | Return receipt (`is_return`, negative qty) reduces PO `received_qty` (EN-PO:781-819, :2513). Allowed against a closed PO (EN-PR:748-781) | Return picking reduces `qty_received` (OD-PSO:83-156); re-receiving the returned goods ("exchange") leaves PO quantities right (OD-PSO:599-623) | Return wizard (`scenario_purchase_return_wizard.rst:62`) | No return document. Gate rejections go back on the truck; goods found bad later mean GRN cancel + amend (GRN spec §2) |
| Change after approval | "Update Items" changes qty and rate, and adds or removes rows, on a submitted PO. It refuses qty below received (EN-PO:371-401), UoM changes after receipt (:403-433), removing a received row (:510-565), users without rights (:591-611), and closed or held POs (:2499-2511). Docs: "Use Update Items for changes ERPNext permits after submission", else cancel and amend (https://docs.frappe.io/erpnext/user/manual/en/purchase-order) | A confirmed PO stays editable unless the company sets `po_lock` = lock (`addons/purchase/models/res_company.py:27`). Raising qty after a receipt creates a new move (OD-PSO:577-587); lowering qty below the billed qty schedules an activity (`purchase_order_line.py:186-196`) | Amendment document with `revision` number. It can change party, product, qty, price, taxes and payment terms on a processing PO, and updates pending moves and invoices (`modules/purchase_amendment/tests/scenario_purchase_amendment.rst:88-157`) | None (R7). Cancel + amend if nothing received; otherwise close + new PO (PO spec §5) |
| Cancel with receipts | Blocked by a linked submitted receipt (Frappe back-link). A refused cancel rolls back fully (EN-PR:7960-7982) | **Allowed**: done receipts stay done, open ones are cancelled (`purchase_stock/models/purchase_order.py:221-235`; OD-PROC:75-152) | Draft/quotation only (`purchase.py:309-310`) | Blocked by a pending, approved or closed GRN (DOC-052, PO-025) |
| Approval | No built-in amount rule in the files read (workflow or authorization rules elsewhere; **couldn't verify** from this clone) | `po_double_validation` one_step/two_step with an amount (`res_company.py:33-40`). Managers always pass (`purchase_order.py:1332-1340`), so a manager approves their own PO | Requisition approval group (`scenario_purchase_requisition.rst:112-127`) | Kernel approval rules by amount; self-approval refused (PO-022) |
| Rate kept from PO to receipt | `maintain_same_rate` with action Stop or Warn (`buying_settings.json:92-133`; `erpnext/controllers/tests/test_mapped_discounts.py:25`) | n/a | n/a | GRN takes the PO's net rate as a snapshot; bill amount difference is information only (GRN spec §3.3) |

---

## 2. How their tests are written

**ERPNext** (unittest via `ERPNextTestSuite`)
- One class per DocType (EN-PO:33). Builder functions take kwargs and the flags `do_not_save` / `do_not_submit`, so one helper gives draft, saved or submitted documents (`create_purchase_order`, EN-PO:2636-2677). Flow helpers call the **production mapper** that the UI uses, not a fake: `create_pr_against_po` → `make_purchase_receipt` (EN-PO:2680-2685).
- Settings are toggled per test with a decorator or context manager: `@ERPNextTestSuite.change_settings("Buying Settings", {...})` (EN-PO:34, :59, :65).
- **Do, check, undo, check** (EN-PO:95-122): submit PO, then assert ordered qty; receive 4, assert; raise the allowance and receive 8, assert 12; cancel, assert back to 4.
- Refusals assert the exception **class** and often the **message**: `assertRaisesRegex(frappe.ValidationError, "Cannot set quantity less than received quantity", …)` (EN-PO:394-401).
- Rollback is proven by counting ledger rows before and after a refused cancel (EN-PR:7960-7982).
- Permissions run as a named user: `with self.set_user(...)` (EN-PO:283-291, :591-611).
- Recent tests have sentence names and docstrings that cite the regression issue: "Regression test for #55246" (EN-PO:124-131); `test_closed_row_is_not_mapped_to_purchase_receipt` (EN-IC:163).

**Odoo** (`TransactionCase` subclasses)
- Shared fixtures in `setUpClass`, with a reusable `po_vals` dict (OD-PSO:18-44). A common base has `_create_purchase(product, quantity, confirm=True, receive=…)` and `_receive(po, quantity)` (`addons/purchase_stock/tests/common.py:31-58`).
- UI emulation: `Form(...)` and `Form.from_action(env, picking.button_validate()).save().process()` drive the real backorder wizard (OD-PSO:439-449). `process_cancel_backorder()` is the "no backorder" path (OD-PSO:935).
- Time is frozen with `@freeze_time("2021-01-14 09:12:15")` (OD-PSO:12). The acting user is set with `@users('purchase_user')` (OD-PROC:13).
- Every assert carries a message saying what should have happened ("Done pickings should not change state.", OD-PROC:94).
- Names are numbered (`test_00_…`), with a docstring stating the scenario (OD-PROC:76-79, :106-110).

**Tryton** (doctest scenarios in `.rst`, run through the proteus client)
- Each step is a sentence heading followed by commands and expected output: "Create the purchase then cancel it::", then `>>> pr.state` / `'exception'` (`scenario_purchase_request.rst:108-120`).
- Expected refusals are written as `Traceback … RequiredValidationError: ...` (`scenario_purchase_requisition.rst:76-110`). The file reads as an executable Given/When/Then.

**Patterns to borrow**

1. **Do and undo in one test.** Every effect test asserts the state after the action **and** after its reversal: cancel, close, amend. GRN-018 does this for cancel; make it the rule for every roll-up case (PO-030, PO-055, GRN-011, GRN-014).
2. **One builder per DocType through the real engine path** (`Engine.Do`), with stop-at flags (draft, submitted, approved). This matches ERPNext's `do_not_submit` and our lenient-doubles rule (CD-125).
3. **Assert the message text, not only the error kind.** Our test doc already names texts; keep the Go tests on exact strings.
4. **Quantity walk tables.** ERPNext's 10 → receive 4 → receive 8 (with 50%) → cancel → 4 sequence is a compact way to write GRN-011, GRN-014 and PO-044 as worked examples before the fuzz.
5. **Rollback by row counts** (EN-PR:7960). GRN-024 already does this; reuse it for PO cancel and request close.
6. **Frozen clock** for every date rule (PO-018, PO-042, GRN-006, GRN-023), as Odoo does.
7. **Docstring cites the CD** that a test guards, as ERPNext cites issue numbers.

---

## 3. Scenario catalogue

"Covered" means a listed case asserts it. "Partly" means a case touches it without the specific assertion. GAP means no case. Source line numbers point at the test function's `def` line, or at the scenario step heading.

### 3.1 Requests

| # | Given / When / Then | Source | Ours |
|---|---|---|---|
| S01 | Given a request pulled into an approved PO, when the PO is cancelled, then the request's ordered qty returns to 0 and it is orderable again | EN-MR:459 `test_completed_qty_for_purchase`; TR `scenario_purchase_request.rst:108` | Covered PO-055 |
| S02 | Given a request closed while a draft PO holds its row, when the PO is submitted or approved, then it is refused | EN-MR:459 (Stopped → `InvalidStatusError` on submit) | Covered PO-053, PO-069 |
| S03 | Given a closed request whose rows are on an approved PO, when that PO is short-closed, then the close succeeds and the request's ordered qty is recomputed | EN-MR:1913 `test_purchase_order_of_a_stopped_request_can_be_closed` | **GAP**: §20.3 calls `RefreshOrderStatus` from `OnClose`, but no case closes a PO linked to an already closed request |
| S04 | Given a fully ordered request, when Create PO is used, then nothing is mapped; a closed request can't be mapped | EN-MR:1894 `test_mappers_skip_ordered_rows_and_stopped_requests` | Covered PO-051, PO-052 |
| S05 | Given a request row of 100, when a PO orders 110, then ERPNext refuses unless an over-order allowance is set; we warn on the check page | EN-PO:242 `test_over_order_allowance_against_material_request` | **GAP**: the warning `Line {n}: ordering {x} more than request {PR} asked for.` (§20.5) has no case |
| S06 | Given a PO row linked to a request, when its qty is lowered or the row removed, then the request's pending goes back up by the difference | EN-PO:295 `test_update_remove_child_linked_to_mr` | Partly: PO-057 (fuzz). No named case for editing a held row on a draft |
| S07 | Given lines from two requests for the same item, when both are added to one PO, then each PO line keeps its own request link | EN-PO:740 `test_same_item_from_different_material_requests`; TR merges them into one line (`scenario_purchase_request.rst:171`) | Partly: PO-052 doesn't assert two rows with the same item |
| S08 | Given one request row, when part goes on a PO to vendor A and the rest to vendor B, then the row reads ordered and both POs link it | TR `scenario_purchase_requisition.rst:213`; our §20.4 allows splitting | **GAP**: no explicit split case |
| S09 | Given a request held by a PO that is rejected, withdrawn or deleted, then the rows are held (rejected or withdrawn) or freed (deleted) per `lineOrdered` | TR request `exception` (`scenario_purchase_request.rst:108-144`); our §20.2 table | **GAP**: PO-055 covers approve, cancel, close and amend only |
| S10 | Given a request missing item or qty, when submitted, then it is refused | TR `scenario_purchase_requisition.rst:76-110` | Covered by kernel required-at-submit (CD-178). No PO-area case |
| S11 | Given a request line with free text and no item, then a PO line without an item is created | TR `scenario_purchase_request.rst:187-214` | Differs by design (§20.2 assumption: item master only) |
| S12 | Given a requisition, when it is submitted, then it waits for approval and can be rejected | TR `scenario_purchase_requisition.rst:121`, `:281` | Differs by design (§20.6: no approval) |
| S13 | Given a stale request page, when saved, then "modified after you opened it" | EN-MR:1442 `test_check_modified_date_detects_concurrent_modification` | Covered generically (CD-94). PO-054 for rows |

### 3.2 Ordering

| # | Given / When / Then | Source | Ours |
|---|---|---|---|
| S14 | Given a PO line with qty 0 or −1, when saved, then refused; a client can't force 0 | EN-PO:35, :74 | **GAP**: spec §3.2 "more than 0" has no PO case |
| S15 | Given a negative rate, then refused | EN-PO:59 | **GAP**: spec §3.2 "not negative" has no PO case |
| S16 | Given the same item on two lines, then allowed or refused per setting | EN-PO:1049 `test_group_same_items` (`allow_multiple_items`) | **GAP**: spec silent. Decide and test (request rows already refuse duplicates, §20.5) |
| S17 | Given a disabled UoM or a fraction of a whole-number UoM, then refused | EN-PO:901, :907 | Covered PO-015, PO-016 |
| S18 | Given a vendor on hold or disabled, then a new PO is refused; given it is disabled while the PO is pending, then approve fails re-validation | EN-PO:1074 `test_po_for_blocked_supplier_all`; our §4 `BeforeSubmit`, D-APR-15 | **GAP**: no PO case for "disabled while pending" |
| S19 | Given a draft, when the vendor changes to a foreign-rule party, then taxes are recomputed | TR `scenario_purchase_modify_header.rst:79` | Covered PO-003 |
| S20 | Given line delivery dates, then the header date is the earliest | EN-PO:1295 `test_schedule_date` | Differs by design (§16: header only) |
| S21 | Given a vendor or item carrying a purchase warning, then the buyer sees it on the PO | OD `addons/purchase/tests/test_purchase.py:1015` | GAP, not in scope (no master field) |
| S22 | Given confirmed POs, then numbers follow the sequence | OD `test_purchase.py:94` | Covered PO-019, PO-023, PO-040 |

### 3.3 Approval

| # | Given / When / Then | Source | Ours |
|---|---|---|---|
| S23 | Given two-step approval, when a user without the manager group approves, then the state stays "to approve"; after the group is added, it approves | OD `addons/purchase/tests/test_access_rights.py:104` | Covered PO-019, PO-020 |
| S24 | Given a manager confirms their own PO, then Odoo approves it at once; we refuse self-approval | OD `purchase_order.py:1332-1340` | Covered PO-022 (differs on purpose) |
| S25 | Given an amount below the threshold, then approved on confirm | OD `purchase_order.py:1336-1340` | Covered PO-021 |

### 3.4 Amendment and changes after approval

| # | Given / When / Then | Source | Ours |
|---|---|---|---|
| S26 | Given an approved PO with part received, when qty is raised, then the new qty applies and pending grows | EN-PO:340 `test_update_child`; OD-PSO:577 `test_po_edit_after_receive`; OD-PSO:158 | Differs by design (R7: close + new PO). No case shows the screen's "close and raise new" path |
| S27 | Given received 2 Box, when qty is lowered below received, then refused | EN-PO:371 | n/a (no post-approval edit) |
| S28 | Given a receipt exists, when the UoM is changed, then refused | EN-PO:403 | n/a |
| S29 | Given a received row, when it is removed, then refused | EN-PO:510 | n/a |
| S30 | Given a processing PO, when an amendment changes party, product, qty and price, then revision becomes 1, totals are recomputed and pending moves follow | TR `scenario_purchase_amendment.rst:88-157` | Partly: PO-024 (cancel + amend, number `-1`), only when nothing is received |
| S31 | Given the vendor moves a promised date after confirmation, then open receipts follow; a validated receipt doesn't | OD-PSO:219 `test_04_update_date_planned` | **GAP / decision**: `required_by` is frozen after approval, so the overdue report shows dates the vendor has already changed |
| S32 | Given a closed or held PO, when Update Items runs, then refused | EN-PO:2499 | Covered by R7 (kernel) |

### 3.5 Receiving

| # | Given / When / Then | Source | Ours |
|---|---|---|---|
| S33 | Given a draft (unapproved) PO, when a receipt is made against it, then refused | EN-PO:87 `test_make_purchase_receipt` | Partly: GRN-001 checks the picker only. A **posted** draft or pending PO id isn't tested (DOC `msgSourcePending`) |
| S34 | Given received 3 with accepted 1 and rejected 1, then a mismatch error; a blank received is set to accepted + rejected | EN-PR:94 `test_purchase_receipt_received_qty` | Covered GRN-008, GRN-009 (ours derives accepted) |
| S35 | Given a whole delivery rejected (accepted 0, rejected 10), when submitted, then it is allowed, the PO roll-up is unchanged and status doesn't move | EN-PR:77, EN-PR:3276 `test_return_of_a_fully_rejected_receipt` | **GAP**: no GRN case for accepted = 0 |
| S36 | Given a receipt in a different conversion factor, then PO progress is converted (10/12) | EN-PR:1159; OD-PSO:342 `test_04_multi_uom` | Differs by design (GRN §3.2: PO's unit only); GRN-017 for whole numbers |
| S37 | Given an earlier receipt of 4, when the current receipt computes already received, then it excludes itself | EN-PR:7943 `test_get_already_received_qty` | Covered GRN-016 and the §4 warning rule |
| S38 | Given three partial receipts, then the PO history shows "Received Quantity: 5.0 → 10.0" | OD-PSO:428 `test_message_qty_already_received` | Partly: the PO page lists linked GRNs; the roll-up writes no audit entry (§20.3), by design |
| S39 | Given blind receiving, then the receiver doesn't see ordered or pending quantities and must count | Oracle EBS iProcurement "Blind Receiving" (https://docs.oracle.com/cd/E26401_01/doc.122/e48938/T555694T555724.htm) | **Decision**: our step 3 prefills Received now with pending (GRN §11) |
| S40 | Given a receipt dated before the PO date, then a warning | Oracle notes receipt date tolerances (same page) | Covered GRN-006 |

### 3.6 Partial and multiple receipts

| # | Given / When / Then | Source | Ours |
|---|---|---|---|
| S41 | Given PO 10, when 4 then 8 are received and the second is cancelled, then received reads 4, 12, 4 | EN-PO:95 `test_ordered_qty` | Covered GRN-011, GRN-014, GRN-018 |
| S42 | Given PO 25, when three receipts of 5 are each validated with a backorder, then 15 received and one open backorder | OD-PSO:428 | Covered GRN-011 |
| S43 | Given a short receipt validated with "no backorder", then the PO gets a note listing the missing quantities | OD-PSO:877 `test_log_activity_in_po_with_receipt_without_backorders` | **GAP / decision**: the gate can't say "the rest isn't coming"; only a Purchase User's close does |
| S44 | Given a late PO whose backorder is cancelled, then it is no longer late | OD-PSO:1100 | Partly: PO-037 doesn't assert that a closed PO drops out of Overdue |
| S45 | Given a fully received PO with tolerance left, then a further receipt is still offered, prefilled to the remaining tolerance | EN-PO:124 | Covered GRN-001 (`Show fully received orders`), GRN-014 |
| S46 | Given two receipts submitted at once, then both count | ours | Covered GRN-015 |

### 3.7 Over and under receipt

| # | Given / When / Then | Source | Ours |
|---|---|---|---|
| S47 | Given allowance 0%, when 12 of 10 are received, then refused "over limit by 2"; with 50%, allowed | EN-SU:565-621; EN-PO:95 | **Decision**: warn only (CD-234). GRN-013 covers the warning |
| S48 | Given a user with the bypass role, when over the limit, then saved with a warning | EN-SU:557, :623-638 | n/a |
| S49 | Given a whole-number UoM and a 50% allowance on 3, then only 1 more can be received | EN-PO:178, :198 | n/a unless an allowance is adopted |
| S50 | Given a short-closed PO, then its ordered qty is released | EN-PO:1027; OD-PROC:105; TR `scenario_purchase_line_cancelled_on_shipment.rst:78` | Covered PO-033, PO-055 |
| S51 | Given a two-line PO, when one row is closed, then that row stops receiving, the PO's progress counts it settled and the other row stays open; closing every row closes the PO | EN-IC:49, :64, :83, :163, :171 | **GAP / decision**: no line close |

### 3.8 Rejection and returns

| # | Given / When / Then | Source | Ours |
|---|---|---|---|
| S52 | Given 6 accepted, when 3 are returned to the vendor, then PO received drops to 3 and pending reopens | EN-PO:781 `test_return_against_purchase_order`; EN-PO:2513; OD-PSO:83 | **GAP / decision**: no return. The workaround (cancel + amend GRN) loses the GRN number and is blocked by the lock |
| S53 | Given a closed PO, when a return is made, then it is allowed; a new receipt is still refused | EN-PR:748 | Covered by analogy: GRN-019 (cancel allowed), GRN-020 |
| S54 | Given goods returned and then re-sent by the vendor, then PO received counts them once | OD-PSO:599 | n/a (no return) |
| S55 | Given rejected goods at the gate, then they stay pending on the PO | ours | Covered GRN-012 (ERPNext differs, see §1) |

### 3.9 Cancellation

| # | Given / When / Then | Source | Ours |
|---|---|---|---|
| S56 | Given a receipt exists, when the PO is cancelled, then ERPNext and we refuse; Odoo allows it and leaves done receipts | EN-PR:7960; OD-PROC:75 `test_cancel_redraft_fulfilled` | Covered PO-025, GRN-021 |
| S57 | Given a refused cancel, then nothing changes (row counts equal) | EN-PR:7960 | Covered GRN-024 |
| S58 | Given a **draft GRN** against a PO, when the PO is closed or cancelled, then the GRN's next save or submit is refused with DOC-017 or DOC-038 | OD-PROC:105 (open backorder is cancelled with the PO) | **GAP**: GRN-020 covers only a new GRN |
| S59 | Given a GRN on a short-closed PO whose lines came from a request, when the GRN is cancelled, then the request's `ordered_qty` drops (`min(qty, received)`) | our PO spec §20.3 last sentence | **GAP**: neither PO-055 nor GRN-019 asserts it |
| S60 | Given a GRN on a closed PO with a wrong quantity, when Purchase Manager cancels and Stores amends, then the amend is refused (PO not approved), so the receipt can't be re-entered | our GRN spec §5 ("PO must still be approved") + PO spec §6.4 (R6) | **GAP**: a trap, see Gap 2 |
| S61 | Given a non-stock receipt in a closed accounting period, then its cancel is allowed | EN-PR:8500 | Differs: our lock refuses (GRN-023), by design |
| S62 | Given a requisition cancelled, then only its draft POs are cancelled | OD `addons/purchase_requisition/tests/test_purchase_requisition.py:252` | Partly: PO-056 blocks the cancel instead; the draft PO gets DOC's text on save |

### 3.10 Close and hold

| # | Given / When / Then | Source | Ours |
|---|---|---|---|
| S63 | Given an On Hold PO, when a receipt is submitted, then refused | EN-PO:861 `test_purchase_order_on_hold` | **GAP / decision**: no hold |
| S64 | Given a draft PO, when closed, then refused; only hold, close and reopen are accepted on a submitted PO | EN-PO:2387 | Covered by kernel (DOC); no PO case |

### 3.11 GST (India)

| # | Given / When / Then | Source | Ours |
|---|---|---|---|
| S65 | Given an intra- or inter-state vendor, then CGST+SGST or IGST | PO spec §7 | Covered PO-001, PO-002, PO-004 |
| S66 | Given a normal B2B supply, then goods travel with the **tax invoice**, not a delivery challan. A challan replaces the invoice only for liquid gas of unknown qty, job work, non-supply movement, notified cases, and SKD/CKD or lots | CGST Rule 55(1)(a)-(d), 55(5); Rule 55A; Rule 138A(1)(a) "the invoice or bill of supply or delivery challan, as the case may be" (CBIC Rules PDF pp. 63-64, 145) | **GAP**: the GRN requires "Vendor challan no." for every goods line (GRN §3.1, §4) |
| S67 | Given goods in lots against one invoice, then the invoice is issued before the first lot, each later lot carries a challan citing it, and credit is taken on the last lot | Rule 55(5)(a)-(d); CGST Act s.16(2) first proviso | Partly: GRN-027 (same invoice on two GRNs warns). Bills to enter lists both GRNs, so the invoice can be keyed twice |
| S68 | Given an **unregistered** vendor and a consignment above ₹50,000, then the registered recipient must generate the e-way bill | Rule 138(1)(iii) "due to inward supply from an unregistered person" (CBIC PDF p.139) | **GAP**: the e-way field is optional with no hint (GRN §7). CA |
| S69 | Given an e-way bill generated by the supplier and goods rejected at the gate, then the recipient may reject it on the portal; silence for 72 hours (or until delivery, whichever is earlier) counts as acceptance | Rule 138(11)-(12) (CBIC PDF p.143) | **GAP**: no hint on a fully rejected GRN. CA |
| S70 | Given goods returned to the supplier, then the supplier may issue a credit note, declared by 30 November after the FY | CGST Act s.34(1)-(2) (CBIC s.34 page) | Out of scope; the GRN rejected report helps (GRN-032) |
| S71 | Given reverse charge, then tax is shown but not added | CGST Act s.9(3)-(4) | Covered PO-026 to PO-028 |
| S72 | Given a site whose state differs from its billing branch, then the place of supply follows where the goods' movement ends (s.10(1)(a)), or the "bill to / ship to" rule for third-party delivery (s.10(1)(b)) | IGST s.10. **Couldn't verify on CBIC** (the IGST page returned HTTP 500); secondary: https://www.avalara.com/in/en/blog/2017/06/gst-place-supply-complexities-transaction-involves-3-parties.html | **GAP**: spec assumes site state = branch state (CD-234); nothing checks it. CA |

### 3.12 Concurrency and permissions

| # | Given / When / Then | Source | Ours |
|---|---|---|---|
| S73 | Given a stale page, when saved, then refused | EN-MR:1442 | Covered PO-034, GRN-026 |
| S74 | Given a user without update rights, when items are updated, then refused | EN-PO:591 | Covered PO-041, PO-058 |
| S75 | Given a PO the user can't see, when received against, then refused | ours | Covered GRN-002, GRN-029 |
| S76 | Given a permission-limited reader, when a helper reads a column it shouldn't, then `PermissionError` | EN-PO:2369 | n/a |

That makes 76 scenarios. 27 of them are a gap, partly covered or need an owner decision: S03, S05, S08, S09, S14-S16, S18, S21, S31, S33, S35, S39, S43, S44, S47, S51, S52, S58-S60, S63, S66-S69, S72. The rest are covered, differ by design, or are n/a.

---

## 4. Gaps where we differ from common practice

Each gap gives what the ERPs do, what we do, the risk and a recommendation. **[Owner]** marks a product decision; **[Spec]** marks a spec or test fix the head can make.

**Gap 1. No return after acceptance [Owner]**
- **ERPs:** all three have a return document that reduces PO received qty and reopens pending: EN-PO:781, OD-PSO:83, TR `scenario_purchase_return_wizard.rst:62`. ERPNext allows it even on a closed PO (EN-PR:748).
- **Ours:** cancel the GRN and amend it (GRN §2).
- **Risk:** the original GRN number is replaced, and bill details go to a cancelled GRN. When the period lock covers the GRN date, cancel is refused (GRN §5), so the app can't record the return at all. Defective goods found a week later are common on sites.
- **Recommendation:** keep "no return document" for v1. Test and document the cancel + amend path, including bill details moving and the lock refusal message (new case). List a "Return to vendor" GRN variant (negative accepted, reason required) as the first follow-up.

**Gap 2. A GRN on a closed PO can be cancelled but not re-entered [Spec, then Owner]**
- **Ours:** GRN §5 allows a cancel after the PO closed, but amend needs the PO approved. PO §6.4 / R6 forbids reopening the PO.
- **ERPs:** ERPNext allows Re-open (EN-PO:1327-1350). Odoo can redraft a cancelled PO (OD-PROC:75-103).
- **Risk:** someone corrects a typo on an old GRN and loses the receipt permanently. The PO's pending and the request's ordered qty both shift (S59, S60).
- **Recommendation:** at minimum, the GRN cancel page warns when the PO is closed: "You can't receive against {PO} again after this." Alternatives for the owner: (a) refuse GRN cancel on a closed PO; (b) allow amend against a closed PO when it only replaces a cancelled GRN. Add S60 as a GRN case either way.

**Gap 3. Over-receipt warns with no ceiling [Owner, already decided in CD-234: re-confirm]**
- **ERPs:** ERPNext refuses beyond an item or global % (default 0), with an optional bypass role (EN-SU:524-638). Oracle documents receipt quantity tolerances (S39 source).
- **Ours:** unlimited, warning only.
- **Risk:** a fat-finger 4000 for 400 submits on a phone in gloves. The PO reads fully received and Over by 3600, and nothing stops it.
- **Recommendation:** keep warn-only (the owner's CD-234 reason stands: trucks can't be turned back). Consider a hard stop above a large multiple (for example accepted > 2 × ordered), or a required reason when accepted exceeds pending. Either is a small `Validate` rule.

**Gap 4. Prefilled quantities and "Receive all pending" [Owner]**
- **Industry:** Oracle's blind receiving hides ordered and pending quantities "to count the number of items received" (https://docs.oracle.com/cd/E26401_01/doc.122/e48938/T555694T555724.htm).
- **Ours:** quantities prefilled to pending (GRN §11 step 3).
- **Risk:** Stores taps submit without counting, and short deliveries pass as full. That is the main control a GRN exists for.
- **Recommendation:** keep the prefill for speed (the 1-minute target), but make it visible on the check page ("All lines received as ordered"). Or offer a per-site setting for blind mode later. Owner's call.

**Gap 5. "Vendor challan no." is the wrong label for most deliveries [Spec + CA]**
- **Law:** Rule 55(1) limits a challan in lieu of an invoice to specific cases. Normal supplies travel with the tax invoice (Rule 55A; Rule 138A(1)(a)), and lots use invoice plus challans (Rule 55(5)).
- **Ours:** challan no. required for every goods GRN (GRN §3.1, §4); the invoice is captured later in Bill details.
- **Risk:** Stores types the invoice number into "challan", then someone types it again into Bill details, or never does because it "was entered". "Bills to enter" then misleads.
- **Recommendation:** rename to "Challan or invoice no." with a choice of Delivery challan / Tax invoice. When the choice is Tax invoice, offer to copy it into Bill details. Keep it required for goods.

**Gap 6. No line-level short close [Owner]**
- **ERPs:** ERPNext closes single rows, settles their progress, and closes the parent when all rows are closed (`erpnext/controllers/item_close.py:47-119`; EN-IC).
- **Ours:** whole-PO close only.
- **Risk:** one discontinued item on a 10-line PO forces a choice between leaving it pending forever (polluting Awaiting delivery and Overdue) or closing everything and raising a new PO for the other 9.
- **Recommendation:** defer to v2 unless the owner sees this weekly. If adopted, it's a per-line `closed` flag that pending and `lineOrdered` respect, plus a parity case with request rows.

**Gap 7. No change after approval: qty, rate or expected date [Owner]**
- **ERPs:** all three allow controlled changes. ERPNext Update Items, with guards (EN-PO:340-611). Odoo edits unless locked (`res_company.py:27`). Tryton amendment with a revision number (TR amendment scenario).
- **Ours:** R7 (cancel + amend if nothing received, else close + new PO).
- **Risk:** construction prices and quantities move often. Each change fragments the PO trail and moves request rows. The frozen `required_by` makes the Overdue report wrong once the vendor gives a new date (S31).
- **Recommendation:** keep R7 for qty and rate in v1, as the spec argues. For dates, add an "Expected on" record outside the PO (like the Sent record, §23, which already sits outside R7) that the pending report uses ahead of `required_by`. Small, and it removes the most common false "Overdue".

**Gap 8. The gate can't say "the rest isn't coming" [Owner]**
- **ERPs:** Odoo's no-backorder receipt writes a warning on the PO (OD-PSO:877).
- **Ours:** only a Purchase User can close.
- **Risk:** the buyer never learns, and the PO stays in Awaiting delivery.
- **Recommendation:** a GRN tick "Vendor says the rest won't come" that posts a comment on the PO (§22 comments exist) and puts it in a Purchase home "To close" queue. No state change.

**Gap 9. No hold [keep ours]**
- **ERPs:** ERPNext On Hold blocks receipts (EN-PO:861).
- **Ours:** none.
- **Risk:** low for this company.
- **Recommendation:** keep: a comment plus close covers it; hold adds a state to every test.

**Gap 10. Rejected quantity semantics [keep ours]**
- **ERPs:** ERPNext counts rejected as received until a return is made (`purchase_receipt.py:156-174`).
- **Ours:** CD-234 (accepted only, rejected stays pending), which matches Odoo and Tryton, where rejected goods are simply not received.
- **Recommendation:** keep ours: the truck takes them back, so pending must reopen. Add S35 (fully rejected GRN) as a case.

**Gap 11. PO cancel with receipts [keep ours]**
- **ERPs:** Odoo allows it and leaves receipts (OD-PROC:75). ERPNext blocks like us.
- **Recommendation:** keep: it keeps GRNs attached to a live PO.

**Gap 12. Self-approval [keep ours]**
- **ERPs:** Odoo lets a purchase manager approve their own PO (`purchase_order.py:1332-1340`).
- **Ours:** refuse (PO-022).
- **Recommendation:** keep: it is the control the owner asked for. The only hole is "below every rule" (PO §5), which is documented.

**Gap 13. E-way bill duties at receipt [Spec hint + CA]**
- **Law:** Rule 138(1)(iii) makes the recipient responsible for inward supply from an unregistered person above ₹50,000. Rule 138(11)-(12) gives the recipient 72 hours (or until delivery, whichever is earlier) to reject.
- **Ours:** optional field, no hint.
- **Recommendation:** information only, no refusal (CD-228 keeps tax judgement out). (a) On a GRN whose vendor has no GSTIN and accepted value above ₹50,000: "Unregistered vendor: the e-way bill may be ours to generate. Check with your CA." (b) On a fully rejected GRN with an e-way bill no.: "Reject the e-way bill on the portal within 72 hours if the goods went back."

**Gap 14. Site in a different state from its branch [Spec + CA]**
- **Law:** place of supply follows where the goods' movement ends, IGST s.10(1)(a).
- **Ours:** CD-234 assumes every site is in its branch's state, and nothing checks it.
- **Risk:** a Karnataka site billed to the Maharashtra branch gets the wrong tax split on every PO.
- **Recommendation:** have Site master validation (MST) refuse, or at least flag, a site whose state differs from its branch's. Ask the CA how such sites are billed. **Couldn't verify** s.10(1)(b) on CBIC.

**Gap 15. Test cases missing for rules the specs already state [Spec, no decision]**
- PO area: S14 (qty > 0), S15 (rate not negative), S16 (duplicate item, once decided), S18 (vendor disabled while pending, then approve fails), S05 (over-request warning text), S09 (rejected, withdrawn and deleted PO hold or free request rows), S03 (closing a PO linked to a closed request), S08 (split across vendors), S44 (closed PO leaves Overdue).
- GRN area: S33 (posted draft or pending PO refused with DOC's text), S35 (fully rejected GRN), S58 (draft GRN after PO close or cancel), S59 (GRN cancel on a short-closed PO updates request `ordered_qty`; §20.3 says MOD-GRN owns the call), S60 (amend refused on closed PO, or whatever Gap 2 decides), plus a cancel + amend case that moves bill details (Gap 1).

---

## 5. Sources

**Code (file:line at the commits in the header):** listed inline above.

**Documentation:**
- ERPNext Purchase Order manual: https://docs.frappe.io/erpnext/user/manual/en/purchase-order
- Odoo 18 bill control and 3-way matching (3-way match "only works with the Bill Control policy set to Received quantities"): https://www.odoo.com/documentation/18.0/applications/inventory_and_mrp/purchase/manage_deals/control_bills.html
- Oracle EBS receiving and blind receiving: https://docs.oracle.com/cd/E26401_01/doc.122/e48938/T555694T555724.htm, https://docs.oracle.com/cd/E26401_01/doc.122/e48969/T207713T208957.htm

**Law (CA to confirm):**
- CGST Rules 46, 55, 55A, 138, 138A: https://cbic-gst.gov.in/pdf/24092021-CGST-Rules-2017-Part-A-Rules.pdf (Rule 55 at pp. 63-64; Rule 138(1) at p. 139; 138(11)-(14) at p. 143; 138A at p. 145). Amended up to 24 Sep 2021 only.
- CGST Act s.16(2) incl. the lots proviso: https://taxinformation.cbic.gov.in/content/html/tax_repository/gst/acts/2017_CGST_act/active/chapter5/section16_v1.00.html
- CGST Act s.34 credit notes, 30 November deadline: https://taxinformation.cbic.gov.in/content/html/tax_repository/gst/acts/2017_CGST_act/active/chapter7/section34_v1.00.html
- CGST Act s.9(3)-(4) reverse charge: https://taxinformation.cbic.gov.in/content/html/tax_repository/gst/acts/2017_CGST_act/active/chapter3/section9_v1.00.html
- IGST s.10(1)(b): **couldn't verify on CBIC** (HTTP 500). Secondary: https://www.avalara.com/in/en/blog/2017/06/gst-place-supply-complexities-transaction-involves-3-parties.html

**Couldn't verify:** an over-receipt check in Odoo core (none found in the files read); ERPNext built-in amount-based PO approval (not in the files cloned); Rules 55 and 138 text after September 2021; IGST s.10 on the official site.
