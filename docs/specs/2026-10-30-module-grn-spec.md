# Module spec: Goods Receipt (GRN)

Version 0.4 · 30 October 2026 · Status: for owner review, with CD-234, CD-235 and CD-237 applied · Author: AD-6

Follows the Purchase Order spec (`docs/specs/2026-10-30-module-purchase-order-spec.md`), whose §14 shared decisions, §12 cutover plan and §17 sources apply here. Marks **(CD-234)**, **[Assumption]** and **CA** mean the same as there.

---

## 1. Purpose and approach

Suresh stands at the gate with a phone, in gloves, with a truck waiting. He picks the PO, checks the quantities, photographs the challan and submits. The PO's pending quantity drops, and someone with `write` can add the bill details later from the photo.

**Three ways to hold bill details, which arrive after the goods.** Approved documents can't be edited (R7, DOC-051).

| | Approach | For | Against |
|---|---|---|---|
| **A (chosen, CD-234)** | A `Bill details` panel beside an approved GRN, in its own table, audited | Fits R7; bill details can be typed later from the attached photo, so Suresh needn't do it at the gate; no new lifecycle | One GRN holds one bill |
| B | A fourth document, Vendor Bill, with three-way match | Handles one bill across many GRNs | Starts rebuilding your accounting system; a full lifecycle and print for no filing benefit |
| C | Header fields on the GRN | Simplest | A bill that arrives after approval means cancel and amend, and Stores must type tax data |

## 2. Scope

**In scope.** Receipt against one approved PO, one site; goods and services; partial receipts; received, accepted and rejected quantities with a reason; over-delivery warning; delivery and transport details; photos; bill details; print; registers.

**Non-goals.**
- Receipt without a PO [Assumption]: small local buys go on an expense claim. Listed for owner review in §15.
- One GRN across several POs [Assumption]: one GRN per PO, so several POs on one truck mean several GRNs.
- Quality inspection records, and stock movement or valuation (no stock module).
- Purchase return documents, debit notes (CD-234). Goods rejected at the gate go back on the truck. Goods found bad after acceptance: cancel and amend the GRN, or handle it in your accounting system. Delivery Challan is a later module.
- Three-way match, and any payment or due-date tracking.
- Generating e-way bills, and any check of the e-way threshold.

## 3. DocType

`internal/modules/goodsreceipt`: `Name: "goodsreceipt.Receipt"`, `Label: "Goods Receipt"`, `PluralLabel: "Goods Receipts"`, tables `goods_receipt` and `goods_receipt_line` (AUTOINCREMENT, CD-156; `parent_id`, `line_no`, CD-161), `Series: "GR/{FY}/{#####}"` (CD-113), `Submittable`, `Approvable`, `Printable`, `OwnerField: "created_by"`, `ProjectFields: ["project_id"]`, `SiteFields: ["site_id"]`.

No approval rule is seeded, so submit goes straight to approved (`00-fixtures.md`). `Approvable` stays true so an owner can add a rule later without code.

```go
Links:   []doctype.LinkSpec{{Field: "po_line_id", Target: purchase.DocType, OnLine: true}}
RollUps: []doctype.RollUp{{TargetDocType: purchase.DocType, TargetField: "received_qty",
                           SourceField: "accepted_qty", JoinField: "po_line_id"}}
```

The line link gives DOC-017's closed-source refusal, DOC-038's cancelled-source refusal and DOC-052's cancel block on the PO. `purchase_order_id` on the header gets its own `LinkSpec` too, so CD-70 runs `CheckLink` (AUTH-101).

### 3.1 Header

| Field | Label | Type | Rule |
|---|---|---|---|
| `number` | GRN number | Text, read-only | List priority 1 |
| `purchase_order_id` | Purchase order | Link `purchase.Order` | Required. No `Step`, so it's on the first step, `details` (hooks spec §4.1). Can't change once the draft has lines; a wrong PO means delete the draft |
| `vendor_id` | Vendor | Link Party, `System`, `ReadOnly` | Copied from the PO in `Validate`. The name is what MST-043 reads (CD-216 item 8) |
| `site_id` | Site | Int | Posted by the module's first step from the PO; `Validate` overwrites it from the PO. Hidden on forms. Display comes from `snap_site_name` |
| `project_id` | Project | Int | The same |
| `receipt_date` | Receipt date | Date | Required, default today. Not in the future. Sets the FY of the number. On the Choose PO step |
| `challan_no` | Vendor challan no. | Text, max 40 | Required at submit when any line is goods. Step `delivery` |
| `challan_date` | Challan date | Date | Optional. Not after the receipt date. Step `delivery` |
| `vehicle_no` | Vehicle no. | Text, max 20 | Optional. Step `delivery` |
| `transporter` | Transporter | Text, max 140 | Optional. Step `delivery` |
| `eway_bill_no` | E-way bill no. | Text, max 20 | Optional, no format check. Step `delivery` |
| `remarks` | Remarks | Text, max 500 | Printed |
| `accepted_value` | Accepted value | Money, read-only | Σ accepted × net rate, before tax. List priority 2 |
| `state` | State | Select, read-only | List priority 3 |

