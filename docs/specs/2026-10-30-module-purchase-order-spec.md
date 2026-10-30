# Module spec: Purchase Order

Version 0.3 · 30 October 2026 · Status: for owner review, with CD-234 and CD-235 applied · Author: AD-6

Builds on the core platform spec, the design system, the decision log (to CD-229) and `docs/tests/core/`. Where this spec names a kernel rule it does not restate it. Goods Receipt and Expense specs depend on the shared decisions in §14 and the cutover plan in §12.

**How to read the marks.**
- **(CD-234)**, **(CD-235)**: the owner's answers of 30 October.
- **[Assumption]**: a default of mine, listed in §16 for review.
- **CA**: "check with your CA". Facts are from the research of 30 October (sources in §17). They are not legal advice, and the app never decides tax treatment.

---

## 1. Purpose and approach

Priya raises a purchase order, it gets approved, a numbered PO goes to the vendor, and Stores can see what is still due. That replaces the paper or spreadsheet PO and feeds the GRN.

**Three ways to build it**

| | Approach | For | Against |
|---|---|---|---|
| **A (recommended)** | One package per module (`internal/modules/purchase`, `…/goodsreceipt`, `…/expense`) registering real DocTypes on the kernel, with one small shared package (`…/modkit`) for GST-document helpers and settings | Follows spec §1's promise that a module is "two tables, a few rules and a print template"; each module can run in parallel; no kernel surgery | Small amount of shared code to own |
| B | One generic "trade document" DocType with a type column for PO, GRN and later invoices | Fewer tables | GRN and PO have different lines and rights; every rule becomes a type switch; it fights the registry and CD-09 rights |
| C | Build PO, GRN and Expense as one work package | One plan | Loses parallelism, and blocks Expense on purchase questions it doesn't care about |

## 2. Scope

**In scope.** PO for goods and services; one vendor, one delivery site, one project per PO; line discount; GST computed per line; approval by amount; print; partial receipt tracking through the GRN; manual close; cancel and amend; pending-items report; cutover of open POs; settings screen shared with GRN and Expense.

**Non-goals.**
- Purchase requests, RFQs, supplier quotation comparison.
- Rate contracts or blanket POs.
- Stock ledger, stock value, reorder levels.
- Vendor bills, payments, vendor ledger: your accounting system keeps these. Staff re-key entries, and the app tracks nothing about that (CD-228, CD-234).
- Sending a PO from the app. The user prints or saves a PDF (D-FILE-10) and sends it.
- Editing an approved PO (R7), reopening a closed one (R6), header-level discount (`07-numbers-lists.md` deferred table).
- GST filing, TDS computation, self-invoices and payment vouchers for reverse charge (CD-228).
- Freight as a separate charges table: freight is a service line with its own tax code, as in Odoo and Tryton [Assumption].
- Multi-currency (F2), import POs (overseas vendors are refused, §7).

## 3. DocType

Registered in `internal/modules/purchase`: `Name: "purchase.Order"`, `Label: "Purchase Order"`, `PluralLabel: "Purchase Orders"`, tables `purchase_order` and `purchase_order_line` (`AUTOINCREMENT` ids, CD-156; lines have `id`, `parent_id` ON DELETE CASCADE, `line_no`, CD-161), `Series: "PO/{FY}/{#####}"`, `Submittable`, `Approvable`, `Printable`, `HasRoundedTotal`, `OwnerField: "created_by"`, `ProjectFields: ["project_id"]`, `SiteFields: ["site_id"]`.

Every money, quantity and rate is a kernel fixed-point type. Field names are unique across header and lines (CD-151). Wizard steps follow `docscreen.FormSteps` (`internal/ui/docscreen/form.go`): header steps first, then items, then check. So the order is Details → Terms → Items → Check and submit. Fields with no `Step` are on Details, which also makes the first save carry `project_id` and `site_id`, as a restricted user needs.

### 3.1 Header

