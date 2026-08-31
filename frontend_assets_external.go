//go:build external_frontend

package main

import "embed"

// External frontend builds intentionally carry no static files. The router's
// external mode serves API routes only; the edge serves the frontend artifact.
// Keep the same symbols so main.go and the embedded fallback share one path.
var buildFS embed.FS
var indexPage []byte
var classicBuildFS embed.FS
var classicIndexPage []byte
