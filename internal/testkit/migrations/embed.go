// Package migrations embeds the test-only SQL migrations of internal/testkit.
package migrations

import "embed"

// FS holds all *.sql files in internal/testkit/migrations.
//
//go:embed *.sql
var FS embed.FS
