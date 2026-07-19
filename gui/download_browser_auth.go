package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

// init wires the downloader's L2 anti-bot escalation to the browser session:
// after the user (or the agent) passes an interactive challenge in the
// persistent browser, download_file / web_fetch(save_path) automatically
// reuse that session's cookies + User-Agent. It also routes browser-side
// download progress into the shared download log.
func init() {
	websearch.SetBrowserAuthProvider(func(_ context.Context, rawURL string) (map[string]string, error) {
		return browser.ExportAuthHeadersForURL(rawURL)
	})
	browser.DownloadLogf = websearch.LogDownloadf
	// Wire the standalone (corelib/agent) download path to the same browser
	// capabilities; corelib/agent cannot import corelib/browser directly.
	agent.BrowserDownloadFunc = func(ctx context.Context, rawURL, destPath string, timeoutSec int) (string, int64, error) {
		res, err := browser.DownloadViaBrowser(ctx, rawURL, destPath, timeoutSec)
		if err != nil {
			return "", 0, err
		}
		return res.SavedTo, res.Bytes, nil
	}
	agent.BrowserAuthFunc = func(_ context.Context, rawURL string) (map[string]string, error) {
		return browser.ExportAuthHeadersForURL(rawURL)
	}
}

// buildFetchHeaders collects request headers for web_fetch / download_file
// from tool args: `use_browser_cookies` (pre-inject the live browser
// session's auth), an explicit `headers` object, and the `cookie` shortcut.
// Later sources override earlier ones. Returns an error message for the agent
// when use_browser_cookies was requested but no usable session exists.
func buildFetchHeaders(args map[string]interface{}, rawURL string) (map[string]string, string) {
	headers := map[string]string{}
	if useBrowser, _ := args["use_browser_cookies"].(bool); useBrowser {
		if u, err := url.Parse(rawURL); err != nil || !strings.EqualFold(u.Scheme, "https") {
			return nil, "use_browser_cookies 仅支持 https URL（http 明文会泄漏浏览器会话 cookie）"
		}
		bh, err := browser.ExportAuthHeadersForURL(rawURL)
		if err != nil {
			return nil, fmt.Sprintf("use_browser_cookies 失败: %v（请先用 browser 工具打开目标网站完成验证后再试）", err)
		}
		for k, v := range bh {
			headers[k] = v
		}
		log.Printf("[download_file] use_browser_cookies: injected %d headers for %q", len(bh), rawURL)
	}
	if raw, ok := args["headers"].(map[string]interface{}); ok {
		for k, v := range raw {
			if s, ok := v.(string); ok && strings.TrimSpace(k) != "" {
				headers[k] = s
			}
		}
	}
	if c := strings.TrimSpace(stringVal(args, "cookie")); c != "" {
		headers["Cookie"] = c
	}
	if len(headers) == 0 {
		return nil, ""
	}
	return headers, ""
}

// downloadViaBrowserTool handles download_file(via_browser=true): the managed
// browser itself downloads the URL (Browser.setDownloadBehavior + Fetch
// interception), so the request runs with the browser's cookies, TLS
// fingerprint and JS environment.
func (h *IMMessageHandler) downloadViaBrowserTool(rawURL, absPath string, args map[string]interface{}) string {
	timeoutSec := intArg(args, "timeout", 120)
	if timeoutSec <= 0 {
		timeoutSec = 120
	}
	// Clamp like the HTTP path: the browser-wide download behavior and the
	// browserDownloadMu are held for the whole duration, so an oversized
	// timeout would hijack all other browser downloads.
	if timeoutSec > 600 {
		timeoutSec = 600
	}
	log.Printf("[download_file] via_browser url=%q abs=%q timeout=%ds", rawURL, absPath, timeoutSec)
	ctx := context.Background()
	if h != nil {
		var cancel context.CancelFunc
		ctx, cancel = h.imToolContext()
		defer cancel()
	}
	res, err := browser.DownloadViaBrowser(ctx, rawURL, absPath, timeoutSec)
	if err != nil {
		return fmt.Sprintf("浏览器下载失败: %v", err)
	}
	return fmt.Sprintf("文件已保存到 %s (%d 字节)\nsaved_path: %s\n下载过程日志: ~/.maclaw/logs/download.log", res.SavedTo, res.Bytes, res.SavedTo)
}