| Field | Label | Type | Rule |
|---|---|---|---|
| `number` | PO number | Text, read-only (CD-144) | List priority 1 |
| `vendor_id` | Vendor | Link Party | Required. Vendor role (`party.CheckRole`). The name is what MST-043 reads (CD-216 item 8) |
| `vendor_address_id` | Vendor address | Link Address | Default the party's primary billing address; must belong to the vendor (CD-176). Step `terms`, shown as radios of this vendor's addresses by a module hook (§15 item 11), because `/pick/{Link}` can't filter by vendor |
| `site_id` | Deliver to | Link Site | Required (CD-234) |
| `branch_id` | Billed to | Link Branch, read-only | Copied from the site's branch in `Validate` |
| `project_id` | Project | Link Project | Required [Assumption]: a blank project hides the PO from project-restricted users (AUTH-093), and CD-178 keeps a Required restricted field required on drafts. Stock buys use a general project. `Validate` fills a blank from the site's project for an unrestricted user; a restricted user gets `Project is required.` first (CD-178 runs before `Validate`) and picks it |
| `order_date` | Order date | Date | Required, default today. Not in the future. Sets the FY of the number (D-NAM-04) |
| `required_by` | Required by | Date | Optional. Not before the order date [Assumption] |
| `payment_terms_id` | Payment terms | Link Payment Terms | Default the party's terms (MST-050). Step `terms` |
| `vendor_quote_ref` | Vendor quote ref | Text, max 140 | Optional. Step `terms` |
| `terms` | Terms and conditions | Text, max 2,000 | Default from settings (§14.1). Printed. Step `terms` |
| `reverse_charge` | Reverse charge applies | Check | Default off. See §7. Step `terms` |
| `remarks` | Remarks | Text, max 500 | Internal; never printed. Step `terms` |
| `earlier_po_no` | Earlier PO no. | Text, max 40 | Cutover only (§12). Step `terms`, inside a Disclosure |
| `earlier_po_date` | Earlier PO date | Date | Cutover only |
| `net_total` | Taxable value | Money, read-only | Sum of line amounts |
| `cgst_total`, `sgst_total`, `igst_total` | CGST, SGST, IGST | Money, read-only | From `gstcalc.Totals` |
| `grand_total` | Grand total | Money, read-only | Net + tax, or net alone under reverse charge (§7) |
| `rounded_total` | Rounded total | Money, read-only | List priority 2 |
| `receipt_status` | Receipt | Select, read-only: `not_received`, `part_received`, `fully_received` | §6.3. `RollUp`-class system field |
| `state` | State | Select, read-only (CD-144) | List priority 3, filterable |

List columns also show vendor, order date and project (priorities 4 to 6). Filters: state, receipt status, vendor, project, site, order date.

Snapshot columns, not Fields, written in `Validate` while draft (D-MST-03): `snap_vendor_code`, `snap_vendor_name`, `snap_vendor_gstin`, `snap_vendor_category`, `snap_vendor_state`, `snap_vendor_address`, `snap_branch_name`, `snap_branch_gstin`, `snap_place_state`, `snap_site_name`, `snap_terms_name`. They come from `address.PartySnapshot`, `company` and `site`.

### 3.2 Lines (1 to 500 at submit, DOC limits; zero allowed on a draft, R8)

| Field | Label | Type | Rule |
|---|---|---|---|
| `item_id` | Item | Link Item | Required |
| `description` | Description | Text, max 500 | Default item name; printed |
| `uom_id` | UoM | Link UoM | Required; default stock UoM; must have a conversion (CD-199 text). Whole-number check through `uom.CheckLineQty` |
| `qty` | Quantity | Qty | Required, more than 0 |
| `price` | Rate | Rate | Not negative. Default the item's standard rate |
| `discount_pct` | Discount | Percent | Default 0, 0 to 100 (NUM-015) |
| `rate` | Net rate | Rate, read-only | `price` less discount, 4 dp |
| `tax_code_id` | Tax code | Link Tax Code | Required. Default the item's tax code |
| `hsn_sac` | HSN/SAC | Text | Default from the item; editable, validated by item rules |
| `amount` | Amount | Money, read-only | `gstcalc.LineAmount(qty, rate)` |
| `cgst`, `sgst`, `igst` | | Money, read-only | Per line (D-NUM-08) |
| `already_received` | Already received | Qty | Default 0. Not negative, not above `qty`. Cutover only: copies through amend, and is shown only when `earlier_po_no` is set (§12) |
| `received_qty` | Received | Qty, `RollUp` | Sum of accepted quantities on approved and closed GRN lines |

`uom.RegisterQtyUse` is called from the package `init` with `purchase_order_line`, `parent_id`, `uom_id`, `qty`.

Line snapshot columns: `snap_item_code`, `snap_item_name`, `snap_uom_code`, `snap_factor`, `snap_stock_qty`, `snap_gst_rate`, `snap_tax_code`.

**Pending quantity** is never stored: `max(0, qty − already_received − received_qty)`.

## 4. Validation

`Hooks.Validate` runs on every draft save, reads inside the transaction (CD-94), stores snapshots and totals, and returns field errors. Required fields are checked at submit, not on drafts (CD-178). Master-link wording belongs to DOC and MST (CD-38, CD-170); the module adds only these messages. Each covers exactly its own condition (CD-147).

| Condition | Message | When |
|---|---|---|
| Order date after today | `Order date can't be in the future.` Past dates are always allowed, so back-entry works (§12) | Save |
| Required-by before order date | `Required by can't be before the order date.` | Save |
| Site's project differs from the PO's project | `Site {code} is on project {code}. Pick that project or another site.` | Save |
| Overseas vendor | `Overseas vendors aren't supported on purchase orders yet.` | Save |
| Vendor's state unknown | NUM-038's text | Save |
| Branch's state unknown | NUM-039's text | Save |
| Already received above quantity | `Row {n}: Already received can't be more than the quantity.` | Save |
| Earlier PO no. blank while a line has Already received | `Earlier PO no. is required when a line has Already received.` | Save |
| Earlier PO date with no Earlier PO no. | `Earlier PO no. is required with an earlier PO date.` | Save |
| Tax code disabled | CD-170 pattern | Save |
| Reverse charge on with a Nil-Rated, Exempted or Non-GST line | `Row {n}: Reverse charge needs a taxable tax code on every line.` | Submit |

