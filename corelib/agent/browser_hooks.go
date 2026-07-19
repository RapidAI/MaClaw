package agent

import "context"

// Optional host-wired hooks for browser-backed download features.
// corelib/agent cannot import corelib/browser (browser imports agent, which
// would be an import cycle), so hosts that have a browser wire these up at
// startup — exactly like websearch.SetBrowserAuthProvider. When nil, the
// corresponding tool parameters fail with a clear "not available in this
// environment" message instead of being silently ignored.
var (
	// BrowserDownloadFunc performs a browser-side download (CDP
	// Browser.setDownloadBehavior). Usually browser.DownloadViaBrowser.
	BrowserDownloadFunc func(ctx context.Context, rawURL, destPath string, timeoutSec int) (savedTo string, bytes int64, err error)

	// BrowserAuthFunc returns browser-session auth headers (Cookie, UA) for a
	// URL. Usually browser.ExportAuthHeadersForURL.
	BrowserAuthFunc func(ctx context.Context, rawURL string) (map[string]string, error)
)
