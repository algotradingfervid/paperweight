# Purchase Order and Purchase Request (PO)

## Scope
The Purchase Order module of `docs/specs/2026-10-30-module-purchase-order-spec.md` v0.5: `purchase.Order` and `purchase.Request`, one owner per draft with hand-over, comments, Mark as sent, Copy, last price, the pending items report and the Purchase home with its queues. Cases are spec §18 (PO-001 to PO-047), §26 (PO-048 to PO-070) and §27.5 (PO-071 to PO-085), in one area, `PO`. The plan is `docs/superpowers/plans/2026-10-31-mod-po.md`.

Fixtures are in `docs/tests/core/00-fixtures.md`, including the `purchase.Order` and `purchase.Request` rights rows and the request fixtures PR-A and PR-B (CD-247). The reference PO gives the worked numbers: taxable ₹79,800.00, CGST and SGST ₹7,182.00 each, grand total ₹94,164.00.

**MOD-GRN exit.** PO-025, PO-031, PO-033 and PO-044 need a real GRN. They are MOD-GRN's exit criteria, as CD-40 did for cases needing another WP; MOD-PO's own exit is every other P1 case. No test-local stand-in for the GRN (lenient doubles, CD-125). Each is marked **Exit:** below.

Review focus (spec §18): PO-012 and PO-013 must run on the real `auth.Service`, since passing only on authfake hides differences. PO-031 and PO-044 guard the one hazard the spec adds (`already_received` beside the roll-up).

## Test cases

### PO-001 · Reference PO totals to the paisa, intra-state
- **Priority:** P1
- **Level:** integration
- **Given** the reference PO (`00-fixtures.md`): vendor `SHREE` (Mumbai, Maharashtra), delivery to a site of branch PUNE (Maharashtra), lines MS-ANGLE-50 1,200.000 kg at ₹62.5000 and WIRE-18G 50.000 kg at ₹96.0000, both 18% **When** it is saved **Then** `net_total` = ₹79,800.00, `cgst_total` = ₹7,182.00, `sgst_total` = ₹7,182.00, `igst_total` = ₹0.00, `grand_total` = ₹94,164.00 and `rounded_total` = ₹94,164.00.
- **Go test:** `TestPO_ReferenceTotals`
- **Sources:** PO spec §3.1, §7, §18

### PO-002 · Delivery to a BLR site from SHREE gives IGST, same grand total
- **Priority:** P1
- **Level:** integration
- **Given** the reference PO's lines and vendor `SHREE` (Maharashtra) **When** it delivers to `TECH-SITE` (branch BLR, Karnataka) **Then** the supply is inter-state (`gstcalc.IsInterStateSupply`, place state from the site's branch): `igst_total` = ₹14,364.00, CGST and SGST are ₹0.00, and `grand_total` is ₹94,164.00 as in PO-001.
- **Go test:** `TestPO_InterStateSplit`
- **Sources:** PO spec §7, §18

### PO-003 · Changing vendor on a draft recomputes the split (NUM-041)
- **Priority:** P1
- **Level:** integration
- **Given** a draft PO with vendor `SHREE` delivering to a PUNE site (intra-state) **When** the vendor is changed to `BHARAT` (Karnataka) and the draft is saved **Then** the split is recomputed to IGST (NUM-041), and the vendor snapshot columns follow the new vendor. Changing the site on a draft recomputes the split too (§8).
- **Go test:** `TestPO_VendorChangeRecomputes`
- **Sources:** PO spec §8, §18; `07-numbers-lists.md` NUM-041

### PO-004 · SEZ vendor is inter-state in the same state
- **Priority:** P1
- **Level:** integration
- **Given** a vendor in Maharashtra whose GST category is SEZ **When** a PO delivering to a PUNE site is saved **Then** the supply is inter-state (IGST), although supplier and place are in the same state.
- **Go test:** `TestPO_SEZInterState`
- **Sources:** PO spec §7, §7.1, §18

### PO-005 · PlaceReason texts for all three cases, on the check page and print
- **Priority:** P1
- **Level:** integration
- **Given** POs for the three cases of §7.1 **Then** `gstcalc.PlaceReason` returns, and the check page, the document page and the print show:
  - same state: "Intra-state: the supplier and the delivery site are both in Maharashtra."
  - different: "Inter-state: the supplier is in Karnataka and the delivery site is in Maharashtra."
  - SEZ supplier: "Inter-state: Shree Steel Traders is an SEZ supplier."
- **Go test:** `TestPO_PlaceReasonShown`
- **Sources:** PO spec §7, §7.1, §18. The plan splits it: unit part M0-8, print P8, check page P15a

### PO-006 · Line discount gives the net rate (NUM-015) and amount
- **Priority:** P1
- **Level:** unit
- **Given** a line with `price`, `discount_pct` between 0 and 100 and `qty` **Then** `rate` is `price` less the discount to 4 dp (NUM-015) and `amount` is `gstcalc.LineAmount(qty, rate)`. `discount_pct` defaults to 0.
- **Go test:** `TestPO_LineDiscount`
- **Sources:** PO spec §3.2, §18; `07-numbers-lists.md` NUM-015

### PO-007 · Snapshots hold after the vendor, address and item change
- **Priority:** P1
- **Level:** integration
- **Given** an approved PO **When** the vendor's name, its address and an item's name are edited in the masters **Then** the PO's `snap_vendor_*`, `snap_vendor_address` and line `snap_item_*` columns, and the document page and print, still show the values as of the last draft save (D-MST-03, MST-011).
- **Go test:** `TestPO_SnapshotsSurviveMasterEdits`
- **Sources:** PO spec §3.1, §3.2, §8, §18

### PO-008 · Non-vendor party refused with MST's text
- **Priority:** P1
- **Level:** integration
- **Given** a party without the vendor role **When** it is saved as `vendor_id` **Then** the save is refused with MST's text from `party.CheckRole`, and nothing is written.
- **Go test:** `TestPO_VendorRoleRequired`
- **Sources:** PO spec §3.1, §8, §18

### PO-009 · Address of another party refused (CD-176)
- **Priority:** P1
- **Level:** integration
- **Given** a PO for `SHREE` **When** `vendor_address_id` is an address of `BHARAT` **Then** the save is refused with CD-176's text, and nothing is written.
- **Go test:** `TestPO_AddressOwner`
- **Sources:** PO spec §3.1, §8, §18; CD-176

