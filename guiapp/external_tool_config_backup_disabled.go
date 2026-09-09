package guiapp

// backupToolConfigs is a compatibility seam; external tool configuration
// backup/restore is disabled.
func backupToolConfigs(_ *App, _ string) func() { return func() {} }
