// Package migrations embeds the test-only SQL migrations of mstfixture. It is
// a leaf so msttest can apply them without importing mstfixture, which imports
// msttest.
package migrations

import "embed"

// FS holds all *.sql files in internal/masters/mstfixture/migrations.
//
//go:embed *.sql
var FS embed.FS
