# Research brief: core platform test cases

Every research agent reads this file first. It fixes the decisions already made, so that agents working in parallel stay consistent.

## The product

An internal business app for a 15 to 20 person Indian company. It's built in stages, one module at a time. The first modules are Purchase Order, GRN (goods receipt) and Expenses. Later modules include Delivery Challan, Sales Invoice, Projects and Stock. Accounting, GST filing and payments live in **Zoho Books**; this app never keeps a general ledger.

Out of scope for ever: multi-tenancy (one company per install). Out of scope for v1: multi-currency (INR only), multi-company, SSO, website/portal, translations (English only), email inbox, live updates.

## Stack (fixed)

- Go, a single binary. SQLite (`modernc.org/sqlite`, WAL mode, `busy_timeout`).
- `sqlc` for typed queries, `goose` for SQL migrations, `templ` + HTMX for screens, minimal Alpine.js only where unavoidable.
- DocTypes are defined in Go and turned into code at build time (no runtime metadata, no adding fields from the browser in v1).
- Tests: Go `testing`, table-driven; Go fuzzing for invariants; browser end-to-end tests run outside the binary.
- **Open-source only**, and always the **latest stable version** of every tool and library. If you recommend a library, name an open-source one and say its licence. **Prefer existing, well-maintained Go libraries over writing our own** wherever one fits.

## The core we're specifying (this work)

| Area | Prefix | Covers |
|---|---|---|
| Identity & permissions | AUTH | users, login, sessions, password rules, roles, per-DocType rights, record-level rules, disabled users |
| Naming series | NAM | document numbers, financial-year reset, padding, prefixes, concurrency, gaps, amend suffix |
| Document lifecycle & audit | DOC | create, save, validate, child tables (lines), submit, cancel, amend, delete rules, locking, linked-document checks, optimistic concurrency, audit trail / version history |
| Approvals | APR | approval rules by role and amount, self-approval, reject with reason, re-submit, multi-level later |
| Files & printing | FILE | attachments (size, type, private), print templates, amount in words (Indian lakh/crore), PDF |
| Masters | MST | company & branches (GSTIN), party (vendor/customer), address & contact, item, UoM & conversion, tax code (GST rate, HSN/SAC), project, site/warehouse, employee, expense type; disable vs delete; duplicates; GSTIN/PAN validation |
| Numbers, dates & lists | NUM | money, quantity and rate precision and rounding; GST split (CGST+SGST vs IGST); financial year and dates; list filtering, sorting, search, pagination; CSV import/export validation |

## Decisions already fixed (don't re-decide; flag in your file if the sources strongly disagree)

- **F1 Single company** per install; the company may have several branches, each with its own optional GSTIN and state.
- **F2 INR only.** No exchange rates.
- **F3 Primary keys** are integer IDs. Masters also have a unique human `code`; documents have a unique `number` from the naming series.
- **F4 Masters are disabled, not deleted,** once anything references them. Never-referenced masters may be deleted.
- **F5 Document states** (one state machine for every document type):
  - `draft` (editable) → `pending_approval` (locked) → `approved` (effects applied, e.g. GRN raises PO received qty) → `closed` (terminal, nothing pending).
  - `pending_approval` → `rejected` → back to `draft` (editable again; the reason is kept).
  - If no approval rule applies, submit goes straight from `draft` to `approved`.
  - `approved` → `cancelled` (effects reversed; blocked while a later, non-cancelled document links to it).
  - `cancelled` → **amend** creates a new `draft` copy that links back to the original.
  - Submitted documents are never edited and never deleted. Drafts may be deleted.
- **F6 Audit trail** is append-only and records who, when, what changed (field-level before/after) and which transition.
- **F7 Times** are stored in UTC and shown in Asia/Kolkata. Business dates are plain dates. The financial year runs 1 April to 31 March.

## How to work

1. Read the relevant tests **and** the code they test in these local clones (read-only):
   - Frappe: `reference/frappe`
   - ERPNext: `reference/erpnext`
   - HRMS: `reference/hrms`
   - Odoo (Community, sparse): `reference/odoo` (core in `odoo/addons/base`, modules in `addons/`)
   - Tryton: `reference/tryton`
   - India Compliance (GST rules for ERPNext): `reference/india-compliance`
   Don't run `git` commands that change these clones.
2. List every scenario relevant to your area. Mark each **v1**, **later** or **never**, with a reason for later/never.
3. Where the systems disagree or a choice is needed, **decide**. Default to ERPNext's behaviour (closest to Indian SME practice). Pick Odoo's or Tryton's when it is simpler or safer for a 20-person company. Record each decision with what each system does and your reason.
4. Write unified test cases for every v1 scenario, merging duplicates across sources.
5. Verify every source reference you cite by opening the file. Cite `path:line` relative to `reference/`.

## Output: one file per area

Write only your own file: `docs/tests/core/<NN>-<area>.md`. Use exactly this structure:

```markdown
# <Area name> (<PREFIX>)

## Scope
<3 to 6 lines: what this area owns, and what it relies on from other areas, by prefix>

## Decisions
| ID | Question | ERPNext/Frappe | Odoo | Decision | Why |
|---|---|---|---|---|---|
| D-<PREFIX>-01 | ... | ... | ... | ... | ... |

## Test cases

### <PREFIX>-001 · <one-line behaviour>
- **Priority:** P1 (must have for v1) | P2 (v1, can follow the first release)
- **Level:** unit | integration | e2e
- **Given** ... **When** ... **Then** ...   (exact values: numbers, strings, error messages)
- **Go test:** `TestXxx_Yyy`
- **Sources:** `frappe/...:123` (`test_name`), `odoo/...:45` (`test_name`)

## Deferred and excluded
| Scenario | Source | later/never | Reason |
|---|---|---|---|

## Interfaces this area needs from others
<bullets: what you assumed another area provides, e.g. "NAM provides Next(series, date) at transition to pending_approval">
```

Error messages in test cases are short, plain English, and say how to fix the problem, e.g. "Quantity must be more than 0." Use realistic Indian values (₹, GSTINs such as `27AABCS1429B1ZU`, states, HSN codes).

Aim for completeness over brevity: every real edge case in the sources that applies to v1 should become a test case. Typical size is 40 to 120 test cases per area.
