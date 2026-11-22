//go:build !windows

package browser

func parkedWindowPosition() (int, int) {
	return parkedOriginForScreen(0, 0)
}

func startManagedBrowserParkLoop(string) {}

func captureParkForeground() {}
