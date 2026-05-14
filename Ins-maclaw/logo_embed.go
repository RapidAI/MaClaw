//go:build windows

package main

import _ "embed"

// wizardLogoPNG is the same MaClaw logo PNG used by the main GUI.
//
//go:embed assets/appicon.png
var wizardLogoPNG []byte
