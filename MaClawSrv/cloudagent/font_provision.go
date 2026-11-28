package cloudagent

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/docgen"
)

// Font provisioning is the cloud host's self-repair for PDF generation: a
// bare Linux host has no system CJK fonts, so rendering falls back to the
// embedded subset face. The subset covers common Chinese, but a full face
// has wider coverage — so a render that relies on the subset kicks off a
// background download into the docgen font cache dir. The detection cache
// only holds real fonts, so the next render after the download lands picks
// the full face up with no restart.
//
// MACLAW_FONT_DOWNLOAD=off disables the download; MACLAW_CJK_FONT_URL
// overrides the source list with a single URL.

const cloudProvisionedFontName = "wqy-microhei.ttc"

// cloudFontMaxBytes caps the download. WenQuanYi MicroHei is ~5MB; anything
// much larger is not a font.
const cloudFontMaxBytes = 48 << 20

// cloudCJKFontURLs fetch WenQuanYi MicroHei (Apache 2.0 / GPLv3-with-font-
// exception dual-licensed). The choice is constrained by hard requirements,
// all verified against the actual PDF engine before adoption:
//   - Noto Sans SC is disqualified twice over: upstream only ships a
//     variable font whose default instance is wght=100 (the PDF engine
//     embeds the default outlines — everything renders Thin), and its
//     static noto-cjk releases are CFF-outline, which cannot embed at all.
//   - DroidSansFallbackFull is disqualified because it is a fallback face
//     by design: it covers CJK but NOT ASCII digits/letters, ℃ or —, so a
//     weather report would print boxes for every number.
//
// MicroHei covers ASCII, Latin-1, GB2312 and beyond in a static TrueType
// face, and the .ttc container is handled by the same extraction detection
// already uses.
var cloudCJKFontURLs = []string{
	"https://cdn.jsdelivr.net/gh/anthonyfok/fonts-wqy-microhei@master/wqy-microhei.ttc",
	"https://github.com/anthonyfok/fonts-wqy-microhei/raw/master/wqy-microhei.ttc",
}

// cloudFontProvRetryInterval bounds download retries: a failed attempt
// (offline host, blocked egress) may try again after this long. Success
// needs no retry at all — the downloaded font ends the fallback state, so
// UsingEmbeddedFallback stops triggering provisioning.
var cloudFontProvRetryInterval = 10 * time.Minute

var (
	cloudFontProvMu          sync.Mutex
	cloudFontProvRunning     bool
	cloudFontProvLastAttempt time.Time
)

// MaybeProvisionCJKFont exposes font provisioning to the other PDF render
// paths in the same binary (hub bots render through bot_pdf.go, not the
// cloud session tools — a host running only bots would otherwise never
// self-repair). State is process-wide, so either path provisions for both.
func MaybeProvisionCJKFont() {
	maybeProvisionCJKFont()
}

// maybeProvisionCJKFont starts a background download when rendering is
// relying on the embedded subset. It never blocks the render path and never
// errors outwards — worst case the subset keeps serving. At most one
// download runs at a time.
func maybeProvisionCJKFont() {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("MACLAW_FONT_DOWNLOAD")), "off") {
		return
	}
	if !docgen.UsingEmbeddedFallback() {
		return
	}
	cloudFontProvMu.Lock()
	if cloudFontProvRunning || time.Since(cloudFontProvLastAttempt) < cloudFontProvRetryInterval {
		cloudFontProvMu.Unlock()
		return
	}
	cloudFontProvRunning = true
	cloudFontProvLastAttempt = time.Now()
	cloudFontProvMu.Unlock()
	go func() {
		defer func() {
			cloudFontProvMu.Lock()
			cloudFontProvRunning = false
			cloudFontProvMu.Unlock()
		}()
		if err := provisionCJKFont(context.Background(), nil); err != nil {
			log.Printf("[cloudagent] 中文字体下载失败，继续使用内嵌兜底字体: %v", err)
		}
	}()
}

// provisionCJKFont downloads the first usable font into the cache dir and
// validates it with the same rules detection applies, so a bad or hostile
// payload never lands. The write is atomic (tmp + rename): a concurrent
// detection probe either sees the old state or the finished file.
func provisionCJKFont(ctx context.Context, client *http.Client) error {
	dir := docgen.FontCacheDir()
	if dir == "" {
		return fmt.Errorf("no font cache dir")
	}
	target := filepath.Join(dir, cloudProvisionedFontName)
	if data, err := os.ReadFile(target); err == nil {
		if docgen.ValidateEmbeddableCJKFont(data) == nil {
			return nil
		}
		// The file exists but detection keeps rejecting it — overwritten
		// or corrupted after the atomic write. Keeping it bricks
		// self-repair forever (the stat shortcut above would return early
		// on every attempt), so it is removed and re-downloaded.
		log.Printf("[cloudagent] 缓存字体 %s 校验失败，重新下载", target)
		os.Remove(target)
	}
	cleanStaleFontTempFiles(dir)
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	urls := cloudCJKFontURLs
	if override := strings.TrimSpace(os.Getenv("MACLAW_CJK_FONT_URL")); override != "" {
		urls = []string{override}
	}
	var lastErr error
	for _, u := range urls {
		data, err := downloadFontBytes(ctx, client, u)
		if err != nil {
			lastErr = err
			continue
		}
		if err := docgen.ValidateEmbeddableCJKFont(data); err != nil {
			lastErr = fmt.Errorf("%s 校验失败: %w", u, err)
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		tmp, err := os.CreateTemp(dir, ".fontdl-*")
		if err != nil {
			return err
		}
		tmpName := tmp.Name()
		if _, err := tmp.Write(data); err != nil {
			tmp.Close()
			os.Remove(tmpName)
			return err
		}
		if err := tmp.Close(); err != nil {
			os.Remove(tmpName)
			return err
		}
		if err := os.Rename(tmpName, target); err != nil {
			os.Remove(tmpName)
			return err
		}
		log.Printf("[cloudagent] 已下载完整中文字体到 %s（%d 字节），后续 PDF 将使用完整字体", target, len(data))
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no font source configured")
	}
	return lastErr
}

// cleanStaleFontTempFiles removes download leftovers. A crash between
// CreateTemp and Rename strands a partial font in the cache dir forever;
// the files are small but there is no reason to keep them past a day.
// Errors are ignored — a stubborn file just waits for the next cleanup.
func cleanStaleFontTempFiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".fontdl-") {
			continue
		}
		info, err := entry.Info()
		if err != nil || time.Since(info.ModTime()) < 24*time.Hour {
			continue
		}
		os.Remove(filepath.Join(dir, entry.Name()))
	}
}

func downloadFontBytes(ctx context.Context, client *http.Client, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	// An empty User-Agent is blocked by some CDN edges; the exact value
	// does not matter, being present does.
	req.Header.Set("User-Agent", "maclaw-font-provision/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, cloudFontMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > cloudFontMaxBytes {
		return nil, fmt.Errorf("%s: 字体文件超过 %d MB 上限", u, cloudFontMaxBytes>>20)
	}
	return data, nil
}