### PO-010 · Site on another project refused
- **Priority:** P1
- **Level:** integration
- **Given** a PO on project `PRJ-METRO` **When** its site is `TECH-SITE` (project `PRJ-TECHPARK`) **Then** the save is refused with "Site TECH-SITE is on project PRJ-TECHPARK. Pick that project or another site."
- **Go test:** `TestPO_SiteProjectMismatch`
- **Sources:** PO spec §4, §18

### PO-011 · Blank project defaults from the site for an unrestricted user
- **Priority:** P2
- **Level:** integration
- **Given** an unrestricted user (e.g. `ravi`) **When** a PO is saved with site `METRO-SITE` and a blank project **Then** `Validate` fills `project_id` with `PRJ-METRO`.
- **Go test:** `TestPO_ProjectDefaultsFromSite`
- **Sources:** PO spec §3.1, §18

### PO-012 · Required project stays required on a draft for a restricted user (CD-178)
- **Priority:** P1
- **Level:** integration
- **Given** `priya` (restricted to `PRJ-METRO`), on the real `auth.Service` **When** she saves a draft PO with a site and a blank project **Then** the save is refused with "Project is required." (CD-178 runs before `Validate`), and nothing is written.
- **Go test:** `TestPO_RestrictedDraftNeedsProject`
- **Sources:** PO spec §3.1, §18; CD-178

### PO-013 · Priya sees only PRJ-METRO POs, on the real auth.Service
- **Priority:** P1
- **Level:** integration
- **Given** POs on `PRJ-METRO` and `PRJ-TECHPARK`, on the real `auth.Service` **When** `priya` lists and opens POs **Then** she sees only the `PRJ-METRO` POs; a `PRJ-TECHPARK` PO answers as not found.
- **Go test:** `TestPO_ProjectRestrictionReal`
- **Sources:** PO spec §10, §18

### PO-014 · Suresh opens a PO only for his sites
- **Priority:** P1
- **Level:** integration
- **Given** POs delivering to `METRO-SITE` and `TECH-SITE`, on the real `auth.Service` **When** `suresh` (sites `PUNE-WH`, `METRO-SITE`) opens them **Then** he can open the `METRO-SITE` PO and the `TECH-SITE` PO answers as not found. Project and site restrictions apply together (AUTH-095).
- **Go test:** `TestPO_SiteRestrictionReal`
- **Sources:** PO spec §10, §18

### PO-015 · Whole-number UoM refuses 2.5 Nos
- **Priority:** P1
- **Level:** integration
- **Given** a PO line for `BOLT-M12` in Nos (whole number) **When** qty 2.5 is saved **Then** the line is refused through `uom.CheckLineQty` with its text.
- **Go test:** `TestPO_WholeNumberUoM`
- **Sources:** PO spec §3.2, §18

### PO-016 · RegisterQtyUse is wired: turning whole-number on with a fractional PO line refuses (MST-092)
- **Priority:** P1
- **Level:** integration
- **Given** a PO line with a fractional qty in a UoM that is not whole-number **When** that UoM is changed to whole-number **Then** MST-092 refuses the change, because the package `init` calls `uom.RegisterQtyUse` with `purchase_order_line`, `parent_id`, `uom_id`, `qty`.
- **Go test:** `TestPO_QtyUseRegistered`
- **Sources:** PO spec §3.2, §18; `06-masters.md` MST-092

### PO-017 · Overseas vendor refused
- **Priority:** P1
- **Level:** integration
- **Given** an overseas vendor **When** a PO with it is saved **Then** it is refused with "Overseas vendors aren't supported on purchase orders yet."
- **Go test:** `TestPO_OverseasRefused`
- **Sources:** PO spec §4, §18

### PO-018 · Future order date refused; required-by before order date refused
- **Priority:** P1
- **Level:** unit
- **Given** the fixture clock 2026-10-24 **When** `order_date` is after today **Then** "Order date can't be in the future." Past dates are always allowed. **When** `required_by` is before `order_date` **Then** "Required by can't be before the order date."
- **Go test:** `TestPO_DateRules`
- **Sources:** PO spec §3.1, §4, §18

### PO-019 · ₹94,164.00 needs Purchase Manager; number on approve only
- **Priority:** P1
- **Level:** integration
- **Given** the reference PO (₹94,164.00) raised by `priya` and the test seed's approval rules **When** she submits it **Then** it is `pending_approval` for the Purchase Manager rule and has no number (`Draft {id}`). **When** `ravi` approves it **Then** it is approved and numbered `PO/26-27/00001`.
- **Go test:** `TestPO_ApprovalAndNumber`
- **Sources:** PO spec §5, §6.1, §18

### PO-020 · ₹5,00,000 goes to Director (highest rule)
- **Priority:** P1
- **Level:** integration
- **Given** a PO whose amount is ₹5,00,000.00 **When** it is submitted **Then** the approval request goes to the Director rule, the highest rule that applies.
- **Go test:** `TestPO_DirectorThreshold`
- **Sources:** PO spec §5, §18

### PO-021 · Below every rule: approved and numbered on submit
- **Priority:** P1
- **Level:** integration
- **Given** a PO below every approval rule **When** it is submitted **Then** it goes straight to approved and gets its number on submit (F5), with no second person.
- **Go test:** `TestPO_NoRuleAutoApproved`
- **Sources:** PO spec §5, §18

### PO-022 · Self-approval refused for a manager who raised it
- **Priority:** P1
- **Level:** integration
- **Given** a PO raised by a Purchase Manager that needs Purchase Manager approval (self-approval off) **When** that manager approves it **Then** it is refused by APR's eligibility rule and stays pending.
- **Go test:** `TestPO_NoSelfApproval`
- **Sources:** PO spec §5, §10, §18

### PO-023 · Number FY comes from the order date, 31-03-2026 gives 25-26
- **Priority:** P1
- **Level:** integration
- **Given** a PO with `order_date` 31-03-2026 **When** it is approved **Then** its number is `PO/25-26/00001` (FY from `Hooks.Date`, D-NAM-04).
- **Go test:** `TestPO_NumberFYFromOrderDate`
- **Sources:** PO spec §3.1, §6.1, §18