**Why copied fields look like this** (kernel check, 30 October).
- `create` checks rights against a record built from the posted header, before the insert and before `Validate` (`internal/kernel/doc/save.go`). `System` fields never reach that header, so a site-restricted user given a `System` site gets a 404 on every create. `site_id` and `project_id` must therefore be posted.
- A `Link` field gets a master check on every save, so a site disabled after the PO would stop the receipt. A plain `Int` column has no master check, and `SiteFields` and `ProjectFields` read it the same way. The foreign key stays in the migration.
- `vendor_id` and `uom_id` carry no restriction, so `System` plus `ReadOnly` keeps them out of input and out of master checks. Audit diffs leave them out (DOC-088).
- A plain save never trusts the posted `site_id`: `Validate` resets it from the PO.

Snapshot columns: `snap_vendor_name`, `snap_vendor_gstin`, `snap_vendor_address`, `snap_po_number`, `snap_po_date`, `snap_site_name`, `snap_project_code`, `snap_branch_id`. Amend copies none of these and runs no hook today (PO spec §15 item 8).

### 3.2 Lines

Lines come from PO lines only, never typed. The Received quantities step renders from the PO's lines with pending, overlaid with this draft's lines, and posts only lines with a quantity above 0. So a line cleared to 0 is simply not stored, and `Receive all pending` can bring it back.

| Field | Label | Type | Rule |
|---|---|---|---|
| `po_line_id` | PO line | Int | Required; a line of `purchase_order_id` |
| `uom_id` | UoM | Link UoM, `System`, `ReadOnly` | Copied from the PO line: a receipt uses the PO's unit (ERPNext's rule). A vendor delivering in another unit is converted by the user [Assumption] |
| `received_qty` | Received | Qty | More than 0 at submit |
| `rejected_qty` | Rejected | Qty | Default 0. Not above received |
| `accepted_qty` | Accepted | Qty, read-only | Received − rejected |
| `reject_reason` | Reason for rejecting | Text, max 200 | Required when rejected is above 0 |

Snapshot columns: `snap_item_code`, `snap_item_name`, `snap_hsn_sac`, `snap_uom_code`, `snap_rate`.

`uom.RegisterQtyUse` is called twice from `init`: for `received_qty` and for `rejected_qty`, both with `goods_receipt_line`, `parent_id`, `uom_id`.

### 3.3 Bill details (not a DocType) (CD-234)

Table `grn_bill` in `goodsreceipt`, one row per GRN, `grn_id` unique, with `version`.

| Column | Label | Rule |
|---|---|---|
| `invoice_no` | Vendor invoice no. | Required, max 40. Not limited to Rule 46's 16 characters, so an odd vendor number can still be recorded |
| `invoice_date` | Invoice date | Required; not in the future |
| `invoice_amount` | Invoice total | Money, optional |

- Editable while the GRN is `approved`, by anyone with `write` on the GRN at their scope: Stores by default, since there is no Accounts role (CD-234). Refused on a draft, pending or cancelled GRN: `Add bill details after the receipt is approved.`
- Every write goes through `db.Write`, posts the page's `version`, checks state inside the transaction (CD-94) and writes an audit entry with `audit.Change` rows on the GRN.
- Same vendor, same invoice number on another GRN: allowed with a warning banner, `Invoice {no} is also on {GR number}.`, because one bill can cover two deliveries.
- A difference from `accepted_value` is shown as information only, because accepted value excludes tax: `Accepted value before tax is ₹{x}.` No match or refusal.
- Bill details live on after cancel for audit, read-only.

Photos of the challan and invoice are ordinary attachments: D-FILE-06 lets anyone with `write` add a file in `approved` and `closed`, so a late bill photo needs nothing new.

