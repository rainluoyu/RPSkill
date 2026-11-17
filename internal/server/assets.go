package server

import (
	"embed"
	"io/fs"
)

// UI is generated from web/dist by scripts/sync-web.sh before Go builds.
// Run make build (or make build-web before direct Go commands) in a source checkout.
// Generated assets are not tracked in Git; deployed binaries need no Node runtime.
//
//go:embed all:ui
var uiFiles embed.FS

func embeddedUI() fs.FS {
	sub, err := fs.Sub(uiFiles, "ui")
	if err != nil {
		panic(err)
	}
	return sub
}
