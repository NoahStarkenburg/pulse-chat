// Package migrations holds the SQL schema migrations and embeds them into the
// binary. Embedding means the migrate command (and tests) can apply the schema
// anywhere the binary runs, with no migration files on disk to ship alongside.
package migrations

import "embed"

// FS holds every .sql migration in this directory, in filename order.
//
//go:embed *.sql
var FS embed.FS
