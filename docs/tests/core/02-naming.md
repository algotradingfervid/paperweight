# Naming series (NAM)

## Scope
NAM owns document numbers: series templates per DocType, format tokens, financial-year reset, padding, character and length rules, the counter table, when and how a number is assigned, amendment suffixes, the admin screen for templates and counters, and the GST document-number validator that later invoice and challan series reuse.
It relies on DOC for the state machine, write transactions, `amended_from` links and the audit trail (F5, F6); on NUM for the financial-year function and business dates (F7); and on AUTH for the Administrator-only check to change series and counters (by role; it is not one of the 11 DocType rights).
Masters (`code`) are not named by NAM; they belong to MST. Line rows keep integer IDs (F3).

## Decisions
| ID | Question | ERPNext/Frappe | Odoo | Decision | Why |
|---|---|---|---|---|---|
| D-NAM-01 | When is the number assigned? | On first save (insert) via `set_new_name`; drafts carry a real number (`frappe/frappe/model/document.py:1034`, `frappe/frappe/model/naming.py:144`). Tryton assigns at quote (`tryton/modules/purchase/purchase.py:970`). | Purchase order: on create (`odoo/addons/purchase/models/purchase_order.py:461`). Invoices/journal entries: on post only (`odoo/addons/account/models/account_move.py:1000`). | **When the document first becomes `approved`**, including draft → approved when no approval rule applies. Drafts, pending and rejected documents have no number (shown as "Draft 42"). | Drafts and rejected documents may be deleted (F5); if they never hold a number, deletes can never make gaps, and no counter-revert logic is needed. An unapproved PO shouldn't go to a vendor anyway. Confirmed by spec R1 (NAM wins over DOC and APR). A withdrawn document (`pending_approval → draft`, spec R2) also has no number. |
| D-NAM-02 | Are gaps allowed? | Gaps are tolerated; deleting the last-numbered doc decrements the counter (`frappe/frappe/model/naming.py:455`, `frappe/frappe/model/delete_doc.py:265`). | `standard` allows gaps, `no_gap` doesn't (`odoo/odoo/addons/base/models/ir_sequence.py:131`); account entries can only be deleted if last in the chain (`odoo/addons/account/models/account_move.py:4189`). | **Gapless.** The counter is incremented in the same SQLite write transaction as the approval; a failed approval rolls the counter back. Nothing holding a number is ever deleted. | GST Rule 46(b) asks for consecutive serial numbers on invoices; it costs nothing to apply the same rule to PO/GRN/Expense, and auditors like it. |
| D-NAM-03 | Concurrency | On SQLite, one atomic `INSERT … ON CONFLICT DO UPDATE SET current = current + 1 RETURNING current` (`frappe/frappe/model/naming.py:429`); `SELECT … FOR UPDATE` on other DBs. | `no_gap`: row lock `FOR UPDATE NOWAIT`, second writer fails (`ir_sequence.py:57`, test `:96`); account: unique-index lock and retry (`sequence_mixin.py:357`). | Frappe's single UPSERT…RETURNING inside a `BEGIN IMMEDIATE` transaction; `busy_timeout` makes the second writer wait, not fail. A unique index on `(doctype, number)` is the safety net. | SQLite allows one writer, so there's no race once the increment and the state change share a transaction. |
| D-NAM-04 | Financial-year reset and date basis | `{FY}`/`TFY` use the posting/transaction date (`erpnext/erpnext/accounts/utils.py:1636`); `YY`/`MM` use the current clock unless a global setting is on (`utils.py:1657`). The counter is keyed by the rendered prefix, so a new FY prefix starts at 1 (`naming.py:90`). | Uses the document's date (`purchase_order.py:463`); date ranges per year (`ir_sequence.py:260`); staggered year renders as `24-25` (`account_move.py:4398`). | Counter key = (DocType, rendered prefix). **Every** date token is taken from the document's business date, never from the clock. `{FY}` renders `25-26`. | Back-dated documents land in the correct FY, and approving at 00:30 IST on 1 April doesn't move a 31 March document into the new year. |
| D-NAM-05 | Template syntax and tokens | Dot syntax `PO-.YYYY.-.#####` or `format:{MM}-{##}`; tokens YY, YYYY, MM, DD, JJJ, WW, timestamp, field values, FY, TFY, ABBR (`naming.py:370`, `erpnext/erpnext/hooks.py:493`). | `%(year)s`, `%(y)s`, `%(month)s`, `%(doy)s`, `%(woy)s`, hours… in prefix/suffix (`ir_sequence.py:234`). | Brace syntax: `PO/{FY}/{#####}`. Tokens: `{FY}`, `{YYYY}`, `{YY}`, `{MM}` and exactly one number part `{###}`–`{######}`, which must come last. | It covers what Indian SMEs actually use. Field, day, week and time tokens make numbers long or unstable. Having the number part last keeps the amendment suffix unambiguous. |
| D-NAM-06 | Allowed characters and maximum length | Frappe allows letters, digits, `- / . # { }` and space (`naming.py:24`); rejects `<>` (`naming.py:543`). India Compliance: at most 16 characters, first character alphanumeric, then only `A-Za-z0-9-/` (`india-compliance/india_compliance/gst_india/constants/__init__.py:1476`); throws for Sales Invoice (`gst_india/utils/__init__.py:1216`). | l10n_in warns over 16 characters or other special characters (`odoo/addons/l10n_in/models/account_invoice.py:191`); uses 4 digits for staggered years "to avoid too long sequences (see Indian GST rule 46(b))" (`account_move.py:4420`). | Every series follows the GST rule: letters, digits, `-` and `/` only; starts with a letter or digit; **a rendered base number of 14 characters or fewer**, so an amendment suffix `-1`…`-9` still fits in 16. Checked when the template is saved. | One rule for all series means the later Sales Invoice and Delivery Challan series (and e-way bills) need no special case. `PO/26-27/00001` is exactly 14. |
| D-NAM-07 | Padding and overflow | The number of `#` sets zero padding; past the padding the number simply grows (`naming.py:436`). | `padding` field; grows likewise (`ir_sequence.py:146`, `:258`). | 3 to 6 digits. When a prefix runs out (e.g. 999 with `{###}`), approval fails with a clear error. It doesn't silently grow. | Growing could break the 16-character guarantee and the sort order. |
| D-NAM-08 | Amendment naming | Default "Amend Counter": `X` → `X-1` → `X-2` (not `X-1-1`), found by parsing the last `-` part; per-DocType option "Default Naming" gives a fresh number instead (`naming.py:578`, `document_naming_settings.py:205`, test `test_document_naming_settings.py:155`). | No amend concept; a cancelled entry keeps its name and is reset to draft. | Always the amend counter: `root number + "-" + amendment_no`. `amendment_no` and the root are **stored** columns, never parsed from the string. At most 9 amendments. At most one amendment per cancelled document. | Parsing on `-` breaks for prefixes containing hyphens (`PO-26-27-00007`). A cap of 9 keeps it within 16 characters. |
| D-NAM-09 | Cancelled documents | Keep their number (`frappe/frappe/tests/test_naming.py:311`). | Cancel keeps the name (`account_move.py:1004`). | Keep the number; it is never reused; cancelling doesn't touch the counter. | Fixed by F5; keeps the audit trail. |
| D-NAM-10 | Renaming / manual numbers | Rename exists for `allow_rename` DocTypes; `prompt` naming lets users type a name (`naming.py:607`). | Journal entry name can be edited in draft when `document_sequence_editable` is on. | **Never.** Numbers are system-assigned and immutable. A client-supplied number is rejected. | A number is a legal and audit reference; editing it invites duplicates. |
| D-NAM-11 | Manually setting the counter | System Manager sets any value, with a Version log (`document_naming_settings.py:222`). | `number_next` can be written to any value, even lower (`test_ir_sequence.py:262`). | Admin may set "last number used" per prefix, **never below the highest number already issued**. Audited. | Needed at go-live to continue numbering from the old system; lowering it would create duplicates. |
| D-NAM-12 | How many series per DocType | Several options per DocType, user picks; optional "user must always select" (`document_naming_settings.py:124`). | Any number of `ir.sequence` records by code. | One series template per DocType in v1, seeded from the Go definition and editable by Admin. | PO, GRN and Expense need one each; branch or return series come later. |
| D-NAM-13 | Same prefix on two DocTypes | Rejected: "Series {0} already used in {1}" (`document_naming_settings.py:162`). | Not checked. | Rejected. | Two DocTypes printing `PO/26-27/00001` would confuse people. |
| D-NAM-14 | Changing a template mid-year | A new prefix starts a new counter; the same prefix continues (`naming.py:128`). | Editing prefix/padding applies to the next number. | Allowed; it applies from the next approval; existing numbers never change. If the rendered prefix is unchanged, the counter continues. | Same as Frappe, simple and predictable. |
| D-NAM-15 | Where the FY comes from | Fiscal Year master; error "{0} {1} is not in any active Fiscal Year" if missing (`erpnext/erpnext/accounts/utils.py:134`). | Company fiscal year end setting, staggered years supported (`account_move.py:4441`). | Calculated from the date (1 April – 31 March, F7). No FY master, so no "missing FY" error. | F7 fixes the FY; a master would only be a way to get it wrong. |

