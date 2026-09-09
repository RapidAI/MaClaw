package petpack

import "embed"

// Bundled official pet packs (manifests + native state frames).
//
//go:embed all:bundled
var bundledRoot embed.FS

// BundledFS returns the embedded bundled/ tree (ids as top-level dirs).
func BundledFS() embed.FS {
	return bundledRoot
}
