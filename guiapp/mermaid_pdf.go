package guiapp

import (
	"encoding/base64"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/docgen"
)

const (
	mermaidPDFRasterEvent = "maclaw:mermaid-raster"
	mermaidPDFRasterRunes = 50_000
)

// How long one diagram may take, and how long the first diagram of a document
// waits for the window to finish loading. Tests shorten these.
var (
	mermaidPDFRasterWait   = 30 * time.Second
	mermaidRasterReadyWait = 3 * time.Second
)

// A printed diagram is deterministic for a given source: the page is always
// the light theme. Remembering a few successes avoids a second trip into the
// window when the same document is exported again. Failures are not remembered.
// A timeout or a diagram that would not parse must be allowed to try again.
const (
	mermaidRasterCacheEntries = 8
	mermaidRasterCacheBytes   = 16 << 20
)

var errMermaidRasterUnavailable = errors.New("mermaid raster unavailable")

type mermaidRasterResult struct {
	png []byte
	err error
}

type mermaidCachedPNG struct {
	source string
	png    []byte
}

// installMermaidPDFRaster points PDF generation at this window's diagram
// drawer. Headless tests have no events bus, and installing there would make
// every later PDF in the process wait on a page that is not open.
func (a *App) installMermaidPDFRaster() {
	if !a.hasWailsEventsContext() {
		return
	}
	docgen.SetMermaidRasterizer(a.rasterMermaidPNG)
}

// MermaidRasterReady is called by the window once it can draw a mermaid fence.
// A PDF that started while the page was still loading unblocks here instead of
// keeping every diagram as source.
func (a *App) MermaidRasterReady() {
	if a == nil {
		return
	}
	a.installMermaidPDFRaster()
	a.mermaidRasterGaveUp.Store(false)
	ch := a.mermaidReadyNotify()
	if a.mermaidRasterReady.CompareAndSwap(false, true) {
		close(ch)
	}
}

func (a *App) mermaidReadyNotify() chan struct{} {
	a.mermaidReadyOnce.Do(func() {
		a.mermaidReadyCh = make(chan struct{})
	})
	return a.mermaidReadyCh
}

func (a *App) waitMermaidRasterReady(d time.Duration) bool {
	if a == nil {
		return false
	}
	if a.mermaidRasterReady.Load() {
		return true
	}
	ch := a.mermaidReadyNotify()
	if a.mermaidRasterReady.Load() {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ch:
		return true
	case <-timer.C:
		return a.mermaidRasterReady.Load()
	}
}

