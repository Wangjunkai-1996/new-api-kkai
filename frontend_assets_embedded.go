//go:build !external_frontend

package main

import "embed"

// Keep the default binary self-contained: both frontend themes are embedded
// for the existing single-image deployment and emergency fallback path.
//
//go:embed web/default/dist
var buildFS embed.FS

//go:embed web/default/dist/index.html
var indexPage []byte

//go:embed web/classic/dist
var classicBuildFS embed.FS

//go:embed web/classic/dist/index.html
var classicIndexPage []byte
