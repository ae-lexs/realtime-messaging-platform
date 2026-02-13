// Package apiv1 embeds the generated OpenAPI v2 specification.
package apiv1

import _ "embed"

// Spec contains the OpenAPI v2 JSON specification generated from proto
// definitions by protoc-gen-openapiv2. It is embedded at compile time so
// the binary works with scratch-based production images.
//
//go:embed openapi.swagger.json
var Spec []byte
