# Numbers, dates & lists (NUM)

## Scope
NUM owns the value types every other area builds on: money (paise), quantity, rate, percent and UoM conversion factor, all as fixed-point integers with one rounding rule. It also owns line and document totals, the GST split (CGST+SGST vs IGST), Indian number display and parsing, business dates and the financial year, IST display, list queries (filter, sort, search, pagination, count) and CSV import/export.
It relies on AUTH for the read, export and import rights (plus create on each imported row) and the record-level permission predicate applied to every list, count and export; on MST for branch state/GSTIN, party state/GSTIN, tax codes (GST rate, HSN/SAC), UoM whole-number flags and conversion factors, and master validation (each master's `csvio.RowFunc` creates and validates every imported row, CD-32); on DOC for the DocType field definitions in `internal/kernel/doctype` (which fields can be filtered, sorted, searched, imported; plural labels; master lookups).
NUM provides the financial-year function to NAM and the formatters to FILE.

## Decisions
| ID | Question | ERPNext/Frappe | Odoo | Decision | Why |
|---|---|---|---|---|---|
| D-NUM-01 | How are numbers represented? | Python `float` with `flt(value, precision)` rounding after each step (`frappe/frappe/utils/data.py:1135`); epsilon tricks to fight IEEE-754 (`:1294`). Tryton uses `Decimal` (`tryton/modules/purchase/purchase.py:1741`). | `float` with `float_round` and epsilon correction (`odoo/odoo/tools/float_utils.py:72`, `:121-131`). | **Fixed-point `int64` types everywhere: Go structs, SQLite `INTEGER` columns, strings in forms/JSON. No `float64` for any business value.** Intermediate products use 128-bit integer maths (`math/bits` or `math/big`). | Both float systems need epsilon hacks and still carry special cases (`2.675`, `1.005`). Integers make sums exact and tests deterministic. SQLite `INTEGER` `SUM` is exact. |
| D-NUM-02 | Money scale | Currency precision from the number format, so 2 for `#,##,###.##` (`frappe/frappe/model/meta.py:982-987`). | Currency rounding 0.01 for INR. | **`Money` = int64 paise (2 dp).** | INR only (F2); paisa is the smallest unit. |
| D-NUM-03 | Quantity scale | `float_precision` default 3, set by the setup wizard (`frappe/frappe/desk/page/setup_wizard/setup_wizard.py:372`; `frappe/frappe/model/meta.py:989`). Whole-number UoM check (`erpnext/erpnext/utilities/transaction_base.py:644-662`). | Per-UoM rounding (product module; exact default not verified in the sparse clone). | **`Qty` = int64 thousandths (3 dp).** Whole-number UoMs (MST flag) reject fractions. | Same as ERPNext default. Enough for kg/metre/litre to the gram/millimetre/millilitre. |
| D-NUM-04 | Rate (unit price) scale | `rate` is a Currency field, so 2 dp by default (`erpnext/erpnext/buying/doctype/purchase_order_item/purchase_order_item.json`, `rate` has no precision override). | Separate "Product Price" decimal precision (not in the sparse clone; couldn't verify default). Tryton uses `price_digits` from the product module (`tryton/modules/purchase/purchase.py:28`, `:1297`; module not in clone). | **`Rate` = int64 ten-thousandths of a rupee (4 dp).** Line amounts are still paise. | Bulk consumables (fasteners, cable per metre) are quoted below one paisa per unit, e.g. ₹0.1234. 4 dp covers this and still fits int64. |
| D-NUM-05 | Percent scale (GST rate, discount) | `Percent` fields use float precision (3). | Tax `amount` float. | **`Percent` = int64 thousandths of a percent ("millipercent"): 18% = 18000, 0.25% = 250, 0.125% = 125.** Range 0 to 100.000. | **Departs from the brief's basis-point suggestion:** half of the 0.25% GST rate is 0.125% for CGST/SGST, and basis points (12.5 bp) can't hold it. Millipercent can. |
| D-NUM-06 | UoM conversion factor scale | `conversion_factor` precision 9 (`erpnext/erpnext/buying/doctype/purchase_order_item/purchase_order_item.json:259`). | UoM `factor` float. | **`Factor` = int64 scaled by 10^9 (9 dp).** Stock qty = qty × factor, rounded half-up to 3 dp. | Matches ERPNext. 1/0.6-style factors need many digits to round-trip. |
| D-NUM-07 | Rounding rule | Options are Banker's (legacy), Banker's and Commercial (`frappe/frappe/core/doctype/system_settings/system_settings.json:555-558`). New sites get **Banker's** (`setup_wizard.py:373`; `frappe/frappe/tests/test_utils.py:2207` `test_default_rounding`). | `float_round` defaults to **HALF-UP** (`odoo/odoo/tools/float_utils.py:76`, `:135`). India cash rounding is HALF-UP to ₹1 (`odoo/addons/l10n_in/models/template_in.py:50-58`; `odoo/addons/account/models/account_cash_rounding.py:33-41`). | **Half-up (ties away from zero) for every rounding: money, tax, quantity, rate, rounded total.** | **Departs from ERPNext's default.** Half-up matches Excel `ROUND()`, hand calculation, Odoo India and the statutory "50 paise or more goes up" rule for GST (CGST Act s.170; not in the clones, couldn't verify locally). Users will check our numbers against Excel and Zoho. Banker's rounding would show 1-paisa "errors" on ties. |
| D-NUM-08 | Round tax per line or per document? | Default: unrounded line taxes are summed per tax row and rounded once (`erpnext/erpnext/controllers/taxes_and_totals.py:481-575`, `:769-778`). Optional "Round tax amount row-wise" setting, off by default (`erpnext/erpnext/accounts/doctype/accounts_settings/accounts_settings.json:453`; `taxes_and_totals.py:505-507`). India Compliance then needs a diffusion step so item-wise GST adds back to the row (`india-compliance/india_compliance/gst_india/overrides/test_transaction.py:1226` `test_rounding_gst_details_across_many_items`). | Company default `round_globally` (`odoo/addons/account/models/company.py:132-135`), **but the India chart sets `round_per_line`** (`odoo/addons/l10n_in/models/template_in.py:46`). Tryton rounds each line amount (`tryton/modules/purchase/purchase.py:1736-1743`). | **Round per line, per tax component. Document totals are exact sums of rounded line values.** | Follows Odoo India. With integers it gives "header = Σ lines" by construction, so no distribution step and no "Item-wise tax details do not match" class of bug. |
| D-NUM-09 | How are CGST and SGST computed? | India Compliance uses two tax rows (CGST, SGST), each at half the rate, each computed on the taxable value; intra-state must use both (`india-compliance/india_compliance/gst_india/overrides/transaction.py:418-447`). | Two equal percent taxes; the test requires their amounts to always match (`odoo/addons/account/tests/test_taxes_computation.py:545-566` `test_percent_taxes_for_l10n_in`). | **CGST = SGST = round_half_up(taxable × rate/2), computed once and used for both. IGST = round_half_up(taxable × rate). A rounded IGST is never halved.** | Halves are always equal and never fall on half a paisa. The cost is that an intra-state line can be 1 paisa away from the IGST figure (₹10.05 @ 18%: 0.90 + 0.90 vs 1.81). This is accepted and documented. |
| D-NUM-10 | Inter-state or intra-state? | Inter-state when the place of supply's state ≠ source state. For purchases the source is the supplier GSTIN's state (supplier address state if unregistered) and the place of supply is the company GSTIN's state. SEZ is always inter-state (`india-compliance/india_compliance/gst_india/overrides/transaction.py:609-660`; `india_compliance/gst_india/utils/__init__.py:720-731`). | Fiscal positions "Within <company state>" vs "Inter State", auto-applied by partner state (`odoo/addons/l10n_in/models/template_in.py:61-81`). | **For purchase-side documents: compare the supplier's state code (GSTIN digits 1-2, else the supplier address state) with the receiving branch's state code (branch GSTIN digits 1-2, else branch state, F1). Equal means CGST+SGST, different means IGST.** SEZ supplier means IGST (P2). Imports and overseas are deferred. | Same rule as India Compliance, scoped to what v1 documents need. |
| D-NUM-11 | Rounded total | `rounded_total` = grand total rounded to the currency's smallest fraction; INR has none set, so it rounds to a whole rupee with `rounding_adjustment` = rounded − grand. A per-document `disable_rounded_total` flag exists (`erpnext/erpnext/controllers/taxes_and_totals.py:888-907`; `frappe/frappe/utils/data.py:1379-1392`; `erpnext/erpnext/selling/doctype/quotation/test_quotation.py:1323`). Optional rounding of GST rows to whole rupees (`round_off_gst_values`, off by default) (`india-compliance/.../transaction.py:766-781`). | India cash rounding to ₹1, HALF-UP (`template_in.py:50-58`). | **Each DocType declares in Go whether it has a rounded total. If it does: rounded total = grand total rounded half-up to the rupee, and rounding adjustment = rounded − grand, in [−0.49, +0.50]. If it doesn't: rounded total = grand total and adjustment = 0.00. There is no per-document toggle in v1. GST components are never rounded to rupees.** | Fewer knobs. Zoho Books holds the ledger, so whole-rupee GST rounding is out of scope. Setting "rounded = grand" when off (ERPNext stores 0) avoids a zero that means "not applicable". |
| D-NUM-12 | Input with more decimals than allowed | Values are silently rounded on save (`round_floats_in`, `taxes_and_totals.py:229`). | Silently rounded to field digits. | **User input (forms and CSV) with more decimals than the type allows is rejected with a clear message. Only computed values are rounded.** | No silent changes to what the user typed (operating rule: never suppress errors). APR already relies on this message. |
| D-NUM-13 | Number display | India uses `#,##,###.##` (`frappe/frappe/geo/country_info.json:1259`). Lakh grouping (`frappe/frappe/utils/data.py:1483-1491`; `frappe/frappe/tests/test_fmt_money.py:49` `test_lacs`). Negative shows as "-1,000.00" (`test_fmt_money.py:27`). | "Indian Grouping" `[3,2,0]` renders 123456789 as 12,34,56,789.00 (`odoo/odoo/addons/base/models/res_lang.py:121-126`). | **Always Indian grouping (no setting). Money is shown with 2 decimals, a leading "-" for negatives and "₹" only where asked (`-₹1,234.50`). "-0.00" is never shown.** | Single company in India (F1/F2). |
| D-NUM-14 | Number parsing | `_parse_number` accepts both Indian (`12,34,567.89`) and international grouping. It rejects values that read differently in two formats (`frappe/frappe/core/doctype/data_import/importer.py:44-68`; `test_importer.py:1112` `test_parse_number_follows_number_format`). | n/a | **Accept digits with optional Indian or international grouping, an optional leading "-", an optional "₹" and surrounding spaces. Reject every other form: decimal comma, scientific notation, brackets, non-ASCII digits.** | Users paste from Excel and Zoho in both groupings. A decimal comma is never valid in India, so rejecting it removes ambiguity. |
| D-NUM-15 | Date display and parsing | India's default date format is `dd-mm-yyyy` (`frappe/frappe/geo/doctype/country/country.py:71`). Supported formats include month-first ones (`frappe/frappe/core/doctype/system_settings/system_settings.json:185`; `frappe/frappe/utils/dateutils.py:21-32`). The import guesser tries day-first then month-first (`frappe/frappe/utils/data.py:2679-2705`). | n/a | **Display `dd-mm-yyyy`. Parse `dd-mm-yyyy`, `dd/mm/yyyy`, `dd.mm.yyyy`, `dd-Mon-yyyy` and ISO `yyyy-mm-dd`. Day and month may be 1 or 2 digits. Never month-first, never 2-digit years. Years 1900-2099.** | Removes the 04/05 ambiguity for good. |
| D-NUM-16 | Where does the financial year come from? | `Fiscal Year` records; a date outside every record raises "Date … is not in any active Fiscal Year" (`erpnext/erpnext/accounts/utils.py:99-143`). The name is "2025-2026" (`erpnext/erpnext/setup/setup_wizard/operations/install_fixtures.py:598-604`; `erpnext/erpnext/accounts/doctype/fiscal_year/fiscal_year.py:116-118`). | Fiscal year last day/month on the company (not in the sparse clone). | **Computed from the date (F7: 1 April to 31 March), with no table. `FiscalYear(d)` returns start, end, label "2025-26" and short "25-26".** | Fixed by F7, so a table only adds a way to fail. NAM's file already expects `"26-27"` style short labels. |
| D-NUM-17 | Adding months to a date | `add_months` uses `relativedelta`, which clamps to month end (`frappe/frappe/utils/data.py:312`, `:330`). | n/a | **`AddMonths` clamps to the last day of the target month. Go's `time.AddDate` is never used for months.** | `time.Date(2025,1,31).AddDate(0,1,0)` is 3 March in Go. That would silently shift due dates. |
| D-NUM-18 | Time storage and "today" | Stored in system time zone. Asia/Kolkata offset is "+05:30" (`frappe/frappe/tests/test_utils.py:1254-1256`). | UTC storage. | **Timestamps are stored as fixed-width UTC text `2025-04-01T04:30:00.000Z`. Business dates are stored as `YYYY-MM-DD` text. "Today" and "this FY" use Asia/Kolkata. The binary embeds `time/tzdata`.** | F7. Fixed-width text sorts chronologically in SQLite and is readable. Embedding tzdata keeps the single binary working on hosts without a zoneinfo database. |
| D-NUM-19 | Filter semantics for empty values | `!=`, `not in` and `not like` wrap columns in `ifnull(col, '')`, so empty rows match `!=`. `in []` gives `1=0` and `not in []` gives `1=1`. `is set`/`is not set` exist (`frappe/frappe/tests/test_db_query.py:1683-1702` `test_coalesce_with_in_ops`; `:1177` `test_is_set_is_not_set`; `frappe/frappe/model/db_query.py:1093-1137`). | Domain operators (ORM core not in the sparse clone; couldn't verify). | **Same as Frappe: `!=` and `not in` include empty rows; `in []` matches nothing; `not in []` matches all. For text, "is empty" means NULL or ''. For numbers and dates, only NULL; 0 is a value.** | Matches what users expect: "branch is not Pune" should include drafts with no branch. |
| D-NUM-20 | Default sort and stable order | The DocType's `sort_field`, else `creation desc` (`frappe/frappe/model/db_query.py:1356-1373`). Pagination is limit/offset (`:1433-1434`). | `_order` per model, with `id` appended. | **Each DocType declares its default sort (documents: date desc then id desc; masters: name asc then id asc). `id` is always appended as the last key in the same direction, so pages never repeat or skip rows.** | Offset pagination is only stable with a unique final key. |
| D-NUM-21 | Page size and counts | `page_length` is free; `get_count` runs a separate COUNT with the same filters and may skip an exact count if slow (`frappe/frappe/desk/reportview.py:88-125`). | Pager with exact count. | **Offset pagination. Page size 25 by default, allowed 25/50/100/500. Always an exact count, with the same filters, search and permission predicate.** | At 15-20 users and tens of thousands of rows, SQLite counts are cheap. Keyset paging isn't worth it yet. |
| D-NUM-22 | Search | `search_widget` ORs `like %txt%` over name, title field and the DocType's `search_fields` (`frappe/frappe/desk/search.py:286-298`). LIKE values have `\` and `%` escaped (`frappe/frappe/model/db_query.py:1100-1102`). | `name_search` with `ilike`. | **Each DocType declares its search fields (masters: code, name; documents: number, party name). Search is a case-insensitive substring match on the trimmed text, with `%`, `_` and `\` taken literally, ANDed with the filters. Full-text search comes later.** | Simple and predictable. Good enough at this data size. |
| D-NUM-23 | CSV import: per row or all-or-nothing? | Each row is its own transaction: commit on success, rollback and log on failure, then retry failed rows (`frappe/frappe/core/doctype/data_import/importer.py:345-358`; `test_importer.py:251` `test_data_import_without_mandatory_values`; `:171` `test_skip_rows_during_import`). | `base_import` isn't in the sparse clone (couldn't verify). | **Validate every row first (dry run). Import only when no row has an error, in one transaction. Otherwise nothing is written and every error is listed with its row number.** | A half-imported master list is harder to fix than a rejected file. Files are small (≤5,000 rows), and the user fixes the file and uploads it again. |
| D-NUM-24 | CSV encoding | Tries `utf-8-sig`, `utf-8`, `windows-1250`, `windows-1252` in order (`frappe/frappe/core/doctype/file/file.py:48`; `frappe/frappe/utils/csvutils.py:70-86`). | n/a | **UTF-8 only, with or without a BOM. Anything else is rejected with "save as CSV UTF-8" instructions.** | A Windows-1252 guess can silently garble names. Excel's "CSV UTF-8" option is standard. |
| D-NUM-25 | What can be imported in v1? | Insert, update or upsert into any DocType, including child tables and trees (`test_importer.py:293`, `:327`). | n/a | **v1: insert new master records only (party, address/contact, item, UoM, tax code, project, site, employee, expense type). Update/upsert and document import (PO with lines) are deferred.** | Masters are the real onboarding need. Updates via CSV need a change-review step. |
| D-NUM-26 | CSV export format and safety | Escapes values starting with `= + - @ \t \r` with a leading `'` and reverses that on import (`frappe/frappe/utils/csvutils.py:13-37`; `frappe/frappe/tests/test_utils.py:1574`). Child fields the user can't read are left out (`frappe/frappe/core/doctype/data_import/test_exporter.py:126`). | n/a | **UTF-8 with BOM. Same filters, search, sort and permission predicate as the list, over all matching rows. Numbers are plain (`141120000.00`), dates ISO (`2025-04-01`), timestamps IST (`2025-04-01 10:00:00`). Only text cells get formula escaping, never numeric cells.** | Excel opens it correctly in any locale. ISO dates can't be misread as month-first. Numeric `-500.00` must stay numeric. |
| D-NUM-27 | Third-party libraries | n/a | n/a | **Standard library only: `math/bits` or `math/big` (128-bit products), `time` + `time/tzdata`, `encoding/csv`, `unicode/utf8`, `strconv`. No decimal library.** Considered and rejected: `github.com/shopspring/decimal` (MIT), `github.com/cockroachdb/apd/v3` (Apache-2.0), `github.com/govalues/decimal` (MIT). | Fixed-scale int64 with one rounding mode is about 300 lines and maps directly to SQLite `INTEGER`. Arbitrary-precision decimals would need TEXT columns and conversion at every query. Latest versions of the rejected libraries weren't checked, since none is recommended. |

> **Fixtures:** every case uses `00-fixtures.md` (spec R5): company Vardhan Infra Projects Pvt Ltd, branches `PUNE` (Pune Works, `27AAFCV2449G1Z7`, Maharashtra) and `BLR` (Bengaluru Site Office, `29AAFCV2449G1Z3`, Karnataka), parties `SHREE` (Mumbai `27AABCS1429B1ZU`), `BHARAT` (`29AABCT1332L1ZA`, Karnataka), `RAJKOT` (`24AAACR5055K1ZD`, Gujarat), `COROM` (`33AAACT2727Q1Z3`, Tamil Nadu) and `LOCAL` (unregistered, Pune), the fixture users, and the clock 2026-10-24 10:30 IST (FY 2026-27). The brief's sample GSTIN `27AABCS1429B1Z5` fails the GSTIN check-digit algorithm used by India Compliance (`india-compliance/india_compliance/gst_india/utils/__init__.py:554-573`). It appears only where a test needs an invalid check digit (NUM-145).

## Test cases

### A. Value types and rounding

### NUM-001 · Money is stored as integer paise and round-trips through text
- **Priority:** P1
- **Level:** unit
- **Given** the text "14,112.80" **When** `ParseMoney` runs **Then** it returns `Money(1411280)`, `FormatMoney(1411280)` returns "14,112.80" and `FormatINR(1411280)` returns "₹14,112.80". `ParseMoney("0")` gives 0 and `FormatMoney(0)` gives "0.00".
- **Go test:** `TestMoney_ParseFormatRoundTrip`
- **Sources:** `frappe/frappe/utils/data.py:1135` (`flt`), `frappe/frappe/tests/test_utils.py:2130` (`test_flt_parses_strings_and_ignores_invalid_input`)

### NUM-002 · Half-up rounding at a tie rounds away from zero
- **Priority:** P1
- **Level:** unit
- **Given** qty 2.5 and rate ₹33.33 (exact product ₹83.325) **When** the line amount is computed **Then** it is ₹83.33 (banker's rounding would give ₹83.32). Rounding ₹0.5 to rupees gives ₹1, ₹2.5 gives ₹3 and ₹-0.5 gives ₹-1.
- **Go test:** `TestRound_HalfUpTies`
- **Sources:** `frappe/frappe/tests/test_utils.py:2137` (`test_commercial_rounding_examples`), `odoo/odoo/tools/float_utils.py:135` (HALF-UP)

### NUM-003 · Rounding is sign-symmetric and idempotent
- **Priority:** P1
- **Level:** unit (fuzz)
- **Given** any scaled value x and any target scale **When** it is rounded **Then** `Round(-x) == -Round(x)` (e.g. −0.005 → −0.01 and 0.005 → 0.01) and `Round(Round(x)) == Round(x)`.
- **Go test:** `FuzzRound_SymmetricIdempotent`
- **Sources:** `frappe/frappe/tests/test_utils.py:2200` (`test_rounding_is_sign_symmetric`), `frappe/frappe/tests/test_utils.py:2173` (`test_rounding_is_idempotent`)

### NUM-004 · Values that trip up floats round correctly
- **Priority:** P1
- **Level:** unit
- **Given** the exact values 2.675, 1.005 and 1.015 **When** each is rounded to 2 dp **Then** the results are 2.68, 1.01 and 1.02. ₹0.10 + ₹0.20 equals exactly ₹0.30, and ten additions of ₹0.10 equal ₹1.00.
- **Go test:** `TestRound_FloatTrapValues`
- **Sources:** `frappe/frappe/tests/test_utils.py:2143` (`test_commercial_rounding_examples`, 2.675 case), `odoo/odoo/tools/float_utils.py:121-131` (IEEE-754 epsilon comment)

### NUM-005 · Line amount matches an exact rational reference
- **Priority:** P1
- **Level:** unit (fuzz)
- **Given** random qty in [0.001, 1,00,00,000.000] and rate in [0.0001, 10,00,00,000.0000] **When** `LineAmount(qty, rate)` runs **Then** it equals `big.Rat` qty×rate rounded half-up to paise, or returns the "too large" error from NUM-012. It never panics or wraps around.
- **Go test:** `FuzzLineAmount_MatchesBigRat`
- **Sources:** `frappe/frappe/tests/test_utils.py:2182` (`test_commercial_rounding_matches_round_half_up`)

### NUM-006 · Quantity has 3 decimals
- **Priority:** P1
- **Level:** unit
- **Given** the texts "2.5", "1,00,000" and "0.001" **When** parsed as `Qty` **Then** they become 2500, 100000000 and 1 (thousandths). "1.2345" fails with "Quantity can have at most 3 decimal places."
- **Go test:** `TestQty_ParseScale`
- **Sources:** `frappe/frappe/desk/page/setup_wizard/setup_wizard.py:372`, `frappe/frappe/model/meta.py:989`

### NUM-007 · Whole-number units reject fractions
- **Priority:** P1
- **Level:** unit
- **Given** a `BOLT-M12` line with UoM "Nos", flagged whole-number (MST) **When** qty 3.4 is validated **Then** it fails with "Quantity for Nos must be a whole number (you entered 3.4)." Qty 3 or 3.000 passes. For an `MS-ANGLE-50` line in UoM "kg" (not whole-number), 3.4 passes.
- **Go test:** `TestQty_WholeNumberUoM`
- **Sources:** `erpnext/erpnext/buying/doctype/purchase_order/test_purchase_order.py:888` (`test_uom_integer_validation`), `erpnext/erpnext/utilities/transaction_base.py:644-662`

### NUM-008 · Rate has 4 decimals and shows at least 2
- **Priority:** P1
- **Level:** unit
- **Given** "12.345", "0.1234" and "12.5" **When** parsed as `Rate` **Then** they become 123450, 1234 and 125000. They display as "12.345", "0.1234" and "12.50". "0.12345" fails with "Rate can have at most 4 decimal places."
- **Go test:** `TestRate_ParseFormat`
- **Sources:** `tryton/modules/purchase/purchase.py:1297` (`digits=price_digits`), `erpnext/erpnext/buying/doctype/purchase_order_item/purchase_order_item.json` (`rate` Currency)

### NUM-009 · Percent is stored in thousandths of a percent
- **Priority:** P1
- **Level:** unit
- **Given** "18", "0.25", "0.125" and "1.5" **When** parsed as `Percent` **Then** they become 18000, 250, 125 and 1500. "0.1255" fails with "Percentage can have at most 3 decimal places." "100.001" and "-1" fail with "Percentage must be between 0 and 100."
- **Go test:** `TestPercent_ParseRange`
- **Sources:** `frappe/frappe/model/meta.py:989` (Percent uses float precision 3)

### NUM-010 · Excess decimals in typed money are rejected, not rounded
- **Priority:** P1
- **Level:** unit
- **Given** the money input "10.005" **When** `ParseMoney` runs **Then** it fails with "Amount can have at most 2 decimal places." "10.50" and "10.5" are accepted as 1050 paise.
- **Go test:** `TestMoney_RejectExcessDecimals`
- **Sources:** `erpnext/erpnext/controllers/taxes_and_totals.py:229` (`round_floats_in`, silent rounding not followed); D-NUM-12

### NUM-011 · Negative rates and non-positive line quantities are rejected
- **Priority:** P1
- **Level:** unit
- **Given** rate "-5" **When** validated as a line rate **Then** it fails with "Rate cannot be negative." Given qty 0 or -1 on a line, validation fails with "Quantity must be more than 0." Rate 0.0000 is allowed (free item).
- **Go test:** `TestLineValues_SignRules`
- **Sources:** BRIEF example message; `erpnext/erpnext/controllers/taxes_and_totals.py:235-240` (zero-qty handling only for returns/debit notes, deferred)

### NUM-012 · Upper limits give a clear error instead of overflowing
- **Priority:** P1
- **Level:** unit
- **Given** qty 1,00,00,000.000 (the maximum) and rate ₹10,00,00,000.0000 (the maximum) **When** the line amount is computed **Then** the 128-bit product is checked and the call fails with "Line amount is too large. The most a line can be is ₹10,00,00,00,000.00." Entering money "10,00,00,00,000.01" fails with "Amount is too large. The maximum is ₹10,00,00,00,000.00." Entering qty "1,00,00,000.001" fails with "Quantity is too large. The maximum is 1,00,00,000."
- **Go test:** `TestLimits_NoOverflow`
- **Sources:** D-NUM-01 (int64 range); no direct source equivalent

### NUM-013 · Line amount is qty × rate rounded half-up to paise
- **Priority:** P1
- **Level:** unit
- **Given** these lines (qty, rate) **When** amounts are computed **Then**: (6, 12.30) → ₹73.80; (3.333, 12.345) → ₹41.15 (exact 41.145885); (1,000, 0.1234) → ₹123.40; (2.5, 33.33) → ₹83.33.
- **Go test:** `TestLineAmount_Table`
- **Sources:** `erpnext/erpnext/controllers/taxes_and_totals.py:240` (`amount = flt(rate * qty, precision)`), `tryton/modules/purchase/purchase.py:1741-1743`, `erpnext/erpnext/selling/doctype/quotation/test_quotation.py:1323`

### NUM-014 · UoM conversion rounds stock qty to 3 decimals
- **Priority:** P1
- **Level:** unit
- **Given** qty 3,333.333 kg and factor 0.6 (a kg → Nos conversion set up in this test) **When** stock qty is computed **Then** it is 2,000.000 (exact 1,999.9998). Factor text "0.0000000001" fails with "Conversion factor can have at most 9 decimal places." Factor 0 fails with "Conversion factor must be more than 0."
- **Go test:** `TestFactor_StockQtyRounding`
- **Sources:** `erpnext/erpnext/buying/doctype/purchase_order/test_purchase_order.py:967` (`test_ordered_qty_rounds_each_open_order_for_whole_number_item`), `erpnext/erpnext/buying/doctype/purchase_order_item/purchase_order_item.json:259`

### NUM-015 · Line discount percent gives a rate rounded to 4 decimals
- **Priority:** P2
- **Level:** unit
- **Given** price ₹100.0000 and discount 12.5% **When** the net rate is computed **Then** it is ₹87.5000. Given price ₹99.9900 and discount 33.333%, it is ₹66.6603. A discount of 100% gives rate 0.0000. 100.001% fails with "Percentage must be between 0 and 100."
- **Go test:** `TestDiscount_NetRate`
- **Sources:** `erpnext/erpnext/controllers/taxes_and_totals.py:172` (`calculate_item_rate`)

### NUM-016 · No floating-point columns exist in the schema
- **Priority:** P1
- **Level:** integration
- **Given** every goose migration applied to a fresh SQLite database **When** `PRAGMA table_info` is read for all tables except goose's own tables (every table whose name starts with `goose_`, CD-71; no other exclusion) **Then** every column's declared type is exactly INTEGER, TEXT or BLOB (CD-34), so no column is REAL, FLOAT, DOUBLE, NUMERIC, DECIMAL, BOOLEAN, DATETIME or untyped. Every Money/Qty/Rate/Percent/Factor column is INTEGER NOT NULL (or INTEGER where optional).
- **Go test:** `TestSchema_NoFloatColumns`
- **Sources:** D-NUM-01

### NUM-017 · Totals in SQL are exact integer sums
- **Priority:** P1
- **Level:** integration
- **Given** 10,000 rows each with amount ₹0.01 **When** the list totals query runs **Then** it returns 10000 paise (₹100.00) as an int64. A query helper that gets a REAL value for a money column (e.g. from `TOTAL()` or `AVG()`) returns the error "Money column returned a decimal value; use SUM on integer columns."
- **Go test:** `TestSQL_SumIsExactInteger`
- **Sources:** D-NUM-01

### NUM-018 · Money crosses forms and JSON as a string
- **Priority:** P2
- **Level:** unit
- **Given** `Money(123450)` **When** marshalled to JSON **Then** the output is `"1234.50"`. Unmarshalling `"1,234.50"` gives 123450. Unmarshalling the JSON number `1234.5` fails with "Amount must be sent as text, e.g. \"1234.50\"."
- **Go test:** `TestMoney_JSONAsString`
- **Sources:** D-NUM-01

### NUM-019 · Parse and format round-trip for every value type, and the parser never panics
- **Priority:** P1
- **Level:** unit (fuzz)
- **Given** any int64 within limits for Money, Qty, Rate, Percent and Factor **When** formatted and parsed back **Then** the value is unchanged. Given arbitrary byte strings, every parser returns a value or an error and never panics.
- **Go test:** `FuzzValues_RoundTripNoPanic`
- **Sources:** `frappe/frappe/tests/test_utils.py:2214` (`test_cint` property test)

### B. Line and document totals

### NUM-020 · Net total is the sum of rounded line amounts
- **Priority:** P1
- **Level:** unit
- **Given** lines of ₹73.80, ₹41.15 and ₹123.40 **When** totals are computed **Then** net total = ₹238.35 exactly. Reordering the lines gives the same totals.
- **Go test:** `TestTotals_NetIsSumOfLines`
- **Sources:** `erpnext/erpnext/controllers/taxes_and_totals.py:456` (`calculate_net_total`)

### NUM-021 · Tax is rounded per line, not on the document total
- **Priority:** P1
- **Level:** unit
- **Given** 3 inter-state lines of ₹10.25 at 18% **When** tax is computed **Then** each line's IGST is ₹1.85 (exact 1.845) and the document IGST total is ₹5.55. Rounding on the net total would give ₹5.54, and that result must not appear. Intra-state, the same lines give CGST ₹0.92 per line (₹2.76 total) and SGST ₹2.76.
- **Go test:** `TestTax_RoundPerLine`
- **Sources:** `odoo/addons/l10n_in/models/template_in.py:46` (`round_per_line`), `odoo/addons/account/tests/test_taxes_computation.py:66` (`test_random_case_1`, per-line vs global), `erpnext/erpnext/controllers/taxes_and_totals.py:481-575`

### NUM-022 · Line GST breakup always adds up to the header, even across many lines
- **Priority:** P1
- **Level:** unit
- **Given** 250 intra-state lines, each qty 1 at rate ₹12.345 (amount ₹12.35) at 18% **When** totals are computed **Then** each line's CGST is ₹1.11 and the header CGST is ₹277.50 = Σ lines, with the same for SGST. No line differs from its own rate×taxable by more than ₹0.005. Rounding the whole document instead (₹277.88) is never used.
- **Go test:** `TestTax_HeaderEqualsSumOfLines`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/test_transaction.py:1226` (`test_rounding_gst_details_across_many_items`), `india-compliance/india_compliance/gst_india/utils/gstr_1/test_rounding.py:113` (`test_no_row_carries_everyone_elses_rounding`)

### NUM-023 · Grand total and rounded total
- **Priority:** P1
- **Level:** unit
- **Given** a DocType with rounded total enabled **When** grand totals are ₹73.80, ₹1,180.50 and ₹1,180.49 **Then** rounded totals are ₹74.00, ₹1,181.00 and ₹1,180.00, with rounding adjustments +0.20, +0.50 and −0.49. Grand total = net total + Σ CGST + Σ SGST + Σ IGST.
- **Go test:** `TestTotals_RoundedTotalHalfUp`
- **Sources:** `erpnext/erpnext/selling/doctype/quotation/test_quotation.py:1323` (`test_grand_total_and_rounded_total_values`), `erpnext/erpnext/controllers/taxes_and_totals.py:888-907`, `odoo/addons/l10n_in/models/template_in.py:50-58`

### NUM-024 · Rounded total off for a DocType
- **Priority:** P1
- **Level:** unit
- **Given** a DocType declared without rounded total (e.g. Expense Claim) and grand total ₹14,112.80 **When** totals are computed **Then** rounded total = ₹14,112.80, rounding adjustment = ₹0.00 and the payable total = ₹14,112.80. With rounding on, payable = ₹14,113.00 (FILE-082 puts this into words).
- **Go test:** `TestTotals_RoundingDisabledForDocType`
- **Sources:** `erpnext/erpnext/selling/doctype/quotation/test_quotation.py:1330-1335`, `erpnext/erpnext/controllers/tests/test_taxes_and_totals.py:132` (`test_disabling_rounded_total_resets_base_fields`)

### NUM-025 · Zero-rated, nil-rated and exempt lines carry no GST
- **Priority:** P1
- **Level:** unit
- **Given** a line of ₹5,000.00 whose tax code is 0% (nil-rated) next to an 18% line of ₹1,000.00 (intra-state) **When** totals are computed **Then** the 0% line has CGST/SGST/IGST of ₹0.00, the document CGST = ₹90.00, SGST = ₹90.00 and grand total = ₹6,180.00.
- **Go test:** `TestTax_ZeroRatedLine`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/test_transaction.py:1219` (`test_rounding_gst_details`, nil-rated item appended)

### NUM-026 · Recomputing totals is deterministic
- **Priority:** P1
- **Level:** integration
- **Given** the reference PO (`SHREE` to branch `PUNE`, taxable ₹79,800.00, CGST ₹7,182.00, SGST ₹7,182.00, grand total ₹94,164.00) saved as a draft **When** totals are recomputed from its stored lines without changes **Then** every stored line and header value is identical and DOC records no field change in the audit trail.
- **Go test:** `TestTotals_RecomputeIsStable`
- **Sources:** `erpnext/erpnext/controllers/taxes_and_totals.py:51` (`calculate`)

### C. GST split

### NUM-030 · Same state gives CGST + SGST
- **Priority:** P1
- **Level:** unit
- **Given** branch `PUNE` Pune Works (`27AAFCV2449G1Z7`, state 27) and supplier `SHREE` Shree Steel Traders (Mumbai `27AABCS1429B1ZU`) **When** a ₹1,000.00 line at 18% is taxed **Then** CGST = ₹90.00 at 9%, SGST = ₹90.00 at 9% and IGST = ₹0.00. The reference PO (MS-ANGLE-50 1,200.000 kg @ ₹62.5000 = ₹75,000.00 and WIRE-18G 50.000 kg @ ₹96.0000 = ₹4,800.00) gives taxable ₹79,800.00, CGST ₹7,182.00, SGST ₹7,182.00, IGST ₹0.00 and grand total ₹94,164.00.
- **Go test:** `TestGST_IntraStateSplit`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/transaction.py:609-624` (`is_inter_state_supply`), `odoo/addons/l10n_in/models/template_in.py:69-75`

### NUM-031 · Different state gives IGST
- **Priority:** P1
- **Level:** unit
- **Given** branch `PUNE` (27) and supplier `BHARAT` Bharat Tools (`29AABCT1332L1ZA`, Karnataka) **When** a ₹1,000.00 line at 28% is taxed **Then** IGST = ₹280.00 and CGST = SGST = ₹0.00. Supplier `SHREE` (27) to branch `BLR` (`29AAFCV2449G1Z3`) also gives IGST.
- **Go test:** `TestGST_InterStateIGST`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/transaction.py:609-660`, `odoo/addons/l10n_in/models/template_in.py:76-81`

### NUM-032 · The half-paisa case: ₹10.05 at 18%
- **Priority:** P1
- **Level:** unit
- **Given** taxable ₹10.05 at 18% **When** split intra-state **Then** CGST = round(0.9045) = ₹0.90 and SGST = ₹0.90 (total ₹1.80). Inter-state, IGST = round(1.809) = ₹1.81. Splitting ₹1.81 into halves (₹0.905) never happens, and CGST always equals SGST.
- **Go test:** `TestGST_HalfPaisaSplit`
- **Sources:** `odoo/addons/account/tests/test_taxes_computation.py:545` (`test_percent_taxes_for_l10n_in`, equal halves), `india-compliance/india_compliance/gst_india/overrides/transaction.py:418-447`

### NUM-033 · Tie cases round half-up for IGST and the halves
- **Priority:** P1
- **Level:** unit
- **Given** taxable ₹10.25 at 18% **When** taxed **Then** IGST = ₹1.85 (exact 1.845; banker's would give 1.84), and intra-state CGST = SGST = ₹0.92 (exact 0.9225). Given taxable ₹0.50 at 5%, IGST = ₹0.03 (exact 0.025) and CGST = SGST = ₹0.01 (exact 0.0125).
- **Go test:** `TestGST_TieRounding`
- **Sources:** `odoo/odoo/tools/float_utils.py:135`, D-NUM-07

### NUM-034 · Known India Compliance value: ₹62.51 at 18% intra-state
- **Priority:** P1
- **Level:** unit
- **Given** taxable ₹62.51 at 18% intra-state **When** taxed **Then** CGST = ₹5.63 (exact 5.6259) and SGST = ₹5.63.
- **Go test:** `TestGST_KnownValue6251`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/test_transaction.py:1219` (`test_rounding_gst_details`)

### NUM-035 · Fractional GST rates split exactly
- **Priority:** P1
- **Level:** unit
- **Given** taxable ₹1,00,000.00 at 0.25% **When** taxed **Then** IGST = ₹250.00 and intra-state CGST = SGST = ₹125.00 at 0.125% (stored as 125 millipercent). Given ₹33,333.33 at 3%, IGST = ₹1,000.00 (exact 999.9999) and CGST = SGST = ₹500.00 (exact 499.99995) at 1.5%.
- **Go test:** `TestGST_FractionalRates`
- **Sources:** D-NUM-05

### NUM-036 · CGST always equals SGST (property)
- **Priority:** P1
- **Level:** unit (fuzz)
- **Given** random taxable values from ₹0.01 to ₹10,00,00,000.00 and rates from {0, 0.1, 0.25, 1, 1.5, 3, 5, 6, 12, 18, 28, 40}% **When** split intra-state **Then** CGST == SGST, CGST + SGST differs from the inter-state IGST by at most ₹0.01, and every amount ≥ 0.
- **Go test:** `FuzzGST_HalvesEqual`
- **Sources:** `odoo/addons/account/tests/test_taxes_computation.py:545-566`

### NUM-037 · A document never mixes IGST with CGST/SGST
- **Priority:** P1
- **Level:** integration
- **Given** an intra-state document (supplier `SHREE` to branch `PUNE`) whose user tries to set one line's tax type to IGST (manual override on an expense line) **When** saved **Then** it fails with "This bill is intra-state (supplier and branch are both in Maharashtra), so use CGST and SGST, not IGST." The reverse (CGST on an inter-state document from `BHARAT` to `PUNE`) fails with "This bill is inter-state (Karnataka to Maharashtra), so use IGST, not CGST and SGST."
- **Go test:** `TestGST_NoMixedTaxTypes`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/transaction.py:418-447` (`validate_for_invalid_account_type`), `india-compliance/india_compliance/gst_india/overrides/test_transaction.py:1515` (`test_invalid_intra_state_supply`)

### NUM-038 · An unregistered supplier's state comes from their address
- **Priority:** P1
- **Level:** unit
- **Given** the unregistered supplier `LOCAL` Ganesh Hardware (no GSTIN, billing address in Maharashtra, 27) and branch `BLR` (29) **When** the split is decided **Then** IGST applies. With branch `PUNE` (27), CGST+SGST applies. If the state on `LOCAL`'s address is cleared in this test, so it has neither a GSTIN nor an address state, it fails with "Set the state on the supplier's address so GST can be worked out."
- **Go test:** `TestGST_UnregisteredSupplierState`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/transaction.py:648-654`

### NUM-039 · A branch without a GSTIN uses its state; a branch without a state is an error
- **Priority:** P1
- **Level:** unit
- **Given** an extra branch `NASHIK` "Nashik Store", created in this test with no GSTIN and state Maharashtra (27), and supplier `SHREE` (27) **When** the split is decided **Then** CGST+SGST applies. A branch with neither GSTIN nor state fails with "Set the state for branch Nashik Store so GST can be worked out."
- **Go test:** `TestGST_BranchStateFallback`
- **Sources:** BRIEF F1; `india-compliance/india_compliance/gst_india/utils/__init__.py:720-723`

### NUM-040 · The state code comes from the GSTIN's first two digits
- **Priority:** P1
- **Level:** unit
- **Given** supplier `RAJKOT` Rajkot Castings with GSTIN `24AAACR5055K1ZD` **When** the state code is resolved **Then** it is "24" (Gujarat), even if the supplier's address record says a different state. (MST flags that mismatch on the master.)
- **Go test:** `TestGST_StateFromGSTIN`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/transaction.py:657` (`(doc.supplier_gstin or doc.company_gstin)[:2]`)

### NUM-041 · Changing supplier or branch on a draft recomputes the split
- **Priority:** P1
- **Level:** integration
- **Given** a draft PO from `SHREE` (27) to branch `PUNE` (27) with CGST ₹90.00 + SGST ₹90.00 on ₹1,000.00 **When** the supplier is changed to `BHARAT` (29) and saved **Then** the line shows IGST ₹180.00, CGST = SGST = ₹0.00, and the grand total stays ₹1,180.00.
- **Go test:** `TestGST_RecomputeOnPartyChange`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/test_transaction.py:2373` (`test_correct_place_of_supply_on_address_update_by_erpnext`)

### NUM-042 · An SEZ supplier is always inter-state
- **Priority:** P2
- **Level:** unit
- **Given** supplier `SHREE` (Maharashtra, 27) with its GST category changed to "SEZ" in this test, and branch `PUNE` (27) **When** the split is decided **Then** IGST applies.
- **Go test:** `TestGST_SEZIsInterState`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/transaction.py:617-618`

### NUM-043 · GST typed from a bill is accepted within ₹1 of the computed value
- **Priority:** P2
- **Level:** unit
- **Given** an expense line with taxable ₹100.00 at 18% intra-state and a user-typed CGST of ₹9.40 **When** saved **Then** it is accepted (difference ₹0.40 ≤ ₹1.00). A typed CGST of ₹12.50 fails with "CGST on line 2 is ₹12.50 but 9% of ₹100.00 is ₹9.00. Check the rate or the amount." A typed SGST different from the CGST fails with "CGST and SGST must be the same amount."
- **Go test:** `TestGST_EnteredAmountTolerance`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/transaction.py:69` (`ALLOWED_TAX_DIFFERENCE = 1`), `:1394-1424` (`validate_item_gst_details`), `india-compliance/india_compliance/gst_india/overrides/test_transaction.py:1203` (`test_invalid_item_gst_details`)

### NUM-044 · Working back from a tax-inclusive amount gives exactly the gross
- **Priority:** P2
- **Level:** unit
- **Given** a gross bill amount of ₹11.86 at 18% **When** split intra-state **Then** taxable = ₹10.06, CGST = ₹0.90 and SGST = ₹0.90 (sum ₹11.86). Taxable is rounded from 10.0508, then the ₹0.01 shortfall goes to taxable. Inter-state: taxable = ₹10.05 and IGST = ₹1.81 (sum ₹11.86). Gross ₹1,180.00 at 18% gives taxable ₹1,000.00 with CGST ₹90.00 and SGST ₹90.00.
- **Go test:** `TestGST_InclusiveBackCalculation`
- **Sources:** `erpnext/erpnext/controllers/taxes_and_totals.py:354-400` (`determine_exclusive_rate`), `odoo/addons/account/tests/test_taxes_computation.py:66-96` (price-included 8%)

### D. Indian number display and parsing

### NUM-050 · Lakh/crore grouping
- **Priority:** P1
- **Level:** unit
- **Given** rupee amounts 100; 1,000; 10,000; 1,00,000; 10,00,000; 1,00,00,000; 10,00,00,000; 1,00,00,00,000 **When** `FormatMoney` runs **Then** the outputs are "100.00", "1,000.00", "10,000.00", "1,00,000.00", "10,00,000.00", "1,00,00,000.00", "10,00,00,000.00" and "1,00,00,00,000.00".
- **Go test:** `TestFormat_IndianGrouping`
- **Sources:** `frappe/frappe/tests/test_fmt_money.py:49` (`test_lacs`), `odoo/odoo/addons/base/tests/test_res_lang.py:11` (`test_00_intersperse`), `odoo/odoo/addons/base/models/res_lang.py:121-126`

### NUM-051 · The brief's examples
- **Priority:** P1
- **Level:** unit
- **Given** 1411280 paise and 1411200000 paise **When** formatted **Then** the results are "14,112.80" and "1,41,12,000.00". With symbol: "₹14,112.80" and "₹1,41,12,000.00".
- **Go test:** `TestFormat_BriefExamples`
- **Sources:** `frappe/frappe/utils/data.py:1428-1491` (`fmt_money`)

### NUM-052 · Negative numbers and zero
- **Priority:** P1
- **Level:** unit
- **Given** −1,00,000.00, −0.01 and −1,234.50 **When** formatted **Then** the results are "-1,00,000.00", "-0.01" and "-1,234.50", and with symbol "-₹1,234.50". A value that rounds to zero (e.g. a display of −0.004 rupee) shows "0.00", never "-0.00".
- **Go test:** `TestFormat_NegativeAndZero`
- **Sources:** `frappe/frappe/tests/test_fmt_money.py:27` (`test_negative`), `frappe/frappe/tests/test_fmt_money.py:60` (`test_no_precision`, "-0.3" → "0")

### NUM-053 · Quantity display drops trailing zeros
- **Priority:** P1
- **Level:** unit
- **Given** quantities 2.500, 1,00,000.000, 0.001 and 12.340 **When** formatted **Then** the results are "2.5", "1,00,000", "0.001" and "12.34". With UoM: "2.5 kg".
- **Go test:** `TestFormat_Qty`
- **Sources:** `frappe/frappe/tests/test_fmt_money.py:70` (`test_currency_precision`, trailing digits trimmed beyond 2)

### NUM-054 · Rate display shows 2 to 4 decimals
- **Priority:** P1
- **Level:** unit
- **Given** rates 12.3450, 12.5000, 1,23,456.7890 and 0.1234 **When** formatted **Then** the results are "12.345", "12.50", "1,23,456.789" and "0.1234".
- **Go test:** `TestFormat_Rate`
- **Sources:** `frappe/frappe/tests/test_fmt_money.py:70-88` (`test_currency_precision`)

### NUM-055 · Percent display
- **Priority:** P1
- **Level:** unit
- **Given** 18000, 250, 125, 1500 and 0 millipercent **When** formatted **Then** the results are "18%", "0.25%", "0.125%", "1.5%" and "0%".
- **Go test:** `TestFormat_Percent`
- **Sources:** D-NUM-05

### NUM-056 · Accepted number inputs
- **Priority:** P1
- **Level:** unit
- **Given** the money inputs "1,41,12,000.00", "14112000", "1,41,12,000", "14,112,000.00", "₹ 1,234.50", "₹1,234.50", "  1234.5  ", ".5", "5." and "-1,234.50" **When** parsed **Then** they give 1411200000, 1411200000, 1411200000, 1411200000, 123450, 123450, 123450, 50, 500 and −123450 paise.
- **Go test:** `TestParse_AcceptedForms`
- **Sources:** `frappe/frappe/core/doctype/data_import/test_importer.py:1112` (`test_parse_number_follows_number_format`, "12,34,567.89" accepted), `frappe/frappe/core/doctype/data_import/importer.py:54-56`

### NUM-057 · Rejected number inputs
- **Priority:** P1
- **Level:** unit
- **Given** "1.234,56", "12,34", "1,2345.00", "1e5", "1.5E+11", "(500)", "Rs. 500", "abc", "१२३", "1 234" and "--5" **When** parsed as money **Then** each fails with "\"<input>\" is not a valid amount. Write it like 1,234.50 or 1234.50." An empty input on a required field fails with DOC's "<Label> is required."
- **Go test:** `TestParse_RejectedForms`
- **Sources:** `frappe/frappe/core/doctype/data_import/test_importer.py:1112` ("19,18", "1.234,56" → None under `#,###.##`), `frappe/frappe/core/doctype/data_import/importer.py:1690-1703`

### NUM-058 · Each type has its own decimal limit message
- **Priority:** P1
- **Level:** unit
- **Given** "1.001" as money, "1.0001" as quantity, "1.00001" as rate and "1.0001" as percent **When** parsed **Then** the messages are "Amount can have at most 2 decimal places.", "Quantity can have at most 3 decimal places.", "Rate can have at most 4 decimal places." and "Percentage can have at most 3 decimal places." Trailing zeros beyond the limit ("1.000" as money) are accepted.
- **Go test:** `TestParse_DecimalLimitsPerType`
- **Sources:** D-NUM-12; `docs/tests/core/04-approvals.md` (APR relies on the money message)

### NUM-059 · Amount fields reformat on blur
- **Priority:** P2
- **Level:** e2e
- **Given** the PO line form **When** the user types "1411200" in Rate and tabs out **Then** the field shows "14,11,200.00" and the line amount updates. Typing "1.234,56" shows the inline error from NUM-057 and keeps the typed text.
- **Go test:** `TestE2E_AmountInputFormatting`
- **Sources:** `frappe/frappe/utils/data.py:1428` (`fmt_money`)

### E. Dates and financial year

### NUM-070 · Financial year from a date
- **Priority:** P1
- **Level:** unit
- **Given** dates 01-04-2025, 15-01-2026, 31-03-2026 and 01-04-2026 **When** `FiscalYear(d)` runs **Then** the results are 2025-26, 2025-26, 2025-26 and 2026-27.
- **Go test:** `TestFiscalYear_FromDate`
- **Sources:** `erpnext/erpnext/accounts/utils.py:121-128` (`get_fiscal_years`, start ≤ date ≤ end)

### NUM-071 · Financial year labels and bounds, including the century
- **Priority:** P1
- **Level:** unit
- **Given** 10-10-2026 **When** `FiscalYear` runs **Then** start = 2026-04-01, end = 2027-03-31, label = "2026-27" and short = "26-27". Given 01-04-1999: label "1999-00", short "99-00". Given 01-04-2099: label "2099-00", short "99-00".
- **Go test:** `TestFiscalYear_Labels`
- **Sources:** `erpnext/erpnext/setup/setup_wizard/operations/install_fixtures.py:598-604` (`get_fy_details`), `docs/tests/core/02-naming.md` (NAM expects `"26-27"`)

### NUM-072 · Leap financial years
- **Priority:** P1
- **Level:** unit
- **Given** FY 2023-24 and FY 2024-25 **When** their day counts are computed **Then** they are 366 (includes 29-02-2024) and 365. FY 2027-28 has 366 (29-02-2028).
- **Go test:** `TestFiscalYear_LeapDays`
- **Sources:** `erpnext/erpnext/accounts/doctype/fiscal_year/test_fiscal_year.py:11` (`test_extra_year`, bounds validation)

### NUM-073 · A timestamp's business date and FY use IST
- **Priority:** P1
- **Level:** unit
- **Given** UTC 2025-03-31T18:29:59Z **When** converted to a business date **Then** it is 31-03-2025 (23:59:59 IST), FY 2024-25. UTC 2025-03-31T18:30:00Z gives 01-04-2025 (00:00 IST), FY 2025-26.
- **Go test:** `TestDate_BusinessDateFromUTC`
- **Sources:** BRIEF F7; `frappe/frappe/utils/data.py:402` (`convert_utc_to_timezone`)

### NUM-074 · "Today" is the Indian date, not the server's UTC date
- **Priority:** P1
- **Level:** unit
- **Given** an injected clock at 2025-03-31T20:00:00Z **When** `Today()` runs (e.g. for a new PO's default date) **Then** it returns 01-04-2025, and `FiscalYear(Today())` is 2025-26. At the fixture clock (`2026-10-24T05:00:00.000Z`, 10:30 IST), `Today()` returns 24-10-2026 and the FY is 2026-27.
- **Go test:** `TestDate_TodayInIST`
- **Sources:** `frappe/frappe/utils/data.py:436` (`nowdate` in system time zone)

### NUM-075 · Timestamps are stored as fixed-width UTC and shown in IST
- **Priority:** P1
- **Level:** unit
- **Given** the instant 01-04-2025 10:00 IST **When** stored **Then** the DB text is "2025-04-01T04:30:00.000Z", and `FormatIST` shows "01-04-2025 10:00". The fixture clock `2026-10-24T05:00:00.000Z` shows "24-10-2026 10:30". For 100 random instants, sorting the stored strings gives the same order as sorting the instants.
- **Go test:** `TestTime_StorageAndDisplay`
- **Sources:** BRIEF F7; `docs/tests/core/05-files-print.md` (FILE expects `FormatIST(t)` → `24-10-2026 15:42`)

### NUM-076 · Asia/Kolkata works without a system time-zone database
- **Priority:** P1
- **Level:** integration
- **Given** the binary run with `ZONEINFO` pointing at an empty directory and no `/usr/share/zoneinfo` **When** IST conversion runs **Then** it succeeds with offset +05:30 (from the embedded `time/tzdata`).
- **Go test:** `TestTime_EmbeddedTZData`
- **Sources:** `frappe/frappe/tests/test_utils.py:1254` (`test_get_timezone_utc_offset`, "+05:30")

### NUM-077 · Dates display as dd-mm-yyyy
- **Priority:** P1
- **Level:** unit
- **Given** the date 2025-04-01 **When** `FormatDate` runs **Then** it returns "01-04-2025".
- **Go test:** `TestDate_Format`
- **Sources:** `frappe/frappe/geo/doctype/country/country.py:71` (default "dd-mm-yyyy")

### NUM-078 · Accepted date inputs, always day-first
- **Priority:** P1
- **Level:** unit
- **Given** "01-04-2025", "1-4-2025", "01/04/2025", "01.04.2025", "2025-04-01", "01-Apr-2025" and "01-apr-2025" **When** parsed **Then** each gives 2025-04-01. "04/05/2025" gives 4 May 2025.
- **Go test:** `TestDate_ParseAccepted`
- **Sources:** `frappe/frappe/utils/dateutils.py:21-32` (`dateformats`), `frappe/frappe/tests/test_utils.py:176` (`test_date_time`, "01-04-2022" day-first)

### NUM-079 · Rejected date inputs
- **Priority:** P1
- **Level:** unit
- **Given** "31-02-2025", "29-02-2025", "29-02-2100", "00-01-2025", "2025-13-01", "01-04-25", "45748" (Excel serial) and "04/31/2025" **When** parsed **Then** each fails with "<input> is not a valid date. Use dd-mm-yyyy, for example 31-03-2025." "29-02-2024" and "29-02-2000" are accepted.
- **Go test:** `TestDate_ParseRejected`
- **Sources:** `frappe/frappe/utils/data.py:119-148` (`getdate`, "is not a valid date string"), `frappe/frappe/core/doctype/data_import/importer.py:1652`

### NUM-080 · Year range guard
- **Priority:** P1
- **Level:** unit
- **Given** "01-04-0025" or "01-04-2125" **When** parsed **Then** it fails with "Year must be between 1900 and 2099."
- **Go test:** `TestDate_YearRange`
- **Sources:** `frappe/frappe/utils/data.py:109-114` (`is_invalid_date_string` for "0001-01-01")

### NUM-081 · Adding months clamps to the month end
- **Priority:** P1
- **Level:** unit
- **Given** these additions **When** `AddMonths` runs **Then**: 31-01-2024 +1 → 29-02-2024; 31-01-2025 +1 → 28-02-2025; 30-11-2025 +3 → 28-02-2026; 31-03-2025 +1 → 30-04-2025; 31-08-2025 −6 → 28-02-2025; 15-06-2025 +12 → 15-06-2026.
- **Go test:** `TestDate_AddMonthsClamp`
- **Sources:** `frappe/frappe/utils/data.py:312`, `:330` (`relativedelta`)

### NUM-082 · Adding days across months, leap days and FY ends
- **Priority:** P1
- **Level:** unit
- **Given** these additions **When** `AddDays` runs **Then**: 25-02-2024 +5 → 01-03-2024; 25-02-2025 +5 → 02-03-2025; 30-03-2025 +30 → 29-04-2025; 01-04-2025 −1 → 31-03-2025. `DaysBetween(01-04-2025, 31-03-2026)` = 364.
- **Go test:** `TestDate_AddDays`
- **Sources:** `frappe/frappe/utils/data.py:325` (`add_days`), `:340` (`date_diff`)

### NUM-083 · Month-end helpers
- **Priority:** P1
- **Level:** unit
- **Given** 10-02-2024, 10-02-2025 and 24-12-2020 **When** `LastDayOfMonth` runs **Then** the results are 29-02-2024, 28-02-2025 and 31-12-2020. `IsMonthEnd(31-12-2020)` is true and `IsMonthEnd(24-12-2020)` is false.
- **Go test:** `TestDate_MonthEnd`
- **Sources:** `frappe/frappe/utils/data.py:588-599`, `frappe/frappe/tests/test_utils.py:1250` (`test_is_last_day_of_the_month`)

### NUM-084 · Due dates from payment terms
- **Priority:** P2
- **Level:** unit
- **Given** bill date 15-01-2026 **When** due dates are computed **Then** "30 days after bill date" → 14-02-2026, "15 days after end of bill month" → 15-02-2026, and "1 month after end of bill month" → 28-02-2026. A due date before the bill date fails with "Due date can't be before the bill date."
- **Go test:** `TestDate_DueDateRules`
- **Sources:** `erpnext/erpnext/accounts/party.py:753-770` (`get_due_date_from_template`), `erpnext/erpnext/accounts/party.py:773-781` (`validate_due_date`)

### NUM-085 · Date range presets use IST and the April FY
- **Priority:** P2
- **Level:** unit
- **Given** today 24-10-2026 (IST) **When** presets are resolved **Then** "This FY" = 01-04-2026 to 31-03-2027, "Last FY" = 01-04-2025 to 31-03-2026, "This month" = 01-10-2026 to 31-10-2026 and "Last 30 days" = 25-09-2026 to 24-10-2026.
- **Go test:** `TestDate_RangePresets`
- **Sources:** `frappe/frappe/tests/test_utils.py:1318` (`test_get_timespan_date_range`)

### NUM-086 · FY quarters start in April
- **Priority:** P2
- **Level:** unit
- **Given** 15-07-2025 and 10-02-2026 **When** `FiscalQuarter` runs **Then** the results are "Q2 2025-26" (Jul to Sep) and "Q4 2025-26" (Jan to Mar).
- **Go test:** `TestFiscalYear_Quarter`
- **Sources:** `frappe/frappe/utils/data.py:510` (`get_quarter_start`, calendar quarters; FY offset is ours)

### F. List queries

### NUM-100 · The equals filter
- **Priority:** P1
- **Level:** integration
- **Given** POs in states draft (3), pending_approval (2) and approved (5) **When** the list is filtered `status = approved` **Then** 5 rows and count 5 come back, numbered `PO/26-27/00001` to `PO/26-27/00005`. In the unfiltered list, the draft and pending_approval rows have no number and show `Draft {id}` (e.g. `Draft 42`, from `naming.Label`, CD-59) in the number column (spec R1).
- **Go test:** `TestList_FilterEquals`
- **Sources:** `frappe/frappe/tests/test_db_query.py:507` (`test_filters_4`)

### NUM-101 · "Not equals" includes empty values
- **Priority:** P1
- **Level:** integration
- **Given** 4 expenses with project `PRJ-METRO`, 2 with project `PRJ-TECHPARK` and 3 with no project **When** filtered `project != PRJ-TECHPARK` **Then** 7 rows come back (the 3 empty ones included).
- **Go test:** `TestList_NotEqualsIncludesEmpty`
- **Sources:** `frappe/frappe/tests/test_db_query.py:317` (`test_child_table_filter_matches_parents_without_child_rows`, `!=` includes empty), `frappe/frappe/model/db_query.py:1093-1137` (ifnull fallback)

### NUM-102 · Contains is a case-insensitive substring match with literal wildcards
- **Priority:** P1
- **Level:** integration
- **Given** the fixture parties plus an extra party `STEELCO` "STEELCO Traders" created in this test, and notes "50% advance" and "500 units" **When** filtered `name contains "steel"` **Then** only "Shree Steel Traders" (`SHREE`) and "STEELCO Traders" come back. `remarks contains "50%"` returns only "50% advance". `contains "a_b"` matches "a_b" but not "axb".
- **Go test:** `TestList_ContainsLiteral`
- **Sources:** `frappe/frappe/model/db_query.py:1100-1102` (escape `\` and `%`), `frappe/frappe/tests/test_db_query.py:480` (`test_filters_1`)

### NUM-103 · In and not in, including empty lists and values with commas
- **Priority:** P1
- **Level:** integration
- **Given** POs as in NUM-100 **When** filtered `status in [draft, pending_approval]` **Then** 5 rows come back. `in []` returns 0 rows and `not in []` returns all 10. For an extra party `SHAHRAO` named "Shah, Rao & Co", created in this test, `name in ["Shah, Rao & Co"]` returns that party (the value is not split at the comma).
- **Go test:** `TestList_InNotIn`
- **Sources:** `frappe/frappe/tests/test_db_query.py:512` (`test_in_not_in_filters`), `:528` (`test_in_filter_json_encoded_values`), `:1683` (`test_coalesce_with_in_ops`)

### NUM-104 · Date between is inclusive and needs both ends
- **Priority:** P1
- **Level:** integration
- **Given** POs dated 31-03-2026, 01-04-2026, 30-04-2026 and 01-05-2026 **When** filtered `date between 01-04-2026 and 30-04-2026` **Then** the middle two come back. Start 30-04-2026 with end 01-04-2026 fails with "The start date must be on or before the end date." Only one end fails with "Choose both a start and an end date." (open-ended ranges use "on or after" / "on or before").
- **Go test:** `TestList_DateBetween`
- **Sources:** `frappe/frappe/tests/test_db_query.py:577` (`test_between_filters`), `:621` (`test_between_filters_date_bounds`)

### NUM-105 · Date filters on timestamps cover whole IST days
- **Priority:** P1
- **Level:** integration
- **Given** records created at 2026-03-31T18:15:00Z (31-03 23:45 IST), 2026-03-31T18:45:00Z (01-04 00:15 IST) and 2026-04-01T18:29:59Z (01-04 23:59:59 IST) **When** filtered `created between 01-04-2026 and 01-04-2026` **Then** the last two come back. The SQL bounds are `>= '2026-03-31T18:30:00.000Z' AND < '2026-04-01T18:30:00.000Z'`.
- **Go test:** `TestList_TimestampDayRangeIST`
- **Sources:** `frappe/frappe/tests/test_db_query.py:621-656` (single date covers the entire day)

### NUM-106 · Money comparisons are exact integer comparisons
- **Priority:** P1
- **Level:** integration
- **Given** POs with grand totals ₹99,999.99, ₹1,00,000.00 and ₹1,00,000.01 **When** filtered `grand_total >= "1,00,000"` **Then** the last two come back. The filter value goes through the NUM-056 parser, and "1,00,000.005" fails with "Amount can have at most 2 decimal places."
- **Go test:** `TestList_MoneyComparison`
- **Sources:** `frappe/frappe/tests/test_db_query.py:1704` (`test_coalesce_with_datetime_ops`, comparisons without fallback)

### NUM-107 · "Is empty" and "is not empty"
- **Priority:** P1
- **Level:** integration
- **Given** the 5 fixture parties (4 with a GSTIN: `SHREE`, `BHARAT`, `RAJKOT`, `COROM`; `LOCAL` with GSTIN NULL), plus 2 extra unregistered parties created in this test (one with GSTIN NULL, one with ''), and expenses with amount 0.00 (1) **When** filtered `gstin is empty` **Then** 3 rows come back, and `gstin is not empty` returns 4. `amount is empty` does not match the 0.00 expense.
- **Go test:** `TestList_IsEmpty`
- **Sources:** `frappe/frappe/tests/test_db_query.py:1177` (`test_is_set_is_not_set`), `frappe/frappe/tests/test_db_query.py:560` (`test_none_filter`)

### NUM-108 · Filters and search combine with AND
- **Priority:** P1
- **Level:** integration
- **Given** 10 approved and 5 draft POs, of which 3 approved and 2 draft are from `SHREE` Shree Steel Traders **When** filtered `status = approved` with search "shree" **Then** 3 rows come back with count 3.
- **Go test:** `TestList_FiltersAndSearchAreANDed`
- **Sources:** `frappe/frappe/tests/test_db_query.py:566` (`test_or_filters`, OR groups deferred)

### NUM-109 · Unknown fields and unsupported operators are rejected
- **Priority:** P1
- **Level:** integration
- **Given** a list request **When** it filters on `password_hash` or `grand_total; drop table party` **Then** it fails with HTTP 422 (validation, CD-44) "You can't filter Purchase Orders by 'password_hash'." `contains` on a money field fails with "'contains' can't be used on Grand Total. Use =, <, >, between."
- **Go test:** `TestList_RejectUnknownFieldOrOperator`
- **Sources:** `frappe/frappe/tests/test_db_query.py:988` (`test_filter_sanitizer`)

### NUM-110 · Filter values are always bound parameters
- **Priority:** P1
- **Level:** integration
- **Given** search text `'; DROP TABLE party;--` and filter `name = "x' OR '1'='1"` **When** the list runs **Then** 0 rows come back and the `party` table still exists with all rows.
- **Go test:** `TestList_NoSQLInjection`
- **Sources:** `frappe/frappe/tests/test_db_query.py:1628` (`test_permission_criterion_escapes_values_safely`)

### NUM-111 · Sort accepts only declared fields and asc/desc
- **Priority:** P1
- **Level:** integration
- **Given** a list request **When** sort is `sleep(1) asc`, `grand_total sideways` or an undeclared field **Then** it fails with HTTP 422 (validation, CD-44) "You can't sort Purchase Orders by 'sleep(1)'." Sort `grand_total desc` works.
- **Go test:** `TestList_SortWhitelist`
- **Sources:** `frappe/frappe/tests/test_db_query.py:1064` (`test_order_by_group_by_sanitizer`)

### NUM-112 · Default sort per DocType
- **Priority:** P1
- **Level:** integration
- **Given** POs and parties with no sort requested **When** listed **Then** POs come by date desc then id desc, and parties by name asc (case-insensitive) then id asc.
- **Go test:** `TestList_DefaultSort`
- **Sources:** `frappe/frappe/model/db_query.py:1356-1373` (sort_field or `creation desc`)

### NUM-113 · Pagination is stable when sort values tie
- **Priority:** P1
- **Level:** integration
- **Given** 60 POs with the same date **When** pages 1, 2 and 3 of size 25 are fetched sorted by date **Then** they hold 25, 25 and 10 rows, each PO id appears exactly once across the pages, and fetching page 2 twice gives the same rows.
- **Go test:** `TestList_StablePagination`
- **Sources:** `frappe/frappe/model/db_query.py:1433-1434` (limit/offset); D-NUM-20

### NUM-114 · Numbers and dates sort by value, not as text
- **Priority:** P1
- **Level:** integration
- **Given** grand totals ₹9,999.00, ₹1,00,000.00 and ₹25,000.00, and dates 02-01-2026 and 31-12-2025 **When** sorted ascending **Then** the totals order is 9,999 → 25,000 → 1,00,000 and the dates order is 31-12-2025 → 02-01-2026.
- **Go test:** `TestList_SortByValue`
- **Sources:** D-NUM-01, D-NUM-18

### NUM-115 · Empty values sort last in both directions
- **Priority:** P1
- **Level:** integration
- **Given** expenses with project `PRJ-METRO`, `PRJ-TECHPARK` and none **When** sorted by project asc, then desc **Then** the orders are PRJ-METRO, PRJ-TECHPARK, (none) and PRJ-TECHPARK, PRJ-METRO, (none).
- **Go test:** `TestList_EmptyLast`
- **Sources:** D-NUM-20 (SQLite `NULLS LAST`)

### NUM-116 · Text sorts case-insensitively
- **Priority:** P1
- **Level:** integration
- **Given** `BHARAT` "Bharat Tools" and two extra parties created in this test, "apex Fabricators" and "chennai Traders" **When** sorted by name asc **Then** the order is apex, Bharat, chennai.
- **Go test:** `TestList_TextSortNoCase`
- **Sources:** D-NUM-20

### NUM-117 · Page size and page number validation
- **Priority:** P1
- **Level:** integration
- **Given** a list request **When** page size is omitted **Then** it is 25. Size 50/100/500 is honoured. Size 1000 fails with HTTP 422 (validation, CD-44) "Page size must be 25, 50, 100 or 500." Page 0 fails with "Page must be 1 or more." A page past the end returns 0 rows with the correct total, and the UI shows "No more results."
- **Go test:** `TestList_PageSizeRules`
- **Sources:** `frappe/frappe/model/db_query.py:175-198` (`page_length`/`limit`)

### NUM-118 · The count matches the filters, not the page
- **Priority:** P1
- **Level:** integration
- **Given** 61 POs matching `status = approved` **When** page 2 of size 25 is fetched **Then** 25 rows come back with total 61, and the UI shows "Showing 26–50 of 61".
- **Go test:** `TestList_CountIndependentOfPage`
- **Sources:** `frappe/frappe/desk/reportview.py:88-125` (`get_count`), `frappe/frappe/tests/test_db_query.py:1869` (`test_get_count`)

### NUM-119 · Search covers code and name for masters, number and party for documents
- **Priority:** P1
- **Level:** integration
- **Given** the fixture parties, approved PO `PO/26-27/00042` from `SHREE` Shree Steel Traders, and a draft PO from `BHARAT` shown as `Draft 43` **When** searching parties for "  local " **Then** `LOCAL` (Ganesh Hardware) comes back by its code. Searching "cables" returns `COROM` (Coromandel Cables) by its name. Searching POs for "00042" or "shree" returns `PO/26-27/00042`. The draft has no number yet (spec R1), so "00042" doesn't return it, but "bharat" does. Empty search applies no filter.
- **Go test:** `TestList_SearchFields`
- **Sources:** `frappe/frappe/desk/search.py:286-298` (`search_widget`)

### NUM-120 · Search treats special characters literally
- **Priority:** P1
- **Level:** integration
- **Given** the fixture items plus three extra items created in this test, "Bolt 10%", "Bolt 100" and "A\B Plate" **When** searching "10%" **Then** only "Bolt 10%" comes back. Searching "\" returns "A\B Plate". Searching "_" returns only names containing "_".
- **Go test:** `TestList_SearchLiteralChars`
- **Sources:** `frappe/frappe/desk/search.py:327` (strips `%`), `frappe/frappe/model/db_query.py:1100-1102`

### NUM-121 · The permission predicate is applied to rows, counts, search and filters
- **Priority:** P1
- **Level:** integration
- **Given** Priya (`priya@vardhan.test`), restricted to project `PRJ-METRO`, and 20 approved POs: 8 for `PRJ-METRO` and 12 for `PRJ-TECHPARK` **When** she lists POs **Then** 8 rows come back with count 8. Search "PO/" returns only the `PRJ-METRO` POs. Filter `project = PRJ-TECHPARK` returns 0 rows with count 0 (no error that would reveal `PRJ-TECHPARK` data). The totals row (NUM-126) sums only `PRJ-METRO`. Likewise Suresh (`suresh@vardhan.test`), restricted to sites `PUNE-WH` and `METRO-SITE`, sees only GRNs for those two sites and none for `TECH-SITE`.
- **Go test:** `TestList_PermissionPredicateAlwaysApplied`
- **Sources:** `frappe/frappe/tests/test_db_query.py:439` (`test_build_match_conditions`), `frappe/frappe/tests/test_db_query.py:1570` (`test_permission_query_condition`)

### NUM-122 · No read right means no list
- **Priority:** P1
- **Level:** integration
- **Given** Deepak (`deepak@vardhan.test`, Employee only), who has no read right on Purchase Order **When** he opens the PO list or calls its count **Then** both return HTTP 403 "You don't have permission to read Purchase Order. Ask your administrator for a role that allows it." (AUTH's wording).
- **Go test:** `TestList_NoReadRight`
- **Sources:** `frappe/frappe/tests/test_db_query.py:658` (`test_comments_blob_not_readable`, PermissionError)

### NUM-123 · Disabled masters are hidden from pickers by default
- **Priority:** P1
- **Level:** integration
- **Given** the 5 active fixture parties and 2 extra parties created and disabled in this test **When** the party link picker searches "" **Then** 5 parties come back. On the party list, the default filter shows 5, and turning on "Show disabled" shows 7 with disabled ones marked "Disabled".
- **Go test:** `TestList_DisabledHiddenInPickers`
- **Sources:** BRIEF F4; `frappe/frappe/desk/search.py:151` (`search_widget`)

### NUM-124 · Filters on lines match within a single line
- **Priority:** P2
- **Level:** integration
- **Given** `PO/26-27/00001` with lines (item `MS-ANGLE-50`, qty 5) and (`WIRE-18G`, qty 50), and `PO/26-27/00002` with line (`MS-ANGLE-50`, qty 50) **When** filtered `line.item = MS-ANGLE-50 AND line.qty >= 10` **Then** only `PO/26-27/00002` comes back, once. `line.item = MS-ANGLE-50` alone returns `PO/26-27/00001` and `PO/26-27/00002`, each once.
- **Go test:** `TestList_ChildLineFilterSameRow`
- **Sources:** `frappe/frappe/tests/test_db_query.py:337` (`test_child_table_filters_match_same_child_row`), `:380` (`test_child_table_filter_returns_one_row_per_matching_child`)

### NUM-125 · List state lives in the URL
- **Priority:** P2
- **Level:** e2e
- **Given** the PO list **When** the user filters status = approved, searches "steel", sorts by grand total desc and goes to page 2 **Then** the URL holds those parameters. Reloading, using browser Back, or opening the URL as another permitted user shows the same list.
- **Go test:** `TestE2E_ListStateInURL`
- **Sources:** `frappe/frappe/desk/reportview.py:385-395` (form params)

### NUM-126 · The totals row sums all matching rows
- **Priority:** P2
- **Level:** integration
- **Given** 61 permitted POs matching the filter, with a combined grand total of ₹14,12,345.60 **When** page 1 is shown **Then** the totals row shows "₹14,12,345.60" for all 61 rows, not the first 25.
- **Go test:** `TestList_TotalsRowAllMatching`
- **Sources:** `frappe/frappe/desk/reportview.py:88` (separate aggregate query pattern)

### NUM-127 · List speed at realistic size
- **Priority:** P2
- **Level:** integration
- **Given** 50,000 POs and 2,00,000 lines with indexes on (date, id), (status, date, id) and (party_id) **When** page 1 with a status filter and count is fetched **Then** it finishes in under 200 ms on the test runner.
- **Go test:** `BenchmarkList_POPage`
- **Sources:** `frappe/frappe/desk/reportview.py:111-118` (count timeout concern)

### G. CSV import and export

### NUM-140 · The import template lists the importable fields
- **Priority:** P1
- **Level:** integration
- **Given** Anil Kapoor (`admin@vardhan.test`, Administrator) on the Item import page **When** he downloads the template **Then** he gets a UTF-8 (with BOM) CSV whose first row is "Code,Name,Type,UoM,HSN/SAC,Tax Code,Standard Rate,Disabled" (CD-37), with no data rows. A help line on the page lists the required columns (Code, Name, Type, UoM).
- **Go test:** `TestImport_Template`
- **Sources:** `frappe/frappe/core/doctype/data_import/test_exporter.py:39` (`test_exports_specified_fields`)

### NUM-141 · Headers map by label or field name, ignoring case, spaces and the BOM
- **Priority:** P1
- **Level:** unit
- **Given** first-row headers "﻿code", "  ITEM NAME ", "uom" and "hsn_sac" **When** mapped for Item **Then** they map to Code, Name, UoM and HSN/SAC.
- **Go test:** `TestImport_HeaderMapping`
- **Sources:** `frappe/frappe/core/doctype/data_import/test_importer.py:914` (`test_data_import_without_label`), `:941` (`test_import_maps_headers_in_user_language`)

### NUM-142 · Missing required or duplicate columns reject the file before any row is read
- **Priority:** P1
- **Level:** unit
- **Given** a party file without a Name column **When** previewed **Then** it fails with "Column 'Name' is missing. Add it to the first row." A file with "GSTIN" twice fails with "Column 'GSTIN' appears twice. Keep one."
- **Go test:** `TestImport_RequiredAndDuplicateColumns`
- **Sources:** `frappe/frappe/core/doctype/data_import/test_importer.py:228` (`test_file_with_no_column_for_the_record_itself_still_saves`)

### NUM-143 · Unknown columns are ignored with a warning
- **Priority:** P1
- **Level:** unit
- **Given** an item file with an extra column "Colour" **When** previewed **Then** the preview shows the warning "Column 'Colour' isn't a field of Item and will be ignored." and the import can go ahead.
- **Go test:** `TestImport_UnknownColumnWarning`
- **Sources:** `frappe/frappe/core/doctype/data_import/test_data_import.py:44` (`test_invalid_column_mapping_still_warns`)

### NUM-144 · Preview is a dry run that writes nothing
- **Priority:** P1
- **Level:** integration
- **Given** a 120-row party file with errors on rows 7 and 58 **When** previewed **Then** it shows "118 rows are ready, 2 rows have errors" with each error and its spreadsheet row number (header is row 1). The party count, audit trail and naming counters don't change.
- **Go test:** `TestImport_DryRunWritesNothing`
- **Sources:** `frappe/frappe/core/doctype/data_import/test_importer.py:220` (`test_data_import_preview`), `frappe/frappe/core/doctype/data_import/importer.py:937-943`

### NUM-145 · Import is all-or-nothing
- **Priority:** P1
- **Level:** integration
- **Given** a 100-row party file where row 57's GSTIN fails MST's check digit **When** the user clicks Import **Then** 0 parties are created and the result says "Nothing was imported. Fix 1 row and upload again." with "Row 57: GSTIN 27AABCS1429B1Z5 is not valid: the last character doesn't match. Check for typing mistakes." (MST-031's text with the row prefix, CD-38). After the fix, all 100 are created in one transaction, each with an audit entry "created by import".
- **Go test:** `TestImport_AllOrNothing`
- **Sources:** `frappe/frappe/core/doctype/data_import/importer.py:345-358` (per-row commit; not followed, D-NUM-23)

### NUM-146 · Every row goes through the same validation as the form, and all errors are listed
- **Priority:** P1
- **Level:** integration
- **Given** an item file where rows 2 and 3 have no Name and row 4 has UoM blank **When** previewed **Then** the errors are "Row 2: Name is required.", "Row 3: Name is required." and "Row 4: UoM is required." (all reported, not only the first).
- **Go test:** `TestImport_RowValidationAllErrors`
- **Sources:** `frappe/frappe/core/doctype/data_import/test_importer.py:251` (`test_data_import_without_mandatory_values`)

### NUM-147 · Duplicate codes in the file or the database
- **Priority:** P1
- **Level:** integration
- **Given** a party file where rows 5 and 9 both have the new code `KAVERI`, and row 11 has code `SHREE` (already in the DB) **When** previewed **Then** the errors are "Row 9: Code KAVERI is repeated (also on row 5)." and "Row 11: Code SHREE already exists."
- **Go test:** `TestImport_DuplicateCodes`
- **Sources:** `frappe/frappe/core/doctype/data_import/test_importer.py:386` (`test_data_import_upsert_unchanged_existing_record`)

### NUM-148 · Link columns resolve by code and must be active
- **Priority:** P1
- **Level:** integration
- **Given** an item file with UoM "Kgs" on row 6 (no such code) and "Box" on row 8 (the fixture UoM, disabled in this test) **When** previewed **Then** the errors are "Row 6: UoM 'Kgs' not found. Use an existing UoM code such as kg." and "Row 8: UoM 'Box' is disabled. Enable it or use another UoM."
- **Go test:** `TestImport_LinkByCode`
- **Sources:** `frappe/frappe/core/doctype/data_import/test_importer.py:729` (`test_user_without_create_permission_gets_the_link_error`), `:887` (`test_link_and_select_warnings_include_row_numbers`)

### NUM-149 · Numbers in CSV use the same parser as the form
- **Priority:** P1
- **Level:** integration
- **Given** an item file with the NUM-140 template columns and Standard Rate (Item's optional `standard_rate`, CD-37) "1,41,120.50" (quoted), "141120.5" and "1.234,56" on rows 2-4 **When** previewed **Then** rows 2 and 3 parse to ₹1,41,120.50, and row 4 fails with "Row 4, column 'Standard Rate': \"1.234,56\" is not a valid number. Write it like 1234.50." "12.12345" fails with "Row 5, column 'Standard Rate': Rate can have at most 4 decimal places."
- **Go test:** `TestImport_NumbersIndianFormat`
- **Sources:** `frappe/frappe/core/doctype/data_import/test_importer.py:1156` (`test_import_reads_numbers_in_user_number_format`), `:1162` (`test_import_blocks_numbers_in_other_format`)

### NUM-150 · Dates in CSV are read day-first
- **Priority:** P1
- **Level:** integration
- **Given** an employee file with Date of Joining "01-04-2025", "1/4/2025", "2025-04-01", "01-Apr-2025", "04/05/2025" and "31-02-2025" on rows 2-7 **When** previewed **Then** rows 2-5 give 2025-04-01 and row 6 gives 2025-05-04. Row 7 fails with "Row 7, column 'Date of Joining': 31-02-2025 is not a valid date. Use dd-mm-yyyy, for example 31-03-2025."
- **Go test:** `TestImport_DatesDayFirst`
- **Sources:** `frappe/frappe/core/doctype/data_import/importer.py:1943-1984` (`guess_date_format_for_column`), `:1652`

### NUM-151 · UTF-8 with or without BOM is accepted; anything else is rejected
- **Priority:** P1
- **Level:** integration
- **Given** the same party file saved as Excel "CSV UTF-8" (with BOM) and as plain UTF-8 **When** previewed **Then** both work, and "Śrī Gaṇeśa Traders" and "₹" are kept exactly. A Windows-1252 file containing byte 0xE9 fails with "This file isn't saved as UTF-8. In Excel choose Save As → CSV UTF-8 (Comma delimited) and upload again."
- **Go test:** `TestImport_EncodingUTF8Only`
- **Sources:** `frappe/frappe/core/doctype/file/file.py:48` (`FILE_ENCODING_OPTIONS`), `frappe/frappe/utils/csvutils.py:70-86`

### NUM-152 · CSV mechanics
- **Priority:** P1
- **Level:** unit
- **Given** a file with CRLF line endings, a quoted name "Shah, Rao & Co", an address with an embedded newline, doubled quotes ("12"" pipe"), cells with surrounding spaces and 3 trailing blank rows **When** parsed **Then** the values are read correctly and trimmed, and blank rows are skipped. A row with 9 cells under an 8-column header fails with "Row 12 has 9 values but the header has 8. Check for an unquoted comma."
- **Go test:** `TestImport_CSVMechanics`
- **Sources:** `frappe/frappe/core/doctype/data_import/test_importer.py:189` (`test_data_validation_semicolon_success`, "child description with ,comma and")

### NUM-153 · Size limits and empty files
- **Priority:** P1
- **Level:** unit
- **Given** a file with 6,200 data rows **When** uploaded **Then** it fails with "This file has 6,200 rows. Import at most 5,000 rows at a time." A file over 5 MB fails with "This file is larger than 5 MB. Split it into smaller files." A header-only file fails with "The file has no rows to import."
- **Go test:** `TestImport_Limits`
- **Sources:** `frappe/frappe/core/doctype/data_import/importer.py:28` (`MAX_ROWS_IN_PREVIEW`; no hard cap in Frappe)

### NUM-154 · Text keeps leading zeros and long digit strings
- **Priority:** P1
- **Level:** unit
- **Given** cells "00123" (Code), "400001" (PIN) and "09876543210" (Phone) **When** imported **Then** they are stored exactly as text. A GSTIN cell "2.7E+14" (Excel's scientific mangling) fails with MST's GSTIN error (here MST-030's "GSTIN 2.7E+14 must have 15 characters.") plus the hint "Excel may have changed this value. Format the column as Text and retype it."
- **Go test:** `TestImport_TextPreserved`
- **Sources:** `frappe/frappe/tests/test_utils.py:1574` (`test_export_reimport_round_trip_preserves_legit_data`, "+1-555-0100")

### NUM-155 · The formula-escape apostrophe is removed on import
- **Priority:** P1
- **Level:** unit
- **Given** text cells "'=SUM(A1)", "'+91 98765 43210" and "'hello" **When** imported **Then** they are stored as "=SUM(A1)", "+91 98765 43210" and "'hello". Only an apostrophe before a formula-trigger character is removed.
- **Go test:** `TestImport_UnescapeFormula`
- **Sources:** `frappe/frappe/utils/csvutils.py:24-37` (`unescape_formula_injection`), `frappe/frappe/tests/test_utils.py:1549` (`test_unescape_reverses_escape`), `:1560` (`test_unescape_does_not_strip_literal_leading_quote`)

### NUM-156 · Import needs the import right, plus create on every row
- **Priority:** P1
- **Level:** integration
- **Given** Priya (`priya@vardhan.test`) with Item read and create but not import **When** she opens the Item import page or posts a file **Then** both return HTTP 403 "You don't have permission to import Item. Ask your administrator for a role that allows it." (AUTH's wording, AUTH-081). **When** a user with import but not create posts the file **Then** nothing is imported and every row is listed with "Row 2: you don't have permission to create Item." (and so on per row).
- **Go test:** `TestImport_NeedsImportRight`
- **Sources:** `frappe/frappe/core/doctype/data_import/test_importer.py:685` (`test_create_field_needs_create_permission`), `frappe/frappe/core/doctype/data_import/data_import.py:179` (import permission), `docs/tests/core/01-auth.md` (AUTH-081)

### NUM-157 · A semicolon-separated file gets a helpful error
- **Priority:** P2
- **Level:** unit
- **Given** a file whose first row is "Code;Name;UoM" **When** previewed **Then** it fails with "This file seems to use ';' between values. Save it as CSV (comma separated) and upload again."
- **Go test:** `TestImport_SemicolonHint`
- **Sources:** `frappe/frappe/core/doctype/data_import/test_importer.py:211` (`test_data_validation_semicolon_failure`)

### NUM-158 · Rows with errors can be downloaded
- **Priority:** P2
- **Level:** integration
- **Given** a preview with errors on rows 7 and 58 **When** the user clicks "Download rows with errors" **Then** they get a UTF-8 BOM CSV with the original header plus an "Error" column, holding only those 2 rows with their messages.
- **Go test:** `TestImport_ErrorRowsDownload`
- **Sources:** `frappe/frappe/core/doctype/data_import/test_importer.py:808` (`test_skipped_rows_export_reads_cells_from_the_file`)

### NUM-159 · Export uses the list's filters, search, sort and permissions
- **Priority:** P1
- **Level:** integration
- **Given** Priya (`priya@vardhan.test`, restricted to project `PRJ-METRO`, with PO export right) viewing POs filtered `status = approved`, searched "steel" and sorted by grand total desc, with 61 matching rows **When** she exports **Then** the CSV has 1 header row and 61 data rows in the same order, all for `PRJ-METRO`, not only the 25 on screen.
- **Go test:** `TestExport_MatchesListAndPermissions`
- **Sources:** `frappe/frappe/core/doctype/data_import/test_exporter.py:126` (`test_export_respects_child_field_permissions`)

### NUM-160 · Export needs the export right
- **Priority:** P1
- **Level:** integration
- **Given** Suresh (`suresh@vardhan.test`, Stores) with read but not export rights on Purchase Order **When** he requests the export **Then** he gets HTTP 403 "You don't have permission to export Purchase Order. Ask your administrator for a role that allows it." (AUTH's wording, AUTH-080). The export button is hidden for him.
- **Go test:** `TestExport_NeedsExportRight`
- **Sources:** `frappe/frappe/core/doctype/data_import/test_exporter.py:146-147` (`export` permission)

### NUM-161 · Export value formats
- **Priority:** P1
- **Level:** integration
- **Given** a PO with grand total ₹1,41,12,000.00, qty 2.5, rate ₹12.345, GST 18%, date 01-04-2025 and created at 2025-04-01T04:30:00.000Z **When** exported **Then** the cells are "14112000.00", "2.5", "12.345", "18", "2025-04-01" and "2025-04-01 10:00:00" (IST). The file starts with the UTF-8 BOM and the header uses field labels.
- **Go test:** `TestExport_ValueFormats`
- **Sources:** `frappe/frappe/core/doctype/data_import/test_exporter.py:113` (`test_export_csv_response`), D-NUM-26

### NUM-162 · Formula injection is escaped in text cells only
- **Priority:** P1
- **Level:** unit
- **Given** remarks "=HYPERLINK(\"http://x\")", "+91 98765 43210", "-5 units" and "@SUM(1)", and a numeric rounding adjustment of −0.49 **When** exported **Then** the text cells start with "'" ("'=HYPERLINK…", "'+91 98765 43210", "'-5 units", "'@SUM(1)") and the number cell is "-0.49" without an apostrophe.
- **Go test:** `TestExport_FormulaEscapeTextOnly`
- **Sources:** `frappe/frappe/utils/csvutils.py:13-21`, `frappe/frappe/tests/test_utils.py:1534` (`test_escape_formula_injection_prefixes_trigger_chars`), `frappe/frappe/tests/test_utils.py:1495` (`test_formula_like_strings_are_not_written_as_formulas`, numeric cells keep native type)

### NUM-163 · Export then import round-trips masters
- **Priority:** P1
- **Level:** integration
- **Given** 50 parties with GSTINs, PINs with leading zeros, Hindi and accented names, and remarks starting with "=" **When** exported, then imported into an empty database **Then** every imported field equals the original. Exporting again gives a byte-identical file (apart from the timestamp column).
- **Go test:** `TestExportImport_RoundTrip`
- **Sources:** `frappe/frappe/tests/test_utils.py:1574` (`test_export_reimport_round_trip_preserves_legit_data`)

### NUM-164 · Export size limit
- **Priority:** P2
- **Level:** integration
- **Given** 62,000 matching rows **When** export is requested **Then** it fails with "Too many rows to export (62,000). Narrow the filters to 50,000 rows or fewer." At 50,000 rows the export streams and memory use stays under 100 MB.
- **Go test:** `TestExport_RowLimit`
- **Sources:** `frappe/frappe/desk/reportview.py:518` (export removes page length)

## Deferred and excluded
| Scenario | Source | later/never | Reason |
|---|---|---|---|
| Multi-currency amounts, exchange rates, base vs transaction currency | `erpnext/erpnext/controllers/taxes_and_totals.py:156`, `:297` | never (v1) / later | F2: INR only. |
| Configurable rounding method (banker's / commercial) | `frappe/frappe/core/doctype/system_settings/system_settings.json:555` | never | One rule (half-up) is simpler and matches Indian practice (D-NUM-07). |
| Configurable number/date format per user | `frappe/frappe/core/doctype/system_settings/system_settings.json:182-211` | never | Single Indian company. One format avoids mixed-format bugs. |
| Round GST components to whole rupees | `india-compliance/india_compliance/gst_india/overrides/transaction.py:766-781` | later | Zoho Books does the books. Revisit if Zoho bills don't match. |
| Per-document "disable rounded total" toggle | `erpnext/erpnext/selling/doctype/quotation/test_quotation.py:1330` | later | DocType-level flag only in v1 (D-NUM-11). |
| Document-level additional discount, discount on grand total | `erpnext/erpnext/controllers/taxes_and_totals.py:909-1012` | later | Not needed for PO/GRN/Expense v1. Needs distribution across lines. |
| Tax on previous row, actual/fixed charges, cess (ad valorem and per quantity) | `erpnext/erpnext/controllers/taxes_and_totals.py:654-698`; `india-compliance/.../transaction.py:477-500` | later | Only plain GST percentages on purchases in v1. Cess applies to few goods. |
| Reverse charge (RCM) amounts | `india-compliance/india_compliance/gst_india/overrides/transaction.py:1045-1115` | later | Booked in Zoho Books. Flag only when needed. |
| Imports, exports, overseas place of supply ("96-Other Countries") | `india-compliance/india_compliance/gst_india/utils/__init__.py:733-745`; `test_transaction.py:1383` | later | No import/export flows in v1. |
| Sales-side place of supply (ship-to state, B2C) | `india-compliance/india_compliance/gst_india/utils/__init__.py:664-705` | later | Arrives with the Sales Invoice / Delivery Challan modules. |
| Negative amounts on return/debit-note lines | `erpnext/erpnext/controllers/taxes_and_totals.py:235-238` | later | No returns in v1. The rounding functions are already sign-symmetric (NUM-003). |
| Fiscal Year table, short years, company-specific FY | `erpnext/erpnext/accounts/doctype/fiscal_year/test_fiscal_year.py:11-46` | never | F7 fixes April to March. Computed (D-NUM-16). |
| 2-digit years and month-first dates on input | `frappe/frappe/utils/data.py:2688-2698` | never | Ambiguous (D-NUM-15). |
| Saved filters / named list views | `frappe/frappe/model/db_query.py` (user settings) | later | Nice to have. URL state (NUM-125) covers sharing. |
| OR filter groups and nested conditions | `frappe/frappe/tests/test_db_query.py:566` (`test_or_filters`) | later | AND-only covers v1 lists. |
| Group-by / report builder aggregates | `frappe/frappe/tests/test_db_query.py:1251-1300` | later | Reports come with later modules. |
| Full-text search (SQLite FTS5) and multi-word token search | `frappe/frappe/tests/test_sqlite_search.py` | later | LIKE substring is enough at this data size. |
| Keyset (cursor) pagination | `frappe/frappe/model/db_query.py:1433` | later | Offset with an id tie-break is enough below about 1 lakh rows. |
| Approximate/timeout-bounded counts | `frappe/frappe/desk/reportview.py:111-125` | never (v1) | SQLite counts are fast at this scale. |
| Field-level (permlevel) column hiding in lists and exports | `frappe/frappe/tests/test_db_query.py:1322` (`test_permlevel_fields`); `test_exporter.py:126` | later | AUTH v1 has DocType and record rights, not field levels. |
| CSV update / upsert of existing records | `frappe/frappe/core/doctype/data_import/test_importer.py:293`, `:327` | later | Needs a change-review screen (D-NUM-25). |
| CSV import of documents with lines (PO, expense), tree imports | `frappe/frappe/core/doctype/data_import/test_importer.py:1422` | later | Documents carry approvals and naming. Masters first. |
| Auto-creating missing linked masters during import | `frappe/frappe/core/doctype/data_import/test_importer.py:693` | never | Typos would create junk masters. The user creates them first. |
| Windows-1250/1252 encoding fallback | `frappe/frappe/core/doctype/file/file.py:48` | never | Risk of silent garbling (D-NUM-24). |
| Custom delimiters / CSV sniffer | `frappe/frappe/core/doctype/data_import/test_importer.py:199` | never | Comma only. NUM-157 gives the fix. |
| XLSX import/export | `frappe/frappe/utils/xlsxutils.py` | later | CSV is enough for v1. Would need a library (e.g. `github.com/xuri/excelize/v2`, BSD-3-Clause). |
| Background (queued) import with progress events | `frappe/frappe/core/doctype/data_import/importer.py:313-330` | never (v1) | 5,000 rows import in one request in seconds. There are no live updates in v1. |
| Amount in words (Indian lakh/crore) | `frappe/frappe/utils/data.py:1538-1631` | n/a (FILE) | Owned by FILE. It uses NUM's payable total. |

## Interfaces this area needs from others
- **AUTH provides** `auth.Service.Can(ctx, p, doctype, r Right, rec *RecordRef) error` (returns `ErrForbidden` / `ErrNotFound`) for the rights `read`, `export`, `import` (once per import) and `create` (per imported row) (its errors implement `doctype.HTTPError`, CD-44; `ErrNotFound` is `doctype.ErrNotFound`, CD-54), and `auth.Service.Scope(ctx, p, doctype, r Right) (Predicate, error)`, where `Predicate{SQL string; Args []any}`. NUM passes that predicate to `list.Build(dt, q, scope)` and ANDs it into every list, count, search, totals row, link picker and export query. An empty predicate (`SQL == ""`) means "all rows". It must use bound args only, and its columns are qualified with the DocType's table name, reaching line tables through `EXISTS (… WHERE <lines>.parent_id = <table>.id …)`; `list.Build` never aliases the main table (CD-16). (Spec §5, R4.)
- **MST provides** for each branch: `StateCode` (from GSTIN digits 1-2, else the state field) and display name. For each party: GSTIN, GST category (Registered/Unregistered/SEZ) and billing-address `StateCode`. For each tax code: rate as `Percent` (millipercent) and HSN/SAC. For each UoM: `WholeNumber bool`. For each item: UoM conversion `Factor` (10^-9 scale). MST also provides GSTIN check-digit validation and the "disabled" flag used in link pickers and import.
- **DOC provides** each DocType's `doctype.DocType` registration in `internal/kernel/doctype` (CD-02: `Fields`, `SearchFields`, `DefaultSort`, `Importable`, `HasRoundedTotal`, `PluralLabel` for messages and list titles (CD-35), `LinesTable`, and `Master` lookups for link checks (CD-21)), with per-field `Filterable`, `Sortable`, `Required`, `Importable`, `OnLine` flags, type (Money/Qty/Rate/Percent/Date/Timestamp/Text/Link/Select) and the CD-42 extras `ListPriority` (list columns, via `list.Columns`), `Options` (Select), `MaxLen`, `Hint`, `Step`. DOC also provides `audit.Writer` (`audit.Default` in production, `auditfake` in tests, CD-03), which import uses for the "created by import" entry. List rows for unnumbered documents (draft, pending_approval, rejected) display `Draft {id}` (spec R1).
- **MST provides** one `csvio.RowFunc` per importable master, registered with `csvio.RegisterImporter(doctype, fn)` (CD-32). csvio calls it per row inside one `db.Write` (rolled back for the dry run); it creates the row with the form's validation and returns `[]doctype.FieldError`, which csvio shows as "Row N: <message>" (CD-38). Its `row` is keyed by field `Name`, with cells trimmed and formula-unescaped and Link cells already resolved from code to the target id; csvio writes the "created by import" audit entry, and the `RowFunc` skips its own `create` entry when `csvio.IsImport(ctx)` is true (CD-60).
- **NAM provides** `naming.Label(number, id)` (`Draft {id}` when the number is empty); `list` imports `naming` for it (CD-59).
- **NUM provides to every area** `types.UserID`, `types.Page` and `types.Result[T]` (CD-01; `auth.UserID`, `list.Page`, `list.Result` are aliases).
- **NUM provides to NAM** `types.FiscalYear(d types.Date) types.FY` (`Start`, `End`, `Label` "2026-27", `Short` "26-27") and `Today()` in Asia/Kolkata from the injected `clock.Clock`.
- **NUM provides to FILE** `FormatMoney(paise)` → "1,41,12,000.00", `FormatINR(paise)` → "₹1,41,12,000.00", `FormatQty(q)` → "1,200", `FormatQtyUoM(q, uom)` → "1,200 kg", `FormatRate(r)`, `FormatPercent(p)`, `FormatDate(d)` → "24-10-2026", `FormatIST(t)` → "24-10-2026 15:42" (the only timestamp display format; all of these are §5 contracts, CD-33), and `PayableTotal(doc)` (rounded total when the DocType rounds, else grand total). FILE's `print.InWordsINR(m types.Money)` turns the payable total into words.
- **NUM provides to APR and modules** `ParseMoney`/`ParseQty`/`ParseRate`/`ParsePercent`/`ParseDate` with the messages above; in `internal/kernel/gstcalc` (spec §5, CD-31) `LineAmount(q types.Qty, r types.Rate) (types.Money, error)` (the NUM-012 "too large" error), `SplitGST(taxable, rate, interState) (cgst, sgst, igst)`, `IsInterState(supplierState, receiverState string) bool`, `Totals(lines, rounded) DocTotals` (each `Line` carries `InterState`) and `StateName(code) (string, bool)` (the GST state table); and `AddMonths`, `AddDays` and `LastDayOfMonth`.