`BeforeSubmit` re-reads what can change under a pending document: vendor still enabled, site and project still valid, and the line UoM conversions. The kernel re-runs validation on approve (D-APR-15), so a document pending when a master is disabled fails at approve with the kernel's text.

## 5. States and approvals

The kernel's single state machine (spec §3.2), unchanged.

| Action | Who (defaults) | Module work |
|---|---|---|
| Save, submit, withdraw, revise, close | Purchase User | §4; close asks a reason (3 to 500 characters) |
| Approve, reject | Purchase Manager, Director per the approval rule | None beyond re-validation |
| Cancel | Purchase Manager | Blocked while a GRN is pending, approved or closed (DOC-052) |
| Amend | Purchase User | Copies everything, including `already_received` |
| Delete draft | Purchase User, own | Kernel |

**Approval.** `Hooks.Amount` is `rounded_total`, or under reverse charge the same figure plus the tax (D-APR-03: the amount includes taxes, and the tax is still a cost). Rules are data, entered at go-live (§12). The test seed's ₹50,000 and ₹5,00,000 are not assumed for production. A PO below every rule goes straight to approved and gets its number on submit (F5), with no second person. The owner should know this when setting thresholds.

**Correcting a PO.**
- Nothing received: cancel and amend.
- Part received: close it with a reason and raise a new PO for the remainder. This is the fast path and the one the screen suggests.
- A rate wrong on lines already received: cancel the GRNs, cancel the PO, amend, receive again. Slow but honest; R7 stays.

## 6. Numbering, receipts and closing

### 6.1 Numbering

`PO/{FY}/{#####}`, 14 characters, per D-NAM-01 to -15: assigned at first approval, gapless, one counter per rendered prefix, FY from `order_date`, amendments `PO/26-27/00042-1` to `-9`. No module code beyond `Hooks.Date`. Cutover in §12.

### 6.2 Effects

A PO has no `ApplyEffects` work of its own. Hooks return nil; `ReverseEffects` likewise, since the kernel blocks cancel while a GRN exists.

### 6.3 Receipt status

`receipt_status` is what buyers filter by. The kernel's roll-up writes only the target line and runs no target hook, and `ApplyEffects` runs before the roll-up (`internal/kernel/doc/transitions.go`, `enterApproved`). So:

- `purchase.RefreshReceiptStatus(ctx, tx, poID, accepted map[int64]types.Qty) error` is exported and takes each PO line's accepted total as the caller computed it.
- GRN's `ApplyEffects` and `ReverseEffects` call it, after the kernel has set the GRN's state. The GRN spec owns the call.
- `not_received`: no line has anything received. `fully_received`: every line's pending is 0. Otherwise `part_received`.
- `already_received` counts, so a cutover PO starts `part_received` from its first save.

A fully received PO stays `approved`. There is no automatic close, because the kernel has no system-initiated transition and no system needs one (ERPNext derives Completed in the same way). Reports treat its pending as 0.

### 6.4 Closing with quantity pending

Close means "nothing more is coming". After close, pending shows 0 in reports, new GRNs are refused with DOC-017's text, and a GRN against the closed PO can still be cancelled (D-DOC-09). A closed PO can't be reopened (R6). The screen warns before closing: `{n} lines still have {x} pending. Closing means no more goods can be received against this order.`

## 7. GST

**Decided here (closes the audit items for the PO).**

| Item | Decision | CA |
|---|---|---|
| Place of supply | Computed. Supplier state from the GSTIN or address (NUM-038). Place state from the delivery site's branch (`snap_place_state`), which assumes a site is in its branch's state (CD-234). `gstcalc.IsInterStateSupply` decides | CA: the legal test is supplier location against place of supply (IGST s.8), and goods are supplied where movement ends (s.10(1)(a)) |
| Place-of-supply reason (CD-200 moved it here) | New `gstcalc.PlaceReason(category, supplierName, supplierState, placeState string) string`, shown on the check page and the document page, printed on the PO. Texts in §7.1 | |
| Vendor invoice no., IRN, e-way bill | Not on the PO. Those arrive with the goods; see the GRN spec (CD-234) | |
| Reverse charge | A tick on the PO, off by default, chosen by the buyer. Never worked out by the app. Tax is computed and shown, marked as payable by the buyer, and left out of the grand total. Self-invoice and payment voucher stay in your accounting system (CD-234) | CA: whether it applies depends on the service and the supplier's status (Notification 13/2017-CT(R), as amended) |
| TDS | Not in the app. A PO is neither a credit nor a payment, so it isn't the point of deduction; rate depends on payee and nature of payment (CD-234) | CA: Income-tax Act 2025 s.393 |
| GST TDS (s.51) | Nothing to do: a private company is not a deductor | CA to confirm |
| Blocked credit (s.17(5)) | Not on the PO. Credit is decided in your accounting system | |

**Reverse charge on the check page and print:** `Reverse charge applies. GST of ₹{x} is payable by us, not to the vendor.`

**Not done, on purpose.** The app doesn't refuse a taxable code on an unregistered or composition vendor, or warn about it. Those are tax judgements with exceptions, and CD-228 keeps tax decisions out of the app. CA may want this later.

