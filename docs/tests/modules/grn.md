# Goods Receipt (GRN)

## Scope
The Goods Receipt module of `docs/specs/2026-10-30-module-grn-spec.md` v0.5: `goodsreceipt.Receipt` against one approved `purchase.Order`, received, rejected and accepted quantities, the over-delivery warning, the PO's receipt status, the period lock, bill details, print and the two reports. Cases are spec §16: GRN-001 to GRN-036 from v0.4, and scenario cases GRN-037 to GRN-050 from the procure-to-pay research (CD-272) and the cross-module flows X1 and X2 (CD-271), in one area, `GRN`. Each scenario case cites its source: an S-number or gap in `docs/research/2026-10-31-procure-to-pay-erp-practice.md`, or an X-number in `docs/research/2026-10-31-expense-erp-practice.md` §4, with the ERP test it follows. The plan is `docs/superpowers/plans/2026-10-31-mod-grn.md`; the **Built in** line of each case names its task.

Fixtures are in `docs/tests/core/00-fixtures.md`, including the `goodsreceipt.Receipt` rights row and the GRN test seed (no approval rule for `purchase.Order` or `goodsreceipt.Receipt`, so both approve on submit). Unless a case says otherwise, **the reference PO** is the one in `00-fixtures.md`, raised by `priya` for vendor `SHREE`, delivering to `METRO-SITE` (branch PUNE, project `PRJ-METRO`), approved and numbered `PO/26-27/00001`: line 1 MS-ANGLE-50 1,200.000 kg at net rate ₹62.5000, line 2 WIRE-18G 50.000 kg at ₹96.0000. `suresh` (Stores; sites PUNE-WH and METRO-SITE) receives. The clock is 2026-10-24 10:30 IST.

