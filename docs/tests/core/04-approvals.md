# Approvals (APR)

## Scope
APR owns approval rules (document type + amount threshold + approver), deciding at submit whether a document needs approval, who may approve it, the approve, reject and withdraw actions, revising a rejected document, the self-approval policy, the "Waiting for my approval" list and the append-only approval history. It drives the F5 transitions `draft → pending_approval`, `draft → approved` (no rule applies), `pending_approval → approved`, `pending_approval → rejected`, `pending_approval → draft` (withdraw, spec R2) and `rejected → draft` (revise).
It relies on DOC for the state machine itself, locking, validation and applying/reversing effects; on AUTH for users, roles, enabled/disabled users and rights (approve and reject need `approve`, revise needs `write`, withdraw needs `submit`; spec R4); on MST for employees and their reporting manager; on NUM for money comparison in paise; on NAM for the document number, which is assigned only inside the transaction that first enters `approved` (spec R1). Pending, rejected and revised documents have no number and show `Draft {id}`, e.g. `Draft 42`.
Single-level approval only in v1; multi-level chains, email, delegation and reminders are deferred.

## Decisions
| ID | Question | ERPNext/Frappe | Odoo | Decision | Why |
|---|---|---|---|---|---|
| D-APR-01 | Rule model | ERPNext Authorization Rule: transaction + based_on (Grand Total / discount) + value + approving role or user, optional company, applicable-to user/role/employee/designation (`erpnext/erpnext/setup/doctype/authorization_rule/authorization_rule.py:20-48`). Frappe Workflow: states + transitions, each transition has one allowed role and an optional Python condition (`frappe/frappe/model/workflow.py:105-109`). | PO: company-wide `po_double_validation` one_step/two_step + one amount; approver = Purchase Manager group (`odoo/addons/purchase/models/purchase_order.py:1332-1341`). Expense: approver is a person (expense manager → department manager → team leader) (`odoo/addons/hr_expense/models/hr_expense.py:1856-1871`). | An **approval rule** = document type + "from amount" (₹, inclusive) + approver. Approver is either a **role**, or (expense claims only) the **employee's reporting manager** with a required **fallback role**. No Python conditions, no per-user "applicable to" scoping, no discount rules. | Covers both real needs (PO by amount and role; expenses by manager) with one small table. Frappe-style free conditions are unsafe and untestable; ERPNext's applicable-to scoping is more than 20 people need. |
| D-APR-02 | When does an amount hit a threshold? | `value <= total` selects the rule, so total equal to the value needs approval (`erpnext/erpnext/setup/doctype/authorization_control/authorization_control.py:96-101`); the expense variant uses strict `value < total` (`...authorization_control.py:259`) — inconsistent. | No approval only when `amount_total < po_double_validation_amount` (`odoo/addons/purchase/models/purchase_order.py:1338-1340`), so equal needs approval. | Approval is needed when amount **≥ from amount**. Compared as integer paise. ₹49,999.99 under a ₹50,000.00 rule → no approval; ₹50,000.00 → approval. | ERPNext (main path) and Odoo agree; "₹50,000 and above" is how people say it. |
| D-APR-03 | Which amount? | PO uses `base_grand_total` (incl. taxes) (`erpnext/erpnext/buying/doctype/purchase_order/purchase_order.py:450-452`). | `amount_total` incl. taxes (`purchase_order.py:1338`). | **Grand total incl. GST, after round-off** for PO and other documents; **total claimed amount** for expense claims. | Both sources use the tax-inclusive total; that is the cash the company commits. |
| D-APR-04 | Several rules match | Takes the highest matching value; approvers of that rule **and of every higher rule** are authorised (`authorization_control.py:30-61`, `value = max or value > total`). | One threshold only. | The matching rule is the one with the **highest from amount ≤ document amount**. Eligible approvers = holders of that rule's role **plus** holders of the role of any higher rule for the same document type. | ERPNext behaviour; lets the Director approve a ₹60,000 PO even though the Purchase Manager rule matched. |
| D-APR-05 | No rule applies | No rule rows → check skipped (`authorization_control.py:158-160`); no workflow matches the doc → behaves as if none configured (`frappe/frappe/model/workflow.py:54-71`). | one_step or under threshold → confirm goes straight to `purchase` (`purchase_order.py:716-720`). | Submit goes straight `draft → approved` (F5). A rule with from amount ₹0.00 makes every document of that type need approval. | Fixed by F5; Odoo test uses amount 0 the same way (`odoo/addons/purchase/tests/test_access_rights.py:119-120`). |
| D-APR-06 | Self-approval | Workflow transition `allow_self_approval` defaults to **1**; owner blocked only when 0; Administrator always bypasses (`frappe/frappe/model/workflow.py:363-364`, `workflow_transition.json:63-68`). HRMS `prevent_self_expense_approval` defaults to 0 (`hrms/hrms/hr/doctype/expense_claim/expense_claim.py:207-217`). | Expense: "It is your own expense" for everyone except the expense admin (`hr_expense.py:1693-1694`). PO: a Purchase Manager's own PO is auto-approved at confirm (`purchase_order.py:716-718`). | **Not allowed by default.** Each rule has `allow_self_approval` (default **off**). "Own" = the user who created the document, the user who submitted it, or (expense) the claimant employee's user. No super-user bypass. When allowed, the requester still clicks Approve (no auto-approve at submit). | Safer default for money; sources disagree, so pick the safe one with an explicit per-rule escape for an owner-director. Explicit approve keeps F5 uniform and audited. |
| D-APR-07 | Expense approver | Employee `expense_approver` (enabled users only), else department approvers (`hrms/hrms/hr/doctype/department_approver/department_approver.py:72-77`). | expense_manager → department manager → team leader; **no approver → auto-approved** (`hr_expense.py:1395-1416`, test `test_expense_state_autovalidation`). | Rule approver "reporting manager": the user linked to the employee's reporting manager, if that user is enabled, can read the claim and isn't the claimant; otherwise holders of the rule's fallback role. Never auto-approve for lack of a manager. | Reporting manager is the normal Indian SME practice; Odoo's silent auto-approval is a money leak. |
| D-APR-08 | Nobody can approve | Frappe still enters the state; emails nobody. | Odoo auto-approves expense; PO stays `to approve`. | Submit is **refused** when no eligible approver exists at that moment: "No one else can approve this Purchase Order. Give the Purchase Manager role to another active user, then submit again." If approvers later disappear while pending, the document shows in an admin "Stuck approvals" list. | Fail early with a fix, rather than a document no one will ever see. |
| D-APR-09 | Snapshot vs live rule | Workflow Action stores permitted roles when the state is entered (`frappe/frappe/workflow/doctype/workflow_action/workflow_action.py:376-392`). | `manager_id` set at submit (`hr_expense.py:1401-1402`). | At submit, store an **approval request** with the rule id, amount, approver role(s) and (expense) the designated manager. Later rule edits don't change pending requests. **Who** holds a role and who is enabled is checked live at action time. | Predictable for the requester; still lets an admin fix a stuck request by giving a role. |
| D-APR-10 | Reject | Frappe: Reject is just another transition, no reason field. HRMS: `approval_status = Rejected` is a terminal submitted state, sanctioned amounts zeroed (`expense_claim.py:553-554`). | Expense refuse wizard, `reason` **required** (`odoo/addons/hr_expense/wizard/hr_expense_refuse_reason.py:12`); posted to chatter (`hr_expense.py:1816-1832`). | Reject needs a **reason** (trimmed, 1–1,000 chars) and the `approve` right. Document goes to `rejected` (locked, still no number). Requester or anyone with `write` right uses **Revise** to move it to `draft`. Reason stays in history and is shown on the document. | Odoo's required reason is what makes "re-submit" possible; F5 fixes rejected → draft. |
| D-APR-11 | Amount changes after rejection | Frappe re-evaluates transition conditions on the current doc (`frappe/frappe/model/workflow.py:105-109`); re-entering a state creates a fresh action (`test_workflow.py:143`). | PO back to draft then `button_confirm` re-checks `_approval_allowed` (`purchase_order.py:704-721`). | Re-submit **re-evaluates rules from scratch** on the new amount. Old approvals/rejections don't carry over. Below every threshold now → straight to `approved`. Higher → the higher rule's approver. | Both sources re-evaluate at submit; carrying old decisions would let an edited document skip review. |
| D-APR-12 | Withdraw while pending | No transition unless configured. | PO `button_draft` from `to approve` (`purchase_order.py:704-706`); Tryton `quotation → draft` (`tryton/modules/purchase/purchase.py:311`). | **Withdraw is in v1 (changed by spec R2).** `pending_approval → draft`, allowed for the submitter or anyone with `submit` right on the document who can see it. No reason needed. In one transaction: the open approval request is closed with outcome "withdrawn", a `withdrawn` history row and an audit entry (`pending_approval → draft`, action `withdraw`) are written, `version` goes up by 1. No number, no effects. A later submit re-evaluates rules and opens a new request. Refused from every other state. | Requesters need a way to pull back a document without asking an approver to reject it (spec R2). |
| D-APR-13 | Notifications | Workflow Action rows ("to do" list filtered by role, Open only) (`workflow_action.py:60-83`) + optional email to each eligible user (`workflow_action.py:94-131`, `:340-373`). | Mail activity scheduled for the approver (`hr_expense.py:1216-1230`). | **v1: in-app only** — a "Waiting for my approval" list with a count in the navigation, and "My requests" showing state and rejection reason. **Email later.** | In-app list covers a 20-person office; email needs SMTP setup, an outbox and retries (and no live updates in v1). |
| D-APR-14 | Levels | Frappe: any number of workflow states; ERPNext authorization: single. | PO: two_step only; expense: single. | **Single level in v1.** Rule table is designed so a later "level" column can add chains. | Two of three sources are single-level; enough for 20 people. |
| D-APR-15 | Approve re-validates? | Submit re-runs all validation; workflow submit goes through `doc.submit()` (`frappe/frappe/model/workflow.py:245-263`). | `_confirmation_error_message` at confirm (`purchase_order.py:751-760`). | Approve re-runs the document's DOC validation and applies effects **in the same transaction**. If anything fails, nothing changes and the document stays `pending_approval`. | Masters can be disabled or a PO over-received while a document waits. |
| D-APR-16 | Concurrent actions / stale page | Email link shows "link expired" when state moved on (`workflow_action.py:133-157`, `:202`). | — | Approve/Reject/Withdraw require the document to still be `pending_approval` (conditional `WHERE state = ?`); the second click gets "PO/26-27/00001 is no longer waiting for approval. It was approved by Ravi Kulkarni." (a stale withdraw gets the R2 message, see APR-095). | Safe and clear; matches DOC optimistic concurrency. |
| D-APR-17 | Approver needs read access | Recipients filtered by `has_permission` (`workflow_action.py:344-352`); HRMS **shares** the doc with the approver (`hrms/hrms/hr/utils.py:785-806`). | Team approver can't approve employees he doesn't manage (`test_expenses_access_rights.py:86`). | An eligible approver must also pass AUTH read rights on the document (incl. record-level rules). No automatic sharing. Order of checks for approve and reject (CD-52): DOC checks visibility first (a user who can't see the record gets 404, never an APR message), then APR's `CheckApprover` gives the approve-right, self-approval and eligibility messages, then DOC's from-state guard. A reporting manager gets read access to their reports' claims through AUTH's `team` scope (CD-11): the seeded Employee role has `read` and `approve` with scope `team` on Expense Claim, covering claims of employees who report directly to them. | No hidden permission side-effects. |
| D-APR-18 | Disabled users | Only enabled users get actions/emails (`frappe/frappe/utils/user.py:473-485`); HRMS lists only enabled approvers (`department_approver.py:72-77`). | — | Disabled users are never eligible approvers and can't act. A pending document from a requester who was later disabled can still be approved or rejected. | Matches sources; work must not get stuck because the requester left. |
| D-APR-19 | Rule maintenance | Duplicate rule blocked (`authorization_rule.py:51-72`); approver required (`:74-76`). | Settings fields. | Rules are edited by Administrators only, changes audited (F6). Duplicate (same document type + from amount) blocked. A rule used by any approval request is **disabled, not deleted** (F4 pattern). | Audit and history must keep resolving the rule. |
| D-APR-20 | Who/when recorded | Workflow comment per state (`frappe/frappe/model/workflow.py:265`). | `manager_id` = approver, `approval_date` (`hr_expense.py:1719-1730`); PO `date_approve` (`purchase_order.py:700`); Tryton `confirmed_by` (`tryton/modules/purchase/purchase.py:985`). | Append-only **approval history** per document: action (submitted/approved/rejected/revised/withdrawn), user, UTC time, amount, rule id, reason/comment. Document shows "Approved by … on …" in Asia/Kolkata time, in the one timestamp format `24-10-2026 14:45` (`types.FormatIST`, CD-33). | Needed for audit and print; F6/F7. |

## Test cases

### Rules: setup and validation

### APR-001 · An Administrator creates an amount rule for Purchase Order
- **Priority:** P1
- **Level:** integration
- **Given** Anil (role Administrator) and no Purchase Order rules (the seed PO rules are removed in this test) **When** he saves a rule {document type: Purchase Order, from amount: ₹50,000.00, approver: role Purchase Manager, allow self approval: off} **Then** the rule is saved enabled, the from amount is stored as 5000000 paise, and an audit entry "approval rule created" records Anil and the values.
- **Go test:** `TestApprovalRule_CreateAmountRule`
- **Sources:** `erpnext/erpnext/setup/doctype/authorization_rule/test_authorization_rule.py:10-20` (`make_rule`), `odoo/addons/purchase/tests/test_create_picking.py:115` (`test_01_check_double_validation`)

### APR-002 · Only Administrators can create, edit or disable rules
- **Priority:** P1
- **Level:** integration
- **Given** Ravi has role Purchase Manager but not Administrator **When** he tries to create or edit an approval rule **Then** it is refused (403) with "You don't have permission to change approval rules. Ask an Administrator." ("Administrator", never "Admin", CD-66) and nothing is saved.
- **Go test:** `TestApprovalRule_OnlyAdminCanEdit`
- **Sources:** `odoo/addons/purchase/models/purchase_order.py:1332-1341` (company setting), `erpnext/erpnext/setup/doctype/authorization_rule/authorization_rule.py:11`

### APR-003 · From amount must be ₹0.00 or more
- **Priority:** P1
- **Level:** unit
- **Given** a new Purchase Order rule **When** from amount is −₹1.00 **Then** save fails with "From amount must be ₹0.00 or more." ; with ₹0.00 it saves.
- **Go test:** `TestApprovalRule_FromAmountNotNegative`
- **Sources:** `erpnext/erpnext/setup/doctype/authorization_rule/test_authorization_rule.py:43` (`test_discount_limits_are_within_percent_range`), `erpnext/erpnext/setup/doctype/authorization_rule/authorization_rule.py:121-122`

### APR-004 · From amount accepts at most 2 decimals
- **Priority:** P2
- **Level:** unit
- **Given** a rule form **When** from amount "50000.005" is entered **Then** save fails with "Amount can have at most 2 decimal places." (NUM money parsing).
- **Go test:** `TestApprovalRule_FromAmountPrecision`
- **Sources:** `erpnext/erpnext/setup/doctype/authorization_control/authorization_control.py:29` (flt), NUM

### APR-005 · A role rule must name an approver role
- **Priority:** P1
- **Level:** unit
- **Given** a Purchase Order rule with approver type "role" **When** the role is empty **Then** save fails with "Choose an approver role for this rule."
- **Go test:** `TestApprovalRule_RoleRequired`
- **Sources:** `erpnext/erpnext/setup/doctype/authorization_rule/authorization_rule.py:75-76` (`Please enter Approving Role or Approving User`)

### APR-006 · Approver role must exist and be enabled
- **Priority:** P2
- **Level:** unit
- **Given** a new role `Site Auditor` created in this test and then disabled **When** a rule names it **Then** save fails with "Role Site Auditor is disabled. Choose an active role."
- **Go test:** `TestApprovalRule_RoleMustBeActive`
- **Sources:** `erpnext/erpnext/setup/doctype/authorization_rule/test_authorization_rule.py:62` (`test_master_type_is_derived_from_basis`, link validation)

### APR-007 · Reporting-manager approver needs a fallback role
- **Priority:** P1
- **Level:** unit
- **Given** an Expense Claim rule with approver type "reporting manager" **When** fallback role is empty **Then** save fails with "Choose a fallback role for claims whose employee has no reporting manager."
- **Go test:** `TestApprovalRule_ManagerNeedsFallbackRole`
- **Sources:** `hrms/hrms/hr/doctype/department_approver/department_approver.py:32-77` (`get_approvers`), `odoo/addons/hr_expense/models/hr_expense.py:1856-1871` (`_get_default_responsible_for_approval`)

### APR-008 · Reporting-manager approver is allowed only on Expense Claim
- **Priority:** P2
- **Level:** unit
- **Given** a Purchase Order rule **When** approver type is "reporting manager" **Then** save fails with "Reporting manager approval is only available for Expense Claim. Choose a role."
- **Go test:** `TestApprovalRule_ManagerOnlyForExpenses`
- **Sources:** `erpnext/erpnext/setup/doctype/authorization_rule/authorization_rule.py:81-92` (basis not allowed for transaction)

### APR-009 · Rules can only be set on document types, not masters
- **Priority:** P1
- **Level:** unit
- **Given** the rule form **When** document type "Item" is chosen **Then** it is refused with "Item is a master and can't need approval. Choose a document type." The allowed list in v1 is exactly the DocTypes marked approvable (Purchase Order, Goods Receipt Note, Expense Claim).
- **Go test:** `TestApprovalRule_OnlyApprovableDocTypes`
- **Sources:** `frappe/frappe/workflow/doctype/workflow/workflow.py:175-186` (non-submittable doctype check), `erpnext/erpnext/setup/doctype/authorization_rule/authorization_rule.py:38-47` (transaction list)

### APR-010 · Duplicate rule (same document type and from amount) is blocked
- **Priority:** P1
- **Level:** integration
- **Given** the seed rule R1 Purchase Order from ₹50,000.00 **When** another Purchase Order rule from ₹50,000.00 is saved (any approver) **Then** it fails with "A Purchase Order rule from ₹50,000.00 already exists. Edit that rule instead." A rule from ₹2,00,000.00 saves fine.
- **Go test:** `TestApprovalRule_DuplicateBlocked`
- **Sources:** `erpnext/erpnext/setup/doctype/authorization_rule/test_authorization_rule.py:22` (`test_duplicate_rule_is_blocked`), `:26` (`test_rules_with_distinct_scope_or_limit_are_allowed`), `frappe/frappe/workflow/doctype/workflow/test_workflow.py:706` (`test_overlapping_conditions_are_rejected_at_the_same_priority`)

### APR-011 · A disabled rule doesn't count as a duplicate and doesn't apply
- **Priority:** P2
- **Level:** integration
- **Given** the seed rule R1 (Purchase Order from ₹50,000.00 → Purchase Manager) is disabled **When** a new enabled rule from ₹50,000.00 → Director is saved, and a ₹60,000.00 PO is submitted **Then** the save succeeds and only the new enabled rule is used.
- **Go test:** `TestApprovalRule_DisabledIgnored`
- **Sources:** `frappe/frappe/workflow/doctype/workflow/test_workflow.py:755` (`test_inactive_peers_do_not_block_a_workflow`), `:762` (`test_reactivating_a_workflow_rechecks_its_peers`)

### APR-012 · Re-enabling a rule re-checks duplicates
- **Priority:** P2
- **Level:** integration
- **Given** enabled seed rule R1 and a disabled rule B, both Purchase Order from ₹50,000.00 **When** B is enabled **Then** it fails with "A Purchase Order rule from ₹50,000.00 already exists. Edit that rule instead."
- **Go test:** `TestApprovalRule_ReenableRechecksDuplicate`
- **Sources:** `frappe/frappe/workflow/doctype/workflow/test_workflow.py:762` (`test_reactivating_a_workflow_rechecks_its_peers`)

### APR-013 · A used rule can be disabled but not deleted
- **Priority:** P1
- **Level:** integration
- **Given** rule R1 has been applied to 3 approval requests **When** Anil (Administrator) deletes R1 **Then** it fails with "This rule has been used by 3 documents. Disable it instead of deleting." Disabling succeeds. A never-used rule can be deleted.
- **Go test:** `TestApprovalRule_UsedRuleDisableNotDelete`
- **Sources:** F4; `frappe/frappe/workflow/doctype/workflow/test_workflow.py:674` (`test_deleting_a_workflow_drops_it_from_the_cache`)

### APR-014 · Rule changes are audited with before/after values
- **Priority:** P2
- **Level:** integration
- **Given** rule R1 from ₹50,000.00 **When** Anil changes it to ₹75,000.00 **Then** the audit trail has one entry: user Anil, field from_amount, before 5000000, after 7500000.
- **Go test:** `TestApprovalRule_ChangesAudited`
- **Sources:** F6; `frappe/frappe/model/workflow.py:265` (workflow comment)

### Rules: matching at submit

### APR-015 · No rule for the document type → submit goes straight to approved
- **Priority:** P1
- **Level:** integration
- **Given** no rule exists for Goods Receipt Note (seed) and the GRN counter for FY 2026-27 is unused **When** Suresh submits GRN Draft 43 for ₹1,20,000.00 **Then** the state goes `draft → approved` in one step, the GRN gets number `GR/26-27/00001` in that transaction, effects are applied, no approval request is created, and history records "submitted" and "approved (no approval needed)" by Suresh.
- **Go test:** `TestApprovalSubmit_NoRuleApprovesDirectly`
- **Sources:** `erpnext/erpnext/setup/doctype/authorization_control/authorization_control.py:158-160`, `frappe/frappe/workflow/doctype/workflow/test_workflow.py:83` (`test_deleting_workflow_clears_cached_name`), `odoo/addons/purchase/models/purchase_order.py:716-718`

### APR-016 · Amount below the lowest threshold → straight to approved
- **Priority:** P1
- **Level:** integration
- **Given** seed rule R1 from ₹50,000.00 and an unused PO counter for FY 2026-27 **When** Priya submits PO Draft 41 with grand total ₹49,999.99 **Then** the state is `approved`, it gets number `PO/26-27/00001`, and no request is created.
- **Go test:** `TestApprovalSubmit_BelowThresholdApproved`
- **Sources:** `odoo/addons/purchase/models/purchase_order.py:1336-1340` (`_approval_allowed`), `frappe/frappe/workflow/doctype/workflow/test_workflow.py:594` (`test_conditional_workflow_skips_documents_it_does_not_match`)

### APR-017 · Amount exactly at the threshold needs approval
- **Priority:** P1
- **Level:** unit
- **Given** rule R1 from ₹50,000.00 **When** a PO with grand total ₹50,000.00 is submitted **Then** state is `pending_approval` and the request points at R1.
- **Go test:** `TestApprovalMatch_EqualToThresholdNeedsApproval`
- **Sources:** `erpnext/erpnext/setup/doctype/authorization_control/test_authorization_control.py:125` (`test_item_discount_approval_with_margin_and_exact_threshold`), `erpnext/erpnext/setup/doctype/authorization_control/authorization_control.py:96-101`, `odoo/addons/purchase/models/purchase_order.py:1338`

### APR-018 · Threshold comparison is exact in paise (table)
- **Priority:** P1
- **Level:** unit
- **Given** rule from ₹50,000.00 **When** amounts {₹49,999.99, ₹50,000.00, ₹50,000.01, ₹0.00} are checked **Then** needs approval = {false, true, true, false}. With a rule from ₹0.00, ₹0.00 → true.
- **Go test:** `TestApprovalMatch_ThresholdTable`
- **Sources:** `odoo/addons/purchase/models/purchase_order.py:1338-1340`; NUM paise arithmetic

### APR-019 · Threshold uses grand total including GST after round-off
- **Priority:** P1
- **Level:** integration
- **Given** seed rule R1 from ₹50,000.00 and a PO from `BHARAT` to branch PUNE (inter-state) with net ₹42,400.00 + IGST 18% ₹7,632.00 = ₹50,032.00, rounded to ₹50,032.00 **When** submitted **Then** it is `pending_approval` (net amount alone would be below the threshold). The request stores amount 5003200 paise.
- **Go test:** `TestApprovalMatch_UsesGrandTotalWithGST`
- **Sources:** `erpnext/erpnext/buying/doctype/purchase_order/purchase_order.py:450-452` (`base_grand_total`), `odoo/addons/purchase/models/purchase_order.py:1338` (`amount_total`)

### APR-020 · Expense claim uses total claimed amount
- **Priority:** P1
- **Level:** integration
- **Given** seed rule E1 (Expense Claim from ₹0.00 → reporting manager, fallback Director) plus a rule E2 Expense Claim from ₹25,000.00 → role Director added in this test, and Deepak's claim with lines ₹18,000.00 + ₹7,000.00 **When** submitted **Then** amount used is ₹25,000.00 and the claim is `pending_approval` under E2.
- **Go test:** `TestApprovalMatch_ExpenseUsesClaimTotal`
- **Sources:** `erpnext/erpnext/setup/doctype/authorization_control/authorization_control.py:244-263` (`get_value_based_rule`, total_claimed_amount)

### APR-021 · Highest matching threshold wins
- **Priority:** P1
- **Level:** unit
- **Given** seed rules R1 PO from ₹50,000.00 → Purchase Manager and R2 PO from ₹5,00,000.00 → Director **When** POs of ₹60,000.00 and ₹6,00,000.00 are submitted **Then** the ₹60,000 request uses R1 and the ₹6,00,000 request uses R2.
- **Go test:** `TestApprovalMatch_HighestThresholdWins`
- **Sources:** `erpnext/erpnext/setup/doctype/authorization_control/authorization_control.py:28-30` (max amount), `frappe/frappe/workflow/doctype/workflow/test_workflow.py:613` (`test_highest_priority_matching_workflow_wins`)

### APR-022 · Rules for other document types are ignored
- **Priority:** P1
- **Level:** unit
- **Given** the seed PO rules are removed in this test, so only the Expense Claim rule from ₹0.00 exists **When** a ₹10,00,000.00 PO is submitted **Then** it goes straight to `approved`.
- **Go test:** `TestApprovalMatch_OtherDocTypeIgnored`
- **Sources:** `erpnext/erpnext/setup/doctype/authorization_control/test_authorization_control.py:79` (`test_supplier_quotation_update_with_unrelated_rule`)

### APR-023 · A ₹0.00 rule sends every document of the type for approval
- **Priority:** P1
- **Level:** integration
- **Given** a PO rule from ₹0.00 → Purchase Manager added in this test **When** a ₹1,500.00 PO is submitted **Then** it is `pending_approval`.
- **Go test:** `TestApprovalMatch_ZeroThresholdAlwaysApplies`
- **Sources:** `odoo/addons/purchase/tests/test_access_rights.py:104` (`test_double_validation`, amount 0)

### APR-024 · Rules are evaluated at submit, never at save
- **Priority:** P1
- **Level:** integration
- **Given** seed rule R1 from ₹50,000.00 **When** a draft PO of ₹80,000.00 is saved several times **Then** no approval request exists, no number is assigned (it shows `Draft 42`), and state stays `draft` until Submit.
- **Go test:** `TestApprovalMatch_OnlyAtSubmit`
- **Sources:** `frappe/frappe/workflow/doctype/workflow/test_workflow.py:175` (`test_workflow_action_not_duplicated_on_resave`), `odoo/addons/purchase/models/purchase_order.py:708-721`

### APR-025 · The request snapshots the rule at submit
- **Priority:** P1
- **Level:** integration
- **Given** Priya's PO Draft 42 (₹60,000.00) is pending under R1 (Purchase Manager) **When** Anil (Administrator) changes R1's role to Director, or raises R1 to ₹75,000.00 **Then** Draft 42 still waits for a Purchase Manager; only documents submitted afterwards use the new rule.
- **Go test:** `TestApprovalRequest_SnapshotsRule`
- **Sources:** `frappe/frappe/workflow/doctype/workflow_action/workflow_action.py:376-392` (`create_workflow_actions_for_roles`), `frappe/frappe/workflow/doctype/workflow/test_workflow.py:662` (`test_renaming_a_state_leaves_documents_in_it_alone`), `odoo/addons/hr_expense/models/hr_expense.py:1401-1402`

### APR-026 · Disabling a rule doesn't release pending documents
- **Priority:** P2
- **Level:** integration
- **Given** Draft 42 pending under R1 **When** R1 is disabled **Then** Draft 42 stays `pending_approval` and can still be approved or rejected by a Purchase Manager (Ravi).
- **Go test:** `TestApprovalRequest_DisabledRuleKeepsPending`
- **Sources:** `frappe/frappe/workflow/doctype/workflow/test_workflow.py:662` (`test_renaming_a_state_leaves_documents_in_it_alone`)

### APR-027 · Submit locks the document but gives it no number
- **Priority:** P1
- **Level:** integration
- **Given** Priya's draft PO Draft 42 with grand total ₹60,000.00 **When** Priya submits on 2026-10-24 **Then** state is `pending_approval`, no number is assigned (NAM is not called; it still shows `Draft 42`), and any edit is refused with "Draft 42 is waiting for approval and can't be changed. Withdraw it to make changes."
- **Go test:** `TestApprovalSubmit_LocksWithoutNumber`
- **Sources:** `tryton/modules/purchase/purchase.py:968-974` (`quote` sets number), `odoo/addons/hr_expense/tests/test_expenses.py:630` (`test_corner_case_expense_submitted_cannot_be_zero`, case 2), `frappe/frappe/model/workflow.py:336-355`

### APR-028 · Submit validates the document first
- **Priority:** P1
- **Level:** integration
- **Given** a PO with a line whose quantity is 0 **When** submitted **Then** submit fails with DOC's "Quantity must be more than 0." and no approval request is created.
- **Go test:** `TestApprovalSubmit_ValidationBeforeRules`
- **Sources:** `odoo/addons/purchase/models/purchase_order.py:712-714` (`_confirmation_error_message`), `odoo/addons/hr_expense/tests/test_expenses.py:630` (case 1)

### Approver eligibility and self-approval

### APR-029 · Holder of the rule's role can approve
- **Priority:** P1
- **Level:** integration
- **Given** Priya's Draft 42 (₹60,000.00) pending under R1, the PO counter for FY 2026-27 unused, and Ravi has role Purchase Manager and read access **When** Ravi approves **Then** state is `approved`, the PO gets number `PO/26-27/00001` in the same transaction, and history records "approved" by Ravi.
- **Go test:** `TestApprovalApprove_RoleHolderCanApprove`
- **Sources:** `odoo/addons/purchase/tests/test_create_picking.py:112` (`test_01_check_double_validation`), `frappe/frappe/workflow/doctype/workflow/test_workflow.py:55` (`test_approve`)

### APR-030 · User without the role cannot approve
- **Priority:** P1
- **Level:** integration
- **Given** Draft 43 (₹60,000.00, project PRJ-METRO) created and submitted by Ravi (Ravi is given the Purchase User role in this test, because the seeded Purchase Manager role has no `create` or `submit` right on Purchase Order; see 01-auth.md), pending under R1, and Priya (role Purchase User only, can read it) **When** Priya approves **Then** it fails (403) with "Only a user with the Purchase Manager or Director role can approve Draft 43 (₹60,000.00)." and state stays `pending_approval`. (Priya can see the PO, so DOC's visibility check passes and APR's `CheckApprover` gives the message; DOC runs no generic `approve` check for approve or reject, CD-52.)
- **Go test:** `TestApprovalApprove_NonApproverRefused`
- **Sources:** `odoo/addons/purchase/tests/test_access_rights.py:104` (`test_double_validation`), `erpnext/erpnext/setup/doctype/authorization_control/test_authorization_control.py:249` (`test_validate_approving_authority_raises_when_over_limit`), `erpnext/erpnext/setup/doctype/authorization_control/authorization_control.py:57-61`

### APR-031 · Odoo-style silent skip is not used — a refused approve always errors
- **Priority:** P2
- **Level:** unit
- **Given** a non-approver calls Approve **When** the call returns **Then** it returns an error (not success with no change). (Odoo `button_approve` silently filters out orders the user may not approve.)
- **Go test:** `TestApprovalApprove_RefusalIsAnError`
- **Sources:** `odoo/addons/purchase/models/purchase_order.py:698-700`, `odoo/addons/purchase/tests/test_access_rights.py:128-129`

### APR-032 · Higher-tier approver can approve a lower-tier request
- **Priority:** P1
- **Level:** integration
- **Given** seed R1 (₹50,000.00 → Purchase Manager), R2 (₹5,00,000.00 → Director), Priya's Draft 42 of ₹60,000.00 under R1 **When** Meena (Director only) approves **Then** it succeeds.
- **Go test:** `TestApprovalApprove_HigherTierCanApprove`
- **Sources:** `erpnext/erpnext/setup/doctype/authorization_control/authorization_control.py:32-39` (`value = max or value > total`)

### APR-033 · Lower-tier approver cannot approve a higher-tier request
- **Priority:** P1
- **Level:** integration
- **Given** Priya's Draft 50 of ₹6,00,000.00 under R2 **When** Ravi (Purchase Manager, not Director) approves **Then** it fails with "Only a user with the Director role can approve Draft 50 (₹6,00,000.00)."
- **Go test:** `TestApprovalApprove_LowerTierRefused`
- **Sources:** `erpnext/erpnext/setup/doctype/authorization_control/authorization_control.py:57-61` (`Not authorized since {0} exceeds limits`)

### APR-034 · Approver must also be able to read the document
- **Priority:** P1
- **Level:** integration
- **Given** Ravi has Purchase Manager but an AUTH restriction added in this test limits him to project PRJ-TECHPARK, and Priya's Draft 42 is for project PRJ-METRO **When** he approves or rejects **Then** it is refused as not found (404, `doctype.ErrNotFound`, the same answer as for a document that doesn't exist; CD-52, CD-54): DOC's visibility check answers before APR is called, so no APR message is shown. Draft 42 stays `pending_approval` and is not in his "Waiting for my approval" list.
- **Go test:** `TestApprovalApprove_NeedsReadAccess`
- **Sources:** `frappe/frappe/workflow/doctype/workflow_action/workflow_action.py:344-352` (`user_has_permission`), `odoo/addons/hr_expense/tests/test_expenses_access_rights.py:86` (`test_expense_team_approver_cant_approve_expense_of_employee_he_does_not_manage`)

