// Package docs embeds the OpenAPI 3.0 specification so it can be
// served at runtime without reading from disk. The canonical spec
// lives at docs/openapi.yaml; this file simply exposes it as a
// []byte via go:embed.
package docs

import _ "embed"

// OpenAPISpec is the raw OpenAPI 3.0 YAML served at GET /api/openapi.yaml.
//
//go:embed openapi.yaml
var OpenAPISpec []byte
