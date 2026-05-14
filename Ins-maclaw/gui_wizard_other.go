//go:build !windows

package main

func guiRunInstallWizard(defaultBrand brandOption, currentVersion string, checkOnly, noLaunch bool) (bool, error) {
	return false, nil
}
