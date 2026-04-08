//go:build !oem_qianxin && windows

package main

import _ "embed"

//go:embed build/windows/icon.ico
var icon []byte
