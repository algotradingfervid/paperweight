# Masters (MST)

## Scope
MST owns the reference data every document points at: the single Company and its Branches (GSTIN, state), Party (vendor and/or customer in one table), Address and Contact, Item, UoM and per-item UoM conversion, Tax code (GST treatment and rate), HSN/SAC rules, Project, Site/Warehouse, Employee, Expense type and Payment terms. It also owns the rules shared by all masters: unique human `code` (F3), disable vs delete (F4), duplicate detection, GSTIN/PAN/state/pincode validation.
It relies on DOC for the generic "is this record referenced?" check, field-level audit (F6) and optimistic concurrency; on AUTH for the users that Employees link to, for the Masters Manager role and for per-DocType rights; on NUM for quantity/factor precision, the CGST+SGST vs IGST split and CSV import; on NAM only if a master later gets auto-numbered codes (not in v1).

## Decisions
| ID | Question | ERPNext/Frappe | Odoo | Decision | Why |
|---|---|---|---|---|---|
| D-MST-01 | Code uniqueness, case and format | `name` is the key; uniqueness follows MariaDB collation (case-insensitive); free text | No code uniqueness on product `default_code` (only an onchange warning `product/models/product_product.py:586`) | Every master has a required `code`, trimmed and **stored upper-case**, 1–20 chars, `^[A-Z0-9][A-Z0-9._/-]{0,19}$`, unique per master type. Uniqueness is therefore case-insensitive. | Upper-casing removes "v-001 vs V-001" ambiguity; short codes fit print layouts. |
| D-MST-02 | May a code be renamed? | Yes, `rename_doc` with optional merge (`erpnext/erpnext/selling/doctype/customer/test_customer.py:208`) | Codes are plain fields, editable | **Allowed** for users with the Masters Manager role (changed by spec R4: there is no Masters Manager right; the 11 rights are fixed), audited (old → new). Uniqueness re-checked. **No merge** in v1. | F3 integer IDs mean documents link by ID, so a rename can't break links. Merge is risky and rare. |
| D-MST-03 | What do old documents show after a master changes? | Documents copy display values (supplier_name, address_display, GSTIN, HSN) at creation | Same (invoice keeps partner snapshot fields) | Documents store a **snapshot** of name, GSTIN, state, address text, HSN/SAC, UoM and GST rate when the line/header is set. Later master edits, renames or disabling don't change submitted documents. | Printed documents and GST data must stay as issued. |
| D-MST-04 | Disabled master: what is blocked? | Link search hides `disabled=1` (`frappe/frappe/desk/search.py:303`); saving a document with a disabled party raises `PartyDisabled` (`erpnext/erpnext/accounts/party.py:904`) | `active=False` hides from search; records stay readable | Disabled masters are hidden from pickers and **rejected when a draft is saved or submitted**. Approved/closed/cancelled documents still show them. Lists and reports can filter "include disabled". Re-enabling is always allowed. | ERPNext behaviour; stops new use without losing history. |
| D-MST-05 | When can a master be deleted? | Blocked when linked to a non-cancelled doc (`frappe/frappe/model/delete_doc.py:415`) | `ondelete='restrict'` on most links | **Delete only if nothing references it at all**, including drafts, cancelled documents and other masters (FK `RESTRICT`). Otherwise "disable it instead". | F4; we never delete cancelled documents, so their links must stay valid. |
| D-MST-06 | Where does a party's GSTIN live? | On Party *and* on Address; the address GSTIN can back-fill the party (`india-compliance/india_compliance/gst_india/overrides/address.py:14`) | On partner (`vat`); child contacts inherit the commercial partner's | **On the Address** (one GSTIN per registration/state). Party holds **PAN** and **GST category**. All GSTINs of one party must carry the party's PAN. Party's "primary GSTIN" = GSTIN of its primary billing address. | A vendor registered in two states has two GSTINs and one PAN; this models it without duplicating parties. |
| D-MST-07 | GSTIN validation | Upper/trim, length 15, check digit, regex per GST category, state code = address state (`utils/__init__.py:375`, `:554`, `:413`; `overrides/address.py:71`) | `check_vat_in` plus a state *warning* only (`addons/l10n_in/models/res_partner.py:53`) | India Compliance rules exactly, and state mismatch is an **error** (not a warning). | GST returns reject a wrong state; catching it at the master is cheapest. |
| D-MST-08 | Duplicate detection | No GSTIN duplicate check; duplicate customer *names* get " - 1" suffix (`test_customer.py:295`) | Same-VAT partner is a **warning**, and checks archived partners too (`odoo/addons/base/models/res_partner.py:542`) | Same GSTIN on another party (enabled **or disabled**) → **error** naming that party. Same PAN on another party → warning. Same name → warning. | One GSTIN = one legal registration; a second party record splits history. Including disabled parties (Odoo) points the user to re-enable instead. |
| D-MST-09 | GST categories in v1 | Registered Regular, Composition, Unregistered, SEZ, Overseas, Deemed Export, UIN Holders, Tax Deductor, Tax Collector, ISD | regular, composition, unregistered, consumer, overseas, SEZ, deemed export, UIN | v1: **Registered Regular, Registered Composition, Unregistered, SEZ, Overseas**. Others later. Category is guessed from GSTIN when blank. | Covers a 20-person company's vendors/customers; the rest are rare. |
| D-MST-10 | State list and codes | `STATE_NUMBERS` (`india-compliance/.../constants/__init__.py:96`): 36 states/UTs + 97 Other Territory + 96 Other Countries | `res.country.state.l10n_in_tin` | Use India Compliance's list verbatim. Legacy codes 25 (Daman & Diu) and 28 (old Andhra Pradesh) are **rejected**. | One source of truth that matches the GST portal today. |
| D-MST-11 | Pincode check | 6 digits, not starting 0, first 3 digits within the state's range (`utils/__init__.py:461`) | None | Adopt for Indian addresses. | Cheap, catches typos that e-way bills later reject. |
| D-MST-12 | Address/contact ownership | Dynamic links: one address/contact can link to many parties (`frappe/frappe/contacts`) | Contacts are child partners | Each Address/Contact belongs to **exactly one owner** (a Party or a Branch). | Simpler queries and permissions; sharing addresses across parties is rare. |
| D-MST-13 | Primary address/contact | Setting a new primary clears the old one (`contact.py:83`, `address.py:95`) | `address_get` resolution | One primary billing address, one primary shipping address, one primary contact per party; promoting one clears the other. First address of a type becomes primary automatically. | ERPNext behaviour; documents need one default. |
| D-MST-14 | Branch GSTIN | Company addresses each carry a GSTIN | Branch companies with own VAT | Branch has required **state**, optional **GSTIN**; GSTIN's PAN must equal the Company PAN; two branches in the same state **may share** a GSTIN. Company is a single row (F1). | GST registration is per state, not per office. |
| D-MST-15 | HSN/SAC rules | Required for sales items when enabled; length 4/6/8 (`gst_hsn_code.py:115`, `constants:1524`); `99` prefix = service | Warning only: digits, 4/6/8 (`addons/l10n_in/models/product_template.py:23`) | Goods: HSN **required**, digits only, length 4, 6 or 8, must not start with `99`. Services: SAC optional in v1, if given exactly 6 digits starting `99`. | Brief: HSN required for goods; services become required when Sales Invoice ships. |
| D-MST-16 | GST rates allowed | `GST_TAX_RATES` = 0, 0.1, 0.25, 1, 1.5, 3, 5, 6, 7.5, 12, 18, 28, **40** (`constants/__init__.py:1426`); Taxable needs rate > 0, other treatments force 0 (`overrides/item_tax_template.py:15`) | Tax records per rate | Tax code = treatment (Taxable, Nil-Rated, Exempted, Non-GST, Zero-Rated) + rate. Rate must be in India Compliance's set. Seed: 0.25, 3, 5, 12, 18, 28, 40 Taxable plus Nil-Rated, Exempted, Non-GST at 0. 0.1/1/1.5/6/7.5 are allowed but not seeded. | **Finding:** India Compliance now lists **40%**, matching the GST rate rationalisation (5/18/40 plus special rates) that I understand took effect 22 Sep 2025; 12% and 28% stay valid for older documents and a few goods. I couldn't verify the notification itself, only the code. Admins disable rates they no longer use. |
| D-MST-17 | UoM conversion model | Per-item table (`item.uoms`) with global prefill (`item.py:1746`); stock UoM row factor = 1 | Global UoM categories with relative factors | **Per-item** conversion only: "1 Box = 12 Nos". Stock UoM row auto-added with factor 1. Factor > 0, ≤ 6 decimals. Purchase UoM must have a conversion row. Global prefill later. | Simple and explicit for a small catalogue. |
| D-MST-18 | Whole-number UoMs | `must_be_whole_number`; line qty must be integer (`erpnext/erpnext/utilities/transaction_base.py:644`), tolerating conversion dust | Rounding per UoM | UoM has `whole_number`. A line qty in a whole-number UoM, **and** the converted stock qty when the stock UoM is whole-number, must be integers after rounding to NUM quantity precision. | Stops "3.4 Nos" and "1.3 Box = 15.6 Nos". |
| D-MST-19 | Changing an item's stock UoM | Blocked while stock/bins exist; allowed after cancel; clears old conversions (`test_item.py:1256-1305`) | Category change blocked once used | Stock UoM **locked once the item is referenced** by any non-draft document. While unlocked, changing it resets the conversion table to the new stock UoM. | Prevents silent re-scaling of past quantities. |
| D-MST-20 | Site/Warehouse: tree or flat | Tree (nested set) with group/ledger warehouses | Locations tree | **Flat** in v1. Each site/warehouse belongs to a branch, has type `warehouse` or `site`, optional project. | Tree adds conversion rules (group↔ledger) a 20-person company doesn't need; Stock is a later module. |
| D-MST-21 | Employee ↔ user link | User unique among **Active** employees (`employee.py:339`); Left employee disables the user (`employee.py:325`) | Unique (user, company) always (`addons/hr/models/hr_employee.py:390`) | Optional user link, unique among active employees. Marking an employee Left **disables the linked user**. | ERPNext; lets a rehired person's user move to the new record. |
| D-MST-22 | Reporting-manager loops | Self blocked (`employee.py:356`); nested set blocks loops (`frappe/frappe/utils/nestedset.py:252`) | Cycles allowed on purpose (`hr_employee.py:707`) | Block self and any cycle; manager must be Active; can't mark someone Left while active people report to them. | Approval routing (APR) walks this chain; a cycle would loop forever. |
| D-MST-23 | Payment terms | Template rows: portions must sum 100, credit days ≥ 0, duplicate rows rejected; discount and credit-months options | Payment term lines with % and days | Rows = portion % + days after document date. Sum must equal 100.00, days integer ≥ 0, duplicate (days) rows rejected. Discounts, end-of-month and credit months later. | Covers "100% in 30 days" and "50% advance, 50% in 30 days". |
| D-MST-24 | Expense type defaults | Expense Claim Type: accounts per company only | Expense product: type, taxes, cost | Code, name, optional default HSN/SAC and default tax code, validated with item rules. | Speeds up expense lines while keeping GST data right. |
| D-MST-25 | Brief's example GSTIN | n/a | n/a | `27AABCS1429B1Z5` **fails** the check digit (correct last character is `U`). Tests use `27AABCS1429B1ZU` and keep `…Z5` as the invalid-checksum case (confirmed by spec R5: `00-fixtures.md` is canonical). | Flagged for the brief owner. |

