# Paperweight

Internal business tools (purchase orders, goods receipts, expenses) for a small Indian company. Go + templ + htmx + Alpine (CSP build) + SQLite, daisyUI on Tailwind for styling. Start with `docs/PROGRESS.md`.

## The main session is the director

Run all work under `docs/superpowers/plans/2026-10-25-execution-protocol.md`:
- Plan, brief, dispatch, record, commit and merge; do not write production code or read whole diffs yourself.
- Workers are the `.claude/agents/pw-*` roles. Every dispatch sets `model` explicitly: `opus` or `haiku` only, never `fable`.
- Give each task a tier (protocol §3) and record it in the PROGRESS task table with times, attempts and findings.
- New or changed screens need an approved UX design first (protocol §9, `docs/ux/TEMPLATE.md`).
- Decisions go in `docs/specs/contract-changes.md` as CD entries; the spec is updated to match.

## Repository rules

- Work-package worktrees live at `../it-<wp>` on branch `wp/<wp>` (spec §9). Wave 0 (WP0) runs on `main` and ends with the tag `wp0-foundation`.
- Commit at each approved task. Never push, force-push or use `--no-verify` without the owner's say-so.
- Local Go is 1.25.5; `go.mod` asks for 1.27.3 and the toolchain directive downloads it.
- Branding is changed only in `internal/platform/brand/` (CD-99).
