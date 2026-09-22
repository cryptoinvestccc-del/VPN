//go:build webui

package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var built embed.FS

// Embedded returns the assets compiled into this binary.
func Embedded() (fs.FS, bool) {
	sub, err := fs.Sub(built, "dist")
	if err != nil {
		return nil, false
	}
	return sub, true
}
