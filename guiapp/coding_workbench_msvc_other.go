//go:build !windows

package guiapp

import "io"

func registryVSInstallPaths() []string { return nil }

func windowsSDKRootFromRegistry() string { return "" }

func probeWindowsMSVCFromRegistry() *windowsMSVCToolchain { return nil }

func windowsLogonPathDirs() []string { return nil }

func windowsToolPathIsLocal(path string) bool { return true }

func writeWindowsCmdBat(w io.Writer, script string) error {
	_, err := io.WriteString(w, script)
	return err
}
