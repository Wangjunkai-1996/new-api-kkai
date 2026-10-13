//go:build !external_frontend

package main

import "embed"

// Keep the default binary self-contained with the default frontend for the
// existing single-image deployment and emergency fallback path.
//
//go:embed web/default/dist
var buildFS embed.FS

//go:embed web/default/dist/index.html
var indexPage []byte
