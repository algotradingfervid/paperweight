// Package migrations embeds every SQL migration in this folder.
package migrations

import "embed"

// FS holds all *.sql files in migrations/.
// STUB: the //go:embed *.sql directive is added with 00001_schema_conventions.sql.
var FS embed.FS