// SubmitMermaidRaster delivers the PNG for one in-flight diagram.
// An empty or undecodable payload leaves that fence as source text.
func (a *App) SubmitMermaidRaster(id, dataURL string) {
	if a == nil {
		return
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	wait, ok := a.mermaidRasterWait.Load(id)
	if !ok {
		return
	}
	ch, ok := wait.(chan mermaidRasterResult)
	if !ok {
		return
	}
	png, err := pngBytesFromDataURL(dataURL)
	select {
	case ch <- mermaidRasterResult{png: png, err: err}:
	default:
	}
}

func (a *App) rasterMermaidPNG(source string) ([]byte, error) {
	if a == nil {
		return nil, errMermaidRasterUnavailable
	}
	source = strings.TrimSpace(source)
	if source == "" || utf8.RuneCountInString(source) > mermaidPDFRasterRunes {
		return nil, errMermaidRasterUnavailable
	}
	// The event bus drops text that is not valid UTF-8, and then this wait
	// would hold the PDF for a picture that was never requested.
	if !utf8.ValidString(source) {
		log.Printf("[pdf] mermaid 流程图未能画成图片，PDF 里仍是源码: 源码不是合法文本")
		return nil, errMermaidRasterUnavailable
	}
	if png, ok := a.cachedMermaidPNG(source); ok {
		return png, nil
	}
	// A draw that already timed out means the window is not answering. The
	// rest of this document must not each wait the full timeout. A picture
	// already drawn is returned above, so the pause does not hide it.
	if until := a.mermaidRasterPauseUntil.Load(); until > time.Now().UnixNano() {
		return nil, errMermaidRasterUnavailable
	}
	if !a.hasWailsEventsContext() {
		a.logMermaidRasterSkipped()
		return nil, errMermaidRasterUnavailable
	}
	if !a.mermaidRasterReady.Load() {
		if a.mermaidRasterGaveUp.Load() || !a.waitMermaidRasterReady(mermaidRasterReadyWait) {
			a.mermaidRasterGaveUp.Store(true)
			a.logMermaidRasterSkipped()
			return nil, errMermaidRasterUnavailable
		}
	}
	id := strconv.FormatUint(a.mermaidRasterSeq.Add(1), 10)
	ch := make(chan mermaidRasterResult, 1)
	a.mermaidRasterWait.Store(id, ch)
	defer a.mermaidRasterWait.Delete(id)
	a.emitEvent(mermaidPDFRasterEvent, map[string]string{
		"id":     id,
		"source": source,
	})
	timer := time.NewTimer(mermaidPDFRasterWait)
	defer timer.Stop()
	remember := func(png []byte) ([]byte, error) {
		a.rememberMermaidPNG(source, png)
		return png, nil
	}
	select {
	case res := <-ch:
		if res.err != nil || len(res.png) == 0 {
			if res.err == nil {
				res.err = errMermaidRasterUnavailable
			}
			log.Printf("[pdf] mermaid 流程图未能画成图片，PDF 里仍是源码: %v", res.err)
			return nil, res.err
		}
		return remember(res.png)
	case <-timer.C:
		// The picture may have arrived as the timer fired.
		select {
		case res := <-ch:
			if res.err == nil && len(res.png) > 0 {
				return remember(res.png)
			}
		default:
		}
		a.mermaidRasterPauseUntil.Store(time.Now().Add(2 * time.Second).UnixNano())
		log.Printf("[pdf] mermaid 流程图绘制超时，PDF 里仍是源码")
		return nil, errMermaidRasterUnavailable
	}
}

func (a *App) cachedMermaidPNG(source string) ([]byte, bool) {
	if a == nil || source == "" {
		return nil, false
	}
	a.mermaidRasterCacheMu.Lock()
	defer a.mermaidRasterCacheMu.Unlock()
	for i := len(a.mermaidRasterCache) - 1; i >= 0; i-- {
		if a.mermaidRasterCache[i].source != source {
			continue
		}
		hit := a.mermaidRasterCache[i]
		a.mermaidRasterCache = append(a.mermaidRasterCache[:i], a.mermaidRasterCache[i+1:]...)
		a.mermaidRasterCache = append(a.mermaidRasterCache, hit)
		return hit.png, true
	}
	return nil, false
}

func (a *App) rememberMermaidPNG(source string, png []byte) {
	if a == nil || source == "" || len(png) == 0 || len(png) > mermaidRasterCacheBytes {
		return
	}
	// A data URL can decode without being a picture. Those bytes must not be
	// a cache hit: writePNG would refuse them, and the next export would skip
	// the window and leave the fence as source.
	if !docgen.PNGBytesEmbeddable(png) {
		return
	}
	a.mermaidRasterCacheMu.Lock()
	defer a.mermaidRasterCacheMu.Unlock()
	for i, hit := range a.mermaidRasterCache {
		if hit.source != source {
			continue
		}
		a.mermaidRasterCache = append(a.mermaidRasterCache[:i], a.mermaidRasterCache[i+1:]...)
		break
	}
	a.mermaidRasterCache = append(a.mermaidRasterCache, mermaidCachedPNG{source: source, png: png})
	total := 0
	for _, hit := range a.mermaidRasterCache {
		total += len(hit.png)
	}
	for len(a.mermaidRasterCache) > 0 && (len(a.mermaidRasterCache) > mermaidRasterCacheEntries || total > mermaidRasterCacheBytes) {
		total -= len(a.mermaidRasterCache[0].png)
		a.mermaidRasterCache = a.mermaidRasterCache[1:]
	}
}

func (a *App) logMermaidRasterSkipped() {
	if a.mermaidRasterSkipLogged.CompareAndSwap(false, true) {
		log.Printf("[pdf] mermaid 流程图仍按源码排版：窗口还不能画图")
	}
}

func pngBytesFromDataURL(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	comma := strings.Index(raw, ",")
	if comma < 0 {
		return nil, errMermaidRasterUnavailable
	}
	header := strings.ToLower(raw[:comma])
	if !strings.HasPrefix(header, "data:image/") || !strings.Contains(header, "base64") {
		return nil, errMermaidRasterUnavailable
	}
	payload := strings.TrimSpace(raw[comma+1:])
	if len(payload) == 0 || len(payload) > 12<<20 {
		return nil, errMermaidRasterUnavailable
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(payload)
	}
	if err != nil || len(data) == 0 || len(data) > 8<<20 {
		return nil, errMermaidRasterUnavailable
	}
	return data, nil
}
