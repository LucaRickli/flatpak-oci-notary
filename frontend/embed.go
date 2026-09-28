//go:build !noui

// Package frontend embeds the built admin UI (frontend/build). Build it with
// `deno task build` first, or compile with `-tags noui` to omit the UI.
package frontend

import (
	"embed"
	"io/fs"
)

//go:embed all:build
var build embed.FS

// FS returns the UI files, or nil when built without the UI.
func FS() fs.FS {
	sub, err := fs.Sub(build, "build")
	if err != nil {
		panic(err)
	}
	return sub
}