**Test data used below:** the canonical fixtures in `00-fixtures.md` (spec R5). Company Vardhan Infra Projects Pvt Ltd, PAN `AAFCV2449G`; branches `PUNE` `27AAFCV2449G1Z7` (Maharashtra, head office) and `BLR` `29AAFCV2449G1Z3` (Karnataka). Parties `SHREE` (PAN `AABCS1429B`: `27AABCS1429B1ZU` Mumbai, `27AABCS1429B2ZT` Thane, `07AABCS1429B1ZW` Delhi), `BHARAT` `29AABCT1332L1ZA`, `RAJKOT` `24AAACR5055K1ZD`, `COROM` `33AAACT2727Q1Z3`, `LOCAL` (unregistered). Masters Manager user: kavya. Invalid check digit: `27AABCS1429B1Z5`. Non-fixture GSTINs kept on purpose (all with valid check digits): `27AAGCM1234F1ZS` (a different PAN, MST-048), `99AABCS1429B1ZJ` and `25AABCS1429B1ZY` (unknown/legacy state codes, MST-038), TCS `29AABCF8078M1C8`, OIDAR `9917SGP29001OST`, UIN `0717UNO00157UNO` (special GSTIN shapes from India Compliance tests).

## Test cases

### Common master rules

### MST-001 · Code is required on every master
- **Priority:** P1
- **Level:** unit
- **Given** a new Party with name "Shree Steel Traders" and blank code **When** saved **Then** it fails with "Code is required. Enter a short code such as SHREE." Same for Item, UoM, Tax code, Project, Site, Employee, Expense type, Payment terms, Branch (table-driven).
- **Go test:** `TestMaster_CodeRequired`
- **Sources:** `erpnext/erpnext/buying/doctype/supplier/supplier.py:154`

### MST-002 · Code is trimmed and stored upper-case
- **Priority:** P1
- **Level:** unit
- **Given** code `"  shree "` **When** saved **Then** stored code is `SHREE`.
- **Go test:** `TestMaster_CodeNormalised`
- **Sources:** `india-compliance/india_compliance/gst_india/utils/__init__.py:392` (same upper/strip pattern for GSTIN)

### MST-003 · Code uniqueness is case-insensitive per master type
- **Priority:** P1
- **Level:** integration
- **Given** fixture Party `SHREE` exists **When** another Party is saved with code `shree` **Then** it fails with "Code SHREE is already used by Party Shree Steel Traders. Choose another code." **And** an Item with code `SHREE` is allowed (different master type).
- **Go test:** `TestMaster_CodeUniqueCaseInsensitive`
- **Sources:** `erpnext/erpnext/selling/doctype/customer/test_customer.py:295` (`test_duplicate_customer`), `odoo/addons/product/models/product_product.py:586`

### MST-004 · Code format is enforced
- **Priority:** P1
- **Level:** unit
- **Given** codes `"A B"`, `"-ABC"`, `"ABC#1"`, a 21-character code, `"ÄBC"` **When** saved **Then** each fails with "Code can use letters, digits and . _ / - only, must start with a letter or digit, and be at most 20 characters." **And** `MS-ANGLE-50`, `PO/HSN.1`, `A_1` pass.
- **Go test:** `TestMaster_CodeFormat`
- **Sources:** decision D-MST-01

### MST-005 · Uniqueness holds under concurrent inserts
- **Priority:** P2
- **Level:** integration
- **Given** two transactions insert Party code `NEWPARTY` at the same time **When** both commit **Then** exactly one succeeds and the other gets the MST-003 message (unique index, not a pre-check only).
- **Go test:** `TestMaster_CodeUniqueConcurrent`
- **Sources:** decision D-MST-01

### MST-006 · Renaming a code keeps links and is audited
- **Priority:** P1
- **Level:** integration
- **Given** fixture Party `SHREE` is used by approved PO `PO/26-27/00001` **When** kavya (Masters Manager) renames the code to `SHREE-STL` **Then** the PO still links to the same party ID, shows the snapshot name it was created with, and the audit trail records `code: SHREE → SHREE-STL`.
- **Go test:** `TestMaster_RenameCodeKeepsLinks`
- **Sources:** `erpnext/erpnext/selling/doctype/customer/test_customer.py:208` (`test_rename`)

### MST-007 · Renaming to an existing code is refused (no merge)
- **Priority:** P1
- **Level:** integration
- **Given** fixture Item `MS-ANGLE-50` and Item `MS-ANGLE-65` created in this test **When** `MS-ANGLE-65` is renamed to `ms-angle-50` **Then** it fails with the MST-003 message; nothing is merged.
- **Go test:** `TestMaster_RenameToExistingCodeRefused`
- **Sources:** `erpnext/erpnext/stock/doctype/item/test_item.py:95` (`test_merge_rename_runs_duplicate_stock_reco_check`, merge excluded in v1)

### MST-008 · Only users with the Masters Manager role can rename codes
- **Priority:** P2
- **Level:** integration
- **Given** ravi (Purchase Manager, no Masters Manager role) is given Party `write` in this test **When** he changes the code of `SHREE` **Then** it fails with "Only a Masters Manager can change a code." **And** kavya (Masters Manager) can change it.
- **Go test:** `TestMaster_RenameNeedsRight`
- **Sources:** decision D-MST-02

### MST-009 · Disabled master is hidden from pickers
- **Priority:** P1
- **Level:** integration
- **Given** fixture parties `SHREE` (enabled) and `BHARAT` (disabled in this test) **When** the vendor picker lists vendors **Then** `SHREE` is returned and `BHARAT` is not **And** with `include_disabled=true` (list screen filter) both are returned, `BHARAT` tagged "Disabled".
- **Go test:** `TestMaster_DisabledHiddenFromSearch`
- **Sources:** `frappe/frappe/desk/search.py:303`

### MST-010 · Saving a draft that uses a disabled master fails
- **Priority:** P1
- **Level:** integration
- **Given** a draft PO (`Draft 42`) for vendor `SHREE` **When** kavya disables `SHREE` and the PO is saved **Then** it fails with DOC-034's wording (CD-38): "Vendor SHREE Shree Steel Traders is disabled. Pick an active vendor, or ask an admin to enable it." **When** the vendor is re-enabled **Then** the save succeeds. Table-driven for Item, UoM, Tax code, Project, Site, Branch, Expense type, Payment terms on lines/headers, each with the same wording for its field label, code and name (on lines prefixed "Line N: ", e.g. "Line 1: Item MS-ANGLE-50 MS Angle 50x50x5 is disabled. Pick an active item, or ask an admin to enable it.").
- **Go test:** `TestMaster_DisabledBlocksDraftSave`
- **Sources:** `erpnext/erpnext/buying/doctype/supplier/test_supplier.py:94` (`test_supplier_disabled`), `erpnext/erpnext/selling/doctype/customer/test_customer.py:264` (`test_disabled_customer`), `erpnext/erpnext/accounts/party.py:904`

### MST-011 · Approved documents keep showing a disabled master
- **Priority:** P1
- **Level:** integration
- **Given** approved reference PO `PO/26-27/00001` for vendor `SHREE` with item `MS-ANGLE-50` **When** both are disabled **Then** the PO view, the list, and the print view (for a user with PO `print` right) still show "Shree Steel Traders" and "MS-ANGLE-50", and closing or cancelling the PO is not blocked by the disabled masters.
- **Go test:** `TestMaster_DisabledVisibleOnOldDocs`
- **Sources:** `odoo/odoo/addons/base/models/res_partner.py:546` (deactivated partners are still found), decision D-MST-04

### MST-012 · Amending a cancelled document that uses a now-disabled master
- **Priority:** P2
- **Level:** integration
- **Given** cancelled PO `PO/26-27/00001` whose vendor `SHREE` is now disabled **When** it is amended **Then** the new draft is created **But** saving it fails with the MST-010 message until another vendor is chosen.
- **Go test:** `TestMaster_AmendWithDisabledMaster`
- **Sources:** decision D-MST-04, F5

### MST-013 · Never-referenced master may be deleted
- **Priority:** P1
- **Level:** integration
- **Given** Item `TEST-ONLY` with no references **When** deleted **Then** it is gone and the audit trail records the deletion.
- **Go test:** `TestMaster_DeleteUnreferenced`
- **Sources:** `frappe/frappe/tests/test_dynamic_links.py:74` (delete succeeds after link removed)

### MST-014 · Referenced master cannot be deleted, even by a cancelled or draft document
- **Priority:** P1
- **Level:** integration
- **Given** Item `WIRE-18G` is used only on cancelled PO `PO/26-27/00007` **When** deleted **Then** it fails with "Cannot delete Item WIRE-18G: it is used in PO/26-27/00007. Disable it instead." Repeat with a draft PO (message names `Draft 42`, not a number) and with another master referencing it (e.g. Expense type default tax code).
- **Go test:** `TestMaster_DeleteReferencedRefused`
- **Sources:** `frappe/frappe/model/delete_doc.py:415` (`check_if_doc_is_linked`), `frappe/frappe/tests/test_linked_with.py:1001` (`test_check_delete_integrity`), `frappe/frappe/tests/test_dynamic_links.py:74`

### MST-015 · Delete check covers every referencing table
- **Priority:** P2
- **Level:** integration
- **Given** the schema **When** a test walks every foreign key that points at a master table **Then** each is `ON DELETE RESTRICT` (no `CASCADE`/`SET NULL` to documents), except Address/Contact rows owned by the party (MST-046).
- **Go test:** `TestMaster_AllFKsRestrict`
- **Sources:** decision D-MST-05

### MST-016 · Disable and enable are audited
- **Priority:** P1
- **Level:** integration
- **Given** an enabled Item **When** disabled and later enabled **Then** the audit trail has two entries `disabled: false → true` and `true → false` with user and UTC time.
- **Go test:** `TestMaster_DisableAudited`
- **Sources:** F6

### MST-017 · Name is required and trimmed
- **Priority:** P1
- **Level:** unit
- **Given** name `"   "` **When** saved **Then** it fails with "Name is required." **And** `"  Shree Steel  "` is stored as `Shree Steel`.
- **Go test:** `TestMaster_NameRequiredTrimmed`
- **Sources:** `frappe/frappe/contacts/doctype/address/address.py:75` ("Address Title is mandatory.")

### MST-018 · Optimistic concurrency on master edits
- **Priority:** P2
- **Level:** integration
- **Given** kavya and admin open Item `MS-ANGLE-50` **When** both save **Then** the second save fails with DOC's stale-record message and nothing is overwritten.
- **Go test:** `TestMaster_ConcurrentEditRejected`
- **Sources:** relies on DOC

### Company and branches

### MST-019 · Exactly one company per install
- **Priority:** P1
- **Level:** integration
- **Given** the company "Vardhan Infra Projects Pvt Ltd" exists **When** a second company is created **Then** it fails with "This install supports one company. Add a branch instead." **And** deleting the company is refused.
- **Go test:** `TestCompany_Singleton`
- **Sources:** F1

