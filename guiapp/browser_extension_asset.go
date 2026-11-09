package guiapp

import (
	"embed"
	"io/fs"
)

// The browser extension ships inside the binary. An installed build lives in
// Program Files, where extensions/ from the repo is not next to the exe, so a
// build that only looked on disk silently produced no folder and the tools
// card button did nothing. A dev build still prefers extensions/ on disk
// through companionExtensionSourceDir, so edits show up without a rebuild.
//
// Refresh the embedded copy with scripts/sync-companion-extension-asset.ps1;
// TestCompanionExtensionAssetMatchesSource fails when it drifts.
//
// The all: prefix keeps _locales. A plain directory pattern drops names that
// begin with "_", and the manifest needs those messages for its title.
//
//go:embed all:extension_asset/companion-paper
var companionExtensionAsset embed.FS

func companionEmbeddedExtensionFS() (fs.FS, error) {
	return fs.Sub(companionExtensionAsset, "extension_asset/companion-paper")
}