### APR-035 · Requester cannot approve their own document
- **Priority:** P1
- **Level:** integration
- **Given** R1 with allow self approval off, and Ravi (Purchase Manager) created and submitted Draft 43 for ₹70,000.00 (Ravi is given the Purchase User role in this test, because the seeded Purchase Manager role has no `create` or `submit` right on Purchase Order; see 01-auth.md) **When** Ravi approves **Then** it fails with "You can't approve your own Purchase Order. Ask another Purchase Manager or Director to approve it."
- **Go test:** `TestApprovalSelf_RequesterBlocked`
- **Sources:** `frappe/frappe/model/workflow.py:173-174` (`Self approval is not allowed`), `odoo/addons/hr_expense/tests/test_expenses_access_rights.py:74` (`test_expense_user_cant_approve_own_expense`), `hrms/hrms/hr/doctype/expense_claim/test_expense_claim.py:988` (`test_self_expense_approval_not_allowed`)

### APR-036 · "Own" covers creator and submitter separately
- **Priority:** P1
- **Level:** unit
- **Given** Priya created Draft 44 and Ravi (Purchase Manager) submitted it **When** Ravi approves **Then** it is refused as self-approval; **and** for Draft 45 created by Ravi and submitted by Priya, Ravi is also refused. Ravi is given the Purchase User role in this test, because the seeded Purchase Manager role has no `create` or `submit` right on Purchase Order (see 01-auth.md). Meena (Director) may approve both.
- **Go test:** `TestApprovalSelf_CreatorOrSubmitter`
- **Sources:** `frappe/frappe/model/workflow.py:363-364` (`has_approval_access`, owner), `odoo/addons/purchase/models/purchase_order.py:1332-1341`

