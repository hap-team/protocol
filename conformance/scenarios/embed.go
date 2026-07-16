package scenarios

import _ "embed"

// DefaultSuccess is the canonical success scenario used by the CLI when no
// explicit scenario file is supplied.
//
//go:embed success.json
var DefaultSuccess []byte