**PO cases built here (MOD-GRN's exit).** PO-025, PO-031, PO-033 and PO-044 stay in `po.md`, where they are marked **Exit:**. They are built in this module's tasks: PO-031 and PO-044 in G3 (effects and receipt status, with GRN-035's fuzz), PO-025 and PO-033 in G4 (PO interplay). No test-local stand-in for the PO or the GRN (CD-125).

**Texts.** Line-level texts start `Line {n}: ` (CD-264). Refusals owned by other packages are expected by their exact text (CD-142): DOC-017, DOC-038, DOC-052, DOC-069, R8, AUTH-101 as CD-240 amended it (`You can't use this Purchase Order. It isn't available to you.`), AUTH's 403, `uom.CheckLineQty`, the period lock (PO spec §14.2) and the close refusal of the plan's G0a.

Review focus (spec §16): GRN-015, GRN-022 and GRN-029 run as non-admin users on the real services, because tests as an Administrator hid bugs (CD-213). GRN-012 and GRN-016 guard the roll-up. X5 (a buyer's own cash buy on an expense claim, CD-271) goes to whichever of MOD-EXP and MOD-GRN finishes last, as the next free id.

**Test patterns (research §2, CD-272).** Tests use `grntest` builders that go through the real engine and can stop at draft, submitted or approved; effect tests assert after the action and after its reversal (do and undo); refusals assert the exact text and that nothing was written (row counts for rollbacks); quantity cases are written as walk tables (ordered → received → cancelled); the clock is frozen at 2026-10-24 10:30 IST; each test's comment cites the CD it guards.

## Test cases

### GRN-001 · Only approved, visible POs are in the picker; fully received ones only when widened
- **Priority:** P1
- **Level:** integration
- **Given** approved POs with pending on METRO-SITE and on TECH-SITE, a fully received approved PO on METRO-SITE, a closed PO and a draft PO on METRO-SITE **When** `suresh` lists PO choices **Then** only the METRO-SITE PO with pending is listed, with its PO number, vendor, site and the count of lines with pending. **When** he widens with `Show fully received orders` **Then** the fully received one is added; the TECH-SITE, closed and draft POs never are. On the real `auth.Service`.
- **Go test:** `TestGRN_POPicker`
- **Sources:** GRN spec §8, §11, §16; AUTH-110
- **Built in:** G6

### GRN-002 · Posted PO on another site: AUTH-101's 403, on the real auth.Service
- **Priority:** P1
- **Level:** integration
- **Given** an approved PO delivering to `TECH-SITE` (raised by `admin`) **When** `suresh` posts it on step 1, and also creates a GRN for it directly through the engine with `site_id` set to METRO-SITE **Then** both are refused with 403 and "You can't use this Purchase Order. It isn't available to you." (CD-240), and no draft exists.
- **Go test:** `TestGRN_PONotVisibleReal`
- **Sources:** GRN spec §8, §10, §16; `01-auth.md` AUTH-101; CD-240
- **Built in:** G6

### GRN-003 · Draft copies vendor, site, project and lines from the PO
- **Priority:** P1
- **Level:** integration
- **Given** the reference PO with 400.000 kg of line 1 already received on an approved GRN **When** `suresh` creates a draft from it **Then** `vendor_id` is SHREE, `site_id` METRO-SITE and `project_id` PRJ-METRO, the header snapshots hold the PO's number, date, vendor name, GSTIN and address and the site name, and the draft has two lines: line 1 `received_qty` 800.000 and line 2 50.000, each with the PO line's UoM, item snapshot and net rate. A fully received PO gives a draft with no lines.
- **Go test:** `TestGRN_CopiesFromPO`
- **Sources:** GRN spec §3.1, §3.2, §11, §16
- **Built in:** G6

### GRN-004 · Changing the PO on a draft with lines is refused
- **Priority:** P1
- **Level:** unit
- **Given** a draft GRN with lines against the reference PO **When** it is saved with another approved PO in `purchase_order_id` **Then** it is refused on that field with "Delete this draft to receive against another order." and nothing changes. **Given** a draft with no lines **When** the PO is changed **Then** it saves and the snapshots follow the new PO. A blank PO is refused with "Choose a purchase order." After each refused step save, no derived value is stale (CD-302): the header snapshots, vendor, site and project match the stored PO, and each line's snapshots match its own PO line.
- **Go test:** `TestGRN_POFixedOnceLines`
- **Sources:** GRN spec §3.1, §4, §16
- **Built in:** G2

### GRN-005 · Line from another PO refused
- **Priority:** P1
- **Level:** integration
- **Given** a draft GRN against the reference PO **When** a line is saved with `po_line_id` of a line of another approved PO on METRO-SITE (readable by `suresh`, so AUTH-101 doesn't fire first) **Then** it is refused with "Line 1: This line isn't on PO/26-27/00001." on that line's `po_line_id`. **When** two lines carry the same `po_line_id` **Then** "Line 2: This PO line is already on line 1."
- **Go test:** `TestGRN_ForeignLine`
- **Sources:** GRN spec §4, §16
- **Built in:** G2

### GRN-006 · Date rules: future and challan-after-receipt refuse; before PO warns
- **Priority:** P1
- **Level:** unit
- **Given** a draft GRN **When** `receipt_date` is 25-10-2026 **Then** "Receipt date can't be in the future." **When** `challan_date` is after `receipt_date` **Then** "Challan date can't be after the receipt date." A blank receipt date saves as today. **When** `receipt_date` is before the PO's order date **Then** it saves and the warnings include "Receipt date is before the order date {date}." with the date as dd-mm-yyyy.
- **Go test:** `TestGRN_DateRules`
- **Sources:** GRN spec §3.1, §4, §16
- **Built in:** G2 (refusals), G5 (warning rows added to the same test)

### GRN-007 · Blank challan refused for goods, allowed on a service-only GRN
- **Priority:** P1
- **Level:** integration
- **Given** a GRN with a goods line and a blank challan no. **When** it is submitted **Then** it is refused with "Challan or invoice no. is required for goods." on `challan_no` (422, state and version unchanged); the draft save is not refused. **Given** a GRN whose only line is SVC-CRANE **When** it is submitted with a blank challan **Then** it is approved.
- **Go test:** `TestGRN_ChallanRule`
- **Sources:** GRN spec §3.1, §4, §16; CD-264 (2)
- **Built in:** G2

### GRN-008 · Rejected above received; reason required at submit
- **Priority:** P1
- **Level:** unit
- **Given** a line with received 100.000 **When** rejected 120.000 is saved **Then** "Line {n}: Rejected can't be more than received." on `rejected_qty`. **When** rejected is 20.000 with a blank reason **Then** the draft saves, and submit is refused with "Line {n}: Reason for rejecting is required." on `reject_reason`.
- **Go test:** `TestGRN_RejectRules`
- **Sources:** GRN spec §3.2, §4, §16
- **Built in:** G2

### GRN-009 · Accepted = received − rejected; value at the PO's net rate
- **Priority:** P1
- **Level:** unit
- **Given** line 1 received 800.000 kg, rejected 40.000 kg, and line 2 received 50.000 kg **When** it is saved **Then** line 1 `accepted_qty` is 760.000 and line 2 50.000, and `accepted_value` is ₹52,300.00 (760 × ₹62.5000 + 50 × ₹96.0000), before tax. **When** rejected on line 1 is then raised to 900.000 (refused) **Then** line 1's `accepted_qty` is 0 and `accepted_value` is the sum of the lines as stored (CD-302). **Given** lines, each valid, whose accepted values sum above ₹10,00,00,00,000.00 **When** saved **Then** "The total is too large. The most a Goods Receipt can be is ₹10,00,00,00,000.00. Split it into more than one Goods Receipt." on the lines, and `accepted_value` is cleared (CD-308).
- **Go test:** `TestGRN_AcceptedValue`
- **Sources:** GRN spec §3.1, §3.2, §16
- **Built in:** G2

### GRN-010 · Submit with no rule: approved and numbered GR/26-27/00001
- **Priority:** P1
- **Level:** integration
- **Given** a valid draft GRN and no approval rule **When** `suresh` submits it **Then** it is approved at once and numbered `GR/26-27/00001`.
- **Go test:** `TestGRN_SubmitApproves`
- **Sources:** GRN spec §3, §5, §6, §16
- **Built in:** G3

### GRN-011 · Partial receipts: PO pending and status step through part to full
- **Priority:** P1
- **Level:** integration
- **Given** the reference PO (`not_received`) **When** a GRN accepting 400.000 kg of line 1 is approved **Then** line 1's `received_qty` is 400.000, its pending 800.000, and `receipt_status` is `part_received`. **When** a second GRN accepts 800.000 kg of line 1 and 50.000 kg of line 2 **Then** both pendings are 0, `receipt_status` is `fully_received`, and the PO stays `approved`.
- **Go test:** `TestGRN_PartialReceipts`
- **Sources:** GRN spec §5, §16; PO spec §3.2, §6.3
- **Built in:** G3

### GRN-012 · Rejected quantity stays pending on the PO
- **Priority:** P1
- **Level:** integration
- **Given** the reference PO **When** a GRN receiving 1,200.000 kg of line 1 with 200.000 rejected is approved **Then** line 1's `received_qty` is 1,000.000 and its pending 200.000, and `receipt_status` is `part_received`.
- **Go test:** `TestGRN_RejectedStaysPending`
- **Sources:** GRN spec §3.2, §4, §16; CD-234
- **Built in:** G3

### GRN-013 · Accepted above pending warns on the step, check and document pages, and submits; received above pending with the excess rejected doesn't warn
- **Priority:** P1
- **Level:** integration
- **Given** line 1 with 1,200.000 kg pending **When** `suresh` enters 1,250.000 kg received **Then** the Received quantities step, the check page and, after submit, the document page show "Line 1: Accepted 1,250 KGS is more than the 1,200 KGS pending on PO/26-27/00001." (`types.FormatQtyUoM`, trailing zeros dropped), and submit succeeds. **When** he enters 1,250.000 received with 50.000 rejected **Then** no warning shows on any of the three pages.
- **Go test:** `TestGRN_OverDeliveryWarns`
- **Sources:** GRN spec §4, §11, §16; CD-238
- **Built in:** S3 (pages); G5 builds `Warnings` and `LineWarnings`

### GRN-014 · Over-delivery: PO pending floors at 0, Over by shows, status is fully_received; a GRN against a fully received PO works
- **Priority:** P1
- **Level:** integration
- **Given** the reference PO **When** GRNs accepting 1,250.000 kg of line 1 and 50.000 kg of line 2 are approved **Then** line 1's `received_qty` is 1,250.000, its pending 0 (never negative), and `receipt_status` is `fully_received` (the PO page's `Over by 50 KGS` note is MOD-PO P15b's, CD-277). **When** another GRN against the now fully received PO is created and approved **Then** it succeeds and the status stays `fully_received`. G5 adds the row that this GRN carries the over-delivery warning (as GRN-006).
- **Go test:** `TestGRN_OverDeliveryPO`
- **Sources:** GRN spec §4, §16; PO spec §3.2; CD-238
- **Built in:** G3 (the warning row in G5)

### GRN-015 · Two concurrent GRNs both pass; roll-up equals their sum (real engine, non-admin user)
- **Priority:** P1
- **Level:** integration
- **Given** two draft GRNs against line 1 of the reference PO, 700.000 kg and 600.000 kg, by two non-admin Stores users (`suresh` and a second Stores user on METRO-SITE created in the test's setup) **When** both are submitted at the same time on two connections **Then** both are approved, line 1's `received_qty` is 1,300.000 kg, `receipt_status` matches it, and no second connection was opened inside a transaction (nestcheck).
- **Go test:** `TestGRN_ConcurrentReceipts`
- **Sources:** GRN spec §4, §16; CD-94, CD-213
- **Built in:** G5

### GRN-016 · Already received counts toward pending and the warning
- **Priority:** P1
- **Level:** integration
- **Given** a cutover PO whose line 1 has `already_received` 1,000.000 kg of 1,200.000 **When** a GRN accepts 300.000 kg of line 1 **Then** the warning reads "Line 1: Accepted 300 KGS is more than the 200 KGS pending on {PO number}."; after approval, pending is 0, `received_qty` is 300.000 and `already_received` is still 1,000.000.
- **Go test:** `TestGRN_AlreadyReceivedCounts`
- **Sources:** GRN spec §4, §16; PO spec §3.2, §12
- **Built in:** G5

### GRN-017 · Whole-number UoM refuses a fraction; both RegisterQtyUse calls wired
- **Priority:** P1
- **Level:** integration
- **Given** a PO line for BOLT-M12 in Nos (whole number) **When** a GRN line is saved with received 10.5 or rejected 0.5 **Then** each is refused with `uom.CheckLineQty`'s text (`Line {n}: …`, CD-277) on `received_qty` or `rejected_qty`. **Given** one approved GRN whose only fractional quantity is a received 10.5 KGS, and another (in a separate database) whose only fractional quantity is a rejected 0.5 KGS, both on whole PO quantities **When** KGS is made whole-number **Then** MST-092's check finds each GRN, so each registered use is wired.
- **Go test:** `TestGRN_WholeNumber`
- **Sources:** GRN spec §3.2, §4, §16; `06-masters.md` MST-092
- **Built in:** G2

### GRN-018 · Cancel restores pending and status; reason required
- **Priority:** P1
- **Level:** integration
- **Given** the PO of GRN-011 after its first GRN (`part_received`) **When** `ravi` (Purchase Manager) cancels that GRN without a reason **Then** it is refused with the kernel's reason text. **When** he cancels with a reason **Then** the GRN is cancelled, line 1's `received_qty` returns to 0, pending to 1,200.000, and `receipt_status` to `not_received`. `suresh` can't cancel (AUTH's 403).
- **Go test:** `TestGRN_CancelRestores`
- **Sources:** GRN spec §5, §10, §16
- **Built in:** G3

### GRN-019 · Cancel with the PO closed: allowed, PO stays closed
- **Priority:** P1
- **Level:** integration
- **Given** a part-received PO closed by `priya` **When** `ravi` cancels the GRN against it **Then** the cancel succeeds, the PO stays `closed`, and its `received_qty` and `receipt_status` follow the cancel.
- **Go test:** `TestGRN_CancelClosedPO`
- **Sources:** GRN spec §5, §16; D-DOC-09
- **Built in:** G4

### GRN-020 · Closed PO refuses a new GRN (DOC-017)
- **Priority:** P1
- **Level:** integration
- **Given** a closed PO **When** `suresh` creates or submits a GRN against it **Then** it is refused with DOC-017's text and nothing is written.
- **Go test:** `TestGRN_ClosedPORefused`
- **Sources:** GRN spec §3, §16; `03-document.md` DOC-017
- **Built in:** G4

### GRN-021 · PO cancel blocked by the GRN, then allowed after its cancel
- **Priority:** P1
- **Level:** integration
- **Given** the reference PO with an approved GRN `GR/26-27/00001` **When** `ravi` cancels the PO **Then** it is refused with DOC-052's text naming the GRN, and the PO stays approved. **When** the GRN is cancelled first **Then** the PO's cancel succeeds.
- **Go test:** `TestGRN_BlocksPOCancel`
- **Sources:** GRN spec §3, §16; `03-document.md` DOC-052
- **Built in:** G4

### GRN-022 · Amend by restricted Suresh: draft visible, snapshots filled
- **Priority:** P1
- **Level:** integration
- **Given** `GR/26-27/00001` on METRO-SITE cancelled by `ravi` **When** `suresh` amends it, on the real `auth.Service` **Then** the amendment is a draft he can open, owned by him, with `purchase_order_id` and the lines copied, and its header and line snapshots filled before any further save (amend runs `Validate`, M0-10). When submitted it is numbered `GR/26-27/00001-1`.
- **Go test:** `TestGRN_AmendRestrictedReal`
- **Sources:** GRN spec §5, §10, §16; PO spec §15 item 8; CD-247
- **Built in:** G6

### GRN-023 · Lock off by default; on, it refuses submit and cancel at the boundary date; the day after passes; unlocking clears it
- **Priority:** P1
- **Level:** integration
- **Given** no lock **Then** a GRN dated 30-09-2026 submits. **Given** `Locked up to` 30-09-2026 **When** a GRN dated 30-09-2026 is submitted **Then** it is refused with the lock text from PO spec §14.2 naming the GRN (`Draft {id}` before numbering) as a field error on `receipt_date` (422, CD-282). **When** an approved one with that date is cancelled **Then** it is refused with the same text as a refusal. A GRN dated 01-10-2026 passes. **When** the lock is cleared **Then** the refused actions pass.
- **Go test:** `TestGRN_PeriodLock`
- **Sources:** GRN spec §5, §16; PO spec §14.2
- **Built in:** G5

### GRN-024 · Cancel refused by lock leaves nothing changed (rollback)
- **Priority:** P1
- **Level:** integration
- **Given** an approved GRN dated inside a locked period **When** its cancel is refused by the lock **Then** the GRN is still approved with the same version, the PO's `received_qty` and `receipt_status` are unchanged, and no audit or history row was added (DOC-077).
- **Go test:** `TestGRN_LockRollsBack`
- **Sources:** GRN spec §5, §16; `03-document.md` DOC-077
- **Built in:** G5

### GRN-025 · Bill details: refused before approval; saved after; audited
- **Priority:** P1
- **Level:** integration
- **Given** a draft GRN **When** `suresh` saves bill details **Then** "Add bill details after the receipt is approved." (422). **Given** the GRN approved **When** he saves invoice no. `SST/2627/0457`, date 24-10-2026 and total ₹61,714.00 **Then** they are stored with version 1, an audit entry on the GRN lists each changed column, and the reply carries "Accepted value before tax is ₹50,000.00." (the GRN accepted 800 KGS of line 1 at ₹62.5000) After the GRN is cancelled the bill is still shown and a save is refused with the same text.
- **Go test:** `TestGRNBill_StateRules`
- **Sources:** GRN spec §3.3, §16; CD-234
- **Built in:** G7

### GRN-026 · Bill details: stale version 409, no write right 403, closed PO doesn't matter
- **Priority:** P1
- **Level:** integration
- **Given** an approved GRN with bill details at version 1 **When** a save posts version 0 **Then** 409 with DOC's stale wording for Bill details. **When** `priya` (Purchase User, no `write`) saves **Then** AUTH's 403. **When** the PO is closed and `suresh` saves with the right version **Then** it saves. A hidden GRN gives the 404 of a missing one.
- **Go test:** `TestGRNBill_VersionAndRights`
- **Sources:** GRN spec §3.3, §16; CD-94, CD-245
- **Built in:** G7

### GRN-027 · Same invoice on two GRNs warns and saves
- **Priority:** P1
- **Level:** integration
- **Given** `GR/26-27/00001` with invoice `SST/2627/0457` from SHREE **When** the same invoice no. is saved on `GR/26-27/00002` from SHREE **Then** it saves with "Invoice SST/2627/0457 is also on GR/26-27/00001." **When** the viewer can't read the other GRN (the other GRN is against a TECH-SITE PO and the viewer is `suresh`, saving on his METRO-SITE GRN) **Then** the warning reads "Invoice SST/2627/0457 is also on another receipt." A cancelled GRN with that invoice doesn't warn; the same number from another vendor doesn't warn.
- **Go test:** `TestGRNBill_DuplicateWarns`
- **Sources:** GRN spec §3.3, §16; CD-240, CD-248
- **Built in:** G7

### GRN-028 · Bill photo added after approval by Stores (D-FILE-06)
- **Priority:** P1
- **Level:** integration
- **Given** an approved GRN **When** `suresh` attaches a photo of the invoice **Then** it is stored and listed on the GRN; he can't remove it after approval (D-FILE-06).
- **Go test:** `TestGRN_LatePhoto`
- **Sources:** GRN spec §3.3, §16; `05-files-print.md` D-FILE-06; CD-243
- **Built in:** G7

### GRN-029 · Restricted Suresh creates a GRN for his site; a posted other-site PO gets AUTH-101's refusal; a tampered site_id is reset
- **Priority:** P1
- **Level:** integration
- **Given** `suresh` on the real `auth.Service` **When** he saves step 1 with no PO **Then** "Choose a purchase order." (422, not the engine's 404). **When** he creates a GRN for the reference PO **Then** it is created with `site_id` METRO-SITE. **When** he posts a TECH-SITE PO **Then** "You can't use this Purchase Order. It isn't available to you." (403). **When** he saves his draft with `site_id` posted as PUNE-WH **Then** `Validate` resets it to METRO-SITE.
- **Go test:** `TestGRN_RestrictedCreateReal`
- **Sources:** GRN spec §3.1, §8, §10, §16; CD-240
- **Built in:** G6

### GRN-030 · Print golden, with rejected reason and transport details
- **Priority:** P1
- **Level:** integration (golden file)
- **Given** an approved GRN with vehicle no., transporter, e-way bill no., remarks and a line with a rejected quantity and reason **When** it is printed **Then** it matches the golden `grn_module_approved`, titled "Goods Receipt Note", with the reason under the item. A service-only GRN with no challan prints `-` for the challan.
- **Go test:** `TestGRN_PrintGolden`
- **Sources:** GRN spec §9, §16; `05-files-print.md` FILE-067
- **Built in:** G8

### GRN-031 · Bills to enter: filters, scope, CSV needs export
- **Priority:** P1
- **Level:** integration
- **Given** approved GRNs on METRO-SITE and TECH-SITE, one with bill details **When** `suresh` opens Bills to enter **Then** only METRO-SITE rows show; `No bill details yet` leaves out the one with a bill; the vendor and month filters narrow the rows. **When** `suresh` (no `export`) asks for the CSV **Then** AUTH's 403; `priya` (Purchase User, `export`) gets it.
- **Go test:** `TestGRN_BillsReport`
- **Sources:** GRN spec §12, §16; AUTH-090, AUTH-110
- **Built in:** G9

### GRN-032 · Received items report columns read back
- **Priority:** P1
- **Level:** integration
- **Given** an approved GRN with a rejected line **When** the Received items report and its CSV are read **Then** every column (GRN no., date, vendor, PO no., item, HSN, received, accepted, rejected, reason, UoM, rate, accepted value) reads back its value, one assertion per column; the rejected filter keeps only lines with rejected above 0, and the item filter only that item's lines (CD-282).
- **Go test:** `TestGRN_ItemsReport`
- **Sources:** GRN spec §12, §16
- **Built in:** G9

### GRN-033 · Item disabled after the PO: receipt still succeeds
- **Priority:** P1
- **Level:** integration
- **Given** the reference PO approved **When** MS-ANGLE-50, its UoM kg and the vendor SHREE are disabled, and `suresh` creates and submits a GRN for it **Then** it is approved; no master check refuses it.
- **Go test:** `TestGRN_DisabledMasterStillReceives`
- **Sources:** GRN spec §3.1, §8, §16
- **Built in:** G2

### GRN-034 · Default rights match §10
- **Priority:** P1
- **Level:** unit
- **Then** on the real `auth.Service`, `goodsreceipt.Receipt`'s rights are: Stores read, create, write, submit, amend, print (all), delete (own); Purchase User read, print, export (all); Purchase Manager read, cancel, print, export (all); Director read, print, export (all); Administrator every applicable right. `CloseRight` is `doctype.CloseNone`: close through the engine is refused for every role, Administrator included, with "Goods Receipts can't be closed." (the page's missing Close action is tested in S3, CD-277).
- **Go test:** `TestGRN_DefaultRights`
- **Sources:** GRN spec §5, §10, §14 item 4, §16; CD-264 (7)
- **Built in:** G1 (the close page check in S3)

### GRN-035 · Random GRN sequences: pending never below 0, status matches, roll-up equals the sum
- **Priority:** P1
- **Level:** unit (fuzz)
- **Given** random sequences of GRN create, save, submit, cancel and amend, and PO saves of `already_received` (draft POs), PO close, PO cancel (refused while a GRN is live) and PO amend, against one or two POs, with over-delivery and rejected quantities **Then** after every step each PO line's pending is never below 0, `receipt_status` matches the pendings, `received_qty` equals the sum of accepted on approved and closed GRN lines, and no GRN action changes `already_received`.
- **Go test:** `FuzzGRN_Invariants`
- **Sources:** GRN spec §4, §5, §16; CD-180
- **Built in:** G3

### GRN-036 · Phone flow at 360 px, with overflow check
- **Priority:** P1
- **Level:** e2e
- **Given** `suresh` signed in at 360 px **When** he picks an approved five-line PO on METRO-SITE, enters the challan, receives all five lines with one rejected and its reason, and submits **Then** the GRN is approved and numbered, nothing on any step is wider than the screen, and every step answers within its deadline.
- **Go test:** `TestE2E_GRNFlow`
- **Sources:** GRN spec §11, §16; design system §1.3
- **Built in:** S6

### GRN-037 · A posted draft or pending PO is refused with DOC's source-state text
- **Priority:** P1
- **Level:** integration
- **Given** a draft PO and a PO waiting for approval (a PO approval rule added in this test), both on METRO-SITE **When** `suresh` posts each on step 1, and creates a GRN for each through the engine **Then** each is refused with DOC's source-state text as DOC renders it (lower-cased label, unnumbered PO shown as `Draft {id}`, CD-142): the draft gives "Draft {id} isn't approved yet. Pick an approved purchase order." and the pending one "Draft {id} is waiting for approval. Pick an approved purchase order.", and nothing is written. Step 1's `DraftFromPO` doesn't check the PO's state, so both paths reach the engine's refusal.
- **Go test:** `TestGRN_UnapprovedPORefused`
- **Sources:** research S33 (ERPNext `test_purchase_order.py:87` `test_make_purchase_receipt`); GRN spec §3, §16; `doc/messages.go` `msgSourceNotApproved`, `msgSourcePending`; `doc/links.go` `checkSource`
- **Built in:** G4

### GRN-038 · A fully rejected GRN submits; the PO's roll-up and status don't move
- **Priority:** P1
- **Level:** integration
- **Given** the reference PO **When** a GRN receiving 10.000 kg of line 1 with 10.000 rejected and a reason is submitted **Then** it is approved with accepted 0 and accepted value ₹0.00, line 1's `received_qty` stays 0, pending 1,200.000, `receipt_status` stays `not_received`. Cancelling it changes nothing on the PO either (do and undo).
- **Go test:** `TestGRN_FullyRejected`
- **Sources:** research S35, Gap 10 (ERPNext `test_purchase_receipt.py:77`, `:3276` `test_return_of_a_fully_rejected_receipt`); GRN spec §4; CD-234
- **Built in:** G3

### GRN-039 · A draft GRN whose PO is then closed or cancelled is refused on its next save and submit
- **Priority:** P1
- **Level:** integration
- **Given** a draft GRN against the reference PO **When** `priya` closes the PO and `suresh` saves or submits the draft **Then** each is refused with DOC-017's text. **Given** a draft GRN against a PO that `ravi` then cancels (no other GRN blocks it, since a draft doesn't block) **When** the draft is saved or submitted **Then** DOC-038's text. Nothing is written.
- **Go test:** `TestGRN_DraftAfterPOClosed`
- **Sources:** research S58 (Odoo `purchase_stock/tests/test_purchase_order_process.py:105`); `03-document.md` DOC-017, DOC-038
- **Built in:** G4

### GRN-040 · Cancelling a GRN on a short-closed PO whose lines came from a request updates the request's ordered quantity
- **Priority:** P1
- **Level:** integration
- **Given** request `PR-A` row of 40 bags pulled into a PO, a GRN accepting 25 approved, and the PO closed (so the request reads 25 ordered) **When** `ravi` cancels the GRN **Then** the request's `ordered_qty` drops to 0 and its `order_status` to `not_ordered`; the PO stays closed. Re-approving a replacement (amendment) accepting 25 brings it back to 25 (do and undo).
- **Go test:** `TestGRN_CancelClosedPOUpdatesRequest`
- **Sources:** research S59; PO spec §20.2 (`lineOrdered` on a closed PO), §20.3 (the GRN's call)
- **Built in:** G4

### GRN-041 · Amend replacing a cancelled GRN on a closed PO is allowed; a new GRN still isn't; the cancel page warns
- **Priority:** P1
- **Level:** integration
- **Given** a GRN accepting 600.000 kg against the reference PO, which `priya` then closes **When** `ravi` opens the GRN's cancel page **Then** it shows "PO/26-27/00001 is closed. After this cancel, only an amendment of this receipt can receive against it again." **When** he cancels it and `suresh` amends it to 580.000 and submits **Then** the amendment is approved as `GR/26-27/00001-1`, line 1's `received_qty` is 580.000, and the PO stays closed. **When** `suresh` creates a fresh GRN against the closed PO **Then** DOC-017's text.
- **Go test:** `TestGRN_AmendOnClosedPO`
- **Sources:** research S60, Gap 2 (ERPNext re-open, `test_purchase_order.py:1327`); GRN spec §5; CD-272
- **Built in:** G4 (service; the cancel page warning text in S3)

### GRN-042 · Cancel and amend as the return: pending follows, bill details move, the lock refuses
- **Priority:** P1
- **Level:** integration
- **Given** `GR/26-27/00001` accepting 800.000 kg of line 1, with bill details (invoice `SST/2627/0457`) **When** goods are found bad, `ravi` cancels it and `suresh` amends it to accept 700.000 and submits **Then** the walk reads: pending 1,200 → 400 → 1,200 → 500; the amendment `GR/26-27/00001-1` has the bill details with an audit entry `Copied from GR/26-27/00001`, and the cancelled GRN still shows its own, read-only. **Given** `Locked up to` on or after the GRN's date **When** the cancel is tried **Then** the lock text, and nothing changes (row counts).
- **Go test:** `TestGRN_CancelAmendReturn`
- **Sources:** research Gap 1, S52 (ERPNext `test_purchase_order.py:781`, Odoo `test_purchase_order.py:83` return pickings); GRN spec §3.3, §5; CD-272
- **Built in:** G7

### GRN-043 · Accepted above twice the ordered quantity is refused; twice exactly saves with the warning
- **Priority:** P1
- **Level:** unit
- **Given** line 1 ordered 1,200.000 kg **When** received 2,400.010 kg is saved **Then** "Line 1: Accepted 2,400.01 KGS is more than twice the 1,200 KGS ordered. Check the quantity." on `received_qty`. **When** 2,400.000 is saved **Then** it saves (G5 adds the row that it carries the over-delivery warning). **When** received is 3,000.000 with 700.000 rejected (accepted 2,300.000) **Then** it saves.
- **Go test:** `TestGRN_TwiceOrderedStop`
- **Sources:** research Gap 3, S47 (ERPNext `controllers/status_updater.py:524-621` over-allowance); GRN spec §4; CD-272
- **Built in:** G2 (the warning row in G5)

### GRN-044 · All lines received as ordered only when every pending line is received as ordered
- **Priority:** P1
- **Level:** integration
- **Given** a draft GRN against the reference PO built with `grntest.GRN` at each line's pending quantity (what step 1 prefills) **Then** the notes include "All lines received as ordered." **When** line 2 is cleared, or line 1 is changed to 1,100.000, or 20.000 is rejected on line 1 **Then** the note is absent.
- **Go test:** `TestGRN_ReceivedAsOrderedNote`
- **Sources:** research Gap 4, S39 (Oracle blind receiving); GRN spec §4, §11; CD-272
- **Built in:** G5 (notes service; shown on the check page in S3)

### GRN-045 · Challan or invoice: the kind is required at submit for goods
- **Priority:** P1
- **Level:** integration
- **Given** a goods GRN with `challan_no` `SST/2627/0457` and no kind **When** it is submitted **Then** "Choose Delivery challan or Tax invoice." on `challan_kind`; the draft save isn't refused. **Given** a goods GRN with both number and kind blank **Then** both errors, "Challan or invoice no. is required for goods." and "Choose Delivery challan or Tax invoice.", each on its field. With `Tax invoice` chosen it is approved. A service-only GRN with no number and no kind is approved. `challan_kind` accepts only `Delivery challan` and `Tax invoice`.
- **Go test:** `TestGRN_ChallanKind`
- **Sources:** research Gap 5, S66 (CGST Rules 55(1), 55A, 138A(1)(a)); GRN spec §3.1, §4; CD-272
- **Built in:** G2

### GRN-046 · A tax invoice at the gate prefills Bill details, and nothing is stored until saved
- **Priority:** P1
- **Level:** integration
- **Given** an approved GRN with `challan_kind` `Tax invoice`, `challan_no` `SST/2627/0457`, `challan_date` 23-10-2026 and no bill details **When** the bill panel is read **Then** it offers invoice no. `SST/2627/0457` and date 23-10-2026 with "Copied from the tax invoice entered at the gate. Check and save.", at version 0, and no `grn_bill` row exists. **When** saved **Then** the row exists. A GRN with kind `Delivery challan` offers nothing.
- **Go test:** `TestGRNBill_CopyFromInvoice`
- **Sources:** research Gap 5; GRN spec §3.3; CD-272
- **Built in:** G7

### GRN-047 · Vendor says the rest won't come posts a PO comment and lists the PO in To close; cancel removes it; the PO's state never changes
- **Priority:** P1
- **Level:** integration
- **Given** a GRN accepting 600.000 of line 1 with `rest_not_coming` ticked **When** it is approved **Then** the PO has one comment by `suresh` with purchase's text (`Vendor says the rest won't come. Close this order if nothing more is due.`, which names no GRN), it is listed in `priya`'s To close queue, and it stays approved and `part_received`. **When** the GRN is cancelled **Then** the PO leaves To close (the comment stays). **When** `priya` closes a listed PO **Then** it leaves To close. **When** a ticked GRN is cancelled and amended, and the amendment (which keeps the tick) is approved **Then** the PO is back in To close through the new GRN's row.
- **Go test:** `TestGRN_RestNotComing`
- **Sources:** research Gap 8, S43 (Odoo `test_purchase_order.py:877` `test_log_activity_in_po_with_receipt_without_backorders`); GRN spec §3.1, §5; CD-272
- **Built in:** G10 (the queue is MOD-PO P14's, the row P19's)

### GRN-048 · The two e-way bill hints show only on their conditions
- **Priority:** P1
- **Level:** integration
- **Given** a PO from `LOCAL` (unregistered) **When** a GRN accepts ₹50,000.01 of goods **Then** the notes (shown on the check and document pages in S3) include "Unregistered vendor: the e-way bill may be ours to generate. Check with your CA."; at exactly ₹50,000.00, or from `SHREE`, they don't. **Given** a GRN with every line fully rejected and e-way bill no. `331000123456` **Then** "Reject the e-way bill on the portal within 72 hours if the goods went back."; without the e-way no., or with any line accepted, it doesn't show. Neither hint refuses anything.
- **Go test:** `TestGRN_EWayHints`
- **Sources:** research Gap 13, S68, S69 (CGST Rule 138(1)(iii), 138(11)-(12)); GRN spec §7; CD-272
- **Built in:** G5 (notes service; pages in S3)

### GRN-049 · Request → PO → approval → partial GRN → short close, through the screens
- **Priority:** P1
- **Level:** e2e
- **Given** the browser at 360 px **When** `suresh` requests 10 Nos of BOLT-M12, `priya` creates the PO from it, `ravi` approves it and it is marked as sent, `suresh` receives 6 on a GRN, and `priya` closes the PO with a reason **Then** after each step the services read: PO `part_received` with 4 pending after the GRN; after the close pending 0, a new GRN against it refused with DOC-017's text, and the request showing 6 ordered. Every step has a short deadline.
- **Go test:** `TestE2E_RequestToShortClose`
- **Sources:** research (expense) §4 X1, CD-271 (ERPNext `test_purchase_order.py:95-122` chain); PO-033, PO-055, GRN-011
- **Built in:** S6

### GRN-050 · GRN cancel restores pending, through the screens
- **Priority:** P1
- **Level:** e2e
- **Given** X1's PO with the 6-unit GRN approved, before the close **When** `ravi` cancels the GRN with a reason in the browser **Then** the PO's pending reads 10 and `receipt_status` `not_received`, read through the services after the browser step.
- **Go test:** `TestE2E_GRNCancelRestores`
- **Sources:** research (expense) §4 X2, CD-271 (ERPNext `test_purchase_order.py:118-122`); GRN-018
- **Built in:** S6

