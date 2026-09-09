//go:build desktop || production

package guiapp

import "embed"

//go:embed all:frontend/dist
var assets embed.FS
