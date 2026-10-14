// Package webui holds the built frontend, so the panel stays one binary.
package webui

import "embed"

// Dist is web/dist. Only .gitkeep is committed there, so the Go build works
// before the frontend has been built; the panel then says the UI is missing.
//
//go:embed all:dist
var Dist embed.FS