### PO-024 · Amendment numbered -1, copies lines and already_received; Ravi can approve Priya's amendment
- **Priority:** P1
- **Level:** integration
- **Given** approved `PO/26-27/00001` raised by `priya`, with a line that has `already_received` **When** `ravi` cancels it and `priya` amends it (Purchase User holds `amend`, §10) **Then** the amendment copies every field and line, including `already_received`, and is owned by `priya`. **When** it is submitted and `ravi` approves it **Then** it is numbered `PO/26-27/00001-1`.
- **Go test:** `TestPO_AmendCopies`
- **Sources:** PO spec §5, §6.1, §10, §18

### PO-025 · Cancel blocked by an approved GRN, naming it
- **Priority:** P1
- **Level:** integration
- **Exit:** MOD-GRN exit (spec §18): needs a real GRN.
- **Given** approved `PO/26-27/00001` with an approved GRN `GR/26-27/00001` against it **When** `ravi` cancels the PO **Then** it is refused with DOC-052's text naming the GRN, and the PO stays approved.
- **Go test:** `TestPO_CancelBlockedByGRN`
- **Sources:** PO spec §5, §18; `03-document.md` DOC-052

### PO-026 · Reverse charge: tax shown, not in grand total; Amount includes it
- **Priority:** P1
- **Level:** integration
- **Given** the reference PO with `reverse_charge` on **When** it is saved **Then** CGST and SGST are still computed and shown, `grand_total` is the net alone (₹79,800.00), and `Hooks.Amount` is the rounded total plus the tax (D-APR-03).
- **Go test:** `TestPO_ReverseChargeTotals`
- **Sources:** PO spec §3.1, §5, §7, §18

### PO-027 · Reverse charge refused on a nil-rated line at submit
- **Priority:** P1
- **Level:** integration
- **Given** a PO with `reverse_charge` on and a line with a Nil-Rated, Exempted or Non-GST tax code **When** it is submitted **Then** it is refused with "Line {n}: Reverse charge needs a taxable tax code on every line." for that row. The draft save is not refused.
- **Go test:** `TestPO_ReverseChargeNeedsTaxable`
- **Sources:** PO spec §4, §18

### PO-028 · Reverse-charge note on the print and check page
- **Priority:** P1
- **Level:** integration
- **Given** a PO with `reverse_charge` on **Then** the check page and the print show "Reverse charge applies. GST of ₹{x} is payable by us, not to the vendor." with the PO's tax as `{x}`. With `reverse_charge` off the note is absent.
- **Go test:** `TestPO_ReverseChargePrint`
- **Sources:** PO spec §7, §9, §18

### PO-029 · Pending = ordered − already received − received, never below 0
- **Priority:** P1
- **Level:** unit
- **Then** pending is `max(0, qty − already_received − received_qty)` and is never stored; over-receipt gives 0.
- **Go test:** `TestPO_PendingFormula`
- **Sources:** PO spec §3.2, §18

### PO-030 · RefreshReceiptStatus gives all three values; already received counts
- **Priority:** P1
- **Level:** integration
- **Given** an approved PO **When** `purchase.RefreshReceiptStatus(ctx, tx, poID, accepted)` runs with nothing received, some received and every line's pending at 0 **Then** `receipt_status` is `not_received`, `part_received` and `fully_received`. A cutover PO with `already_received` on a line is `part_received` from its first save. A fully received PO stays `approved`.
- **Go test:** `TestPO_ReceiptStatus`
- **Sources:** PO spec §6.3, §18

### PO-031 · A real GRN doesn't overwrite already_received
- **Priority:** P1
- **Level:** integration
- **Exit:** MOD-GRN exit (spec §18): needs a real GRN.
- **Given** a PO line with `already_received` set **When** a real GRN against it is approved and the roll-up recomputes `received_qty` **Then** `already_received` is unchanged, and pending counts both.
- **Go test:** `TestPO_OpeningSurvivesRollUp`
- **Sources:** PO spec §3.2, §12, §18

### PO-032 · Already received above qty warns and saves; needs Earlier PO no.
- **Priority:** P1
- **Level:** unit
- **Given** a line whose `already_received` is above its `qty` **When** it is saved **Then** it saves, and `OrderWarnings` (shown on the check page, CD-280) returns "Line {n}: Already received is more than the quantity." (CD-234). **When** a line has `already_received` and `earlier_po_no` is blank **Then** "Earlier PO no. is required when a line has Already received."
- **Go test:** `TestPO_AlreadyReceivedRules`
- **Sources:** PO spec §3.2, §4, §18

### PO-033 · Closing part-received: pending shows 0, GRN refused with DOC-017's text
- **Priority:** P1
- **Level:** integration
- **Exit:** MOD-GRN exit (spec §18): needs a real GRN.
- **Given** a part-received approved PO **When** it is closed with a reason **Then** pending shows 0 in reports, a new GRN against it is refused with DOC-017's text, and a GRN against the closed PO can still be cancelled (D-DOC-09).
- **Go test:** `TestPO_CloseStopsReceipts`
- **Sources:** PO spec §6.4, §18; `03-document.md` DOC-017