### 7.1 Place-of-supply reason

Design-system §8.3 says "Intra-state: supplier and branch are both in Maharashtra". The research shows "branch" is not the legal test, so this spec proposes the following wording and a change to §8.3 (§15 item 4).

| Case | Text |
|---|---|
| Same state | `Intra-state: the supplier and the delivery site are both in Maharashtra.` |
| Different | `Inter-state: the supplier is in Karnataka and the delivery site is in Maharashtra.` |
| SEZ supplier | `Inter-state: Shree Steel Traders is an SEZ supplier.` |

## 8. Links to masters

| Link | Rule |
|---|---|
| `vendor_id` | Party with vendor role; picker uses the party role filter (CD-218 `LinkRole`) |
| `vendor_address_id` | Must belong to the vendor (CD-176). Cleared when the vendor changes, then defaulted again |
| `site_id` | Enabled. Picker respects site restrictions |
| `project_id` | Enabled, respecting project restrictions |
| `payment_terms_id`, `item_id`, `uom_id`, `tax_code_id` | Enabled; DOC's disabled wording |
| Changing vendor or site on a draft | Recomputes the split (NUM-041) |

**Defaults.** Every default in this spec is applied by `Validate` on save when the field is blank, as `mstfixture` already does for expense lines (MST-123). The user sees the value when the line or step comes back, and can change it. A value cleared on purpose comes back; accepted, because each is either required or harmless. To print no terms, type a dash. No fragment hook is needed. Line defaults need the item, so they apply on `Save line`.

Masters keep showing on approved POs through snapshots (MST-011). A master used by a PO can't be deleted (MST-014).

## 9. Print

`print.TradeDocument` through `print.Register`, title `Purchase Order`, DRAFT and CANCELLED watermarks from the kernel (D-FILE-14). Golden `po_approved` updates.

Additions to `print.TradeDoc`, all additive and omitted when empty (FILE-064): `DeliverTo []string`, `Refs []DocRef` where `DocRef` is a new `struct{ Label, Value string }` (Required by, Payment terms, Vendor quote ref, Earlier PO no.), `PlaceReason string`, `ReverseChargeNote string`. `print` is FILE's, so this goes to the head as a small contract change (§15 item 5).

The print shows net rate and amount, not the discount. HSN/SAC is shown. CGST and SGST rows or an IGST row come from the snapshot. A PO isn't a tax invoice, so Rule 46's invoice particulars don't apply to it (CA to confirm).

## 10. Rights and restrictions

Production `DefaultRights`, matching `00-fixtures.md`:

| Role | Rights (scope) |
|---|---|
| Purchase User | read, create, write, submit, amend, print, export (all); delete (own) |
| Purchase Manager | read, approve, cancel, print, export (all) |
| Director | read, approve, print, export (all) |
| Stores | read (all) |
| Administrator | every applicable right |

- Restrictions apply through `ProjectFields` and `SiteFields`, so Priya sees only PRJ-METRO POs and Suresh only his sites'. Both dimensions apply together (AUTH-095).
- CD-10 gives each creating role `read` on the linked masters.
- `amend` moves from Purchase Manager to Purchase User, a change from the fixture seed for `fixture.Order`. The amender becomes `created_by`, and approval eligibility excludes the creator as well as the requester (`internal/kernel/approval/eligible.go`, `own`). If Ravi amended, nobody at his rule level could approve, and a company with one manager couldn't submit the correction. This way Ravi cancels, Priya amends and Ravi approves.

## 11. Screens

New screens need a UX design document and owner approval before plan tasks (CD-97, protocol §9). This section is the brief for it, not the design. Everything is built from design-system §7 components through `docscreen`, with no new component expected. `docscreen` has no per-DocType hook today, so the additions below go through §15 item 11, built in MOD-0.

| Screen | URL | Notes |
|---|---|---|
| List | `/doc/purchase.Order` | §8.1 cards then rows; filters per §3.1; `New purchase order` |
| Wizard | `/doc/purchase.Order/{id}/edit/{step}` | Steps: Details → Terms → Items → Check and submit. Items uses add-to-a-list on phones and the fitting grid from 64rem (§8.2) |
| Check and submit | last step | Totals, place reason, reverse-charge note, amount in words, approval preview |
| Document page | `/doc/purchase.Order/{id}` | §8.4, plus a pending column on each line (Ordered, Received, Pending) and linked GRNs, read through `doc.LinkedBlockers` so `purchase` never imports `goodsreceipt` |
| Approval page | as §8.5 | Same data; no module work |
| Close | reason page | Single page with the pending warning (§6.4). Not a dialog: §7 keeps reasons out of dialogs |
| Pending items report | `/reports/purchase/pending` | §13 |
| Business settings | `/settings/business` | Single page, Administrator only (§14.1) |

**Phones.** Priya works mostly on desktop, and Ravi and Meena approve on phones, so the approval page is the phone-critical one. Long values must pass the overflow gate.

## 12. Cutover plan

Owner decisions: go live as soon as the modules are ready (CD-228). Open POs are on paper or email. The year's back-dated data goes in first, and then the app goes live (CD-234). This plan covers all three modules.

