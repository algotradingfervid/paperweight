# Canonical test fixtures

Every test case in `01-` to `07-` uses these values, so that tests from different areas can share one seed (`internal/testkit/seed`). All GSTINs pass the India Compliance check-digit algorithm (verified 24 October 2026).

## Company and branches (F1)

| Field | Value |
|---|---|
| Company legal name | Vardhan Infra Projects Pvt Ltd |
| Company PAN | `AAFCV2449G` |
| Branch `PUNE` | Pune Works, Maharashtra (state 27), GSTIN `27AAFCV2449G1Z7`, head office |
| Branch `BLR` | Bengaluru Site Office, Karnataka (state 29), GSTIN `29AAFCV2449G1Z3` |

## Parties

| Code | Name | PAN | GST category | Address GSTIN(s) |
|---|---|---|---|---|
| `SHREE` | Shree Steel Traders | `AABCS1429B` | Registered Regular | Mumbai `27AABCS1429B1ZU` (primary billing); Thane `27AABCS1429B2ZT`; Delhi `07AABCS1429B1ZW` |
| `BHARAT` | Bharat Tools | `AABCT1332L` | Registered Regular | Bengaluru `29AABCT1332L1ZA` |
| `RAJKOT` | Rajkot Castings | `AAACR5055K` | Registered Regular | Rajkot `24AAACR5055K1ZD` |
| `COROM` | Coromandel Cables | `AAACT2727Q` | Registered Regular | Chennai `33AAACT2727Q1Z3` |
| `LOCAL` | Ganesh Hardware (unregistered) | none | Unregistered | Pune address, no GSTIN |

Invalid-checksum GSTIN used in negative tests: `27AABCS1429B1Z5`.

## Items, units and tax codes

| Item code | Name | Type | HSN/SAC | Stock UoM | Other UoM | GST |
|---|---|---|---|---|---|---|
| `MS-ANGLE-50` | MS Angle 50x50x5 | Goods | `7216` | kg (fractional) | none | 18% |
| `BOLT-M12` | Hex Bolt M12x60 | Goods | `7318` | Nos (whole number) | Box = 100 Nos | 18% |
| `WIRE-18G` | Binding Wire 18G | Goods | `7217` | kg | none | 18% |
| `CABLE-4C` | Armoured Cable 4 core 16 sq mm | Goods | `8544` | m (fractional) | Drum = 500 m | 18% |
| `CEMENT-OPC` | OPC Cement 53 Grade | Goods | `2523` | Bag (whole number) | none | 28% (older documents) / 18% |
| `SVC-CRANE` | Crane hire | Service | SAC `997313` | Hour | none | 18% |

Unit codes (GST UQC): Nos→`NOS`, kg→`KGS`, m→`MTR`, Box→`BOX`, Drum→`DRUM` (UQC `DRM`), Bag→`BAG`, Hour→`HRS`. The full seeded list is in MST-078 (`06-masters.md`): NOS, KGS, MTR, LTR, BOX, SET, BAG, TON, SQM, HRS, DRUM; NOS, BOX, SET, BAG, DRUM are whole-number; HRS uses UQC `OTH`.

Tax codes seeded: Taxable 0.25%, 3%, 5%, 12%, 18%, 28%, 40%; Nil-Rated, Exempted, Non-GST at 0%. Percent values are stored in millipercent (18% = `18000`).

## Projects, sites, expense types

- Projects: `PRJ-METRO` (Pune Metro Depot), `PRJ-TECHPARK` (Whitefield Tech Park, Bengaluru).
- Sites/warehouses: `PUNE-WH` (warehouse, branch PUNE), `METRO-SITE` (site, branch PUNE, project PRJ-METRO), `TECH-SITE` (site, branch BLR, project PRJ-TECHPARK).
- Expense types: `TRAVEL`, `FOOD`, `FUEL`, `LODGING`, `SITE-MISC`.

## Users, roles and employees