## Test cases

Default templates used below: Purchase Order `PO/{FY}/{#####}`, GRN `GRN/{FY}/{#####}`, Expense `EXP/{FY}/{#####}` (see `00-fixtures.md`). "Approve" means the DOC transition into `approved` (from `pending_approval`, or straight from `draft` when no approval rule applies). Users, approval rules and the clock (2026-10-24 10:30 IST, FY 2026-27) are the fixture ones unless a case says otherwise. With the seed rules, a PO of ₹50,000.00 or more (such as the reference PO, ₹94,164.00) needs Purchase Manager approval, an Expense Claim always needs approval, and a GRN has no rule, so submitting a GRN approves it.

### Template validation and rendering

### NAM-001 · Default templates for v1 DocTypes are valid and render the first number
- **Priority:** P1
- **Level:** unit
- **Given** the seeded templates for Purchase Order, GRN and Expense **When** each is rendered for document date 2026-04-01 with counter 1 **Then** results are `PO/26-27/00001`, `GRN/26-27/00001`, `EXP/26-27/00001`; each is 14 or fewer characters.
- **Go test:** `TestTemplate_DefaultsRender`
- **Sources:** `erpnext/erpnext/buying/doctype/purchase_order/purchase_order.json:191` (`PUR-ORD-.YYYY.-`), `erpnext/erpnext/stock/doctype/purchase_receipt/purchase_receipt.json:192`, `hrms/hrms/hr/doctype/expense_claim/expense_claim.json:77`, `odoo/addons/purchase/data/purchase_data.xml:22`

### NAM-002 · `{FY}` renders the Indian financial year from the document date
- **Priority:** P1
- **Level:** unit
- **Given** template `PO/{FY}/{#####}` **When** rendered for these dates **Then** the FY part is: 2025-04-01 → `25-26`; 2026-03-31 → `25-26`; 2026-04-01 → `26-27`; 2028-02-29 → `27-28`; 2000-01-15 → `99-00`; 2099-04-01 → `99-00`.
- **Go test:** `TestRender_FYToken`
- **Sources:** `erpnext/erpnext/accounts/utils.py:92` (TFY truncation `25-26`), `erpnext/erpnext/accounts/utils.py:1633`, `erpnext/erpnext/accounts/test/test_utils.py:187` (`test_naming_series_variable_parsing`), `odoo/addons/account/models/account_move.py:4398`, `odoo/addons/account/tests/test_sequence_mixin.py:604` (`test_sequence_staggered_year`)

### NAM-003 · `{YYYY}`, `{YY}` and `{MM}` render from the document date
- **Priority:** P1
- **Level:** unit
- **Given** templates `PO/{YYYY}/{####}`, `P{YY}{MM}/{###}` **When** rendered for date 2026-04-05, counter 1 **Then** `PO/2026/0001` and `P2604/001`.
- **Go test:** `TestRender_CalendarTokens`
- **Sources:** `frappe/frappe/model/naming.py:382` (YY/MM/YYYY), `odoo/odoo/addons/base/tests/test_ir_sequence.py:202` (`test_ir_sequence_interpolation_dict`)

### NAM-004 · Date tokens use the document's business date, never the clock or UTC
- **Priority:** P1
- **Level:** integration
- **Given** a PO dated 2026-03-31 pending approval and the clock at 2026-03-31T19:00:00Z (01-04-2026 00:30 IST) **When** it is approved **Then** its number is `PO/25-26/00001`, not `PO/26-27/…`. **And** a PO dated 2026-04-02 approved at the fixture clock (2026-10-24 10:30 IST) gets `PO/26-27/…`.
- **Go test:** `TestAssign_UsesDocumentDateNotClock`
- **Sources:** `erpnext/erpnext/accounts/utils.py:1657` (YY uses now() unless setting), `erpnext/erpnext/setup/doctype/global_defaults/global_defaults.json:93` (`use_posting_datetime_for_naming_documents`), `odoo/addons/purchase/models/purchase_order.py:463` (sequence_date = date_order)

### NAM-005 · Counter resets when a new financial year starts
- **Priority:** P1
- **Level:** integration
- **Given** the last PO issued is `PO/25-26/00143` **When** a PO dated 2026-04-01 is approved **Then** it gets `PO/26-27/00001`; the `PO/25-26/` counter stays at 143.
- **Go test:** `TestAssign_FYReset`
- **Sources:** `frappe/frappe/model/naming.py:90` (`get_prefix`: counter keyed by rendered prefix), `frappe/frappe/tests/test_naming.py:403` (`test_naming_series_prefix`), `odoo/odoo/addons/base/models/ir_sequence.py:260` (`_create_date_range_seq`), `odoo/odoo/addons/base/tests/test_ir_sequence.py:292` (`test_ir_sequence_date_range_1_create`)

### NAM-006 · A back-dated document continues the old year's counter
- **Priority:** P1
- **Level:** integration
- **Given** `PO/25-26/` is at 143 and `PO/26-27/` is at 4 **When** a PO dated 2026-03-28 is approved on 2026-04-12 **Then** it gets `PO/25-26/00144`; the next PO dated 2026-04-12 gets `PO/26-27/00005`.
- **Go test:** `TestAssign_BackdatedUsesOwnFY`
- **Sources:** `odoo/addons/account/tests/test_sequence_mixin.py:604` (`test_sequence_staggered_year`: dates in the earlier year continue `23-24`), `erpnext/erpnext/accounts/utils.py:1636`

