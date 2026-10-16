//go:build !windows

package guiapp

func recoverBlankWebView(reason string) {
	bootLog("recoverBlankWebView skipped reason=%s (non-windows)", reason)
}

func hideNativeEnvCheckSplash() {}

func syncWebViewClientSize() {}

func captureMainWindowPNG(tag string) {
	bootLog("captureMainWindowPNG skipped tag=%s (non-windows)", tag)
}