### MST-020 · Company PAN format
- **Priority:** P1
- **Level:** unit
- **Given** PAN `aafcv2449g` **When** saved **Then** stored as `AAFCV2449G`. **Given** `AAFCV2449` or `1AFCV2449G` **Then** fails with "PAN must be 10 characters: 5 letters, 4 digits, 1 letter (e.g. AAFCV2449G)."
- **Go test:** `TestCompany_PANFormat`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/party.py:62`, `india-compliance/india_compliance/gst_india/constants/__init__.py:1473`

### MST-021 · Branch requires code, name and state
- **Priority:** P1
- **Level:** unit
- **Given** a new branch `MUM` with no state **When** saved **Then** fails with "State is required. Pick the state where this branch is located."
- **Go test:** `TestBranch_RequiredFields`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/address.py:85`

### MST-022 · Branch GSTIN must match branch state
- **Priority:** P1
- **Level:** unit
- **Given** branch `BLR`, state Karnataka (29), with GSTIN `27AAFCV2449G1Z7` **When** saved **Then** fails with "GSTIN 27AAFCV2449G1Z7 is for Maharashtra (27) but the branch state is Karnataka (29). Fix the GSTIN or the state." **And** `29AAFCV2449G1Z3` passes.
- **Go test:** `TestBranch_GSTINStateMatch`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/address.py:71` (`validate_state`)

### MST-023 · Branch GSTIN must carry the company PAN
- **Priority:** P1
- **Level:** unit
- **Given** company PAN `AAFCV2449G` **When** branch `PUNE` (Maharashtra) is saved with `27AABCS1429B1ZU` **Then** fails with "GSTIN 27AABCS1429B1ZU belongs to PAN AABCS1429B, not the company PAN AAFCV2449G."
- **Go test:** `TestBranch_GSTINMatchesCompanyPAN`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/address.py:36` (PAN comparison before linking)

### MST-024 · Branch GSTIN is optional
- **Priority:** P1
- **Level:** unit
- **Given** a new branch `NASHIK`, Maharashtra, no GSTIN **When** saved **Then** it succeeds and its GST state code is `27`.
- **Go test:** `TestBranch_GSTINOptional`
- **Sources:** F1

### MST-025 · Two branches in one state may share a GSTIN
- **Priority:** P2
- **Level:** integration
- **Given** fixture branch `PUNE` with `27AAFCV2449G1Z7` **When** a new branch `MUM` (Maharashtra) is saved with the same GSTIN **Then** it succeeds. **When** branch `BLR` (Karnataka) is changed to use it **Then** the MST-022 error.
- **Go test:** `TestBranch_SharedGSTINSameState`
- **Sources:** `erpnext/erpnext/setup/doctype/company/test_company.py:183` (`test_primary_address`), decision D-MST-14

### MST-026 · Branch GSTIN undergoes full GSTIN validation
- **Priority:** P1
- **Level:** unit
- **Given** branch `PUNE` GSTIN set to `27AABCS1429B1Z5` (bad check digit) **When** saved **Then** fails with the MST-031 message (the check digit is reported, not the PAN mismatch).
- **Go test:** `TestBranch_GSTINValidated`
- **Sources:** `india-compliance/india_compliance/setup_wizard.py:83`

### MST-027 · At least one enabled branch must remain
- **Priority:** P1
- **Level:** integration
- **Given** `TECH-SITE` and branch `BLR` are disabled, and `PUNE-WH` and `METRO-SITE` are disabled, so `PUNE` is the only enabled branch and has no enabled sites **When** `PUNE` is disabled **Then** fails with "At least one branch must stay enabled."
- **Go test:** `TestBranch_LastEnabledBranch`
- **Sources:** decision D-MST-14

### MST-028 · First install seeds one company and one head-office branch
- **Priority:** P2
- **Level:** integration
- **Given** an empty database **When** setup completes with company "Vardhan Infra Projects Pvt Ltd", state Maharashtra, GSTIN `27AAFCV2449G1Z7`, head-office branch code `PUNE` **Then** one company (PAN `AAFCV2449G` derived) and one head-office branch `PUNE` exist.
- **Go test:** `TestSetup_SeedsCompanyAndBranch`
- **Sources:** `india-compliance/india_compliance/setup_wizard.py:83`

### MST-029 · Branch has its own address
- **Priority:** P2
- **Level:** integration
- **Given** branch `PUNE` **When** an address "Plot 12, MIDC Bhosari, Pune 411026, Maharashtra" is added **Then** it is owned by the branch and its state must equal the branch state (else "Branch address state must match the branch state.").
- **Go test:** `TestBranch_Address`
- **Sources:** `erpnext/erpnext/setup/doctype/company/test_company.py:183` (`test_primary_address`)

### GSTIN, PAN and state validation (shared by Branch and Address)

### MST-030 · GSTIN is upper-cased, trimmed and must be 15 characters
- **Priority:** P1
- **Level:** unit
- **Given** `" 27aabcs1429b1zu "` **Then** stored `27AABCS1429B1ZU`. **Given** `27AABCS1429B1Z` (14) **Then** fails with "GSTIN 27AABCS1429B1Z must have 15 characters."
- **Go test:** `TestGSTIN_NormaliseAndLength`
- **Sources:** `india-compliance/india_compliance/gst_india/utils/__init__.py:375` (`validate_gstin`)

### MST-031 · GSTIN check digit is verified
- **Priority:** P1
- **Level:** unit
- **Given** `27AABCS1429B1Z5` **Then** fails with "GSTIN 27AABCS1429B1Z5 is not valid: the last character doesn't match. Check for typing mistakes." **And** `27AABCS1429B1ZU`, `24AAACR5055K1ZD`, `29AABCF8078M1C8`, `9917SGP29001OST`, `0717UNO00157UNO` pass the check digit.
- **Go test:** `TestGSTIN_CheckDigit`
- **Sources:** `india-compliance/india_compliance/gst_india/utils/__init__.py:554` (`validate_gstin_check_digit`)

### MST-032 · GSTIN check digit fuzz invariant
- **Priority:** P2
- **Level:** unit
- **Given** any 14-character prefix over `[0-9A-Z]` **When** the check digit is computed and appended **Then** validation passes, and changing any single character of the 15 makes it fail.
- **Go test:** `FuzzGSTIN_CheckDigit`
- **Sources:** `india-compliance/india_compliance/gst_india/utils/__init__.py:554`

### MST-033 · GSTIN format must match the GST category
- **Priority:** P1
- **Level:** unit
- **Given** category Overseas and GSTIN `27AABCS1429B1ZU` **Then** fails with "GSTIN 27AABCS1429B1ZU doesn't match GST category Overseas. Check the GSTIN and the category." **Given** category Registered Regular and `9917SGP29001OST` **Then** same message for Registered Regular. Table: Regular/Composition/SEZ accept `NORMAL` pattern (`^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][1-9A-Z][Z1-9ABD-J][0-9A-Z]$`); Registered Regular also accepts the TCS and TDS patterns (e.g. TCS `29AABCF8078M1C8`; CD-39); Overseas accepts NRI/OIDAR patterns. **Given** a UIN GSTIN `0717UNO00157UNO` with any category **Then** fails with "This GSTIN category isn't supported yet." (CD-39).
- **Go test:** `TestGSTIN_FormatPerCategory`
- **Sources:** `india-compliance/india_compliance/gst_india/utils/__init__.py:413` (`validate_gst_category`), `india-compliance/india_compliance/gst_india/constants/__init__.py:1444`, `:1461`

### MST-034 · Unregistered with a GSTIN, or registered without one, is refused
- **Priority:** P1
- **Level:** unit
- **Given** category Unregistered and GSTIN `27AABCS1429B1ZU` **Then** fails with "GST category can't be Unregistered when a GSTIN is entered." **Given** category Registered Regular and no GSTIN **Then** fails with "Enter a GSTIN, or set GST category to Unregistered or Overseas."
- **Go test:** `TestGSTIN_CategoryRequiresGSTIN`
- **Sources:** `india-compliance/india_compliance/gst_india/utils/__init__.py:413`

### MST-035 · GST category is guessed from the GSTIN when blank
- **Priority:** P1
- **Level:** unit
- **Given** blank category: `27AABCS1429B1ZU` → Registered Regular; `9917SGP29001OST` → Overseas; no GSTIN + country India → Unregistered; no GSTIN + country Singapore → Overseas; `29AABCF8078M1C8` (e-commerce TCS) → Registered Regular; a TDS-pattern GSTIN → Registered Regular (CD-39). **And** a user-chosen Composition or SEZ with a regular-pattern GSTIN is kept. **Given** the UIN GSTIN `0717UNO00157UNO` **Then** no category is guessed and it fails with "This GSTIN category isn't supported yet." (CD-39).
- **Go test:** `TestGSTIN_GuessCategory`
- **Sources:** `india-compliance/india_compliance/gst_india/utils/__init__.py:509` (`guess_gst_category`), `india-compliance/india_compliance/gst_india/utils/test_utils.py:95` (`test_guess_gst_category_for_oidar`), `india-compliance/india_compliance/gst_india/overrides/test_party.py:6` (`test_validate_new_party`), `:23` (`test_validate_new_party_with_tcs`)

### MST-036 · PAN is derived from the GSTIN
- **Priority:** P1
- **Level:** unit
- **Given** GSTIN `24AAACR5055K1ZD` (RAJKOT) **Then** PAN = `AAACR5055K`. **Given** `9917SGP29001OST` or `0717UNO00157UNO` **Then** PAN = "" (characters 3–12 are not a PAN).
- **Go test:** `TestGSTIN_PANFromGSTIN`
- **Sources:** `india-compliance/india_compliance/gst_india/utils/__init__.py:457`, `india-compliance/india_compliance/gst_india/utils/test_utils.py:99` (`test_get_pan_from_gstin`), `odoo/addons/l10n_in/models/res_partner.py:121`

### MST-037 · State code comes from the GSTIN's first two digits
- **Priority:** P1
- **Level:** unit
- **Given** GSTIN `33AAACT2727Q1Z3` on an address with blank state **When** saved **Then** state = Tamil Nadu, state code `33`. **Given** a state already chosen that differs **Then** the MST-022-style error "GSTIN 33… is for Tamil Nadu (33) but the address state is Kerala (32)."
- **Go test:** `TestGSTIN_StateFromGSTIN`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/address.py:100`, `odoo/addons/l10n_in/models/res_partner.py:53` (`_compute_l10n_in_gst_state_warning`)

### MST-038 · Unknown or legacy state codes are rejected
- **Priority:** P1
- **Level:** unit
- **Given** `99AABCS1429B1ZJ` (registered pattern, state 99) and `25AABCS1429B1ZY` (old Daman & Diu), both with valid check digits **When** used on an Indian address **Then** each fails with "GSTIN state code 99 is not a current GST state code. Check the GSTIN." (25 for the second).
- **Go test:** `TestGSTIN_UnknownStateCode`
- **Sources:** `india-compliance/india_compliance/gst_india/constants/__init__.py:96` (`STATE_NUMBERS`), decision D-MST-10

### MST-039 · State list matches the GST state codes
- **Priority:** P1
- **Level:** unit
- **Given** the seeded state table **Then** it equals India Compliance's `STATE_NUMBERS`: e.g. Maharashtra 27, Karnataka 29, Delhi 07, Ladakh 38, Andhra Pradesh 37, Telangana 36, Dadra and Nagar Haveli and Daman and Diu 26, Other Territory 97; "Other Countries" 96 used only for overseas place of supply.
- **Go test:** `TestStates_MatchGSTCodes`
- **Sources:** `india-compliance/india_compliance/gst_india/constants/__init__.py:96`

### MST-040 · Manual PAN validated; GSTIN-derived PAN wins
- **Priority:** P1
- **Level:** unit
- **Given** party `SHREE` built without addresses, category Unregistered, PAN entered as `aabcs1429b` **Then** stored `AABCS1429B`. **Given** PAN `AABCS1429` **Then** "PAN must be 10 characters: 5 letters, 4 digits, 1 letter (e.g. AAFCV2449G)." **Given** the party then gets its Mumbai address GSTIN `27AABCS1429B1ZU` **Then** PAN stays `AABCS1429B`.
- **Go test:** `TestParty_PANValidation`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/party.py:62` (`validate_pan`), `india-compliance/india_compliance/gst_india/utils/__init__.py:449`

