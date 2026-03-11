// Package assets embeds the web dashboard static files into the binary.
package assets

import "embed"

// WebFS contains all files under the web/ directory.
//
//go:embed web
var WebFS embed.FS