**Why not type received quantities into `received_qty`.** The roll-up recomputes that column as a sum over GRN lines (D-DOC-13), so the first real GRN would wipe an imported value. ERPNext has the same hazard and none of the three systems studied has a coded answer. That's why `already_received` is a separate, plain field added into pending.

**What makes back-entry work with no kernel change**

| Concern | How it works |
|---|---|
| Dates | Only future dates are refused. Number, FY and print follow the document's own date (D-NAM-04). Nothing compares a date with today beyond that |
| Numbering | The FY in the number comes from the document date, so a date before 1 April 2026 gets a `25-26` prefix with its own counter. Numbers follow entry order, not date order |
| Approvals | **No approval rules exist during back-entry** (CD-235). With no rule, submit goes straight to approved and numbers the document (F5), so nobody re-approves a year of history. Rules go in on go-live day. Print shows the person who entered it as the approver, and the audit shows the real entry time |
| Period lock | Off by default (§14.2) |
| Receipts on old claims | The reason field takes `Paper file` where no photo exists |

**Steps**

| When | Step | Who |
|---|---|---|
| Set-up | Masters CSV import, checked with the dry run | Masters Manager |
| Set-up | Link each user to an employee; assign roles and restrictions. **No approval rules yet** | Administrator |
| Set-up | `app admin create` before ports open (CD-228); restore drill against S3 (CD-226) | Owner with Claude Code |
| Set-up | Set each series' last number used (NAM-056): `PO/26-27/` to the last paper PO, `GR/26-27/` and `EX/26-27/` the same, or 0 if none. Any earlier FY entered needs its own prefix set (NAM-057 refuses a lower number) | Administrator |
| Back-entry | Enter the year's POs with their real dates. The paper number goes in `Earlier PO no.`. Enter GRNs against them with real dates, and claims and advances (only an Administrator enters claims and advances for someone else, CD-235) | Purchase User, Stores, Administrator |
| Back-entry | An open PO with goods already received: either enter its GRNs, or fill `Already received` per line. Never both, or the goods count twice | Purchase User |
| Back-entry | Check each PO's pending against the paper, using the pending items report | Purchase User |
| Go-live day | Enter production approval rules, after confirming two active Fallback Approvers (Expense spec §6). From this moment every new document follows them | Administrator |
| Go-live day | Paper stops. New POs, GRNs and claims go in the app | Everyone |
| After go-live | The Administrator may turn on the period lock (§14.2) | Administrator |
| FY rollover | The first PO dated 1 April uses a new prefix and starts at 1 (NAM-059 can preset) | Automatic |

**Consequences to accept.**
- The printed number on a re-entered PO is new. The vendor has the old number, so the print shows `Earlier PO no.`.
- App numbers don't follow date order in the back-entered range, so a list sorted by date shows numbers out of order. Nothing depends on that order.
- Anything submitted during back-entry is approved at once. Keep the window short, and don't start real new work until the rules are in.
- Paper stays where it is. CA: GST records must be kept 72 months from the annual return's due date (CGST s.36).

**Abort.** Until the first real PO goes to a vendor, the database can be deleted and the cutover rerun. After that, fix forward through cancel and amend.

## 13. Reports and exports

The generic list and CSV export cover the PO register, with `export` right and scope (AUTH-090, CD-145). One new report:

**Pending items** (`/reports/purchase/pending`, CSV at `…/export.csv`). One row per PO line with pending above 0, on approved POs. Columns: PO number, order date, required by, vendor, project, site, item code, item name, ordered, received (including already received), pending, UoM, net rate, pending value. Overdue rows (required-by before today) carry the word `Overdue`, not just colour. Filters: vendor, project, site, item, overdue only. Guarded by `read` with the export right checked in the handler; rows use `Scope`, so restrictions apply and it can't widen access (AUTH-110).

Out for v1: spend analysis, vendor performance, delay reports. All of them can be done from the CSV.

## 14. Shared decisions (apply to PO, GRN and Expense)

### 14.1 Settings

A single-row `business_settings` table in `modkit`, one Administrator-only page, every change audited, with a `version` posted by the page (CD-94). Fields: `locked_up_to` Date, blank by default; `default_po_terms` Text. Administrator is the role the owner calls Super admin (head, 30 October).

### 14.2 Period lock (CD-234)

- Off by default: `locked_up_to` is blank. The year's back-dated data goes in first (§12).
- An Administrator locks a period by setting the date and unlocks it by moving the date back or clearing it. Both are audited. One date covers everything on or before it, so unlocking one month in the middle of a year isn't possible without unlocking what follows [Assumption].
- GRN, Expense Claim and Employee Advance documents dated on or before it can't be submitted, approved or cancelled. Close stays allowed. The check is `modkit.CheckNotLocked(ctx, tx, date)`, called from `BeforeSubmit` and `ReverseEffects`, inside the transaction, so it needs no kernel change.
- Message: `{Label} {number} is dated {date}, which is locked. The period is locked up to {lock date}.`
- POs are exempt: they aren't tax documents, and ERPNext's default closed-documents list excludes Purchase Order too.
- No role bypasses a lock while it stands, because the Administrator can move the date.
- CA chooses the date, for example the last filed GSTR-3B. There's no legal "locked period" for purchase documents; statutory cut-offs such as 30 November (CGST s.16(4)) are anchors only.