### Party

### MST-041 · Party must be a vendor, a customer, or both
- **Priority:** P1
- **Level:** unit
- **Given** party with `is_vendor=false`, `is_customer=false` **Then** fails with "Tick Vendor, Customer or both." **And** a party with both ticked appears in both the vendor and customer pickers.
- **Go test:** `TestParty_RoleFlags`
- **Sources:** decision (one table, brief)

### MST-042 · Vendor picker shows only vendors
- **Priority:** P1
- **Level:** integration
- **Given** fixture party `SHREE` (vendor only), and parties created in this test `C-ONLY` "Only Customer Ltd" (customer only) and `B-MIX` "Mixed Party Ltd" (both) **When** the PO vendor picker is opened **Then** it lists `SHREE` and `B-MIX` but not `C-ONLY`; saving a PO with `C-ONLY` fails with "Only Customer Ltd is not marked as a vendor. Tick Vendor on the party first."
- **Go test:** `TestParty_VendorPickerFilter`
- **Sources:** `erpnext/erpnext/accounts/party.py:904` (party-type validation on documents)

### MST-043 · Unticking a role that is in use is refused
- **Priority:** P2
- **Level:** integration
- **Given** `B-MIX` "Mixed Party Ltd" (vendor and customer, created in this test) used on approved PO `PO/26-27/00003` **When** `is_vendor` is unticked **Then** fails with "B-MIX is used as a vendor on PO/26-27/00003. Disable the party instead."
- **Go test:** `TestParty_RoleInUse`
- **Sources:** decision D-MST-04

### MST-044 · Duplicate GSTIN across parties is refused, including disabled parties
- **Priority:** P1
- **Level:** integration
- **Given** fixture party `SHREE` (Mumbai address GSTIN `27AABCS1429B1ZU`) is disabled **When** a new party `SHREE-NEW` adds an address with `27AABCS1429B1ZU` **Then** fails with "GSTIN 27AABCS1429B1ZU is already used by party SHREE (disabled). Enable and use that party instead." **And** two addresses of the **same** party may hold the same GSTIN (billing + shipping in one state).
- **Go test:** `TestParty_DuplicateGSTINRefused`
- **Sources:** `odoo/odoo/addons/base/models/res_partner.py:542` (`_compute_same_vat_partner_id`, `active_test=False`), `india-compliance/india_compliance/gst_india/overrides/party.py:96` (`get_docs_with_previous_gstin`)

### MST-045 · Same PAN or same name on another party warns but saves
- **Priority:** P2
- **Level:** integration
- **Given** party `SHREE` (PAN `AABCS1429B`) with its unreferenced Delhi address deleted in this test **When** a new party `SHREE-DEL` is saved with Delhi GSTIN `07AABCS1429B1ZW` **Then** it saves with warning "Party SHREE has the same PAN AABCS1429B. You may be creating a duplicate; consider adding a Delhi address to SHREE." Same-name parties warn "A party named Shree Steel Traders already exists (SHREE)."
- **Go test:** `TestParty_SamePANOrNameWarns`
- **Sources:** `erpnext/erpnext/selling/doctype/customer/test_customer.py:295` (`test_duplicate_customer`), `odoo/odoo/addons/base/models/res_partner.py:542`

### MST-046 · Deleting an unreferenced party deletes its own addresses and contacts
- **Priority:** P1
- **Level:** integration
- **Given** a party created in this test with two addresses and one contact, never used **When** deleted **Then** party, addresses and contact are gone. **Given** one of its addresses is snapshotted on a PO **Then** the delete is refused (MST-014).
- **Go test:** `TestParty_DeleteCascadesOwnAddresses`
- **Sources:** `erpnext/erpnext/buying/doctype/supplier/supplier.py:260` (`on_trash` → `delete_contact_and_address`), `erpnext/erpnext/selling/doctype/customer/test_customer.py:254` (`test_delete_customer_contact`)

### MST-047 · Party GST category Overseas requires a non-India country
- **Priority:** P1
- **Level:** unit
- **Given** a party address with country Singapore and category Registered Regular **Then** fails with "Addresses outside India must have GST category Overseas." **And** an Indian address with category Overseas is allowed (high-seas sales).
- **Go test:** `TestParty_OverseasCountry`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/address.py:61` (`validate_overseas_gst_category`), `india-compliance/india_compliance/gst_india/overrides/test_party.py:33` (`test_validate_oidar_party`)

### MST-048 · All GSTINs of a party must share its PAN
- **Priority:** P1
- **Level:** integration
- **Given** party `SHREE` with PAN `AABCS1429B` **When** an address with `27AAGCM1234F1ZS` (valid check digit, other PAN) is added **Then** fails with "GSTIN 27AAGCM1234F1ZS belongs to PAN AAGCM1234F, but this party's PAN is AABCS1429B. Use a separate party for a different business."
- **Go test:** `TestParty_GSTINsSharePAN`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/address.py:36`

### MST-049 · First GSTIN on an unregistered party upgrades it
- **Priority:** P1
- **Level:** integration
- **Given** party `COROM` with its Chennai address removed, category Unregistered and PAN blank **When** a billing address with `33AAACT2727Q1Z3` is added **Then** party category becomes Registered Regular and PAN `AAACT2727Q`.
- **Go test:** `TestParty_GSTINBackfillsParty`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/address.py:14` (`update_party_gstin_and_gst_category`), `india-compliance/india_compliance/gst_india/overrides/test_party.py:45`

### MST-050 · Party default payment terms and default branch fields
- **Priority:** P2
- **Level:** integration
- **Given** vendor `SHREE` with default payment terms `NET30` **When** a new PO is created for it **Then** payment terms default to `NET30`; if `NET30` is disabled the field is left blank.
- **Go test:** `TestParty_DefaultPaymentTerms`
- **Sources:** `erpnext/erpnext/buying/doctype/supplier/test_supplier.py:39` (`test_supplier_default_payment_terms`)

### MST-051 · Primary contact/address must belong to the party
- **Priority:** P1
- **Level:** integration
- **Given** a contact owned by `BHARAT` **When** set as primary contact of `SHREE` **Then** fails with "That contact belongs to another party. Pick one of this party's contacts."
- **Go test:** `TestParty_PrimaryMustBelong`
- **Sources:** `erpnext/erpnext/buying/doctype/supplier/test_supplier.py:129` (`test_primary_contact_must_be_linked_to_supplier`), `erpnext/erpnext/buying/doctype/supplier/supplier.py:169`

### MST-052 · Changing an address GSTIN doesn't touch approved documents
- **Priority:** P1
- **Level:** integration
- **Given** approved PO `PO/26-27/00001` snapshotting vendor `SHREE` GSTIN `27AABCS1429B1ZU` **When** the Mumbai address GSTIN is changed to `27AABCS1429B2ZT` **Then** the PO still shows `…1ZU`; new POs pick `…2ZT`; the change is audited.
- **Go test:** `TestParty_GSTINChangeKeepsSnapshots`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/party.py:80` (`set_docs_with_previous_gstin`), decision D-MST-03

### Address and contact

### MST-053 · Address required fields
- **Priority:** P1
- **Level:** unit
- **Given** an Indian address missing line 1, city or state **Then** fails with "Address line 1 is required.", "City is required.", "State is required for an Indian address." respectively. Country defaults to India.
- **Go test:** `TestAddress_RequiredFields`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/address.py:85`, `frappe/frappe/contacts/doctype/address/address.py:75`

### MST-054 · Address type is billing or shipping (or both)
- **Priority:** P1
- **Level:** unit
- **Given** an address with neither billing nor shipping ticked **Then** fails with "Tick Billing, Shipping or both."
- **Go test:** `TestAddress_Type`
- **Sources:** `frappe/frappe/contacts/doctype/address/address.py:95` (preferred billing/shipping flags)

### MST-055 · Indian pincode format
- **Priority:** P1
- **Level:** unit
- **Given** pincodes `012345`, `40001`, `4000011`, `40O001` **Then** fails with "Pincode must be 6 digits and can't start with 0." **And** blank pincode is allowed.
- **Go test:** `TestAddress_PincodeFormat`
- **Sources:** `india-compliance/india_compliance/gst_india/utils/__init__.py:461` (`validate_pincode`), `india-compliance/india_compliance/gst_india/constants/__init__.py:1474`

### MST-056 · Pincode must belong to the state
- **Priority:** P2
- **Level:** unit
- **Given** state Karnataka and pincode `500001` **Then** fails with "Pincode 500001 is not in Karnataka. Check the pincode or the state." **And** Telangana `500001`, Maharashtra `400093`, both Ladakh and Jammu and Kashmir `194101` pass.
- **Go test:** `TestAddress_PincodeState`
- **Sources:** `india-compliance/india_compliance/gst_india/utils/test_utils.py:110` (`test_validate_pincode`), `india-compliance/india_compliance/gst_india/constants/__init__.py:197`

### MST-057 · Foreign address skips state code, pincode and GSTIN-state checks
- **Priority:** P2
- **Level:** unit
- **Given** country Singapore, postal code `118524`, category Overseas, GSTIN `9917SGP29001OST` **Then** saves; GST state code is blank; place-of-supply code is 96.
- **Go test:** `TestAddress_Foreign`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/address.py:71`, `india-compliance/india_compliance/gst_india/overrides/test_party.py:45`

### MST-058 · Many addresses per party
- **Priority:** P1
- **Level:** integration
- **Given** party `SHREE` **When** its three addresses are added (Mumbai billing `27AABCS1429B1ZU`, Thane shipping `27AABCS1429B2ZT`, Delhi billing+shipping `07AABCS1429B1ZW`) **Then** all are listed under the party, each with its own state and GSTIN.
- **Go test:** `TestAddress_ManyPerParty`
- **Sources:** `odoo/odoo/addons/base/tests/test_res_partner.py:728` (`test_address_get`)

### MST-059 · First address of a type becomes primary
- **Priority:** P1
- **Level:** integration
- **Given** party with no addresses **When** a billing address is added **Then** it is the primary billing address.
- **Go test:** `TestAddress_FirstIsPrimary`
- **Sources:** `erpnext/erpnext/buying/doctype/supplier/supplier.py:248` (`create_primary_address`)

### MST-060 · Promoting a primary address clears the previous one
- **Priority:** P1
- **Level:** integration
- **Given** Mumbai is primary billing **When** Delhi is marked primary billing **Then** Mumbai's primary billing flag is cleared; exactly one primary billing remains. Same for shipping.
- **Go test:** `TestAddress_OnePrimaryPerType`
- **Sources:** `frappe/frappe/contacts/doctype/address/address.py:95` (`validate_preferred_address`), `frappe/frappe/contacts/doctype/contact/test_contact.py:229` (`test_only_one_primary_contact_per_link`)

