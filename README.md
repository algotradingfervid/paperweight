# internaltools

Internal business tools for a small Indian company: purchase orders, goods receipts (GRN), expense claims and, later, delivery challans, projects and invoices. It is one Go binary on SQLite, built module by module on a shared core (users and rights, numbering, document lifecycle, approvals, attachments and printing, masters, and number/GST rules). Accounting stays in Zoho Books.

**Status:** specification and plans only. No code has been written yet.

## Stack

Go 1.27, SQLite (`modernc.org/sqlite`, pure Go), sqlc, goose, chi, scs, templ, htmx 2, Alpine.js (CSP build), hand-written CSS bundled by esbuild. Open-source only. Full table: spec §2.

## Read this first

1. [`docs/PROGRESS.md`](docs/PROGRESS.md): current status and build order.
2. [`docs/specs/2026-10-24-core-platform-spec.md`](docs/specs/2026-10-24-core-platform-spec.md): the core platform spec.
3. [`docs/specs/2026-10-24-design-system.md`](docs/specs/2026-10-24-design-system.md): design system and front-end spec (mobile-first, light mode only, no horizontal scrolling, wizards for complex forms).
4. [`docs/specs/contract-changes.md`](docs/specs/contract-changes.md): decision log.
5. [`docs/tests/core/README.md`](docs/tests/core/README.md): 722 test cases that define the behaviour.
6. [`docs/superpowers/plans/`](docs/superpowers/plans/): one implementation plan per work package, written for parallel development.

## Reference sources

The test cases cite Frappe, ERPNext, HRMS, Odoo, Tryton and India Compliance by file and line. Those projects aren't included here. To follow the citations, clone them into `reference/` (ignored by git), for example `git clone --depth 1 https://github.com/frappe/erpnext reference/erpnext`.
