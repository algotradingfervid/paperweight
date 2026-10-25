// Package migrations embeds every SQL migration in this folder.
package migrations

import "embed"

// FS holds all *.sql files in migrations/.
//
//go:embed *.sql
var FS embed.FS
