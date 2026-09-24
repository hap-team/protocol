package schemas

import "embed"

// Files contains the released, offline HAP schemas.
//
//go:embed 0.2/*.json
var Files embed.FS