| User (email) | Name | Roles | Employee / reporting manager | Restrictions |
|---|---|---|---|---|
| `admin@vardhan.test` | Anil Kapoor | Administrator | EMP-001, none | none |
| `meena@vardhan.test` | Meena Iyer | Director | EMP-002, none | none |
| `ravi@vardhan.test` | Ravi Kulkarni | Purchase Manager, Employee | EMP-003, reports to EMP-002 | none |
| `priya@vardhan.test` | Priya Shah | Purchase User, Employee | EMP-004, reports to EMP-003 | Projects: PRJ-METRO |
| `suresh@vardhan.test` | Suresh Patil | Stores, Employee | EMP-005, reports to EMP-003 | Sites: PUNE-WH, METRO-SITE |
| `kavya@vardhan.test` | Kavya Rao | Masters Manager, Employee | EMP-006, reports to EMP-002 | none |
| `deepak@vardhan.test` | Deepak Nair | Employee | EMP-007, reports to EMP-004 | none |
| `old@vardhan.test` | Former Staff | Employee (disabled user) | EMP-008, status Left | none |

Canonical roles: **Administrator, Director, Purchase Manager, Purchase User, Stores, Masters Manager, Employee**. New users get Employee by default (D-AUTH-22).

Test password for all fixture users: `Correct-Horse-42`.

### Seeded rights matrix (rows every area relies on)

| Role | DocType | Rights (scope) |
|---|---|---|
| Administrator | every DocType | every applicable right (all) |
| Purchase Manager | Purchase Order / `fixture.Order` | read, approve, cancel, amend, print, export (all) |
| Purchase User | Purchase Order / `fixture.Order` | read, create, write, submit, print, export (all); delete (own) |
| Stores | GRN / `fixture.Receipt` | read, create, write, submit, print (all) |
| Stores | Purchase Order / `fixture.Order` | read (all) |
| Employee | Expense Claim | read, create, write, submit, print (own) |
| Employee | Expense Claim / `fixture.Claim` | read, approve (team: claims of employees who report directly to me) |
| Director | Purchase Order / `fixture.Order`, Expense Claim / `fixture.Claim` | read, approve, print, export (all) |
| Masters Manager | every master | read, create, write, delete, export, import (all) |

Scopes are `own`, `team` and `all` (CD-11). So Priya creates and submits orders, Ravi approves them (and has no Withdraw button), Suresh receives goods, and Priya approves Deepak's expense claims as Deepak's reporting manager. A test that needs anything else adds it in its own setup.

Permission-denied (403) message, owned by AUTH and used everywhere: "You don't have permission to {right} {DocType}. Ask your administrator for a role that allows it."

## Approval rules (test seed)

| Document type | From amount | Approver | Fallback role | Self-approval |
|---|---|---|---|---|
| `fixture.Order` / Purchase Order | ₹50,000.00 | role Purchase Manager | none | off |
| `fixture.Order` / Purchase Order | ₹5,00,000.00 | role Director | none | off |
| Expense Claim / `fixture.Claim` | ₹0.00 | reporting manager | Director | off |

GRN (`fixture.Receipt`) has no approval rule, so submit goes straight to approved.

## Numbering (D-NAM-01, -04, -05)

| Document type | Template | First number in FY 2026-27 |
|---|---|---|
| `fixture.Order` / Purchase Order | `PO/{FY}/{#####}` | `PO/26-27/00001` |
| `fixture.Receipt` / GRN | `GRN/{FY}/{#####}` | `GRN/26-27/00001` |
| `fixture.Claim` / Expense Claim | `EXP/{FY}/{#####}` | `EXP/26-27/00001` |
| Expense Claim | `EXP/{FY}/{#####}` | `EXP/26-27/00001` |

Unnumbered documents display as `Draft 42` (their ID). Amendments: `PO/26-27/00042-1`.

## Dates and clock

The test clock is fixed at **2026-10-24 10:30 IST** (`2026-10-24T05:00:00.000Z`), which falls in FY **2026-27** (short `26-27`). Back-dated cases use 2026-03-31 (FY 2025-26).

## Reference PO used in calculation and print tests

PO for `SHREE` (Mumbai, Maharashtra) to branch PUNE (Maharashtra), so intra-state (CGST + SGST):

| Line | Item | Qty | Rate | Amount |
|---|---|---|---|---|
| 1 | MS-ANGLE-50 | 1,200.000 kg | ₹62.5000 | ₹75,000.00 |
| 2 | WIRE-18G | 50.000 kg | ₹96.0000 | ₹4,800.00 |

Taxable ₹79,800.00; CGST 9% ₹7,182.00; SGST 9% ₹7,182.00; grand total ₹94,164.00; rounded total ₹94,164.00; in words "Rupees Ninety Four Thousand One Hundred Sixty Four Only". Amount is ≥ ₹50,000, so it needs Purchase Manager approval.
