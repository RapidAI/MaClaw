package main

import (
	_ "embed"
	"strings"
)

// First-party VS Code extension (Mode C, see vscode-ext/): packaged VSIX is
// committed as a build artifact so dev builds work without a Node toolchain.
// Refresh with: cd vscode-ext && npm run package
//
//go:embed vscode_ext_asset/maclaw-acp.vsix
var maclawACPVsix []byte

//go:embed vscode_ext_asset/version.txt
var maclawACPVsixVersionRaw string

func maclawACPVsixVersion() string {
	return strings.TrimSpace(maclawACPVsixVersionRaw)
}
