# Module spec: Purchase Order

Version 0.5 · 31 October 2026 · Status: v0.4 accepted (CD-238); the v0.5 additions (§20 to §25, CD-246) are for owner review · Authors: AD-6 (v0.4), head director (v0.5)

Builds on the core platform spec, the design system, the decision log (to CD-229) and `docs/tests/core/`. Where this spec names a kernel rule it does not restate it. Goods Receipt and Expense specs depend on the shared decisions in §14 and the cutover plan in §12.

**What changed in v0.5.** The owner asked for a fuller module, modelled on Zoho Books and other SME ERPs, that stays simple and never turns PO creation into a fight between people (31 October). The research (§17, "SME ERPs") and the owner's four answers (CD-246) add: Purchase Requests (§20), one owner per document with hand-over (§21), internal comments (§22), sending to the vendor and the To send queue (§23), copy and last-price defaults (§24), and a Purchase home with work queues (§25). Sections 2, 3, 4, 10, 11, 15, 16, 18 and 19 point to them. Nothing in v0.4 is removed except two non-goals.

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

**In scope.** Purchase requests from site staff, pulled into POs by a buyer (§20); one owner per draft, with hand-over (§21); internal comments (§22); Mark as sent and the To send queue (§23); copy a PO and last-price defaults (§24); a Purchase home with work queues (§25); PO for goods and services; one vendor, one delivery site, one project per PO; line discount; GST computed per line; approval by amount; print; partial receipt tracking through the GRN; manual close; cancel and amend; pending-items report; cutover of open POs; settings screen shared with GRN and Expense.

