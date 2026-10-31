// Package purchase holds the Purchase Order and Purchase Request DocTypes and
// their services (PO spec §3, §20). Hooks and the frozen tx calls read what
// they need beyond the tx from one Wiring keyed by database (CD-284);
// everything that acts for a principal is a method on *Service. Its tables
// are migrations 10050–10199 (spec §14.5). Like every module service package
// it never imports internal/ui (CD-237); its screens are in purchase/web.
package purchase