### MST-061 · Disabled address is not offered on new documents
- **Priority:** P2
- **Level:** integration
- **Given** `SHREE`'s Thane shipping address disabled **When** a new PO for `SHREE` is created **Then** it isn't offered; a draft already using it fails on save with "Address Thane is disabled. Pick another address." Disabling the primary address clears its primary flag.
- **Go test:** `TestAddress_Disabled`
- **Sources:** decision D-MST-04

### MST-062 · Contact requires a name
- **Priority:** P1
- **Level:** unit
- **Given** contact with no first name **Then** fails with "Contact name is required." Full name is built as "first middle last".
- **Go test:** `TestContact_NameRequired`
- **Sources:** `frappe/frappe/contacts/doctype/contact/test_contact.py:39` (`test_get_full_name`)

### MST-063 · Contact email format
- **Priority:** P1
- **Level:** unit
- **Given** email `accounts@shreesteel` or `accounts shree@shreesteel.test` **Then** fails with "Enter a valid email address, e.g. accounts@shreesteel.test." Stored lower-case.
- **Go test:** `TestContact_EmailFormat`
- **Sources:** `frappe/frappe/contacts/doctype/contact/contact.py:157`

### MST-064 · Contact mobile format
- **Priority:** P1
- **Level:** unit
- **Given** mobile `98200 12345`, `+91-9820012345`, `09820012345` **Then** stored as `+919820012345`. **Given** `5820012345` or `98200123` **Then** fails with "Enter a 10-digit Indian mobile number starting 6–9, optionally with +91."
- **Go test:** `TestContact_MobileFormat`
- **Sources:** `frappe/frappe/contacts/doctype/contact/test_contact.py:27` (`test_check_default_phone_and_mobile`)

### MST-065 · Several emails/phones, exactly one primary each
- **Priority:** P2
- **Level:** unit
- **Given** a contact with 3 emails none primary **Then** the first becomes primary. **Given** two marked primary **Then** fails with "Only one email can be primary."
- **Go test:** `TestContact_OnePrimaryEmail`
- **Sources:** `frappe/frappe/contacts/doctype/contact/contact.py:178` (`set_primary_email`), `frappe/frappe/contacts/doctype/contact/test_contact.py:15` (`test_check_default_email`)