### APR-037 · Claimant can't approve their own expense even if someone else entered it
- **Priority:** P1
- **Level:** integration
- **Given** Anil (Administrator) entered Expense Claim Draft 12 for employee EMP-003 Ravi (user ravi), whose reporting manager is Meena, and in this test Ravi is also given the Director role (the rule's fallback role) **When** Ravi approves **Then** it fails with "You can't approve your own Expense Claim. Ask another Director to approve it."
- **Go test:** `TestApprovalSelf_ClaimantBlocked`
- **Sources:** `odoo/addons/hr_expense/models/hr_expense.py:1693-1694` (`It is your own expense`), `hrms/hrms/hr/doctype/expense_claim/expense_claim.py:207-217` (employee user vs session user)

### APR-038 · Per-rule allow-self-approval lets the requester approve explicitly
- **Priority:** P2
- **Level:** integration
- **Given** R2 (₹5,00,000.00 → Director) has allow self approval on, and Meena (Director, the only Director) submits Draft 50 for ₹6,00,000.00 **When** submit runs **Then** state is `pending_approval` (not auto-approved) and there is no number; **When** Meena approves **Then** it succeeds, the PO gets `PO/26-27/00001` (PO counter unused), and history marks the entry "self-approved".
- **Go test:** `TestApprovalSelf_AllowedByRule`
- **Sources:** `frappe/frappe/workflow/doctype/workflow_transition/workflow_transition.json:63-68` (`allow_self_approval`), `hrms/hrms/hr/doctype/expense_claim/test_expense_claim.py:960` (`test_self_expense_approval`)

### APR-039 · No super-user bypass of self-approval
- **Priority:** P1
- **Level:** integration
- **Given** Anil has role Administrator and, in this test, also Purchase Manager; R1 allow self approval off; Anil submitted Draft 46 **When** Anil approves **Then** it is refused as self-approval. (Frappe lets Administrator bypass; we don't.)
- **Go test:** `TestApprovalSelf_AdminNotExempt`
- **Sources:** `frappe/frappe/model/workflow.py:363-364`

### APR-040 · Submit is refused when no one else could approve
- **Priority:** P1
- **Level:** integration
- **Given** R1 → Purchase Manager, allow self approval off, Ravi is the only enabled Purchase Manager and the Director role is removed from Meena in this test (so no Director exists) (Ravi is given the Purchase User role in this test, because the seeded Purchase Manager role has no `create` or `submit` right on Purchase Order; see 01-auth.md) **When** Ravi submits a ₹70,000.00 PO **Then** submit fails with "No one else can approve this Purchase Order. Give the Purchase Manager role to another active user, then submit again." and the PO stays `draft`.
- **Go test:** `TestApprovalSubmit_NoEligibleApproverRefused`
- **Sources:** `odoo/addons/hr_expense/tests/test_expenses_states.py:196` (`test_expense_state_autovalidation`, the behaviour we reject), `frappe/frappe/workflow/doctype/workflow_action/workflow_action.py:340-373`

### APR-041 · Disabled users are not eligible approvers
- **Priority:** P1
- **Level:** integration
- **Given** Ravi (the only Purchase Manager) is disabled in this test and Meena (Director) is enabled **When** eligible approvers for Priya's Draft 42 are computed **Then** the list is [Meena]; a disabled user's session can't approve anything (AUTH).
- **Go test:** `TestApprovalEligible_ExcludesDisabled`
- **Sources:** `frappe/frappe/utils/user.py:473-485` (`get_users_with_role`, enabled = 1), `hrms/hrms/hr/doctype/department_approver/department_approver.py:72-77`

### APR-042 · Eligibility is checked live at action time
- **Priority:** P1
- **Level:** integration
- **Given** Priya's Draft 42 pending under R1 and Kavya did not have Purchase Manager at submit **When** Anil (Administrator) gives Kavya Purchase Manager and Kavya approves **Then** it succeeds. **When** instead Ravi's Purchase Manager role is removed before he clicks approve **Then** his approve fails with the not-eligible message.
- **Go test:** `TestApprovalEligible_LiveRoleCheck`
- **Sources:** `frappe/frappe/model/workflow.py:102-109` (`get_transitions` uses current roles), `frappe/frappe/workflow/doctype/workflow/test_workflow.py:121` (`test_if_workflow_actions_were_processed_using_role`)

### APR-043 · Stuck approvals list for Administrators
- **Priority:** P2
- **Level:** integration
- **Given** Priya's Draft 42 pending under R1, and Ravi and Meena are disabled in this test (so every Purchase Manager and Director is disabled) **When** Anil (Administrator) opens "Stuck approvals" **Then** Draft 42 is listed with "No active approver: needs Purchase Manager or Director." It drops off once an eligible user exists.
- **Go test:** `TestApprovalStuck_ListedForAdmin`
- **Sources:** `frappe/frappe/workflow/doctype/workflow_action/workflow_action.py:340-373`

### Expense claims: reporting manager

### APR-044 · Reporting manager is the approver for an expense claim
- **Priority:** P1
- **Level:** integration
- **Given** seed rule E1 Expense Claim from ₹0.00 → reporting manager, fallback Director; employee Deepak reports to Priya (user priya, enabled) **When** Deepak submits Expense Claim Draft 10 for ₹3,450.00 **Then** it is `pending_approval`, the request records designated approver Priya, and Priya can approve. Priya reads and approves Deepak's claim through the seeded Employee `team` scope (CD-11); no extra right is added in this test.
- **Go test:** `TestApprovalExpense_ReportingManagerApproves`
- **Sources:** `odoo/addons/hr_expense/models/hr_expense.py:1856-1871` (`_get_default_responsible_for_approval`), `hrms/hrms/hr/doctype/expense_claim/test_expense_claim.py:637` (`test_expense_approver_perms`)

### APR-045 · Another employee's manager can't approve
- **Priority:** P1
- **Level:** integration
- **Given** Deepak's Draft 10 designated to Priya, and Ravi manages Priya and Suresh but not Deepak, and has no Director role **When** Ravi approves **Then** it is refused as not found (404, `doctype.ErrNotFound`; CD-52): his `team` scope covers only claims of Priya and Suresh, so he can't see Draft 10, DOC's visibility check answers before APR is called, and Draft 10 is not in his "Waiting for my approval" list. **When** Anil (Administrator: he can see every claim and holds `approve`, but is neither Priya nor a Director) approves **Then** it fails with "Only Priya Shah or a user with the Director role can approve Draft 10 (₹3,450.00)." That message is given only to a user who can see the claim but isn't eligible. Draft 10 stays `pending_approval`.
- **Go test:** `TestApprovalExpense_OtherManagerRefused`
- **Sources:** `odoo/addons/hr_expense/tests/test_expenses_access_rights.py:86`, `odoo/addons/hr_expense/models/hr_expense.py:1696-1697` (`It is not from your department`)

### APR-046 · Fallback role holders may also approve a reporting-manager claim
- **Priority:** P2
- **Level:** integration
- **Given** Deepak's Draft 10 designated to Priya **When** Meena (Director, the fallback role) approves **Then** it succeeds.
- **Go test:** `TestApprovalExpense_FallbackRoleCanApprove`
- **Sources:** `odoo/addons/hr_expense/models/hr_expense.py:1653` (`is_approver`), `:1696` (approvers may act outside their team), `hrms/hrms/hr/doctype/department_approver/department_approver.py:32-77`

### APR-047 · No reporting manager → fallback role
- **Priority:** P1
- **Level:** integration
- **Given** employee Anil (EMP-001) has no reporting manager **When** he submits a ₹2,000.00 claim **Then** it is `pending_approval` with no designated user and approver role Director.
- **Go test:** `TestApprovalExpense_NoManagerUsesFallback`
- **Sources:** `odoo/addons/hr_expense/models/hr_expense.py:1856-1871`, `odoo/addons/hr_expense/tests/test_expenses_states.py:196` (`test_expense_state_autovalidation`, we don't auto-approve)

### APR-048 · Manager disabled or without a user → fallback role
- **Priority:** P1
- **Level:** integration
- **Given** Deepak's reporting manager Priya is disabled in this test (or the manager employee has no linked user) **When** Deepak submits **Then** the request uses fallback role Director with no designated user.
- **Go test:** `TestApprovalExpense_DisabledManagerUsesFallback`
- **Sources:** `hrms/hrms/hr/doctype/department_approver/department_approver.py:72-77` (enabled = 1)

### APR-049 · Manager disabled after submit → fallback holders can act
- **Priority:** P1
- **Level:** integration
- **Given** Deepak's Draft 10 designated to Priya **When** Priya is disabled and Meena (Director) approves **Then** it succeeds; the claim moves from Priya's list to the Director list.
- **Go test:** `TestApprovalExpense_ManagerDisabledWhilePending`
- **Sources:** `frappe/frappe/utils/user.py:473-485`

### APR-050 · Employee who manages themselves falls back
- **Priority:** P2
- **Level:** unit
- **Given** employee Kavya's reporting manager is set to Kavya in this test (bad data) **When** she submits a claim **Then** the designated user is not Kavya; approver role Director is used.
- **Go test:** `TestApprovalExpense_SelfManagerFallsBack`
- **Sources:** `odoo/addons/hr_expense/models/hr_expense.py:1861-1865` (`expense_manager - employee.user_id`)

### APR-051 · Higher expense rule overrides the manager
- **Priority:** P1
- **Level:** integration
- **Given** seed E1 (₹0.00 → reporting manager) and E2 (₹25,000.00 → Director, added in this test) **When** Deepak submits ₹30,000.00 **Then** the request uses E2 and Priya (his manager, not Director) cannot approve it.
- **Go test:** `TestApprovalExpense_HighAmountGoesToRole`
- **Sources:** `erpnext/erpnext/setup/doctype/authorization_control/authorization_control.py:244-321` (`get_value_based_rule`, highest value)

### Approve action

### APR-052 · Approve applies effects in the same transaction
- **Priority:** P1
- **Level:** integration
- **Given** a GRN rule from ₹0.00 → Purchase Manager added in this test, and Suresh's GRN Draft 7 (₹1,20,000.00, 50 Nos of BOLT-M12 against approved `PO/26-27/00001`) is pending **When** Ravi approves **Then** state is `approved`, the GRN gets `GR/26-27/00001`, and the PO line's received quantity rises by 50 in the same transaction; history records approver Ravi, UTC time and amount ₹1,20,000.00.
- **Go test:** `TestApprovalApprove_AppliesEffectsAtomically`
- **Sources:** `frappe/frappe/model/workflow.py:245-263` (submit on transition), `odoo/addons/hr_expense/models/hr_expense.py:1719-1730` (`_do_approve`)

### APR-053 · Approve re-runs validation; failure leaves it pending
- **Priority:** P1
- **Level:** integration
- **Given** GRN Draft 7 waits for approval and meanwhile another GRN received the remaining PO quantity **When** Ravi approves **Then** it fails with DOC's over-receipt message, nothing changes (no number is used), state stays `pending_approval`, and no "approved" history row is written.
- **Go test:** `TestApprovalApprove_RevalidatesAndRollsBack`
- **Sources:** `frappe/frappe/model/workflow.py:242-263`, `odoo/addons/purchase/models/purchase_order.py:708-721`

### APR-054 · Approve with an optional comment
- **Priority:** P2
- **Level:** integration
- **Given** Priya's Draft 42 pending **When** Ravi approves with comment "Rate checked against last PO." **Then** the comment is kept in the history row; approving with no comment also works.
- **Go test:** `TestApprovalApprove_OptionalComment`
- **Sources:** `frappe/frappe/model/workflow.py:265` (`add_comment`)

### APR-055 · Only pending documents can be approved
- **Priority:** P1
- **Level:** unit
- **Given** documents in states draft, approved, rejected, cancelled, closed, none of them reached through a person's approval decision **When** Approve is called on each **Then** each fails with "<name> is not waiting for approval. It is <State>." where <name> is `Draft {id}` for unnumbered documents, e.g. "Draft 42 is not waiting for approval. It is Rejected." and "PO/26-27/00001 is not waiting for approval. It is Approved." (CD-26: when a person's decision produced the current state, the wording is APR-071's "is no longer waiting … It was approved by {Name}.")
- **Go test:** `TestApprovalApprove_OnlyFromPending`
- **Sources:** `frappe/frappe/workflow/doctype/workflow/test_workflow.py:66` (`test_wrong_action`), `frappe/frappe/model/workflow.py:170-171` (`Not a valid Workflow Action`), `odoo/addons/hr_expense/models/hr_expense.py:1720` (filters submitted/draft)

### APR-056 · Approval stamps who and when on the document
- **Priority:** P1
- **Level:** integration
- **Given** the test clock is moved to 2026-10-24 09:15:00 UTC and Ravi approves Priya's Draft 42 (it becomes `PO/26-27/00001`) **When** the document is viewed **Then** it shows "Approved by Ravi Kulkarni on 24-10-2026 14:45" (Asia/Kolkata, `types.FormatIST`, the one timestamp format, CD-33), and `approved_by`/`approved_at` are stored (UTC).
- **Go test:** `TestApprovalApprove_StampsApprover`
- **Sources:** `odoo/addons/purchase/models/purchase_order.py:700` (`date_approve`), `odoo/addons/hr_expense/models/hr_expense.py:1724-1728`, `tryton/modules/purchase/purchase.py:985` (`confirmed_by`)

### APR-057 · Approved documents can't be approved or rejected again
- **Priority:** P1
- **Level:** unit
- **Given** `PO/26-27/00001` is approved (set up directly in that state, not by a person's approval in this test) **When** anyone calls Reject **Then** it fails with "PO/26-27/00001 is not waiting for approval. It is Approved." (CD-26)
- **Go test:** `TestApprovalReject_NotAfterApprove`
- **Sources:** `frappe/frappe/workflow/doctype/workflow/test_workflow.py:66` (`test_wrong_action`)

### Reject, revise and re-submit

### APR-058 · Reject needs a reason
- **Priority:** P1
- **Level:** unit
- **Given** Priya's Draft 42 pending **When** Ravi rejects with reason "" or "   " **Then** it fails with "Enter a reason for rejecting." and state stays `pending_approval`.
- **Go test:** `TestApprovalReject_ReasonRequired`
- **Sources:** `odoo/addons/hr_expense/wizard/hr_expense_refuse_reason.py:12` (`reason` required)

### APR-059 · Reject reason length limit
- **Priority:** P2
- **Level:** unit
- **Given** Draft 42 pending **When** the reason is 1,001 characters **Then** it fails with "Reason must be 1,000 characters or fewer."; 1,000 characters is accepted; leading/trailing spaces are trimmed.
- **Go test:** `TestApprovalReject_ReasonLength`
- **Sources:** `odoo/addons/hr_expense/wizard/hr_expense_refuse_reason.py:12` (Char field)

### APR-060 · Reject moves the document to rejected and keeps the reason
- **Priority:** P1
- **Level:** integration
- **Given** Priya's Draft 42 pending **When** Ravi rejects with "Rate is 12% above last PO. Renegotiate with vendor." **Then** state is `rejected`, the document stays locked, no effects are applied, no number is assigned (still `Draft 42`), and the reason shows on the document and in history with Ravi and the time.
- **Go test:** `TestApprovalReject_StoresReason`
- **Sources:** `odoo/addons/hr_expense/models/hr_expense.py:1816-1832` (`_do_refuse`), `hrms/hrms/hr/doctype/expense_claim/test_expense_claim.py:622` (`test_rejected_expense_claim`, no GL)

### APR-061 · Same eligibility rules apply to reject as to approve
- **Priority:** P1
- **Level:** integration
- **Given** Priya's Draft 42 pending under R1 **When** Priya (requester, no approver role) rejects **Then** it fails with the not-eligible message as for approve (she can see Draft 42, so the message is APR's, CD-52); **and** for Draft 43 created and submitted by Ravi, Ravi's reject fails with the own-document message.
- **Go test:** `TestApprovalReject_SameEligibility`
- **Sources:** `odoo/addons/hr_expense/models/hr_expense.py:1701-1704` (`_check_can_refuse`), `odoo/addons/hr_expense/tests/test_expenses.py:1308` (`test_expense_team_approver_refuse_mixed_selection`)

### APR-062 · Rejected document can't be edited until revised
- **Priority:** P1
- **Level:** integration
- **Given** Draft 42 is `rejected` **When** Priya edits the quantity **Then** it fails with DOC's guard text "Draft 42 was rejected. Revise it to move it back to draft." (CD-142)
- **Go test:** `TestApprovalRevise_RejectedIsLocked`
- **Sources:** F5; `frappe/frappe/workflow/doctype/workflow/workflow.py:199-203` (no submitted→draft without a transition)

### APR-063 · Revise moves rejected → draft
- **Priority:** P1
- **Level:** integration
- **Given** Draft 42 is `rejected` **When** Priya (creator, has `write` right) clicks Revise **Then** state is `draft`, the document is editable, it still has no number and shows `Draft 42`, and the last rejection reason stays visible as "Last rejected by Ravi Kulkarni: Rate is 12% above last PO. Renegotiate with vendor."
- **Go test:** `TestApprovalRevise_ToDraftKeepsReason`
- **Sources:** `frappe/frappe/workflow/doctype/workflow/test_workflow.py:143` (`test_workflow_action_recreated_when_state_is_re_entered`, Reject → Review), `odoo/addons/hr_expense/models/hr_expense.py:1441-1450` (`action_reset`)

### APR-064 · Only users with write right on the document can revise
- **Priority:** P2
- **Level:** integration
- **Given** Draft 42 is `rejected` **When** Suresh (given read but not `write` on Purchase Order in this test) clicks Revise **Then** it fails with "You don't have permission to change Purchase Order. Ask your administrator for a role that allows it." (revise needs `write`, spec R4)
- **Go test:** `TestApprovalRevise_NeedsWriteRight`
- **Sources:** `odoo/addons/hr_expense/models/hr_expense.py:1706-1708` (`Only HR Officers, accountants, or the concerned employee can reset to draft.`)

### APR-065 · Re-submit with a lower amount below every threshold → approved directly
- **Priority:** P1
- **Level:** integration
- **Given** Draft 42 was rejected at ₹60,000.00 and revised, and the PO counter for FY 2026-27 is unused **When** Priya reduces it to ₹45,000.00 and submits **Then** it goes straight to `approved` and gets `PO/26-27/00001`; history shows submitted → rejected → revised → submitted → approved (no approval needed).
- **Go test:** `TestApprovalResubmit_LowerAmountSkipsApproval`
- **Sources:** `odoo/addons/purchase/models/purchase_order.py:704-721` (`button_draft` then `button_confirm` re-checks), `frappe/frappe/model/workflow.py:105-109`

### APR-066 · Re-submit with a higher amount goes to the higher rule
- **Priority:** P1
- **Level:** integration
- **Given** Draft 42 rejected at ₹60,000.00 (R1) and revised **When** raised to ₹5,50,000.00 and submitted **Then** a new request under R2 (Director) is created; Ravi (Purchase Manager) can no longer approve it.
- **Go test:** `TestApprovalResubmit_HigherAmountHigherRule`
- **Sources:** `erpnext/erpnext/setup/doctype/authorization_control/authorization_control.py:63-115`

### APR-067 · Re-submit creates a fresh request; old decisions don't carry over
- **Priority:** P1
- **Level:** integration
- **Given** Draft 42 rejected and revised without changes **When** re-submitted **Then** a new open request exists, the old one is closed with outcome "rejected", and the document is `pending_approval` again (not auto-approved), still `Draft 42`.
- **Go test:** `TestApprovalResubmit_FreshRequest`
- **Sources:** `frappe/frappe/workflow/doctype/workflow/test_workflow.py:143` (`test_workflow_action_recreated_when_state_is_re_entered`)

### APR-068 · Rejection is not counted as approval history for re-submit with same approver
- **Priority:** P2
- **Level:** integration
- **Given** Draft 42 rejected by Ravi, revised and re-submitted at the same amount **When** Ravi approves this time **Then** it succeeds (an approver who rejected earlier may approve the new request).
- **Go test:** `TestApprovalResubmit_SameApproverMayApprove`
- **Sources:** `frappe/frappe/workflow/doctype/workflow/test_workflow.py:143`

### APR-069 · Revised draft can be deleted
- **Priority:** P2
- **Level:** integration
- **Given** Draft 42 revised to `draft` **When** Priya deletes it **Then** it is deleted per DOC draft rules; its approval history rows are kept in the audit trail (append-only). No approval request is open (the last one was already closed with outcome "rejected"), so nothing is marked "withdrawn"; that outcome is only for the withdraw action (APR-091).
- **Go test:** `TestApprovalRevise_DraftDeletable`
- **Sources:** `frappe/frappe/workflow/doctype/workflow_action/workflow_action.py:98-100` (`on_trash` clears actions), F5/F6

### APR-070 · A rejected expense claim does not reach Zoho export
- **Priority:** P2
- **Level:** integration
- **Given** Deepak's Expense Claim Draft 10 is `rejected` **When** the "approved, not yet exported" list is built **Then** Draft 10 is absent.
- **Go test:** `TestApprovalReject_NotExportable`
- **Sources:** `hrms/hrms/hr/doctype/expense_claim/test_expense_claim.py:622` (`test_rejected_expense_claim`, no GL entries), `odoo/addons/hr_expense/models/hr_expense.py:1712-1714` (`You can only generate an accounting entry for approved expense(s).`)

### Withdraw (spec R2)

### APR-091 · Submitter withdraws a pending document back to draft
- **Priority:** P1
- **Level:** integration
- **Given** Priya's PO Draft 42 (₹60,000.00) is `pending_approval` under R1 at version 3 **When** Priya clicks Withdraw **Then** in one transaction: state is `draft`; the open approval request is closed with outcome "withdrawn"; an approval history row `withdrawn` records Priya and the UTC time; the audit trail has a transition entry `pending_approval → draft` with action `withdraw` by Priya; `version` is 4; no number is assigned (it still shows `Draft 42`) and no effects are applied. The document is editable again.
- **Go test:** `TestApproval_Withdraw_SubmitterBackToDraft`
- **Sources:** spec R2, §3.2; `odoo/addons/purchase/models/purchase_order.py:704-706` (`button_draft`), `tryton/modules/purchase/purchase.py:311`

### APR-092 · Withdraw needs the submit right and visibility
- **Priority:** P1
- **Level:** integration
- **Given** Priya's Draft 42 is `pending_approval` **When** Anil (Administrator, which holds `submit` on Purchase Order; not the submitter) withdraws it **Then** it succeeds and the history row records Anil Kapoor. **When**, on a second pending copy, Deepak (given read but not `submit` on Purchase Order in this test) withdraws **Then** 403 "You don't have permission to submit Purchase Order. Ask your administrator for a role that allows it." and nothing changes. **When** Kavya (given role Purchase User and restricted to project PRJ-TECHPARK in this test, so she can't see the PRJ-METRO PO) withdraws **Then** 404 and nothing changes.
- **Go test:** `TestApproval_Withdraw_NeedsSubmitRight`
- **Sources:** spec R2, R4

### APR-093 · Withdrawn document leaves the approver's list and shows as withdrawn to the requester
- **Priority:** P1
- **Level:** integration
- **Given** Priya's Draft 42 is in Ravi's "Waiting for my approval" list and his count shows 1 **When** Priya withdraws it **Then** Draft 42 is no longer in Ravi's list and his count shows 0 (also gone from Meena's list); Priya's "My requests" shows the request for Draft 42 with outcome "withdrawn", by Priya, with the time.
- **Go test:** `TestApproval_Withdraw_LeavesInbox`
- **Sources:** spec R2; `frappe/frappe/workflow/doctype/workflow_action/workflow_action.py:60-83` (Open only)

### APR-094 · Withdraw is refused outside pending approval
- **Priority:** P1
- **Level:** integration
- **Given** documents in each state **When** Priya withdraws **Then** each is refused and nothing changes (state, version, history, audit): draft → "Draft 42 hasn't been submitted, so there is nothing to withdraw."; rejected → "Draft 42 was rejected. Revise it to move it back to draft."; approved → "PO/26-27/00001 has already been approved and can't be withdrawn. Cancel and amend it to make changes."; closed → "PO/26-27/00001 is closed and can't be changed."; cancelled → "PO/26-27/00001 is cancelled and can't be changed. Amend it to make a corrected copy."
- **Go test:** `TestApproval_Withdraw_OnlyFromPending`
- **Sources:** spec R2, §3.2

### APR-095 · Approver approves while the submitter's page is stale (race)
- **Priority:** P1
- **Level:** integration
- **Given** Priya's Draft 42 is pending, the PO counter for FY 2026-27 is unused, and Priya has the page open with the Withdraw button **When** Ravi approves (it becomes `PO/26-27/00001`) and then Priya's Withdraw click arrives **Then** the withdraw fails with "PO/26-27/00001 has already been approved and can't be withdrawn. Cancel and amend it to make changes."; the approval stands (state `approved`, number kept, effects applied once, no `withdrawn` history row). Run also as concurrent goroutines: exactly one of approve or withdraw succeeds (conditional `UPDATE … WHERE state = 'pending_approval'`).
- **Go test:** `TestApproval_Withdraw_StaleAfterApprove`
- **Sources:** spec R2; `frappe/frappe/workflow/doctype/workflow_action/workflow_action.py:133-157` (state check)

### APR-096 · Re-submit after withdraw opens a new request on current rules and amount
- **Priority:** P1
- **Level:** integration
- **Given** Priya withdrew Draft 42 (₹60,000.00, R1) **When** she raises it to ₹5,50,000.00 and submits again **Then** a new open approval request under R2 (Director) is created from the current rules and amount; the withdrawn request stays in history with outcome "withdrawn"; Ravi (Purchase Manager) can no longer approve it, Meena can.
- **Go test:** `TestApproval_Withdraw_ResubmitNewRequest`
- **Sources:** spec R2; `odoo/addons/purchase/models/purchase_order.py:704-721` (`button_draft` then `button_confirm` re-checks)

### APR-097 · Requester withdraws from the browser and the approver's count drops
- **Priority:** P1
- **Level:** e2e
- **Given** Priya is signed in with a draft PO Draft 42 for ₹94,164.00 (reference PO) **When** she submits it **Then** she sees a "Withdraw" button and Ravi's "Waiting for my approval" count shows 1. **When** Priya clicks Withdraw **Then** the document shows state Draft and is editable, and when Ravi reloads his count shows 0 and Draft 42 is not in his list. (e2e: lives under `e2e/`, owned and run by WP3, CD-40; Go test named per CD-72 and CD-74.)
- **Go test:** `TestE2E_ApprovalWithdraw`
- **Sources:** spec R2, §6

### Concurrency and stale actions

### APR-071 · Two approvers act at once — first wins
- **Priority:** P1
- **Level:** integration
- **Given** Priya's Draft 42 pending and Ravi and Meena both have the page open **When** Ravi approves (it becomes `PO/26-27/00001`), then Meena rejects **Then** Ravi's action succeeds; Meena gets "PO/26-27/00001 is no longer waiting for approval. It was approved by Ravi Kulkarni." (CD-26: a person's decision produced the current state) and nothing changes.
- **Go test:** `TestApprovalConcurrency_FirstActionWins`
- **Sources:** `frappe/frappe/workflow/doctype/workflow_action/workflow_action.py:133-157` (`apply_action`, state check), `:202` (`return_link_expired_page`)

### APR-072 · Concurrent approve calls produce exactly one approval (race)
- **Priority:** P1
- **Level:** integration
- **Given** Priya's Draft 42 pending **When** 10 goroutines call Approve concurrently as Ravi and Meena **Then** exactly one succeeds, effects apply once, exactly one number (`PO/26-27/00001`) is used, one "approved" history row exists, and the others get the "no longer waiting" error (SQLite `busy_timeout`, conditional `UPDATE … WHERE state='pending_approval'`).
- **Go test:** `TestApprovalConcurrency_ParallelApproveOnce`
- **Sources:** `frappe/frappe/model/workflow.py:152-174`; F5 optimistic concurrency (DOC)

### APR-073 · Requester can't edit while pending, even via API
- **Priority:** P1
- **Level:** integration
- **Given** Priya's Draft 42 pending **When** a direct save request changes grand total to ₹40,000.00 **Then** it fails with "Draft 42 is waiting for approval and can't be changed. Withdraw it to make changes." and the approval request amount is unchanged.
- **Go test:** `TestApprovalPending_EditRefused`
- **Sources:** `odoo/addons/hr_expense/tests/test_expenses.py:630` (`test_corner_case_expense_submitted_cannot_be_zero`, case 2), `odoo/addons/hr_expense/models/hr_expense.py:1010-1019`

### APR-074 · Approver can't edit the document while approving
- **Priority:** P1
- **Level:** integration
- **Given** Ravi is an eligible approver of Priya's Draft 42 **When** he tries to change a line rate before approving **Then** it is refused with the same pending-edit message; the only actions offered are Approve and Reject. There is no Edit, Save or Withdraw (the seeded Purchase Manager role has no `submit` right on Purchase Order; same as AUTH-084). Note (CD-142): the seeded Purchase Manager also has no `write` right, and DOC checks rights before the state guard, so Ravi first gets AUTH's 403; the test asserts that, then grants `write` to check the pending-edit message.
- **Go test:** `TestApprovalPending_ApproverCannotEdit`
- **Sources:** `odoo/addons/hr_expense/models/hr_expense.py:1010-1019` (`You can't edit this expense`)

### APR-075 · Pending documents can't be deleted or cancelled
- **Priority:** P1
- **Level:** integration
- **Given** Priya's Draft 42 pending **When** Priya (delete right on her own drafts) deletes it and Ravi (cancel right) cancels it **Then** delete fails with "Draft 42 is waiting for approval and can't be deleted. Withdraw it first, then delete it." and cancel fails with "Draft 42 is waiting for approval. Withdraw it first if it needs changes." (same messages as DOC-062 and DOC-005)
- **Go test:** `TestApprovalPending_NoDeleteNoCancel`
- **Sources:** `frappe/frappe/workflow/doctype/workflow/workflow.py:206-207` (`Cannot cancel before submitting`), F5

### APR-076 · State can't be changed by writing the state field
- **Priority:** P1
- **Level:** unit
- **Given** a draft PO of ₹60,000.00 **When** a save request sets `state = approved` directly (or import sets it) **Then** the `state` input is ignored like every system field (DOC-003, CD-142): the save succeeds and the document stays `draft`.
- **Go test:** `TestApprovalTransition_NoDirectStateWrite`
- **Sources:** `frappe/frappe/model/workflow.py:336-355` (`Workflow State transition not allowed from {0} to {1}`), `odoo/addons/hr_expense/models/hr_expense.py:1007-1008`, `:1023-1027`

### Waiting for my approval and notifications

### APR-077 · "Waiting for my approval" lists only what the user can act on
- **Priority:** P1
- **Level:** integration
- **Given** pending: Priya's Draft 42 (₹60,000.00, R1), Priya's Draft 50 (₹6,00,000.00, R2), Draft 43 (created by Ravi, R1), Deepak's Expense Claim Draft 10 (designated Priya) **When** Ravi (Purchase Manager) opens the list **Then** he sees only Draft 42; Meena (Director) sees Draft 42, Draft 43, Draft 50 and Draft 10 (Director is the claim's fallback role); Priya sees only Draft 10.
- **Go test:** `TestApprovalInbox_OnlyActionable`
- **Sources:** `frappe/frappe/workflow/doctype/workflow_action/workflow_action.py:60-83` (permission query, Open only), `:340-352`, `frappe/frappe/workflow/doctype/workflow/test_workflow.py:92` (`test_get_common_transition_actions`)

### APR-078 · Count badge matches the list
- **Priority:** P2
- **Level:** integration
- **Given** Meena's list has 3 items **When** the navigation renders **Then** the badge shows 3; after she approves one and reloads, it shows 2.
- **Go test:** `TestApprovalInbox_CountMatches`
- **Sources:** `frappe/frappe/workflow/doctype/workflow_action/workflow_action.py:107` (`clear_doctype_notifications`)

### APR-079 · Inbox rows show what's needed to decide
- **Priority:** P2
- **Level:** e2e
- **Given** Draft 42 for party `SHREE` Shree Steel Traders, ₹94,164.00 (reference PO), submitted by Priya Shah at the fixture clock (2026-10-24 10:30 IST) **When** Ravi opens his list **Then** the row shows document type, "Draft 42", party, amount "₹94,164.00", submitted by, submitted on "24-10-2026 10:30" (Asia/Kolkata, the one timestamp format, CD-33, CD-51), and links to the document; sorted oldest first. The party comes from `doctype.Info.Party` (CD-55). (e2e: lives under `e2e/`, owned and run by WP3, CD-40; e2e Go tests start `TestE2E_`, CD-74.)
- **Go test:** `TestE2E_ApprovalInboxRowContents`
- **Sources:** `frappe/frappe/workflow/doctype/workflow_action/workflow_action.py:486-521` (`get_common_email_args`)

### APR-080 · Item leaves the inbox once acted on or re-entered correctly
- **Priority:** P1
- **Level:** integration
- **Given** Priya's Draft 42 in Ravi's list **When** Meena approves it **Then** it disappears from Ravi's list. If instead it is rejected, revised and re-submitted, it appears again exactly once.
- **Go test:** `TestApprovalInbox_ClosesAndReopens`
- **Sources:** `frappe/frappe/workflow/doctype/workflow/test_workflow.py:143` (`test_workflow_action_recreated_when_state_is_re_entered`), `:175` (`test_workflow_action_not_duplicated_on_resave`)

### APR-081 · "My requests" shows the requester the outcome and reason
- **Priority:** P1
- **Level:** integration
- **Given** Priya submitted Draft 42 (pending), a PO now approved as `PO/26-27/00001`, and Draft 45 (rejected with reason) **When** Priya opens "My requests" **Then** she sees all three with their states and, for Draft 45, the reason and who rejected it.
- **Go test:** `TestApprovalInbox_MyRequests`
- **Sources:** `odoo/addons/hr_expense/models/hr_expense.py:1216-1235` (`update_activities_and_mails`), `hrms/hrms/hr/doctype/expense_claim/expense_claim.py:219-222` (`notify_approval_status`)

### APR-082 · Approver who is also requester doesn't see their own document as actionable
- **Priority:** P1
- **Level:** unit
- **Given** R1 allow self approval off and Ravi submitted Draft 43 (Ravi is given the Purchase User role in this test, because the seeded Purchase Manager role has no `create` or `submit` right on Purchase Order; see 01-auth.md) **When** Ravi's inbox is built **Then** Draft 43 is absent; with allow self approval on, it is present.
- **Go test:** `TestApprovalInbox_ExcludesOwnUnlessAllowed`
- **Sources:** `frappe/frappe/workflow/doctype/workflow_action/workflow_action.py:350-354` (owner removed unless `send_email_to_creator`)

### History and audit

### APR-083 · Every approval step writes an append-only history row
- **Priority:** P1
- **Level:** integration
- **Given** Priya's Draft 42 goes submit → reject → revise → submit → approve **When** history is read **Then** it has 5 rows in order with action, user, UTC time, amount and rule id; rows can't be updated or deleted (attempt fails at the store layer).
- **Go test:** `TestApprovalHistory_AppendOnly`
- **Sources:** F6; `frappe/frappe/model/workflow.py:265`, `odoo/addons/hr_expense/models/hr_expense.py:1826-1832` (`message_post_with_source` refuse reason)

### APR-084 · History records the rule and amount at the time
- **Priority:** P2
- **Level:** integration
- **Given** Draft 42 submitted at ₹60,000.00 under R1, rejected, revised to ₹5,50,000.00 and submitted under R2 **When** history is read **Then** the first submit row shows ₹60,000.00 / R1 and the second ₹5,50,000.00 / R2, even after R1 is later edited.
- **Go test:** `TestApprovalHistory_KeepsRuleAndAmount`
- **Sources:** `frappe/frappe/workflow/doctype/workflow_action/workflow_action.py:376-392`

### APR-085 · State transitions also appear in the document audit trail
- **Priority:** P2
- **Level:** integration
- **Given** Ravi approves Priya's Draft 42 **When** the DOC audit trail is read **Then** it has an entry with transition `pending_approval → approved`, user Ravi, UTC time, and the number `PO/26-27/00001` assigned in that transition.
- **Go test:** `TestApprovalHistory_InDocAudit`
- **Sources:** F6; `frappe/frappe/model/workflow.py:265`

### Cancel and amend interplay

### APR-086 · Cancelling an approved document doesn't need approval
- **Priority:** P2
- **Level:** integration
- **Given** `PO/26-27/00001` approved under R1, no later document links to it **When** Meena (with `cancel` right) cancels it **Then** it is `cancelled` directly; no approval request is created.
- **Go test:** `TestApprovalCancel_NoApprovalNeeded`
- **Sources:** `frappe/frappe/model/workflow.py:270-290` (`can_cancel_document`), `odoo/addons/purchase/models/purchase_order.py:723-733`

### APR-087 · Amended copy needs fresh approval
- **Priority:** P1
- **Level:** integration
- **Given** `PO/26-27/00042` (₹60,000.00) was approved, then cancelled and amended to a new draft Draft 61 **When** the amended draft is submitted **Then** it is `pending_approval` under R1 (the original approval does not carry over), it still shows `Draft 61` (it gets `PO/26-27/00042-1` only when approved), and its history starts empty apart from a link to `PO/26-27/00042`.
- **Go test:** `TestApprovalAmend_NeedsFreshApproval`
- **Sources:** F5; `frappe/frappe/workflow/doctype/workflow/workflow.py:194-196` (`Cannot change state of Cancelled Document`)

### APR-088 · Approval fields are not copied by amend or duplicate
- **Priority:** P1
- **Level:** unit
- **Given** approved `PO/26-27/00001` with approved_by Ravi **When** it is duplicated or amended **Then** the copy has no number (shows `Draft {id}`), empty approved_by, approved_at, rejection reason and no open request.
- **Go test:** `TestApprovalCopy_ClearsApprovalFields`
- **Sources:** `odoo/addons/hr_expense/models/hr_expense.py:147` (`approval_state` `copy=False`), `frappe/frappe/workflow/doctype/workflow/test_workflow.py:828-848` (`create_workflow_state_field`, `no_copy`)

### Fuzz and invariants

### APR-089 · Invariant: state machine reachable states (fuzz)
- **Priority:** P2
- **Level:** unit
- **Given** random sequences of {submit, withdraw, approve, reject, revise, edit, cancel} by random users over random amounts and rules **When** applied **Then** after every step: a document is `approved` only if no rule matched at its last submit or an eligible non-self approver approved it; `pending_approval` documents have exactly one open request; `draft` documents have no open request; `rejected` documents have a non-empty reason; a document has a number only if it is `approved`, `closed` or `cancelled`; no effects exist for non-approved documents.
- **Go test:** `FuzzApproval_StateInvariants`
- **Sources:** `frappe/frappe/workflow/doctype/workflow/workflow.py:167-207` (`validate_docstatus`), F5

### APR-090 · Invariant: rule matching is monotonic in amount (fuzz)
- **Priority:** P2
- **Level:** unit
- **Given** any set of enabled rules for a document type **When** amounts a ≤ b are matched **Then** the matched rule for b has from amount ≥ that for a, and if a needs approval then b does too.
- **Go test:** `FuzzApprovalMatch_Monotonic`
- **Sources:** `erpnext/erpnext/setup/doctype/authorization_control/authorization_control.py:24-61`

## Deferred and excluded
| Scenario | Source | later/never | Reason |
|---|---|---|---|
| Multi-level approval chains (e.g. Purchase Manager then Director) | `frappe/frappe/workflow/doctype/workflow/workflow.py` (states/transitions); `odoo/addons/purchase/models/purchase_order.py:1332-1341` (two_step) | later | v1 is single-level (D-APR-14); higher-tier approvers already cover large amounts. |
| Email to approvers / requester, with approve-from-email links | `frappe/frappe/workflow/doctype/workflow_action/workflow_action.py:94-131`, `:395-412`, `:133-170` | later | Needs SMTP config, outbox and retries; signed links add a security surface. In-app list in v1. |
| Email failure must not break the transition (log and continue) | `frappe/frappe/workflow/doctype/workflow_action/workflow_action.py:408-412` | later | Applies when email arrives; keep the rule for that release. |
| Bulk approve / reject from the list | `frappe/frappe/model/workflow.py:379-453`; `frappe/frappe/workflow/doctype/workflow/test_workflow.py:112` | later | Low volume at 20 people; per-document checks keep audit simple. |
| Delegation / out-of-office approver | — | later | Fallback role and live role checks cover absence in v1. |
| Reminders / escalation after N days | `odoo/addons/hr_expense/models/hr_expense.py:1813` (`last_notification_date`) | later | Needs a scheduler and email. |
| Partial approval (sanctioned amount less than claimed) | `hrms/hrms/hr/doctype/expense_claim/expense_claim.py:660-665`, `:546-560` | later | Reject with reason + revise covers it; partial sanction complicates Zoho export. |
| Duplicate-expense warning on approve | `odoo/addons/hr_expense/models/hr_expense.py:1418-1426` | later | Belongs to the Expenses module, not core APR. |
| Discount-based rules (average / item / customer discount) | `erpnext/erpnext/setup/doctype/authorization_rule/authorization_rule.py:81-105`; `erpnext/erpnext/setup/doctype/authorization_control/test_authorization_control.py:102-200` | later | Only relevant to Sales Invoice, a later module. |
| Rules scoped to branch, project, vendor or "applicable to" user/role/designation | `erpnext/erpnext/setup/doctype/authorization_control/authorization_control.py:117-128`, `:244-321` | later | Not needed for one company of 20; adds ambiguity. |
| Free-form conditions (Python/expressions) on transitions | `frappe/frappe/model/workflow.py:130-134`; `frappe/frappe/workflow/doctype/workflow/test_workflow.py:230` (`test_syntax_error_in_transition_rule`) | never | Unsafe and untestable; amount thresholds cover the need. |
| Transition tasks / webhooks / server scripts on approval | `frappe/frappe/model/workflow.py:189-240`; `test_workflow.py:304` (`test_sync_tasks`) | never | No runtime scripting in a compiled single binary. |
| Configurable states per document type | `frappe/frappe/workflow/doctype/workflow/workflow.py:99-160` | never | F5 fixes one state machine for all documents. |
| Auto-approve when the employee has no manager | `odoo/addons/hr_expense/tests/test_expenses_states.py:196` | never | Money leaves without review (D-APR-07). |
| Auto-approve at submit when the requester holds the approver role | `odoo/addons/purchase/models/purchase_order.py:716-718`, `:1341` | never | Self-approval is off by default and explicit when allowed (D-APR-06). |
| Super-user (Administrator) bypass of self-approval | `frappe/frappe/model/workflow.py:363-364` | never | One rule for everyone; audit clarity. |
| Approval required to cancel | `frappe/frappe/model/workflow.py:270-290` | later | Cancel is a DOC/AUTH right in v1. |
| Auto-sharing the document with the approver | `hrms/hrms/hr/utils.py:785-806` | never | Hidden permission side-effects; AUTH record rules grant manager access instead. |
| Multi-company rule scoping, currency conversion of threshold | `erpnext/erpnext/setup/doctype/authorization_control/authorization_control.py:32-49`; `odoo/addons/purchase/models/purchase_order.py:1338-1340` (`_convert`) | never | F1 single company, F2 INR only. |

## Interfaces this area needs from others
- **DOC** provides `doc.Engine.Do(ctx, p, doctype, id, a, in)` running the F5 actions `submit`, `withdraw`, `approve`, `reject`, `revise` in one `BEGIN IMMEDIATE` transaction that calls APR in the §5 call protocol order (CD-23; `Record` takes the `doctype.Action*` constants, CD-54); for approve and reject it checks visibility first (404), then `CheckApprover`, then its from-state guard, and runs no generic `Can(approve)` (CD-52); `doctype.ErrNotFound` is the one not-found sentinel (CD-54) and every error reaching a handler implements `doctype.HTTPError` (CD-44); `doctype.InfoSource.Info` for document facts (creator, employee, projects, state, display label `Draft 42` or number, DocType label, party name `Info.Party`; CD-05, CD-55) and the `doctype` registry for labels and flags (CD-02); `Hooks.Validate` and `Hooks.BeforeSubmit` re-run at approve; `Hooks.ApplyEffects` run in the approve transaction; `Hooks.Amount` (grand total incl. GST after round-off; claim total for expenses); lock enforcement for `pending_approval`/`rejected`; conditional state update (`WHERE state = ?`) for concurrency; and copy/amend that clears fields marked no-copy.
- **AUTH** provides `auth.Service.Can(ctx, p, doctype, Right, rec)` for `write` (revise), `submit` (submit, withdraw) and `read` (DOC's visibility check before approve and reject, CD-52) including scopes `own`, `team`, `all` and restrictions (a reporting manager reads and approves their direct reports' expense claims through `team`, CD-11); `UsersWithRight(ctx, doctype, Right, rec)` returning enabled users only (APR's `CheckApprover` and inbox use it for `approve` and `read`); tests use the shared `authfake` with its seeding helpers (CD-56); `HasRole(ctx, u, role)` for rule roles; `User(ctx, tx, id)` for names; and disabled-user checks at action time.
- **APR itself** implements `approval.Service`: `Evaluate` and `Open` at submit, `CheckApprover` at approve and reject, `Record` for every history row (submitted, approved, rejected, revised, withdrawn; withdraw also closes the open request as "withdrawn"), and `Inbox` for "Waiting for my approval".
- **MST** implements `auth.Directory` (CD-13): `ManagerChain`, `UserForEmployee`, `EmployeeForUser`, `DirectReports` (the last two skipping employees who have left, CD-54), each taking the caller's `tx`; and active/disabled status of employees and roles. Wave 1 tests use `testkit.Directory` (`authfake.NewDirectory()`, CD-56).
- **NAM** provides `naming.Service.Assign(ctx, tx, doctype, docID, date)` called inside the transaction that first enters `approved` (approve, or submit when no rule applies), and `AssignAmendment` when an amendment is approved (spec R1). Pending, rejected and revised documents have no number and show `Draft {id}`.
- **NUM** provides money as integer paise, `types.ParseMoney` ("at most 2 decimal places"), `types.FormatINR` with Indian grouping (₹6,00,000.00) and `types.FormatIST` (`24-10-2026 14:45`) (CD-33), merged to main first so no formatter case waits (CD-53); paging types `types.Page`/`types.Result` (CD-01).
- **DOC/F6 audit** provides `audit.Writer` (`audit.Default` in production, `audit/auditfake` in tests; CD-03), an append-only writer that records each transition (including `withdraw`) and APR's rule changes; APR keeps its approval history in its own append-only table.
- **F7** UTC storage and Asia/Kolkata display for `approved_at` and history times.
