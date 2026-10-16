//go:build !linux

package guiapp

func registerFileCompanionLinuxDesktop() error {
	return nil
}

// UnregisterFileCompanionLinuxDesktop is a no-op outside Linux. The desktop
// file is written only into the Linux user applications directory.
func (a *App) UnregisterFileCompanionLinuxDesktop() error {
	return nil
}
