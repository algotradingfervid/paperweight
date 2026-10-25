// Package migrations embeds the test-only SQL migrations of internal/fixture
// (CD-114). They never ship in the release binary.
package migrations

import "embed"

// FS holds all *.sql files in internal/fixture/migrations.
//
//go:embed *.sql
var FS embed.FS
