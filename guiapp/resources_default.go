//go:build !oem_qianxin && !windows

package guiapp

import _ "embed"

//go:embed build/appicon.png
var icon []byte
