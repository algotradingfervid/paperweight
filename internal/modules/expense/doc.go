// Package expense is the Expense module (Expense spec v0.6): the Expense Claim
// (expense.Claim) and Employee Advance (expense.Advance) DocTypes, their hooks,
// and the services that act for a principal. Its tables are migrations
// 10350–10499 (PO spec §14.5); its sqlc.yaml reads only those and the
// migrations of the tables it reads (CD-117). Like every module service
// package it never imports internal/ui (CD-237); its screens are in
// expense/web.
//
// Each later task owns its own files: hooks.go composes the hooks from
// functions declared in each owner's stubs_<task>.go, which that task
// replaces (plan MOD-EXP, File ownership).
package expense