## 4. Validation and the over-receipt rule

Required fields are checked at submit (CD-178). `Validate` copies header and line snapshots from the PO, computes accepted and value, and reads inside the transaction.

| Condition | Message | When |
|---|---|---|
| Receipt date after today | `Receipt date can't be in the future.` | Save |
| Challan date after receipt date | `Challan date can't be after the receipt date.` | Save |
| Line from another PO | `Row {n}: This line isn't on {PO number}.` | Save |
| Rejected above received | `Row {n}: Rejected can't be more than received.` | Save |
| Rejected with no reason | `Row {n}: Reason for rejecting is required.` | Submit |
| Blank challan with a goods line | `Vendor challan no. is required for goods.` | Submit |
| No line with a quantity | R8's text | Submit |
| Blank purchase order | `Choose a purchase order.` | Create |
| PO changed on a draft with lines | `Delete this draft to receive against another order.` | Save |
| Lock | PO spec §14.2's message, from `BeforeSubmit` | Submit |

Source-state wording is DOC's: DOC-017, DOC-038, `msgSourcePending` and so on (CD-155). Whole-number checks use `uom.CheckLineQty` for received and rejected.

**Over-delivery warns only (CD-234).** Nothing refuses it, so there is no allowance setting and no quantity invariant.

- On the Received quantities step, the check page and the document page: `Row {n}: Accepted {x} is more than the {y} pending on {PO number}.`, through `Module.Check` and `Module.Page`. It compares accepted, so goods rejected at the gate never trigger it.
- Pending is computed with `already_received`, accepted on other approved or closed GRNs, and this GRN's accepted.
- The PO's pending floors at 0, its `received_qty` can exceed `qty`, and the document page shows `Over by {x}`.
- A receipt date before the PO's order date also warns, not refuses, because back-entered paper can run that way (`Receipt date is before the order date {date}.`) [Assumption].

Accepted is what counts, not received, because rejected goods don't reduce pending (CD-234).

## 5. States, effects and cancel

The kernel's state machine, with `submit` going straight to `approved`.

| Action | Effect |
|---|---|
| Submit | `BeforeSubmit` lock check; number assigned; state approved; `ApplyEffects`; roll-up |
| Cancel | Reason required; `ReverseEffects` runs after the state is set; roll-up |
| Amend | Copies header and lines. `purchase_order_id` and the lines are stored fields, and `Validate` fills the rest (PO spec §15 item 8). PO must still be approved |
| Close | Not offered and refused (§14 item 4) |
| Delete draft | Stores, own. The only way out of a wrong PO |

**`ApplyEffects` and `ReverseEffects`** read, for every line of this GRN's PO, the sum of accepted on approved and closed GRN lines, and call `purchase.RefreshReceiptStatus(ctx, tx, poID, accepted)`. The kernel has already set this GRN's state, so the sum is right in both directions. `ReverseEffects` also calls `modkit.CheckNotLocked` (PO spec §14.2) and returns its refusal, which rolls the cancel back (DOC-077).

**Cancelling after the PO closed** is allowed (D-DOC-09). The PO's roll-ups change and it stays closed.

**Cancelling a GRN with bill details.** Allowed. The cancel page reminds the user: `This receipt has bill details ({invoice no.}). Correct your accounts too.`

**Lock.** Off by default (PO spec §14.2). When on, a GRN dated on or before `Locked up to` can't be submitted or cancelled. An amendment keeps the old date, so its user changes the receipt date or the lock refuses it.

## 6. Numbering

`GR/{FY}/{#####}`, 14 characters (CD-113). First approval, gapless, FY from `receipt_date`. The document is still called GRN. Cutover sets the counter (PO spec §12).

## 7. GST and the audit's open items