**Non-goals.**
- RFQs, supplier quotation comparison. (Purchase requests are in scope from v0.5, §20.)
- Rate contracts or blanket POs.
- Stock ledger, stock value, reorder levels.
- Vendor bills, payments, vendor ledger: your accounting system keeps these. Staff re-key entries, and the app tracks nothing about that (CD-228, CD-234).
- Emailing a PO from the app. The user downloads or shares the PDF (D-FILE-10) and records that it was sent (§23).
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
| `vendor_address_id` | Vendor address | Link Address | Default the party's primary billing address; must belong to the vendor (CD-176). Step `terms`, shown as radios of this vendor's addresses by a custom step (§15 item 11), because `/pick/{Link}` can't filter by vendor |
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
| `copied_from_no` | Copied from | Text, max 40, not read-only (the kernel refuses read-only values on create, `doc/validate.go` `checkReadOnly`) | Hidden by `Module.Hide`, so a crafted post is dropped. Set only by Copy (§24.1) to the source's display (`naming.Label`); shown on the check and document pages through `PageExtra.Pairs`, never printed |
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
| `already_received` | Already received | Qty | Default 0. Not negative. Above `qty` warns only (CD-234): `Row {n}: Already received is more than the quantity.` Cutover only: copies through amend, and is shown only when `earlier_po_no` is set (§12) |
| `received_qty` | Received | Qty, `RollUp` | Sum of accepted quantities on approved and closed GRN lines |
| `request_line_id` | Request row | Int, not read-only (like `fixture.Receipt`'s `order_line_id`) | Hidden by `Module.Hide`. Set only by Create PO from a request and Add from requests (§20.4). `LinkSpec{Field: "request_line_id", Target: "purchase.Request", OnLine: true}`. No `RollUp`: ordered quantities are the module's (§20.3) |

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
| Earlier PO no. blank while a line has Already received | `Earlier PO no. is required when a line has Already received.` | Save |
| Earlier PO date with no Earlier PO no. | `Earlier PO no. is required with an earlier PO date.` | Save |
| Tax code disabled | CD-170 pattern | Save |
| Reverse charge on with a Nil-Rated, Exempted or Non-GST line | `Row {n}: Reverse charge needs a taxable tax code on every line.` | Submit |
| Request rows: the five conditions in §20.5 | §20.5's texts | Save and submit |

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
| Purchase User | read, create, amend, print, export (all); write, submit, delete (own) (§21, CD-246). `purchase.Order`'s `CloseRight` is `amend`, so any Purchase User closes an approved PO, as in v0.4; `amend` has no other effect on an approved PO (CD-247) |
| Purchase Manager | read, approve, cancel, print, export (all); hand over any draft (§21.2) |
| Director | read, approve, print, export (all) |
| Stores | read (all) |
| Administrator | every applicable right |

- `write` and `submit` are `own` from v0.5, so only a draft's owner edits, submits, withdraws or revises it (§21). Rights for Purchase Request are in §20.6.
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
| Purchase home | `/modules/purchase` | Work queues with counts (§25). The nav's Purchase entry opens it |
| Request list, wizard, page | `/doc/purchase.Request…` | §20.7. Phone first: site staff raise requests on phones |
| Add from requests | Items step of a PO, custom step body | §20.4 |
| Owner line, Hand over | Document page card; `/modules/purchase/orders/{id}/handover` | §21 |
| Comments | Document page card on PO and Request; `/modules/comments/{doctype}/{id}` | §22 |
| Send to vendor | `/modules/purchase/orders/{id}/send` | §23 |
| Copy | Button on the document page, `/modules/purchase/orders/{id}/copy` | §24.1 |

**Phones.** Priya works mostly on desktop, and Ravi and Meena approve on phones, so the approval page is the phone-critical one. Long values must pass the overflow gate.

## 12. Cutover plan

Owner decisions: go live as soon as the modules are ready (CD-228). Open POs are on paper or email. The year's back-dated data goes in first, and then the app goes live (CD-234). This plan covers all three modules.

**Why not type received quantities into `received_qty`.** The roll-up recomputes that column as a sum over GRN lines (D-DOC-13), so the first real GRN would wipe an imported value. ERPNext has the same hazard and none of the three systems studied has a coded answer. That's why `already_received` is a separate, plain field added into pending.

**What makes back-entry work with no kernel change**

| Concern | How it works |
|---|---|
| Dates | Only future dates are refused. Number, FY and print follow the document's own date (D-NAM-04). Nothing compares a date with today beyond that |
| Numbering | The FY in the number comes from the document date, so a date before 1 April 2026 gets a `25-26` prefix with its own counter. Numbers follow entry order, not date order |
| Approvals | **No approval rules exist during back-entry** (CD-235). With no rule, submit goes straight to approved and numbers the document (F5), so nobody re-approves a year of history. Rules go in on go-live day. Print's approved-by line reads the approval history's no-rule entry (`approved_no_rule`), since no request exists, so it shows the person who submitted. The audit shows the real entry time |
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
- GRN, Expense Claim and Employee Advance documents dated on or before it can't be submitted, approved or cancelled. Close stays allowed. The check is `modkit.CheckNotLocked(ctx, tx, label, display string, date types.Date)`, where `display` is `naming.Label(number, id)`, called from `BeforeSubmit` and `ReverseEffects` inside the transaction, so it needs no kernel change.
- Message: `{Label} {display} is dated {date}, which is locked. The period is locked up to {lock date}.` A document not yet numbered shows `Draft 42`.
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
- `internal/app` registers routes and hooks. `EnsureSeries` already runs for every registered series (CD-28). Approval rules are never seeded: they are data entered on go-live day (§12).

## 15. Proposed changes for the head

I don't edit the core spec or the decision log. Rulings of 30 October: items 8 and `CloseRight`, `AllowBlocked` and the FILE changes are accepted in principle for MOD-0; item 11 is accepted in principle as `docs/specs/2026-10-30-module-docscreen-hooks-spec.md` (CD-237), and that document's §3 is authoritative; item 2's guard and moving `amend` are accepted. The rest await the head's ruling with all three specs in. Proposed CD entries:

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
11. **`docscreen` module hooks (additive, in MOD-0).** Specified in `docs/specs/2026-10-30-module-docscreen-hooks-spec.md`. Its §3 is authoritative for names: `Steps`, `Line`, `Hide`, `Check`, `Page`, `Reason`.
12. **Fixture seed.** Amended by CD-247: `fixture.Order` keeps its seed, because 51 core test files use it. `00-fixtures.md` gains separate rows for `purchase.Order` and `purchase.Request` with §10's and §20.6's rights.
13. **`doc.Engine` reassign (v0.5, §21.2).** DOC gains a `reassign` path: a draft or rejected document's `OwnerField` changes to another user, with the posted `version`, an audit entry and a history row. Generic, so GRN and Expense can opt in later. Owned by DOC; High tier; MOD-0.
14. **Comments in `modkit` (v0.5, §22).** One append-only `module_comment` table and a panel component, used by every module DocType that opts in.
15. **Module routes calling `Engine.Do` (v0.5).** Copy (§24.1) and Create PO from a request (§20.4) create drafts through `Engine.Do` from a module handler. Hooks spec §5 rule 2 forbids hooks from calling it, not module routes; this item makes that explicit.
16. **Fixture seed for requests.** `00-fixtures.md` gains the Purchase Request rights rows (§20.6) and two request fixtures.
17. **State labels per DocType (CD-247).** `DocType.StateLabels map[State]string`, additive; blank keeps the global label. Request shows `approved` as `Submitted`.
18. **Close hook (CD-247).** An optional `doctype.Closer` interface (`OnClose(ctx, tx, id) error`) that the kernel's close calls inside its transaction after the state is set. PO uses it to refresh linked requests (§20.3). A hooks value without it is unchanged.

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

**v0.5 assumptions (head director), for review with CD-246.**
- Every employee can raise a Purchase Request for the sites and projects they are allowed; requests need no approval (§20.6).
- A request line names an item from the item master; there is no free-text item in v1 (§20.2).
- A PO line pulled from a request keeps the request's item and UoM; only the quantity and price change (§20.5).
- Hand-over moves a draft to another Purchase User; a Purchase Manager can hand over anyone's draft, but never edits it (§21.2).
- Comments can't be edited or deleted, and nobody is notified; the Purchase home shows recent comments on your documents (§22).
- Mark as sent is recorded by the PO's owner or a Purchase Manager or Director, and can be undone (§23).
- Only the requester closes a request; a buyer who won't order something says so in a comment (§20.3).
- A short-closed PO counts only what was received as ordered, so the rest can go on a new PO (§20.2).
- The default price is the last price paid to the same vendor for the same item and UoM on the same project, else the item's standard rate (§24.2).

## 17. Sources

**Kernel.** Spec §3.2, §4.3, §5; `internal/kernel/doc/rollup.go`, `transitions.go`, `links.go`; `internal/masters/party/rules.go:20`; `internal/masters/uom/qtyuse.go:24`; CD-156, 157, 178, 216, 228.

**SME ERPs (research, 31 October, v0.5).** Zoho Books purchase orders and approvals: https://www.zoho.com/in/books/help/purchase-order/ and https://www.zoho.com/books/help/transaction-approval/transaction-approval-workflow.html; Tally Prime order processing: https://help.tallysolutions.com/tally-prime/purchase-process/purchase-order-tally/; ERPNext Purchase Order and Material Request: https://docs.frappe.io/erpnext/user/manual/en/purchase-order, https://docs.frappe.io/erpnext/user/manual/en/material-request; Frappe stale-save refusal `frappe/model/document.py` (version-15); Odoo 18 purchase: `addons/purchase/models/purchase_order.py`, https://www.odoo.com/documentation/18.0/applications/inventory_and_mrp/purchase/. GST fields customary on a PO follow the invoice list (CBIC flyer, https://www.gstcouncil.gov.in/sites/default/files/e-version-gst-flyers/Tax_Invoice_and_other_new.pdf); nothing official prescribes a PO's contents.

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
| PO-032 | Already received above qty warns and saves; needs Earlier PO no. | `TestPO_AlreadyReceivedRules` | P1 | unit |
| PO-033 | Closing part-received: pending shows 0, GRN refused with DOC-017's text | `TestPO_CloseStopsReceipts` | P1 | integration |
| PO-034 | Stale PO page: version conflict on every write (CD-94) | `TestPO_StaleVersion` | P1 | integration |
| PO-035 | Print golden: letterhead, deliver-to, refs, totals, words | `TestPO_PrintGolden` | P1 | integration (golden file) |
| PO-036 | Print needs `print` and visibility (R3) | `TestPO_PrintRights` | P1 | integration |
| PO-037 | Pending report totals, overdue word, scope filtered | `TestPO_PendingReport` | P1 | integration |
| PO-038 | Pending report CSV needs `export` (CD-145) | `TestPO_PendingExportRight` | P1 | integration |
| PO-039 | Settings: only Administrator, audited, stale refused | `TestSettings_AdminOnlyAudited` | P1 | integration |
| PO-040 | Cutover: counter set to 437 gives first number 00438; lower refused | `TestPO_CutoverCounter` | P1 | integration |
| PO-046 | Back-entry: a PO dated 15-04-2026 with no rules is approved on submit, FY 26-27; dated 31-03-2026 takes prefix 25-26 and its own counter; the print names the submitter as approver | `TestPO_BackEntry` | P1 | integration |
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
| 0 | **MOD-0** shared | `internal/modules/modkit`, settings page, `gstcalc.PlaceReason`, `print.TradeDoc` additions, `docscreen` module hooks (§15 item 11), amend calling `Validate`, `CloseRight` and `AllowBlocked` (Expense spec §15), `msgLimit`, overdue approvals, checker, import guard | WP3 Task 5 and the owner's review (CD-237); plan check against `main` first | none |
| 1 | **MOD-PO** | `internal/modules/purchase` | MOD-0 | MOD-EXP |
| 2 | **MOD-GRN** | `internal/modules/goodsreceipt` | MOD-PO tasks 1 to 3 | MOD-EXP |
| 1 | **MOD-EXP** | `internal/modules/expense` | MOD-0 | MOD-PO, MOD-GRN |
| 4 | **MOD-CUT** | back-entry runbook, go-live rehearsal on a copy | All | none |

**MOD-PO tasks.** Superseded in v0.5 by the plan `docs/superpowers/plans/2026-10-31-mod-po.md`, which also covers §20 to §25. The table below is kept for history.

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

---

# v0.5 additions (CD-246)

The rule behind every addition: **each document has one owner at a time, and everyone else asks, comments or approves; they never edit it.** Site staff own requests, a buyer owns the PO, an approver approves or rejects with a reason, and the buyer records that it was sent. The research found that SME ERPs avoid edit fights in three ways: a separate request document that a buyer turns into a PO (ERPNext Material Request), freezing a document once it is committed (ERPNext, Odoo), and an internal comment thread (Zoho Books, Odoo). None of them keeps one owner per draft. The kernel already refuses a stale save (CD-94), the equivalent of ERPNext's "modified after you opened it".

## 20. Purchase Request

### 20.1 Purpose

Suresh at the site needs 40 bags of cement by Friday. Today he phones or WhatsApps Priya, and Priya types it into a PO. With requests, Suresh raises a Purchase Request on his phone, Priya pulls its lines into a PO, and Suresh sees Ordered and the PO number on his request. He never edits the PO, and Priya never edits his request.

### 20.2 DocType

Registered in `internal/modules/purchase`: `Name: "purchase.Request"`, `Label: "Purchase Request"`, `PluralLabel: "Purchase Requests"`, tables `purchase_request` and `purchase_request_line` (AUTOINCREMENT ids; lines with `id`, `parent_id` ON DELETE CASCADE, `line_no`), `Series: "PR/{FY}/{#####}"` (14 characters), `Submittable`, **not** `Approvable`, not `Printable`, `OwnerField: "created_by"`, `ProjectFields: ["project_id"]`, `SiteFields: ["site_id"]`. It opts into attachments (CD-243), so Suresh can add a photo of the part. Migrations come from the purchase range (`10050–10199`).

Steps: Details → Items → Check and submit. There is no Terms step.

**Header**

| Field | Label | Type | Rule |
|---|---|---|---|
| `number` | Request number | Text, read-only | List priority 1 |
| `site_id` | Needed at | Link Site | Required |
| `project_id` | Project | Link Project | Required. Defaults from the site, as on the PO (§3.1) |
| `request_date` | Request date | Date | Required, default today, not in the future. Sets the FY of the number |
| `required_by` | Needed by | Date | Optional, not before the request date |
| `purpose` | What is it for? | Text, max 500 | Optional |
| `order_status` | Ordered | Select, read-only: `not_ordered`, `part_ordered`, `ordered` | §20.3. List priority 2, filterable |
| `state` | State | Select, read-only | List priority 3 |

**Lines (1 to 500 at submit, DOC's limit; zero on a draft)**

| Field | Label | Type | Rule |
|---|---|---|---|
| `item_id` | Item | Link Item | Required. Item master only, no free text [Assumption] |
| `description` | Description | Text, max 500 | Default item name |
| `uom_id` | Unit | Link UoM | Required; default stock UoM; whole-number check through `uom.CheckLineQty`; `uom.RegisterQtyUse` for this table |
| `qty` | Quantity | Qty | Required, more than 0 |
| `note` | Note | Text, max 200 | Optional, e.g. a brand or size |
| `ordered_qty` | Ordered | Qty, `System`, read-only | Written by `RefreshOrderStatus` (§20.3) |

No line field name repeats a header field name (CD-151).

**What a PO line counts towards its request row** (one function, `lineOrdered`, used everywhere):

| PO state | Ordered | Held |
|---|---|---|
| draft, pending approval, rejected | 0 | `qty` |
| approved | `qty` | 0 |
| closed | `min(qty, already_received + received_qty)`: a short-closed PO frees the rest, so it can go on a new PO (§5) | 0 |
| cancelled | 0 | 0 |

**Pending to order** (never stored) = `max(0, qty − ordered − held)`, summed over the row's PO lines. A closed request has 0 pending.

**Messages (request).** `Request date can't be in the future.` `Needed by can't be before the request date.` The site and project rules and texts are the PO's (§4).

### 20.3 States

- **Submit** moves a request straight to approved and numbers it (no approval rule applies, F5). The screen calls the state **Submitted**: the state label for a non-approvable DocType is a docscreen display label, which the plan check confirms.
- **Close** means "no longer needed". Only the owner closes (the default close right, `submit`, is `own`). It asks for a reason (3 to 500 characters). A buyer who won't order something says so in a comment (§22) and the requester closes it: one owner, no fight. A closed request can't be pulled into a PO, and lines already ordered stay ordered. When a draft, pending or rejected PO still holds its lines, the close page warns, through the Reason hook: `{n} rows are on orders not yet approved: {list}. Those orders can't be approved until the rows are removed.` The kernel then refuses to approve such a PO with DOC's closed text (§20.5); the approver rejects it and the buyer removes the rows.
- **Cancel** is done by the owner. The kernel blocks it while a pending, approved or closed PO links the request (DOC-052, `LinkSpec`). A draft or rejected PO still holding the row gets DOC's cancelled text on its next save.
- There is no amend or withdraw: to change a submitted request, the owner closes it and raises a new one.
- **`order_status`** is `not_ordered` when nothing is ordered, `ordered` when every line's ordered quantity reaches its requested quantity, and `part_ordered` otherwise. `purchase.RefreshOrderStatus(ctx, tx, requestIDs []int64)` recomputes `ordered_qty` and `order_status` from `lineOrdered`, counting the PO in its new state. It writes them as the kernel's roll-up writes system fields: no version bump and no audit entry (the plan check confirms against `rollup.go`). Callers: PO `ApplyEffects` (approve), `ReverseEffects` (cancel), `OnClose` (close, §15 item 18), and MOD-GRN wherever it calls `RefreshReceiptStatus` on a closed PO, because a GRN cancelled against a closed PO changes that PO's ordered amount.

### 20.4 Turning requests into a PO

There are two ways in, and both are a buyer's action.

1. **Create PO** on a request's page (needs `create` on Purchase Order). `GET /modules/purchase/requests/{id}/order` shows one small page: the request's pending rows and a Vendor picker. `POST` posts the request's `version` and the vendor, and creates a PO draft through `Engine.Do` (§15 item 15) with that vendor, the request's site, project and needed-by date, and one PO line per row with pending to order above 0: item, description, UoM, quantity equal to the pending amount, and `request_line_id`. Because the vendor is known at create, `Validate` fills the last price (§24.2) and the other defaults on that first save. It then opens the PO wizard at Details. A request with nothing pending answers `Everything on {PR} is already on an order.` A blank vendor answers `Pick a vendor.`
2. **Add from requests** on a PO's Items step: a link, `Add from requests ({n} open)`, shown when open request lines exist for the PO's site and project. Its custom step body (hooks spec `Steps["items"]` with `Body` and `Save`) lists pending request lines for that site and project, grouped by request. Each line shows the requester, needed-by date, item, unit and pending quantity, with a tick and a quantity input that defaults to the pending amount. Lines with nothing pending because other POs hold them show `On {PO display} ({owner})` and can't be ticked. That tells a buyer whom to talk to, instead of ordering twice. `Add selected` appends the lines (`Save.Lines` keeps the existing ones), and the buyer returns to Items.

One PO can take lines from many requests. A request line can be split across POs, for example two vendors, until nothing is pending.

### 20.5 Rules on PO lines with a request row

`Validate` (every save, inside the transaction) and `BeforeSubmit`. Each condition has its own message (CD-147).

| Condition | Message |
|---|---|
| The request is a draft | DOC's `{PR} isn't approved yet. Pick an approved purchase request.` |
| The request is closed | DOC's `{PR} is closed. Remove the line or pick another purchase request.` |
| The request is cancelled | DOC's `{PR} is cancelled. Remove the line or pick another purchase request.` |
| The request's site differs from the PO's site | `Row {n}: Request {PR} is for site {code}. This order delivers to {code}.` |
| The request's project differs from the PO's project | `Row {n}: Request {PR} is for project {code}. This order is for {code}.` |
| The item differs from the request row's | `Row {n}: Request {PR} row {m} is for {item code}. Remove the row to order a different item.` |
| The UoM differs from the request row's | `Row {n}: Request {PR} row {m} is in {uom code}. Keep that unit.` |
| The same request row is on two rows of this PO | `Row {n}: Request {PR} row {m} is already on row {k}.` |
| Other POs' ordered plus held (`lineOrdered`) already reach the request row's quantity | `Row {n}: Request {PR} row {m} is already on {PO display}.` (the oldest such PO) |

Ordering more than was requested is allowed. The check page warns: `Row {n}: ordering {x} more than request {PR} asked for.` The quantity check reads the other POs inside the write, so when two buyers save at once the second is refused (CD-94). This is a cross-record invariant. Its write paths, and what guards each: PO save and submit (`Validate`, `BeforeSubmit`); PO approve (the kernel re-validates); PO amend (copies `request_line_id`; M0 makes amend run `Validate`); PO cancel and close (free quantity; `RefreshOrderStatus`); request close and cancel (call no hook; the kernel's `LinkSpec` refuses the PO on its next save or approve). One post-state function, `checkRequestRows(ctx, tx, poID)`, is called last by `Validate` and `BeforeSubmit`.

### 20.6 Rights (production `DefaultRights`)

| Role | Rights on Purchase Request (scope) |
|---|---|
| Employee | read, create, write, submit, cancel, delete (own); close through `submit` (own) |
| Stores | read (all) |
| Purchase User | read (all) |
| Purchase Manager, Director | read (all) |
| Administrator | every applicable right |

Site and project restrictions apply, so Suresh raises and sees requests for PUNE-WH and METRO-SITE only. Every user has Employee by default (D-AUTH-22), so everyone can request. That is the intent [Assumption].

### 20.7 Screens

| Screen | Notes |
|---|---|
| List `/doc/purchase.Request` | Phone cards: number or `Draft {id}`, site, needed by, order status. Filters: state, order status, site, project (declared `Filterable`). "Mine" and "to order" are Purchase home queues (§25). Button `New request` |
| Wizard | Details (Needed at, Project, Needed by, What is it for?) → Items (add-to-a-list cards at every width; `CardsOnly`) → Check and submit, whose button reads `Submit request` |
| Document page | Lines with Requested, Ordered and Pending to order, plus a line note when an unapproved PO holds the line: `On {PO display} ({owner})` when `LinkRead` lets the viewer read that PO, else `On another order` (CD-233). A Linked orders card lists number, vendor, state and receipt status, read through `LinkRead`. Then Comments (§22) and attachments. Buyers see `Create PO` |

The UX design document covers these screens (protocol §9).

## 21. One owner per document

### 21.1 The rule

A PO in draft or rejected has exactly one owner, `created_by`. Only the owner edits, submits, withdraws, revises or deletes it (§10: `write`, `submit` and `delete` are `own`). Everyone else who can read it sees a banner at the top of the document page and the wizard:

`Owned by {name}. Only {name} can change this draft. Add a comment below, or ask {name} to hand it over.`

People holding the hand-over right also see `Hand over`. Approvers never edit: they approve, or reject with a reason, and the owner revises (kernel, unchanged). Requesters never touch a PO, and buyers never touch a request (§20). Two tabs of the same owner still get the stale-version refusal (CD-94).

### 21.2 Hand over

| | |
|---|---|
| When | The PO is in draft or rejected |
| Who | The owner, or anyone with `approve` on Purchase Order (Purchase Manager, Director). They hand the draft over; they never edit it |
| To whom | An enabled user, other than the owner, who holds `create` and `read` on this PO (`CanTx` as that user with the PO's `RecordRef`) and `write` on a copy of the `RecordRef` with `CreatedBy` set to them. `UsersWithRightTx(write, rec)` can't be used: with `own` scope it returns only the current owner |
| How | `GET/POST /modules/purchase/orders/{id}/handover`: pick a person (radios of the eligible users, read with `UsersWithRightTx` and the restriction check), add an optional note, then `Hand over`. The form posts the PO's `version` |
| Effect | DOC's reassign path (§15 item 13) returns a typed `*doc.ReassignError` whose `Reason` is one of `NotDraft`, `Pending`, `SameOwner`, `CantCreate`, `CantSee`, `Disabled`; the module maps each to the text below. It sets `created_by`, bumps `version`, writes an audit entry and a history row (`Handed over from {a} to {b}`). If a note was given, it is posted as a comment (§22) by the person handing over |

**Messages**

| Condition | Message |
|---|---|
| The PO is approved, closed or cancelled | `An approved, closed or cancelled order can't be handed over.` |
| The PO is pending approval | `Withdraw this order before handing it over.` |
| The person picked is the current owner | `This order is already {name}'s.` |
| The person can't raise POs | `{name} can't raise purchase orders. Pick someone with the Purchase User role.` |
| The person can't see the PO's project or site | `{name} can't see this order's project or site. Pick someone else.` |
| The person's sign-in is disabled | `{name} can't sign in at the moment. Pick someone else.` |
| The person handing over has no right to | AUTH's 403 text |

An amendment belongs to whoever amends (§10). Requests have no hand-over: the requester closes the request and someone raises a new one [Assumption].

## 22. Comments

A shared feature in `modkit`. PO and Request opt in now; GRN and Expense can opt in later with one line each.

- **Table.** `module_comment`: `id` AUTOINCREMENT, `doctype`, `record_id`, `body` (1 to 1,000 characters), `created_by`, `created_at`. Triggers refuse UPDATE and DELETE, so comments are append-only and are their own record. They aren't copied by amend or copy.
- **Service.** `modkit.AddComment` and `modkit.ListComments` check `read` on the record inside the transaction (`CanTx` with the record's `RecordRef`). A record the user can't read answers AUTH-108's 404, the same as a missing one (CD-245). DocTypes opt in with `modkit.RegisterComments(doctype)`; any other DocType answers 404.
- **Route.** `POST /modules/comments/{doctype}/{id}`, guarded by `read`, with CSRF. No `version`: a comment doesn't change the document. htmx appends the new comment and clears the box. The response redraws only the comment list and the box it just cleared.
- **Panel.** A card on the document page (`PageExtra.Cards`) in every state, cancelled included. Comments show oldest first, with name and time. Below them are a text area and `Post comment`. Never printed, never exported.
- **Messages.** Empty: `Write a comment first.` Too long: `A comment can be at most 1,000 characters.`
- **Seeing new comments.** There are no notifications (D-APR-13 stands). The Purchase home lists comments from the last 7 days on documents you own, newest first, at most 10 (§25).

## 23. Sending to the vendor

- **Table.** `purchase_order_sent`, at most one row per PO: `po_id` (primary key, references `purchase_order` ON DELETE CASCADE), `sent_on` Date, `sent_via` (CHECK in `email`, `whatsapp`, `courier`, `by_hand`, `other`), `note` (max 200), `sent_by`, `sent_at`, `version`. It is separate from the PO because an approved PO doesn't change (R7). The form posts the row's `version`, 0 when there is no row. Mark as sent inserts (posted 0) or updates (posted current) with `version + 1`; Mark as not sent deletes the row (posted current). A stale version is a 409 with DOC's stale text. Each change writes an audit entry on the PO (`Marked as sent`, `Marked as not sent`) through `audit.Writer`.
- **Page.** `/modules/purchase/orders/{id}/send`, for approved POs only. It has `Download PDF`, and `Share` on devices with the Web Share API for files (the PDF goes to WhatsApp, a mail app and so on; the button is hidden where unsupported, so the page works without JavaScript). Below those is a form: Sent on (default today), How (radios: Email, WhatsApp, Courier, By hand, Other), Note, and the button `Mark as sent`. Once sent, the page shows `Mark as not sent`.
- **Who.** The PO's owner (`write`), or anyone with `approve` on Purchase Order (Purchase Manager, Director), or an Administrator. Anyone else gets AUTH's 403 text.
- **Document page.** It shows `Sent to vendor on {date} by {how} ({name})`, or `Not sent yet` with `Send to vendor`. After approval, the owner finds the PO in the To send queue (§25).
- **Queue.** The To send queue (§25) lists approved POs with no row.
- **Messages.** Not approved: `Only an approved order can be marked as sent.` Future date: `Sent on can't be in the future.` Before the order date: `Sent on can't be before the order date.` No method chosen: `Choose how it was sent.`

## 24. Faster entry

### 24.1 Copy

`Copy` on the page of any PO the user can read, in any state, for a user with `create`. `POST /modules/purchase/orders/{id}/copy` creates a draft owned by the copier through `Engine.Do` and opens it on Details.

| Copied | Not copied |
|---|---|
| vendor, vendor address, site, project, payment terms, terms, reverse charge; lines' item, description, UoM, qty, price, discount, tax code, HSN/SAC | number, dates (order date is today), required by, vendor quote ref, remarks, earlier PO fields, `already_received`, `request_line_id`, attachments, comments, sent record |

`copied_from_no` holds the source's display. A site or project the copier may not use is refused by AUTH's create check (AUTH-099's text). Create runs link checks and `Validate`, so a disabled master is never copied: a disabled header link (vendor, address, payment terms) is left blank, and a line whose item, UoM or tax code is disabled is left out. The check page notes read: `Copied from {display}. Rates are as on that order; check them before you submit.`, then for each blank `{Label} {code} is disabled, so it was left blank.` and for each line `Row {n} of {display} was left out: {label} {code} is disabled.`

### 24.2 Last price

- **Default.** When a line is saved with a blank price, the default is the price and discount of the newest line (latest `order_date`, then highest id) on an approved or closed PO with the same vendor, item, UoM and project. If there is none, it is the item's standard rate. Restricting the lookup to the same project means the default never reveals a price from a project the user can't see: `Validate` has no principal, so it can't apply scope.
- **Hint.** After a line is saved, its card and grid row on the Items step carry the note `Last bought at ₹{rate} from {vendor} on {date} ({PO})`, the newest for that item and UoM across vendors, through `Module.LineCard`'s `Note` (hooks spec §3). It is read through `Auth.Scope` (AUTH-110) in the hook, which has the principal. `LastPrices(ctx, p, itemID, uomID, limit)` returns up to three for the document page's line notes.
- A rate above the last price raises no warning [Assumption]. It is a price, not a rule.

### 24.3 Everything else stays

Item entry is docscreen's grid on desktop and add-to-a-list on phones (§11). Vendor defaults (address, payment terms) and item defaults (rate, tax code, HSN, UoM) apply on save (§8).

## 25. Purchase home

`/modules/purchase`, the nav's Purchase entry. Guarded by `read` on Purchase Order or Purchase Request. One column of cards on a phone and two from 64rem. Each card shows a count and links to a queue page, `/modules/purchase/q/{queue}`, which the module renders with the list components from its own scoped query; the pending report covers Overdue deliveries. The generic list can only filter declared fields, so queues don't go through it. A user sees only the cards they have rights for. Every count goes through `Auth.Scope` (AUTH-110).

| Card | Shows | Who sees it |
|---|---|---|
| My drafts | POs I own in draft or rejected; rejected ones marked `Needs changes` | create on PO |
| Waiting for approval | My POs pending, with `Waiting {n} days` (§14.3) | create on PO |
| To approve | Count of my approval requests, linking to `/approvals` | approve on PO |
| To send | Approved POs not sent: mine, or all for a user with `approve` | create or approve on PO |
| Awaiting delivery | Approved POs not fully received | read on PO |
| Overdue deliveries | Lines past required-by with pending above 0, linking to the pending report filtered to overdue | read on PO |
| Requests to order | Submitted requests with pending to order, oldest needed-by first | create on PO |
| My requests | My open requests, each with its order status | create on Request |
| Recent comments | §22 | anyone |

The two main buttons are `New purchase order` and `New request`. When nothing is waiting, the page says: `Nothing waiting for you.`

## 26. v0.5 tests

These are added to §18. They are in the same area, PO, so `checktestids` needs no new area.

| ID | Behaviour | Go test | P | Level |
|---|---|---|---|---|
| PO-048 | Request submit numbers `PR/26-27/00001` with no approval; the request date sets the FY | `TestRequest_SubmitNumbers` | P1 | integration |
| PO-049 | Suresh raises and sees requests only for his sites, on the real `auth.Service` | `TestRequest_SiteRestrictionReal` | P1 | integration |
| PO-050 | Request rights: owner edits and closes; another employee and a Purchase User can neither edit nor close | `TestRequest_Rights` | P1 | integration |
| PO-051 | Create PO from a request: draft with site, project, needed-by and only pending lines with `request_line_id`; nothing pending refused | `TestRequest_CreatePO` | P1 | integration |
| PO-052 | Add from requests lists only same site and project with pending above 0, and shows `On {PO}` for held lines | `TestRequest_AddFromRequests` | P1 | integration |
| PO-053 | Each §20.5 condition gives its own message | `TestPO_RequestRowRules` | P1 | integration |
| PO-054 | Two buyers save the same request row at once: the second is refused, on two connections | `TestPO_RequestRowRace` | P1 | integration |
| PO-055 | `ordered_qty` and `order_status` after PO approve, cancel, short close (rest freed for a new PO) and an amend with request rows | `TestRequest_OrderStatus` | P1 | integration |
| PO-056 | Request cancel blocked while a PO links it, naming the PO | `TestRequest_CancelBlocked` | P1 | integration |
| PO-057 | Random request and PO sequences: pending to order never below 0, held plus ordered never above requested unless one PO over-orders, status matches | `FuzzRequest_OrderInvariants` | P1 | unit (fuzz) |
| PO-058 | Only the owner edits, submits, withdraws, revises and deletes a draft; Ravi and a second Purchase User get 403, on the real `auth.Service` | `TestPO_OwnerOnlyReal` | P1 | integration |
| PO-059 | Hand over by owner and by Ravi; every §21.2 message; stale 409; audit and history row; new owner edits and old owner can't | `TestPO_HandOver` | P1 | integration |
| PO-060 | Owner banner and Hand over button shown to the right people | `TestPO_OwnerBanner` | P1 | integration |
| PO-061 | Comments: read needed to view and post; hidden record 404 same as missing; append-only trigger; both messages | `TestComments_Rules` | P1 | integration |
| PO-062 | Comments panel on PO and Request pages in every state, cancelled included; never in print | `TestComments_Panel` | P1 | integration |
| PO-063 | Mark as sent: approved only, owner only, date rules, undo, audited, stale 409 | `TestPO_MarkSent` | P1 | integration |
| PO-064 | Copy: the copied and not-copied columns of §24.1; copier owns it; restricted refused with AUTH-099's text; check note | `TestPO_Copy` | P1 | integration |
| PO-065 | Last price default from same vendor, item, UoM and project, else standard rate; hint scoped | `TestPO_LastPrice` | P1 | integration |
| PO-066 | Purchase home cards and counts for Priya, Ravi, Suresh and Deepak, scoped | `TestPurchaseHome_Cards` | P1 | integration |
| PO-067 | Default rights for Purchase Request match `00-fixtures.md` | `TestRequest_DefaultRights` | P1 | unit |
| PO-069 | Closing a request held by an unapproved PO warns; the PO's approve is then refused with DOC's closed text | `TestRequest_CloseWhileHeld` | P1 | integration |
| PO-070 | Queue pages list only the user's scoped rows, matching the home counts | `TestPurchaseHome_Queues` | P1 | integration |
| PO-068 | Phone flow: Suresh requests, Priya creates the PO, Ravi approves, Priya marks it sent, Suresh sees Ordered | `TestE2E_RequestToSentFlow` | P1 | e2e |

PO-041 changes with §10: Purchase User `write`, `submit` and `delete` become `own`.