### MST-066 · One primary contact per party
- **Priority:** P1
- **Level:** integration
- **Given** contact Sunil Mehta is primary for `SHREE` **When** contact Kiran Joshi (also `SHREE`'s) is saved as primary **Then** Sunil Mehta is no longer primary.
- **Go test:** `TestContact_OnePrimaryPerParty`
- **Sources:** `frappe/frappe/contacts/doctype/contact/test_contact.py:229`, `:243` (`test_promoting_contact_clears_existing_primary_contact`), `frappe/frappe/contacts/doctype/contact/contact.py:83`

### MST-067 · Address/contact belongs to exactly one owner
- **Priority:** P1
- **Level:** unit
- **Given** an address with no owner, or with both a party and a branch owner **Then** fails with "An address must belong to one party or one branch."
- **Go test:** `TestAddress_SingleOwner`
- **Sources:** decision D-MST-12

### MST-068 · Address display text for documents
- **Priority:** P2
- **Level:** unit
- **Given** `SHREE`'s Mumbai address with lines "Plot 12, MIDC", "Andheri East", city Mumbai, state Maharashtra, pincode 400093, GSTIN `27AABCS1429B1ZU` **Then** display = "Plot 12, MIDC\nAndheri East\nMumbai - 400093\nMaharashtra (27)\nGSTIN: 27AABCS1429B1ZU"; this text is what documents snapshot.
- **Go test:** `TestAddress_DisplayText`
- **Sources:** `frappe/frappe/contacts/doctype/address/test_address.py:46` (`test_template_works`)

### Item and HSN/SAC

### MST-069 · Item required fields
- **Priority:** P1
- **Level:** unit
- **Given** item missing type (goods/service) or stock UoM **Then** fails with "Item type is required (Goods or Service)." / "Stock UoM is required."
- **Go test:** `TestItem_RequiredFields`
- **Sources:** `erpnext/erpnext/stock/doctype/item/item.py:480`

### MST-070 · Goods require an HSN code
- **Priority:** P1
- **Level:** unit
- **Given** goods item `MS-ANGLE-50` "MS Angle 50x50x5" without HSN **Then** fails with "HSN code is required for goods. Enter a 4, 6 or 8 digit HSN code."
- **Go test:** `TestItem_GoodsNeedHSN`
- **Sources:** `india-compliance/india_compliance/gst_india/doctype/gst_hsn_code/gst_hsn_code.py:115` (`validate_hsn_code`), `india-compliance/india_compliance/gst_india/overrides/item.py:25`

### MST-071 · HSN length 4, 6 or 8 digits only
- **Priority:** P1
- **Level:** unit
- **Given** HSN `7214` ✓, `721420` ✓, `72142090` ✓; `100` ✗, `72142` ✗, `7214209010` ✗, `72A4` ✗, `7214 20` ✗ **Then** failures say "HSN code must be 4, 6 or 8 digits."
- **Go test:** `TestItem_HSNLength`
- **Sources:** `india-compliance/india_compliance/gst_india/doctype/gst_hsn_code/test_gst_hsn_code.py:31` (`test_validate_hsn_with_invalid_hsn_length`), `india-compliance/india_compliance/gst_india/constants/__init__.py:1524`, `odoo/addons/l10n_in/models/product_template.py:23`

### MST-072 · Goods HSN can't be a service code
- **Priority:** P1
- **Level:** unit
- **Given** goods item with HSN `998719` **Then** fails with "Codes starting 99 are service (SAC) codes. Set the item type to Service or use a goods HSN."
- **Go test:** `TestItem_GoodsHSNNot99`
- **Sources:** `india-compliance/india_compliance/gst_india/constants/__init__.py:1525` (`SERVICE_HSN_PREFIX`), `india-compliance/india_compliance/gst_india/utils/e_invoice.py:634`

### MST-073 · Service SAC optional, but validated when given
- **Priority:** P1
- **Level:** unit
- **Given** service item `SVC-CRANE` "Crane hire" with blank SAC **Then** saves. With `997313` ✓; `9973` ✗, `721420` ✗ → "SAC code must be 6 digits starting with 99."
- **Go test:** `TestItem_ServiceSAC`
- **Sources:** `india-compliance/india_compliance/gst_india/constants/__init__.py:1525`, decision D-MST-15

### MST-074 · Default tax code on item must be enabled
- **Priority:** P1
- **Level:** integration
- **Given** tax code `GST18` disabled **When** item default tax code is set to it **Then** fails with the MST-010 style message "Tax code GST18 is disabled. Pick another tax code."
- **Go test:** `TestItem_DefaultTaxCodeEnabled`
- **Sources:** `erpnext/erpnext/stock/doctype/item/test_item.py:231` (`test_item_tax_template`)

### MST-075 · Item type can't change once used
- **Priority:** P2
- **Level:** integration
- **Given** goods item `MS-ANGLE-50` used on approved GRN `GR/26-27/00001` **When** changed to Service **Then** fails with "Item type can't change after the item is used. Create a new item."
- **Go test:** `TestItem_TypeLocked`
- **Sources:** `erpnext/erpnext/stock/doctype/item/test_item.py:851` (`test_maintain_stock_cannot_be_turned_on_after_an_invoice`), `:1470` (`test_update_is_stock_item`)

### MST-076 · Service items can't be stock items
- **Priority:** P2
- **Level:** unit
- **Given** service item with `is_stock=true` **Then** fails with "Service items can't be stock items."
- **Go test:** `TestItem_ServiceNotStock`
- **Sources:** `odoo/addons/hr_expense/models/product_template.py:21` (product type drives behaviour)

### UoM and conversion

### MST-077 · UoM code, name and whole-number flag
- **Priority:** P1
- **Level:** unit
- **Given** UoM `NOS` "Numbers" whole-number, `KGS` "Kilograms" not whole-number **Then** both save; codes unique (MST-003).
- **Go test:** `TestUoM_Basic`
- **Sources:** `erpnext/erpnext/setup/doctype/uom/uom.json:36` (`must_be_whole_number`)

### MST-078 · Seeded UoMs carry GST UQC codes
- **Priority:** P2
- **Level:** unit
- **Given** a fresh install **Then** UoMs NOS, KGS, MTR, LTR, BOX, SET, BAG, TON, SQM, HRS, DRUM exist with UQC codes from the GST list (e.g. NOS→"NOS", KGS→"KGS", BOX→"BOX", TON→"TON", DRUM→"DRM"); HRS has no hours UQC and maps to "OTH" (CD-39); NOS, BOX, SET, BAG, DRUM are whole-number. Fixture units map to these codes: Nos→NOS, kg→KGS, m→MTR, Box→BOX, Drum→DRUM, Bag→BAG, Hour→HRS.
- **Go test:** `TestUoM_SeedUQC`
- **Sources:** `india-compliance/india_compliance/gst_india/constants/__init__.py:139` (`GST_UOMS`), `odoo/addons/l10n_in/models/uom_uom.py:8`

### MST-079 · Stock UoM conversion row is added automatically with factor 1
- **Priority:** P1
- **Level:** unit
- **Given** item `BOLT-M12` with stock UoM NOS and no conversion rows **When** saved **Then** conversion table = [(NOS, 1)].
- **Go test:** `TestItemUoM_StockRowAuto`
- **Sources:** `erpnext/erpnext/stock/doctype/item/item.py:489`

### MST-080 · Stock UoM factor must be 1
- **Priority:** P1
- **Level:** unit
- **Given** item `BOLT-M12`, stock UoM NOS and row (NOS, 2) **Then** fails with "The conversion factor for the stock UoM must be 1."
- **Go test:** `TestItemUoM_StockFactorOne`
- **Sources:** `erpnext/erpnext/stock/doctype/item/item.py:523` (`validate_conversion_factor`)

### MST-081 · Conversion factor must be greater than zero
- **Priority:** P1
- **Level:** unit
- **Given** item `BOLT-M12` rows (BOX, 0) or (BOX, -100) **Then** fails with "Row 2: Conversion factor must be more than 0."
- **Go test:** `TestItemUoM_FactorPositive`
- **Sources:** `erpnext/erpnext/stock/doctype/item/item.py:1152` (`validate_uom_conversion_factor`), `odoo/addons/uom/models/uom_uom.py:45`

### MST-082 · A UoM appears only once in an item's conversion table
- **Priority:** P1
- **Level:** unit
- **Given** item `BOLT-M12` rows (BOX, 100) and (BOX, 50) **Then** fails with "UoM BOX is entered more than once. Keep one row per UoM."
- **Go test:** `TestItemUoM_NoDuplicateUoM`
- **Sources:** `erpnext/erpnext/stock/doctype/item/item.py:523`

### MST-083 · Purchase UoM must have a conversion row
- **Priority:** P1
- **Level:** unit
- **Given** item `BOLT-M12`, stock UoM NOS, purchase UoM BOX, no BOX row **Then** fails with "Purchase UoM BOX has no conversion for this item. Add a row such as 1 BOX = 100 NOS." **And** with row (BOX, 100) it saves. Blank purchase UoM defaults to stock UoM.
- **Go test:** `TestItemUoM_PurchaseUoMNeedsRow`
- **Sources:** `erpnext/erpnext/stock/doctype/item/test_item.py:1026` (`test_default_uoms_need_conversion_when_uoms_are_restricted`), `erpnext/erpnext/stock/doctype/item/item.py:1160`

### MST-084 · Converting a quantity to stock UoM
- **Priority:** P1
- **Level:** unit
- **Given** item `MS-ANGLE-50` (stock UoM KGS) with row (TON, 1000) added in this test, item `BOLT-M12` (stock NOS, row (BOX, 100)) and item `CABLE-4C` (stock MTR, row (DRUM, 500)) **Then** 2.5 TON → 2500 KGS; 3 BOX → 300 NOS; 2 DRUM → 1000 MTR (unit code `DRUM`, UQC `DRM`, CD-62); rate ₹54,000/TON → ₹54/KGS.
- **Go test:** `TestItemUoM_ConvertToStock`
- **Sources:** `erpnext/erpnext/stock/doctype/item/test_item.py:999` (`test_uom_conversion_factor`), `odoo/addons/uom/tests/test_uom.py:18` (`test_10_conversion`)

### MST-085 · Fractional factors keep enough precision
- **Priority:** P2
- **Level:** unit
- **Given** stock UoM KGS, row (GRAM, 0.001) **Then** 1234 GRAM → 1.234 KGS (not 1.23 or 1.24 due to float error); factor stored as decimal, at most 6 decimal places ("Conversion factor can have at most 6 decimal places.").
- **Go test:** `TestItemUoM_FactorPrecision`
- **Sources:** `odoo/addons/uom/tests/test_uom.py:18` (dozen precision regression), `erpnext/erpnext/buying/doctype/purchase_order/test_purchase_order.py:894` (`test_min_order_qty_with_uom_conversion_dust`)

### MST-086 · Whole-number UoM refuses fractional line quantities
- **Priority:** P1
- **Level:** integration
- **Given** item `BOLT-M12`, UoM NOS whole-number **When** a PO line has qty 3.4 NOS **Then** fails with "Row 1: Quantity 3.4 can't be a fraction for UoM NOS. Enter a whole number." **And** qty 3 passes.
- **Go test:** `TestUoM_WholeNumberLineQty`
- **Sources:** `erpnext/erpnext/buying/doctype/purchase_order/test_purchase_order.py:888` (`test_uom_integer_validation`), `erpnext/erpnext/utilities/transaction_base.py:644`

### MST-087 · Converted stock qty must be whole when the stock UoM is whole-number
- **Priority:** P1
- **Level:** integration
- **Given** item `TEST-CLIP` created in this test, stock UoM NOS (whole), purchase UoM KGS (not whole) with row (KGS, 12) **When** PO qty 1.25 KGS **Then** stock qty 15 NOS passes. **When** qty 1.3 KGS **Then** fails with "Row 1: 1.3 KGS = 15.6 NOS, but NOS must be a whole number. Change the quantity."
- **Go test:** `TestUoM_WholeNumberStockQty`
- **Sources:** `erpnext/erpnext/buying/doctype/purchase_order/test_purchase_order.py:930` (`test_uom_integer_check_tolerates_conversion_dust`)

### MST-088 · Whole-number check tolerates rounding dust
- **Priority:** P2
- **Level:** unit
- **Given** stock UoM NOS, row (KGS, 0.6), qty rounded to NUM precision 3333.333 KGS → stock 1999.9998 **Then** rounded to quantity precision it is 2000 NOS and passes; 3333.9 KGS (2000.34 NOS) fails.
- **Go test:** `TestUoM_WholeNumberToleratesDust`
- **Sources:** `erpnext/erpnext/buying/doctype/purchase_order/test_purchase_order.py:930`

### MST-089 · Changing the stock UoM resets conversions while the item is unused
- **Priority:** P1
- **Level:** integration
- **Given** unused item stock UoM GRAM with rows (GRAM,1),(BOX,2) **When** stock UoM changed to NOS **Then** table becomes [(NOS, 1)] and the user sees "Stock UoM changed. Re-enter the other conversions."
- **Go test:** `TestItemUoM_StockUoMChangeResets`
- **Sources:** `erpnext/erpnext/stock/doctype/item/test_item.py:1293` (`test_erasure_of_old_conversions`), `erpnext/erpnext/stock/doctype/item/item.py:480`

### MST-090 · Stock UoM is locked once the item is used
- **Priority:** P1
- **Level:** integration
- **Given** item `MS-ANGLE-50` used on approved GRN `GR/26-27/00001` **When** stock UoM changes KGS → TON **Then** fails with "Stock UoM can't change after the item is used. Create a new item." **And** if only drafts use it, the change is allowed and the drafts are re-validated on next save.
- **Go test:** `TestItemUoM_StockUoMLocked`
- **Sources:** `erpnext/erpnext/stock/doctype/item/test_item.py:1256` (`test_check_stock_uom_with_bin`), `:1283` (`test_stock_uom_can_change_after_stock_is_cancelled`), `erpnext/erpnext/stock/doctype/item/item.py:1636`

### MST-091 · Editing a conversion factor doesn't change existing documents
- **Priority:** P1
- **Level:** integration
- **Given** approved PO `PO/26-27/00002` line 2 BOX of `BOLT-M12` with snapshot factor 100 (200 NOS) **When** the item's BOX factor is changed to 50 **Then** the PO still shows 200 NOS; new POs use 50.
- **Go test:** `TestItemUoM_FactorSnapshot`
- **Sources:** `erpnext/erpnext/stock/doctype/item/test_item.py:1012` (`test_item_uom_conversion_factor_overrides_global_factor`), decision D-MST-03

### MST-092 · Whole-number flag can't be turned on if fractional quantities exist
- **Priority:** P2
- **Level:** integration
- **Given** UoM MTR used for `CABLE-4C` with qty 12.5 on approved PO `PO/26-27/00004` **When** whole-number is turned on **Then** fails with "MTR is used with fractional quantities (e.g. PO/26-27/00004). Create a new UoM instead."
- **Go test:** `TestUoM_WholeNumberFlagChange`
- **Sources:** decision D-MST-18

### MST-093 · Disabled UoM can't be used on new conversion rows or lines
- **Priority:** P2
- **Level:** integration
- **Given** UoM BAG (stock UoM of `CEMENT-OPC`) disabled **When** added as a conversion row or used on a draft line **Then** fails with "UoM BAG is disabled. Pick another UoM."
- **Go test:** `TestUoM_DisabledUoM`
- **Sources:** `erpnext/erpnext/setup/doctype/uom/uom.json:43` (`enabled`), `frappe/frappe/desk/search.py:301`

### Tax code / GST rate

### MST-094 · Taxable tax code needs a rate above zero
- **Priority:** P1
- **Level:** unit
- **Given** tax code `GST0T` treatment Taxable rate 0 **Then** fails with "A Taxable tax code needs a GST rate above 0. Use Nil-Rated or Exempted for 0%."
- **Go test:** `TestTaxCode_TaxableNeedsRate`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/item_tax_template.py:15`, `india-compliance/india_compliance/gst_india/overrides/test_item_tax_template.py:24` (`test_validate_zero_tax_options`)

### MST-095 · Non-taxable treatments force rate 0
- **Priority:** P1
- **Level:** unit
- **Given** treatment Exempted (or Nil-Rated, Non-GST, Zero-Rated) with rate 5 **When** saved **Then** stored rate = 0.
- **Go test:** `TestTaxCode_NonTaxableZero`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/item_tax_template.py:15`, `india-compliance/india_compliance/gst_india/overrides/test_item_tax_template.py:14`

### MST-096 · Rate must be a notified GST rate
- **Priority:** P1
- **Level:** unit
- **Given** rates 0.25, 3, 5, 12, 18, 28, 40 (and 0.1, 1, 1.5, 6, 7.5) **Then** accepted. **Given** 10, 110, -5, 18.5 **Then** fails with "GST rate 10% is not a notified rate. Use one of 0.1, 0.25, 1, 1.5, 3, 5, 6, 7.5, 12, 18, 28, 40."
- **Go test:** `TestTaxCode_AllowedRates`
- **Sources:** `india-compliance/india_compliance/gst_india/constants/__init__.py:1426` (`GST_TAX_RATES`), `india-compliance/india_compliance/gst_india/overrides/test_item_tax_template.py:32` (`test_validate_tax_rates`), `india-compliance/india_compliance/gst_india/utils/transaction_data.py:642`

### MST-097 · Seeded tax codes
- **Priority:** P1
- **Level:** integration
- **Given** fresh install **Then** tax codes exist: GST0.25, GST3, GST5, GST12, GST18, GST28, GST40 (Taxable), NIL (Nil-Rated, 0), EXEMPT (Exempted, 0), NONGST (Non-GST, 0); all enabled.
- **Go test:** `TestTaxCode_Seed`
- **Sources:** `india-compliance/india_compliance/gst_india/constants/custom_fields.py:340` (treatment options), `:489` (default_gst_rate select from `GST_TAX_RATES`)

### MST-098 · Tax code exposes intra/inter-state split
- **Priority:** P1
- **Level:** unit
- **Given** GST18 **Then** intra-state = CGST 9 + SGST 9, inter-state = IGST 18; GST0.25 → 0.125 + 0.125 / 0.25; GST3 → 1.5 + 1.5 / 3. (Rounding of amounts belongs to NUM.)
- **Go test:** `TestTaxCode_Split`
- **Sources:** `india-compliance/india_compliance/gst_india/overrides/item_tax_template.py:46` (intra rate × 2 = GST rate)

### MST-099 · Rate and treatment locked once used
- **Priority:** P1
- **Level:** integration
- **Given** GST12 used on an approved PO **When** its rate is changed to 5 **Then** fails with "Rate can't change after this tax code is used. Disable it and create a new one." Name/description edits are allowed.
- **Go test:** `TestTaxCode_RateLocked`
- **Sources:** decision D-MST-03

### Project

### MST-100 · Project required fields and dates
- **Priority:** P1
- **Level:** unit
- **Given** project `PRJ-METRO` with start 2026-07-01 and end 2026-06-30 **Then** fails with "End date can't be before start date."
- **Go test:** `TestProject_Dates`
- **Sources:** `erpnext/erpnext/projects/doctype/project/project.py:121` (`validate_from_to_dates`)

### MST-101 · Project status
- **Priority:** P1
- **Level:** unit
- **Given** a new project **Then** status = Open. Allowed: Open, On Hold, Completed, Cancelled. **Given** status "Done" **Then** fails with "Status must be Open, On Hold, Completed or Cancelled."
- **Go test:** `TestProject_Status`
- **Sources:** `erpnext/erpnext/projects/doctype/project/project.py:346` (`validate_completed_status`)

### MST-102 · Completed or cancelled projects can't take new documents
- **Priority:** P1
- **Level:** integration
- **Given** project `PRJ-METRO` Completed **When** a draft PO with that project is saved **Then** fails with "Project PRJ-METRO is Completed. Reopen it or pick another project." **And** approved POs still show it; On Hold behaves like Open but warns.
- **Go test:** `TestProject_ClosedBlocksNewDocs`
- **Sources:** `erpnext/erpnext/projects/doctype/project/project.py:124` (disabled template refused), decision D-MST-04

### MST-103 · Project optional customer must be a customer
- **Priority:** P2
- **Level:** integration
- **Given** project `PRJ-METRO` customer set to vendor-only `SHREE` **Then** fails with "Shree Steel Traders is not marked as a customer."
- **Go test:** `TestProject_CustomerRole`
- **Sources:** `erpnext/erpnext/projects/doctype/project/test_project.py:210` (`test_project_linking_with_sales_order`)

### MST-104 · Project branch is required and must be enabled
- **Priority:** P2
- **Level:** unit
- **Given** project without branch **Then** fails with "Branch is required."
- **Go test:** `TestProject_Branch`
- **Sources:** F1

### Site / Warehouse

### MST-105 · Site/warehouse belongs to a branch and has a type
- **Priority:** P1
- **Level:** unit
- **Given** `PUNE-WH` type warehouse, branch PUNE **Then** saves. Missing branch → "Branch is required."; type other than warehouse/site → "Type must be Warehouse or Site."
- **Go test:** `TestSite_RequiredFields`
- **Sources:** `erpnext/erpnext/stock/doctype/warehouse/test_warehouse.py:38` (`test_naming`)

### MST-106 · Site may link to a project of the same branch
- **Priority:** P2
- **Level:** integration
- **Given** site `TECH-SITE` (branch BLR) linked to project `PRJ-METRO` (branch PUNE) **Then** fails with "The site's branch must match the project's branch (PUNE)."
- **Go test:** `TestSite_ProjectBranchMatch`
- **Sources:** `erpnext/erpnext/stock/doctype/warehouse/test_warehouse.py:185` (`test_parent_warehouse_must_be_group_of_same_company`)

### MST-107 · Site branch can't change once used
- **Priority:** P2
- **Level:** integration
- **Given** `PUNE-WH` used on approved GRN `GR/26-27/00001` **When** its branch changes to BLR **Then** fails with "Branch can't change after the site is used."
- **Go test:** `TestSite_BranchLocked`
- **Sources:** `erpnext/erpnext/stock/doctype/warehouse/test_warehouse.py:147` (`test_company_cannot_change_with_stock`)

### MST-108 · Disabling a branch with enabled sites
- **Priority:** P2
- **Level:** integration
- **Given** branch PUNE has enabled sites `PUNE-WH` and `METRO-SITE` **When** PUNE is disabled **Then** fails with "Disable the branch's sites first: METRO-SITE, PUNE-WH."
- **Go test:** `TestBranch_DisableWithSites`
- **Sources:** `erpnext/erpnext/stock/doctype/warehouse/warehouse.py:225` (child exists check)

### Employee

### MST-109 · Employee required fields
- **Priority:** P1
- **Level:** unit
- **Given** employee without name, branch or date of joining **Then** fails with "Name is required." / "Branch is required." / "Date of joining is required."
- **Go test:** `TestEmployee_RequiredFields`
- **Sources:** `erpnext/erpnext/setup/doctype/employee/employee.py:124`

### MST-110 · Employee dates
- **Priority:** P1
- **Level:** unit
- **Given** today 2026-10-24 (Asia/Kolkata) **When** date of birth 2026-11-01 **Then** "Date of birth can't be in the future." **When** relieving 2024-03-31 before joining 2024-04-01 **Then** "Relieving date can't be before date of joining."
- **Go test:** `TestEmployee_Dates`
- **Sources:** `erpnext/erpnext/setup/doctype/employee/employee.py:280` (`validate_date`)

### MST-111 · A user links to at most one active employee
- **Priority:** P1
- **Level:** integration
- **Given** user `deepak@vardhan.test` linked to active employee `EMP-007` **When** `EMP-009` (created in this test) is saved with the same user **Then** fails with "User deepak@vardhan.test is already linked to employee EMP-007." (a partial unique index on `employee.user_id` over active employees enforces this even under concurrent saves, CD-39) **When** `EMP-007` is Left (which disables the user, MST-119) and an Administrator re-enables `deepak@vardhan.test` **Then** linking it to `EMP-009` succeeds.
- **Go test:** `TestEmployee_UserUniqueAmongActive`
- **Sources:** `erpnext/erpnext/setup/doctype/employee/employee.py:339` (`validate_duplicate_user_id`), `odoo/addons/hr/models/hr_employee.py:390`

### MST-112 · Linked user must exist and be enabled
- **Priority:** P2
- **Level:** integration
- **Given** user id 999 that doesn't exist, or the disabled user `old@vardhan.test` **When** linked to an active employee **Then** fails with "Pick an existing, enabled user."
- **Go test:** `TestEmployee_UserMustExist`
- **Sources:** `erpnext/erpnext/setup/doctype/employee/employee.py:321`

### MST-113 · Employee can't report to themselves
- **Priority:** P1
- **Level:** unit
- **Given** `EMP-007` with reporting manager `EMP-007` **Then** fails with "An employee can't report to themselves."
- **Go test:** `TestEmployee_NoSelfManager`
- **Sources:** `erpnext/erpnext/setup/doctype/employee/employee.py:356` (`validate_reports_to`)

### MST-114 · Reporting-manager cycles are refused
- **Priority:** P1
- **Level:** integration
- **Given** the fixture chain EMP-004 → EMP-003 → EMP-002 (priya reports to ravi, ravi reports to meena) **When** EMP-002's manager is set to EMP-004 **Then** fails with "This would make a reporting loop (EMP-002 → EMP-004 → EMP-003 → EMP-002). Pick another manager."
- **Go test:** `TestEmployee_NoManagerCycle`
- **Sources:** `frappe/frappe/tests/test_nestedset.py:141` (`test_recursion`), `frappe/frappe/utils/nestedset.py:252`, `odoo/odoo/addons/base/tests/test_res_partner.py:420` (`test_res_partner_recursion`)

### MST-115 · Reporting-manager loop check holds for any chain (fuzz)
- **Priority:** P2
- **Level:** unit
- **Given** random employee forests (starting from the fixture reporting chain) and random manager updates **Then** after every accepted update, following managers from any employee ends within N steps without revisiting a node.
- **Go test:** `FuzzEmployee_ManagerChainAcyclic`
- **Sources:** decision D-MST-22

### MST-116 · Reporting manager must be active
- **Priority:** P1
- **Level:** integration
- **Given** EMP-008 is Left **When** EMP-007 is saved with manager EMP-008 **Then** fails with "Reporting manager EMP-008 has left. Pick an active manager."
- **Go test:** `TestEmployee_ManagerActive`
- **Sources:** `erpnext/erpnext/setup/doctype/employee/employee.py:299`

### MST-117 · Can't mark someone Left while active employees report to them
- **Priority:** P1
- **Level:** integration
- **Given** EMP-007 (Deepak Nair) reports to EMP-004 **When** EMP-004 status = Left **Then** fails with "EMP-007 (Deepak Nair) still reports to EMP-004. Move them to another manager first."
- **Go test:** `TestEmployee_LeftWithReportees`
- **Sources:** `erpnext/erpnext/setup/doctype/employee/test_employee.py:15` (`test_employee_status_left`), `erpnext/erpnext/setup/doctype/employee/employee.py:299`

### MST-118 · Left requires a relieving date
- **Priority:** P1
- **Level:** unit
- **Given** status Left and no relieving date **Then** fails with "Enter the relieving date."
- **Go test:** `TestEmployee_LeftNeedsRelievingDate`
- **Sources:** `erpnext/erpnext/setup/doctype/employee/employee.py:319`

### MST-119 · Marking an employee Left disables the linked user
- **Priority:** P1
- **Level:** integration
- **Given** EMP-007 active with enabled user `deepak@vardhan.test` **When** marked Left **Then** the user is disabled (AUTH revokes sessions) and the change is audited on both records; marking Active again re-enables the user.
- **Go test:** `TestEmployee_LeftDisablesUser`
- **Sources:** `erpnext/erpnext/setup/doctype/employee/employee.py:325` (`update_user_status`), `erpnext/erpnext/setup/doctype/employee/test_employee.py:27` (`test_user_has_employee`)

### MST-120 · Left employees are hidden from new documents
- **Priority:** P1
- **Level:** integration
- **Given** EMP-008 is Left **When** a new expense claim picks an employee **Then** EMP-008 isn't offered; a draft already for EMP-008 (created before it was Left) fails on save with "Employee EMP-008 has left. Pick an active employee."
- **Go test:** `TestEmployee_LeftHidden`
- **Sources:** `erpnext/erpnext/accounts/party.py:920` (Employee not active message), decision D-MST-04

### Expense type

### MST-121 · Expense type basics
- **Priority:** P1
- **Level:** unit
- **Given** expense type `TRAVEL` "Local travel" **Then** saves with no default HSN/SAC or tax code.
- **Go test:** `TestExpenseType_Basic`
- **Sources:** `hrms/hrms/hr/doctype/expense_claim_type/expense_claim_type.py:27`

### MST-122 · Expense type default SAC/HSN is validated
- **Priority:** P1
- **Level:** unit
- **Given** default code `9964` **Then** fails with "SAC code must be 6 digits starting with 99." **And** `996411` (local transport) passes; a goods HSN `4820` (stationery) passes the MST-071 rules.
- **Go test:** `TestExpenseType_DefaultHSN`
- **Sources:** `india-compliance/india_compliance/gst_india/doctype/gst_hsn_code/gst_hsn_code.py:115`

### MST-123 · Expense type default tax code is copied to new expense lines
- **Priority:** P2
- **Level:** integration
- **Given** expense type `LODGING` with default SAC `996311` and tax code GST18 **When** an expense line picks `LODGING` **Then** the line gets SAC 996311 and GST18, both editable on the line.
- **Go test:** `TestExpenseType_DefaultsCopied`
- **Sources:** `odoo/addons/hr_expense/models/product_template.py:12`

### Payment terms

### MST-124 · Portions must total 100%
- **Priority:** P1
- **Level:** unit
- **Given** rows [50% at 30 days] **Then** fails with "Payment portions add up to 50%. They must total 100%." **And** [50% at 0 days, 50% at 30 days] saves. 33.33+33.33+33.34 saves; 33.33×3 fails.
- **Go test:** `TestPaymentTerms_Total100`
- **Sources:** `erpnext/erpnext/accounts/doctype/payment_terms_template/test_payment_terms_template.py:17` (`test_create_template`), `erpnext/erpnext/accounts/doctype/payment_terms_template/payment_terms_template.py:33`

### MST-125 · Days can't be negative or fractional
- **Priority:** P1
- **Level:** unit
- **Given** row 100% at -30 days or 7.5 days **Then** fails with "Row 1: Days must be a whole number, 0 or more."
- **Go test:** `TestPaymentTerms_DaysNonNegative`
- **Sources:** `erpnext/erpnext/accounts/doctype/payment_terms_template/test_payment_terms_template.py:47` (`test_credit_days`)

### MST-126 · Duplicate rows are refused
- **Priority:** P1
- **Level:** unit
- **Given** [50% at 30 days, 50% at 30 days] **Then** fails with "Row 2 repeats row 1 (30 days). Merge them into one row of 100%."
- **Go test:** `TestPaymentTerms_NoDuplicateRows`
- **Sources:** `erpnext/erpnext/accounts/doctype/payment_terms_template/test_payment_terms_template.py:111` (`test_duplicate_terms`), `erpnext/erpnext/accounts/doctype/payment_terms_template/payment_terms_template.py:41`

### MST-127 · Portion must be above 0
- **Priority:** P1
- **Level:** unit
- **Given** [0% at 0 days, 100% at 30 days] **Then** fails with "Row 1: Portion must be more than 0%."
- **Go test:** `TestPaymentTerms_PortionPositive`
- **Sources:** `erpnext/erpnext/accounts/doctype/payment_terms_template/payment_terms_template.py:33`

### MST-128 · Due dates computed from document date
- **Priority:** P1
- **Level:** unit
- **Given** terms [50% at 0, 50% at 30] and document date 2026-01-31, total ₹1,18,000 **Then** schedule = (2026-01-31, ₹59,000), (2026-03-02, ₹59,000). **Given** 100% at 30 from 2024-02-15 **Then** 2024-03-16 (leap year).
- **Go test:** `TestPaymentTerms_DueDates`
- **Sources:** `erpnext/erpnext/buying/doctype/supplier/test_supplier.py:39` (`test_supplier_default_payment_terms`), `erpnext/erpnext/accounts/doctype/payment_terms_template/test_payment_terms_template.py:88`

### MST-129 · Schedule amounts sum exactly to the total
- **Priority:** P2
- **Level:** unit
- **Given** terms 33.33/33.33/33.34 and total ₹1,000.00 **Then** amounts 333.30, 333.30, 333.40 and the last row absorbs rounding so the sum is exactly ₹1,000.00 (rounding per NUM).
- **Go test:** `TestPaymentTerms_ScheduleSumsExactly`
- **Sources:** relies on NUM

## Deferred and excluded
| Scenario | Source | later/never | Reason |
|---|---|---|---|
| Party merge on rename | `erpnext/erpnext/stock/doctype/item/test_item.py:95`, `erpnext/erpnext/buying/doctype/supplier/supplier.py:268` | later | Risky bulk relinking; disable + new record suffices for v1 |
| Party on hold / frozen (block POs, payments) | `erpnext/erpnext/buying/doctype/supplier/test_supplier.py:153`, `erpnext/erpnext/selling/doctype/customer/test_customer.py:241`, `:277` | later | Disable covers v1; payments live in Zoho Books |
| Customer credit limit and overdue blocking | `erpnext/erpnext/selling/doctype/customer/test_customer.py:324-548` | later | Needs receivables from Zoho; Sales module |
| Party accounts, price lists, currency per party | `erpnext/erpnext/buying/doctype/supplier/test_supplier.py:149`, `test_customer.py:72` | never | No ledger (Zoho Books); INR only (F2) |
| Internal supplier/customer (inter-company) | `erpnext/erpnext/buying/doctype/supplier/test_supplier.py:107` | never | Single company (F1) |
| Supplier/customer portal users | `erpnext/erpnext/buying/doctype/supplier/test_supplier.py:323` | never | No portal |
| GSTIN lookup / autofill and GSTIN status verification via GST API | `india-compliance/india_compliance/gst_india/overrides/party.py:54`, `odoo/addons/l10n_in/models/res_partner.py:137` | later | Needs paid GSP API account |
| GST categories Deemed Export, UIN Holders, Tax Deductor, Tax Collector, ISD | `india-compliance/india_compliance/gst_india/constants/__init__.py:1461` | later | Rare for this company; the regexes are already in the validator |
| TAN derived from GSTIN | `odoo/addons/l10n_in/models/res_partner.py:126` | later | TDS is in Zoho Books |
| Bulk update of all docs with previous GSTIN | `india-compliance/india_compliance/gst_india/overrides/party.py:80` | never | Contradicts snapshots (D-MST-03) |
| Address/contact shared by several parties (dynamic links) | `frappe/frappe/contacts/doctype/address/address.py:83` (`link_address`, dynamic links) | never | D-MST-12 |
| Country-specific address templates | `frappe/frappe/contacts/doctype/address/test_address.py:46` | never | India-only formatting, English only |
| Google Contacts sync | `frappe/frappe/contacts/doctype/contact/contact.py:81` | never | Out of scope |
| HSN minimum-digits setting (6 or 8 only for turnover > ₹5 Cr) | `india-compliance/india_compliance/gst_india/doctype/gst_hsn_code/test_gst_hsn_code.py:40` | later | Accept 4/6/8 for now; add setting if turnover grows |
| HSN master with tax per HSN, auto-apply to items | `india-compliance/india_compliance/gst_india/overrides/item.py:33` | later | Tax code on item is enough for v1 |
| Item tax templates with validity dates and net-rate ranges (e.g. apparel slab by price) | `erpnext/erpnext/stock/doctype/item/item.py:492` | later | Rare; add when Sales Invoice ships |
| Compensation cess | India Compliance cess tax types (not cited by line) | later | Not relevant to current items |
| Item variants, attributes, barcodes, manufacturers | `erpnext/erpnext/stock/doctype/item/test_item.py:446-1149` | later | Not needed for v1 catalogue |
| SAC required for service items | `india-compliance/india_compliance/gst_india/overrides/item.py:25` | later | Becomes required with Sales Invoice |
| Global UoM conversion table and prefill (Kg↔Gram, Pound↔Gram) | `erpnext/erpnext/stock/doctype/item/test_item.py:1052-1076`, `odoo/addons/uom/tests/test_uom.py:18` | later | Per-item conversion is enough (D-MST-17) |
| Packaging rounding (round to multiple of package) | `odoo/addons/uom/tests/test_uom.py:38`, `:49` | later | Stock module |
| Warehouse tree (group/ledger, nested set) | `erpnext/erpnext/stock/doctype/warehouse/test_warehouse.py:21-96` | later | D-MST-20 |
| Warehouse can't be disabled/deleted while stock exists | `erpnext/erpnext/stock/doctype/warehouse/test_warehouse.py:200`, `erpnext/erpnext/stock/doctype/warehouse/warehouse.py:215` | later | Stock module; v1 GRN doesn't keep balances |
| Warehouse accounts / in-transit warehouses | `erpnext/erpnext/stock/doctype/warehouse/test_warehouse.py:168`, `:209` | never | No ledger |
| Project templates, tasks, % complete, costing/billing totals | `erpnext/erpnext/projects/doctype/project/test_project.py:36-234` | later | Projects module |
| Employee salary currency, HR-user permissions, auto-create user | `erpnext/erpnext/setup/doctype/employee/test_employee.py:36-135` | later/never | HR out of scope; INR only |
| Payment term discounts, end-of-month basis, credit months | `erpnext/erpnext/accounts/doctype/payment_terms_template/test_payment_terms_template.py:65`, `:88`; `erpnext/erpnext/buying/doctype/supplier/test_supplier.py:52` | later | Simple day-based terms cover v1 |
| Multi-company branches, parent company | `erpnext/erpnext/setup/doctype/company/test_company.py:257` | never | F1 |
| Company chart of accounts, abbreviations | `erpnext/erpnext/setup/doctype/company/test_company.py:24`, `company.py:344` | never | No ledger |

## Interfaces this area needs from others
- **DOC** provides the registry in `internal/kernel/doctype` (CD-02). Each master registers as a DocType with `Submittable` false, `Hooks` nil and a `Master` lookup (`doctype.MasterLookup` returning `MasterInfo{Code, Name, Disabled, Blocked, Warning}`, CD-21). DOC validates every link to a master through that lookup on draft save, submit and approve: not found → DOC-032, disabled → DOC-034's wording (MST-010, CD-38), `Blocked` → that message as given, prefixed "Line N: " on lines (MST-102, MST-120), `Warning` → an entry in `doc.Result.Warnings` (On Hold projects) (CD-68). DOC shows a linked master as `{Code} {Name}` from the `MasterInfo` (CD-68), so MST fills both. Each master's `Fields` carry the CD-42 extras (`ListPriority`, `Options`, `MaxLen`, `Hint`, `Step`) that the generic list, form and wizard use. Project and Site declare `ProjectFields`/`SiteFields` `["id"]` so restrictions apply to those masters themselves (CD-64). Errors reaching a handler implement `doctype.HTTPError` (CD-44); a missing record is `doctype.ErrNotFound` (CD-54). Documents snapshot display fields through MST's snapshot functions (D-MST-03).
- **DOC** provides `doc.Engine.References(ctx, tx, table, id, limit) ([]Reference, error)` with DocType, field, id, display (document number, `Draft 42` (spec R1), or `{Label} {code}` for masters) and state (CD-22), covering drafts, cancelled documents and other masters. It is used for delete (MST-014), locking fields after non-draft use (MST-075, MST-090, MST-099, MST-107) and role untick (MST-043, which needs the field). `IsReferenced` stays as a convenience.
- **DOC** provides the audit trail write for master create/update/disable/delete/rename (F6) through `audit.Writer` (CD-03) and optimistic concurrency (row version) on master saves.
- **AUTH** provides users and `auth.Service.User(ctx, tx, u) (UserInfo, bool, error)` (MST-111, MST-112), `DisableUser(ctx, tx, u, reason)` used when an employee is marked Left (MST-119; also ends sessions) and `EnableUser(ctx, tx, u)` when marked Active again (CD-12); the Masters Manager role (`HasRole`); per-DocType rights (`Can`) read from each master's `DefaultRights`. Production `DefaultRights` give Masters Manager the master rights and give `read` (all) to every role whose documents link to that master (CD-10).
- **AUTH and APR** consume `auth.Directory`, which MST implements (CD-13): `EmployeeForUser`, `UserForEmployee`, `ManagerChain` (direct manager first, acyclic, active managers), `DirectReports` (active only), `ProjectCodes`, `SiteCodes`, each taking `tx`. AUTH uses it for `own`/`team` scopes and restrictions; APR for reporting managers.
- **FILE** declares `print.Letterhead`; MST's `company` package implements it (`ForBranch`) from the company, branch and branch address (CD-30); WP3 wires it.
- **NUM** provides quantity precision and rounding used by the whole-number and conversion checks (MST-085, MST-087, MST-088), the CGST/SGST/IGST amount split (MST-098 provides only rates), money rounding for payment schedules (MST-129), "today" in Asia/Kolkata (MST-110), the formatters (CD-33) and the GST state names (`gstcalc.StateName`, CD-31). NUM's CSV import calls MST's per-master `csvio.RowFunc` (registered with `csvio.RegisterImporter`, CD-32) for every row inside one transaction, so no GSTIN/HSN check is bypassed on import; MST's messages appear prefixed "Row N: " (CD-38). The `row` is keyed by field `Name`, cells trimmed and formula-unescaped, links already resolved from code to id; csvio writes the "created by import" audit entry and flags the context so MST skips its own `create` entry (CD-60). The Item template is "Code,Name,Type,UoM,HSN/SAC,Tax Code,Standard Rate,Disabled" (CD-37).
- **NUM / list** provides the picker search used by MST-009 with an `include_disabled` flag.
- **UI** provides the generic list, form and wizard components; Party and Employee create wizards run in the wizard's Master mode, holding step values in the server-side session and saving once at "Check and save"; edits of existing masters save per section (CD-50).
- **Brief owner:** the example GSTIN `27AABCS1429B1Z5` in BRIEF.md fails the check digit; resolved by spec R5, all areas use the GSTINs in `00-fixtures.md`.