| Item | Decision | CA |
|---|---|---|
| Vendor invoice no. and date | Bill details panel (CD-234) | CA: credit needs the invoice held and goods received (CGST s.16(2)). On supplies in lots, credit comes on the last lot |
| IRN | Not captured (CD-234). It's on the invoice photo as a QR code. I found no rule that a recipient must record it | CA: an in-scope invoice without e-invoicing "shall not be treated as an invoice" (Rule 48(5)). Depends on the vendor's turnover above ₹5 crore |
| E-way bill no. | Optional field, no threshold logic (CD-234). The app doesn't know whether one was required | CA: ₹50,000 under Rule 138, with state limits that differ. Maharashtra's ₹1 lakh intra-state limit couldn't be confirmed from a live primary page |
| Reverse charge | Lives on the PO. The GRN print copies the note | CA |
| TDS | Not in the app | CA |
| Tax on the GRN | None stored. Rates come from the bill, keyed in your accounting system | |
| MSME 45-day rule | Not tracked. The GRN's receipt date is evidence of acceptance | CA: MSMED s.15, and Income-tax Act 2025 s.37(2)(g) for micro and small vendors. Vendor Udyam status isn't in the Party master |
| Rejected goods on the invoice | Not handled. Rejected quantity shows on the print and in the report | CA: what happens to credit on rejected goods |

## 8. Links to masters

Everything comes from the PO. The GRN's own picker is the PO: approved, not closed, visible to this user, with pending above 0 by default. `Show fully received orders` widens it, because another truck can still arrive and over-delivery only warns (CD-234). The module's first step runs `auth.CheckLinkTx` before `Engine.Do`, so a posted foreign PO gets AUTH-101's refusal, not the 404 that `create` would give (`You can't use Purchase Order PO/26-27/00030. It isn't available to you.`). A party link named `vendor_id` means MST-043 sees the GRN. Masters disabled after the PO are not re-checked, because goods on a truck must still be receivable after someone disables the item [Assumption]. The field types in §3.1 make this hold with no kernel change.

## 9. Print

`print.GRNDocument`, title `Goods Receipt Note` (FILE-067), with the existing fields filled from snapshots. Additions to `print.GRNDoc`, additive and omitted when empty: `VehicleNo`, `Transporter`, `EWayBillNo`, `Remarks`, and `RejectReason` on `GRNLine`, printed under the item. The golden `grn_approved` updates. `ChallanNo` blank on a service GRN prints as a dash, which needs a small change to the template.

## 10. Rights and restrictions

| Role | Rights (scope) |
|---|---|
| Stores | read, create, write, submit, amend, print (all); delete (own) |
| Purchase User | read, print, export (all) |
| Purchase Manager | read, cancel, print, export (all) [Assumption] |
| Director | read, print, export (all) |
| Administrator | every applicable right |

- Stores has no cancel: a receipt undone by the person who entered it, with no second look, is the classic hole. Purchase Manager cancels and Stores amends, the same split as the PO (PO spec §10).
- Site restrictions: Suresh sees GRNs and POs for his sites only. `site_id` is copied from the PO, and the PO is visibility-checked.
- A restricted user's first save must carry the site, or `create` refuses with a 404. So step 1 is the PO picker, and its module handler reads the PO, checks it, and creates the draft with site, project and lines (§3.1). A blank PO refuses on that step: `Choose a purchase order.`

## 11. Screens

UX design and approval come first (CD-97). Phone first, no new component expected. `docscreen.FormSteps` puts unstepped fields first and items last, so the order differs from design-system §8.7: Choose PO and receipt date → Delivery details → Received quantities → Check and submit (§14 item 1). The challan is in Suresh's hand when the truck arrives, so this costs little.

| Step | Content |
|---|---|
| 1 Choose PO | Picker: approved, my sites, pending by default (§8). Row shows PO number, vendor, site, pending lines. One visible PO preselects. Receipt date. Its `Save` creates the draft with one line per PO line with pending above 0, or no lines for a fully received PO |
| 2 Delivery details | Challan no., challan date, vehicle, transporter, e-way bill no., photos with `Take photo`. Service-only GRN: `Nothing needed here` except photos, since steps are fixed |
| 3 Received quantities | One card for every PO line, including fully received ones: item, `Ordered 1,200 · Pending 400 kg`, `Received now` prefilled with pending, which is blank at pending 0. `Receive all pending` and `Clear all`. A line cleared to 0 is left out (§3.2). A `Some rejected` disclosure reveals Rejected and reason. More than pending shows the warning in §4 |
| 4 Check and submit | Lines, accepted value, photos, `Submit` |

**Document page** adds the Bill details panel (read for all, form for those with write) and a link to the PO.

**Targets.** Gloves mean the 48 px action bar and a large qty field matter. Measurable goal for the UX document: a five-line GRN on a phone in under a minute.

## 12. Reports and exports

The generic list and CSV cover the GRN register. Two more, each guarded by `read` with `export` checked in the handler, and scope applied:

**Bills to enter** (`/reports/grn/bills`). One row per approved or closed GRN. Columns: GRN no., receipt date, vendor, GSTIN, PO no., challan no., invoice no., invoice date, invoice total, accepted value, photos count. Filters: no bill details yet, vendor, month. Staff key your accounting system from this list; the app doesn't record what has been keyed (CD-234).

**Received items** (`/reports/grn/items`). One row per GRN line: GRN no., date, vendor, PO no., item, HSN, received, accepted, rejected, reason, UoM, rate, accepted value. The rejected filter gives a vendor-quality view without a new feature.

## 13. Cutover

PO spec §12 applies. GRN-specific points:
- GRNs are back-entered with real dates, against back-entered POs, and with no approval rule they approve on submit anyway.
- An open PO's earlier receipts are either real GRNs or `Already received`, never both.
- GRN numbering continues from the last issued GRN number in the FY, or 0.
- Vendor challan can be left blank only on a service-only GRN, so paper GRNs need a challan number. Type `Not recorded` if it's lost.

## 14. Proposed changes for the head

Rulings of 30 October: the `Int` columns with a module handler running `CheckLinkTx`, `CloseRight` and the hooks (CD-237) are accepted for MOD-0.

1. **Module hooks.** Steps 1 and 3 here aren't field forms, so this spec uses `Steps`, `Page`, `Reason` and `NoLinePages` from the hooks spec, whose §3 is authoritative. Design-system §8.7's GRN step order changes to §11's.
2. **`print.GRNDoc` and `GRNLine`** gain the additive fields in §9.
3. **No change, for the record.** `doc.partyOf` shows the first `Required` master link, so a GRN's `Info.Party` is empty. The list shows the vendor column, and a GRN is in the inbox only if an owner adds a rule. Accepted.
4. **`doctype.DocType.CloseRight`.** The action list would offer Close on a GRN, where it means nothing. GRN sets `"none"`, using the field in Expense spec §15 item 1.
5. **Module first step.** A custom first step creates the draft with lines (hooks spec §4.1).

## 15. Open points for owner review

**My assumptions, for review.**
- Every receipt needs a PO. Small local cash buys go on an expense claim.
- One PO per GRN.
- A receipt uses the PO's unit.
- Stores can't cancel a GRN; Purchase Manager can.
- Stores fills bill details, since there's no Accounts role, unless another role gets `write`.
- Photos aren't compulsory.
- A receipt dated before its PO warns, not refuses.

## 16. Test plan

Fixtures: `00-fixtures.md`, the reference PO.