### PO-034 · Stale PO page: version conflict on every write (CD-94)
- **Priority:** P1
- **Level:** integration
- **Given** a PO page opened at one version and changed elsewhere **When** any write is posted with the old version **Then** it is a version conflict (409, DOC's stale text) and nothing changes.
- **Go test:** `TestPO_StaleVersion`
- **Sources:** PO spec §18; CD-94

### PO-035 · Print golden: letterhead, deliver-to, refs, totals, words
- **Priority:** P1
- **Level:** integration (golden file)
- **Given** the approved reference PO **Then** its print matches the golden `po_approved`: letterhead, `DeliverTo`, `Refs` (Required by, Payment terms, Vendor quote ref, Earlier PO no.), net rate and amount without the discount, HSN/SAC, the CGST and SGST rows, totals and "Rupees Ninety Four Thousand One Hundred Sixty Four Only".
- **Go test:** `TestPO_PrintGolden`
- **Sources:** PO spec §9, §18

### PO-036 · Print needs print and visibility (R3)
- **Priority:** P1
- **Level:** integration
- **Given** a PO **When** a user without the `print` right, or one who can't see the record, asks for its print **Then** it is refused (R3); a user with both gets it.
- **Go test:** `TestPO_PrintRights`
- **Sources:** PO spec §9, §18

### PO-037 · Pending report totals, overdue word, scope filtered
- **Priority:** P1
- **Level:** integration
- **Given** approved POs with pending lines on both projects, one past `required_by` **When** a user opens `/reports/purchase/pending` **Then** there is one row per PO line with pending above 0 on approved POs, with the §13 columns (received includes already received), the overdue row carries the word `Overdue`, and rows go through `Scope`, so `priya` sees only `PRJ-METRO` rows.
- **Go test:** `TestPO_PendingReport`
- **Sources:** PO spec §13, §18

### PO-038 · Pending report CSV needs export (CD-145)
- **Priority:** P1
- **Level:** integration
- **Given** a user with `read` but without `export` on Purchase Order **When** they ask for `/reports/purchase/pending/export.csv` **Then** it is refused; with `export` the CSV is returned.
- **Go test:** `TestPO_PendingExportRight`
- **Sources:** PO spec §13, §18; CD-145

### PO-039 · Settings: only Administrator, audited, stale refused
- **Priority:** P1
- **Level:** integration
- **Given** the single-row `business_settings` (`locked_up_to`, `default_po_terms`) **When** a non-Administrator opens or posts `/settings/business` **Then** it is refused. **When** an Administrator changes it **Then** the change is audited. **When** the page posts a stale `version` **Then** it is refused (CD-94).
- **Go test:** `TestSettings_AdminOnlyAudited`
- **Sources:** PO spec §14.1, §18

### PO-040 · Cutover: counter set to 437 gives first number 00438; lower refused
- **Priority:** P1
- **Level:** integration
- **Given** the `PO/26-27/` series' last number used set to 437 (NAM-056) **When** the next PO is approved **Then** it is `PO/26-27/00438`. Setting a lower number afterwards is refused (NAM-057).
- **Go test:** `TestPO_CutoverCounter`
- **Sources:** PO spec §12, §18

### PO-041 · Default rights match 00-fixtures.md for every role
- **Priority:** P1
- **Level:** unit
- **Then** `purchase.Order`'s `DefaultRights` equal the `purchase.Order` rows of `00-fixtures.md` (spec §10) for every role, including Purchase User `write`, `submit` and `delete` at `own` scope, `amend` at `all`, and `CloseRight` = `amend`.
- **Go test:** `TestPO_DefaultRights`
- **Sources:** PO spec §10, §18, §26 (PO-041 changes with §10); CD-247

### PO-042 · Waiting 3 days appears on My requests on day 3, not day 2, and goes on decision
- **Priority:** P1
- **Level:** integration
- **Given** a pending approval request **Then** My requests shows no badge on day 2, shows `Waiting 3 days` on day 3 (calendar days from `approval.RequestItem`'s submitted time), and shows none once the request is decided.
- **Go test:** `TestApprovalWaiting_Days`
- **Sources:** PO spec §14.3, §18

### PO-043 · Overdue approvals: Administrator only, own refusal text, oldest first
- **Priority:** P1
- **Level:** integration
- **Given** approval requests open for different lengths of time **When** an Administrator calls `(*Approvals).Overdue(ctx, p)` **Then** it lists requests open more than 3 days, oldest first, with who can approve. **When** a non-administrator calls it **Then** it is refused with "You don't have permission to see overdue approvals. Ask an Administrator."
- **Go test:** `TestApprovalOverdue_List`
- **Sources:** PO spec §14.3, §18; CD-152

### PO-044 · Random lifecycle with GRN sequences, over-delivery included: pending never below 0, status matches
- **Priority:** P1
- **Level:** unit (fuzz)
- **Exit:** MOD-GRN exit (spec §18): needs a real GRN.
- **Given** random sequences of PO and GRN actions, over-delivery included **Then** pending is never below 0 and `receipt_status` always matches the lines (§6.3).
- **Go test:** `FuzzPO_ReceiptInvariants`
- **Sources:** PO spec §6.3, §18

### PO-045 · Full PO flow on a phone width, with overflow check
- **Priority:** P1
- **Level:** e2e
- **Given** a phone-width browser **When** a PO is raised, submitted and approved through the screens **Then** each page passes the overflow check.
- **Go test:** `TestE2E_PurchaseOrderFlow`
- **Sources:** PO spec §11, §18

### PO-046 · Back-entry approved on submit, FY from the date, submitter named as approver
- **Priority:** P1
- **Level:** integration
- **Given** no approval rules (CD-235) **When** a PO dated 15-04-2026 is submitted **Then** it is approved on submit with FY 26-27. **When** a PO dated 31-03-2026 is submitted **Then** it takes prefix `25-26` and its own counter. The print names the submitter as approver (it reads the approval history's `approved_no_rule` entry).
- **Go test:** `TestPO_BackEntry`
- **Sources:** PO spec §12, §18

### PO-047 · Amend produces a draft with snapshots filled
- **Priority:** P1
- **Level:** integration
- **Given** a cancelled PO **When** it is amended **Then** the new draft's snapshot columns are filled without a further save, because amend runs `Validate` (M0-10).
- **Go test:** `TestPO_AmendFillsSnapshots`
- **Sources:** PO spec §15 item 8, §18

### PO-048 · Request submit numbers PR/26-27/00001 with no approval; the request date sets the FY
- **Priority:** P1
- **Level:** integration
- **Given** draft request PR-A **When** `suresh` submits it **Then** it moves straight to approved with no approval rule and is numbered `PR/26-27/00001`. The FY of the number comes from `request_date`.
- **Go test:** `TestRequest_SubmitNumbers`
- **Sources:** PO spec §20.2, §20.3, §26

### PO-049 · Suresh raises and sees requests only for his sites, on the real auth.Service
- **Priority:** P1
- **Level:** integration
- **Given** the real `auth.Service` **When** `suresh` (sites `PUNE-WH`, `METRO-SITE`) raises a request for `METRO-SITE` **Then** it saves; for `TECH-SITE` it is refused. **When** he lists requests **Then** he sees only requests for his sites.
- **Go test:** `TestRequest_SiteRestrictionReal`
- **Sources:** PO spec §20.6, §26

### PO-050 · Request rights: owner edits and closes; a Purchase Manager closes but can't edit; another employee and a Purchase User can neither edit nor close
- **Priority:** P1
- **Level:** integration
- **Given** PR-A owned by `suresh` **Then** `suresh` edits the draft and closes it once submitted. `deepak` (another employee, who can't read PR-A) gets 404 on edit and on close (CD-245, CD-309); `priya` (Purchase User) gets AUTH's 403 on each. `ravi` (Purchase Manager) closes PR-A once submitted but gets AUTH's 403 on edit (CD-253). One case runs on the real `auth.Service`.
- **Go test:** `TestRequest_Rights`
- **Sources:** PO spec §20.3, §20.6, §26

### PO-051 · Create PO from a request: only pending lines with request_line_id; nothing pending refused
- **Priority:** P1
- **Level:** integration
- **Given** submitted PR-A and a buyer with `create` on Purchase Order **When** the buyer posts the request's `version` and a vendor to `/modules/purchase/requests/{id}/order` **Then** a PO draft is created through `Engine.Do` with that vendor, the request's site, project and needed-by date, and one line per row with pending to order above 0 (item, description, UoM, qty equal to the pending amount, `request_line_id`). **When** nothing is pending on the request **Then** "Everything on {PR} is already on an order."
- **Go test:** `TestRequest_CreatePO`
- **Sources:** PO spec §20.4, §26

### PO-052 · Add from requests lists only same site and project with pending above 0, and shows On {PO} for held lines
- **Priority:** P1
- **Level:** integration
- **Given** submitted PR-A (`METRO-SITE` / `PRJ-METRO`) and PR-B (`PUNE-WH` / `PRJ-TECHPARK`) **When** a buyer opens Add from requests on a PO for `METRO-SITE` / `PRJ-METRO` **Then** only PR-A's lines with pending above 0 are listed, grouped by request. A line whose pending is held by other POs shows "On {PO display} ({owner})" and can't be ticked.
- **Go test:** `TestRequest_AddFromRequests`
- **Sources:** PO spec §20.4, §26

### PO-053 · Each §20.5 condition gives its own message
- **Priority:** P1
- **Level:** integration
- **Given** a PO line with `request_line_id` **When** it is saved or submitted in each condition **Then** the message is:
  - request is a draft: DOC's "{PR} isn't approved yet. Pick an approved purchase request."
  - request is closed: DOC's "{PR} is closed. Remove the line or pick another purchase request."
  - request is cancelled: DOC's "{PR} is cancelled. Remove the line or pick another purchase request."
  - request's site differs: "Line {n}: Request {PR} is for site {code}. This order delivers to {code}."
  - request's project differs: "Line {n}: Request {PR} is for project {code}. This order is for {code}."
  - item differs: "Line {n}: Request {PR} line {m} is for {item code}. Remove the row to order a different item."
  - UoM differs: "Line {n}: Request {PR} line {m} is in {uom code}. Keep that unit."
  - same request row on two rows: "Line {n}: Request {PR} line {m} is already on line {k}."
  - other POs' ordered plus held reach the row's qty: "Line {n}: Request {PR} line {m} is already on {PO display}." (the oldest such PO)
- **Go test:** `TestPO_RequestRowRules`
- **Sources:** PO spec §20.5, §26

### PO-054 · Two buyers save the same request row at once: the second is refused, on two connections
- **Priority:** P1
- **Level:** integration
- **Given** a request row and two buyers' draft POs **When** both save a line for that row's full pending quantity at once, on two connections **Then** one saves and the other is refused with "Line {n}: Request {PR} line {m} is already on {PO display}." (the check reads the other POs inside the write, CD-94).
- **Go test:** `TestPO_RequestRowRace`
- **Sources:** PO spec §20.5, §26

### PO-055 · ordered_qty and order_status after PO approve, cancel, short close and an amend with request rows
- **Priority:** P1
- **Level:** integration
- **Given** a request and a PO holding its rows **Then** after each action `ordered_qty` and `order_status` follow `lineOrdered` (§20.2): approve counts `qty`; cancel counts 0; a short close counts `min(qty, already_received + received_qty)`, so the rest is freed for a new PO; an amendment copies `request_line_id`. `order_status` is `not_ordered`, `part_ordered` or `ordered`, and the writes bump no version and write no audit entry.
- **Go test:** `TestRequest_OrderStatus`
- **Sources:** PO spec §20.2, §20.3, §20.5, §26

### PO-056 · Request cancel blocked while a PO links it, naming the PO
- **Priority:** P1
- **Level:** integration
- **Given** a submitted request linked by a pending, approved or closed PO **When** its owner cancels it **Then** it is refused with DOC-052's text naming the PO (`LinkSpec`).
- **Go test:** `TestRequest_CancelBlocked`
- **Sources:** PO spec §20.3, §26; `03-document.md` DOC-052

### PO-057 · Random request and PO sequences: order invariants hold
- **Priority:** P1
- **Level:** unit (fuzz)
- **Given** random sequences of request and PO actions **Then** pending to order is never below 0, held plus ordered is never above requested unless one PO over-orders, and `order_status` matches.
- **Go test:** `FuzzRequest_OrderInvariants`
- **Sources:** PO spec §20.2, §20.3, §26

### PO-058 · Only the owner edits, submits, withdraws, revises and deletes a draft, on the real auth.Service
- **Priority:** P1
- **Level:** integration
- **Given** a draft PO owned by `priya`, on the real `auth.Service` **Then** `priya` can edit, submit, withdraw, revise and delete it. `ravi` and a second Purchase User (`arjun`) get 403 on each.
- **Go test:** `TestPO_OwnerOnlyReal`
- **Sources:** PO spec §10, §21.1, §26

### PO-059 · Hand over by owner and by Ravi; every §21.2 message; stale 409; audit and history row
- **Priority:** P1
- **Level:** integration
- **Given** a draft PO owned by `priya` **When** she hands it over, and when `ravi` (holding `approve`) hands one over **Then** `created_by` changes, `version` goes up, an audit entry and the history row "Handed over from {a} to {b}" are written, a given note is posted as a comment by the person handing over, the new owner can edit and the old owner can't. Each refusal gives:
  - approved, closed or cancelled: "An approved, closed or cancelled order can't be handed over."
  - pending approval: "Withdraw this order before handing it over."
  - current owner picked: "This order is already {name}'s."
  - person can't raise POs: "{name} can't raise purchase orders. Pick someone with the Purchase User role."
  - person can't see the project or site: "{name} can't see this order's project or site. Pick someone else."
  - person's sign-in disabled: "{name} can't sign in at the moment. Pick someone else."
  - person handing over has no right to: AUTH's 403 text.
  A stale posted `version` is a 409.
- **Go test:** `TestPO_HandOver`
- **Sources:** PO spec §21.2, §26; CD-247 (2)

### PO-060 · Owner banner and Hand over button shown to the right people
- **Priority:** P1
- **Level:** integration
- **Given** a draft PO owned by `priya` **When** another user who can read it opens the document page or the wizard **Then** they see "Owned by Priya Shah. Only Priya Shah can change this draft. Add a comment below, or ask Priya Shah to hand it over." The owner doesn't see it. `Hand over` shows only to people holding the hand-over right (the owner, or `approve` on Purchase Order).
- **Go test:** `TestPO_OwnerBanner`
- **Sources:** PO spec §21.1, §21.2, §26

### PO-061 · Comments: read needed to view and post; hidden record 404 same as missing; append-only trigger; both messages
- **Priority:** P1
- **Level:** integration
- **Given** a PO with comments opted in **Then** `modkit.AddComment` and `modkit.ListComments` need `read` on the record; a record the user can't read answers AUTH-108's 404, the same as a missing one; a DocType not registered with `modkit.RegisterComments` answers 404; UPDATE and DELETE on `module_comment` are refused by triggers. An empty body gives "Write a comment first." and one over 1,000 characters gives "A comment can be at most 1,000 characters."
- **Go test:** `TestComments_Rules`
- **Sources:** PO spec §22, §26; CD-245

### PO-062 · Comments panel on PO and Request pages in every state, cancelled included; never in print
- **Priority:** P1
- **Level:** integration
- **Given** a PO and a request in each state, cancelled included **Then** the document page shows the comments card, oldest first with name and time, a text area and `Post comment`. The PO print has no comments.
- **Go test:** `TestComments_Panel`
- **Sources:** PO spec §22, §26

### PO-063 · Mark as sent: approved only, owner only, date rules, undo, audited, stale 409
- **Priority:** P1
- **Level:** integration
- **Given** an approved PO owned by `priya` **When** she marks it as sent (posted version 0) **Then** a `purchase_order_sent` row is written and the PO gets an audit entry `Marked as sent`; Mark as not sent (posted current) deletes the row with `Marked as not sent`. The owner, anyone with `approve` on Purchase Order, or an Administrator may do it; anyone else gets AUTH's 403 text. Refusals: not approved "Only an approved order can be marked as sent."; future date "Sent on can't be in the future."; before the order date "Sent on can't be before the order date.". A stale version is a 409 with DOC's stale text.
- **Go test:** `TestPO_MarkSent`
- **Sources:** PO spec §23, §26; CD-247 (6)

### PO-064 · Copy: copied and not-copied columns; copier owns it; restricted refused; check note
- **Priority:** P1
- **Level:** integration
- **Given** a PO the user can read **When** a user with `create` copies it **Then** the draft is owned by the copier and copies vendor, vendor address, site, project, payment terms, terms, reverse charge and the lines' item, description, UoM, qty, price, discount, tax code, HSN/SAC; it does not copy number, dates (order date is today), required by, vendor quote ref, remarks, earlier PO fields, `already_received`, `request_line_id`, attachments, comments or the sent record. `copied_from_no` holds the source's display. A site or project the copier may not use is refused with AUTH-099's text. A disabled header link is left blank and a line with a disabled item, UoM or tax code is left out. The check page notes read "Copied from {display}. Rates are as on that order; check them before you submit.", then "{Label} {code} is disabled, so it was left blank." per blank and "Line {n} of {display} was left out: {label} {code} is disabled." per line.
- **Go test:** `TestPO_Copy`
- **Sources:** PO spec §24.1, §26; CD-247 (7)

### PO-065 · Last price default from same vendor, item, UoM and project, else standard rate; hint scoped
- **Priority:** P1
- **Level:** integration
- **Given** approved or closed POs with lines for the same item **When** a line is saved with a blank price **Then** price and discount come from the newest line (latest `order_date`, then highest id) on an approved or closed PO with the same vendor, item, UoM and project, else the item's standard rate. The Items step note "Last bought at ₹{rate} from {vendor} on {date} ({PO})" is the newest for that item and UoM across vendors, read through `Auth.Scope`, so it never shows a PO the user can't see. `LastPricesFor(ctx, p, keys)` returns the newest per item and UoM, hiding unreadable POs (CD-258 Q6 (a)).
- **Go test:** `TestPO_LastPrice`
- **Sources:** PO spec §24.2, §26

### PO-066 · Purchase home cards and counts for Priya, Ravi, Suresh and Deepak, scoped
- **Priority:** P1
- **Level:** integration
- **Given** POs and requests in each queue's states **When** `priya`, `ravi`, `suresh` and `deepak` open `/modules/purchase` **Then** each sees only the §25 cards their rights allow (My drafts, Waiting for approval, To approve, To send, Awaiting delivery, Overdue deliveries, Requests to order, My requests, Recent comments), and every count goes through `Auth.Scope`.
- **Go test:** `TestPurchaseHome_Cards`
- **Sources:** PO spec §25, §26

### PO-067 · Default rights for Purchase Request match 00-fixtures.md
- **Priority:** P1
- **Level:** unit
- **Then** `purchase.Request`'s `DefaultRights` equal the `purchase.Request` rows of `00-fixtures.md` (spec §20.6) for every role.
- **Go test:** `TestRequest_DefaultRights`
- **Sources:** PO spec §20.6, §26

### PO-068 · Phone flow: Suresh requests, Priya creates the PO, Ravi approves, Priya marks it sent, Suresh sees Ordered
- **Priority:** P1
- **Level:** e2e
- **Given** a phone-width browser **When** `suresh` raises and submits a request, `priya` creates a PO from it, `ravi` approves it and `priya` marks it as sent **Then** `suresh` sees Ordered on his request.
- **Go test:** `TestE2E_RequestToSentFlow`
- **Sources:** PO spec §20, §23, §26

### PO-069 · Closing a request held by an unapproved PO warns; the PO's approve is then refused with DOC's closed text
- **Priority:** P1
- **Level:** integration
- **Given** a submitted request whose rows are on a draft, pending or rejected PO **When** the owner opens the close page **Then** it warns "{n} rows are on orders not yet approved: {list}. Those orders can't be approved until the rows are removed." **When** the request is closed and the PO is approved **Then** the approve is refused with DOC's closed text.
- **Go test:** `TestRequest_CloseWhileHeld`
- **Sources:** PO spec §20.3, §20.5, §26

### PO-070 · Queue pages list only the user's scoped rows, matching the home counts
- **Priority:** P1
- **Level:** integration
- **Given** the same data as PO-066 **When** each user opens `/modules/purchase/q/{queue}` **Then** the rows are only the user's scoped rows and their number matches the card's count.
- **Go test:** `TestPurchaseHome_Queues`
- **Sources:** PO spec §25, §26

## v0.6 cases: procure-to-pay research (CD-272, CD-276)

PO spec §27. Each case cites its research scenario (`docs/research/2026-10-31-procure-to-pay-erp-practice.md` §3, `S{nn}`) and the source the researcher read there. Tests follow the CD-272 patterns: do and undo in one test, the real Engine path through the module's builder, exact message text, quantity walk tables, rollback by row counts, a frozen clock for date rules (`authtest.T0` is 24-10-2026 10:30 IST; a case that names another date sets it), and a comment citing the CD.

### PO-071 · Line qty 0 or −1 refused, on save and on a crafted post
- **Priority:** P1
- **Level:** integration
- **Given** a draft PO **When** line 1 is saved with qty `0`, and again with `-1` posted directly to the engine **Then** each is refused with "Line 1: Quantity must be more than 0." (DOC's positive text), and the line row count is unchanged.
- **Go test:** `TestPO_QtyMustBePositive`
- **Sources:** PO spec §3.2, §27.5; research S14 (ERPNext `test_purchase_order.py:35`, `:74`)

### PO-072 · Negative rate refused
- **Priority:** P1
- **Level:** integration
- **Given** a draft PO **When** line 1 is saved with price `-1.0000` **Then** it is refused with "Line 1: Rate can't be negative." (DOC's not-negative text). Price `0.0000` saves.
- **Go test:** `TestPO_RateNotNegative`
- **Sources:** PO spec §3.2, §27.5; research S15 (ERPNext `test_purchase_order.py:59`)

### PO-073 · Same item on two lines saves; each keeps its own pending
- **Priority:** P1
- **Level:** integration
- **Given** a draft PO with two lines of `MS-ANGLE-50`, 100 kg and 50 kg, with different descriptions **When** it is saved, submitted and approved **Then** both lines save with no warning, and `Pending` is 100 and 50 per line. Two lines with the same request row are still refused (PO-053).
- **Go test:** `TestPO_SameItemTwoLines`
- **Sources:** PO spec §27.4; CD-276; research S16 (ERPNext `test_purchase_order.py:1049` `test_group_same_items`)

### PO-074 · Vendor disabled while the PO is pending: approve refused; a new PO with it refused
- **Priority:** P1
- **Level:** integration
- **Given** `priya`'s PO for `SHREE` pending approval **When** `SHREE` is disabled and `ravi` approves **Then** the approve is refused with DOC's disabled text for the vendor link ("Vendor {display} is disabled. Pick an active vendor, or ask an admin to enable it."), the PO stays pending with no number, and the approval request stays open. **When** a new draft names `SHREE` **Then** its save is refused with the same text. Re-enabling `SHREE` lets the approve through (undo).
- **Go test:** `TestPO_VendorDisabledWhilePending`
- **Sources:** PO spec §4 (`BeforeSubmit`), §27.5; D-APR-15; research S18 (ERPNext `test_purchase_order.py:1074` `test_po_for_blocked_supplier_all`)

### PO-075 · Ordering more than the request row asked warns and saves
- **Priority:** P1
- **Level:** integration
- **Given** PR-A line 1 for 40 Bag **When** a PO line linked to it is saved with 45 Bag **Then** it saves, and `OrderWarnings` (shown on the check page, CD-280) includes "Line 1: ordering 5 more than request PR/26-27/00001 asked for." At 40 Bag there is no warning.
- **Go test:** `TestPO_OverRequestWarning`
- **Sources:** PO spec §20.5, §27.5; research S05 (ERPNext `test_purchase_order.py:242` refuses without an allowance; we warn)

### PO-076 · A rejected or withdrawn PO still holds its request rows; a deleted draft frees them
- **Priority:** P1
- **Level:** integration
- **Given** PR-A line 1 (40 Bag) on `priya`'s PO for 40 Bag **When** the PO is submitted and rejected **Then** pending to order is 0 (held). **When** she revises, resubmits and withdraws it **Then** it is still 0. **When** she deletes the draft **Then** pending to order is 40 again and a second buyer's PO for 40 Bag saves. A walk table asserts `ordered_qty`, held and pending after each step.
- **Go test:** `TestRequest_HeldByRejectedWithdrawnDeleted`
- **Sources:** PO spec §20.2 (`lineOrdered`), §27.5; research S09 (Tryton `modules/purchase_request/tests/scenario_purchase_request.rst:108-144`)

### PO-077 · Closing a PO whose request is already closed succeeds and recomputes ordered_qty
- **Priority:** P1
- **Level:** integration
- **Given** PR-A line 1 (40 Bag) on approved `PO/26-27/00001` for 40 Bag, with 25 received, and PR-A closed by `suresh` **When** a Purchase User closes the PO with a reason **Then** the close succeeds, and PR-A line 1's `ordered_qty` reads 25 (`min(qty, received)`), with `order_status` `part_ordered`. No version bump and no audit entry on PR-A.
- **Go test:** `TestRequest_ClosePOOfClosedRequest`
- **Sources:** PO spec §20.3, §27.5; research S03 (ERPNext `test_material_request.py:1913` `test_purchase_order_of_a_stopped_request_can_be_closed`)

### PO-078 · One request row split across POs to two vendors
- **Priority:** P1
- **Level:** integration
- **Given** PR-A line 1 for 40 Bag **When** `priya` orders 25 Bag from `SHREE` and `arjun` orders 15 Bag from `BHARAT`, and both POs are approved **Then** line 1 reads ordered 40 with `order_status` `ordered` once every line is ordered. `HeldBy` and the Linked orders read list both POs. Cancelling the `BHARAT` PO puts 15 back as pending to order.
- **Go test:** `TestRequest_SplitAcrossVendors`
- **Sources:** PO spec §20.4, §27.5; research S08 (Tryton `modules/purchase_requisition/tests/scenario_purchase_requisition.rst:213`)

### PO-079 · A closed PO leaves Overdue
- **Priority:** P1
- **Level:** integration
- **Given** an approved PO with 15 Bag pending and `required_by` 28-10-2026, with the clock at 31-10-2026 **Then** the pending report has its row marked `Overdue`, and the overdue-only filter lists it. **When** the PO is closed **Then** the report has no row for it, and the overdue count used by the Purchase home's Overdue deliveries card drops by one.
- **Go test:** `TestPO_ClosedLeavesOverdue`
- **Sources:** PO spec §13, §25, §27.5; research S44 (Odoo `addons/purchase_stock/tests/test_purchase_order.py:1100`)

### PO-080 · Expected on: set and clear, rights, approved only, messages, version, audit
- **Priority:** P1
- **Level:** integration
- **Given** approved `PO/26-27/00001` owned by `priya` **When** `arjun` (Purchase User) sets Expected on 05-11-2026 with posted version 0 **Then** a `purchase_order_expected` row is written at version 1 and the PO gets the audit entry "Expected on set to 05-11-2026". **When** `ravi` changes it with posted version 1 **Then** it is version 2. **When** clear is posted with version 2 **Then** the row is deleted with "Expected on cleared". `suresh` (Stores) gets AUTH's 403. Refusals: a draft PO "Only an approved order can have an expected date."; blank "Enter the expected date."; a date before the order date "Expected on can't be before the order date."; a stale version gives a 409 with DOC's stale text, and the row count is unchanged. One case runs on the real `auth.Service`.
- **Go test:** `TestPO_ExpectedOn`
- **Sources:** PO spec §27.1; CD-272 Gap 7; research S31 (Odoo `addons/purchase_stock/tests/test_purchase_order.py:219` `test_04_update_date_planned`)

### PO-081 · Expected on is the due date ahead of required-by
- **Priority:** P1
- **Level:** integration
- **Given** the clock at 31-10-2026 and three approved POs with pending: A with `required_by` 28-10-2026 and no Expected on; B with `required_by` 28-10-2026 and Expected on 05-11-2026; C with no `required_by` and Expected on 30-10-2026 **Then** the pending report marks A and C `Overdue` and not B, the overdue-only filter lists A and C, and the Overdue deliveries count is 2. **When** B's Expected on is cleared **Then** B is overdue again (undo). The CSV carries the Expected on column.
- **Go test:** `TestPO_PendingUsesExpectedOn`
- **Sources:** PO spec §13, §25, §27.1; CD-272 Gap 7; research S31

### PO-082 · SetRestNotComing: on, idempotent, off
- **Priority:** P1
- **Level:** integration
- **Given** approved `PO/26-27/00001` **When** `purchase.SetRestNotComing(ctx, tx, poID, grnID, suresh, true)` runs inside a `db.Write` **Then** one `purchase_order_rest` row exists and the PO has the comment "Vendor says the rest won't come. Close this order if nothing more is due." by `suresh`. The PO's state and version are unchanged. **When** it runs again with the same pair **Then** the row count and comment count are unchanged. **When** it runs with `on=false` **Then** the row is gone and the comment stays; `on=false` for a pair with no row returns nil. An unknown PO id is a plain internal error, and the transaction writes nothing.
- **Go test:** `TestPO_RestNotComing`
- **Sources:** PO spec §27.2; CD-272 Gap 8, CD-276; research S43 (Odoo `addons/purchase_stock/tests/test_purchase_order.py:877`). The GRN tick end to end is MOD-GRN's GRN-047.

### PO-083 · To close queue
- **Priority:** P1
- **Level:** integration
- **Given** two approved PRJ-METRO POs with a rest row (set on 25-10-2026 and 27-10-2026), one approved PO without, and one approved PRJ-TECHPARK PO with a row **When** `priya` opens the Purchase home and `/modules/purchase/q/to-close` **Then** the To close card counts 2 (the PRJ-TECHPARK PO is outside her scope), and the queue lists both, oldest row first, each with the meta "Vendor says the rest won't come · 25-10-2026" (and · 27-10-2026); no row names a GRN. `suresh` (no `amend`) gets no card, and the queue is a 404 for him. **When** one PO is closed **Then** the count is 1 and the queue lists one row.
- **Go test:** `TestPurchaseHome_ToClose`
- **Sources:** PO spec §25, §27.2; CD-272 Gap 8; CD-240 family (rows name no GRN); research S43 (Odoo `test_purchase_order.py:877`)

### PO-084 · Site and project branches in different states warn
- **Priority:** P1
- **Level:** integration
- **Given** site `PUNE-WH` (branch PUNE, Maharashtra, no project) and project `PRJ-TECHPARK` (branch BLR, Karnataka) **When** a PO with both is saved **Then** it saves, and `OrderWarnings` (shown on the check page, CD-280) returns "Site PUNE-WH is under branch PUNE in Maharashtra, but project PRJ-TECHPARK belongs to branch BLR in Karnataka. Check the tax with your CA.", and the check page shows it. With `METRO-SITE` and `PRJ-METRO` (both PUNE) there is no warning.
- **Go test:** `TestPO_SiteProjectStateWarning`
- **Sources:** PO spec §27.3; CD-272 Gap 14, CD-276 (branch states compared); research S72 (IGST s.10(1)(a))

### PO-085 · An over-received line shows Over by on the document page
- **Priority:** P1
- **Level:** integration
- **Given** an approved cutover PO (Earlier PO no. `P-17`) with a line for 40 Bag and `already_received` 45 (PO-032's path: it warns and saves), and a second line for 40 Bag with `received_qty` 45 set through `purchasetest.SetReceived` **Then** the document page's line notes include "Over by 5 Bag" (`types.FormatQtyUoM`), pending reads 0, and the receipt status is `fully_received`. At 40 or below there is no Over by note. Both lines show the note. MOD-GRN's GRN-014 covers the real GRN path.
- **Go test:** `TestPO_OverByLineNote`
- **Sources:** PO spec §27.5; GRN spec §4; CD-234 (over-receipt warns only), CD-277
