//go:build !webui

package webui

import "io/fs"

// Embedded reports that this binary carries no assets. Building with
// `-tags webui` after `npm run build` swaps in the real implementation.
func Embedded() (fs.FS, bool) { return nil, false }