| ID | Behaviour | Go test | P | Level |
|---|---|---|---|---|
| GRN-001 | Only approved, visible POs are in the picker; fully received ones only when widened | `TestGRN_POPicker` | P1 | integration |
| GRN-002 | Posted PO on another site: AUTH-101's 403, on the real `auth.Service` | `TestGRN_PONotVisibleReal` | P1 | integration |
| GRN-003 | Draft copies vendor, site, project and lines from the PO | `TestGRN_CopiesFromPO` | P1 | integration |
| GRN-004 | Changing the PO on a draft with lines is refused | `TestGRN_POFixedOnceLines` | P1 | unit |
| GRN-005 | Line from another PO refused | `TestGRN_ForeignLine` | P1 | integration |
| GRN-006 | Date rules: future and challan-after-receipt refuse; before PO warns | `TestGRN_DateRules` | P1 | unit |
| GRN-007 | Blank challan refused for goods, allowed on a service-only GRN | `TestGRN_ChallanRule` | P1 | integration |
| GRN-008 | Rejected above received; reason required at submit | `TestGRN_RejectRules` | P1 | unit |
| GRN-009 | Accepted = received − rejected; value at the PO's net rate | `TestGRN_AcceptedValue` | P1 | unit |
| GRN-010 | Submit with no rule: approved and numbered `GR/26-27/00001` | `TestGRN_SubmitApproves` | P1 | integration |
| GRN-011 | Partial receipts: PO pending and status step through part to full | `TestGRN_PartialReceipts` | P1 | integration |
| GRN-012 | Rejected quantity stays pending on the PO | `TestGRN_RejectedStaysPending` | P1 | integration |
| GRN-013 | Accepted above pending warns on the step, check and document pages, and submits; received above pending with the excess rejected doesn't warn | `TestGRN_OverDeliveryWarns` | P1 | integration |
| GRN-014 | Over-delivery: PO pending floors at 0, `Over by` shows, status is `fully_received`; a GRN against a fully received PO works | `TestGRN_OverDeliveryPO` | P1 | integration |
| GRN-015 | Two concurrent GRNs both pass; roll-up equals their sum (real engine, non-admin user) | `TestGRN_ConcurrentReceipts` | P1 | integration |
| GRN-016 | Already received counts toward pending and the warning | `TestGRN_AlreadyReceivedCounts` | P1 | integration |
| GRN-017 | Whole-number UoM refuses a fraction; both `RegisterQtyUse` calls wired | `TestGRN_WholeNumber` | P1 | integration |
| GRN-018 | Cancel restores pending and status; reason required | `TestGRN_CancelRestores` | P1 | integration |
| GRN-019 | Cancel with the PO closed: allowed, PO stays closed | `TestGRN_CancelClosedPO` | P1 | integration |
| GRN-020 | Closed PO refuses a new GRN (DOC-017) | `TestGRN_ClosedPORefused` | P1 | integration |
| GRN-021 | PO cancel blocked by the GRN, then allowed after its cancel | `TestGRN_BlocksPOCancel` | P1 | integration |
| GRN-022 | Amend by restricted Suresh: draft visible, snapshots filled | `TestGRN_AmendRestrictedReal` | P1 | integration |
| GRN-023 | Lock off by default; on, it refuses submit and cancel at the boundary date; the day after passes; unlocking clears it | `TestGRN_PeriodLock` | P1 | integration |
| GRN-024 | Cancel refused by lock leaves nothing changed (rollback) | `TestGRN_LockRollsBack` | P1 | integration |
| GRN-025 | Bill details: refused before approval; saved after; audited | `TestGRNBill_StateRules` | P1 | integration |
| GRN-026 | Bill details: stale version 409, no write right 403, closed PO doesn't matter | `TestGRNBill_VersionAndRights` | P1 | integration |
| GRN-027 | Same invoice on two GRNs warns and saves | `TestGRNBill_DuplicateWarns` | P1 | integration |
| GRN-028 | Bill photo added after approval by Stores (D-FILE-06) | `TestGRN_LatePhoto` | P1 | integration |
| GRN-029 | Restricted Suresh creates a GRN for his site; a posted other-site PO gets AUTH-101's refusal; a tampered `site_id` is reset | `TestGRN_RestrictedCreateReal` | P1 | integration |
| GRN-030 | Print golden, with rejected reason and transport details | `TestGRN_PrintGolden` | P1 | integration (golden file) |
| GRN-031 | Bills to enter: filters, scope, CSV needs `export` | `TestGRN_BillsReport` | P1 | integration |
| GRN-032 | Received items report columns read back | `TestGRN_ItemsReport` | P1 | integration |
| GRN-033 | Item disabled after the PO: receipt still succeeds | `TestGRN_DisabledMasterStillReceives` | P1 | integration |
| GRN-034 | Default rights match §10 | `TestGRN_DefaultRights` | P1 | unit |
| GRN-035 | Random GRN sequences: pending never below 0, status matches, roll-up equals the sum | `FuzzGRN_Invariants` | P1 | unit (fuzz) |
| GRN-036 | Phone flow at 360 px, with overflow check | `TestE2E_GRNFlow` | P1 | e2e |

Review focus: GRN-015, GRN-022 and GRN-029 must run as a non-admin on the real services, because tests as an Administrator hid bugs (CD-213). GRN-012 and GRN-016 guard the roll-up.

## 17. Work-package outline

MOD-GRN starts when MOD-PO tasks 1 to 3 are on `main` (DocType, pending, `RefreshReceiptStatus`). High tier for anything touching pending or the roll-up.

| # | Task | Tier | Lane |
|---|---|---|---|
| 1 | Migration, DocType, links, roll-up, `RegisterQtyUse` | High | Serial first |
| 2 | Validate, snapshots, submit rules | High | A |
| 3 | Over-delivery warning and lock; concurrency tests | High | B, after 2 |
| 4 | Effects and `RefreshReceiptStatus` calls | High | B |
| 5 | Bill details | High | C |
| 6 | Print and golden | Standard | D |
| 7 | Reports and CSV | Standard | E |
| 8 | Wizard and document page, after UX approval and §14 item 1 | Standard | F |
| 9 | End-to-end and overflow | Standard | Last |
