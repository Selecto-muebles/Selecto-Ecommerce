package migrations

import "embed"

// Files contains the immutable SQL migration sources shipped with the binary.
//
//go:embed *.sql
var Files embed.FS
