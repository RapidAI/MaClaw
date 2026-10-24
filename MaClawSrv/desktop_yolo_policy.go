package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/taskengine"
)

// Screen YOLO (OmniParser) policy for desktop screenshots.
//
// MACLAW_SCREEN_YOLO=auto (default) runs the detector only when all hold:
//   - the chat model is text-only (a vision model sees the image instead),
//   - the screenshot is of the desktop, not a web page read by probe refs,
//   - the focused window is not a browser, or it is a native file dialog,
//   - OCR lines plus accessibility elements are below
//     MACLAW_SCREEN_YOLO_MIN_ELEMENTS (default 15).
// on forces it for every text-only desktop screenshot, off never runs it.
// Inference always goes through the per-host model service
// (MACLAW_MODEL_SERVICE_SOCKET); without it, or when the service does not
// answer within MACLAW_SCREEN_YOLO_TIMEOUT_MS (default 8000), YOLO is skipped
// and the observation keeps OCR and accessibility. One pass costs about 2 s
// of CPU on 8 cores and peaks near 2 GB, so it is never loaded per process.

const (
	desktopYOLOModeAuto = "auto"
	desktopYOLOModeOn   = "on"
	desktopYOLOModeOff  = "off"

	desktopYOLODefaultMinElements = 15
	desktopYOLODefaultTimeout     = 8 * time.Second
)

func desktopScreenYOLOMode() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_SCREEN_YOLO"))) {
	case "on", "1", "true", "yes", "always":
		return desktopYOLOModeOn
	case "off", "0", "false", "no", "never":
		return desktopYOLOModeOff
	default:
		return desktopYOLOModeAuto
	}
}

func desktopScreenYOLOMinElements() int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("MACLAW_SCREEN_YOLO_MIN_ELEMENTS"))); err == nil && v >= 0 {
		return v
	}
	return desktopYOLODefaultMinElements
}

func desktopScreenYOLOTimeout() time.Duration {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("MACLAW_SCREEN_YOLO_TIMEOUT_MS"))); err == nil && v > 0 {
		return time.Duration(v) * time.Millisecond
	}
	return desktopYOLODefaultTimeout
}

// desktopVisionHints carries what the screenshot caller knows about the
// screen: the accessibility element count (-1 unknown) and a reader for
// the focused window's WM_CLASS and title.
type desktopVisionHints struct {
	AXCount int
	Window  func() (class, title string, ok bool)
}

type desktopVisionHintsKey struct{}

func desktopVisionHintsFrom(ctx context.Context) desktopVisionHints {
	if ctx != nil {
		if h, ok := ctx.Value(desktopVisionHintsKey{}).(desktopVisionHints); ok {
			return h
		}
	}
	return desktopVisionHints{AXCount: -1}
}

func withDesktopVisionHints(ctx context.Context, h desktopVisionHints) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, desktopVisionHintsKey{}, h)
}

// desktopEnsureVisionHints adds a focused-window reader for this desktop
// when the caller did not set hints. The reader runs xdotool only if the
// YOLO gate needs it.
func desktopEnsureVisionHints(ctx context.Context, scope agentruntime.Scope, display string) context.Context {
	if ctx == nil {
		return ctx
	}
	h, ok := ctx.Value(desktopVisionHintsKey{}).(desktopVisionHints)
	if !ok {
		h = desktopVisionHints{AXCount: -1}
	}
	if h.Window != nil {
		return ctx
	}
	h.Window = func() (string, string, bool) {
		run := func(argv ...string) (string, error) {
			if desktopRemoteApp != nil {
				return desktopRemoteApp(ctx, scope.TenantID, scope.UserID, display, argv)
			}
			if client := desktopHubClientFromEnv(); client != nil {
				return client.App(ctx, scope.TenantID, scope.UserID, display, argv)
			}
			if err := refuseLocalDesktop(ctx); err != nil {
				return "", err
			}
			return desktopAppCommand(display, argv...)
		}
		return desktopReadFocusedWindow(run)
	}
	return context.WithValue(ctx, desktopVisionHintsKey{}, h)
}

func desktopReadFocusedWindow(run func(...string) (string, error)) (string, string, bool) {
	firstLine := func(s string) string {
		s = strings.TrimSpace(s)
		if i := strings.IndexAny(s, "\r\n"); i >= 0 {
			s = strings.TrimSpace(s[:i])
		}
		return s
	}
	classOut, err := run(desktopWindowClassRead...)
	if err != nil {
		return "", "", false
	}
	class := firstLine(classOut)
	if class == "" {
		return "", "", false
	}
	title := ""
	if out, err := run("getactivewindow", "getwindowname"); err == nil {
		title = firstLine(out)
	}
	return class, title, true
}