### NAM-007 · A template with no date token never resets
- **Priority:** P2
- **Level:** integration
- **Given** GRN template `GRN{######}` and last issued `GRN000143` dated 2026-03-31 **When** a GRN dated 2026-04-01 is approved **Then** it gets `GRN000144`.
- **Go test:** `TestAssign_NoDateTokenNeverResets`
- **Sources:** `frappe/frappe/tests/test_naming.py:403` (`"SINV": "SINV"` prefix), `odoo/addons/account/models/sequence_mixin.py:194` (`never` reset)

### NAM-008 · A monthly prefix resets every month
- **Priority:** P2
- **Level:** integration
- **Given** Expense template `EXP/{YY}{MM}/{###}` **When** expenses dated 2026-04-30, 2026-04-30 and 2026-05-01 are approved in that order **Then** numbers are `EXP/2604/001`, `EXP/2604/002`, `EXP/2605/001`.
- **Go test:** `TestAssign_MonthlyPrefixResets`
- **Sources:** `frappe/frappe/tests/test_naming.py:410` (`SINV-.YY.-.MM.-.####` prefix), `odoo/addons/account/models/sequence_mixin.py:120` (`month` reset)

### NAM-009 · Template without a number part is rejected
- **Priority:** P1
- **Level:** unit
- **Given** template `PO/{FY}/` **When** validated **Then** error "Series must have a number part, for example {#####}."
- **Go test:** `TestTemplate_Validate/missing_number`
- **Sources:** `frappe/frappe/model/naming.py:55` (Frappe silently appends `.#####`), `frappe/frappe/model/naming.py:75`

### NAM-010 · Template with two number parts is rejected
- **Priority:** P1
- **Level:** unit
- **Given** template `PO/{###}/{#####}` **When** validated **Then** error "Series can have only one number part."
- **Go test:** `TestTemplate_Validate/two_numbers`
- **Sources:** `frappe/frappe/model/naming.py:375` (Frappe uses only the first `#` group, ignores the rest)

### NAM-011 · The number part must come last
- **Priority:** P1
- **Level:** unit
- **Given** templates `PO/{#####}/{FY}` and `TEST1-{#####}-2021-22` **When** validated **Then** error "The number part must be at the end of the series."
- **Go test:** `TestTemplate_Validate/number_not_last`
- **Sources:** `frappe/frappe/tests/test_naming.py:269` (`TEST1-.#####.-2021-22` allowed in Frappe), `odoo/odoo/addons/base/models/ir_sequence.py:138` (suffix)

### NAM-012 · Padding must be 3 to 6 digits
- **Priority:** P1
- **Level:** unit
- **Given** templates `PO/{FY}/{##}` and `PO/{#######}` **When** validated **Then** error "The number part must have 3 to 6 digits." **And** `PO/{FY}/{###}` and `PO{######}` are accepted.
- **Go test:** `TestTemplate_Validate/padding_bounds`
- **Sources:** `odoo/odoo/addons/base/models/ir_sequence.py:146` (`padding`), `frappe/frappe/model/naming.py:377` (`digits = len(e)`)

### NAM-013 · Unknown tokens are rejected
- **Priority:** P1
- **Level:** unit
- **Given** templates `PO/{XYZ}/{###}`, `PO/{DD}/{###}`, `PO/{WW}/{###}`, `PO/{supplier}/{###}` **When** validated **Then** error "Unknown part {XYZ} in series. Use {FY}, {YYYY}, {YY}, {MM} or a number part like {#####}." (with the actual token named).
- **Go test:** `TestTemplate_Validate/unknown_token`
- **Sources:** `odoo/odoo/addons/base/tests/test_ir_sequence.py:234` (`test_ir_sequence_suffix`, `%(invalid)s` → "Invalid prefix or suffix"), `odoo/odoo/addons/base/tests/test_ir_sequence.py:186` (`test_ir_sequence_prefix`), `odoo/odoo/addons/base/models/ir_sequence.py:252`

### NAM-014 · Only letters, digits, `-` and `/` are allowed
- **Priority:** P1
- **Level:** unit
- **Given** templates `$INV/{#####}`, `WINDOWS\NAMING{###}`, `PO {FY}/{###}`, `PO.{FY}.{###}`, `PO_{###}`, `PO<>{###}`, `ПО/{###}` **When** validated **Then** each fails with "Series can use only letters, digits, '-' and '/'."
- **Go test:** `TestTemplate_Validate/bad_characters`
- **Sources:** `frappe/frappe/tests/test_naming.py:418` (`test_naming_series_validation`: `$INV-`, `WINDOWS\NAMING` invalid), `frappe/frappe/model/naming.py:543` (`<>`), `india-compliance/india_compliance/gst_india/constants/__init__.py:1478`, `odoo/odoo/addons/base/models/ir_sequence.py:155` (non-ASCII warning)

### NAM-015 · A series must start with a letter or digit
- **Priority:** P1
- **Level:** unit
- **Given** templates `/PO/{###}` and `-PO{###}` **When** validated **Then** error "Series must start with a letter or digit." **And** `{FY}/PO/{###}` is accepted (renders `26-27/PO/001`).
- **Go test:** `TestTemplate_Validate/first_char`
- **Sources:** `india-compliance/india_compliance/gst_india/constants/__init__.py:1476` ("First character must be alphanumeric")

### NAM-016 · Unclosed or stray braces are rejected
- **Priority:** P2
- **Level:** unit
- **Given** templates `PO/{FY/{###}` and `PO/FY}/{###}` **When** validated **Then** error "Series has an unmatched '{' or '}'."
- **Go test:** `TestTemplate_Validate/braces`
- **Sources:** `frappe/frappe/model/naming.py:25` (`BRACED_PARAMS_PATTERN`)

### NAM-017 · Rendered numbers are capped at 14 characters, leaving room for amendments within GST's 16
- **Priority:** P1
- **Level:** unit
- **Given** template `PO/{FY}/{#####}` (renders 14) **When** validated **Then** it is accepted. **Given** `POR/{FY}/{#####}` (15) or `SCPL/PO/{FY}/{####}` (18) **Then** error "Numbers from this series would be 18 characters. Keep them to 14 or fewer (amendments add 2, and GST allows 16). Shorten the prefix or use fewer digits." (with the actual length).
- **Go test:** `TestTemplate_Validate/max_length`
- **Sources:** `india-compliance/india_compliance/gst_india/utils/__init__.py:1219` (`len(doc.name) <= 16`), `india-compliance/india_compliance/gst_india/overrides/test_purchase_invoice.py:235`, `odoo/addons/l10n_in/models/account_invoice.py:191`, `odoo/addons/account/models/account_move.py:4420`

### NAM-018 · Valid templates are accepted
- **Priority:** P1
- **Level:** unit
- **Given** templates `PO/{FY}/{#####}`, `PO-{FY}-{#####}`, `{FY}/PO/{###}`, `P{YY}{MM}{####}`, `GRN{######}`, `EXP/{YYYY}/{####}` **When** validated **Then** no error.
- **Go test:** `TestTemplate_Validate/valid`
- **Sources:** `frappe/frappe/tests/test_naming.py:418` (`test_naming_series_validation` valid list), `frappe/frappe/core/doctype/document_naming_settings/test_document_naming_settings.py:48`

### NAM-019 · Prefix (counter key) is derived correctly
- **Priority:** P1
- **Level:** unit
- **Given** date 2026-04-05 **When** the prefix is computed **Then** `PO/{FY}/{#####}` → `PO/26-27/`; `P{YY}{MM}{####}` → `P2604`; `GRN{######}` → `GRN`.
- **Go test:** `TestTemplate_Prefix`
- **Sources:** `frappe/frappe/tests/test_naming.py:403` (`test_naming_series_prefix`), `frappe/frappe/model/naming.py:90`

