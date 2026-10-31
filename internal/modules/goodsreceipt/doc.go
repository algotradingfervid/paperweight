// Package goodsreceipt holds the Goods Receipt DocType and its services (GRN
// spec §3 to §12): a receipt against one approved purchase order, its bill
// details and reports. Hooks read what they need beyond the tx from one
// Wiring keyed by database (CD-284). hooks.go composes each hook from the
// functions of the plan's hook table; until a task builds its function, it is
// a stub in that task's stubs_<task>.go that means "no extra rule" (CD-256).
// Its tables are migrations 10200–10349 (PO spec §14.5). Like every module
// service package it never imports internal/ui (CD-237); its screens are in
// goodsreceipt/web.
package goodsreceipt
