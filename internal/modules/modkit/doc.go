// Package modkit holds what the business modules share (PO spec §14, §22):
// business settings with the period lock, and comments on module documents.
// Its tables are migrations 10000–10049; its sqlc.yaml reads only those
// (CD-117). Like every module service package it never imports internal/ui
// (CD-237); its screens are in modkit/web.
package modkit
