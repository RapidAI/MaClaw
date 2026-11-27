//go:build windows

package guiapp

// useVisualHostingWails3 enables WebView2 visual hosting for wails3 builds
// running in a Remote Desktop session. The stock windowed hosting stalls
// over RDP (the official wails3 troubleshooting doc: DPI-context changes in
// the session trigger ~2s synchronous DirectComposition re-marshals per
// controller call, and a disconnected-and-reconnected session can leave the
// surface never presenting). See https://wails.io/troubleshooting/windows/rdp
func useVisualHostingWails3() bool { return perfSessionIsRemote() }
