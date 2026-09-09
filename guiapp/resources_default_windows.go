//go:build !oem_qianxin && windows

package guiapp

import _ "embed"

//go:embed build/windows/icon.ico
var icon []byte
