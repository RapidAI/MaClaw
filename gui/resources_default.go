//go:build !oem_qianxin && !windows

package main

import _ "embed"

//go:embed build/appicon.png
var icon []byte
