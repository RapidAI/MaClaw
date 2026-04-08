//go:build desktop || production

package main

import "embed"

//go:embed all:frontend/dist
var assets embed.FS
