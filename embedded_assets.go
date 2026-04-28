package assets

import "embed"

// FrontendDist embeds the built frontend so the server can ship as a single binary.
//
//go:embed all:frontend/dist
var FrontendDist embed.FS
