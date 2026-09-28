//go:build noui

package frontend

import "io/fs"

// FS returns nil: the binary was built without the UI.
func FS() fs.FS { return nil }