### NAM-020 · Every number from a valid template passes the GST format (fuzz)
- **Priority:** P1
- **Level:** unit
- **Given** random valid templates, dates between 2000-01-01 and 2099-12-31, counters from 1 to the padding maximum and amendment numbers 0–9 **When** rendered **Then** every result is 16 characters or fewer and matches `^[A-Za-z0-9][A-Za-z0-9/-]{0,15}$`; without an amendment, 14 or fewer.
- **Go test:** `FuzzRender_GSTSafe`
- **Sources:** `india-compliance/india_compliance/gst_india/constants/__init__.py:1478`

### NAM-021 · GST document-number validator (for later invoice and challan series)
- **Priority:** P1
- **Level:** unit
- **Given** `ValidateGSTDocNumber` **When** called with `SI$1231`, `012345678901234567`, `SI 2020 05`, `SI.2020.0001`, `PI2021 - 001` **Then** each fails: over 16 → "Number must be 16 characters or fewer for GST."; otherwise → "Number must start with a letter or digit and use only letters, digits, '-' and '/' for GST." **When** called with `012345678901236`, `SI/2020/0001`, `SI/2020-0001`, `2020-PI-0001`, `PI2020-0001` **Then** no error.
- **Go test:** `TestValidateGSTDocNumber`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/test_sales_invoice.py:8` (`test_validate_invoice_number`), `india-compliance/india_compliance/gst_india/utils/__init__.py:1216`

### When the number is assigned

### NAM-022 · A new draft has no number
- **Priority:** P1
- **Level:** integration
- **Given** priya creates and saves the reference PO (id 42) **When** it is read back **Then** `number` is NULL, the counter table is unchanged, and the document's display label is "Draft 42".
- **Go test:** `TestAssign_DraftHasNoNumber`
- **Sources:** `odoo/addons/account/models/account_move.py:1013` (name set only when state ≠ draft), `tryton/modules/purchase/purchase.py:94` (`number` readonly, empty until quote), contrast `frappe/frappe/model/document.py:1034`

### NAM-023 · Submitting for approval does not assign a number; approval does
- **Priority:** P1
- **Level:** integration
- **Given** draft PO 42 created by priya: the reference PO for project PRJ-METRO, dated 2026-10-24, ₹94,164.00, so the Purchase Manager rule applies **When** priya submits it **Then** state is `pending_approval`, `number` is NULL, and it shows as "Draft 42" in lists, on the form and in ravi's "Waiting for my approval" list. **When** ravi approves it **Then** `number` = `PO/26-27/00001`.
- **Go test:** `TestAssign_OnApproval`
- **Sources:** `odoo/addons/account/models/account_move.py:1000` (`_compute_name` on post), `tryton/modules/purchase/purchase.py:970` (`quote` calls `set_number`)

### NAM-024 · Submitting with no approval rule assigns the number straight away
- **Priority:** P1
- **Level:** integration
- **Given** a draft GRN dated 2026-10-24 (GRN has no approval rule) and empty counters **When** suresh submits it **Then** state is `approved` and `number` = `GRN/26-27/00001` in the same transaction.
- **Go test:** `TestAssign_DirectApprove`
- **Sources:** `tryton/modules/purchase/purchase.py:805` (`set_number`)

### NAM-025 · A rejected or withdrawn document stays unnumbered and gets the next number when finally approved
- **Priority:** P1
- **Level:** integration
- **Given** PO 42 (the reference PO) pending approval, `PO/26-27/` at 5 **When** ravi rejects it with reason "Rate too high" **Then** state is `rejected`, `number` NULL, it shows as "Draft 42", and the counter is still 5. **When** priya revises it (back to `draft`, still no number), resubmits it, and ravi approves it after PO 43 was approved **Then** PO 43 = `PO/26-27/00006` and PO 42 = `PO/26-27/00007`. **And** PO 44 pending approval that priya withdraws is `draft` with `number` NULL and the counter unchanged at 7; when it is resubmitted and ravi approves it, it gets the next number, `PO/26-27/00008`.
- **Go test:** `TestAssign_RejectedThenApproved`
- **Sources:** `odoo/addons/account/tests/test_sequence_mixin.py:255` (`test_sequence_draft_first_of_period`)

### NAM-026 · Deleting a draft never affects the counter
- **Priority:** P1
- **Level:** integration
- **Given** PO A approved as `PO/26-27/00001`, PO B saved as draft **When** B is deleted and PO C is approved **Then** C = `PO/26-27/00002`; no number is missing.
- **Go test:** `TestAssign_DeleteDraftNoGap`
- **Sources:** contrast `frappe/frappe/model/delete_doc.py:265` (`update_naming_series` reverts only if last), `frappe/frappe/tests/test_naming.py:226` (`test_revert_series`), `odoo/addons/account/tests/test_sequence_mixin.py:1261` (`test_sequence_deletion_1`)

### NAM-027 · Numbers follow approval order, not creation order
- **Priority:** P1
- **Level:** integration
- **Given** drafts PO 1 (created 2026-10-24 09:00 IST) and PO 2 (created 09:05 IST) **When** PO 2 is approved before PO 1 **Then** PO 2 = `PO/26-27/00001`, PO 1 = `PO/26-27/00002`.
- **Go test:** `TestAssign_ApprovalOrder`
- **Sources:** `odoo/addons/account/tests/test_sequence_mixin.py:255` (`test_sequence_draft_first_of_period`)

### NAM-028 · Each DocType has its own counter
- **Priority:** P1
- **Level:** integration
- **Given** empty counters **When** a PO, then a GRN, then a PO are approved **Then** numbers are `PO/26-27/00001`, `GRN/26-27/00001`, `PO/26-27/00002`.
- **Go test:** `TestAssign_PerDocTypeCounters`
- **Sources:** `odoo/odoo/addons/base/tests/test_ir_sequence.py:154` (`TestIrSequenceGenerate`), `frappe/frappe/core/doctype/document_naming_settings/document_naming_settings.py:57`

### NAM-029 · The number is set once and survives later transitions
- **Priority:** P1
- **Level:** integration
- **Given** PO `PO/26-27/00007` approved **When** it is closed, or cancelled **Then** `number` is still `PO/26-27/00007` and the counter is unchanged.
- **Go test:** `TestAssign_NumberStableAcrossTransitions`
- **Sources:** `frappe/frappe/tests/test_naming.py:311` (`test_naming_for_cancelled_and_amended_doc`), `tryton/modules/purchase/purchase.py:815` (only purchases without a number get one)

### Counter, transactions and concurrency

### NAM-030 · Counter row is created on first use and increments atomically
- **Priority:** P1
- **Level:** unit
- **Given** no counter row for (`Purchase Order`, `PO/26-27/`) **When** `Next` is called twice in write transactions **Then** it returns 1 then 2 (rendered `00001`, `00002`) and the stored `last` is 2.
- **Go test:** `TestCounter_UpsertIncrement`
- **Sources:** `frappe/frappe/tests/test_naming.py:53` (`test_getseries_uses_an_atomic_sqlite_increment`), `frappe/frappe/model/naming.py:429`

### NAM-031 · A failed approval rolls the counter back
- **Priority:** P1
- **Level:** integration
- **Given** `GRN/26-27/` at 0 and a draft GRN whose approval effect fails (received qty 120 against PO pending qty 100) **When** suresh submits it (GRN has no approval rule, so submit is its approval) **Then** the submit fails with DOC's error, state stays `draft`, `number` NULL, counter still 0. **When** a valid GRN is then submitted **Then** it is approved as `GRN/26-27/00001`.
- **Go test:** `TestAssign_RollbackNoGap`
- **Sources:** `odoo/odoo/addons/base/models/ir_sequence.py:57` (`_update_nogap` in-transaction), `odoo/odoo/addons/base/models/ir_sequence.py:133` (no_gap help)

### NAM-032 · Numbering refuses to run outside a write transaction
- **Priority:** P2
- **Level:** unit
- **Given** no transaction (nil `tx`), or a read transaction from `db.Read` (`tx.Writable()` is false, CD-07) **When** the counter increment, `Assign` or `AssignAmendment` is called **Then** it returns `ErrNeedsTx` and no counter changes.
- **Go test:** `TestCounter_RequiresTx`
- **Sources:** `odoo/addons/account/models/sequence_mixin.py:357` (`_locked_increment` must run in the posting transaction)

### NAM-033 · Concurrent approvals get unique, consecutive numbers
- **Priority:** P1
- **Level:** integration
- **Given** 50 POs pending approval, SQLite in WAL mode with `busy_timeout` 5000 ms **When** 50 goroutines approve them at once **Then** all 50 succeed and the numbers are exactly `PO/26-27/00001` … `PO/26-27/00050`, with no duplicates or gaps.
- **Go test:** `TestAssign_ConcurrentApprovals`
- **Sources:** `odoo/addons/account/tests/test_sequence_mixin.py:1334` (`test_sequence_concurency`), `odoo/odoo/addons/base/tests/test_ir_sequence.py:96` (`test_ir_sequence_draw_twice_no_gap`), `odoo/odoo/addons/base/tests/test_ir_sequence.py:61` (`test_ir_sequence_draw_twice`)

### NAM-034 · Waiting past the busy timeout fails cleanly without using a number
- **Priority:** P2
- **Level:** integration
- **Given** another connection holds the write lock longer than `busy_timeout` (the test opens the database file from `testkit.DBPath(t)` with `db.WithBusyTimeout(200 * time.Millisecond)`, CD-08) **When** a PO is approved **Then** the error is `db.ErrBusy` ("The system is busy. Please try again in a moment."), state unchanged, counter unchanged.
- **Go test:** `TestAssign_BusyTimeout`
- **Sources:** `odoo/odoo/addons/base/tests/test_ir_sequence.py:96` (`LockNotAvailable`), `odoo/odoo/addons/base/models/ir_sequence.py:60` (`FOR UPDATE NOWAIT`)

### NAM-035 · Unique index stops duplicate numbers
- **Priority:** P2
- **Level:** integration
- **Given** a PO numbered `PO/26-27/00001` **When** a second PO row is written with the same number (counter forced back in the test) **Then** the write fails with "Number PO/26-27/00001 is already used. Please try again." and the transaction rolls back.
- **Go test:** `TestAssign_UniqueIndexBackstop`
- **Sources:** `odoo/addons/account/models/sequence_mixin.py:383` (unique constraint as lock), `frappe/frappe/tests/test_naming.py:475` (`test_hash_collision`)

### NAM-036 · Previewing the next number neither locks nor uses it
- **Priority:** P1
- **Level:** integration
- **Given** `PO/26-27/` at 2 **When** the next number is previewed twice (form header, settings page) **Then** both show `PO/26-27/00003`, the counter is still 2, and no write lock is taken. **When** a PO is then approved **Then** it gets `PO/26-27/00003`.
- **Go test:** `TestPreview_DoesNotConsume`
- **Sources:** `odoo/odoo/addons/base/models/ir_sequence.py:65` (`_predict_nextval`), `odoo/addons/account/tests/test_sequence_mixin.py:1361` (`test_sequence_concurency_no_useless_lock`), `frappe/frappe/model/naming.py:114` (`get_preview`)

### NAM-037 · Invariant: numbers per prefix are always 1..N with no gaps or duplicates (fuzz)
- **Priority:** P1
- **Level:** integration
- **Given** a random sequence of create, submit, withdraw, reject, revise, approve, failing approve, delete draft, cancel, amend and approve-amendment on POs with random dates across 2025-26 and 2026-27 **When** it finishes **Then** for each prefix the counter equals the count of non-amendment numbers, those numbers are exactly 1..N, every number is unique, and every amendment number is `root-k` with k = 1..m contiguous per root.
- **Go test:** `FuzzNaming_GaplessInvariant`
- **Sources:** `odoo/addons/account/models/sequence_mixin.py:489` (`_is_end_of_seq_chain`), `odoo/odoo/addons/base/models/ir_sequence.py:131`

### Cancel and amend

### NAM-038 · Cancelling keeps the number and doesn't touch the counter
- **Priority:** P1
- **Level:** integration
- **Given** `PO/26-27/00007` approved, counter 7 **When** it is cancelled **Then** its number is still `PO/26-27/00007`, the counter stays 7, and the next approved PO gets `PO/26-27/00008`.
- **Go test:** `TestCancel_KeepsNumber`
- **Sources:** `frappe/frappe/tests/test_naming.py:311` (`test_naming_for_cancelled_and_amended_doc`)

### NAM-039 · An amendment gets the root number with `-1` when approved, without using the counter
- **Priority:** P1
- **Level:** integration
- **Given** cancelled `PO/26-27/00007`, counter 8 **When** it is amended **Then** the new draft has `number` NULL, `amended_from` = the original, `amendment_no` = 1. **When** the amendment is approved **Then** its number is `PO/26-27/00007-1` and the counter is still 8.
- **Go test:** `TestAmend_FirstSuffix`
- **Sources:** `frappe/frappe/model/naming.py:578` (`_set_amended_name`), `frappe/frappe/tests/test_naming.py:336`, `frappe/frappe/core/doctype/document_naming_settings/test_document_naming_settings.py:155` (`test_amended_naming`)

### NAM-040 · Amending an amendment increments the suffix, it doesn't stack it
- **Priority:** P1
- **Level:** integration
- **Given** `PO/26-27/00007-1` approved then cancelled **When** it is amended and the amendment approved **Then** the number is `PO/26-27/00007-2` (not `PO/26-27/00007-1-1`), and `amended_from` points to `PO/26-27/00007-1`.
- **Go test:** `TestAmend_SecondSuffix`
- **Sources:** `frappe/frappe/model/naming.py:590` (`am_id = … + 1`), `frappe/frappe/tests/test_naming.py:340`

### NAM-041 · The amendment suffix comes from stored fields, not from splitting on hyphens
- **Priority:** P1
- **Level:** integration
- **Given** template `PO-{FY}-{#####}` and cancelled `PO-26-27-00007` **When** it is amended twice (each time approved then cancelled) **Then** numbers are `PO-26-27-00007-1` then `PO-26-27-00007-2`.
- **Go test:** `TestAmend_HyphenatedPrefix`
- **Sources:** `frappe/frappe/model/naming.py:591` (parses `split("-")[-1]`)

### NAM-042 · An amendment keeps the root's financial year
- **Priority:** P1
- **Level:** integration
- **Given** `PO/25-26/00143` dated 2026-03-20, cancelled on 2026-04-05 **When** it is amended, its date set to 2026-04-05, and approved **Then** its number is `PO/25-26/00143-1`; the `PO/26-27/` counter is unchanged.
- **Go test:** `TestAmend_KeepsRootFY`
- **Sources:** `frappe/frappe/model/naming.py:180` (amended name set before any series logic)

### NAM-043 · Only one amendment per cancelled document
- **Priority:** P1
- **Level:** integration
- **Given** cancelled `PO/26-27/00007` that already has an amendment (Draft 51, or approved `PO/26-27/00007-1`) **When** the user amends `PO/26-27/00007` again **Then** error "PO/26-27/00007 has already been amended as Draft 51. Open that document instead." while the amendment is unapproved, and "PO/26-27/00007 has already been amended as PO/26-27/00007-1. Open that document instead." once it is approved (DOC-065's wording, CD-27). DOC refuses the draft case; NAM's `AssignAmendment` refuses the approved case with the same text.
- **Go test:** `TestAmend_OnlyOnce`
- **Sources:** `frappe/frappe/model/naming.py:594` (a second amendment would collide on `X-1`)

### NAM-044 · A deleted draft amendment frees its suffix
- **Priority:** P2
- **Level:** integration
- **Given** cancelled `PO/26-27/00007` amended into Draft 51 **When** Draft 51 is deleted, the PO is amended again and that amendment approved **Then** its number is `PO/26-27/00007-1`.
- **Go test:** `TestAmend_DeletedDraftFreesSuffix`
- **Sources:** `frappe/frappe/core/doctype/document_naming_settings/test_document_naming_settings.py:170` (`amended_doc.delete()` then re-amend)

### NAM-045 · At most nine amendments
- **Priority:** P2
- **Level:** integration
- **Given** `PO/26-27/00007-9` cancelled **When** the user amends it **Then** error "PO/26-27/00007 has been amended 9 times, which is the limit. Create a new purchase order instead."
- **Go test:** `TestAmend_Limit`
- **Sources:** `india-compliance/india_compliance/gst_india/utils/__init__.py:1219` (16-character limit drives the cap)

### NAM-046 · An amendment number stays within 16 characters
- **Priority:** P1
- **Level:** unit
- **Given** the longest allowed base `PO/26-27/00007` (14) **When** rendered with amendment 9 **Then** `PO/26-27/00007-9` (16) passes `ValidateGSTDocNumber`.
- **Go test:** `TestRender_AmendmentLength`
- **Sources:** `india-compliance/india_compliance/gst_india/constants/__init__.py:1478`

### NAM-047 · Only a cancelled document can be amended
- **Priority:** P2
- **Level:** integration
- **Given** approved (not cancelled) `PO/26-27/00007` **When** the user amends it **Then** error "PO/26-27/00007 can't be amended because it isn't cancelled. Cancel it first." (DOC owns the rule; NAM checks that no suffix is computed.)
- **Go test:** `TestAmend_RequiresCancelled`
- **Sources:** `frappe/frappe/model/document.py:1173` (`validate_amended_from`)

### Immutability

### NAM-048 · A client can't supply a number
- **Priority:** P1
- **Level:** integration
- **Given** a create or save request for a PO carrying `number = "PO/26-27/09999"` **When** it is processed **Then** it is rejected with "Document numbers are set automatically and can't be entered." and nothing is saved.
- **Go test:** `TestNumber_ClientCannotSet`
- **Sources:** `tryton/modules/purchase/purchase.py:94` (`readonly=True`), contrast `frappe/frappe/model/naming.py:607` (`_prompt_autoname`)

### NAM-049 · A number can't be changed after it's assigned
- **Priority:** P1
- **Level:** integration
- **Given** approved `PO/26-27/00007` **When** any request (form, API, CSV import) tries to set its number to `PO/26-27/00008` **Then** error "Document numbers can't be changed." and the stored number is unchanged. There is no rename action.
- **Go test:** `TestNumber_Immutable`
- **Sources:** `frappe/frappe/tests/test_naming.py:61` (`test_field_autoname_name_sync`: name doesn't follow field edits)

### NAM-050 · Duplicating a document gives an unnumbered draft
- **Priority:** P1
- **Level:** integration
- **Given** approved `PO/26-27/00007` (or its amendment) **When** a user duplicates it **Then** the copy is a draft with `number` NULL, `amended_from` NULL and `amendment_no` 0.
- **Go test:** `TestCopy_ClearsNumber`
- **Sources:** `tryton/modules/purchase/purchase.py:778` (`default.setdefault('number', None)`), `frappe/frappe/model/document.py:4399` (copy clears `amended_from`)

### Admin settings

### NAM-051 · Admin sees each series with its next number
- **Priority:** P1
- **Level:** e2e
- **Given** admin (Anil Kapoor, Administrator), `PO/26-27/` at 7, fixture clock 2026-10-24 **When** Anil opens Settings → Numbering **Then** a row shows "Purchase Order · PO/{FY}/{#####} · Next: PO/26-27/00008", and similar rows for GRN and Expense. NAM owns this screen and builds it from UI components (CD-49); this browser test is owned and run by WP3 (CD-40).
- **Go test:** `TestE2E_NamingSettingsList`
- **Sources:** `frappe/frappe/core/doctype/document_naming_settings/document_naming_settings.py:43` (`get_transactions_and_prefixes`), `:198` (`get_current`)

### NAM-052 · Editing a template affects only future numbers; a new prefix starts at 1
- **Priority:** P1
- **Level:** integration
- **Given** `PO/26-27/00007` issued **When** admin changes the template to `PUR/{FY}/{#####}` and a PO is then approved **Then** it gets `PUR/26-27/00001`; `PO/26-27/00007` is unchanged.
- **Go test:** `TestSettings_TemplateChangeNewPrefix`
- **Sources:** `frappe/frappe/core/doctype/document_naming_settings/test_document_naming_settings.py:75` (`test_updates_naming_options`), `frappe/frappe/model/naming.py:90`

### NAM-053 · Same rendered prefix with new padding continues the counter
- **Priority:** P2
- **Level:** integration
- **Given** `PO/26-27/00143` issued **When** the template changes to `PO/{FY}/{####}` and a PO is approved **Then** it gets `PO/26-27/0144`.
- **Go test:** `TestSettings_PaddingChangeContinues`
- **Sources:** `frappe/frappe/model/naming.py:93` (counter keyed by prefix without digits)

### NAM-054 · Invalid template edits are rejected and nothing is saved
- **Priority:** P2
- **Level:** integration
- **Given** template `PO/{FY}/{#####}` **When** admin saves `PO {FY}/{##}` **Then** the first failing rule's error from NAM-009..NAM-017 is shown ("Series can use only letters, digits, '-' and '/'.") and the stored template is unchanged.
- **Go test:** `TestSettings_InvalidEditRejected`
- **Sources:** `frappe/frappe/core/doctype/document_naming_settings/document_naming_settings.py:185` (`validate_series_name`)

### NAM-055 · A prefix already used by another DocType is rejected
- **Priority:** P1
- **Level:** integration
- **Given** Purchase Order uses `PO/{FY}/{#####}` **When** admin sets GRN's template to `PO/{FY}/{####}` **Then** error "Prefix PO/{FY}/ is already used by Purchase Order. Use a different prefix."
- **Go test:** `TestSettings_DuplicatePrefix`
- **Sources:** `frappe/frappe/core/doctype/document_naming_settings/document_naming_settings.py:162` (`check_duplicate`)

### NAM-056 · Admin sets the last number used at go-live
- **Priority:** P1
- **Level:** integration
- **Given** the old system issued up to PO 143 for FY 26-27 and `PO/26-27/` has no counter row **When** admin sets "Last number used" for `PO/26-27/` to 143 **Then** the next approved PO is `PO/26-27/00144`.
- **Go test:** `TestSettings_SetCounter`
- **Sources:** `frappe/frappe/core/doctype/document_naming_settings/test_document_naming_settings.py:144` (`test_update_series_counter`), `frappe/frappe/model/naming.py:128` (`update_counter`), `odoo/odoo/addons/base/tests/test_ir_sequence.py:262` (`test_00_read_next_sequence`)

### NAM-057 · The counter can't go below a number already issued
- **Priority:** P1
- **Level:** integration
- **Given** `PO/26-27/00150` is the highest PO issued **When** admin sets the counter to 120 **Then** error "PO/26-27/ has already issued numbers up to 00150. Set 150 or more." and the counter is unchanged. Setting 150 or 200 succeeds.
- **Go test:** `TestSettings_CounterNotBelowIssued`
- **Sources:** contrast `odoo/odoo/addons/base/tests/test_ir_sequence.py:282` (reset to 1 allowed), `frappe/frappe/model/naming.py:129` ("Incorrectly updating series can result in unusable transactions")

### NAM-058 · Counter value must fit the padding
- **Priority:** P2
- **Level:** unit
- **Given** template `PO/{FY}/{#####}` **When** admin enters -1, 100000 or `12a` **Then** error "Last number used must be a whole number from 0 to 99999."
- **Go test:** `TestSettings_CounterRange`
- **Sources:** `frappe/frappe/model/naming.py:137` (`cint(new_count)`)

### NAM-059 · Admin can preset the counter for the coming financial year
- **Priority:** P2
- **Level:** integration
- **Given** the clock moved forward from the fixture clock to 2027-03-20 and no `PO/27-28/` row **When** admin picks FY 27-28 and sets 0 → 50 **Then** a counter row for `PO/27-28/` is created at 50, and the first PO dated 2027-04-01 gets `PO/27-28/00051`.
- **Go test:** `TestSettings_PresetNextFY`
- **Sources:** `frappe/frappe/model/naming.py:133` (initialise if not present)

### NAM-060 · Only Admin can change series or counters
- **Priority:** P1
- **Level:** integration
- **Given** ravi (Purchase Manager) and priya (Purchase User), neither an Administrator **When** either saves a template or a counter **Then** HTTP 403 with "You don't have permission to change document numbering. Ask an Administrator." and nothing changes (CD-66).
- **Go test:** `TestSettings_AdminOnly`
- **Sources:** `frappe/frappe/core/doctype/document_naming_settings/document_naming_settings.py:223` (`frappe.only_for("System Manager")`)

### NAM-061 · Template and counter changes are audited
- **Priority:** P1
- **Level:** integration
- **Given** admin (Anil Kapoor) at the fixture clock **When** Anil changes PO's template from `PO/{FY}/{#####}` to `PUR/{FY}/{#####}` and sets `PUR/26-27/` from 0 to 12 **Then** two append-only audit entries record user Anil Kapoor, UTC time `2026-10-24T05:00:00.000Z`, and before/after values (`template`: old → new; `last`: 0 → 12).
- **Go test:** `TestSettings_Audited`
- **Sources:** `frappe/frappe/core/doctype/document_naming_settings/document_naming_settings.py:240` (`create_version_log_for_change`)

### NAM-062 · Template preview shows the next three numbers without using them
- **Priority:** P2
- **Level:** integration
- **Given** `PO/26-27/` at 7, fixture clock 2026-10-24 **When** admin types `PO/{FY}/{#####}` in the preview box **Then** it shows `PO/26-27/00008`, `PO/26-27/00009`, `PO/26-27/00010` and the counter stays 7. For a new prefix it shows `…00001`–`…00003`. The preview comes from `naming.Admin.PreviewTemplate(ctx, tpl, date)`, which returns the three numbers as one string joined with ", " (CD-57).
- **Go test:** `TestSettings_Preview`
- **Sources:** `frappe/frappe/core/doctype/document_naming_settings/test_document_naming_settings.py:53` (`test_naming_preview`), `frappe/frappe/model/naming.py:114`

### Overflow and display

### NAM-063 · An exhausted series stops with a clear error and rolls back
- **Priority:** P2
- **Level:** integration
- **Given** template `GRN/{FY}/{###}` with `GRN/26-27/` at 999 **When** suresh submits a draft GRN (no approval rule, so submit is its approval) **Then** error "Series GRN/26-27/ has used all 999 numbers. Ask an Administrator to add a digit to the series." (CD-66); the GRN stays `draft` with no number and the counter stays 999.
- **Go test:** `TestAssign_PaddingExhausted`
- **Sources:** `frappe/frappe/model/naming.py:436` (Frappe grows past padding), `odoo/odoo/addons/base/models/ir_sequence.py:258`

### NAM-064 · Number assignment is recorded in the approval audit entry
- **Priority:** P2
- **Level:** integration
- **Given** PO 42 (the reference PO) pending approval and `PO/26-27/` at 0 **When** ravi approves it **Then** the transition audit entry (action `approve`, `pending_approval → approved`, by Ravi Kulkarni) includes field `number`: NULL → `PO/26-27/00001`.
- **Go test:** `TestAssign_AuditedWithTransition`
- **Sources:** `frappe/frappe/core/doctype/document_naming_settings/document_naming_settings.py:240`

### NAM-065 · Drafts print with a draft label instead of a number
- **Priority:** P2
- **Level:** integration
- **Given** draft PO 42 (the reference PO) and ravi, who has the `print` right on Purchase Order and can see the PO **When** ravi prints it **Then** the number field shows "Draft 42" and the page carries a "DRAFT" watermark; while it is `pending_approval` it still prints "Draft 42" with the watermark; after approval it prints `PO/26-27/00001` with no watermark.
- **Go test:** `TestPrint_DraftLabel`
- **Sources:** `odoo/addons/purchase/models/purchase_order.py:80` (`'New'` placeholder), `odoo/addons/account/models/account_move.py:1019` (`_compute_name_placeholder`)

## Deferred and excluded
| Scenario | Source | later/never | Reason |
|---|---|---|---|
| Several series per DocType with a user choice (e.g. normal vs return), "user must always select" | `frappe/frappe/core/doctype/document_naming_settings/document_naming_settings.py:131`, `erpnext/erpnext/stock/doctype/purchase_receipt/purchase_receipt.json:192` | later | One series each is enough for PO/GRN/Expense; add when returns arrive. |
| Branch/GSTIN-wise series (`{BRANCH}` token) | GST Rule 46(b) series per registration | later | Needed with Sales Invoice/Delivery Challan; the counter key (DocType, rendered prefix) already supports it. |
| Credit note / refund / self-billing prefixes | `odoo/addons/account/models/account_move.py:4411`, `odoo/addons/l10n_in/models/account_invoice.py:388` | later | Arrives with invoicing. |
| GST implications of amending an invoice into a later FY | `frappe/frappe/model/naming.py:180` | later | Decide with Sales Invoice; POs are fine keeping the root FY. |
| Importing historical documents with their old numbers | `frappe/frappe/model/naming.py:161` (`in_import`) | later | Go-live uses the counter setting (NAM-056); document import comes with CSV import work. |
| Field-value tokens (`{supplier}`, `{company}`, ABBR) | `frappe/frappe/tests/test_naming.py:434`, `erpnext/erpnext/accounts/utils.py:1643` | never | Single company (F1); field values make numbers unstable, long and full of disallowed characters. |
| Day, week, day-of-year, time, timestamp tokens | `frappe/frappe/model/naming.py:386`, `frappe/frappe/tests/test_naming.py:344`, `odoo/odoo/addons/base/tests/test_ir_sequence.py:218` | never | Not used for Indian SME numbering; break the 14-character budget. |
| Custom token parsers via hooks | `frappe/frappe/tests/test_naming.py:483` (`test_custom_parser`) | never | Tokens are fixed in Go. |
| Reverting the counter when the last document is deleted | `frappe/frappe/model/naming.py:455`, `frappe/frappe/tests/test_naming.py:226`, `:295` | never | Drafts never hold numbers; numbered documents are never deleted. |
| Deleting only the last of a chain | `odoo/addons/account/models/account_move.py:4189`, `odoo/addons/account/tests/test_sequence_mixin.py:1261` | never | Same reason. |
| Gap-allowed "standard" implementation, switching implementations | `odoo/odoo/addons/base/models/ir_sequence.py:185`, `odoo/odoo/addons/base/tests/test_ir_sequence.py:116` | never | Gapless always (D-NAM-02). |
| Step/increment other than 1 | `odoo/odoo/addons/base/models/ir_sequence.py:144` | never | No use case. |
| Editable per-sequence date ranges, non-April or staggered fiscal year end | `odoo/odoo/addons/base/tests/test_ir_sequence.py:292`, `odoo/addons/account/tests/test_sequence_mixin.py:604` | never | FY fixed at 1 Apr – 31 Mar (F7). |
| Fiscal Year master and "not in any active Fiscal Year" error | `erpnext/erpnext/accounts/utils.py:134` | never | FY is calculated (D-NAM-15). |
| Name/date consistency constraint and resequencing | `odoo/addons/account/models/sequence_mixin.py:158`, `odoo/addons/account/tests/test_sequence_mixin.py:186` | never | The date is locked once a number exists. |
| Suffix after the number part | `odoo/odoo/addons/base/models/ir_sequence.py:138`, `odoo/addons/account/tests/test_sequence_mixin.py:867` | never | Conflicts with the amendment suffix (D-NAM-05). |
| "Default Naming" for amendments (fresh number) | `frappe/frappe/model/naming.py:585` | never | One rule; the `-n` suffix shows the link on paper. |
| Rename, prompt/manual numbers, editable sequence numbers | `frappe/frappe/model/naming.py:607`, `odoo/addons/account/tests/test_sequence_mixin.py:147` | never | Numbers are immutable (D-NAM-10). A supplier's bill number is a separate field on later Purchase Invoice. |
| Hash, UUID, autoincrement, field-based names for documents | `frappe/frappe/tests/test_naming.py:392`, `:475`, `:514` | never | F3: integer IDs plus `number`. |
| Naming child rows | `frappe/frappe/tests/test_naming.py:86` | never | Lines use integer IDs. |
| Lowering a counter below issued numbers | `odoo/odoo/addons/base/tests/test_ir_sequence.py:282` | never | Creates duplicates (D-NAM-11). |
| Names equal to the DocType, "New …" names | `frappe/frappe/tests/test_naming.py:367` | never | Numbers are generated from validated templates, so these cannot occur. |

## Interfaces this area needs from others
- **NAM provides** `naming.Service.Assign(ctx, tx, doctype, docID, d types.Date) (string, error)` (spec §5), called by DOC's `doc.Engine.Do` **inside the write transaction that moves a document into `approved` for the first time**: on `approve` (from `pending_approval`), or on `submit` straight from `draft` when `approval.Service.Evaluate` finds no rule. Spec R1 confirms this (see D-NAM-01). DOC and APR must not expect a number on draft, pending, rejected or withdrawn documents; they display as "Draft {id}", and every message that names such a document uses "Draft 42", never a number. `withdraw` (`pending_approval → draft`, spec R2) and `revise` (`rejected → draft`) never call NAM.
- **NAM provides** `naming.Service.AssignAmendment(ctx, tx, doctype, docID, root string, n int) (string, error)` (spec §5), called by DOC when an amendment is approved; DOC stores `amended_from_id`, `amendment_root_id` and `amendment_no` (1..9) and enforces one live amendment per cancelled document.
- **NAM provides** `naming.Service.Preview(ctx, doctype, d types.Date) (string, error)` (read-only) and `naming.Service.ValidateTemplate(tpl string) error` (spec §5). Outside the service interface, the `naming` package exports (spec §5, CD-28) `ValidateGSTDocNumber(s string) error` (for later Sales Invoice / Delivery Challan), `Label(number string, id int64) string` (`"Draft {id}"` for unnumbered documents; implemented by WP0, NAM keeps its tests, CD-53), `GuardNumberField(stored, supplied *string) error` (DOC calls it on save and import) and `CheckAmendmentNo(root string, k int, docLabel string) error` (DOC calls it on amend).
- **NAM provides** `naming.Admin` (spec §5, CD-28): `List(ctx) ([]SeriesInfo, error)`, `SetTemplate(ctx, p, doctype, tpl)`, `SetCounter(ctx, p, doctype, prefix string, last int64)`, `EnsureSeries(ctx, doctype, label, defaultTpl)` and `PreviewTemplate(ctx, tpl string, d types.Date) (string, error)` (CD-57). `internal/app` calls `EnsureSeries` at startup for every registered DocType that has a `Series`. Every NAM error that reaches a handler implements `doctype.HTTPError` (CD-44).
- **NAM provides** the Settings → Numbering screen (NAM-051 list, NAM-062 preview), built from `internal/ui` components after the UI work package merges (CD-49). WP3 mounts it behind AUTH's Admin guard and owns `TestE2E_NamingSettingsList`.
- **DOC provides** `doc.Engine.Do` running each action in one `db.DB.Write` transaction (`BEGIN IMMEDIATE`, SQLite WAL, `busy_timeout` 5000 ms), full rollback on any failure in an approval, the series template on `doc.DocType.Series`, the business date via `Hooks.Date`, a `number TEXT NULL` column per document table with a unique index on it, copy/duplicate that clears `number` and amendment fields, rejection of client-supplied `number`, and the append-only audit trail (F6). NAM takes an `audit.Writer` in its constructor (CD-03; production passes `audit.Default`, tests use `audit/auditfake`) and writes to it for template and counter changes.
- **NUM provides** `types.FiscalYear(d types.Date) types.FY` (spec §5) returning e.g. Start 2026-04-01, End 2027-03-31, Short `26-27`, and business dates as plain `types.Date` values (F7).
- **AUTH provides** `auth.Service.HasRole(ctx, u, "Administrator")` for the numbering settings screen and counter edits (NAM's tests use WP0's `authfake` with `AddRole`, CD-56), and the Admin route guard the screen is mounted behind. Numbering settings are not one of the 11 DocType rights, so they are Administrator-only by role.
- **FILE provides** printing through `print.Register`: the print view shows `number`, or "Draft {id}" with a DRAFT watermark when the number is NULL. Printing needs the `print` right on the DocType plus visibility of the record (spec R3, checked with `auth.Service.Can(..., "print", rec)`).
- **MST (later)** provides a short branch code if `{BRANCH}` series are added for GSTIN-wise invoices.