// desktopAXElementCount counts the element ids of an app_probe listing.
func desktopAXElementCount(block string) int {
	if strings.TrimSpace(block) == "" {
		return 0
	}
	return strings.Count(block, "\n[")
}

type desktopYOLODecision struct {
	Run    bool
	Reason string
}

// decideDesktopYOLO is the pure gate. ocrCount is the OCR line count,
// axCount the accessibility element count (-1 unknown).
func decideDesktopYOLO(mode string, clickPixels bool, class, title string, winOK bool, ocrCount, axCount, minElements int) desktopYOLODecision {
	switch mode {
	case desktopYOLOModeOff:
		return desktopYOLODecision{false, "mode_off"}
	case desktopYOLOModeOn:
		return desktopYOLODecision{true, "mode_on"}
	}
	if !clickPixels {
		return desktopYOLODecision{false, "web_page"}
	}
	if !winOK {
		return desktopYOLODecision{false, "window_unknown"}
	}
	dialog := desktopFileDialogTitleRe.MatchString(strings.TrimSpace(title))
	if desktopWindowIsBrowser(class) && !dialog {
		return desktopYOLODecision{false, "browser_window"}
	}
	n := ocrCount
	if axCount > 0 {
		n += axCount
	}
	if n >= minElements {
		return desktopYOLODecision{false, fmt.Sprintf("enough_elements(%d>=%d)", n, minElements)}
	}
	if dialog {
		return desktopYOLODecision{true, fmt.Sprintf("native_dialog_few_elements(%d<%d)", n, minElements)}
	}
	return desktopYOLODecision{true, fmt.Sprintf("few_elements(%d<%d)", n, minElements)}
}

var errNoModelService = errors.New("no model service")
var errYOLOBusy = errors.New("yolo busy")

// desktopDetectViaService runs one YOLO pass on the shared model service.
// It never loads the detector in this process. A pass already in flight
// from this process makes the next one skip rather than queue.
func (m *srvAIModelManager) desktopDetectViaService(ctx context.Context, b64 string) ([]taskengine.UIElement, int64, error) {
	if m == nil || m.remote == nil {
		return nil, 0, errNoModelService
	}
	select {
	case desktopYOLOSem <- struct{}{}:
	default:
		return nil, 0, errYOLOBusy
	}
	defer func() { <-desktopYOLOSem }()
	if ctx == nil {
		ctx = context.Background()
	}
	cctx, cancel := context.WithTimeout(ctx, desktopScreenYOLOTimeout())
	defer cancel()
	var out modelServiceDetectResp
	err := m.remote.call(cctx, "/v1/detect", modelServiceImageReq{PNGBase64: b64}, &out)
	if err != nil && cctx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
		err = fmt.Errorf("timeout after %s: %w", desktopScreenYOLOTimeout(), err)
	}
	return out.Elements, out.PeakRSSKB, err
}

func desktopYOLOSkipReason(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, errNoModelService):
		return "no_model_service"
	case errors.Is(err, errYOLOBusy):
		return "busy"
	case errors.Is(err, errSrvAIModelNotReady):
		return "weights_missing"
	case strings.HasPrefix(err.Error(), "timeout after"):
		return "timeout"
	case errors.Is(err, errModelServiceUnavailable):
		return "service_unavailable"
	default:
		return "error"
	}
}

// procStatusKB reads a VmRSS/VmHWM style field of /proc/self/status (0 if
// unavailable, e.g. not Linux).
func procStatusKB(field string) int64 {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, field+":") {
			f := strings.Fields(strings.TrimPrefix(line, field+":"))
			if len(f) > 0 {
				v, _ := strconv.ParseInt(f[0], 10, 64)
				return v
			}
		}
	}
	return 0
}

// desktopVisionMetrics is one observe's log record.
type desktopVisionMetrics struct {
	YOLO        bool
	Reason      string
	WindowClass string
	OCRLines    int
	AXCount     int
	YOLOElems   int
	OCRMs       int64
	YOLOMs      int64
	TotalMs     int64
	SvcPeakKB   int64
}

func (mt desktopVisionMetrics) line() string {
	ran := "no"
	if mt.YOLO {
		ran = "yes"
	}
	return fmt.Sprintf("[desktop-vision] observe yolo=%s reason=%s window_class=%q ocr_lines=%d ax=%d yolo_elems=%d ocr_ms=%d yolo_ms=%d total_ms=%d rss_mb=%d peak_rss_mb=%d svc_peak_rss_mb=%d",
		ran, mt.Reason, mt.WindowClass, mt.OCRLines, mt.AXCount, mt.YOLOElems, mt.OCRMs, mt.YOLOMs, mt.TotalMs,
		procStatusKB("VmRSS")/1024, procStatusKB("VmHWM")/1024, mt.SvcPeakKB/1024)
}

var desktopVisionLog = func(s string) { log.Print(s) }