### 14.3 Reminders (CD-234)

In-app only. No email, so D-APR-13 stands.

- **My requests** shows `Waiting {n} days` on a pending request from the third day, using `approval.RequestItem`'s submitted time.
- **Overdue approvals**: a new Administrator page beside Stuck approvals, listing requests open more than 3 days, oldest first, with who can approve. `(*Approvals).Overdue(ctx, p)` is additive; its refusal for a non-administrator gets its own message: `You don't have permission to see overdue approvals. Ask an Administrator.` (CD-152's rule).
- 3 days is a constant [Assumption]. Calendar days, because the app has no holiday list.
- An approver who never opens the app is still not told; the requester and Administrator see it and act. That's the known cost of this choice.

### 14.4 No accounting tracking (CD-234)

The app holds no `Entered` mark, no Accounts role, and no payment record. Staff re-key from the reports in each spec. Nothing stops an entry being keyed twice in your accounting system.

### 14.5 Package and data conventions

- `internal/modules/modkit`, `purchase`, `goodsreceipt`, `expense`. Modules import masters and kernel; a module imports only earlier modules (spec §4.2 rule 4): `goodsreceipt → purchase`.
- Migrations: `10000–10049` modkit, `10050–10199` purchase, `10200–10349` goodsreceipt, `10350–10499` expense.
- Each module has its own `sqlc.yaml` and `dbq/`.
- `internal/app` registers routes, approval seeds and series. `EnsureSeries` already runs for every registered series (CD-28).

## 15. Proposed changes for the head

I don't edit the core spec or the decision log. Rulings of 30 October: items 8 and `CloseRight` and the FILE changes are accepted in principle for MOD-0; item 11 needs its design pass (`docs/specs/2026-10-30-module-docscreen-hooks-spec.md`); moving `amend` is accepted. The rest await the head's ruling with all three specs in. Proposed CD entries:

1. **Module packages and migration ranges** as §14.5; spec §4.1 gains `internal/modules/`, and §4.5 splits `10000+`.
2. **Import rule row** for `internal/modules/*` in `TestContracts_KernelImportsFollowTable`, per plan checklist item 8.
3. **`checktestids`** reads only `docs/tests/core/0*.md`. Either module test files go there as `08-`, `09-` and `0a-`, or the checker gains `docs/tests/modules/*.md`. I recommend the second, with areas `PO`, `GRN`, `EXP`.
4. **Design-system §8.3** place-of-supply wording changes to §7.1.
5. **`print.TradeDoc`** gains the additive fields in §9.
6. **`gstcalc.PlaceReason`** (NUM, additive), which CD-200 deferred to the first module with GST lines.
7. **Approval additions** (additive): `(*Approvals).Overdue`, waiting days on My requests, and the Overdue approvals page.
8. **Amend runs `Validate`.** `amend` copies only stored fields and calls no hook (`internal/kernel/doc/terminal.go`), so a new draft has no snapshots until its next save, and it opens on the check page. Proposal: `amend` calls `Hooks.Validate` once after the copy and ignores its errors, since a draft may hold problems. Every module with snapshots needs it.
9. **Spec §1 and §11 and BRIEF** still say Zoho sync is the first module after Expenses; CD-228 says it's decided after the modules are in use.
10. **`docs/ux/` design documents** for PO, GRN and Expense: one `crew-ux-designer` task per module once these specs are approved.
11. **`docscreen` module hooks (additive, UI's code, in MOD-0).** `docscreen.Deps.Modules map[string]docscreen.Module`, each optional: `StepBody(slug)` and `StepPost(slug)` (a step the module renders and saves through `Engine.Do`, inside docscreen's frame, version and navigation; a module's first step may create the draft with lines), `CheckExtra` (place reason, reverse-charge note), `PageExtra` (pending column, linked GRNs, bill panel, over-delivery note), `ReasonNote(action)` (close warning), `HideField` (`already_received`, earlier-PO fields until wanted). Modules without an entry behave as today.
12. **Fixture seed.** `amend` moves as §10 says (ruled). `00-fixtures.md` moves with it, so tests and production agree.

## 16. Open points for owner review

The owner has answered both batches (CD-234, CD-235).

**My other assumptions, for review.**
- Project is required on every PO; stock buys use a general project.
- Freight and similar charges are service lines.
- Required-by is on the header only, not per line.
- Rates on a PO have no approval check against the item's standard rate.
- Purchase User amends, not Purchase Manager (§10).
- A PO below every approval rule is approved by the person who raised it.
- One lock date, not a list of periods (§14.2).
- 3 calendar days before a request counts as overdue.

## 17. Sources

**Kernel.** Spec §3.2, §4.3, §5; `internal/kernel/doc/rollup.go`, `transitions.go`, `links.go`; `internal/masters/party/rules.go:20`; `internal/masters/uom/qtyuse.go:24`; CD-156, 157, 178, 216, 228.

**Patterns** (paths relative to `reference/`). ERPNext status and percentages `erpnext/erpnext/controllers/status_updater.py:88`, `:689`; Update Items `erpnext/erpnext/accounts/services/child_item_update.py`; PO Analysis `erpnext/erpnext/buying/report/purchase_order_analysis/purchase_order_analysis.py:261`; Odoo and Tryton have no charges table on the PO (`odoo/addons/purchase/models/purchase_order_line.py:39`, `tryton/modules/purchase/purchase.py:1325`); closed documents list `erpnext/erpnext/hooks.py:391`.

**Law (CA to confirm).** CGST Act ss.16, 17(5), 36; IGST Act ss.8, 10, 12; CGST Rules 46, 56, 138; Notification 13/2017-CT(R) as amended; Circular 170/02/2022; Income-tax Act 2025 ss.393, 402 (https://egazette.gov.in/WriteReadData/2025/265620.pdf); CBIC repository https://taxinformation.cbic.gov.in/content/html/tax_repository/gst/. The 57th GST Council recommendations of 8 October 2026 are not yet law and were not designed for.

## 18. Test plan

Module cases follow `docs/tests/core` style: one Go test per id, P1 must pass before go-live. Full Given/When/Then text goes in the test file once this spec is approved. Fixtures: `00-fixtures.md`. The reference PO gives the worked numbers: taxable ₹79,800.00, CGST and SGST ₹7,182.00 each, grand total ₹94,164.00.

| ID | Behaviour | Go test | P | Level |
|---|---|---|---|---|
| PO-001 | Reference PO totals to the paisa, intra-state | `TestPO_ReferenceTotals` | P1 | integration |
| PO-002 | Delivery to a BLR site from SHREE gives IGST, same grand total | `TestPO_InterStateSplit` | P1 | integration |
| PO-003 | Changing vendor on a draft recomputes the split (NUM-041) | `TestPO_VendorChangeRecomputes` | P1 | integration |
| PO-004 | SEZ vendor is inter-state in the same state | `TestPO_SEZInterState` | P1 | integration |
| PO-005 | PlaceReason texts for all three cases, on the check page and print | `TestPO_PlaceReasonShown` | P1 | integration |
| PO-006 | Line discount gives the net rate (NUM-015) and amount | `TestPO_LineDiscount` | P1 | unit |
| PO-007 | Snapshots hold after the vendor, address and item change | `TestPO_SnapshotsSurviveMasterEdits` | P1 | integration |
| PO-008 | Non-vendor party refused with MST's text | `TestPO_VendorRoleRequired` | P1 | integration |
| PO-009 | Address of another party refused (CD-176) | `TestPO_AddressOwner` | P1 | integration |
| PO-010 | Site on another project refused | `TestPO_SiteProjectMismatch` | P1 | integration |
| PO-011 | Blank project defaults from the site for an unrestricted user | `TestPO_ProjectDefaultsFromSite` | P2 | integration |
| PO-012 | Required project stays required on a draft for a restricted user (CD-178) | `TestPO_RestrictedDraftNeedsProject` | P1 | integration |
| PO-013 | Priya sees only PRJ-METRO POs, on the real `auth.Service` | `TestPO_ProjectRestrictionReal` | P1 | integration |
| PO-014 | Suresh opens a PO only for his sites | `TestPO_SiteRestrictionReal` | P1 | integration |
| PO-015 | Whole-number UoM refuses 2.5 Nos | `TestPO_WholeNumberUoM` | P1 | integration |
| PO-016 | `RegisterQtyUse` is wired: turning whole-number on with a fractional PO line refuses (MST-092) | `TestPO_QtyUseRegistered` | P1 | integration |
| PO-017 | Overseas vendor refused | `TestPO_OverseasRefused` | P1 | integration |
| PO-018 | Future order date refused; required-by before order date refused | `TestPO_DateRules` | P1 | unit |
| PO-019 | ₹94,164.00 needs Purchase Manager; number on approve only | `TestPO_ApprovalAndNumber` | P1 | integration |
| PO-020 | ₹5,00,000 goes to Director (highest rule) | `TestPO_DirectorThreshold` | P1 | integration |
| PO-021 | Below every rule: approved and numbered on submit | `TestPO_NoRuleAutoApproved` | P1 | integration |
| PO-022 | Self-approval refused for a manager who raised it | `TestPO_NoSelfApproval` | P1 | integration |
| PO-023 | Number FY comes from the order date, 31-03-2026 gives 25-26 | `TestPO_NumberFYFromOrderDate` | P1 | integration |
| PO-024 | Amendment numbered `-1`, copies lines and `already_received`; Ravi can approve Priya's amendment | `TestPO_AmendCopies` | P1 | integration |
| PO-025 | Cancel blocked by an approved GRN, naming it | `TestPO_CancelBlockedByGRN` | P1 | integration |
| PO-026 | Reverse charge: tax shown, not in grand total; Amount includes it | `TestPO_ReverseChargeTotals` | P1 | integration |
| PO-027 | Reverse charge refused on a nil-rated line at submit | `TestPO_ReverseChargeNeedsTaxable` | P1 | integration |
| PO-028 | Reverse-charge note on the print and check page | `TestPO_ReverseChargePrint` | P1 | integration |
| PO-029 | Pending = ordered − already received − received, never below 0 | `TestPO_PendingFormula` | P1 | unit |
| PO-030 | `RefreshReceiptStatus` gives all three values; already received counts | `TestPO_ReceiptStatus` | P1 | integration |
| PO-031 | A real GRN doesn't overwrite `already_received` | `TestPO_OpeningSurvivesRollUp` | P1 | integration |
| PO-032 | Already received above qty refused; needs Earlier PO no. | `TestPO_AlreadyReceivedRules` | P1 | unit |
| PO-033 | Closing part-received: pending shows 0, GRN refused with DOC-017's text | `TestPO_CloseStopsReceipts` | P1 | integration |
| PO-034 | Stale PO page: version conflict on every write (CD-94) | `TestPO_StaleVersion` | P1 | integration |
| PO-035 | Print golden: letterhead, deliver-to, refs, totals, words | `TestPO_PrintGolden` | P1 | integration (golden file) |
| PO-036 | Print needs `print` and visibility (R3) | `TestPO_PrintRights` | P1 | integration |
| PO-037 | Pending report totals, overdue word, scope filtered | `TestPO_PendingReport` | P1 | integration |
| PO-038 | Pending report CSV needs `export` (CD-145) | `TestPO_PendingExportRight` | P1 | integration |
| PO-039 | Settings: only Administrator, audited, stale refused | `TestSettings_AdminOnlyAudited` | P1 | integration |
| PO-040 | Cutover: counter set to 437 gives first number 00438; lower refused | `TestPO_CutoverCounter` | P1 | integration |
| PO-046 | Back-entry: a PO dated 15-04-2026 with no rules is approved on submit, FY 26-27; dated 31-03-2026 takes prefix 25-26 and its own counter | `TestPO_BackEntry` | P1 | integration |
| PO-047 | Amend produces a draft with snapshots filled | `TestPO_AmendFillsSnapshots` | P1 | integration |
| PO-041 | Default rights match `00-fixtures.md` for every role | `TestPO_DefaultRights` | P1 | unit |
| PO-042 | `Waiting 3 days` appears on My requests on day 3, not day 2, and goes on decision | `TestApprovalWaiting_Days` | P1 | integration |
| PO-043 | Overdue approvals: Administrator only, own refusal text, oldest first | `TestApprovalOverdue_List` | P1 | integration |
| PO-044 | Random lifecycle with GRN sequences, over-delivery included: pending never below 0, status matches | `FuzzPO_ReceiptInvariants` | P1 | unit (fuzz) |
| PO-045 | Full PO flow on a phone width, with overflow check | `TestE2E_PurchaseOrderFlow` | P1 | e2e |

**Which WP runs what.** PO-025, PO-031, PO-033 and PO-044 need a real GRN. They are MOD-GRN's exit criteria, as CD-40 did for cases needing another WP. MOD-PO's own exit is every other P1 case. No test-local stand-in for the GRN (lenient doubles, CD-125).

Review focus: PO-012 and PO-013 must run on the real `auth.Service`, since passing only on authfake hides differences. PO-031 and PO-044 guard the one hazard this spec adds.

## 19. Work-package outline

Gates are milestones on `main`. All WPs follow the execution protocol; High tier for anything computing money or writing across documents. Screen tasks wait for an approved UX design (CD-97).

| Stage | Package | Owns | Depends on | Parallel with |
|---|---|---|---|---|
| 0 | **MOD-0** shared | `internal/modules/modkit`, settings page, `gstcalc.PlaceReason`, `print.TradeDoc` additions, `docscreen` module hooks (§15 item 11), amend calling `Validate`, `CloseRight` (Expense spec §14), overdue approvals, checker, import table | `core-v1` wiring (WP3 Task 2) | none |
| 1 | **MOD-PO** | `internal/modules/purchase` | MOD-0 | MOD-EXP |
| 2 | **MOD-GRN** | `internal/modules/goodsreceipt` | MOD-PO tasks 1 to 3 | MOD-EXP |
| 1 | **MOD-EXP** | `internal/modules/expense` | MOD-0 | MOD-PO, MOD-GRN |
| 4 | **MOD-CUT** | back-entry runbook, go-live rehearsal on a copy | All | none |

**MOD-PO tasks.**

| # | Task | Tier | Lane |
|---|---|---|---|
| 1 | Migration, DocType, default rights, `RegisterQtyUse`, schema tests | High | Serial first |
| 2 | Validate: links, snapshots, GST, totals, reverse charge | High | A |
| 3 | Pending and `RefreshReceiptStatus` | High | B |
| 4 | Print view and golden | Standard | C |
| 5 | Pending report and CSV | Standard | D |
| 6 | Wizard and document page, from the UX design | Standard | E, after design approval |
| 7 | End-to-end flow and overflow | Standard | Last |

Tasks 2 to 5 share a Go package, so each runs in a lane worktree. Plan check before dispatch per `docs/engineering-lessons.md` §1: every name compiled against `main`, every literal under 14 characters where it renders a number, every message with its own text.
